// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy_test

import (
	"strings"
	"testing"

	"github.com/VuteTech/Bor/agent/internal/policy"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// mustJS generates JS for a policy and fails the test on error.
func mustJS(t *testing.T, pol *pb.PolkitPolicy) string {
	t.Helper()
	js, err := policy.PolkitPoliciesToJS(pol)
	if err != nil {
		t.Fatalf("PolkitPoliciesToJS: %v", err)
	}
	return string(js)
}

// TestPolkitPoliciesToJS_DenyActionPrefix checks the NM use-case: deny users
// not in "wheel" from managing networks via an action prefix.
func TestPolkitPoliciesToJS_DenyActionPrefix(t *testing.T) {
	pol := &pb.PolkitPolicy{
		Rules: []*pb.PolkitRule{
			{
				Description:     "Deny non-admins from managing networks",
				ActionPrefixes:  []string{"org.freedesktop.NetworkManager.network."},
				Subject:         &pb.PolkitSubjectFilter{InGroup: "wheel", NegateGroup: true},
				Result:          pb.PolkitResult_POLKIT_RESULT_NO,
			},
		},
	}

	js := mustJS(t, pol)

	if !strings.Contains(js, `action.id.indexOf("org.freedesktop.NetworkManager.network.") === 0`) {
		t.Errorf("expected action prefix match; got:\n%s", js)
	}
	if !strings.Contains(js, `!subject.isInGroup("wheel")`) {
		t.Errorf("expected negated group check; got:\n%s", js)
	}
	if !strings.Contains(js, "return polkit.Result.NO;") {
		t.Errorf("expected NO result; got:\n%s", js)
	}
}

// TestPolkitPoliciesToJS_AllowExactID checks allow via exact action IDs.
func TestPolkitPoliciesToJS_AllowExactID(t *testing.T) {
	pol := &pb.PolkitPolicy{
		Rules: []*pb.PolkitRule{
			{
				Description: "Allow admins to enable networks",
				ActionIds:   []string{"org.freedesktop.NetworkManager.network.enable"},
				Subject:     &pb.PolkitSubjectFilter{InGroup: "wheel"},
				Result:      pb.PolkitResult_POLKIT_RESULT_YES,
			},
		},
	}

	js := mustJS(t, pol)

	if !strings.Contains(js, `action.id === "org.freedesktop.NetworkManager.network.enable"`) {
		t.Errorf("expected exact action ID match; got:\n%s", js)
	}
	if !strings.Contains(js, `subject.isInGroup("wheel")`) {
		t.Errorf("expected group check; got:\n%s", js)
	}
	if !strings.Contains(js, "return polkit.Result.YES;") {
		t.Errorf("expected YES result; got:\n%s", js)
	}
}

// TestPolkitPoliciesToJS_AllResultTypes verifies all result enum values are
// mapped to their correct JS strings.
func TestPolkitPoliciesToJS_AllResultTypes(t *testing.T) {
	cases := []struct {
		result   pb.PolkitResult
		expected string
	}{
		{pb.PolkitResult_POLKIT_RESULT_YES, "YES"},
		{pb.PolkitResult_POLKIT_RESULT_NO, "NO"},
		{pb.PolkitResult_POLKIT_RESULT_AUTH_SELF, "AUTH_SELF"},
		{pb.PolkitResult_POLKIT_RESULT_AUTH_SELF_KEEP, "AUTH_SELF_KEEP"},
		{pb.PolkitResult_POLKIT_RESULT_AUTH_ADMIN, "AUTH_ADMIN"},
		{pb.PolkitResult_POLKIT_RESULT_AUTH_ADMIN_KEEP, "AUTH_ADMIN_KEEP"},
	}

	for _, tc := range cases {
		pol := &pb.PolkitPolicy{
			Rules: []*pb.PolkitRule{
				{
					Description: "test rule",
					ActionIds:   []string{"org.example.test"},
					Result:      tc.result,
				},
			},
		}
		js := mustJS(t, pol)
		want := "return polkit.Result." + tc.expected + ";"
		if !strings.Contains(js, want) {
			t.Errorf("result %v: expected %q in output; got:\n%s", tc.result, want, js)
		}
	}
}

// TestPolkitPoliciesToJS_SubjectFilters checks all subject filter fields.
func TestPolkitPoliciesToJS_SubjectFilters(t *testing.T) {
	pol := &pb.PolkitPolicy{
		Rules: []*pb.PolkitRule{
			{
				Description: "full subject filter",
				ActionIds:   []string{"org.example.test"},
				Subject: &pb.PolkitSubjectFilter{
					IsUser:        "alice",
					RequireLocal:  true,
					RequireActive: true,
					SystemUnit:    "example.service",
				},
				Result: pb.PolkitResult_POLKIT_RESULT_YES,
			},
		},
	}

	js := mustJS(t, pol)

	checks := []string{
		`subject.user === "alice"`,
		`subject.local === true`,
		`subject.active === true`,
		`subject.system_unit === "example.service"`,
	}
	for _, check := range checks {
		if !strings.Contains(js, check) {
			t.Errorf("expected %q in output; got:\n%s", check, js)
		}
	}
}

// TestMergePolkitPolicies verifies that highest-priority (first in slice)
// rules come first in the merged output.
func TestMergePolkitPolicies(t *testing.T) {
	highPriority := &pb.PolkitPolicy{
		FilePrefix: "50",
		Rules: []*pb.PolkitRule{
			{Description: "high-priority rule", ActionIds: []string{"org.example.high"}, Result: pb.PolkitResult_POLKIT_RESULT_YES},
		},
	}
	lowPriority := &pb.PolkitPolicy{
		Rules: []*pb.PolkitRule{
			{Description: "low-priority rule", ActionIds: []string{"org.example.low"}, Result: pb.PolkitResult_POLKIT_RESULT_NO},
		},
	}

	// Caller sorts descending by priority before calling Merge.
	merged := policy.MergePolkitPolicies([]*pb.PolkitPolicy{highPriority, lowPriority})

	if len(merged.GetRules()) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(merged.GetRules()))
	}
	if merged.GetRules()[0].GetDescription() != "high-priority rule" {
		t.Errorf("expected high-priority rule first, got %q", merged.GetRules()[0].GetDescription())
	}
	if merged.GetRules()[1].GetDescription() != "low-priority rule" {
		t.Errorf("expected low-priority rule second, got %q", merged.GetRules()[1].GetDescription())
	}
	if merged.GetFilePrefix() != "50" {
		t.Errorf("expected file prefix from highest-priority policy, got %q", merged.GetFilePrefix())
	}
}

