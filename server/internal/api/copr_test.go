// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import "testing"

// ── coprSanitizeID ───────────────────────────────────────────────────────────

func TestCOPRSanitizeID(t *testing.T) {
	cases := []struct {
		owner, project string
		want           string
	}{
		{"agriffis", "neovim-nightly", "copr-agriffis-neovim-nightly"},
		{"@copr", "copr", "copr-copr-copr"},
		{"@fedora-copr", "my.project", "copr-fedora-copr-my-project"},
		{"User123", "Project_X", "copr-user123-project_x"},
		{"me", "c++lib", "copr-me-c-lib"},
	}
	for _, tc := range cases {
		got := coprSanitizeID(tc.owner, tc.project)
		if got != tc.want {
			t.Errorf("coprSanitizeID(%q, %q) = %q, want %q", tc.owner, tc.project, got, tc.want)
		}
	}
}

// ── coprGPGKeyURL ────────────────────────────────────────────────────────────

func TestCOPRGPGKeyURL(t *testing.T) {
	cases := []struct {
		baseURL string
		want    string
	}{
		{
			"https://download.copr.fedorainfracloud.org/results/agriffis/neovim-nightly/fedora-42-x86_64/",
			"https://download.copr.fedorainfracloud.org/results/agriffis/neovim-nightly/pubkey.gpg",
		},
		{
			// group project — same logic applies
			"https://download.copr.fedorainfracloud.org/results/@copr/copr/fedora-42-x86_64/",
			"https://download.copr.fedorainfracloud.org/results/@copr/copr/pubkey.gpg",
		},
		{
			// no trailing slash
			"https://download.copr.fedorainfracloud.org/results/user/proj/epel-9-x86_64",
			"https://download.copr.fedorainfracloud.org/results/user/proj/pubkey.gpg",
		},
	}
	for _, tc := range cases {
		got := coprGPGKeyURL(tc.baseURL)
		if got != tc.want {
			t.Errorf("coprGPGKeyURL(%q)\n  got  %q\n  want %q", tc.baseURL, got, tc.want)
		}
	}
}

func TestCOPRGPGKeyURL_Empty(t *testing.T) {
	if got := coprGPGKeyURL(""); got != "" {
		t.Errorf("expected empty string for empty input, got %q", got)
	}
}

// ── regex validation ──────────────────────────────────────────────────────────

func TestCOPROwnerRE(t *testing.T) {
	valid := []string{"agriffis", "@copr", "@fedora-copr", "User123", "me.user", "a_b"}
	for _, s := range valid {
		if !coprOwnerRE.MatchString(s) {
			t.Errorf("coprOwnerRE should accept %q", s)
		}
	}

	invalid := []string{"", " spaces", "-leading", "@", "@@double"}
	for _, s := range invalid {
		if coprOwnerRE.MatchString(s) {
			t.Errorf("coprOwnerRE should reject %q", s)
		}
	}
}

func TestCOPRProjectRE(t *testing.T) {
	valid := []string{"neovim-nightly", "my.project", "c++lib", "proj_1"}
	for _, s := range valid {
		if !coprProjectRE.MatchString(s) {
			t.Errorf("coprProjectRE should accept %q", s)
		}
	}

	invalid := []string{"", " spaces", "-leading"}
	for _, s := range invalid {
		if coprProjectRE.MatchString(s) {
			t.Errorf("coprProjectRE should reject %q", s)
		}
	}
}

func TestCOPRChrootRE(t *testing.T) {
	valid := []string{
		"fedora-42-x86_64",
		"fedora-rawhide-x86_64",
		"epel-9-aarch64",
		"epel-10-x86_64",
		"opensuse-leap-15.6-x86_64",
	}
	for _, s := range valid {
		if !coprChrootRE.MatchString(s) {
			t.Errorf("coprChrootRE should accept %q", s)
		}
	}

	invalid := []string{"", "Fedora-42-x86_64", "-leading", "42-x86_64"}
	for _, s := range invalid {
		if coprChrootRE.MatchString(s) {
			t.Errorf("coprChrootRE should reject %q", s)
		}
	}
}
