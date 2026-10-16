// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestGroupSlug(t *testing.T) {
	if got := GroupSlug("students"); got != "students" {
		t.Errorf("plain name: %q", got)
	}
	if got := GroupSlug("Teachers"); got != "teachers" {
		t.Errorf("case folded without hash: %q", got)
	}
	du := GroupSlug("Domain Users")
	if !strings.HasPrefix(du, "domain_users-") || len(du) != len("domain_users-")+6 {
		t.Errorf("sanitised name needs a hash suffix: %q", du)
	}
	if GroupSlug("Domain Users") == GroupSlug("domain_users") {
		t.Error("sanitised and literal names must stay distinct")
	}
	if got := GroupSlug("staff@ad.example.com"); !strings.HasPrefix(got, "staff_ad.example.com-") {
		t.Errorf("fully qualified name: %q", got)
	}
	if got := GroupSlug("///"); !strings.HasPrefix(got, "group-") {
		t.Errorf("name without usable characters: %q", got)
	}
}

func TestMergeKConfigPolicies_GroupScopes(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "base", Name: "Baseline", Priority: 1, Policy: &pb.KConfigPolicy{
			ShellAccess: kcBool(false), EnforcedFields: []string{"shellAccess"},
			IconTheme: kcStr("breeze"),
		}},
		{ID: "stud", Name: "Students", Priority: 5, Groups: []string{"students"}, Policy: &pb.KConfigPolicy{
			RunCommand: kcBool(false), EnforcedFields: []string{"runCommand"},
			IconTheme: kcStr("breeze-dark"),
		}},
		{ID: "teach", Name: "Teachers", Priority: 9, Groups: []string{"Teachers", "students"}, Policy: &pb.KConfigPolicy{
			IconTheme: kcStr("oxygen"),
			Layout:    &pb.KConfigLayout{WallpaperImage: "/w.png"},
		}},
	}

	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Node-wide files only hold the unscoped policy.
	node := string(res.Files["kdeglobals"])
	if !strings.Contains(node, "shell_access=false") || !strings.Contains(node, "Theme=breeze\n") {
		t.Errorf("node-wide kdeglobals wrong:\n%s", node)
	}
	if strings.Contains(node, "run_command") || strings.Contains(node, "oxygen") {
		t.Errorf("scoped entries leaked into the node-wide file:\n%s", node)
	}

	// Scoped files: students get both scoped policies with Teachers
	// (priority 9) winning the icon theme; teachers get theirs.
	students := string(res.GroupFiles["students"]["kdeglobals"])
	if !strings.Contains(students, "run_command=false") || !strings.Contains(students, "Theme=oxygen") || strings.Contains(students, "shell_access") {
		t.Errorf("students overlay wrong:\n%s", students)
	}
	teachers := string(res.GroupFiles["teachers"]["kdeglobals"])
	if !strings.Contains(teachers, "Theme=oxygen") || strings.Contains(teachers, "run_command") {
		t.Errorf("teachers overlay wrong:\n%s", teachers)
	}

	// Scope order for the hook: highest priority first (both scopes carry
	// the priority-9 policy), ties broken by slug.
	if len(res.Groups) != 2 || res.Groups[0].Slug != "students" || res.Groups[0].Priority != 9 || res.Groups[1].Slug != "teachers" || res.Groups[1].Priority != 9 {
		t.Errorf("unexpected group scopes: %+v", res.Groups)
	}
	if strings.Join(res.Groups[0].PolicyIDs, ",") != "teach,stud" {
		t.Errorf("students scope should list both policies in priority order, got %v", res.Groups[0].PolicyIDs)
	}
	if strings.Join(res.Groups[1].PolicyIDs, ",") != "teach" {
		t.Errorf("teachers scope should list only the teachers policy, got %v", res.Groups[1].PolicyIDs)
	}

	// Items of scoped entries carry the overlay prefix; the lower-priority
	// students theme is reported as overridden within its scope only.
	var studOverridden, studPrefixed bool
	for _, it := range res.Items["stud"] {
		if strings.HasPrefix(it.SchemaId, "groups/students/") {
			studPrefixed = true
		}
		if it.Key == "[Icons] Theme" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE && strings.Contains(it.Message, "Teachers") {
			studOverridden = true
		}
	}
	if !studPrefixed || !studOverridden {
		t.Errorf("students items wrong: %+v", res.Items["stud"])
	}

	// A layout on a scoped policy is refused with an explanation.
	var layoutRefused bool
	for _, it := range res.Items["teach"] {
		if it.SchemaId == "layout" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE && strings.Contains(it.Message, "group scope") {
			layoutRefused = true
		}
	}
	if !layoutRefused || res.Layout != nil {
		t.Errorf("scoped layout should be refused, got layout=%v items=%+v", res.Layout, res.Items["teach"])
	}
}