// TestPolkitPoliciesToJS_ManagedHeader checks that the managed header is present.
func TestPolkitPoliciesToJS_ManagedHeader(t *testing.T) {
	pol := &pb.PolkitPolicy{
		Rules: []*pb.PolkitRule{
			{Description: "test", ActionIds: []string{"org.example.test"}, Result: pb.PolkitResult_POLKIT_RESULT_NO},
		},
	}
	js := mustJS(t, pol)
	if !strings.Contains(js, "managed by Bor") {
		t.Errorf("expected managed-by-Bor header; got:\n%s", js)
	}
}

// TestPolkitPoliciesToJS_MultipleActionsOR verifies that multiple action IDs
// and prefixes are combined with &&-separated conditions.
func TestPolkitPoliciesToJS_MultipleActionsOR(t *testing.T) {
	pol := &pb.PolkitPolicy{
		Rules: []*pb.PolkitRule{
			{
				Description:    "multi action",
				ActionIds:      []string{"org.example.a", "org.example.b"},
				ActionPrefixes: []string{"org.example.prefix."},
				Result:         pb.PolkitResult_POLKIT_RESULT_NO,
			},
		},
	}
	js := mustJS(t, pol)
	if !strings.Contains(js, `action.id === "org.example.a"`) {
		t.Errorf("expected first exact ID; got:\n%s", js)
	}
	if !strings.Contains(js, `action.id === "org.example.b"`) {
		t.Errorf("expected second exact ID; got:\n%s", js)
	}
	if !strings.Contains(js, `action.id.indexOf("org.example.prefix.") === 0`) {
		t.Errorf("expected prefix match; got:\n%s", js)
	}
}

// TestRollupPolkitCompliance_AllCompliant verifies COMPLIANT when all rules pass.
func TestRollupPolkitCompliance_AllCompliant(t *testing.T) {
	results := []policy.PolkitRuleResult{
		{RuleIndex: 0, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
		{RuleIndex: 1, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
	}
	status, msg := policy.RollupPolkitCompliance(results)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("expected COMPLIANT, got %v (msg: %s)", status, msg)
	}
}

// TestRollupPolkitCompliance_NonCompliant verifies NON_COMPLIANT propagates.
func TestRollupPolkitCompliance_NonCompliant(t *testing.T) {
	results := []policy.PolkitRuleResult{
		{RuleIndex: 0, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
		{RuleIndex: 1, Description: "bad rule", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "rule block missing"},
	}
	status, msg := policy.RollupPolkitCompliance(results)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("expected NON_COMPLIANT, got %v", status)
	}
	if !strings.Contains(msg, "bad rule") {
		t.Errorf("expected rule description in message; got %q", msg)
	}
}
