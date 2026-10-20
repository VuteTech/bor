// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

// The relying party ID must be a DNS name. The WebAuthn library rejects an
// IP address or a malformed name when the service is created, so a bad
// configuration fails at startup instead of when a user first tries a key.
func TestNewWebAuthnService_RPID(t *testing.T) {
	tests := []struct {
		name    string
		rpid    string
		origins []string
		wantErr bool
	}{
		{name: "dns name with explicit origin", rpid: "bor.example.com", origins: []string{"https://bor.example.com:8443"}},
		{name: "localhost with default origin", rpid: "localhost"},
		{name: "empty", rpid: "", wantErr: true},
		{name: "ipv4", rpid: "192.168.1.10", origins: []string{"https://192.168.1.10:8443"}, wantErr: true},
		{name: "ipv6", rpid: "2001:db8::1", wantErr: true},
		{name: "trailing dot", rpid: "bor.example.com.", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWebAuthnService(nil, tc.rpid, "Bor", tc.origins)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewWebAuthnService(rpid=%q) error = %v, wantErr %v", tc.rpid, err, tc.wantErr)
			}
		})
	}
}
