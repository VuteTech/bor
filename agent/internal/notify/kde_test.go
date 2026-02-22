// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package notify

import (
	"testing"
	"time"
)

func TestParseSessionFile(t *testing.T) {
	content := `UID=1000
USER=testuser
ACTIVE=1
TYPE=x11
CLASS=user
SCOPE=session-2.scope
`
	props := parseSessionFile(content)

	tests := []struct {
		key, want string
	}{
		{"UID", "1000"},
		{"USER", "testuser"},
		{"ACTIVE", "1"},
		{"TYPE", "x11"},
		{"CLASS", "user"},
	}

	for _, tt := range tests {
		if got := props[tt.key]; got != tt.want {
			t.Errorf("props[%q] = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestParseSessionFileEmpty(t *testing.T) {
	props := parseSessionFile("")
	if len(props) != 0 {
		t.Errorf("expected empty map, got %v", props)
	}
}

func TestParseSessionFileComments(t *testing.T) {
	content := `# comment
UID=1000
# another comment
USER=testuser
`
	props := parseSessionFile(content)
	if len(props) != 2 {
		t.Errorf("expected 2 entries, got %d", len(props))
	}
}

func TestNotifierCooldown(t *testing.T) {
	n := New()
	now := time.Now()
	cooldown := 5 * time.Minute

	// First notification should be allowed.
	if !n.shouldNotify(1000, now, cooldown) {
		t.Error("first notification should be allowed")
	}

	// Record it.
	n.recordNotification(1000, now)

	// Immediate retry should be blocked by cooldown.
	if n.shouldNotify(1000, now.Add(1*time.Second), cooldown) {
		t.Error("notification within cooldown should be blocked")
	}

	// After cooldown expires, should be allowed again.
	if !n.shouldNotify(1000, now.Add(6*time.Minute), cooldown) {
		t.Error("notification after cooldown should be allowed")
	}
}

func TestNotifierCooldownDifferentUIDs(t *testing.T) {
	n := New()
	now := time.Now()
	cooldown := 5 * time.Minute

	n.recordNotification(1000, now)

	// Different UID should not be affected by the first UID's cooldown.
	if !n.shouldNotify(1001, now, cooldown) {
		t.Error("different UID should not be affected by cooldown")
	}
}

func TestActiveGraphicalSessionsParsing(t *testing.T) {
	// Test the session file parsing logic with various session types.
	tests := []struct {
		name     string
		content  string
		wantType string
		isActive bool
	}{
		{
			name:     "x11 active",
			content:  "TYPE=x11\nACTIVE=1\nUID=1000\nUSER=test",
			wantType: "x11",
			isActive: true,
		},
		{
			name:     "wayland active",
			content:  "TYPE=wayland\nACTIVE=1\nUID=1000\nUSER=test",
			wantType: "wayland",
			isActive: true,
		},
		{
			name:     "tty session",
			content:  "TYPE=tty\nACTIVE=1\nUID=1000\nUSER=test",
			wantType: "tty",
			isActive: false,
		},
		{
			name:     "inactive x11",
			content:  "TYPE=x11\nACTIVE=0\nUID=1000\nUSER=test",
			wantType: "x11",
			isActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := parseSessionFile(tt.content)
			isGraphical := (props["TYPE"] == "x11" || props["TYPE"] == "wayland") && props["ACTIVE"] == "1"
			if isGraphical != tt.isActive {
				t.Errorf("isGraphical = %v, want %v", isGraphical, tt.isActive)
			}
		})
	}
}
