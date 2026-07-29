// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowlistedRedirect(t *testing.T) {
	policy := allowlistedRedirect("api.launchpad.net", "keyserver.ubuntu.com")

	tests := []struct {
		name    string
		target  string
		hops    int
		allowed bool
	}{
		{"allowlisted host", "https://api.launchpad.net/x", 1, true},
		{"second allowlisted host", "https://keyserver.ubuntu.com/y", 1, true},
		{"internal host blocked", "https://169.254.169.254/latest/meta-data/", 1, false},
		{"loopback blocked", "https://127.0.0.1/", 1, false},
		{"non-https blocked", "http://api.launchpad.net/x", 1, false},
		{"unrelated host blocked", "https://evil.example.com/", 1, false},
		{"too many hops", "https://api.launchpad.net/x", maxOutboundRedirects, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			via := make([]*http.Request, tt.hops)
			err := policy(req, via)
			if tt.allowed && err != nil {
				t.Errorf("expected redirect to %s allowed, got error: %v", tt.target, err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected redirect to %s blocked, got nil error", tt.target)
			}
		})
	}
}
