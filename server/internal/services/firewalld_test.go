// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

func TestValidateFirewalldContent_Valid(t *testing.T) {
	valid := []string{
		`{"services":["ssh","https"]}`,
		`{"zone":"public","target":"DROP","ports":[{"port":"443","protocol":"tcp"}]}`,
		`{"ports":[{"port":"5000-6000","protocol":"udp"}]}`,
		`{"masquerade":true,"forwardPorts":[{"port":"80","protocol":"tcp","toPort":"8080","toAddr":"10.0.0.5"}]}`,
		`{"richRules":["rule family=\"ipv4\" source address=\"10.0.0.0/8\" service name=\"ssh\" accept"]}`,
	}
	for _, c := range valid {
		if err := ValidateFirewalldContent(c); err != nil {
			t.Errorf("ValidateFirewalldContent(%s) = %v, want nil", c, err)
		}
	}
}

func TestValidateFirewalldContent_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":             `{}`,
		"blank":             ``,
		"bad target":        `{"target":"BOGUS"}`,
		"bad zone chars":    `{"zone":"../etc","services":["ssh"]}`,
		"long zone":         `{"zone":"thisnameiswaytoolong","services":["ssh"]}`,
		"bad protocol":      `{"ports":[{"port":"443","protocol":"icmp"}]}`,
		"port out of range": `{"ports":[{"port":"99999","protocol":"tcp"}]}`,
		"inverted range":    `{"ports":[{"port":"6000-5000","protocol":"tcp"}]}`,
		"bad to_addr":       `{"forwardPorts":[{"port":"80","protocol":"tcp","toAddr":"not-an-ip"}]}`,
		"rich not rule":     `{"richRules":["accept everything"]}`,
		"not json":          `{not json}`,
	}
	for name, content := range cases {
		if err := ValidateFirewalldContent(content); err == nil {
			t.Errorf("%s: ValidateFirewalldContent(%s) = nil, want error", name, content)
		}
	}
}
