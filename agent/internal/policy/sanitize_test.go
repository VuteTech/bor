// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import "testing"

func TestValidatePathIdentifier(t *testing.T) {
	valid := []string{"local", "bor-repo", "repo_1", "a.b.c", "ABC123"}
	for _, id := range valid {
		if err := validatePathIdentifier("test", id); err != nil {
			t.Errorf("validatePathIdentifier(%q) = %v, want nil", id, err)
		}
		if !isValidPathIdentifier(id) {
			t.Errorf("isValidPathIdentifier(%q) = false, want true", id)
		}
	}

	invalid := []string{
		"",       // empty
		".",      // current dir
		"..",     // parent dir
		"../etc", // traversal
		"a/b",    // separator
		"a\\b",   // windows separator
		"../../etc/cron.d/x",
		"foo/../bar",
		"name with space",
		"x;rm -rf",
	}
	for _, id := range invalid {
		if err := validatePathIdentifier("test", id); err == nil {
			t.Errorf("validatePathIdentifier(%q) = nil, want error", id)
		}
		if isValidPathIdentifier(id) {
			t.Errorf("isValidPathIdentifier(%q) = true, want false", id)
		}
	}
}

func TestSanitizeAlphanumeric(t *testing.T) {
	cases := map[string]string{
		"abc-123-def":             "abc123def",
		"../etc":                  "etc",
		"a/b/c":                   "abc",
		"550e8400-e29b-41d4-a716": "550e8400e29b41d4a716",
		"":                        "",
		"...":                     "",
	}
	for in, want := range cases {
		if got := sanitizeAlphanumeric(in); got != want {
			t.Errorf("sanitizeAlphanumeric(%q) = %q, want %q", in, got, want)
		}
	}
}
