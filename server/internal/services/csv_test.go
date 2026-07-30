// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "testing"

func TestSanitizeCSVField(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text", "admin", "admin"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"timestamp", "2026-07-30T12:00:00Z", "2026-07-30T12:00:00Z"},
		{"ipv4", "10.0.0.1", "10.0.0.1"},
		{"equals formula", "=cmd|'/c calc'!A1", "'=cmd|'/c calc'!A1"},
		{"plus formula", "+SUM(A1:A2)", "'+SUM(A1:A2)"},
		{"minus formula", "-1+1", "'-1+1"},
		{"at formula", "@SUM(1)", "'@SUM(1)"},
		{"leading tab", "\t=1", "'\t=1"},
		{"leading cr", "\r=1", "'\r=1"},
		{"formula char mid-string", "user=name", "user=name"},
		{"multibyte lead", "über", "über"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeCSVField(tt.in); got != tt.want {
				t.Errorf("sanitizeCSVField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
