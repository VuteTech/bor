// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestComplianceOrderBy_AllowlistAndInjection(t *testing.T) {
	tests := []struct {
		field, order string
		want         string
	}{
		{"node", "asc", "ORDER BY n.name ASC"},
		{"policy", "desc", "ORDER BY p.name DESC"},
		{"status", "asc", "ORDER BY cr.status ASC"},
		{"reported", "", "ORDER BY cr.reported_at DESC"},
		// Unknown / malicious fields fall back to the default column.
		{"", "asc", "ORDER BY cr.reported_at ASC"},
		{"cr.status; DROP TABLE compliance_results;--", "asc", "ORDER BY cr.reported_at ASC"},
		{"node", "sideways", "ORDER BY n.name DESC"},
	}
	for _, tt := range tests {
		t.Run(tt.field+"/"+tt.order, func(t *testing.T) {
			got := complianceOrderBy(tt.field, tt.order)
			if got != tt.want {
				t.Errorf("complianceOrderBy(%q,%q) = %q, want %q", tt.field, tt.order, got, tt.want)
			}
			if strings.Contains(got, "DROP") {
				t.Errorf("complianceOrderBy leaked raw input: %q", got)
			}
		})
	}
}

func TestBuildComplianceFilter(t *testing.T) {
	tests := []struct {
		name          string
		search        string
		status        string
		includeStatus bool
		scope         *models.GroupScopeFilter
		wantSQL       string
		wantArgs      int
	}{
		{"empty", "", "", true, nil, "", 0},
		{"search only", "web", "", true, nil, " AND (n.name ILIKE $1 OR p.name ILIKE $1)", 1},
		{"status only", "", "non_compliant", true, nil, " AND cr.status = $1", 1},
		{"search + status", "web", "error", true, nil,
			" AND (n.name ILIKE $1 OR p.name ILIKE $1) AND cr.status = $2", 2},
		{"status excluded for overview", "web", "error", false, nil,
			" AND (n.name ILIKE $1 OR p.name ILIKE $1)", 1},
		{"blank search ignored", "  ", "", true, nil, "", 0},
		{"empty scope matches nothing", "", "", true, &models.GroupScopeFilter{}, " AND FALSE", 0},
		{"scope after search + status", "web", "error", true,
			&models.GroupScopeFilter{GroupIDs: []string{"g1", "g2"}},
			" AND (n.name ILIKE $1 OR p.name ILIKE $1) AND cr.status = $2" +
				" AND EXISTS (SELECT 1 FROM node_group_members sgm WHERE sgm.node_id = cr.node_id AND sgm.node_group_id = ANY($3::uuid[]))", 3},
		{"scope with status excluded", "", "error", false,
			&models.GroupScopeFilter{GroupIDs: []string{"g1"}},
			" AND EXISTS (SELECT 1 FROM node_group_members sgm WHERE sgm.node_id = cr.node_id AND sgm.node_group_id = ANY($1::uuid[]))", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := buildComplianceFilter(tt.search, tt.status, tt.includeStatus, tt.scope)
			if sql != tt.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tt.wantSQL)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args len = %d, want %d", len(args), tt.wantArgs)
			}
		})
	}
}
