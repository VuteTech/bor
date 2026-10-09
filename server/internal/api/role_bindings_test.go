// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestValidateGlobalScope(t *testing.T) {
	someID := "236c1a2c-42f3-47ad-b10b-3a10b73c7f68"

	tests := []struct {
		name      string
		scopeType string
		scopeID   *string
		want      string
		wantErr   bool
	}{
		{name: "explicit global", scopeType: "global", want: models.ScopeGlobal},
		{name: "empty defaults to global", scopeType: "", want: models.ScopeGlobal},
		{name: "organization rejected", scopeType: "organization", wantErr: true},
		{name: "group rejected", scopeType: "group", wantErr: true},
		{name: "arbitrary value rejected", scopeType: "node_group", wantErr: true},
		{name: "global with scope_id rejected", scopeType: "global", scopeID: &someID, wantErr: true},
		{name: "scoped with scope_id rejected", scopeType: "group", scopeID: &someID, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateGlobalScope(tt.scopeType, tt.scopeID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateGlobalScope(%q, %v) expected error, got none", tt.scopeType, tt.scopeID)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateGlobalScope(%q, %v) error = %v", tt.scopeType, tt.scopeID, err)
			}
			if got != tt.want {
				t.Errorf("validateGlobalScope(%q, %v) = %q, want %q", tt.scopeType, tt.scopeID, got, tt.want)
			}
		})
	}
}
