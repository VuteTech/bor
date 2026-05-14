// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ── XML types (openSUSE One Click Install schema) ─────────────────────────────
//
// Go's encoding/xml field matching ignores the namespace when the struct tag
// does not declare one, so plain names like `xml:"group"` match elements in
// the "http://opensuse.org/Standards/One_Click_Install" default namespace.

type ympMetapackage struct {
	XMLName xml.Name   `xml:"metapackage"`
	Groups  []ympGroup `xml:"group"`
}

type ympGroup struct {
	DistVersion string          `xml:"distversion,attr"`
	Repos       []ympRepository `xml:"repositories>repository"`
	Items       []ympItem       `xml:"software>item"`
}

type ympRepository struct {
	Recommended string   `xml:"recommended,attr"`
	Name        string   `xml:"name"`
	URLs        []ympURL `xml:"url"`
}

// ympURL represents a single <url> element. The score attribute indicates
// mirror priority; the element text is the URL itself.
type ympURL struct {
	Score int    `xml:"score,attr"`
	Value string `xml:",chardata"`
}

type ympItem struct {
	// type: "package" (default) or "pattern". Patterns are not supported.
	Type string `xml:"type,attr"`
	// action: "install" (default) or "remove".
	Action string `xml:"action,attr"`
	Name   string `xml:"name"`
}

// ── Response types ────────────────────────────────────────────────────────────

// YMPImportGroup represents one distversion group extracted from a .ymp file,
// with repositories and packages translated into PackagePolicy-compatible shapes.
type YMPImportGroup struct {
	DistVersion  string             `json:"distVersion"`
	Repositories []YMPImportRepo    `json:"repositories"`
	Packages     []YMPImportPackage `json:"packages"`
	Warnings     []string           `json:"warnings,omitempty"`
}

// YMPImportRepo mirrors the TypeScript RepoEntry shape for Zypper repositories.
// GPGCheck is always false because .ymp files carry no signing key data.
type YMPImportRepo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`    // always "REPOSITORY_TYPE_ZYPPER"
	Enabled  bool   `json:"enabled"` // from <repository recommended="...">
	BaseURL  string `json:"baseurl"`
	GPGCheck bool   `json:"gpgCheck"` // always false
}

// YMPImportPackage mirrors the TypeScript PkgEntry shape.
type YMPImportPackage struct {
	Name  string `json:"name"`
	State string `json:"state"` // "PACKAGE_STATE_PRESENT" or "PACKAGE_STATE_ABSENT"
}

// YMPImportResponse is returned by POST /api/v1/ymp-import.
type YMPImportResponse struct {
	Groups []YMPImportGroup `json:"groups"`
}

// ── Handler ───────────────────────────────────────────────────────────────────

// YMPImportHandler implements POST /api/v1/ymp-import.
// It accepts a multipart form upload with a "file" field containing a .ymp XML
// file, parses it, and returns the extracted repositories and packages grouped
// by distversion so the frontend can preview and selectively merge them.
type YMPImportHandler struct{}

// NewYMPImportHandler constructs a YMPImportHandler.
func NewYMPImportHandler() *YMPImportHandler { return &YMPImportHandler{} }

const ympMaxFileBytes = 1 << 20 // 1 MiB — .ymp files are always small

func (h *YMPImportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, ympMaxFileBytes)
	if err := r.ParseMultipartForm(ympMaxFileBytes); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing 'file' field in form data", http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, ympMaxFileBytes))
	if err != nil {
		http.Error(w, "reading uploaded file", http.StatusInternalServerError)
		return
	}

	result, parseErr := parseYMP(data)
	if parseErr != nil {
		http.Error(w, "invalid .ymp file: "+parseErr.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ── Parser ────────────────────────────────────────────────────────────────────

func parseYMP(data []byte) (*YMPImportResponse, error) {
	var meta ympMetapackage
	if err := xml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if len(meta.Groups) == 0 {
		return nil, fmt.Errorf("no <group> elements found in .ymp file")
	}

	resp := &YMPImportResponse{Groups: make([]YMPImportGroup, 0, len(meta.Groups))}

	for _, g := range meta.Groups {
		grp := YMPImportGroup{DistVersion: g.DistVersion}

		seenIDs := map[string]int{}
		for _, repo := range g.Repos {
			url := ympBestURL(repo.URLs)
			if url == "" {
				grp.Warnings = append(grp.Warnings,
					fmt.Sprintf("repository %q has no URL — skipped", repo.Name))
				continue
			}

			id := ympSanitizeID(repo.Name)
			// Append a counter to make IDs unique within this group.
			if n := seenIDs[id]; n > 0 {
				id = fmt.Sprintf("%s-%d", id, n+1)
			}
			seenIDs[id]++

			grp.Repositories = append(grp.Repositories, YMPImportRepo{
				ID:       id,
				Name:     repo.Name,
				Type:     "REPOSITORY_TYPE_ZYPPER",
				Enabled:  repo.Recommended != "false",
				BaseURL:  url,
				GPGCheck: false,
			})
		}

		seenPkgs := map[string]bool{}
		for _, item := range g.Items {
			// Patterns are not individually installable via PackageKit; skip them.
			if item.Type == "pattern" {
				grp.Warnings = append(grp.Warnings,
					fmt.Sprintf("item %q is a pattern — skipped (only packages are supported)", item.Name))
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" || seenPkgs[name] {
				continue
			}
			seenPkgs[name] = true

			state := "PACKAGE_STATE_PRESENT"
			if item.Action == "remove" {
				state = "PACKAGE_STATE_ABSENT"
			}
			grp.Packages = append(grp.Packages, YMPImportPackage{Name: name, State: state})
		}

		resp.Groups = append(resp.Groups, grp)
	}

	return resp, nil
}

// ympBestURL returns the URL with the highest score from the slice,
// using the first entry as the tiebreaker when scores are equal or absent.
func ympBestURL(urls []ympURL) string {
	if len(urls) == 0 {
		return ""
	}
	best := urls[0]
	for _, u := range urls[1:] {
		if u.Score > best.Score {
			best = u
		}
	}
	return strings.TrimSpace(best.Value)
}

// ympSanitizeID converts a repository name to a valid Bor repo ID matching
// the pattern ^[a-z0-9][a-z0-9_-]*$. Colons (common in OBS project names),
// spaces, and other special characters are replaced with hyphens.
func ympSanitizeID(name string) string {
	var b strings.Builder
	prevSep := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			prevSep = false
		default:
			if !prevSep && b.Len() > 0 {
				b.WriteByte('-')
				prevSep = true
			}
		}
	}
	id := strings.TrimRight(b.String(), "-")
	if id == "" {
		id = "repo"
	}
	return id
}
