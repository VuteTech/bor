// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

func TestScopeAllows(t *testing.T) {
	fleet := &database.ReportRecord{ScopeAll: true}
	lab := &database.ReportRecord{ScopeGroupIDs: []string{"g1"}}
	mixed := &database.ReportRecord{ScopeGroupIDs: []string{"g1", "g2"}}
	empty := &database.ReportRecord{}
	g1 := &models.GroupScopeFilter{GroupIDs: []string{"g1"}}
	g12 := &models.GroupScopeFilter{GroupIDs: []string{"g1", "g2"}}
	none := &models.GroupScopeFilter{}

	for _, tc := range []struct {
		name  string
		scope *models.GroupScopeFilter
		rec   *database.ReportRecord
		want  bool
	}{
		{"global sees fleet", nil, fleet, true},
		{"global sees lab", nil, lab, true},
		{"g1 does not see fleet", g1, fleet, false},
		{"g1 sees lab", g1, lab, true},
		{"g1 does not see mixed", g1, mixed, false},
		{"g1+g2 sees mixed", g12, mixed, true},
		{"g1 does not see an empty scope", g1, empty, false},
		{"no groups sees nothing", none, lab, false},
	} {
		if got := ScopeAllows(tc.scope, tc.rec); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestReportFilename(t *testing.T) {
	at := time.Date(2026, 10, 20, 14, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		label, id, want string
	}{
		{"Q4 Lab Audit", "9f1c2d3e-0000", "compliance-report-q4-lab-audit-20261020-1405"},
		{"", "9f1c2d3e-0000-4", "compliance-report-9f1c2d3e-20261020-1405"},
		{"../../etc/passwd", "x", "compliance-report-etc-passwd-20261020-1405"},
	} {
		got := reportFilename(&database.ReportRecord{ID: tc.id, Label: tc.label, GeneratedAt: at})
		if got != tc.want {
			t.Errorf("%q: got %q want %q", tc.label, got, tc.want)
		}
	}
}

func TestUniqueSorted(t *testing.T) {
	got := uniqueSorted([]string{" b ", "a", "b", "", "a"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}
