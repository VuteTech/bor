// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import "testing"

// ── ExtractVersionFromPackageID ───────────────────────────────────────────────

func TestExtractVersionFromPackageID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"vim;9.0.1000-1.fc39;x86_64;fedora", "9.0.1000-1.fc39"},
		{"google-chrome-stable;125.0.6422.60-1;amd64;", "125.0.6422.60-1"},
		{"curl;8.1.2-4;x86_64;installed", "8.1.2-4"},
		{"pkg;1.0;;", "1.0"},
		{"noversion", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := ExtractVersionFromPackageID(tc.id)
		if got != tc.want {
			t.Errorf("ExtractVersionFromPackageID(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
