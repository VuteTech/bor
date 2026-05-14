// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// coprOwnerRE matches Fedora COPR owner names (users and @group names).
// Group projects have an "@" prefix; user names start with an alphanumeric character.
var (
	coprOwnerRE   = regexp.MustCompile(`^@?[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	coprProjectRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]*$`)
	// coprChrootRE matches COPR chroot names such as "fedora-42-x86_64" or "epel-9-aarch64".
	coprChrootRE = regexp.MustCompile(`^[a-z][a-z0-9._-]+$`)
)

// COPRInfoResponse is returned by GET /api/v1/copr-info.
type COPRInfoResponse struct {
	ID         string `json:"id"`
	BaseURL    string `json:"baseurl,omitempty"`
	GPGKeyData string `json:"gpgKeyData,omitempty"` // base64-encoded binary GPG key
	Warning    string `json:"warning,omitempty"`
}

// COPRInfoHandler implements GET /api/v1/copr-info?owner=<owner>&project=<project>&chroot=<chroot>.
// It calls the Fedora COPR REST API to resolve the DNF repo URL for the
// requested chroot and fetches the project's signing key server-side to avoid
// browser CORS restrictions on copr.fedorainfracloud.org.
type COPRInfoHandler struct {
	client *http.Client
}

// NewCOPRInfoHandler constructs a COPRInfoHandler.
func NewCOPRInfoHandler() *COPRInfoHandler {
	return &COPRInfoHandler{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (h *COPRInfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	owner := r.URL.Query().Get("owner")
	project := r.URL.Query().Get("project")
	chroot := r.URL.Query().Get("chroot")

	if !coprOwnerRE.MatchString(owner) || !coprProjectRE.MatchString(project) {
		http.Error(w, "invalid owner or project name", http.StatusBadRequest)
		return
	}
	if !coprChrootRE.MatchString(chroot) {
		http.Error(w, "invalid chroot name", http.StatusBadRequest)
		return
	}

	baseURL, err := h.fetchBaseURL(owner, project, chroot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	resp := COPRInfoResponse{
		ID:      coprSanitizeID(owner, project),
		BaseURL: baseURL,
	}

	// Derive the GPG key URL from the baseurl returned by COPR.
	// COPR places pubkey.gpg one directory above the chroot-specific path.
	gpgKeyURL := coprGPGKeyURL(baseURL)
	if gpgKeyData, gpgErr := h.fetchGPGKey(gpgKeyURL); gpgErr != nil {
		resp.Warning = "Could not fetch the signing key from COPR. " +
			"GPG verification has been disabled — please upload the GPG key manually " +
			"or enable gpg_check after uploading."
	} else {
		resp.GPGKeyData = gpgKeyData
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// fetchBaseURL queries the COPR API v3 project endpoint and returns the DNF
// repo URL for the requested chroot. Returns a descriptive error when the
// project or chroot does not exist.
func (h *COPRInfoHandler) fetchBaseURL(owner, project, chroot string) (string, error) {
	apiURL := fmt.Sprintf(
		"https://copr.fedorainfracloud.org/api_3/project?ownername=%s&projectname=%s",
		url.QueryEscape(owner),
		url.QueryEscape(project),
	)
	req, err := http.NewRequest(http.MethodGet, apiURL, http.NoBody) //nolint:gosec // G104/G704: URL built from allowlisted domain + validated inputs
	if err != nil {
		return "", fmt.Errorf("building COPR API request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req) //nolint:gosec // G704: allowlisted domain
	if err != nil {
		return "", fmt.Errorf("COPR API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("COPR project %s/%s not found", owner, project)
	default:
		return "", fmt.Errorf("COPR API returned HTTP %d", resp.StatusCode)
	}

	var data struct {
		ChrootRepos map[string]string `json:"chroot_repos"`
		Error       string            `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<17)).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding COPR API response: %w", err)
	}
	if data.Error != "" {
		return "", fmt.Errorf("COPR API error: %s", data.Error)
	}

	baseURL, ok := data.ChrootRepos[chroot]
	if !ok {
		return "", fmt.Errorf("chroot %q is not available for %s/%s", chroot, owner, project)
	}
	return baseURL, nil
}

// fetchGPGKey downloads the ASCII-armored GPG key from gpgKeyURL and returns
// the binary form base64-encoded. DNF reads RPM GPG keys from local files;
// the agent writes this binary to /etc/pki/rpm-gpg/ and references it.
func (h *COPRInfoHandler) fetchGPGKey(gpgKeyURL string) (string, error) {
	// Validate the derived URL as an extra safety check — it must be on the
	// COPR download host, not an attacker-controlled redirect.
	if !strings.HasPrefix(gpgKeyURL, "https://download.copr.fedorainfracloud.org/") {
		return "", fmt.Errorf("unexpected GPG key URL origin: %s", gpgKeyURL)
	}

	resp, err := h.client.Get(gpgKeyURL) //nolint:gosec // G107: URL validated above
	if err != nil {
		return "", fmt.Errorf("GPG key request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GPG key endpoint returned HTTP %d", resp.StatusCode)
	}

	armored, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading GPG key: %w", err)
	}

	binary, err := dearmorPGP(string(armored))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(binary), nil
}

// coprGPGKeyURL derives the pubkey.gpg URL from the chroot-specific baseurl
// returned by the COPR API. COPR places the key one directory above the chroot
// path regardless of whether the project belongs to a user or a group.
//
// Example:
//
//	baseurl  = "https://download.copr.fedorainfracloud.org/results/agriffis/neovim-nightly/fedora-42-x86_64/"
//	→ pubkey = "https://download.copr.fedorainfracloud.org/results/agriffis/neovim-nightly/pubkey.gpg"
func coprGPGKeyURL(baseURL string) string {
	trimmed := strings.TrimSuffix(baseURL, "/")
	slash := strings.LastIndex(trimmed, "/")
	if slash < 0 {
		return ""
	}
	return trimmed[:slash+1] + "pubkey.gpg"
}

// coprSanitizeID produces a valid Bor repo ID from a COPR owner/project pair.
// Group owners have an "@" prefix that is stripped for the ID.
func coprSanitizeID(owner, project string) string {
	o := strings.ToLower(strings.TrimPrefix(owner, "@"))
	p := strings.ToLower(project)

	sanitize := func(s string) string {
		var b strings.Builder
		prevSep := false
		for _, r := range s {
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
		return strings.TrimRight(b.String(), "-")
	}

	id := "copr-" + sanitize(o) + "-" + sanitize(p)
	return id
}
