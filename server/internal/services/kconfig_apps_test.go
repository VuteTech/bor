// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

func TestValidateKConfigPolicy_KonsoleProfile(t *testing.T) {
	valid := []string{
		`{"konsoleProfile": {"name": "Managed", "command": "/bin/bash", "historyMode": "1", "historySize": 5000, "setDefault": true}}`,
		`{"konsoleProfile": {"lock": true}}`,
		`{"konsoleProfile": {"colorScheme": "Breeze", "font": "Hack,10,-1,5,50,0,0,0,0,0", "extraIni": "# comment\n[Terminal Features]\nBlinkingCursorEnabled=false\n\n[Cursor Options]\nCursorShape=1"}}`,
	}
	for _, c := range valid {
		if err := ValidateKConfigPolicy(c); err != nil {
			t.Errorf("%s: unexpected error: %v", c, err)
		}
	}

	invalid := map[string]string{
		"bad name":         `{"konsoleProfile": {"name": "../etc", "setDefault": true}}`,
		"bad history mode": `{"konsoleProfile": {"historyMode": "7"}}`,
		"history too big":  `{"konsoleProfile": {"historyMode": "1", "historySize": 5000000}}`,
		"multiline cmd":    `{"konsoleProfile": {"command": "bash\nrm -rf /"}}`,
		"bad ini line":     `{"konsoleProfile": {"extraIni": "[General]\nthis is not an entry"}}`,
		"only empty":       `{"konsoleProfile": {"name": "Managed"}}`,
	}
	for name, c := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateKConfigPolicy(c); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestValidateKConfigPolicy_HiddenApplications(t *testing.T) {
	if err := ValidateKConfigPolicy(`{"hiddenApplications": ["org.kde.konsole.desktop", "systemsettings.desktop", "kcm_users.desktop", "libreoffice-base.desktop"]}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	invalid := map[string]string{
		"no suffix":  `{"hiddenApplications": ["org.kde.konsole"]}`,
		"path":       `{"hiddenApplications": ["../x.desktop"]}`,
		"slash":      `{"hiddenApplications": ["a/b.desktop"]}`,
		"duplicate":  `{"hiddenApplications": ["a.desktop", "a.desktop"]}`,
		"empty item": `{"hiddenApplications": [""]}`,
	}
	for name, c := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateKConfigPolicy(c); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
