// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"net"
	"strings"
	"testing"
)

func TestNormalizeTangURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string // expected normalized URL; empty means an error is expected
		wantErr string // expected error substring
	}{
		{name: "plain http host", in: "http://tang1.corp", want: "http://tang1.corp"},
		{name: "https with port and path", in: "https://tang1.corp:8443/tang", want: "https://tang1.corp:8443/tang"},
		{name: "trailing slash dropped", in: "http://tang1.corp/", want: "http://tang1.corp"},
		{name: "path trailing slash dropped", in: "http://tang1.corp/tang/", want: "http://tang1.corp/tang"},
		{name: "surrounding whitespace trimmed", in: "  http://tang1.corp  ", want: "http://tang1.corp"},
		{name: "ipv4 literal", in: "http://10.20.30.40:7500", want: "http://10.20.30.40:7500"},
		{name: "empty", in: "", wantErr: "url is required"},
		{name: "bad scheme", in: "ftp://tang1.corp", wantErr: "invalid url"},
		{name: "credentials refused", in: "http://user:pass@tang1.corp", wantErr: "invalid url"},
		{name: "query refused", in: "http://tang1.corp/x?y=1", wantErr: "invalid url"},
		{name: "fragment refused", in: "http://tang1.corp/x#y", wantErr: "invalid url"},
		{name: "embedded whitespace refused", in: "http://tang1.corp/a b", wantErr: "invalid url"},
		{name: "quotes refused", in: `http://tang1.corp/"a"`, wantErr: "invalid url"},
		{name: "ipv6 literal refused", in: "http://[::1]:7500", wantErr: "invalid url"},
		{name: "missing host", in: "http:///adv", wantErr: "invalid url"},
		{name: "too long", in: "http://tang1.corp/" + strings.Repeat("a", 2048), wantErr: "url is too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTangURL(tt.in)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeTangURL(%q) = %q, want error containing %q", tt.in, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("normalizeTangURL(%q) error = %q, want it to contain %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeTangURL(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeTangURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTangBlockedAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // loopback
		"::1",             // loopback v6
		"0.0.0.0",         // unspecified
		"::",              // unspecified v6
		"169.254.169.254", // link-local (cloud metadata)
		"fe80::1",         // link-local v6
		"224.0.0.1",       // multicast
		"255.255.255.255", // broadcast
	}
	for _, a := range blocked {
		if err := tangBlockedAddr(net.ParseIP(a)); err == nil {
			t.Errorf("tangBlockedAddr(%s) = nil, want an error", a)
		}
	}
	// Private and public unicast addresses stay allowed - Tang normally
	// lives on the corporate network.
	allowed := []string{"10.1.2.3", "172.16.0.10", "192.168.1.20", "100.64.0.1", "203.0.113.7", "2001:db8::1"}
	for _, a := range allowed {
		if err := tangBlockedAddr(net.ParseIP(a)); err != nil {
			t.Errorf("tangBlockedAddr(%s) = %v, want nil", a, err)
		}
	}
}
