// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// ── MergePackageEntries ───────────────────────────────────────────────────────

func TestMergePackageEntries_Empty(t *testing.T) {
	result := MergePackageEntries(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result))
	}
}

func TestMergePackageEntries_NilPolicySkipped(t *testing.T) {
	result := MergePackageEntries([]*pb.PackagePolicy{nil})
	if len(result) != 0 {
		t.Errorf("expected 0 entries for nil policy, got %d", len(result))
	}
}

func TestMergePackageEntries_SinglePolicy(t *testing.T) {
	pol := &pb.PackagePolicy{
		Packages: []*pb.PackageEntry{
			{Name: "vim", State: pb.PackageState_PACKAGE_STATE_PRESENT},
		},
	}
	result := MergePackageEntries([]*pb.PackagePolicy{pol})
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].GetName() != "vim" {
		t.Errorf("unexpected name: %q", result[0].GetName())
	}
}

func TestMergePackageEntries_LaterPolicyOverrides(t *testing.T) {
	p1 := &pb.PackagePolicy{
		Packages: []*pb.PackageEntry{
			{Name: "vim", State: pb.PackageState_PACKAGE_STATE_PRESENT},
		},
	}
	p2 := &pb.PackagePolicy{
		Packages: []*pb.PackageEntry{
			{Name: "vim", State: pb.PackageState_PACKAGE_STATE_ABSENT},
		},
	}
	result := MergePackageEntries([]*pb.PackagePolicy{p1, p2})
	if len(result) != 1 {
		t.Fatalf("expected 1 entry after merge, got %d", len(result))
	}
	if result[0].GetState() != pb.PackageState_PACKAGE_STATE_ABSENT {
		t.Errorf("expected ABSENT (last wins), got %v", result[0].GetState())
	}
}

func TestMergePackageEntries_DifferentNamesPreserved(t *testing.T) {
	p1 := &pb.PackagePolicy{
		Packages: []*pb.PackageEntry{{Name: "vim", State: pb.PackageState_PACKAGE_STATE_PRESENT}},
	}
	p2 := &pb.PackagePolicy{
		Packages: []*pb.PackageEntry{{Name: "emacs", State: pb.PackageState_PACKAGE_STATE_ABSENT}},
	}
	result := MergePackageEntries([]*pb.PackagePolicy{p1, p2})
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
}

func TestMergePackageEntries_OrderPreservedFirstSeen(t *testing.T) {
	pol := &pb.PackagePolicy{
		Packages: []*pb.PackageEntry{
			{Name: "zzz", State: pb.PackageState_PACKAGE_STATE_PRESENT},
			{Name: "aaa", State: pb.PackageState_PACKAGE_STATE_PRESENT},
		},
	}
	result := MergePackageEntries([]*pb.PackagePolicy{pol})
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result[0].GetName() != "zzz" || result[1].GetName() != "aaa" {
		t.Errorf("insertion order not preserved: %q %q", result[0].GetName(), result[1].GetName())
	}
}

// ── RollupPackageCompliance ───────────────────────────────────────────────────

func TestRollupPackageCompliance_Empty(t *testing.T) {
	status, _ := RollupPackageCompliance(nil)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_UNKNOWN {
		t.Errorf("expected UNKNOWN for empty items, got %v", status)
	}
}

func TestRollupPackageCompliance_AllCompliant(t *testing.T) {
	items := []ComplianceItem{
		{Key: "pkg:vim", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
		{Key: "pkg:curl", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
	}
	status, msg := RollupPackageCompliance(items)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("expected COMPLIANT, got %v", status)
	}
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestRollupPackageCompliance_OneNonCompliant(t *testing.T) {
	items := []ComplianceItem{
		{Key: "pkg:vim", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
		{Key: "pkg:curl", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "not installed"},
	}
	status, msg := RollupPackageCompliance(items)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("expected NON_COMPLIANT, got %v", status)
	}
	if !strings.Contains(msg, "pkg:curl") {
		t.Errorf("expected message to reference failing key, got %q", msg)
	}
}

func TestRollupPackageCompliance_ErrorDominatesNonCompliant(t *testing.T) {
	items := []ComplianceItem{
		{Key: "pkg:vim", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "absent"},
		{Key: "pkg:curl", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "dbus failed"},
	}
	status, _ := RollupPackageCompliance(items)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Errorf("expected ERROR to dominate NON_COMPLIANT, got %v", status)
	}
}

func TestRollupPackageCompliance_AllInapplicable(t *testing.T) {
	items := []ComplianceItem{
		{Key: "pkg:vim", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE},
		{Key: "pkg:curl", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE},
	}
	status, _ := RollupPackageCompliance(items)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("expected INAPPLICABLE when all items are inapplicable, got %v", status)
	}
}

func TestRollupPackageCompliance_InapplicablePlusCompliant(t *testing.T) {
	items := []ComplianceItem{
		{Key: "pkg:vim", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE},
		{Key: "pkg:curl", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
	}
	status, _ := RollupPackageCompliance(items)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("expected COMPLIANT when inapplicable + compliant, got %v", status)
	}
}

// ── notFoundItem ──────────────────────────────────────────────────────────────

func TestNotFoundItem_Required(t *testing.T) {
	item := notFoundItem("pkg:foo", "foo", false)
	if item.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("required missing package should be NON_COMPLIANT, got %v", item.Status)
	}
}

func TestNotFoundItem_Optional(t *testing.T) {
	item := notFoundItem("pkg:bar", "bar", true)
	if item.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("optional missing package should be INAPPLICABLE, got %v", item.Status)
	}
}
