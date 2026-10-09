// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// permKey is the canonical "resource:action" representation of a permission.
func permKey(resource, action string) string {
	return resource + ":" + action
}

// callerEffectivePermissions returns the set of "resource:action" permissions
// the given user holds, aggregated across their effective roles (direct role
// bindings and roles inherited through user-group membership). It is used to
// enforce that an administrator can never grant a permission they do not
// themselves possess (no privilege escalation). It draws from the same
// ListEffectiveRoleIDs source as the Authorizer, so the guard and the
// enforcement can never disagree.
func callerEffectivePermissions(ctx context.Context, roleRepo *database.RoleRepository, bindingRepo *database.UserRoleBindingRepository, userID string) (map[string]struct{}, error) {
	perms := make(map[string]struct{})
	if userID == "" {
		return perms, nil
	}
	roleIDs, err := bindingRepo.ListEffectiveRoleIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load caller role bindings: %w", err)
	}
	for _, roleID := range roleIDs {
		rolePerms, err := roleRepo.GetPermissionsByRoleID(ctx, roleID)
		if err != nil {
			return nil, fmt.Errorf("failed to load permissions for role %s: %w", roleID, err)
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

// firstUnheldPermission returns the first permission in rolePerms that the
// caller does not hold, or "" when every one is held.
func firstUnheldPermission(callerPerms map[string]struct{}, rolePerms []*models.Permission) string {
	for _, p := range rolePerms {
		key := permKey(p.Resource, p.Action)
		if _, ok := callerPerms[key]; !ok {
			return key
		}
	}
	return ""
}

// callerCanGrantRoles reports whether the caller holds every permission of
// every role in roleIDs, i.e. whether giving those roles to someone hands out
// nothing the caller does not already have. The first unheld permission is
// returned for diagnostics.
func callerCanGrantRoles(ctx context.Context, roleRepo *database.RoleRepository, userBindingRepo *database.UserRoleBindingRepository, callerID string, roleIDs []string) (ok bool, missing string, err error) {
	callerPerms, err := callerEffectivePermissions(ctx, roleRepo, userBindingRepo, callerID)
	if err != nil {
		return false, "", err
	}
	for _, roleID := range roleIDs {
		rolePerms, err := roleRepo.GetPermissionsByRoleID(ctx, roleID)
		if err != nil {
			return false, "", fmt.Errorf("failed to load permissions for role %s: %w", roleID, err)
		}
		if missing := firstUnheldPermission(callerPerms, rolePerms); missing != "" {
			return false, missing, nil
		}
	}
	return true, "", nil
}

// callerID returns the authenticated user's ID, or "".
func callerID(ctx context.Context) string {
	if claims := GetUserFromContext(ctx); claims != nil {
		return claims.UserID
	}
	return ""
}
