// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// fakeNodeGroups is an in-memory nodeGroupGetter.
type fakeNodeGroups struct {
	groups map[string]*models.NodeGroup
	err    error
}

func (f *fakeNodeGroups) GetByID(_ context.Context, id string) (*models.NodeGroup, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.groups[id], nil
}

func TestValidateBindingScope(t *testing.T) {
	const berlin = "236c1a2c-42f3-47ad-b10b-3a10b73c7f68"
	const missing = "11111111-2222-3333-4444-555555555555"
	groups := &fakeNodeGroups{groups: map[string]*models.NodeGroup{berlin: {ID: berlin, Name: "Berlin"}}}
	str := func(s string) *string { return &s }

	tests := []struct {
		name       string
		groups     nodeGroupGetter
		scopeType  string
		scopeID    *string
		wantType   string
		wantID     string
		wantClient bool // error is a client (400) error
		wantErr    bool
	}{
		{name: "explicit global", groups: groups, scopeType: "global", wantType: models.ScopeGlobal},
		{name: "empty defaults to global", groups: groups, scopeType: "", wantType: models.ScopeGlobal},
		{name: "empty scope_id with global is fine", groups: groups, scopeType: "global", scopeID: str(""), wantType: models.ScopeGlobal},
		{name: "node group scope", groups: groups, scopeType: "node_group", scopeID: str(berlin), wantType: models.ScopeNodeGroup, wantID: berlin},
		{name: "global with scope_id rejected", groups: groups, scopeType: "global", scopeID: str(berlin), wantErr: true, wantClient: true},
		{name: "node group without scope_id", groups: groups, scopeType: "node_group", wantErr: true, wantClient: true},
		{name: "node group with malformed id", groups: groups, scopeType: "node_group", scopeID: str("berlin'; --"), wantErr: true, wantClient: true},
		{name: "unknown node group", groups: groups, scopeType: "node_group", scopeID: str(missing), wantErr: true, wantClient: true},
		{name: "organization scope is not a thing", groups: groups, scopeType: "organization", scopeID: str(berlin), wantErr: true, wantClient: true},
		{name: "legacy group scope rejected", groups: groups, scopeType: "group", scopeID: str(berlin), wantErr: true, wantClient: true},
		{name: "no lookup configured", groups: nil, scopeType: "node_group", scopeID: str(berlin), wantErr: true, wantClient: true},
		{name: "lookup failure is a server error", groups: &fakeNodeGroups{err: errors.New("db down")}, scopeType: "node_group", scopeID: str(berlin), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotID, err := validateBindingScope(context.Background(), tt.groups, tt.scopeType, tt.scopeID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got scope %q %v", gotType, gotID)
				}
				if got := errors.Is(err, errInvalidScope); got != tt.wantClient {
					t.Errorf("client error = %v, want %v (err: %v)", got, tt.wantClient, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotType != tt.wantType || derefString(gotID) != tt.wantID {
				t.Errorf("got (%q, %q), want (%q, %q)", gotType, derefString(gotID), tt.wantType, tt.wantID)
			}
		})
	}
}

func TestScopeGroups_FollowsEditionSwitch(t *testing.T) {
	const berlin = "236c1a2c-42f3-47ad-b10b-3a10b73c7f68"
	groups := &fakeNodeGroups{groups: map[string]*models.NodeGroup{berlin: {ID: berlin}}}
	on := false
	enabled := func() bool { return on }

	_, _, err := validateBindingScope(context.Background(), scopeGroups(groups, enabled), models.ScopeNodeGroup, &[]string{berlin}[0])
	if !errors.Is(err, errInvalidScope) {
		t.Fatalf("with the feature off, a node-group scope must be a client error, got %v", err)
	}

	on = true
	typ, id, err := validateBindingScope(context.Background(), scopeGroups(groups, enabled), models.ScopeNodeGroup, &[]string{berlin}[0])
	if err != nil || typ != models.ScopeNodeGroup || derefString(id) != berlin {
		t.Fatalf("with the feature on: (%q, %q, %v), want a valid node-group scope", typ, derefString(id), err)
	}

	if scopeGroups(groups, nil) != nil || scopeGroups(nil, enabled) != nil {
		t.Error("missing lookup or switch must disable node-group scopes")
	}
}