func TestMergeKConfigPolicies_NoGroupsNoGroupFiles(t *testing.T) {
	res, err := MergeKConfigPolicies([]KConfigPolicySource{{ID: "p", Name: "P", Priority: 1, Policy: &pb.KConfigPolicy{ShellAccess: kcBool(false)}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.GroupFiles != nil || len(res.Groups) != 0 {
		t.Errorf("unscoped policies must not create group scopes: %+v", res.Groups)
	}
}

func TestEnvScriptContent_Groups(t *testing.T) {
	got := envScriptContent("/etc/bor/xdg", "/etc/bor/share", []GroupDir{
		{GID: 1002, Dir: "/etc/bor/xdg/groups/teachers"},
		{GID: 1003, Dir: "/etc/bor/xdg/groups/students"},
	})
	for _, want := range []string{
		`bor_gids=" $(id -G 2>/dev/null) "`,
		`case "$bor_gids" in *" 1002 "*) bor_group_dirs="${bor_group_dirs}/etc/bor/xdg/groups/teachers:" ;; esac`,
		`case "$bor_gids" in *" 1003 "*) bor_group_dirs="${bor_group_dirs}/etc/bor/xdg/groups/students:" ;; esac`,
		`export XDG_CONFIG_DIRS="${bor_group_dirs}/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hook lacks %q:\n%s", want, got)
		}
	}
	// Teachers (first) must come before students in the script, so their
	// directory ends up earlier (more specific) in XDG_CONFIG_DIRS.
	if strings.Index(got, "1002") > strings.Index(got, "1003") {
		t.Error("group directories must keep priority order")
	}
	// Group names never appear in the shell; only numeric GIDs do.
	if strings.Contains(got, "Teachers") {
		t.Error("group names must not be embedded in the hook")
	}

	// Without groups the hook is the plain version: no id call, no helper
	// variables, so nodes without scoped policies see no change at all.
	plain := envScriptContent("/etc/bor/xdg", "/etc/bor/share", nil)
	if strings.Contains(plain, "id -G") || strings.Contains(plain, "bor_group_dirs") {
		t.Errorf("without groups the hook must not carry group logic:\n%s", plain)
	}
	if !strings.Contains(plain, `export XDG_CONFIG_DIRS="/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}"`) {
		t.Errorf("plain hook wrong:\n%s", plain)
	}
}

func TestResolveGroupDirs(t *testing.T) {
	scopes := []KConfigGroupScope{
		{Name: "teachers", Slug: "teachers", Priority: 9, PolicyIDs: []string{"t"}},
		{Name: "ghosts", Slug: "ghosts", Priority: 5, PolicyIDs: []string{"g"}},
	}
	lookup := func(name string) (uint32, error) {
		if name == "teachers" {
			return 1002, nil
		}
		return 0, errors.New("unknown group")
	}
	dirs, unresolved := ResolveGroupDirs("/etc/bor/xdg", scopes, lookup)
	if len(dirs) != 1 || dirs[0].GID != 1002 || dirs[0].Dir != "/etc/bor/xdg/groups/teachers" {
		t.Errorf("dirs = %+v", dirs)
	}
	if len(unresolved) != 1 || unresolved[0].Slug != "ghosts" {
		t.Errorf("unresolved = %+v", unresolved)
	}
}

func TestSyncGroupOverlays(t *testing.T) {
	root := t.TempDir()
	changed, err := SyncGroupOverlays(root, map[string]map[string][]byte{
		"students": {"kdeglobals": []byte("[KDE Action Restrictions][$i]\nrun_command=false\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Errorf("expected one written file, got %v", changed)
	}
	data, err := os.ReadFile(filepath.Join(root, "groups", "students", "kdeglobals"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), ManagedFileHeader) || !strings.Contains(string(data), "run_command=false") {
		t.Errorf("overlay file wrong:\n%s", data)
	}
	if got := ListGroupOverlayFiles(root); len(got) != 1 {
		t.Errorf("ListGroupOverlayFiles = %v", got)
	}

	if _, err := SyncGroupOverlays(root, nil); err != nil {
		t.Fatal(err)
	}
	if got := ListGroupOverlayFiles(root); len(got) != 0 {
		t.Errorf("overlays should be cleared, got %v", got)
	}
}
