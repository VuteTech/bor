// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNodeHandler_List_MethodNotAllowed(t *testing.T) {
	handler := &NodeHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("List() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestNodeHandler_Get_MethodNotAllowed(t *testing.T) {
	handler := &NodeHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/123", nil)
	rr := httptest.NewRecorder()

	handler.Get(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Get() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestNodeHandler_Update_MethodNotAllowed(t *testing.T) {
	handler := &NodeHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/123", nil)
	rr := httptest.NewRecorder()

	handler.Update(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Update() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestNodeHandler_CountByStatus_MethodNotAllowed(t *testing.T) {
	handler := &NodeHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/status-counts", nil)
	rr := httptest.NewRecorder()

	handler.CountByStatus(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("CountByStatus() status = %v, want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestParseNodePath(t *testing.T) {
	tests := []struct {
		name              string
		path              string
		expectedID        string
		expectedAction    string
		expectedSubAction string
	}{
		{"valid ID", "/api/v1/nodes/abc-123", "abc-123", "", ""},
		{"no ID", "/api/v1/nodes/", "", "", ""},
		{"status-counts path", "/api/v1/nodes/status-counts", "", "", ""},
		{"wrong prefix", "/api/v1/policies/all/123", "", "", ""},
		{"trailing slash", "/api/v1/nodes/abc-123/", "abc-123", "", ""},
		{"with action", "/api/v1/nodes/abc-123/refresh-metadata", "abc-123", "refresh-metadata", ""},
		{"with groups action", "/api/v1/nodes/abc-123/groups", "abc-123", "groups", ""},
		{"with groups sub-action", "/api/v1/nodes/abc-123/groups/grp-456", "abc-123", "groups", "grp-456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, action, subAction := parseNodePath(tt.path)
			if id != tt.expectedID {
				t.Errorf("parseNodePath(%q) id = %q, want %q", tt.path, id, tt.expectedID)
			}
			if action != tt.expectedAction {
				t.Errorf("parseNodePath(%q) action = %q, want %q", tt.path, action, tt.expectedAction)
			}
			if subAction != tt.expectedSubAction {
				t.Errorf("parseNodePath(%q) subAction = %q, want %q", tt.path, subAction, tt.expectedSubAction)
			}
		})
	}
}
