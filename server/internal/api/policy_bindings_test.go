// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicyBindingHandler_List_MethodNotAllowed(t *testing.T) {
	handler := &PolicyBindingHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-bindings", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("List() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestPolicyBindingHandler_Create_MethodNotAllowed(t *testing.T) {
	handler := &PolicyBindingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-bindings", nil)
	rr := httptest.NewRecorder()

	handler.Create(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Create() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestExtractBindingIDFromPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"empty path", "/api/v1/policy-bindings", ""},
		{"with trailing slash", "/api/v1/policy-bindings/", ""},
		{"with id", "/api/v1/policy-bindings/abc-123", "abc-123"},
		{"with id and trailing slash", "/api/v1/policy-bindings/abc-123/", "abc-123"},
		{"wrong prefix", "/api/v1/nodes/abc-123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBindingIDFromPath(tt.path)
			if got != tt.want {
				t.Errorf("extractBindingIDFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
