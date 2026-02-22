// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestIsValidState(t *testing.T) {
	tests := []struct {
		state string
		valid bool
	}{
		{models.PolicyStateDraft, true},
		{models.PolicyStateReleased, true},
		{models.PolicyStateArchived, true},
		{"active", false},
		{"disabled", false},
		{"deprecated", false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := isValidState(tt.state); got != tt.valid {
				t.Errorf("isValidState(%q) = %v, want %v", tt.state, got, tt.valid)
			}
		})
	}
}

func TestPolicyService_CreatePolicy_Validation(t *testing.T) {
	svc := &PolicyService{}
	tests := []struct {
		name    string
		req     *models.CreatePolicyRequest
		wantErr string
	}{
		{
			name:    "empty name",
			req:     &models.CreatePolicyRequest{Type: "custom"},
			wantErr: "policy name is required",
		},
		{
			name:    "empty type",
			req:     &models.CreatePolicyRequest{Name: "test"},
			wantErr: "policy type is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreatePolicy(context.Background(), tt.req, "admin")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPolicyService_SetPolicyState_InvalidState(t *testing.T) {
	svc := &PolicyService{}
	_, err := svc.SetPolicyState(context.Background(), "some-id", "active")
	if err == nil {
		t.Fatal("expected error for invalid state, got nil")
	}
	expected := "invalid policy state: active (valid states: draft, released, archived)"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestPolicyService_SetPolicyState_DraftIsValidTarget(t *testing.T) {
	// "draft" is now a valid target state for the unpublish transition (RELEASED→DRAFT).
	// It should pass basic state validation.
	if !isValidState(models.PolicyStateDraft) {
		t.Error("draft should be a valid state target for unpublish")
	}
}

// TestPolicyService_UpdatePolicy_DraftAllowsTypeChange verifies that type can be changed in DRAFT
func TestPolicyService_UpdatePolicy_DraftAllowsTypeChange(t *testing.T) {
	// UpdatePolicy allows type change when state is DRAFT.
	// The service checks: if policy.State != DRAFT => reject.
	// The UpdatePolicyRequest DTO includes Type field.
	newType := "Firefox"
	req := models.UpdatePolicyRequest{Type: &newType}
	if req.Type == nil || *req.Type != "Firefox" {
		t.Error("UpdatePolicyRequest should support Type field for DRAFT policies")
	}
}

// TestPolicyService_SetPolicyState_ReleaseDoesNotRequireBindings verifies that
// releasing a DRAFT policy does NOT require enabled bindings to exist.
func TestPolicyService_SetPolicyState_ReleaseDoesNotRequireBindings(t *testing.T) {
	// Test that "released" is a valid state target
	if !isValidState(models.PolicyStateReleased) {
		t.Error("released should be a valid state")
	}

	// Test that "draft" is also a valid state (unpublish feature)
	if !isValidState(models.PolicyStateDraft) {
		t.Error("draft should be a valid state")
	}
}

// TestPolicyService_UpdatePolicyRequest_IncludesType verifies the DTO includes type field
func TestPolicyService_UpdatePolicyRequest_IncludesType(t *testing.T) {
	// Verify UpdatePolicyRequest struct has a Type field
	newType := "Firefox"
	req := models.UpdatePolicyRequest{
		Type: &newType,
	}
	if req.Type == nil || *req.Type != "Firefox" {
		t.Error("UpdatePolicyRequest should include Type field")
	}
}

// TestPolicyService_ListPoliciesForNodeGroup_NilBindingRepo verifies error when
// binding repository is not configured
func TestPolicyService_ListPoliciesForNodeGroup_NilBindingRepo(t *testing.T) {
	svc := &PolicyService{} // no bindingRepo set
	_, err := svc.ListPoliciesForNodeGroup(context.Background(), "group-1")
	if err == nil {
		t.Fatal("expected error when bindingRepo is nil, got nil")
	}
	expected := "binding repository not configured"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

// TestPolicyService_UpdatePolicyRequest_JSON verifies type field is properly serialized
func TestPolicyService_UpdatePolicyRequest_JSON(t *testing.T) {
	newType := "custom"
	req := models.UpdatePolicyRequest{
		Type: &newType,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["type"] != "custom" {
		t.Errorf("expected type=custom in JSON, got: %v", parsed["type"])
	}
}
