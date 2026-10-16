// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"errors"
	"os"
	"os/exec"
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
			Layout:    &pb.KConfigLayout{WallpaperImage: "/s.png", LockPackage: true},
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
	if !strings.Contains(node, "shell_access[$i]=false") || !strings.Contains(node, "Theme=breeze\n") {
		t.Errorf("node-wide kdeglobals wrong:\n%s", node)
	}
	if strings.Contains(node, "run_command") || strings.Contains(node, "oxygen") {
		t.Errorf("scoped entries leaked into the node-wide file:\n%s", node)
	}
	// Node-wide files must never lock a whole group, or KDE would ignore the
	// keys the group overlays add to it.
	if strings.Contains(node, "][$i]\n") {
		t.Errorf("group-level [$i] in the node-wide file:\n%s", node)
	}

	// Scoped files: students get both scoped policies with Teachers
	// (priority 9) winning the icon theme; teachers get theirs.
	students := string(res.GroupFiles["students"]["kdeglobals"])
	if !strings.Contains(students, "run_command[$i]=false") || !strings.Contains(students, "Theme=oxygen") || strings.Contains(students, "shell_access") {
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

	// A layout on a scoped policy is deployed to its groups: each group
	// overlay selects the generated package and the merge records the
	// selection per group; the node-wide files know nothing about it. In
	// the students scope the Teachers layout (priority 9) wins over the
	// Students one, which is reported as overridden within that scope.
	if res.Layout != nil || strings.Contains(node, "LookAndFeelPackage") {
		t.Errorf("scoped layout must not become the node-wide one: %v\n%s", res.Layout, node)
	}
	for _, slug := range []string{"students", "teachers"} {
		sel := res.GroupLayouts[slug]
		if sel == nil || sel.PolicyID != "teach" || sel.Layout.GetWallpaperImage() != "/w.png" || sel.Enforced {
			t.Fatalf("group %s layout selection wrong: %+v", slug, sel)
		}
		if !strings.Contains(string(res.GroupFiles[slug]["kdeglobals"]), "LookAndFeelPackage="+sel.PackageID()+"\n") {
			t.Errorf("group %s overlay should select %s:\n%s", slug, sel.PackageID(), res.GroupFiles[slug]["kdeglobals"])
		}
	}
	var deployed, overridden bool
	for _, it := range res.Items["teach"] {
		if it.SchemaId == "groups/students/kdeglobals" && it.Key == "[KDE] LookAndFeelPackage" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT && strings.Contains(it.Message, `for members of group "students"`) {
			deployed = true
		}
	}
	for _, it := range res.Items["stud"] {
		if it.SchemaId == "groups/students/layout" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE && strings.Contains(it.Message, "Teachers") {
			overridden = true
		}
	}
	if !deployed || !overridden {
		t.Errorf("layout items wrong (deployed=%v overridden=%v):\nteach: %+v\nstud: %+v", deployed, overridden, res.Items["teach"], res.Items["stud"])
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
	got := envScriptContent(EnvHook{ConfigPath: "/etc/bor/xdg", DataPath: "/etc/bor/share", Groups: []GroupDir{
		{GID: 1002, Slug: "teachers", Dir: "/etc/bor/xdg/groups/teachers", DataDir: "/etc/bor/share/groups/teachers", Layout: true},
		{GID: 1003, Slug: "students", Dir: "/etc/bor/xdg/groups/students", DataDir: "/etc/bor/share/groups/students"},
	}})
	for _, want := range []string{
		`bor_gids=" $(id -G 2>/dev/null) "`,
		`bor_node_late=":/etc/bor/share/late"`,
		// A group with a layout of its own drops the node-wide late directory
		// for its members; late directories are prepended so that the
		// highest-priority group's update scripts run last.
		`case "$bor_gids" in *" 1002 "*) bor_group_dirs="${bor_group_dirs}/etc/bor/xdg/groups/teachers:"; bor_group_data="${bor_group_data}/etc/bor/share/groups/teachers:"; bor_group_late=":/etc/bor/share/groups/teachers/late${bor_group_late}"; bor_node_late="" ;; esac`,
		`case "$bor_gids" in *" 1003 "*) bor_group_dirs="${bor_group_dirs}/etc/bor/xdg/groups/students:"; bor_group_data="${bor_group_data}/etc/bor/share/groups/students:"; bor_group_late=":/etc/bor/share/groups/students/late${bor_group_late}" ;; esac`,
		`export XDG_CONFIG_DIRS="${bor_group_dirs}/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}"`,
		`export XDG_DATA_DIRS="${bor_group_data}/etc/bor/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}${bor_node_late}${bor_group_late}"`,
		"unset bor_group_dirs\n",
		"unset bor_group_data bor_group_late bor_node_late\n",
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

	// The profile.d variant wraps both exports in the Plasma-session guard
	// and keeps the group logic; the Plasma hook has no guard.
	profile := profileScriptContent(EnvHook{ConfigPath: "/etc/bor/xdg", DataPath: "/etc/bor/share", Groups: []GroupDir{{GID: 1002, Slug: "teachers", Dir: "/etc/bor/xdg/groups/teachers", DataDir: "/etc/bor/share/groups/teachers"}}})
	for _, want := range []string{
		"bor_cfg=1; bor_data=1\n",
		`case ":${XDG_CURRENT_DESKTOP}:${XDG_SESSION_DESKTOP}:" in`,
		"if [ \"$bor_cfg\" = 1 ]; then\ncase \":${XDG_CONFIG_DIRS:-/etc/xdg}:\" in",
		"if [ \"$bor_data\" = 1 ]; then\ncase \":${XDG_DATA_DIRS:-/usr/local/share:/usr/share}:\" in",
		"unset bor_cfg bor_data\n",
		`*" 1002 "*`,
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile hook lacks %q:\n%s", want, profile)
		}
	}
	if strings.Contains(got, "bor_cfg") {
		t.Error("the Plasma hook must not carry the session guard")
	}

	// Without groups the hook is the plain version: no id call, no helper
	// variables, so nodes without scoped policies see no change at all.
	plain := envScriptContent(EnvHook{ConfigPath: "/etc/bor/xdg", DataPath: "/etc/bor/share"})
	if strings.Contains(plain, "id -G") || strings.Contains(plain, "bor_group_dirs") || strings.Contains(plain, "bor_group_data") || strings.Contains(plain, "plasmashellrc") {
		t.Errorf("without groups or enforced layouts the hook must not carry group or re-apply logic:\n%s", plain)
	}
	if !strings.Contains(plain, `export XDG_CONFIG_DIRS="/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}"`) || !strings.Contains(plain, `export XDG_DATA_DIRS="/etc/bor/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}:/etc/bor/share/late"`) {
		t.Errorf("plain hook wrong:\n%s", plain)
	}
}

func TestEnvScriptContent_Reapply(t *testing.T) {
	const nodeScript = "/etc/bor/share/late/plasma/shells/org.kde.plasma.desktop/contents/updates/bor-aaaa.js"
	const groupScript = "/etc/bor/share/groups/students/late/plasma/shells/org.kde.plasma.desktop/contents/updates/bor-bbbb.js"
	hook := EnvHook{ConfigPath: "/etc/bor/xdg", DataPath: "/etc/bor/share", Reapply: nodeScript, Groups: []GroupDir{
		{GID: 1002, Slug: "teachers", Dir: "/etc/bor/xdg/groups/teachers", DataDir: "/etc/bor/share/groups/teachers"},
		{GID: 1003, Slug: "students", Dir: "/etc/bor/xdg/groups/students", DataDir: "/etc/bor/share/groups/students", Reapply: groupScript},
	}}
	got := envScriptContent(hook)
	for _, want := range []string{
		`bor_done=$("$bor_rc" --file plasmashellrc --group Updates --key performed 2>/dev/null)`,
		`bor_again "` + nodeScript + `"`,
		`case "$bor_gids" in *" 1003 "*) bor_again "` + groupScript + `" ;; esac`,
		`"kwriteconfig${bor_rc#kreadconfig}" --file plasmashellrc --group Updates --key performed "$bor_keep"`,
		"unset -f bor_again\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hook lacks %q:\n%s", want, got)
		}
	}
	// Only the group with an enforced layout gets a line; the record is
	// dropped while bor_gids is still set; the profile.d hook never touches
	// Plasma's records.
	if strings.Contains(got, `*" 1002 "*) bor_again`) {
		t.Error("group without an enforced layout must not re-apply anything")
	}
	if strings.Index(got, "bor_again") > strings.Index(got, "unset bor_gids") {
		t.Error("re-apply block must come before bor_gids is unset")
	}
	if profile := profileScriptContent(hook); strings.Contains(profile, "plasmashellrc") {
		t.Errorf("profile.d hook must not touch plasmashellrc:\n%s", profile)
	}
}

