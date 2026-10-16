// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

func TestValidateKConfigPolicy_UnixGroups(t *testing.T) {
	valid := []string{
		`{"shellAccess": false, "unixGroups": ["students"]}`,
		`{"shellAccess": false, "unixGroups": ["Domain Users", "teachers@ad.example.com", "DOMAIN\\staff"]}`,
	}
	for _, c := range valid {
		if err := ValidateKConfigPolicy(c); err != nil {
			t.Errorf("%s: unexpected error: %v", c, err)
		}
	}

	invalid := map[string]string{
		"empty name":   `{"shellAccess": false, "unixGroups": [" "]}`,
		"slash":        `{"shellAccess": false, "unixGroups": ["a/b"]}`,
		"colon":        `{"shellAccess": false, "unixGroups": ["a:b"]}`,
		"duplicate":    `{"shellAccess": false, "unixGroups": ["Students", "students"]}`,
		"only a scope": `{"unixGroups": ["students"]}`,
	}
	for name, c := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateKConfigPolicy(c); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
