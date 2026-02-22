// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package models

import (
	"encoding/json"
	"testing"
)

func TestMeResponse_JSON(t *testing.T) {
	resp := MeResponse{
		ID:          "user-123",
		Username:    "admin",
		Email:       "admin@example.com",
		FullName:    "Admin User",
		Permissions: []string{"policy:edit", "policy:view", "user:manage"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal MeResponse: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal MeResponse: %v", err)
	}

	if decoded["id"] != "user-123" {
		t.Errorf("id = %v, want user-123", decoded["id"])
	}
	if decoded["username"] != "admin" {
		t.Errorf("username = %v, want admin", decoded["username"])
	}
	if decoded["email"] != "admin@example.com" {
		t.Errorf("email = %v, want admin@example.com", decoded["email"])
	}
	if decoded["full_name"] != "Admin User" {
		t.Errorf("full_name = %v, want Admin User", decoded["full_name"])
	}

	perms, ok := decoded["permissions"].([]interface{})
	if !ok {
		t.Fatal("permissions should be an array")
	}
	if len(perms) != 3 {
		t.Errorf("len(permissions) = %d, want 3", len(perms))
	}

	// Verify no role field exists
	if _, hasRole := decoded["role"]; hasRole {
		t.Error("MeResponse should not contain a 'role' field")
	}
}

func TestMeResponse_EmptyPermissions(t *testing.T) {
	resp := MeResponse{
		ID:          "user-456",
		Username:    "viewer",
		Email:       "viewer@example.com",
		FullName:    "Viewer",
		Permissions: []string{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal MeResponse: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal MeResponse: %v", err)
	}

	perms, ok := decoded["permissions"].([]interface{})
	if !ok {
		t.Fatal("permissions should be an array even when empty")
	}
	if len(perms) != 0 {
		t.Errorf("len(permissions) = %d, want 0", len(perms))
	}
}
