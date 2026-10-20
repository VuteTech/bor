// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"testing"

	"github.com/VuteTech/Bor/server/pkg/macrules"
)

func TestCuratedMacData(t *testing.T) {
	booleans := CuratedSelinuxBooleans()
	if len(booleans) < 40 {
		t.Fatalf("expected the curated boolean list, got %d entries", len(booleans))
	}
	seen := map[string]bool{}
	for _, b := range booleans {
		if err := macrules.ValidateBooleanName(b.Name); err != nil {
			t.Errorf("curated boolean %q: %v", b.Name, err)
		}
		if b.Description == "" || b.Group == "" {
			t.Errorf("curated boolean %q lacks a description or group", b.Name)
		}
		if seen[b.Name] {
			t.Errorf("curated boolean %q listed twice", b.Name)
		}
		seen[b.Name] = true
		if macrules.IsProtectedBoolean(b.Name) {
			t.Errorf("curated boolean %q is protected", b.Name)
		}
	}
	if CuratedSelinuxBoolean("deny_ptrace") == nil || CuratedSelinuxBoolean("nope") != nil {
		t.Error("boolean lookup wrong")
	}
	profiles := CuratedApparmorProfiles()
	if len(profiles) < 20 {
		t.Fatalf("expected the curated profile list, got %d entries", len(profiles))
	}
	for _, p := range profiles {
		if err := macrules.ValidateProfileFile(p.File); err != nil {
			t.Errorf("curated profile %q: %v", p.File, err)
		}
		if p.Protected != macrules.IsProtectedProfile(p.File) {
			t.Errorf("curated profile %q: protected flag disagrees with macrules", p.File)
		}
	}
	if CuratedApparmorProfile("usr.sbin.cupsd") == nil {
		t.Error("profile lookup wrong")
	}
}
