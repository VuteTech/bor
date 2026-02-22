// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package authz

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// Authorizer defines the interface for checking user permissions
type Authorizer interface {
	HasPermission(ctx context.Context, userID string, resource string, action string, scopeType string, scopeID *string) (bool, error)
}

// authorizer implements the Authorizer interface using the RBAC database tables
type authorizer struct {
	bindingRepo *database.UserRoleBindingRepository
	roleRepo    *database.RoleRepository
}

// New creates a new Authorizer
func New(bindingRepo *database.UserRoleBindingRepository, roleRepo *database.RoleRepository) Authorizer {
	return &authorizer{
		bindingRepo: bindingRepo,
		roleRepo:    roleRepo,
	}
}

// HasPermission checks if a user has a specific permission within a given scope.
//
// Logic:
//   - Fetch all role bindings for the user
//   - Filter by matching scope:
//   - "global" applies everywhere
//   - "organization" applies only if scope matches
//   - "group" applies only if scope matches
//   - Collect permissions via role_permissions
//   - Match resource + action
func (a *authorizer) HasPermission(ctx context.Context, userID string, resource string, action string, scopeType string, scopeID *string) (bool, error) {
	bindings, err := a.bindingRepo.ListByUserID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("failed to fetch role bindings: %w", err)
	}

	// Filter bindings by scope
	var matchingBindings []*models.UserRoleBinding
	for _, b := range bindings {
		if matchesScope(b, scopeType, scopeID) {
			matchingBindings = append(matchingBindings, b)
		}
	}

	// Check permissions for each matching role
	for _, b := range matchingBindings {
		perms, err := a.roleRepo.GetPermissionsByRoleID(ctx, b.RoleID)
		if err != nil {
			return false, fmt.Errorf("failed to fetch permissions for role %s: %w", b.RoleID, err)
		}

		for _, p := range perms {
			if p.Resource == resource && p.Action == action {
				return true, nil
			}
		}
	}

	return false, nil
}

// matchesScope checks if a user role binding matches the requested scope.
// Global scope always matches regardless of the requested scope.
// For organization and group scopes, both the scope type and scope ID must match.
// Returns false if either the binding's ScopeID or the requested scopeID is nil
// for non-global scopes, ensuring explicit scope matching for security.
func matchesScope(binding *models.UserRoleBinding, scopeType string, scopeID *string) bool {
	// Global bindings always apply
	if binding.ScopeType == models.ScopeGlobal {
		return true
	}

	// Scope types must match
	if binding.ScopeType != scopeType {
		return false
	}

	// For non-global scopes, scope IDs must match
	if binding.ScopeID == nil || scopeID == nil {
		return false
	}

	return *binding.ScopeID == *scopeID
}
