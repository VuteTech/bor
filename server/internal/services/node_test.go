// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestIsValidNodeStatus(t *testing.T) {
	tests := []struct {
		status string
		valid  bool
	}{
		{models.NodeStatusOnline, true},
		{models.NodeStatusDegraded, false},
		{models.NodeStatusOffline, true},
		{models.NodeStatusUnknown, true},
		{"invalid", false},
		{"", false},
		{"active", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := isValidNodeStatus(tt.status)
			if result != tt.valid {
				t.Errorf("isValidNodeStatus(%q) = %v, want %v", tt.status, result, tt.valid)
			}
		})
	}
}
