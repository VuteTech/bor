// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"strings"
	"testing"
)

func TestAuditLogOrderBy_AllowlistAndInjection(t *testing.T) {
	tests := []struct {
		field, order string
		want         string
	}{
		{"created_at", "desc", "ORDER BY created_at DESC"},
		{"username", "asc", "ORDER BY username ASC"},
		{"action", "asc", "ORDER BY action ASC"},
		{"resource_type", "desc", "ORDER BY resource_type DESC"},
		{"", "asc", "ORDER BY created_at ASC"},
		{"created_at; DROP TABLE audit_logs;--", "asc", "ORDER BY created_at ASC"},
		{"username", "sideways", "ORDER BY username DESC"},
	}
	for _, tt := range tests {
		t.Run(tt.field+"/"+tt.order, func(t *testing.T) {
			got := auditLogOrderBy(tt.field, tt.order)
			if got != tt.want {
				t.Errorf("auditLogOrderBy(%q,%q) = %q, want %q", tt.field, tt.order, got, tt.want)
			}
			if strings.Contains(got, "DROP") {
				t.Errorf("auditLogOrderBy leaked raw input: %q", got)
			}
		})
	}
}
