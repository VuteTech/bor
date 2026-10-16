// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestKonsoleProfileName(t *testing.T) {
	cases := map[string]string{"": "Managed", "  ": "Managed", "Lab Shell": "Lab Shell", "../etc/passwd": "etcpasswd", "Dev-1.0": "Dev-1.0", "..": "Managed", "Tëst": "Tst"}
	for in, want := range cases {
		if got := KonsoleProfileName(&pb.KonsoleProfile{Name: in}); got != want {
			t.Errorf("KonsoleProfileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderKonsoleProfile(t *testing.T) {
	p := &pb.KonsoleProfile{
		Name:        "Managed",
		Command:     "/bin/bash --login",
		HistoryMode: "1",
		HistorySize: 2000,
		ColorScheme: "Breeze",
		Font:        "Hack,10,-1,5,50,0,0,0,0,0",
		ExtraIni:    "\n[Terminal Features]\nBlinkingCursorEnabled=false\nbroken line without equals\n[General]\nName=Should not break\n",
	}
	got := string(RenderKonsoleProfile(p, "Lab terminal"))
	for _, want := range []string{
		"# Konsole profile of policy Lab terminal.",
		"[General]\nName=Managed\nParent=FALLBACK/\nCommand=/bin/bash --login\n",
		"[Scrolling]\nHistoryMode=1\nHistorySize=2000\n",
		"[Appearance]\nColorScheme=Breeze\nFont=Hack,10,-1,5,50,0,0,0,0,0\n",
		"[Terminal Features]\nBlinkingCursorEnabled=false\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("profile lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "broken line") {
		t.Errorf("invalid extra INI line must be dropped:\n%s", got)
	}

	none := string(RenderKonsoleProfile(&pb.KonsoleProfile{HistoryMode: "0"}, ""))
	if !strings.Contains(none, "HistoryMode=0") || strings.Contains(none, "HistorySize") || strings.Contains(none, "[Appearance]") {
		t.Errorf("no-history profile wrong:\n%s", none)
	}
	if strings.Contains(none, "policy") {
		t.Errorf("header without a policy name must not mention one:\n%s", none)
	}
}

func TestKonsoleProfileEntries(t *testing.T) {
	if got := KonsoleProfileEntries(&pb.KonsoleProfile{Command: "/bin/sh"}); len(got) != 0 {
		t.Errorf("a profile without set_default or lock selects nothing, got %v", got)
	}
	def := KonsoleProfileEntries(&pb.KonsoleProfile{Name: "Lab", SetDefault: true})
	if len(def) != 1 || def[0].File != "konsolerc" || def[0].Group != "Desktop Entry" || def[0].Key != "DefaultProfile" || def[0].Value != "Lab.profile" || def[0].Enforced {
		t.Errorf("set_default entries = %+v", def)
	}
	locked := KonsoleProfileEntries(&pb.KonsoleProfile{Lock: true})
	if len(locked) != 4 || !locked[0].Enforced || locked[0].Value != "Managed.profile" {
		t.Fatalf("locked entries = %+v", locked)
	}
	for _, e := range locked[1:] {
		if e.File != KConfigFileGlobals || e.Group != KConfigGroupActionRestrictions || !strings.HasPrefix(e.Key, "action/") || e.Value != "false" || !e.Enforced {
			t.Errorf("lock restriction wrong: %+v", e)
		}
	}
}

func TestHideDesktopEntry(t *testing.T) {
	original := "[Desktop Entry]\nType=Application\nName=Konsole\nNoDisplay=false\nExec=konsole\nMimeType=x/y;\n\n[Desktop Action NewWindow]\nName=Open a New Window\nExec=konsole\n"
	got := string(HideDesktopEntry("org.kde.konsole.desktop", []byte(original)))
	if !strings.HasPrefix(got, "# Managed by Bor.") {
		t.Errorf("missing header:\n%s", got)
	}
	if !strings.Contains(got, "[Desktop Entry]\nNoDisplay=true\nType=Application\n") {
		t.Errorf("NoDisplay must follow the main group header:\n%s", got)
	}
	if strings.Count(got, "NoDisplay=") != 1 {
		t.Errorf("the original NoDisplay line must be dropped:\n%s", got)
	}
	if !strings.Contains(got, "Exec=konsole\nMimeType=x/y;") || !strings.Contains(got, "[Desktop Action NewWindow]\nName=Open a New Window\nExec=konsole\n") {
		t.Errorf("the rest of the entry must be kept:\n%s", got)
	}

	stub := string(HideDesktopEntry("org.example.gone.desktop", nil))
	for _, want := range []string{"[Desktop Entry]", "Type=Application", "Name=org.example.gone", "Exec=true", "NoDisplay=true"} {
		if !strings.Contains(stub, want) {
			t.Errorf("stub lacks %q:\n%s", want, stub)
		}
	}
}

func TestRenderAppFiles(t *testing.T) {
	lookup := func(id string) []byte {
		if id == "org.kde.konsole.desktop" {
			return []byte("[Desktop Entry]\nType=Application\nName=Konsole\nExec=konsole\n")
		}
		return nil
	}
	files := RenderAppFiles(KConfigAppFiles{
		KonsoleProfile:       &pb.KonsoleProfile{Name: "Lab", HistoryMode: "0"},
		KonsoleProfilePolicy: "P",
		HiddenApplications:   []string{"org.kde.konsole.desktop", "org.kde.kate.desktop", "../evil.desktop"},
	}, lookup)
	if len(files) != 3 {
		t.Fatalf("expected profile + 2 desktop entries, got %v", fileKeys(files))
	}
	if _, ok := files["konsole/Lab.profile"]; !ok {
		t.Errorf("profile path wrong: %v", fileKeys(files))
	}
	if !strings.Contains(string(files["applications/org.kde.konsole.desktop"]), "Name=Konsole") {
		t.Error("installed entry must be copied")
	}
	if !strings.Contains(string(files["applications/org.kde.kate.desktop"]), "Exec=true") {
		t.Error("missing entry must get a stub")
	}
	if len(RenderAppFiles(KConfigAppFiles{}, lookup)) != 0 {
		t.Error("empty scope renders nothing")
	}
}

func TestMergeKConfigPolicies_AppFiles(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "a", Name: "Terminal lockdown", Priority: 9, Policy: &pb.KConfigPolicy{
			KonsoleProfile:     &pb.KonsoleProfile{Name: "Managed", HistoryMode: "0", Lock: true},
			HiddenApplications: []string{"org.kde.kmenuedit.desktop", "systemsettings.desktop"},
		}},
		{ID: "b", Name: "Developer profile", Priority: 3, Policy: &pb.KConfigPolicy{
			KonsoleProfile:     &pb.KonsoleProfile{Name: "Dev", HistoryMode: "2", SetDefault: true},
			HiddenApplications: []string{"systemsettings.desktop", "bad id", "org.kde.ksystemlog.desktop"},
		}},
		{ID: "g", Name: "Students", Priority: 5, Groups: []string{"students"}, Policy: &pb.KConfigPolicy{
			HiddenApplications: []string{"org.kde.konsole.desktop"},
			KonsoleProfile:     &pb.KonsoleProfile{Name: "Student", Command: "/bin/rbash", SetDefault: true},
		}},
	}
	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Node scope: the priority-9 profile wins and is locked; hidden ids are
	// combined, de-duplicated and keep priority order.
	if res.Apps.KonsoleProfile == nil || KonsoleProfileName(res.Apps.KonsoleProfile) != "Managed" || res.Apps.KonsoleProfilePolicy != "Terminal lockdown" {
		t.Errorf("node Konsole profile = %+v", res.Apps)
	}
	if strings.Join(res.Apps.HiddenApplications, ",") != "org.kde.kmenuedit.desktop,systemsettings.desktop,org.kde.ksystemlog.desktop" {
		t.Errorf("hidden applications = %v", res.Apps.HiddenApplications)
	}
	globals := string(res.Files["kdeglobals"])
	if !strings.Contains(globals, "action/manage-profiles=false") || !strings.Contains(string(res.Files["konsolerc"]), "DefaultProfile=Managed.profile") {
		t.Errorf("locked profile entries missing:\nkdeglobals:\n%s\nkonsolerc:\n%s", globals, res.Files["konsolerc"])
	}

	// Group scope has its own profile and hidden list.
	if g := res.GroupApps["students"]; g.KonsoleProfile == nil || KonsoleProfileName(g.KonsoleProfile) != "Student" || strings.Join(g.HiddenApplications, ",") != "org.kde.konsole.desktop" {
		t.Errorf("students apps = %+v", g)
	}
	if !strings.Contains(string(res.GroupFiles["students"]["konsolerc"]), "DefaultProfile=Student.profile") {
		t.Errorf("students konsolerc wrong:\n%s", res.GroupFiles["students"]["konsolerc"])
	}

	// Items: the losing profile is inapplicable, its DefaultProfile entry is
	// overridden, the duplicate hidden id is still compliant, the bad id errors.
	var profileLost, defaultOverridden, dupOK, badErr bool
	for _, it := range res.Items["b"] {
		switch {
		case it.SchemaId == "konsole/Dev.profile" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE && strings.Contains(it.Message, "Terminal lockdown"):
			profileLost = true
		case it.SchemaId == "applications/systemsettings.desktop" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT && strings.Contains(it.Message, "also by"):
			dupOK = true
		case it.SchemaId == "applications/bad id" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			badErr = true
		}
	}
	if !profileLost || !dupOK || !badErr {
		t.Errorf("items of b wrong: lost=%v dup=%v bad=%v\n%+v", profileLost, dupOK, badErr, res.Items["b"])
	}
	// The losing profile never claimed DefaultProfile, so no override item
	// exists for it; the winner's entries are compliant.
	for _, it := range res.Items["a"] {
		if it.Key == "[Desktop Entry] DefaultProfile" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
			defaultOverridden = true
		}
	}
	if !defaultOverridden {
		t.Errorf("winner's DefaultProfile item missing: %+v", res.Items["a"])
	}
}

func TestDataChangeClassification(t *testing.T) {
	if !LayoutFilesChanged([]string{"/etc/bor/share/plasma/look-and-feel/org.bor.x.desktop/contents/defaults"}) || LayoutFilesChanged([]string{"/etc/bor/share/konsole/Managed.profile"}) {
		t.Error("LayoutFilesChanged wrong")
	}
	if !AppFilesChanged([]string{"/etc/bor/share/groups/students/applications/org.kde.konsole.desktop"}) || AppFilesChanged([]string{"/etc/bor/share/plasma/shells/x/contents/updates/bor-1.js"}) {
		t.Error("AppFilesChanged wrong")
	}
}

func fileKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
