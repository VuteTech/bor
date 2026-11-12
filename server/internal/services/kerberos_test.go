// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

func TestKerberosNodeName(t *testing.T) {
	tests := []struct {
		name      string
		principal string
		requested string
		want      string
		wantErr   bool
	}{
		{"host principal, no name", "host/ws01.example.com@EXAMPLE.COM", "", "ws01.example.com", false},
		{"host principal, fqdn", "host/ws01.example.com@EXAMPLE.COM", "ws01.example.com", "ws01.example.com", false},
		{"host principal, short name", "host/ws01.example.com@EXAMPLE.COM", "ws01", "ws01", false},
		{"host principal, case ignored", "host/ws01.example.com@EXAMPLE.COM", "WS01.Example.com", "WS01.Example.com", false},
		{"host principal, other host", "host/ws01.example.com@EXAMPLE.COM", "ws02", "", true},
		{"host principal, other domain", "host/ws01.example.com@EXAMPLE.COM", "ws01.evil.com", "", true},
		{"host principal, trailing dot", "host/ws01.example.com.@EXAMPLE.COM", "", "ws01.example.com", false},
		{"ad machine account, no name", "WS01$@EXAMPLE.COM", "", "ws01", false},
		{"ad machine account, short", "WS01$@EXAMPLE.COM", "ws01", "ws01", false},
		{"ad machine account, fqdn", "WS01$@EXAMPLE.COM", "ws01.corp.example.com", "ws01.corp.example.com", false},
		{"ad machine account, other host", "WS01$@EXAMPLE.COM", "ws02.corp.example.com", "", true},
		{"user principal", "alice@EXAMPLE.COM", "", "", true},
		{"user principal posing as host", "alice@EXAMPLE.COM", "alice", "", true},
		{"service principal", "HTTP/web.example.com@EXAMPLE.COM", "web.example.com", "", true},
		{"empty host", "host/@EXAMPLE.COM", "", "", true},
		{"bare dollar", "$@EXAMPLE.COM", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KerberosNodeName(tt.principal, tt.requested)
			if (err != nil) != tt.wantErr {
				t.Fatalf("KerberosNodeName(%q, %q) error = %v, wantErr %v", tt.principal, tt.requested, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("KerberosNodeName(%q, %q) = %q, want %q", tt.principal, tt.requested, got, tt.want)
			}
		})
	}
}
