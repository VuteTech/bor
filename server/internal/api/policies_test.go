// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicyHandler_List_MethodNotAllowed(t *testing.T) {
	handler := &PolicyHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("List() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestExtractPolicyIDAndSubpath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantID      string
		wantSubpath string
	}{
		{"empty path", "/api/v1/policies/all", "", ""},
		{"with trailing slash only", "/api/v1/policies/all/", "", ""},
		{"with id", "/api/v1/policies/all/abc-123", "abc-123", ""},
		{"with id trailing slash", "/api/v1/policies/all/abc-123/", "abc-123", ""},
		{"with id and state subpath", "/api/v1/policies/all/abc-123/state", "abc-123", "state"},
		{"with id and deprecate subpath", "/api/v1/policies/all/abc-123/deprecate", "abc-123", "deprecate"},
		{"wrong prefix", "/api/v1/nodes/abc-123", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotSubpath := extractPolicyIDAndSubpath(tt.path)
			if gotID != tt.wantID {
				t.Errorf("extractPolicyIDAndSubpath(%q) id = %q, want %q", tt.path, gotID, tt.wantID)
			}
			if gotSubpath != tt.wantSubpath {
				t.Errorf("extractPolicyIDAndSubpath(%q) subpath = %q, want %q", tt.path, gotSubpath, tt.wantSubpath)
			}
		})
	}
}
