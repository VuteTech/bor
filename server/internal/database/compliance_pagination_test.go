// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"strings"
	"testing"
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
		wantSQL       string
		wantArgs      int
	}{
		{"empty", "", "", true, "", 0},
		{"search only", "web", "", true, " AND (n.name ILIKE $1 OR p.name ILIKE $1)", 1},
		{"status only", "", "non_compliant", true, " AND cr.status = $1", 1},
		{"search + status", "web", "error", true,
			" AND (n.name ILIKE $1 OR p.name ILIKE $1) AND cr.status = $2", 2},
		{"status excluded for overview", "web", "error", false,
			" AND (n.name ILIKE $1 OR p.name ILIKE $1)", 1},
		{"blank search ignored", "  ", "", true, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := buildComplianceFilter(tt.search, tt.status, tt.includeStatus)
			if sql != tt.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tt.wantSQL)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args len = %d, want %d", len(args), tt.wantArgs)
			}
		})
	}
}
