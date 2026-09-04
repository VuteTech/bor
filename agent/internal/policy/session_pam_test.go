// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"reflect"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func pamSource(name string, enforcePam *bool, includeSSH bool, rules ...*pb.SessionAccessRule) SessionPolicySource {
	return SessionPolicySource{Name: name, Policy: &pb.SessionAccessPolicy{
		Rules:      rules,
		EnforcePam: enforcePam,
		IncludeSsh: includeSSH,
	}}
}

func boolp(b bool) *bool { return &b }

func noGroups(string) []string { return nil }

func TestPamTimeTerm(t *testing.T) {
	cases := []struct {
		days       []string
		start, end string
		want       string
	}{
		{[]string{"mon", "wed", "fri"}, "08:00", "17:00", "MoWeFr0800-1700"},
		{[]string{"mon", "tue", "wed", "thu", "fri"}, "08:00", "17:30", "MoTuWeThFr0800-1730"},
		{[]string{"fri"}, "22:00", "02:00", "Fr2200-0200"},          // crosses midnight
		{[]string{"sat", "sun"}, "00:00", "00:00", "SaSu0000-2400"}, // full day
		{[]string{"sun"}, "22:00", "02:00", "Su2200-0200"},
	}
	for _, c := range cases {
		got, err := pamTimeTerm(&pb.SessionAccessWindow{Days: c.days, Start: c.start, End: c.end})
		if err != nil {
			t.Errorf("term(%v %s-%s) error: %v", c.days, c.start, c.end, err)
			continue
		}
		if got != c.want {
			t.Errorf("term(%v %s-%s) = %q, want %q", c.days, c.start, c.end, got, c.want)
		}
	}
}

func TestPamTimeTerm_DayOrderNormalized(t *testing.T) {
	// Days given out of order must emit in Mon..Sun order.
	got, _ := pamTimeTerm(&pb.SessionAccessWindow{Days: []string{"fri", "mon", "wed"}, Start: "08:00", End: "17:00"})
	if got != "MoWeFr0800-1700" {
		t.Errorf("got %q, want MoWeFr0800-1700", got)
	}
}

func TestRenderTimeConfLines_Basic(t *testing.T) {
	lines, warnings := RenderTimeConfLines([]SessionPolicySource{
		pamSource("Lab", nil, false, &pb.SessionAccessRule{
			Users:   []string{"alice"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"mon", "tue", "wed", "thu", "fri"}, Start: "08:00", End: "17:00"}},
		}),
	}, noGroups)
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	want := []string{"login|gdm-password|sddm|lightdm|xdm;*;alice;MoTuWeThFr0800-1700"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines =\n%v\nwant\n%v", lines, want)
	}
}

func TestRenderTimeConfLines_IncludeSSH(t *testing.T) {
	lines, _ := RenderTimeConfLines([]SessionPolicySource{
		pamSource("Lab", boolp(true), true, &pb.SessionAccessRule{
			Users:   []string{"alice"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"mon"}, Start: "08:00", End: "17:00"}},
		}),
	}, noGroups)
	if len(lines) != 1 || !strings.Contains(lines[0], "|sshd;") {
		t.Fatalf("expected sshd in services, got %v", lines)
	}
}

func TestRenderTimeConfLines_EnforcePamOffSkips(t *testing.T) {
	lines, _ := RenderTimeConfLines([]SessionPolicySource{
		pamSource("NoPam", boolp(false), false, &pb.SessionAccessRule{
			Users:   []string{"alice"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"mon"}, Start: "08:00", End: "17:00"}},
		}),
	}, noGroups)
	if len(lines) != 0 {
		t.Errorf("enforce_pam=false must produce no PAM lines, got %v", lines)
	}
}

func TestRenderTimeConfLines_RootExcluded(t *testing.T) {
	lines, _ := RenderTimeConfLines([]SessionPolicySource{
		pamSource("Lab", nil, false, &pb.SessionAccessRule{
			Users:   []string{"root", "alice"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"mon"}, Start: "08:00", End: "17:00"}},
		}),
	}, noGroups)
	for _, l := range lines {
		if strings.Contains(l, ";root;") {
			t.Errorf("root must never appear in a rule: %q", l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("expected exactly alice's line, got %v", lines)
	}
}

func TestRenderTimeConfLines_GroupExpansionAndUnion(t *testing.T) {
	groups := func(g string) []string {
		if g == "students" {
			return []string{"alice", "bob", "root"} // root must be filtered
		}
		return nil
	}
	// Two policies target alice; her PAM allow-set is the union of both windows.
	lines, _ := RenderTimeConfLines([]SessionPolicySource{
		pamSource("A", nil, false, &pb.SessionAccessRule{
			Groups:  []string{"students"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"mon"}, Start: "08:00", End: "12:00"}},
		}),
		pamSource("B", nil, false, &pb.SessionAccessRule{
			Users:   []string{"alice"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"sat"}, Start: "10:00", End: "12:00"}},
		}),
	}, groups)

	byUser := map[string]string{}
	for _, l := range lines {
		parts := strings.Split(l, ";")
		byUser[parts[2]] = parts[3]
	}
	if _, ok := byUser["root"]; ok {
		t.Error("root leaked via group expansion")
	}
	if byUser["bob"] != "Mo0800-1200" {
		t.Errorf("bob = %q, want Mo0800-1200", byUser["bob"])
	}
	if byUser["alice"] != "Mo0800-1200|Sa1000-1200" {
		t.Errorf("alice = %q, want union Mo0800-1200|Sa1000-1200", byUser["alice"])
	}
}

