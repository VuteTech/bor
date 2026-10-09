// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"testing"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

func ownedBy(userID string) *models.Policy {
	return &models.Policy{ID: "p", CreatedByUserID: &userID}
}

func TestPolicyAccessRules(t *testing.T) {
	berlin := authz.GroupScope("berlin")
	tests := []struct {
		name      string
		scope     authz.Scope
		policy    *models.Policy
		groups    []string
		userID    string
		wantView  bool
		wantWrite bool
	}{
		{"global sees and writes everything", authz.GlobalScope(), ownedBy("other"), []string{"paris"}, "me", true, true},
		{"global writes an unbound draft of someone else", authz.GlobalScope(), ownedBy("other"), nil, "me", true, true},
		{"bound only to my group", berlin, ownedBy("other"), []string{"berlin"}, "me", true, true},
		{"shared with another group: read only", berlin, ownedBy("other"), []string{"berlin", "paris"}, "me", true, false},
		{"bound elsewhere only", berlin, ownedBy("other"), []string{"paris"}, "me", false, false},
		{"my unbound draft", berlin, ownedBy("me"), nil, "me", true, true},
		{"someone else's unbound draft", berlin, ownedBy("other"), nil, "me", false, false},
		// Ownership never extends write reach beyond the owner's groups.
		{"my draft bound elsewhere by an admin: read only", berlin, ownedBy("me"), []string{"paris"}, "me", true, false},
		{"policy without an owner, unbound", berlin, &models.Policy{ID: "p"}, nil, "me", false, false},
		{"no grant at all", authz.Scope{}, ownedBy("me"), nil, "me", false, false},
		{"anonymous caller owns nothing", berlin, &models.Policy{ID: "p", CreatedByUserID: new(string)}, nil, "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canViewPolicy(tt.scope, tt.policy, tt.groups, tt.userID); got != tt.wantView {
				t.Errorf("canViewPolicy = %v, want %v", got, tt.wantView)
			}
			if got := canWritePolicy(tt.scope, tt.policy, tt.groups, tt.userID); got != tt.wantWrite {
				t.Errorf("canWritePolicy = %v, want %v", got, tt.wantWrite)
			}
		})
	}
}

func TestFilterVisiblePolicies_ScopedCountsOnlyInScopeBindings(t *testing.T) {
	shared := &models.Policy{ID: "shared", BindingsCount: 3, EnabledBindingsCount: 2}
	elsewhere := &models.Policy{ID: "elsewhere", BindingsCount: 1, EnabledBindingsCount: 1}
	mine := ownedBy("me")
	mine.ID = "mine"

	idx := &policyGroupIndex{
		groups: map[string][]string{
			"shared":    {"berlin", "paris", "rome"},
			"elsewhere": {"paris"},
		},
		refs: []database.PolicyGroupRef{
			{PolicyID: "shared", GroupID: "berlin", Enabled: true},
			{PolicyID: "shared", GroupID: "paris", Enabled: true},
			{PolicyID: "shared", GroupID: "rome", Enabled: false},
			{PolicyID: "elsewhere", GroupID: "paris", Enabled: true},
		},
	}

	got := filterVisiblePolicies([]*models.Policy{shared, elsewhere, mine}, idx, authz.GroupScope("berlin"), "me")

	if len(got) != 2 || got[0].ID != "shared" || got[1].ID != "mine" {
		t.Fatalf("visible = %v, want [shared mine]", policyIDs(got))
	}
	if shared.BindingsCount != 1 || shared.EnabledBindingsCount != 1 {
		t.Errorf("shared counts = %d/%d, want 1/1 (in-scope bindings only)",
			shared.BindingsCount, shared.EnabledBindingsCount)
	}
}

func TestFilterVisiblePolicies_GlobalUnchanged(t *testing.T) {
	p := &models.Policy{ID: "p", BindingsCount: 5}
	got := filterVisiblePolicies([]*models.Policy{p}, &policyGroupIndex{}, authz.GlobalScope(), "me")
	if len(got) != 1 || p.BindingsCount != 5 {
		t.Errorf("global filter changed the list or counts: %v, count %d", policyIDs(got), p.BindingsCount)
	}
}

func policyIDs(ps []*models.Policy) []string {
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	return ids
}
