// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// Object-level node-group scope rules for delegated administration.
//
// A policy belongs to every node group it is bound to (enabled or not). A
// policy bound nowhere belongs to no group; a delegated administrator can
// still work on such a policy when they created it, which is how a scoped
// administrator authors a draft before binding it to one of their groups.

// isPolicyOwner reports whether userID created the policy.
func isPolicyOwner(p *models.Policy, userID string) bool {
	return userID != "" && p.CreatedByUserID != nil && *p.CreatedByUserID == userID
}

// canViewPolicy is the policy read rule: global grant, or the policy is bound
// to at least one in-scope group, or the caller owns it.
func canViewPolicy(scope authz.Scope, p *models.Policy, groupIDs []string, userID string) bool {
	if scope.IsEmpty() {
		return false
	}
	return scope.AllowsAny(groupIDs) || isPolicyOwner(p, userID)
}

// canWritePolicy is the policy write rule: global grant, or every group the
// policy is bound to is in scope, or the policy is bound nowhere and the
// caller owns it. A bound policy is never writable through ownership alone,
// since a change would reach groups outside the owner's scope.
func canWritePolicy(scope authz.Scope, p *models.Policy, groupIDs []string, userID string) bool {
	if scope.IsGlobal() {
		return true
	}
	if scope.IsEmpty() {
		return false
	}
	if len(groupIDs) == 0 {
		return isPolicyOwner(p, userID)
	}
	return scope.AllowsAll(groupIDs)
}

// policyGroupSource resolves which node groups policies are bound to.
// Implemented by services.PolicyBindingService.
type policyGroupSource interface {
	GetGroupIDsForPolicy(ctx context.Context, policyID string) ([]string, error)
	ListPolicyGroupRefs(ctx context.Context) ([]database.PolicyGroupRef, error)
}

// policyGroupIndex maps policy ID to its bound groups, and counts bindings.
type policyGroupIndex struct {
	groups map[string][]string
	refs   []database.PolicyGroupRef
}

func loadPolicyGroupIndex(ctx context.Context, src policyGroupSource) (*policyGroupIndex, error) {
	if src == nil {
		return nil, fmt.Errorf("policy binding lookup is not configured")
	}
	refs, err := src.ListPolicyGroupRefs(ctx)
	if err != nil {
		return nil, err
	}
	idx := &policyGroupIndex{groups: make(map[string][]string), refs: refs}
	seen := make(map[[2]string]bool)
	for _, ref := range refs {
		k := [2]string{ref.PolicyID, ref.GroupID}
		if !seen[k] {
			seen[k] = true
			idx.groups[ref.PolicyID] = append(idx.groups[ref.PolicyID], ref.GroupID)
		}
	}
	return idx, nil
}

// filterVisiblePolicies keeps the policies the caller may view. For a scoped
// caller, binding counts are recomputed over in-scope bindings only, so the
// list does not reveal how widely a shared policy is bound elsewhere.
func filterVisiblePolicies(policies []*models.Policy, idx *policyGroupIndex, scope authz.Scope, userID string) []*models.Policy {
	if scope.IsGlobal() {
		return policies
	}
	visible := make([]*models.Policy, 0, len(policies))
	for _, p := range policies {
		if canViewPolicy(scope, p, idx.groups[p.ID], userID) {
			visible = append(visible, p)
		}
	}
	type counts struct{ total, enabled int }
	inScope := make(map[string]counts)
	for _, ref := range idx.refs {
		if !scope.Allows(ref.GroupID) {
			continue
		}
		c := inScope[ref.PolicyID]
		c.total++
		if ref.Enabled {
			c.enabled++
		}
		inScope[ref.PolicyID] = c
	}
	for _, p := range visible {
		c := inScope[p.ID]
		p.BindingsCount = c.total
		p.EnabledBindingsCount = c.enabled
	}
	return visible
}

// callerID returns the authenticated user's ID, or "".
func callerID(ctx context.Context) string {
	if claims := GetUserFromContext(ctx); claims != nil {
		return claims.UserID
	}
	return ""
}
