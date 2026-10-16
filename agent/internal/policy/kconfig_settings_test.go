// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestKConfigPolicyToEntries_GenericSettings(t *testing.T) {
	pol := &pb.KConfigPolicy{Settings: []*pb.KConfigSetting{
		{File: "dolphinrc", GroupPath: []string{"General"}, Key: "ShowFullPath", Value: "true", Immutable: true, CatalogueId: "dolphin.ShowFullPath"},
		{File: "kscreenlockerrc", GroupPath: []string{"Greeter", "Wallpaper", "org.kde.image", "General"}, Key: "Image", Value: "$HOME/lock.png", Expand: true},
		{File: "kdeglobals", GroupPath: []string{"General"}, Key: "ColorScheme", Delete: true},
		{File: "kdeglobals", GroupPath: []string{"General"}, Key: "Name", Value: "Firma", Locale: "de"},
	}}

	entries := KConfigPolicyToEntries(pol)

	if e := findEntry(entries, "dolphinrc", "General", "ShowFullPath"); e == nil || !e.Enforced || e.Value != "true" {
		t.Errorf("ShowFullPath entry wrong: %+v", e)
	}
	if e := findEntry(entries, "kscreenlockerrc", "Greeter][Wallpaper][org.kde.image][General", "Image"); e == nil || !e.Expand || e.Enforced {
		t.Errorf("nested group / expand entry wrong: %+v", e)
	}
	if e := findEntry(entries, "kdeglobals", "General", "ColorScheme"); e == nil || !e.Delete {
		t.Errorf("delete entry wrong: %+v", e)
	}
	if e := findEntry(entries, "kdeglobals", "General", "Name"); e == nil || e.Locale != "de" {
		t.Errorf("locale entry wrong: %+v", e)
	}
}

