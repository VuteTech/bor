// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
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
	tests := []struct {
		name       string
		xff        string
		xri        string
		remoteAddr string
		want       string
	}{
		{"xff first", "1.2.3.4, 5.6.7.8", "", "9.10.11.12:1234", "1.2.3.4"},
		{"xri", "", "10.0.0.1", "9.10.11.12:1234", "10.0.0.1"},
		{"remoteAddr with port", "", "", "192.168.1.1:4567", "192.168.1.1"},
		{"remoteAddr no port", "", "", "192.168.1.1", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			got := extractIP(req)
			if got != tt.want {
				t.Errorf("extractIP() = %q, want %q", got, tt.want)
			}
		})
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
