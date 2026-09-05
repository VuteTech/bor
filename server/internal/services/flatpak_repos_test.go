// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestApplyFlatpakRepoRequest(t *testing.T) {
	valid := models.FlatpakRepositoryRequest{
		Name: "corp", URL: "https://repo.corp.example/flatpak/", Title: "Corp", Arches: []string{"x86_64", "aarch64"},
		GPGKeyData: "S0VZ", CatalogEnabled: true, RefreshIntervalS: 7200, Subset: "verified", CollectionID: "com.corp.Stable",
	}
	repo := &models.FlatpakRepository{}
	if err := applyFlatpakRepoRequest(repo, &valid, true); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if string(repo.GPGKey) != "KEY" || len(repo.Arches) != 2 || repo.RefreshIntervalS != 7200 {
		t.Errorf("fields not applied: %+v", repo)
	}

	// Update keeps the key when gpg_key_data is empty and clears on request.
	upd := models.FlatpakRepositoryRequest{Name: "corp", URL: valid.URL, Arches: []string{"x86_64"}, CatalogEnabled: false}
	if err := applyFlatpakRepoRequest(repo, &upd, false); err != nil {
		t.Fatal(err)
	}
	if string(repo.GPGKey) != "KEY" || repo.CatalogEnabled || repo.RefreshIntervalS != 86400 {
		t.Errorf("update semantics wrong: %+v", repo)
	}
	upd.ClearGPGKey = true
	if err := applyFlatpakRepoRequest(repo, &upd, false); err != nil || repo.GPGKey != nil {
		t.Errorf("clear_gpg_key not honoured: %v %v", err, repo.GPGKey)
	}

	invalid := []struct {
		name string
		req  models.FlatpakRepositoryRequest
		want string
	}{
		{"bad name", models.FlatpakRepositoryRequest{Name: "../x", URL: "https://x/"}, "invalid repository name"},
		{"http url", models.FlatpakRepositoryRequest{Name: "r", URL: "http://x/"}, "only https"},
		{"bad arch", models.FlatpakRepositoryRequest{Name: "r", URL: "https://x/", Arches: []string{"mips"}}, "unsupported architecture"},
		{"short interval", models.FlatpakRepositoryRequest{Name: "r", URL: "https://x/", RefreshIntervalS: 60}, "refresh_interval_s"},
		{"bad base64", models.FlatpakRepositoryRequest{Name: "r", URL: "https://x/", GPGKeyData: "!!!"}, "base64"},
		{"bad appstream", models.FlatpakRepositoryRequest{Name: "r", URL: "https://x/", AppstreamURL: "ftp://x/a.xml.gz"}, "appstream_url"},
		{"bad fingerprint", models.FlatpakRepositoryRequest{Name: "r", URL: "https://x/", GPGKeyID: "abc"}, "40-character"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			err := applyFlatpakRepoRequest(&models.FlatpakRepository{}, &tc.req, true)
			var verr *FlatpakRepoValidationError
			if err == nil || !errors.As(err, &verr) {
				t.Fatalf("expected validation error, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestFlatpakNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://dl.flathub.org/repo/flathub.flatpakrepo":           "flathub",
		"https://dl.flathub.org/beta-repo/flathub-beta.flatpakrepo": "flathub-beta",
		"https://example.com/Corp%20Apps.flatpakrepo":               "corp-apps",
		"https://example.com/":                                      "remote",
		"https://example.com/....flatpakrepo":                       "remote",
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := flatpakNameFromURL(u); got != want {
			t.Errorf("%s: got %q want %q", raw, got, want)
		}
	}
}

func TestFlatpakIconURLAndSniff(t *testing.T) {
	repo := &models.FlatpakRepository{URL: "https://dl.flathub.org/repo/"}
	u, err := flatpakIconURL(repo, "x86_64", "org.mozilla.firefox.png")
	if err != nil || u != "https://dl.flathub.org/repo/appstream/x86_64/icons/64x64/org.mozilla.firefox.png" {
		t.Errorf("icon url %q %v", u, err)
	}
	if _, err := flatpakIconURL(repo, "x86_64", "../../etc/passwd"); err == nil {
		t.Error("path traversal accepted")
	}
	if _, err := flatpakIconURL(&models.FlatpakRepository{URL: "oci+https://registry/x"}, "x86_64", "a.png"); err == nil {
		t.Error("OCI remote should have no icon URL")
	}
	if m := sniffFlatpakIcon([]byte("\x89PNG\r\n\x1a\nrest"), ""); m != "image/png" {
		t.Errorf("png sniff = %q", m)
	}
	if m := sniffFlatpakIcon([]byte("<?xml version=\"1.0\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), "image/svg+xml"); m != "" {
		t.Errorf("svg must be rejected (script-capable), got %q", m)
	}
	if m := sniffFlatpakIcon([]byte("<html><script>"), "text/html"); m != "" {
		t.Errorf("html must be rejected, got %q", m)
	}
}
