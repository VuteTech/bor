// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// bindingTargetOf converts a role binding's scope fields into an
// authz.BindingTarget.
func bindingTargetOf(scopeType string, scopeID *string) authz.BindingTarget {
	t := authz.BindingTarget{ScopeType: scopeType}
	if scopeID != nil {
		t.ScopeID = *scopeID
	}
	return t
}

// callerCanDelegateRole reports whether the caller may grant roleID at
// target: every permission the binding would hand out must already be held by
// the caller with at least that reach (see authz.Grants.CanDelegate). The
// caller's grants come from the permission gate; a request that did not pass
// one holds nothing and is refused.
func callerCanDelegateRole(r *http.Request, roleRepo *database.RoleRepository, roleID string, target authz.BindingTarget) (ok bool, missing string, err error) {
	perms, err := roleRepo.GetPermissionsByRoleID(r.Context(), roleID)
	if err != nil {
		return false, "", fmt.Errorf("failed to load role permissions: %w", err)
	}
	ok, missing = requestGrants(r).CanDelegate(perms, target)
	return ok, missing, nil
}

// groupRoleBindingLister lists a user group's role bindings.
type groupRoleBindingLister interface {
	ListByGroupID(ctx context.Context, groupID string) ([]*models.UserGroupRoleBinding, error)
}

// callerCanGrantGroupMembership reports whether the caller may add a user to
// groupID. Membership hands the new member every role binding of the group,
// each at its own scope, so the caller must be able to delegate all of them;
// otherwise anyone allowed to manage user groups could join (or add an
// accomplice to) a group carrying a role they do not hold.
func callerCanGrantGroupMembership(r *http.Request, roleRepo *database.RoleRepository, bindings groupRoleBindingLister, groupID string) (ok bool, missing string, err error) {
	groupBindings, err := bindings.ListByGroupID(r.Context(), groupID)
	if err != nil {
		return false, "", fmt.Errorf("failed to load group role bindings: %w", err)
	}
	for _, b := range groupBindings {
		ok, missing, err := callerCanDelegateRole(r, roleRepo, b.RoleID, bindingTargetOf(b.ScopeType, b.ScopeID))
		if err != nil || !ok {
			return ok, missing, err
		}
	}
	return true, "", nil
}

// derefString returns *s, or "" for nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
