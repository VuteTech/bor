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
	"regexp"
	"strings"
	"time"
)

// ppaNameRE accepts Launchpad owner/PPA names: start with lowercase letter or
// digit, followed by lowercase letters, digits, dots, underscores, or hyphens.
var ppaNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// PPAInfoResponse is returned by GET /api/v1/ppa-info.
type PPAInfoResponse struct {
	URI         string `json:"uri"`
	Fingerprint string `json:"fingerprint,omitempty"`
	GPGKeyData  string `json:"gpgKeyData,omitempty"` // base64-encoded binary GPG key
	Warning     string `json:"warning,omitempty"`
}

// PPAInfoHandler implements GET /api/v1/ppa-info?owner=<owner>&ppa=<name>.
// It fetches the PPA's signing key from Launchpad and the Ubuntu keyserver
// server-side, eliminating any browser CORS restrictions on those external APIs.
type PPAInfoHandler struct {
	client *http.Client
}

// NewPPAInfoHandler constructs a PPAInfoHandler with a conservative HTTP client.
func NewPPAInfoHandler() *PPAInfoHandler {
	return &PPAInfoHandler{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (h *PPAInfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	owner := r.URL.Query().Get("owner")
	ppa := r.URL.Query().Get("ppa")

	// Strict validation to prevent SSRF: only allow safe Launchpad name chars.
	if !ppaNameRE.MatchString(owner) || !ppaNameRE.MatchString(ppa) {
		http.Error(w, "invalid owner or ppa name", http.StatusBadRequest)
		return
	}

	uri := fmt.Sprintf("https://ppa.launchpadcontent.net/%s/%s/ubuntu", owner, ppa)
	resp := PPAInfoResponse{URI: uri}

	// Phase 1: fetch signing key fingerprint from Launchpad REST API.
	fingerprint, err := h.fetchFingerprint(owner, ppa)
	if err != nil || fingerprint == "" {
		resp.Warning = "Could not fetch the signing key fingerprint from Launchpad. " +
			"GPG verification has been disabled — please upload the GPG key manually " +
			"or enable gpg_check after uploading."
		writePPAJSON(w, resp)
		return
	}
	resp.Fingerprint = fingerprint

	// Phase 2: fetch the ASCII-armored key from the Ubuntu keyserver and dearmor it.
	gpgKeyData, err := h.fetchAndDearmorKey(fingerprint)
	if err != nil || gpgKeyData == "" {
		resp.Warning = fmt.Sprintf(
			"Found signing key fingerprint (%s) but could not download it from the keyserver. "+
				"GPG verification has been disabled — please upload the GPG key manually.",
			fingerprint,
		)
		writePPAJSON(w, resp)
		return
	}
	resp.GPGKeyData = gpgKeyData

	writePPAJSON(w, resp)
}

// fetchFingerprint calls the Launchpad REST API to retrieve the PPA's signing
// key fingerprint.
func (h *PPAInfoHandler) fetchFingerprint(owner, ppa string) (string, error) {
	// owner and ppa are validated by ppaNameRE before reaching here, so SSRF is
	// not possible — the URL cannot be redirected to an internal address.
	url := fmt.Sprintf("https://api.launchpad.net/1.0/~%s/+archive/%s", owner, ppa) //nolint:gosec // G704: URL built from allowlisted input
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)                   //nolint:gosec // G704: same
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req) //nolint:gosec // G704: allowlisted URL
	if err != nil {
		return "", fmt.Errorf("launchpad request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("launchpad returned HTTP %d", resp.StatusCode)
	}

	var data struct {
		Fingerprint string `json:"signing_key_fingerprint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&data); err != nil {
		return "", fmt.Errorf("launchpad JSON decode: %w", err)
	}
	return data.Fingerprint, nil
}

// fetchAndDearmorKey downloads the ASCII-armored OpenPGP key for the given
// fingerprint from the Ubuntu keyserver and returns the binary form
// base64-encoded. APT's trusted.gpg.d requires binary .gpg files.
func (h *PPAInfoHandler) fetchAndDearmorKey(fingerprint string) (string, error) {
	url := fmt.Sprintf(
		"https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x%s&options=mr",
		fingerprint,
	)
	resp, err := h.client.Get(url) //nolint:noctx // no upstream context to propagate
	if err != nil {
		return "", fmt.Errorf("keyserver request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("keyserver returned HTTP %d", resp.StatusCode)
	}

	armored, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB limit
	if err != nil {
		return "", fmt.Errorf("reading keyserver response: %w", err)
	}

	binary, err := dearmorPGP(string(armored))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(binary), nil
}

// dearmorPGP strips the ASCII armor framing from an OpenPGP public key block
// and returns the raw binary packet. It does not verify the CRC-24 checksum
// (the line starting with "=") — APT verifies key integrity when it uses the key.
func dearmorPGP(armored string) ([]byte, error) {
	lines := strings.Split(strings.ReplaceAll(armored, "\r\n", "\n"), "\n")
	var bodyLines []string
	pastHeader := false

	for _, line := range lines {
		if !pastHeader {
			// The blank line marks the end of the armor header block.
			if line == "" {
				pastHeader = true
			}
			continue
		}
		// CRC-24 checksum line starts with "="; footer starts with "-----".
		if strings.HasPrefix(line, "=") || strings.HasPrefix(line, "-----") {
			break
		}
		bodyLines = append(bodyLines, line)
	}

	if len(bodyLines) == 0 {
		return nil, fmt.Errorf("no base64 payload found in PGP armor block")
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.Join(bodyLines, ""))
	if err != nil {
		return nil, fmt.Errorf("PGP armor base64 decode: %w", err)
	}
	return decoded, nil
}

func writePPAJSON(w http.ResponseWriter, v PPAInfoResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
