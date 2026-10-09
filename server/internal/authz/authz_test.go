// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package authz

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// mockBindingSource implements BindingSource with fixed effective bindings
// per user (direct and group-inherited alike).
type mockBindingSource struct {
	bindings map[string][]models.EffectiveBinding
	err      error
}

func (m *mockBindingSource) ListEffectiveBindings(_ context.Context, userID string) ([]models.EffectiveBinding, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.bindings[userID], nil
}

// mockPermissionSource implements PermissionSource.
type mockPermissionSource struct {
	permissions map[string][]*models.Permission
	err         error
	calls       int
}

func (m *mockPermissionSource) GetPermissionsByRoleID(_ context.Context, roleID string) ([]*models.Permission, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.permissions[roleID], nil
}

// scopesOn enables node-group-scoped bindings, as a licensed edition would.
var scopesOn = GrantOptions{NodeGroupScopes: true}

func perm(resource, action string) *models.Permission {
	return &models.Permission{Resource: resource, Action: action}
}

func global(roleID string) models.EffectiveBinding {
	return models.EffectiveBinding{RoleID: roleID, ScopeType: models.ScopeGlobal}
}

func scoped(roleID, groupID string) models.EffectiveBinding {
	return models.EffectiveBinding{RoleID: roleID, ScopeType: models.ScopeNodeGroup, ScopeID: &groupID}
}

// orgAdmin mixes scopable and global-only permissions, like the seeded
// Org Admin role.
var testRoles = map[string][]*models.Permission{
	"org-admin": {perm("policy", "edit"), perm("node", "view"), perm("user", "manage"), perm("audit_log", "view")},
	"viewer":    {perm("policy", "view"), perm("node", "view")},
}

func grantsFor(t *testing.T, bindings ...models.EffectiveBinding) Grants {
	t.Helper()
	g, err := ComputeGrants(context.Background(),
		&mockBindingSource{bindings: map[string][]models.EffectiveBinding{"u1": bindings}},
		&mockPermissionSource{permissions: testRoles}, "u1", scopesOn)
	if err != nil {
		t.Fatalf("ComputeGrants() error = %v", err)
	}
	return g
}

func TestComputeGrants_GlobalBindingGrantsEverything(t *testing.T) {
	g := grantsFor(t, global("org-admin"))

	for _, key := range [][2]string{{"policy", "edit"}, {"node", "view"}, {"user", "manage"}, {"audit_log", "view"}} {
		if !g.Scope(key[0], key[1]).IsGlobal() {
			t.Errorf("%s:%s should be global", key[0], key[1])
		}
	}
}

func TestComputeGrants_ScopedBindingGrantsOnlyScopableResources(t *testing.T) {
	g := grantsFor(t, scoped("org-admin", "berlin"))

	edit := g.Scope("policy", "edit")
	if edit.IsGlobal() || !edit.Allows("berlin") || edit.Allows("paris") {
		t.Errorf("policy:edit scope = %+v, want only berlin", edit)
	}
	// A scoped binding must never grant a global-only resource, whatever
	// the role carries.
	if !g.Scope("user", "manage").IsEmpty() {
		t.Error("user:manage must not be granted by a node-group binding")
	}
	if !g.Scope("audit_log", "view").IsEmpty() {
		t.Error("audit_log:view must not be granted by a node-group binding")
	}
}

func TestComputeGrants_MergesScopesAcrossBindings(t *testing.T) {
	g := grantsFor(t, scoped("viewer", "berlin"), scoped("org-admin", "paris"))

	got := g.Scope("node", "view").GroupIDs()
	if want := []string{"berlin", "paris"}; !reflect.DeepEqual(got, want) {
		t.Errorf("node:view groups = %v, want %v", got, want)
	}
	if got := g.Scope("policy", "edit").GroupIDs(); !reflect.DeepEqual(got, []string{"paris"}) {
		t.Errorf("policy:edit groups = %v, want [paris]", got)
	}
}

func TestComputeGrants_GlobalWinsOverScoped(t *testing.T) {
	g := grantsFor(t, scoped("viewer", "berlin"), global("viewer"))

	if !g.Scope("node", "view").IsGlobal() {
		t.Error("a global binding should make the permission global")
	}
}