// The re-apply snippet is run by a real shell against fake kreadconfig6 and
// kwriteconfig6 tools: our scripts disappear from the performed list, the
// rest of it is kept in order, and nothing is written when there is
// nothing to drop.
func TestEnvScript_ReapplyDropsRecord(t *testing.T) {
	shells := []string{}
	for _, sh := range []string{"sh", "dash", "bash"} {
		if _, err := exec.LookPath(sh); err == nil {
			shells = append(shells, sh)
		}
	}
	if len(shells) == 0 {
		t.Skip("no POSIX shell available")
	}
	gid := uint32(os.Getgid()) //nolint:gosec // G115: test helper, real gid fits
	const nodeScript = "/etc/bor/share/late/plasma/shells/org.kde.plasma.desktop/contents/updates/bor-aaaa.js"
	const groupScript = "/etc/bor/share/groups/students/late/plasma/shells/org.kde.plasma.desktop/contents/updates/bor-bbbb.js"
	hook := EnvHook{ConfigPath: "/etc/bor/xdg", DataPath: "/etc/bor/share", Reapply: nodeScript, Groups: []GroupDir{
		{GID: gid, Slug: "students", Dir: "/etc/bor/xdg/groups/students", DataDir: "/etc/bor/share/groups/students", Layout: true, Reapply: groupScript},
		{GID: 4000000000, Slug: "others", Dir: "/etc/bor/xdg/groups/others", DataDir: "/etc/bor/share/groups/others", Layout: true, Reapply: "/etc/bor/share/groups/others/late/x/bor-cccc.js"},
	}}

	for _, sh := range shells {
		for _, tc := range []struct {
			name, performed, want string
		}{
			{"drops ours", "/usr/share/a.js," + nodeScript + ",/usr/share/b.js," + groupScript, "/usr/share/a.js,/usr/share/b.js"},
			{"keeps foreign groups", "/usr/share/a.js,/etc/bor/share/groups/others/late/x/bor-cccc.js", ""},
			{"nothing to drop", "/usr/share/a.js,/usr/share/b.js", ""},
			{"empty list", "", ""},
		} {
			t.Run(sh+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, "hook.sh"), []byte(envScriptContent(hook)))
				writeFile(t, filepath.Join(dir, "kreadconfig6"), []byte("#!/bin/sh\nprintf '%s' \""+tc.performed+"\"\n"))
				writeFile(t, filepath.Join(dir, "kwriteconfig6"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+filepath.Join(dir, "written")+"\"\n"))
				for _, f := range []string{"kreadconfig6", "kwriteconfig6"} {
					if err := os.Chmod(filepath.Join(dir, f), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command(sh, "-c", ". ./hook.sh && printf '%s' \"$XDG_DATA_DIRS\"") //nolint:gosec // G204: test shell
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "XDG_DATA_DIRS=/usr/share")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s failed: %v\n%s", sh, err, out)
				}
				// A member of a group with its own layout gets the group's
				// late directory instead of the node-wide one.
				if got := string(out); got != "/etc/bor/share/groups/students:/etc/bor/share:/usr/share:/etc/bor/share/groups/students/late" {
					t.Errorf("XDG_DATA_DIRS = %q", got)
				}
				written, err := os.ReadFile(filepath.Join(dir, "written"))
				switch {
				case tc.want == "" && err == nil:
					t.Errorf("kwriteconfig6 must not be called, got args:\n%s", written)
				case tc.want != "" && err != nil:
					t.Errorf("kwriteconfig6 should have been called: %v", err)
				case tc.want != "":
					args := strings.Split(strings.TrimRight(string(written), "\n"), "\n")
					if len(args) != 7 || args[len(args)-1] != tc.want || args[5] != "performed" {
						t.Errorf("kwriteconfig6 args = %q, want last %q", args, tc.want)
					}
				}
			})
		}
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
	dirs, unresolved := ResolveGroupDirs("/etc/bor/xdg", "/etc/bor/share", scopes, lookup)
	if len(dirs) != 1 || dirs[0].GID != 1002 || dirs[0].Dir != "/etc/bor/xdg/groups/teachers" || dirs[0].DataDir != "/etc/bor/share/groups/teachers" {
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