func TestMergeKConfigEntries_Markers(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "General", Key: "ColorScheme", Value: "BreezeDark", Enforced: true},
		{File: "kdeglobals", Group: "General", Key: "Wallpaper", Value: "$HOME/bg.png", Enforced: true, Expand: true},
		{File: "kdeglobals", Group: "General", Key: "Obsolete", Delete: true},
		{File: "kdeglobals", Group: "General", Key: "Name", Value: "Firma", Locale: "de"},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	// Mixed enforcement: key-level markers, sorted by key.
	want := "[General]\nColorScheme[$i]=BreezeDark\nName[de]=Firma\nObsolete[$d]\nWallpaper[$ie]=$HOME/bg.png\n"
	if got := string(files["kdeglobals"]); got != want {
		t.Errorf("rendered file mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestMergeKConfigEntries_ImmutabilityStaysOnKeys(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "General", Key: "A", Value: "1", Enforced: true},
		{File: "kdeglobals", Group: "General", Key: "B", Value: "$X", Enforced: true, Expand: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	want := "[General]\nA[$i]=1\nB[$ie]=$X\n"
	if got := string(files["kdeglobals"]); got != want {
		t.Errorf("rendered file mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestMergeKConfigPolicies_ShowOnlyKCMs(t *testing.T) {
	src := KConfigPolicySource{ID: "p", Name: "Kiosk", Priority: 1, Policy: &pb.KConfigPolicy{
		KcmMode:         pb.KcmRestrictionMode_KCM_SHOW_ONLY_LISTED,
		KcmRestrictions: []string{"kcm_access", "kcm_users"},
	}}
	installed := []string{"kcm_access", "kcm_autostart", "kcm_bluetooth", "kcm_users"}

	res, err := MergeKConfigPolicies([]KConfigPolicySource{src}, installed)
	if err != nil {
		t.Fatal(err)
	}
	content := string(res.Files["kdeglobals"])
	for _, line := range []string{"kcm_access[$i]=true", "kcm_users[$i]=true", "kcm_autostart[$i]=false", "kcm_bluetooth[$i]=false"} {
		if !strings.Contains(content, "\n"+line+"\n") {
			t.Errorf("missing %q in:\n%s", line, content)
		}
	}
	items := res.Items["p"]
	if len(items) != 1 || items[0].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT || !strings.Contains(items[0].Message, "Hiding 2 of 4") {
		t.Errorf("expected one compliant summary item, got %+v", items)
	}

	// Without the installed list the mode cannot be applied.
	res, err = MergeKConfigPolicies([]KConfigPolicySource{src}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != nil {
		t.Errorf("nothing should be written without an installed module list, got %v", res.Files)
	}
	items = res.Items["p"]
	if len(items) != 1 || items[0].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Errorf("expected an error item, got %+v", items)
	}
}

func TestMergeKConfigPolicies_ShowOnlyWinsOverLowerHide(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "hide", Name: "Hide", Priority: 1, Policy: &pb.KConfigPolicy{KcmRestrictions: []string{"kcm_access"}}},
		{ID: "show", Name: "Show", Priority: 9, Policy: &pb.KConfigPolicy{
			KcmMode:         pb.KcmRestrictionMode_KCM_SHOW_ONLY_LISTED,
			KcmRestrictions: []string{"kcm_access"},
		}},
	}

	res, err := MergeKConfigPolicies(sources, []string{"kcm_access", "kcm_users"})
	if err != nil {
		t.Fatal(err)
	}
	content := string(res.Files["kdeglobals"])
	if strings.Count(content, "kcm_access[$i]=") != 1 || !strings.Contains(content, "kcm_access[$i]=true") {
		t.Errorf("higher-priority allow list should win, got:\n%s", content)
	}
	if !strings.Contains(content, "kcm_users[$i]=false") {
		t.Errorf("unlisted module should be hidden, got:\n%s", content)
	}
}

func TestMergeKConfigPolicies_FileImmutableKdeglobals(t *testing.T) {
	res, err := MergeKConfigPolicies([]KConfigPolicySource{{ID: "p", Name: "P", Priority: 1, Policy: &pb.KConfigPolicy{
		FileImmutableKdeglobals: true,
		ShellAccess:             kcBool(false),
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := string(res.Files["kdeglobals"])
	if !strings.HasPrefix(content, "[$i]\n\n[KDE Action Restrictions]") {
		t.Errorf("expected file-level [$i] before the first group, got:\n%s", content)
	}
	var sawItem bool
	for _, it := range res.Items["p"] {
		if it.Key == "[$i]" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
			sawItem = true
		}
	}
	if !sawItem {
		t.Errorf("expected a compliance item for the file marker, got %+v", res.Items["p"])
	}

	// The marker alone still produces a (locking) kdeglobals.
	res, err = MergeKConfigPolicies([]KConfigPolicySource{{ID: "p", Name: "P", Priority: 1, Policy: &pb.KConfigPolicy{FileImmutableKdeglobals: true}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.Files["kdeglobals"]); got != "[$i]\n\n" {
		t.Errorf("expected bare marker file, got %q", got)
	}
}

func TestMergeKConfigPolicies_InvalidSettingReported(t *testing.T) {
	res, err := MergeKConfigPolicies([]KConfigPolicySource{{ID: "p", Name: "P", Priority: 1, Policy: &pb.KConfigPolicy{Settings: []*pb.KConfigSetting{
		{File: "../etc/passwd", GroupPath: []string{"General"}, Key: "x", Value: "1"},
		{File: "kdeglobals", GroupPath: []string{"General"}, Key: "Name", Value: "x", Locale: "DE-de"},
		{File: "kdeglobals", GroupPath: []string{"General"}, Key: "Ok", Value: "1"},
	}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || !strings.Contains(string(res.Files["kdeglobals"]), "Ok=1") {
		t.Errorf("only the valid entry should be written, got %v", res.Files)
	}
	errors := 0
	for _, it := range res.Items["p"] {
		if it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
			errors++
		}
	}
	if errors != 2 {
		t.Errorf("expected 2 error items, got %+v", res.Items["p"])
	}
}

func TestInstalledKCMsFrom(t *testing.T) {
	dir := t.TempDir()
	apps := filepath.Join(dir, "applications")
	plugins := filepath.Join(dir, "plugins", "plasma", "kcms", "systemsettings")
	for _, d := range []string{apps, plugins} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"kcm_access.desktop", "kcm_users.desktop", "org.kde.dolphin.desktop"} {
		writeFile(t, filepath.Join(apps, f), []byte("[Desktop Entry]\n"))
	}
	for _, f := range []string{"kcm_bluetooth.so", "kcm_access.so", "notakcm.so"} {
		writeFile(t, filepath.Join(plugins, f), []byte{0})
	}

	got := InstalledKCMsFrom([]string{apps, filepath.Join(dir, "missing")}, []string{filepath.Join(dir, "plugins", "plasma", "kcms", "*", "*.so")})
	want := []string{"kcm_access", "kcm_bluetooth", "kcm_users"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("InstalledKCMsFrom = %v, want %v", got, want)
	}
}
