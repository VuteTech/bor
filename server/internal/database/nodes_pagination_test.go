// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// TestNodeOrderBy_AllowlistAndInjection verifies that only allowlisted sort
// fields reach the ORDER BY clause and that injection attempts fall back to the
// safe default (ORDER BY cannot be parameterized).
func TestNodeOrderBy_AllowlistAndInjection(t *testing.T) {
	tests := []struct {
		field, order string
		want         string
	}{
		{"name", "asc", "ORDER BY n.name ASC NULLS LAST"},
		{"name", "desc", "ORDER BY n.name DESC NULLS LAST"},
		{"status", "asc", "ORDER BY n.status_cached ASC NULLS LAST"},
		{"last_seen", "", "ORDER BY n.last_seen DESC NULLS LAST"},
		// Unknown / malicious fields fall back to the default column.
		{"", "asc", "ORDER BY n.last_seen ASC NULLS LAST"},
		{"n.name; DROP TABLE nodes;--", "asc", "ORDER BY n.last_seen ASC NULLS LAST"},
		{"(SELECT 1)", "desc", "ORDER BY n.last_seen DESC NULLS LAST"},
		// Unknown direction falls back to DESC.
		{"name", "sideways", "ORDER BY n.name DESC NULLS LAST"},
	}
	for _, tt := range tests {
		t.Run(tt.field+"/"+tt.order, func(t *testing.T) {
			got := nodeOrderBy(tt.field, tt.order)
			if got != tt.want {
				t.Errorf("nodeOrderBy(%q,%q) = %q, want %q", tt.field, tt.order, got, tt.want)
			}
			// No user input must survive into the clause.
			if strings.Contains(got, "DROP") || strings.Contains(got, "SELECT") {
				t.Errorf("nodeOrderBy leaked raw input: %q", got)
			}
		})
	}
}

func TestBuildNodeFilter(t *testing.T) {
	tests := []struct {
		name      string
		req       *models.NodeListRequest
		wantWhere string
		wantArgs  int
	}{
		{"no filters", &models.NodeListRequest{}, "", 0},
		{"status only", &models.NodeListRequest{Status: "online"}, "WHERE n.status_cached = $1", 1},
		{"search only", &models.NodeListRequest{Search: "web"},
			"WHERE (n.name ILIKE $1 OR n.fqdn ILIKE $1 OR n.ip_address ILIKE $1 OR n.groups ILIKE $1)", 1},
		{"status + search", &models.NodeListRequest{Status: "online", Search: "web"},
			"WHERE n.status_cached = $1 AND (n.name ILIKE $2 OR n.fqdn ILIKE $2 OR n.ip_address ILIKE $2 OR n.groups ILIKE $2)", 2},
		{"blank search ignored", &models.NodeListRequest{Search: "   "}, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildNodeFilter(tt.req)
			if where != tt.wantWhere {
				t.Errorf("where = %q, want %q", where, tt.wantWhere)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args len = %d, want %d", len(args), tt.wantArgs)
			}
		})
	}
}
