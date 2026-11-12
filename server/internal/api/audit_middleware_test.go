// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestParseResourceFromPath(t *testing.T) {
	tests := []struct {
		path           string
		wantResource   string
		wantResourceID string
	}{
		{"/api/v1/policies", "policies", ""},
		{"/api/v1/policies/abc-123", "policies", "abc-123"},
		{"/api/v1/nodes/node-1/", "nodes", "node-1"},
		{"/api/v1/node-groups", "node-groups", ""},
		{"/api/v1/user-groups/grp-1/members", "user-groups", "grp-1"},
		{"/api/v1/roles/role-1/permissions", "roles", "role-1"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotResource, gotID := parseResourceFromPath(tt.path)
			if gotResource != tt.wantResource {
				t.Errorf("parseResourceFromPath(%q) resource = %q, want %q", tt.path, gotResource, tt.wantResource)
			}
			if gotID != tt.wantResourceID {
				t.Errorf("parseResourceFromPath(%q) resourceID = %q, want %q", tt.path, gotID, tt.wantResourceID)
			}
		})
	}
}

func TestMethodToAction(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodPost, "create"},
		{http.MethodPut, "update"},
		{http.MethodPatch, "update"},
		{http.MethodDelete, "delete"},
		{http.MethodGet, "GET"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := methodToAction(tt.method)
			if got != tt.want {
				t.Errorf("methodToAction(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestExtractIP(t *testing.T) {
	proxies := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.0.2.1/32"),
	}
	tests := []struct {
		name       string
		trusted    []netip.Prefix
		xff        []string
		xri        string
		remoteAddr string
		want       string
	}{
		{"no proxies: xff ignored", nil, []string{"1.2.3.4"}, "", "9.10.11.12:1234", "9.10.11.12"},
		{"no proxies: xri ignored", nil, nil, "1.2.3.4", "9.10.11.12:1234", "9.10.11.12"},
		{"untrusted peer: xff ignored", proxies, []string{"1.2.3.4"}, "", "9.10.11.12:1234", "9.10.11.12"},
		{"trusted peer: single hop", proxies, []string{"1.2.3.4"}, "", "10.1.1.1:1234", "1.2.3.4"},
		{"trusted peer: spoofed left entry skipped", proxies, []string{"6.6.6.6, 1.2.3.4"}, "", "10.1.1.1:1234", "1.2.3.4"},
		{"trusted chain skipped", proxies, []string{"1.2.3.4, 192.0.2.1"}, "", "10.1.1.1:1234", "1.2.3.4"},
		{"multiple header lines", proxies, []string{"6.6.6.6", "1.2.3.4"}, "", "10.1.1.1:1234", "1.2.3.4"},
		{"all hops trusted", proxies, []string{"10.2.2.2"}, "", "10.1.1.1:1234", "10.2.2.2"},
		{"malformed hop stops the walk", proxies, []string{"1.2.3.4, garbage, 10.3.3.3"}, "", "10.1.1.1:1234", "10.3.3.3"},
		{"trusted peer: xri", proxies, nil, "5.6.7.8", "10.1.1.1:1234", "5.6.7.8"},
		{"trusted peer: bad xri", proxies, nil, "nope", "10.1.1.1:1234", "10.1.1.1"},
		{"ipv6 peer", nil, nil, "", "[2001:db8::1]:443", "2001:db8::1"},
		{"remoteAddr no port", nil, nil, "", "192.168.1.1", "192.168.1.1"},
	}

	t.Cleanup(func() { ConfigureClientIP(nil, false) })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ConfigureClientIP(tt.trusted, false)
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			if got := extractIP(req); got != tt.want {
				t.Errorf("extractIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultAuditIPAnonymizes(t *testing.T) {
	t.Cleanup(func() { ConfigureClientIP(nil, false) })
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", http.NoBody)
	req.RemoteAddr = "203.0.113.77:5555"

	ConfigureClientIP(nil, false)
	if got := defaultAuditIP(req); got != "203.0.113.77" {
		t.Errorf("defaultAuditIP() = %q, want plain address", got)
	}
	ConfigureClientIP(nil, true)
	if got := defaultAuditIP(req); got != "203.0.113.0" {
		t.Errorf("defaultAuditIP() = %q, want anonymized 203.0.113.0", got)
	}
}

func TestStatusRecorder(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, statusCode: http.StatusOK}

	sr.WriteHeader(http.StatusCreated)
	if sr.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %d, want %d", sr.statusCode, http.StatusCreated)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("underlying recorder code = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestRedactBody(t *testing.T) {
	body := map[string]interface{}{
		"name":        "corp",
		"password":    "hunter2",
		"panel_input": "-----BEGIN PRIVATE KEY-----",
		"items":       []interface{}{map[string]interface{}{"api_key": "k", "value": "kept"}},
	}
	redactBody(body)

	for _, key := range []string{"password", "panel_input"} {
		if body[key] != "[REDACTED]" {
			t.Errorf("%s = %v, want [REDACTED]", key, body[key])
		}
	}
	if body["name"] != "corp" {
		t.Errorf("name = %v, want it kept", body["name"])
	}
	item := body["items"].([]interface{})[0].(map[string]interface{})
	if item["api_key"] != "[REDACTED]" || item["value"] != "kept" {
		t.Errorf("nested item = %v", item)
	}
}
