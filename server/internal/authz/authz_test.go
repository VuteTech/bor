// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// mockBindingSource implements RoleBindingSource. roleIDs maps a user ID to
// the user's effective role IDs (direct plus group-inherited), mirroring
// what UserRoleBindingRepository.ListEffectiveRoleIDs returns.
type mockBindingSource struct {
	roleIDs map[string][]string
	err     error
}

func (m *mockBindingSource) ListEffectiveRoleIDs(_ context.Context, userID string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.roleIDs[userID], nil
}

// mockPermissionSource implements PermissionSource.
type mockPermissionSource struct {
	permissions map[string][]*models.Permission
	err         error
}

func (m *mockPermissionSource) GetPermissionsByRoleID(_ context.Context, roleID string) ([]*models.Permission, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.permissions[roleID], nil
}

func perm(resource, action string) *models.Permission {
	return &models.Permission{Resource: resource, Action: action}
}

func TestHasPermission(t *testing.T) {
	tests := []struct {
		name     string
		roleIDs  map[string][]string
		perms    map[string][]*models.Permission
		userID   string
		resource string
		action   string
		want     bool
	}{
		{
			name:     "direct role grants permission",
			roleIDs:  map[string][]string{"u1": {"admin"}},
			perms:    map[string][]*models.Permission{"admin": {perm("policy", "write")}},
			userID:   "u1",
			resource: "policy",
			action:   "write",
			want:     true,
		},
		{
			name:     "group-inherited role grants permission",
			roleIDs:  map[string][]string{"u1": {"viewer-via-group"}},
			perms:    map[string][]*models.Permission{"viewer-via-group": {perm("policy", "read")}},
			userID:   "u1",
			resource: "policy",
			action:   "read",
			want:     true,
		},
		{
			name:     "no roles means no permission",
			roleIDs:  map[string][]string{},
			perms:    map[string][]*models.Permission{"admin": {perm("policy", "write")}},
			userID:   "u1",
			resource: "policy",
			action:   "write",
			want:     false,
		},
		{
			name:     "role without the permission is denied",
			roleIDs:  map[string][]string{"u1": {"viewer"}},
			perms:    map[string][]*models.Permission{"viewer": {perm("policy", "read")}},
			userID:   "u1",
			resource: "policy",
			action:   "write",
			want:     false,
		},
		{
			name:     "action must match exactly",
			roleIDs:  map[string][]string{"u1": {"viewer"}},
			perms:    map[string][]*models.Permission{"viewer": {perm("policy", "read")}},
			userID:   "u1",
			resource: "policy",
			action:   "delete",
			want:     false,
		},
		{
			name:     "resource must match exactly",
			roleIDs:  map[string][]string{"u1": {"viewer"}},
			perms:    map[string][]*models.Permission{"viewer": {perm("policy", "read")}},
			userID:   "u1",
			resource: "node",
			action:   "read",
			want:     false,
		},
		{
			name: "any role granting the permission is enough",
			roleIDs: map[string][]string{
				"u1": {"viewer", "node-admin"},
			},
			perms: map[string][]*models.Permission{
				"viewer":     {perm("policy", "read")},
				"node-admin": {perm("node", "manage")},
			},
			userID:   "u1",
			resource: "node",
			action:   "manage",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			az := NewFromSources(
				&mockBindingSource{roleIDs: tt.roleIDs},
				&mockPermissionSource{permissions: tt.perms},
			)

			got, err := az.HasPermission(context.Background(), tt.userID, tt.resource, tt.action)
			if err != nil {
				t.Fatalf("HasPermission() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("HasPermission() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPermission_BindingSourceError(t *testing.T) {
	az := NewFromSources(
		&mockBindingSource{err: errors.New("db down")},
		&mockPermissionSource{},
	)

	if _, err := az.HasPermission(context.Background(), "u1", "policy", "read"); err == nil {
		t.Error("HasPermission() should propagate binding source errors")
	}
}

func TestHasPermission_PermissionSourceError(t *testing.T) {
	az := NewFromSources(
		&mockBindingSource{roleIDs: map[string][]string{"u1": {"admin"}}},
		&mockPermissionSource{err: errors.New("db down")},
	)

	if _, err := az.HasPermission(context.Background(), "u1", "policy", "read"); err == nil {
		t.Error("HasPermission() should propagate permission source errors")
	}
}
