// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestComplianceControlsFor(t *testing.T) {
	mode := ComplianceControlsFor("selinux:mode")
	if len(mode) != 2 || mode[1].ID != "1.3.1.5" || mode[1].Short != "CIS RHEL 9 1.3.1.5" || !strings.Contains(mode[1].Title, "enforcing") {
		t.Errorf("selinux:mode controls = %+v", mode)
	}
	if got := ComplianceControlsFor("apparmor:profile"); len(got) != 2 || got[0].ID != "1.3.1.3" {
		t.Errorf("apparmor:profile controls = %+v", got)
	}
	if ComplianceControlsFor("selinux:boolean") != nil || ComplianceControlsFor("") != nil {
		t.Error("items without a known control must map to nil")
	}
	// The returned slice is a copy.
	mode[0].ID = "x"
	if ComplianceControlsFor("selinux:mode")[0].ID == "x" {
		t.Error("ComplianceControlsFor must not expose the shared table")
	}
}

func TestAnnotateComplianceItems(t *testing.T) {
	in := json.RawMessage(`[{"key":"mode","status":"compliant","message":"enforcing","schema_id":"selinux:mode"},{"key":"deny_ptrace","status":"compliant","schema_id":"selinux:boolean"}]`)
	out := AnnotateComplianceItems(in)
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatalf("annotated document does not parse: %v", err)
	}
	if _, ok := items[0]["controls"]; !ok {
		t.Error("selinux:mode item lacks controls")
	}
	if _, ok := items[1]["controls"]; ok {
		t.Error("selinux:boolean item must not carry controls")
	}
	if string(items[0]["message"]) != `"enforcing"` {
		t.Errorf("other fields must survive, got %s", items[0]["message"])
	}

	untouched := json.RawMessage(`[{"key":"a","schema_id":"flatpak:app"}]`)
	if got := AnnotateComplianceItems(untouched); string(got) != string(untouched) {
		t.Errorf("document without controls must be returned unchanged, got %s", got)
	}
	broken := json.RawMessage(`{"not":"a list"`)
	if got := AnnotateComplianceItems(broken); string(got) != string(broken) {
		t.Error("an unparseable document must be returned unchanged")
	}
	if got := AnnotateComplianceItems(nil); got != nil {
		t.Error("nil must stay nil")
	}
}
