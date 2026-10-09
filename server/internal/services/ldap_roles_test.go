// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"reflect"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestParseLDAPRoleTarget(t *testing.T) {
	tests := []struct {
		value, wantRole, wantGroup string
	}{
		{"Super Admin", "Super Admin", ""},
		{"Org Admin@Berlin Office", "Org Admin", "Berlin Office"},
		{" Org Admin @ Berlin Office ", "Org Admin", "Berlin Office"},
		// Only the first @ separates; group names may contain @.
		{"Policy Editor@lab@floor2", "Policy Editor", "lab@floor2"},
		{"Auditor@", "Auditor", ""},
	}
	for _, tt := range tests {
		role, group := ParseLDAPRoleTarget(tt.value)
		if role != tt.wantRole || group != tt.wantGroup {
			t.Errorf("ParseLDAPRoleTarget(%q) = (%q, %q), want (%q, %q)", tt.value, role, group, tt.wantRole, tt.wantGroup)
		}
	}
}

func TestPlanLDAPRoleSync(t *testing.T) {
	adminGlobal := ldapBindingKey{roleID: "admin", scopeType: models.ScopeGlobal}
	editorBerlin := ldapBindingKey{roleID: "editor", scopeType: models.ScopeNodeGroup, scopeID: "berlin"}
	editorParis := ldapBindingKey{roleID: "editor", scopeType: models.ScopeNodeGroup, scopeID: "paris"}
	// An administrator-made binding of a managed role at another scope.
	editorGlobalManual := ldapBindingKey{roleID: "editor", scopeType: models.ScopeGlobal}

	managed := map[ldapBindingKey]bool{adminGlobal: true, editorBerlin: true, editorParis: true}
	wanted := map[ldapBindingKey]bool{editorBerlin: true}
	current := map[ldapBindingKey]string{
		adminGlobal:        "b-admin",  // left the admin LDAP group
		editorParis:        "b-paris",  // left the Paris group
		editorGlobalManual: "b-manual", // not produced by the map: untouched
	}

	create, remove := planLDAPRoleSync(wanted, managed, current)

	if !reflect.DeepEqual(create, []ldapBindingKey{editorBerlin}) {
		t.Errorf("create = %+v, want [editor@berlin]", create)
	}
	if !reflect.DeepEqual(remove, []string{"b-admin", "b-paris"}) {
		t.Errorf("remove = %v, want [b-admin b-paris]", remove)
	}
}

func TestPlanLDAPRoleSync_NoChangesWhenInSync(t *testing.T) {
	k := ldapBindingKey{roleID: "admin", scopeType: models.ScopeGlobal}
	create, remove := planLDAPRoleSync(
		map[ldapBindingKey]bool{k: true},
		map[ldapBindingKey]bool{k: true},
		map[ldapBindingKey]string{k: "b1"},
	)
	if len(create) != 0 || len(remove) != 0 {
		t.Errorf("in-sync plan = create %v, remove %v; want nothing", create, remove)
	}
}

func TestDropUncreatableScopedKeys(t *testing.T) {
	global := ldapBindingKey{roleID: "admin", scopeType: models.ScopeGlobal}
	newScoped := ldapBindingKey{roleID: "editor", scopeType: models.ScopeNodeGroup, scopeID: "berlin"}
	keptScoped := ldapBindingKey{roleID: "editor", scopeType: models.ScopeNodeGroup, scopeID: "paris"}
	leftScoped := ldapBindingKey{roleID: "viewer", scopeType: models.ScopeNodeGroup, scopeID: "rome"}

	managed := map[ldapBindingKey]bool{global: true, newScoped: true, keptScoped: true, leftScoped: true}
	wanted := map[ldapBindingKey]bool{global: true, newScoped: true, keptScoped: true}
	current := map[ldapBindingKey]string{keptScoped: "b-paris", leftScoped: "b-rome"}

	dropUncreatableScopedKeys(wanted, current)
	create, remove := planLDAPRoleSync(wanted, managed, current)

	if !reflect.DeepEqual(create, []ldapBindingKey{global}) {
		t.Errorf("create = %+v, want only the global binding (no new scoped bindings while off)", create)
	}
	if !reflect.DeepEqual(remove, []string{"b-rome"}) {
		t.Errorf("remove = %v, want the scoped binding whose LDAP group the user left", remove)
	}
}
