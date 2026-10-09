// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package edition

import "testing"

type fixed struct{ on map[Feature]bool }

func (fixed) Name() string             { return "test" }
func (f fixed) Enabled(x Feature) bool { return f.on[x] }

func TestCommunityEnablesNothing(t *testing.T) {
	c := Community{}
	if c.Name() != "community" {
		t.Errorf("Name() = %q, want community", c.Name())
	}
	for _, f := range AllFeatures {
		if c.Enabled(f) {
			t.Errorf("community edition enables %s", f)
		}
	}
	if got := EnabledFeatures(c); len(got) != 0 {
		t.Errorf("EnabledFeatures(Community) = %v, want none", got)
	}
}

func TestEnabledFeatures(t *testing.T) {
	got := EnabledFeatures(fixed{on: map[Feature]bool{NodeGroupScopedRBAC: true, "unknown": true}})
	if len(got) != 1 || got[0] != NodeGroupScopedRBAC {
		t.Errorf("EnabledFeatures = %v, want only known enabled features", got)
	}
}