func TestComputeGrants_MalformedBindingsGrantNothing(t *testing.T) {
	empty := ""
	g := grantsFor(t,
		models.EffectiveBinding{RoleID: "org-admin", ScopeType: "organization"},
		models.EffectiveBinding{RoleID: "org-admin", ScopeType: models.ScopeNodeGroup},
		models.EffectiveBinding{RoleID: "org-admin", ScopeType: models.ScopeNodeGroup, ScopeID: &empty},
	)

	if keys := g.Keys(); len(keys) != 0 {
		t.Errorf("malformed bindings granted %v, want nothing", keys)
	}
}

func TestComputeGrants_CachesRolePermissions(t *testing.T) {
	perms := &mockPermissionSource{permissions: testRoles}
	_, err := ComputeGrants(context.Background(),
		&mockBindingSource{bindings: map[string][]models.EffectiveBinding{
			"u1": {scoped("viewer", "a"), scoped("viewer", "b"), global("viewer")},
		}}, perms, "u1", scopesOn)
	if err != nil {
		t.Fatalf("ComputeGrants() error = %v", err)
	}
	if perms.calls != 1 {
		t.Errorf("role permissions fetched %d times, want 1", perms.calls)
	}
}

func TestComputeGrants_Errors(t *testing.T) {
	if _, err := ComputeGrants(context.Background(),
		&mockBindingSource{err: errors.New("db down")}, &mockPermissionSource{}, "u1", scopesOn); err == nil {
		t.Error("binding source error should propagate")
	}
	if _, err := ComputeGrants(context.Background(),
		&mockBindingSource{bindings: map[string][]models.EffectiveBinding{"u1": {global("viewer")}}},
		&mockPermissionSource{err: errors.New("db down")}, "u1", scopesOn); err == nil {
		t.Error("permission source error should propagate")
	}
}

func TestScope_ReadAndWriteRules(t *testing.T) {
	berlin := GroupScope("berlin")
	tests := []struct {
		name      string
		scope     Scope
		groups    []string
		wantRead  bool
		wantWrite bool
	}{
		{"global, any object", GlobalScope(), []string{"x"}, true, true},
		{"global, object in no group", GlobalScope(), nil, true, true},
		{"scoped, object in scope", berlin, []string{"berlin"}, true, true},
		{"scoped, object shared with another group", berlin, []string{"berlin", "paris"}, true, false},
		{"scoped, object elsewhere", berlin, []string{"paris"}, false, false},
		{"scoped, object in no group", berlin, nil, false, false},
		{"empty scope", Scope{}, []string{"berlin"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.AllowsAny(tt.groups); got != tt.wantRead {
				t.Errorf("AllowsAny = %v, want %v", got, tt.wantRead)
			}
			if got := tt.scope.AllowsAll(tt.groups); got != tt.wantWrite {
				t.Errorf("AllowsAll = %v, want %v", got, tt.wantWrite)
			}
		})
	}
}

func TestScope_Filter(t *testing.T) {
	if f := GlobalScope().Filter(); f != nil {
		t.Errorf("global Filter() = %+v, want nil", f)
	}
	f := GroupScope("b", "a").Filter()
	if f == nil || !reflect.DeepEqual(f.GroupIDs, []string{"a", "b"}) {
		t.Errorf("scoped Filter() = %+v, want sorted [a b]", f)
	}
	// An empty scope filters to nothing rather than to everything.
	if f := (Scope{}).Filter(); f == nil || len(f.GroupIDs) != 0 {
		t.Errorf("empty Filter() = %+v, want empty non-nil filter", f)
	}
}