func mustMerge(t *testing.T, existing string, lines []string) string {
	t.Helper()
	out, err := MergeTimeConf(existing, lines)
	if err != nil {
		t.Fatalf("MergeTimeConf: %v", err)
	}
	return out
}

func TestRenderTimeConfLines_DirectoryStyleName(t *testing.T) {
	// SSSD fully-qualified names carry a non-leading "@" and must render; a
	// leading "@" would be a pam_time netgroup and must never reach the file.
	lines, warnings := RenderTimeConfLines([]SessionPolicySource{
		pamSource("Dir", nil, false, &pb.SessionAccessRule{
			Users:   []string{"alice@corp.example", "@students"},
			Windows: []*pb.SessionAccessWindow{{Days: []string{"mon"}, Start: "08:00", End: "17:00"}},
		}),
	}, noGroups)
	if len(lines) != 1 || !strings.Contains(lines[0], ";alice@corp.example;") {
		t.Fatalf("expected alice@corp.example line, got %v", lines)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "@students") {
		t.Errorf("expected a self-check warning for @students, got %v", warnings)
	}
}

func TestMergeTimeConf_RefusesMissingEndMarker(t *testing.T) {
	// A damaged block must not cause admin rules after it to be discarded.
	damaged := "# admin\n" + timeConfBeginMarker + "\nlogin;*;alice;Mo0800-1700\n# admin rule after damage\nfoo;*;bar;Al0000-2400\n"
	if _, err := MergeTimeConf(damaged, []string{"login;*;bob;Tu0900-1600"}); err == nil {
		t.Fatal("expected refusal when END marker is missing")
	}
}

func TestMergeTimeConf_LegacyMarkerIsReplacedNotDuplicated(t *testing.T) {
	legacy := "# admin\n" + legacyTimeConfBeginMarker + "\nlogin;*;alice;Mo0800-1700\n" + timeConfEndMarker + "\n"
	out := mustMerge(t, legacy, []string{"login;*;bob;Tu0900-1600"})
	if strings.Contains(out, "alice") || strings.Contains(out, legacyTimeConfBeginMarker) {
		t.Error("legacy block should have been replaced, not kept")
	}
	if strings.Count(out, "bor-managed") != 2 { // one BEGIN + one END
		t.Errorf("expected exactly one managed block, got:\n%s", out)
	}
}

func TestMergeTimeConf_PreservesAdminContent(t *testing.T) {
	existing := "# admin rule\ngames;*;!waster;Wk1800-0800\n"
	lines := []string{"login;*;alice;Mo0800-1700"}
	out := mustMerge(t, existing, lines)

	if !strings.Contains(out, "games;*;!waster;Wk1800-0800") {
		t.Error("admin content was lost")
	}
	if !strings.Contains(out, timeConfBeginMarker) || !strings.Contains(out, timeConfEndMarker) {
		t.Error("managed markers missing")
	}
	if !strings.Contains(out, "login;*;alice;Mo0800-1700") {
		t.Error("managed line missing")
	}
	if !HasManagedBlock(out) {
		t.Error("HasManagedBlock should be true")
	}
}

func TestMergeTimeConf_ReplaceAndRemove(t *testing.T) {
	base := "# admin\nfoo;*;bar;Al0000-2400\n"
	withBlock := mustMerge(t, base, []string{"login;*;alice;Mo0800-1700"})

	// Replacing updates the block, admin content intact, no duplication.
	replaced := mustMerge(t, withBlock, []string{"login;*;bob;Tu0900-1600"})
	if strings.Contains(replaced, "alice") {
		t.Error("old managed line should be gone after replace")
	}
	if !strings.Contains(replaced, "bob") || !strings.Contains(replaced, "foo;*;bar;Al0000-2400") {
		t.Error("replace lost new line or admin content")
	}
	if strings.Count(replaced, timeConfBeginMarker) != 1 {
		t.Errorf("expected exactly one managed block, got %d", strings.Count(replaced, timeConfBeginMarker))
	}

	// Removing (empty lines) strips the block but keeps admin content.
	removed := mustMerge(t, replaced, nil)
	if HasManagedBlock(removed) {
		t.Error("managed block should be gone")
	}
	if !strings.Contains(removed, "foo;*;bar;Al0000-2400") {
		t.Error("admin content lost on remove")
	}
}

func TestMergeTimeConf_EmptyStartAndEmptyResult(t *testing.T) {
	out := mustMerge(t, "", []string{"login;*;alice;Mo0800-1700"})
	if !strings.Contains(out, "alice") || !strings.HasSuffix(out, "\n") {
		t.Errorf("unexpected output for empty start: %q", out)
	}
	if mustMerge(t, "", nil) != "" {
		t.Error("empty input + no lines should be empty")
	}
}
