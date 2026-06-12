// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
)

// permKey is the canonical "resource:action" representation of a permission.
func permKey(resource, action string) string {
	return resource + ":" + action
}

// callerEffectivePermissions returns the set of "resource:action" permissions
// the given user holds, aggregated across all their role bindings. It is used to
// enforce that an administrator can never grant a permission they do not
// themselves possess (no privilege escalation).
func callerEffectivePermissions(ctx context.Context, roleRepo *database.RoleRepository, bindingRepo *database.UserRoleBindingRepository, userID string) (map[string]struct{}, error) {
	perms := make(map[string]struct{})
	if userID == "" {
		return perms, nil
	}
	bindings, err := bindingRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load caller role bindings: %w", err)
	}
	for _, b := range bindings {
		rolePerms, err := roleRepo.GetPermissionsByRoleID(ctx, b.RoleID)
		if err != nil {
			return nil, fmt.Errorf("failed to load permissions for role %s: %w", b.RoleID, err)
		}
		for _, p := range rolePerms {
			perms[permKey(p.Resource, p.Action)] = struct{}{}
		}
	}
	return perms, nil
}

// rolePermissionsSubsetOf reports whether every permission attached to roleID is
// contained in the caller's permission set. The first missing permission is
// returned for diagnostics. This guarantees a caller cannot assign a role that
// grants more than they themselves hold.
func rolePermissionsSubsetOf(ctx context.Context, roleRepo *database.RoleRepository, roleID string, callerPerms map[string]struct{}) (subset bool, missing string, err error) {
	rolePerms, err := roleRepo.GetPermissionsByRoleID(ctx, roleID)
	if err != nil {
		return false, "", fmt.Errorf("failed to load role permissions: %w", err)
	}
	for _, p := range rolePerms {
		key := permKey(p.Resource, p.Action)
		if _, ok := callerPerms[key]; !ok {
			return false, key, nil
		}
	}
	return true, "", nil
}