func TestCanDelegate(t *testing.T) {
	viewer := testRoles["viewer"]
	orgAdmin := testRoles["org-admin"]
	globalTarget := BindingTarget{ScopeType: models.ScopeGlobal}
	berlinTarget := BindingTarget{ScopeType: models.ScopeNodeGroup, ScopeID: "berlin"}
	parisTarget := BindingTarget{ScopeType: models.ScopeNodeGroup, ScopeID: "paris"}

	globalAdmin := grantsFor(t, global("org-admin"), global("viewer"))
	berlinAdmin := grantsFor(t, scoped("org-admin", "berlin"), scoped("viewer", "berlin"))

	tests := []struct {
		name   string
		grants Grants
		perms  []*models.Permission
		target BindingTarget
		want   bool
	}{
		{"global holder grants globally", globalAdmin, viewer, globalTarget, true},
		{"global holder grants in any group", globalAdmin, viewer, parisTarget, true},
		{"scoped holder grants in own group", berlinAdmin, viewer, berlinTarget, true},
		{"scoped holder cannot grant globally", berlinAdmin, viewer, globalTarget, false},
		{"scoped holder cannot grant in another group", berlinAdmin, viewer, parisTarget, false},
		// Org Admin carries user:manage, which a scoped binding never grants,
		// so it does not block a node-group-scoped delegation.
		{"global-only perms skipped for scoped target", berlinAdmin, orgAdmin, berlinTarget, true},
		// But granting Org Admin globally needs user:manage globally.
		{"global-only perms required for global target", berlinAdmin, orgAdmin, globalTarget, false},
		{"unknown target scope refused", globalAdmin, viewer, BindingTarget{ScopeType: "organization"}, false},
		{"nothing held", Grants{}, viewer, berlinTarget, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, missing := tt.grants.CanDelegate(tt.perms, tt.target)
			if got != tt.want {
				t.Errorf("CanDelegate() = %v (missing %q), want %v", got, missing, tt.want)
			}
			if !got && missing == "" {
				t.Error("refusal should name the missing permission")
			}
		})
	}
}

func TestAuthorizer_HasPermissionInAnyScope(t *testing.T) {
	az := NewFromSources(
		&mockBindingSource{bindings: map[string][]models.EffectiveBinding{"u1": {scoped("viewer", "berlin")}}},
		&mockPermissionSource{permissions: testRoles},
		func() bool { return true },
	)

	ok, err := az.HasPermission(context.Background(), "u1", "node", "view")
	if err != nil || !ok {
		t.Errorf("HasPermission(node:view) = %v, %v; want true", ok, err)
	}
	ok, err = az.HasPermission(context.Background(), "u1", "policy", "edit")
	if err != nil || ok {
		t.Errorf("HasPermission(policy:edit) = %v, %v; want false", ok, err)
	}
}

func TestIsScopable(t *testing.T) {
	for _, r := range []string{"node", "node_group", "policy", "binding", "compliance"} {
		if !IsScopable(r) {
			t.Errorf("%s should be scopable", r)
		}
	}
	for _, r := range []string{"user", "role", "user_group", "audit_log", "settings", "disk_encryption", "tang_server", "flatpak_repo"} {
		if IsScopable(r) {
			t.Errorf("%s must stay global-only", r)
		}
	}
}

func TestComputeGrants_ScopedBindingsOffByDefault(t *testing.T) {
	berlin := "berlin"
	g, err := ComputeGrants(context.Background(),
		&mockBindingSource{bindings: map[string][]models.EffectiveBinding{"u1": {
			{RoleID: "org-admin", ScopeType: models.ScopeNodeGroup, ScopeID: &berlin},
			{RoleID: "viewer", ScopeType: models.ScopeGlobal},
		}}},
		&mockPermissionSource{permissions: testRoles}, "u1", GrantOptions{})
	if err != nil {
		t.Fatalf("ComputeGrants() error = %v", err)
	}
	if !g.Scope("policy", "edit").IsEmpty() {
		t.Error("a node-group binding must grant nothing when node-group scopes are off")
	}
	if !g.Scope("policy", "view").IsGlobal() {
		t.Error("global bindings must keep working when node-group scopes are off")
	}
}

func TestAuthorizer_ScopeSwitchIsReadPerRequest(t *testing.T) {
	berlin := "berlin"
	on := false
	az := NewFromSources(
		&mockBindingSource{bindings: map[string][]models.EffectiveBinding{"u1": {
			{RoleID: "viewer", ScopeType: models.ScopeNodeGroup, ScopeID: &berlin},
		}}},
		&mockPermissionSource{permissions: testRoles},
		func() bool { return on },
	)

	if ok, _ := az.HasPermission(context.Background(), "u1", "node", "view"); ok {
		t.Error("scoped binding granted access while the feature was off")
	}
	on = true // e.g. a license was installed
	if ok, _ := az.HasPermission(context.Background(), "u1", "node", "view"); !ok {
		t.Error("scoped binding should grant access once the feature is on")
	}
}
