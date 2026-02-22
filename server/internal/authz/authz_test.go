// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package authz

import (
	"context"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// mockBindingRepo implements the methods used by the authorizer from UserRoleBindingRepository
type mockBindingRepo struct {
	bindings []*models.UserRoleBinding
	err      error
}

func (m *mockBindingRepo) ListByUserID(_ context.Context, _ string) ([]*models.UserRoleBinding, error) {
	return m.bindings, m.err
}

// mockRoleRepo implements the methods used by the authorizer from RoleRepository
type mockRoleRepo struct {
	permissions map[string][]*models.Permission
	err         error
}

func (m *mockRoleRepo) GetPermissionsByRoleID(_ context.Context, roleID string) ([]*models.Permission, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.permissions[roleID], nil
}

// testAuthorizer creates an authorizer with mock repos for testing
type testAuthorizer struct {
	bindingRepo *mockBindingRepo
	roleRepo    *mockRoleRepo
}

func (a *testAuthorizer) HasPermission(ctx context.Context, userID string, resource string, action string, scopeType string, scopeID *string) (bool, error) {
	bindings, err := a.bindingRepo.ListByUserID(ctx, userID)
	if err != nil {
		return false, err
	}

	var matchingBindings []*models.UserRoleBinding
	for _, b := range bindings {
		if matchesScope(b, scopeType, scopeID) {
			matchingBindings = append(matchingBindings, b)
		}
	}

	for _, b := range matchingBindings {
		perms, err := a.roleRepo.GetPermissionsByRoleID(ctx, b.RoleID)
		if err != nil {
			return false, err
		}
		for _, p := range perms {
			if p.Resource == resource && p.Action == action {
				return true, nil
			}
		}
	}

	return false, nil
}

func strPtr(s string) *string { return &s }

func TestMatchesScope_GlobalAlwaysMatches(t *testing.T) {
	binding := &models.UserRoleBinding{ScopeType: models.ScopeGlobal}

	if !matchesScope(binding, models.ScopeGlobal, nil) {
		t.Error("global binding should match global scope")
	}
	if !matchesScope(binding, models.ScopeOrganization, strPtr("org-1")) {
		t.Error("global binding should match organization scope")
	}
	if !matchesScope(binding, models.ScopeGroup, strPtr("grp-1")) {
		t.Error("global binding should match group scope")
	}
}

func TestMatchesScope_OrganizationMatch(t *testing.T) {
	binding := &models.UserRoleBinding{
		ScopeType: models.ScopeOrganization,
		ScopeID:   strPtr("org-1"),
	}

	if !matchesScope(binding, models.ScopeOrganization, strPtr("org-1")) {
		t.Error("org binding should match same org scope")
	}
	if matchesScope(binding, models.ScopeOrganization, strPtr("org-2")) {
		t.Error("org binding should not match different org scope")
	}
	if matchesScope(binding, models.ScopeGroup, strPtr("org-1")) {
		t.Error("org binding should not match group scope")
	}
}

func TestMatchesScope_GroupMatch(t *testing.T) {
	binding := &models.UserRoleBinding{
		ScopeType: models.ScopeGroup,
		ScopeID:   strPtr("grp-1"),
	}

	if !matchesScope(binding, models.ScopeGroup, strPtr("grp-1")) {
		t.Error("group binding should match same group scope")
	}
	if matchesScope(binding, models.ScopeGroup, strPtr("grp-2")) {
		t.Error("group binding should not match different group scope")
	}
	if matchesScope(binding, models.ScopeOrganization, strPtr("grp-1")) {
		t.Error("group binding should not match org scope")
	}
}

func TestMatchesScope_NilScopeID(t *testing.T) {
	binding := &models.UserRoleBinding{
		ScopeType: models.ScopeOrganization,
		ScopeID:   nil,
	}

	if matchesScope(binding, models.ScopeOrganization, strPtr("org-1")) {
		t.Error("binding with nil scope_id should not match")
	}

	binding2 := &models.UserRoleBinding{
		ScopeType: models.ScopeOrganization,
		ScopeID:   strPtr("org-1"),
	}
	if matchesScope(binding2, models.ScopeOrganization, nil) {
		t.Error("nil requested scope_id should not match")
	}
}

func TestHasPermission_GlobalAdmin(t *testing.T) {
	az := &testAuthorizer{
		bindingRepo: &mockBindingRepo{
			bindings: []*models.UserRoleBinding{
				{RoleID: "role-admin", ScopeType: models.ScopeGlobal},
			},
		},
		roleRepo: &mockRoleRepo{
			permissions: map[string][]*models.Permission{
				"role-admin": {
					{Resource: "policy", Action: "create"},
					{Resource: "policy", Action: "edit"},
					{Resource: "policy", Action: "delete"},
					{Resource: "user", Action: "manage"},
				},
			},
		},
	}

	ctx := context.Background()

	// Admin should have policy:create
	ok, err := az.HasPermission(ctx, "user-1", "policy", "create", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("global admin should have policy:create permission")
	}

	// Admin should have user:manage
	ok, err = az.HasPermission(ctx, "user-1", "user", "manage", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("global admin should have user:manage permission")
	}

	// Admin should NOT have compliance:view (not assigned)
	ok, err = az.HasPermission(ctx, "user-1", "compliance", "view", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("global admin should not have compliance:view if not assigned")
	}
}

func TestHasPermission_OrgScopedRole(t *testing.T) {
	orgID := "org-123"
	az := &testAuthorizer{
		bindingRepo: &mockBindingRepo{
			bindings: []*models.UserRoleBinding{
				{RoleID: "role-org-admin", ScopeType: models.ScopeOrganization, ScopeID: &orgID},
			},
		},
		roleRepo: &mockRoleRepo{
			permissions: map[string][]*models.Permission{
				"role-org-admin": {
					{Resource: "policy", Action: "create"},
					{Resource: "policy", Action: "view"},
				},
			},
		},
	}

	ctx := context.Background()

	// Should have permission in matching org
	ok, err := az.HasPermission(ctx, "user-1", "policy", "create", models.ScopeOrganization, &orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("org admin should have policy:create in their org")
	}

	// Should NOT have permission in different org
	otherOrg := "org-456"
	ok, err = az.HasPermission(ctx, "user-1", "policy", "create", models.ScopeOrganization, &otherOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("org admin should not have policy:create in different org")
	}
}

func TestHasPermission_NoBindings(t *testing.T) {
	az := &testAuthorizer{
		bindingRepo: &mockBindingRepo{bindings: nil},
		roleRepo:    &mockRoleRepo{permissions: map[string][]*models.Permission{}},
	}

	ctx := context.Background()

	ok, err := az.HasPermission(ctx, "user-1", "policy", "create", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("user with no bindings should not have any permission")
	}
}

func TestHasPermission_MultipleRoles(t *testing.T) {
	az := &testAuthorizer{
		bindingRepo: &mockBindingRepo{
			bindings: []*models.UserRoleBinding{
				{RoleID: "role-viewer", ScopeType: models.ScopeGlobal},
				{RoleID: "role-editor", ScopeType: models.ScopeGlobal},
			},
		},
		roleRepo: &mockRoleRepo{
			permissions: map[string][]*models.Permission{
				"role-viewer": {
					{Resource: "policy", Action: "view"},
				},
				"role-editor": {
					{Resource: "policy", Action: "edit"},
				},
			},
		},
	}

	ctx := context.Background()

	// Should have policy:view from viewer role
	ok, err := az.HasPermission(ctx, "user-1", "policy", "view", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("user should have policy:view from viewer role")
	}

	// Should have policy:edit from editor role
	ok, err = az.HasPermission(ctx, "user-1", "policy", "edit", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("user should have policy:edit from editor role")
	}

	// Should NOT have policy:delete from either role
	ok, err = az.HasPermission(ctx, "user-1", "policy", "delete", models.ScopeGlobal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("user should not have policy:delete")
	}
}

func TestHasPermission_GlobalBindingAppliesEverywhere(t *testing.T) {
	az := &testAuthorizer{
		bindingRepo: &mockBindingRepo{
			bindings: []*models.UserRoleBinding{
				{RoleID: "role-admin", ScopeType: models.ScopeGlobal},
			},
		},
		roleRepo: &mockRoleRepo{
			permissions: map[string][]*models.Permission{
				"role-admin": {
					{Resource: "policy", Action: "create"},
				},
			},
		},
	}

	ctx := context.Background()
	orgID := "org-123"

	// Global role should work even when checking org scope
	ok, err := az.HasPermission(ctx, "user-1", "policy", "create", models.ScopeOrganization, &orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("global binding should apply to organization scope checks")
	}

	groupID := "grp-456"
	ok, err = az.HasPermission(ctx, "user-1", "policy", "create", models.ScopeGroup, &groupID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("global binding should apply to group scope checks")
	}
}
