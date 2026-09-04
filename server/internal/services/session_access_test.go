// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

func TestValidateSessionAccessContent_Valid(t *testing.T) {
	valid := map[string]string{
		"single user weekday window": `{"rules":[{"users":["alice"],"windows":[{"days":["mon","tue","wed","thu","fri"],"start":"08:00","end":"17:30"}]}]}`,
		"group target":               `{"rules":[{"groups":["students"],"windows":[{"days":["sat"],"start":"10:00","end":"12:00"}]}]}`,
		"midnight crossing":          `{"rules":[{"users":["bob"],"windows":[{"days":["fri"],"start":"22:00","end":"02:00"}]}]}`,
		"sunday into monday":         `{"rules":[{"users":["bob"],"windows":[{"days":["sun"],"start":"22:00","end":"02:00"}]}]}`,
		"full day (equal start/end)": `{"rules":[{"users":["kiosk"],"windows":[{"days":["mon","tue","wed","thu","fri","sat","sun"],"start":"00:00","end":"00:00"}]}]}`,
		"touching windows":           `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"12:00"},{"days":["mon"],"start":"12:00","end":"17:00"}]}]}`,
		"end action and warns":       `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}],"endAction":"SESSION_END_ACTION_LOGOUT","warnMinutes":[30,15,5]}]}`,
		"policy switches":            `{"enforcePam":false,"includeSsh":true,"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"multiple rules same user":   `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"12:00"}]},{"users":["alice"],"windows":[{"days":["mon"],"start":"10:00","end":"17:00"}]}]}`,
		"directory-style user name":  `{"rules":[{"users":["alice@corp.example"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
	}
	for name, c := range valid {
		if err := ValidateSessionAccessContent(c); err != nil {
			t.Errorf("%s: ValidateSessionAccessContent(%s) = %v, want nil", name, c, err)
		}
	}
}

func TestValidateSessionAccessContent_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":                     `{}`,
		"blank":                     ``,
		"not json":                  `{not json}`,
		"no rules":                  `{"enforcePam":true}`,
		"rule no targets":           `{"rules":[{"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"rule no windows":           `{"rules":[{"users":["alice"]}]}`,
		"window no days":            `{"rules":[{"users":["alice"],"windows":[{"start":"08:00","end":"17:00"}]}]}`,
		"bad day":                   `{"rules":[{"users":["alice"],"windows":[{"days":["monday"],"start":"08:00","end":"17:00"}]}]}`,
		"duplicate day":             `{"rules":[{"users":["alice"],"windows":[{"days":["mon","mon"],"start":"08:00","end":"17:00"}]}]}`,
		"bad start":                 `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"8:00","end":"17:00"}]}]}`,
		"bad end":                   `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"24:00"}]}]}`,
		"root user":                 `{"rules":[{"users":["root"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"empty user":                `{"rules":[{"users":[""],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"user with pipe":            `{"rules":[{"users":["a|b"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"user with space":           `{"rules":[{"users":["a b"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"group wildcard":            `{"rules":[{"groups":["*"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"netgroup prefix":           `{"rules":[{"groups":["@students"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"unsupported char in name":  `{"rules":[{"users":["a+b"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
		"overlap same day":          `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"12:00"},{"days":["mon"],"start":"11:00","end":"17:00"}]}]}`,
		"overlap via midnight wrap": `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"22:00","end":"02:00"},{"days":["tue"],"start":"01:00","end":"03:00"}]}]}`,
		"warn zero":                 `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}],"warnMinutes":[0]}]}`,
		"warn not descending":       `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}],"warnMinutes":[5,15,30]}]}`,
		"warn duplicate":            `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}],"warnMinutes":[15,15]}]}`,
		"bad end action":            `{"rules":[{"users":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}],"endAction":"NUKE"}]}`,
		"unknown field":             `{"rules":[{"user":["alice"],"windows":[{"days":["mon"],"start":"08:00","end":"17:00"}]}]}`,
	}
	for name, content := range cases {
		if err := ValidateSessionAccessContent(content); err == nil {
			t.Errorf("%s: ValidateSessionAccessContent(%s) = nil, want error", name, content)
		}
	}
}
