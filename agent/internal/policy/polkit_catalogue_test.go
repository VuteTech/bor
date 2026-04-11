// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy_test

import (
	"testing"

	"github.com/VuteTech/Bor/agent/internal/policy"
)

var samplePkactionOutput = []byte(`com.example.action1:
  description:       Do something
  message:           Authentication required to do something
  vendor:            Example Corp
  vendor_url:        https://example.com
  icon:
  implicit any:      auth_admin
  implicit inactive: auth_admin
  implicit active:   yes

com.example.action2:
  description:       Do something else
  message:
  vendor:
  vendor_url:
  icon:
  implicit any:      no
  implicit inactive: no
  implicit active:   no

`)

func TestParsePolkitActions(t *testing.T) {
	actions := policy.ParsePolkitActionsForTest(samplePkactionOutput)

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	a1 := actions[0]
	if a1.GetActionId() != "com.example.action1" {
		t.Errorf("expected action1, got %q", a1.GetActionId())
	}
	if a1.GetDescription() != "Do something" {
		t.Errorf("expected description 'Do something', got %q", a1.GetDescription())
	}
	if a1.GetVendor() != "Example Corp" {
		t.Errorf("expected vendor 'Example Corp', got %q", a1.GetVendor())
	}
	if a1.GetDefaultAny() != "auth_admin" {
		t.Errorf("expected default_any 'auth_admin', got %q", a1.GetDefaultAny())
	}
	if a1.GetDefaultActive() != "yes" {
		t.Errorf("expected default_active 'yes', got %q", a1.GetDefaultActive())
	}

	a2 := actions[1]
	if a2.GetActionId() != "com.example.action2" {
		t.Errorf("expected action2, got %q", a2.GetActionId())
	}
	if a2.GetDefaultAny() != "no" {
		t.Errorf("expected default_any 'no', got %q", a2.GetDefaultAny())
	}
}
