// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package authz provides role-based access control for Bor.
//
// RBAC is global-only: a role binding grants its role's permissions
// everywhere. Scoped (per-organization, per-group) bindings are not
// supported; delegated administration is a planned feature and will be
// introduced with an explicit scope model when it ships.
package authz

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// Authorizer defines the interface for checking user permissions
type Authorizer interface {
	HasPermission(ctx context.Context, userID, resource, action string) (bool, error)
}

// RoleBindingSource yields the effective role IDs a user holds (direct
// bindings plus bindings inherited through user-group membership).
// Implemented by database.UserRoleBindingRepository.
type RoleBindingSource interface {
	ListEffectiveRoleIDs(ctx context.Context, userID string) ([]string, error)
}

// PermissionSource yields the permissions attached to a role.
// Implemented by database.RoleRepository.
type PermissionSource interface {
	GetPermissionsByRoleID(ctx context.Context, roleID string) ([]*models.Permission, error)
}

// authorizer implements the Authorizer interface using the RBAC database tables
type authorizer struct {
	bindings RoleBindingSource
	roles    PermissionSource
}

// New creates a new Authorizer
func New(bindingRepo *database.UserRoleBindingRepository, roleRepo *database.RoleRepository) Authorizer {
	return NewFromSources(bindingRepo, roleRepo)
}

// NewFromSources creates an Authorizer from the narrow source interfaces.
func NewFromSources(bindings RoleBindingSource, roles PermissionSource) Authorizer {
	return &authorizer{bindings: bindings, roles: roles}
}

// HasPermission checks if a user holds a permission through any of their
// effective roles (direct role bindings and user-group role bindings).
func (a *authorizer) HasPermission(ctx context.Context, userID, resource, action string) (bool, error) {
	roleIDs, err := a.bindings.ListEffectiveRoleIDs(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("failed to fetch role bindings: %w", err)
	}

	for _, roleID := range roleIDs {
		perms, err := a.roles.GetPermissionsByRoleID(ctx, roleID)
		if err != nil {
			return false, fmt.Errorf("failed to fetch permissions for role %s: %w", roleID, err)
		}

		for _, p := range perms {
			if p.Resource == resource && p.Action == action {
				return true, nil
			}
		}
	}

	return false, nil
}
