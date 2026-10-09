// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package authz provides role-based access control for Bor.
//
// A role binding is either global or scoped to one node group (delegated
// administration). A global binding grants its role's permissions
// everywhere. A node-group binding grants only the permissions on scopable
// resources (see IsScopable), and only for objects that belong to that node
// group; permissions on every other resource (users, roles, settings, audit
// logs, disk-encryption escrow, ...) are never granted by a scoped binding,
// whatever role it carries. That rule is enforced here, in one place, so no
// caller can get it wrong.
package authz

import (
	"context"
	"fmt"
	"sort"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// scopableResources are the resources a node-group-scoped role binding can
// grant. Every one of them maps to node groups: nodes by membership, node
// groups by identity, policies and policy bindings through the groups the
// policy is bound to, compliance results through their node.
var scopableResources = map[string]bool{
	"node":       true,
	"node_group": true,
	"policy":     true,
	"binding":    true,
	"compliance": true,
}

// IsScopable reports whether a node-group-scoped binding can grant
// permissions on resource.
func IsScopable(resource string) bool {
	return scopableResources[resource]
}

// PermKey is the canonical "resource:action" form of a permission.
func PermKey(resource, action string) string {
	return resource + ":" + action
}

// Scope is the set of places a user holds one permission: everywhere
// (global), in some node groups, or nowhere (empty).
type Scope struct {
	global bool
	groups map[string]struct{}
}

// GlobalScope returns a scope covering everything.
func GlobalScope() Scope { return Scope{global: true} }

// GroupScope returns a scope covering exactly the given node groups.
func GroupScope(groupIDs ...string) Scope {
	s := Scope{}
	for _, id := range groupIDs {
		s.addGroup(id)
	}
	return s
}

func (s *Scope) addGroup(id string) {
	if s.groups == nil {
		s.groups = make(map[string]struct{})
	}
	s.groups[id] = struct{}{}
}

// IsGlobal reports whether the permission is held everywhere.
func (s Scope) IsGlobal() bool { return s.global }

// IsEmpty reports whether the permission is held nowhere.
func (s Scope) IsEmpty() bool { return !s.global && len(s.groups) == 0 }

// Allows reports whether the permission is held for the given node group.
func (s Scope) Allows(groupID string) bool {
	if s.global {
		return true
	}
	_, ok := s.groups[groupID]
	return ok
}

// AllowsAny reports whether the permission is held for at least one of the
// object's node groups. This is the read rule: an object is visible to a
// delegated administrator when it touches one of their groups. An object in
// no group is only visible with a global grant.
func (s Scope) AllowsAny(groupIDs []string) bool {
	if s.global {
		return true
	}
	for _, id := range groupIDs {
		if _, ok := s.groups[id]; ok {
			return true
		}
	}
	return false
}

// AllowsAll reports whether the permission is held for every one of the
// object's node groups. This is the write rule: a delegated administrator
// may only change an object whose effects stay inside their groups. An
// object in no group can only be changed with a global grant.
func (s Scope) AllowsAll(groupIDs []string) bool {
	if s.global {
		return true
	}
	if len(groupIDs) == 0 {
		return false
	}
	for _, id := range groupIDs {
		if _, ok := s.groups[id]; !ok {
			return false
		}
	}
	return true
}

// GroupIDs returns the node groups in the scope, sorted. It is empty for a
// global scope, which covers every group without listing them.
func (s Scope) GroupIDs() []string {
	ids := make([]string, 0, len(s.groups))
	for id := range s.groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Filter returns the query filter for listing objects within the scope: nil
// (no restriction) for a global scope, otherwise the scope's node groups.
func (s Scope) Filter() *models.GroupScopeFilter {
	if s.global {
		return nil
	}
	return &models.GroupScopeFilter{GroupIDs: s.GroupIDs()}
}

// Grants is a user's complete set of permissions, each with its scope.
type Grants struct {
	perms map[string]Scope
}

// NewGrants builds Grants from "resource:action" keys. Intended for tests and
// callers that already hold computed scopes.
func NewGrants(perms map[string]Scope) Grants {
	g := Grants{perms: make(map[string]Scope, len(perms))}
	for k, s := range perms {
		if !s.IsEmpty() {
			g.perms[k] = s
		}
	}
	return g
}

// Scope returns where the user holds resource:action (empty if nowhere).
func (g Grants) Scope(resource, action string) Scope {
	return g.perms[PermKey(resource, action)]
}

// Keys returns every "resource:action" the user holds anywhere, sorted.
func (g Grants) Keys() []string {
	keys := make([]string, 0, len(g.perms))
	for k := range g.perms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Summary returns every held permission with where it is held, for clients.
func (g Grants) Summary() map[string]models.PermissionScope {
	out := make(map[string]models.PermissionScope, len(g.perms))
	for k, s := range g.perms {
		out[k] = models.PermissionScope{Global: s.global, NodeGroupIDs: s.GroupIDs()}
	}
	return out
}

// BindingTarget is the scope a role binding is being created at.
type BindingTarget struct {
	ScopeType string // models.ScopeGlobal or models.ScopeNodeGroup
	ScopeID   string // node group ID for models.ScopeNodeGroup
}

// CanDelegate reports whether a caller holding these grants may create a role
// binding that grants perms at target. It is the privilege-escalation rule:
// nobody can hand out a permission, or a reach for it, that they do not hold
// themselves.
//
//   - Global target: the caller needs every permission globally.
//   - Node-group target: the caller needs every scopable permission for that
//     node group (globally or scoped to it). Non-scopable permissions are
//     skipped: a scoped binding never grants them, so they hand out nothing.
//
// On refusal the first missing permission key is returned for diagnostics.
func (g Grants) CanDelegate(perms []*models.Permission, target BindingTarget) (ok bool, missing string) {
	for _, p := range perms {
		key := PermKey(p.Resource, p.Action)
		held := g.perms[key]
		switch target.ScopeType {
		case models.ScopeGlobal:
			if !held.IsGlobal() {
				return false, key
			}
		case models.ScopeNodeGroup:
			if !IsScopable(p.Resource) {
				continue
			}
			if !held.Allows(target.ScopeID) {
				return false, key
			}
		default:
			return false, key
		}
	}
	return true, ""
}

// BindingSource yields the effective (role, scope) bindings of a user: direct
// bindings plus bindings inherited through user-group membership.
// Implemented by database.UserRoleBindingRepository.
type BindingSource interface {
	ListEffectiveBindings(ctx context.Context, userID string) ([]models.EffectiveBinding, error)
}

// PermissionSource yields the permissions attached to a role.
// Implemented by database.RoleRepository.
type PermissionSource interface {
	GetPermissionsByRoleID(ctx context.Context, roleID string) ([]*models.Permission, error)
}

// GrantOptions selects which kinds of role binding count.
type GrantOptions struct {
	// NodeGroupScopes makes node-group-scoped bindings grant their scopable
	// permissions. When false (the community default), such bindings grant
	// nothing and only global bindings count.
	NodeGroupScopes bool
}

// ComputeGrants resolves a user's effective bindings into Grants. Bindings
// with an unknown scope type, or a node-group scope without a group, grant
// nothing (fail closed); so do node-group bindings unless
// opts.NodeGroupScopes is set.
func ComputeGrants(ctx context.Context, bindings BindingSource, roles PermissionSource, userID string, opts GrantOptions) (Grants, error) {
	g := Grants{perms: make(map[string]Scope)}
	if userID == "" {
		return g, nil
	}

	effective, err := bindings.ListEffectiveBindings(ctx, userID)
	if err != nil {
		return Grants{}, fmt.Errorf("failed to fetch role bindings: %w", err)
	}

	rolePerms := make(map[string][]*models.Permission)
	for _, b := range effective {
		perms, cached := rolePerms[b.RoleID]
		if !cached {
			perms, err = roles.GetPermissionsByRoleID(ctx, b.RoleID)
			if err != nil {
				return Grants{}, fmt.Errorf("failed to fetch permissions for role %s: %w", b.RoleID, err)
			}
			rolePerms[b.RoleID] = perms
		}

		for _, p := range perms {
			key := PermKey(p.Resource, p.Action)
			s := g.perms[key]
			switch {
			case b.ScopeType == models.ScopeGlobal:
				s.global = true
			case opts.NodeGroupScopes && b.ScopeType == models.ScopeNodeGroup && b.ScopeID != nil && *b.ScopeID != "" && IsScopable(p.Resource):
				s.addGroup(*b.ScopeID)
			default:
				continue
			}
			g.perms[key] = s
		}
	}
	return g, nil
}

// Authorizer resolves what a user may do.
type Authorizer interface {
	// Grants returns all of the user's permissions with their scopes.
	Grants(ctx context.Context, userID string) (Grants, error)
	// HasPermission reports whether the user holds the permission anywhere
	// (globally or for at least one node group).
	HasPermission(ctx context.Context, userID, resource, action string) (bool, error)
}

// authorizer implements the Authorizer interface using the RBAC database tables
type authorizer struct {
	bindings        BindingSource
	roles           PermissionSource
	nodeGroupScopes func() bool
}

// New creates a new Authorizer. nodeGroupScopes reports, at request time,
// whether node-group-scoped bindings count (see GrantOptions); nil means
// they never do.
func New(bindingRepo *database.UserRoleBindingRepository, roleRepo *database.RoleRepository, nodeGroupScopes func() bool) Authorizer {
	return NewFromSources(bindingRepo, roleRepo, nodeGroupScopes)
}

// NewFromSources creates an Authorizer from the narrow source interfaces.
func NewFromSources(bindings BindingSource, roles PermissionSource, nodeGroupScopes func() bool) Authorizer {
	return &authorizer{bindings: bindings, roles: roles, nodeGroupScopes: nodeGroupScopes}
}

// Grants returns all of the user's permissions with their scopes.
func (a *authorizer) Grants(ctx context.Context, userID string) (Grants, error) {
	opts := GrantOptions{NodeGroupScopes: a.nodeGroupScopes != nil && a.nodeGroupScopes()}
	return ComputeGrants(ctx, a.bindings, a.roles, userID, opts)
}

// HasPermission reports whether the user holds the permission anywhere.
func (a *authorizer) HasPermission(ctx context.Context, userID, resource, action string) (bool, error) {
	g, err := a.Grants(ctx, userID)
	if err != nil {
		return false, err
	}
	return !g.Scope(resource, action).IsEmpty(), nil
}
