// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// writeFile is a test helper that writes data and fails the test on error.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}

func kcBool(v bool) *bool    { return &v }
func kcStr(v string) *string { return &v }
func kcInt(v int32) *int32   { return &v }

// findEntry returns the first entry matching file, group and key, or nil.
func findEntry(entries []*pb.KConfigEntry, file, group, key string) *pb.KConfigEntry {
	for _, e := range entries {
		if e.File == file && e.Group == group && e.Key == key {
			return e
		}
	}
	return nil
}

func TestKConfigPolicyToEntries_KeyNames(t *testing.T) {
	pol := &pb.KConfigPolicy{
		EnforcedFields:             []string{"shellAccess", "autoLock", "kcmRestrictions"},
		ShellAccess:                kcBool(false),
		RunCommand:                 kcBool(false),
		ActionLogout:               kcBool(false),
		PlasmoidUnlockedDesktop:    kcBool(false),
		AllowConfigureWhenLocked:   kcBool(false),
		BorderlessMaximizedWindows: kcBool(true),
		AutoLock:                   kcBool(true),
		LockOnResume:               kcBool(true),
		LockOnStart:                kcBool(true),
		LockTimeout:                kcInt(10),
		LockGrace:                  kcInt(0),
		LockScreenImage:            kcStr("/usr/share/backgrounds/lock.png"),
		IconTheme:                  kcStr("breeze"),
		KcmRestrictions:            []string{"kcm_autostart"},
		// Legacy resource restrictions must be ignored.
		RestrictWallpaper: kcBool(true), //nolint:staticcheck // SA1019: testing that the deprecated field is ignored
		RestrictAutostart: kcBool(true), //nolint:staticcheck // SA1019: testing that the deprecated field is ignored
	}

	entries := KConfigPolicyToEntries(pol)

	want := []struct{ file, group, key, value string }{
		{"kdeglobals", "KDE Action Restrictions", "shell_access", "false"},
		{"kdeglobals", "KDE Action Restrictions", "run_command", "false"},
		{"kdeglobals", "KDE Action Restrictions", "action/run_command", "false"},
		{"kdeglobals", "KDE Action Restrictions", "logout", "false"},
		{"kdeglobals", "KDE Action Restrictions", "action/logout", "false"},
		{"kdeglobals", "KDE Action Restrictions", "plasma/plasmashell/unlockedDesktop", "false"},
		{"kdeglobals", "KDE Action Restrictions", "plasma/allow_configure_when_locked", "false"},
		{"kwinrc", "Windows", "BorderlessMaximizedWindows", "true"},
		{"kscreenlockerrc", "Daemon", "Autolock", "true"},
		{"kscreenlockerrc", "Daemon", "LockOnResume", "true"},
		{"kscreenlockerrc", "Daemon", "LockOnStart", "true"},
		{"kscreenlockerrc", "Daemon", "Timeout", "10"},
		{"kscreenlockerrc", "Daemon", "LockGrace", "0"},
		{"kscreenlockerrc", "Greeter", "WallpaperPlugin", "org.kde.image"},
		{"kscreenlockerrc", "Greeter][Wallpaper][org.kde.image][General", "Image", "/usr/share/backgrounds/lock.png"},
		{"kdeglobals", "Icons", "Theme", "breeze"},
		{"kdeglobals", "KDE Control Module Restrictions", "kcm_autostart", "false"},
	}
	for _, w := range want {
		e := findEntry(entries, w.file, w.group, w.key)
		if e == nil {
			t.Errorf("missing entry %s [%s] %s", w.file, w.group, w.key)
			continue
		}
		if e.Value != w.value {
			t.Errorf("%s [%s] %s = %q, want %q", w.file, w.group, w.key, e.Value, w.value)
		}
	}

	// Enforcement follows enforcedFields; keys derived from one field share it.
	if e := findEntry(entries, "kdeglobals", "KDE Action Restrictions", "shell_access"); e == nil || !e.Enforced {
		t.Error("shell_access should be enforced")
	}
	if e := findEntry(entries, "kdeglobals", "KDE Action Restrictions", "action/logout"); e == nil || e.Enforced {
		t.Error("action/logout should not be enforced (actionLogout not in enforcedFields)")
	}
	if e := findEntry(entries, "kdeglobals", "KDE Control Module Restrictions", "kcm_autostart"); e == nil || !e.Enforced {
		t.Error("KCM restrictions are always enforced")
	}

	// Things that must NOT be emitted.
	for _, e := range entries {
		switch {
		case e.Group == "KDE Resource Restrictions":
			t.Errorf("legacy resource restriction emitted: %+v", e)
		case e.File == "plasmarc":
			t.Errorf("plasmarc must not be written (Plasma 6 reads the Kiosk action instead): %+v", e)
		case e.File == "kde5rc" || e.File == "kde6rc":
			t.Errorf("KCM restrictions belong in kdeglobals, got file %q", e.File)
		case e.Key == "AutoLock":
			t.Error("screen locker key must be spelled Autolock")
		}
	}
}

func TestKConfigPolicyToEntries_Nil(t *testing.T) {
	if got := KConfigPolicyToEntries(nil); got != nil {
		t.Errorf("expected nil for nil policy, got %v", got)
	}
}

func TestMergeKConfigPolicies_PriorityWins(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "low", Name: "Baseline", Priority: 10, Policy: &pb.KConfigPolicy{
			ShellAccess: kcBool(true), EnforcedFields: []string{"shellAccess"},
		}},
		{ID: "high", Name: "Lab lockdown", Priority: 50, Policy: &pb.KConfigPolicy{
			ShellAccess: kcBool(false), EnforcedFields: []string{"shellAccess"},
		}},
	}

	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}

	content := string(res.Files["kdeglobals"])
	if !strings.Contains(content, "shell_access[$i]=false") {
		t.Errorf("higher-priority value should win, got:\n%s", content)
	}
	if strings.Contains(content, "shell_access=true") || strings.Count(content, "shell_access") != 1 {
		t.Errorf("key must be written exactly once, got:\n%s", content)
	}

	high := res.Items["high"]
	if len(high) != 1 || high[0].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("winner should report one compliant item, got %+v", high)
	}
	low := res.Items["low"]
	if len(low) != 1 || low[0].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Fatalf("loser should report one inapplicable item, got %+v", low)
	}
	if !strings.Contains(low[0].Message, "Lab lockdown") || !strings.Contains(low[0].Message, "applied value: false") {
		t.Errorf("loser message should name the winner and the applied value, got %q", low[0].Message)
	}
	if low[0].SchemaId != "kdeglobals" || low[0].Key != "[KDE Action Restrictions] shell_access" {
		t.Errorf("unexpected item identity: %q / %q", low[0].SchemaId, low[0].Key)
	}
}

func TestMergeKConfigPolicies_TieBrokenByID(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "b-policy", Name: "B", Priority: 5, Policy: &pb.KConfigPolicy{IconTheme: kcStr("from-b")}},
		{ID: "a-policy", Name: "A", Priority: 5, Policy: &pb.KConfigPolicy{IconTheme: kcStr("from-a")}},
	}

	// Run twice with different input order: the result must not change.
	for _, order := range [][]KConfigPolicySource{sources, {sources[1], sources[0]}} {
		res, err := MergeKConfigPolicies(order, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(res.Files["kdeglobals"]), "Theme=from-a") {
			t.Errorf("tie should be broken by policy ID (a-policy wins), got:\n%s", res.Files["kdeglobals"])
		}
	}
}

func TestMergeKConfigPolicies_SameValueNotOverridden(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "one", Name: "One", Priority: 1, Policy: &pb.KConfigPolicy{RunCommand: kcBool(false), EnforcedFields: []string{"runCommand"}}},
		{ID: "two", Name: "Two", Priority: 2, Policy: &pb.KConfigPolicy{RunCommand: kcBool(false), EnforcedFields: []string{"runCommand"}}},
	}

	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		for _, it := range res.Items[id] {
			if it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
				t.Errorf("policy %s: identical values must both be compliant, got %+v", id, it)
			}
		}
	}
	content := string(res.Files["kdeglobals"])
	// Both derived keys appear exactly once (not twice, once per policy).
	if strings.Count(content, "\nrun_command[$i]=false\n") != 1 || strings.Count(content, "\naction/run_command[$i]=false\n") != 1 {
		t.Errorf("run_command and action/run_command should each appear once, got:\n%s", content)
	}
}

func TestMergeKConfigPolicies_URLRulesAdditive(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "a", Name: "A", Priority: 1, Policy: &pb.KConfigPolicy{UrlRestrictions: []*pb.KConfigUrlRestriction{
			{Action: "list", Protocol: "smb", Enabled: false},
			{Action: "open", Protocol: "ftp", Enabled: false},
		}}},
		{ID: "b", Name: "B", Priority: 9, Policy: &pb.KConfigPolicy{UrlRestrictions: []*pb.KConfigUrlRestriction{
			{Action: "open", Protocol: "file", Host: "", Path: "$HOME", Enabled: true},
		}}},
	}

	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := string(res.Files["kdeglobals"])
	if strings.Count(content, "rule_count[$i]=") != 1 || !strings.Contains(content, "rule_count[$i]=3") {
		t.Errorf("expected a single rule_count=3, got:\n%s", content)
	}
	for _, rule := range []string{"list,,,,smb,,,false", "open,,,,ftp,,,false", "open,,,,file,,$HOME,true"} {
		if !strings.Contains(content, rule) {
			t.Errorf("missing rule %q in:\n%s", rule, content)
		}
	}
	if len(res.Items["a"]) != 2 || len(res.Items["b"]) != 1 {
		t.Errorf("expected one item per rule, got a=%d b=%d", len(res.Items["a"]), len(res.Items["b"]))
	}
}

func TestMergeKConfigPolicies_RejectsLineBreaks(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "p", Name: "P", Priority: 1, Policy: &pb.KConfigPolicy{
			IconTheme:   kcStr("breeze\n[KDE Action Restrictions]\nshell_access=true"),
			ShellAccess: kcBool(false),
		}},
	}

	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := string(res.Files["kdeglobals"])
	if strings.Contains(content, "Theme=") || strings.Contains(content, "shell_access=true") {
		t.Errorf("value with line break must not be written, got:\n%s", content)
	}
	if !strings.Contains(content, "shell_access=false") {
		t.Errorf("valid entries must still be written, got:\n%s", content)
	}
	var sawError bool
	for _, it := range res.Items["p"] {
		if it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR && strings.Contains(it.Key, "Theme") {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("expected an error item for the rejected value, got %+v", res.Items["p"])
	}
}

func TestMergeKConfigPolicies_Empty(t *testing.T) {
	res, err := MergeKConfigPolicies(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != nil || len(res.Items) != 0 || len(res.Entries) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestMergeKConfigEntries_SinglePolicy(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Type: "bool", Enforced: true},
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "run_command", Value: "false", Type: "bool", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	data, ok := files["kdeglobals"]
	if !ok {
		t.Fatal("expected 'kdeglobals' file")
	}

	content := string(data)
	// Both entries are enforced: the markers sit on the keys, never on the group
	if !strings.Contains(content, "[KDE Action Restrictions]\n") {
		t.Errorf("expected the group header, got:\n%s", content)
	}
	if !strings.Contains(content, "run_command[$i]=false") {
		t.Errorf("expected run_command=false, got:\n%s", content)
	}
	if !strings.Contains(content, "shell_access[$i]=false") {
		t.Errorf("expected shell_access=false, got:\n%s", content)
	}
}

func TestMergeKConfigEntries_MultipleFiles(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Type: "bool", Enforced: true},
		{File: "kwinrc", Group: "Windows", Key: "BorderlessMaximizedWindows", Value: "true", Type: "bool", Enforced: false},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	if _, ok := files["kdeglobals"]; !ok {
		t.Error("expected 'kdeglobals' file")
	}
	if _, ok := files["kwinrc"]; !ok {
		t.Error("expected 'kwinrc' file")
	}
}

func TestMergeKConfigEntries_MixedEnforcement(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Type: "bool", Enforced: true},
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "run_command", Value: "true", Type: "bool", Enforced: false},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	data := files["kdeglobals"]
	content := string(data)

	// Mixed enforcement: group header should NOT have [$i]
	if strings.Contains(content, "[KDE Action Restrictions][$i]") {
		t.Errorf("expected no group-level [$i] for mixed enforcement, got:\n%s", content)
	}
	if !strings.Contains(content, "[KDE Action Restrictions]") {
		t.Errorf("expected group header, got:\n%s", content)
	}
	// Enforced key should have [$i]
	if !strings.Contains(content, "shell_access[$i]=false") {
		t.Errorf("expected key-level [$i] for enforced key, got:\n%s", content)
	}
	// Non-enforced key should NOT have [$i]
	if !strings.Contains(content, "run_command=true") {
		t.Errorf("expected plain key for non-enforced, got:\n%s", content)
	}
}

func TestMergeKConfigEntries_NoEnforcement(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kscreenlockerrc", Group: "Daemon", Key: "Timeout", Value: "300", Type: "int", Enforced: false},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	content := string(files["kscreenlockerrc"])
	if strings.Contains(content, "[$i]") {
		t.Errorf("expected no [$i] when not enforced, got:\n%s", content)
	}
	if !strings.Contains(content, "[Daemon]") {
		t.Errorf("expected [Daemon] group header, got:\n%s", content)
	}
	if !strings.Contains(content, "Timeout=300") {
		t.Errorf("expected Timeout=300, got:\n%s", content)
	}
}

func TestMergeKConfigEntries_EmptyInput(t *testing.T) {
	files, err := MergeKConfigEntries(nil)
	if err != nil {
		t.Fatal(err)
	}
	if files != nil {
		t.Errorf("expected nil for empty input, got %v", files)
	}
}

func TestMergeKConfigEntries_MultipleGroups(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Type: "bool", Enforced: true},
		{File: "kdeglobals", Group: "Icons", Key: "Theme", Value: "breeze", Type: "string", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	content := string(files["kdeglobals"])
	// Should have both groups
	if !strings.Contains(content, "[KDE Action Restrictions]\n") {
		t.Errorf("expected Action Restrictions group, got:\n%s", content)
	}
	if !strings.Contains(content, "[Icons]\n") {
		t.Errorf("expected Icons group, got:\n%s", content)
	}
	// Groups should be separated by a blank line
	if !strings.Contains(content, "\n\n[") {
		t.Errorf("expected blank line between groups, got:\n%s", content)
	}
}

func TestMergeKConfigEntries_NestedGroupHeader(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kscreenlockerrc", Group: "Greeter][Wallpaper][org.kde.image][General", Key: "Image", Value: "/srv/lock.png", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	content := string(files["kscreenlockerrc"])
	if !strings.Contains(content, "[Greeter][Wallpaper][org.kde.image][General]\nImage[$i]=/srv/lock.png\n") {
		t.Errorf("expected nested group header, got:\n%s", content)
	}
}

func TestBackupOriginal_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kdeglobals")
	original := []byte("[General]\nfoo=bar\n")
	writeFile(t, target, original)

	if err := BackupOriginal(target); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(target + BackupSuffix)
	if err != nil {
		t.Fatalf("backup not created: %v", err)
	}
	if !bytes.Equal(backup, original) {
		t.Errorf("backup content mismatch: got %q", backup)
	}

	// Second call should be idempotent - overwrite original and verify
	// backup still has old content.
	writeFile(t, target, []byte("overwritten"))
	if err := BackupOriginal(target); err != nil {
		t.Fatal(err)
	}
	backup, _ = os.ReadFile(target + BackupSuffix)
	if !bytes.Equal(backup, original) {
		t.Error("second BackupOriginal call overwrote existing backup")
	}
}

func TestBackupOriginal_NoOriginalFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nonexistent")

	if err := BackupOriginal(target); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(target + BackupSuffix)
	if err != nil {
		t.Fatalf("sentinel backup not created: %v", err)
	}
	if len(backup) != 0 {
		t.Errorf("expected empty sentinel, got %d bytes", len(backup))
	}
}

func TestRestoreOriginal_WithContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kdeglobals")
	original := []byte("[General]\nfoo=bar\n")

	// Simulate: backup exists with original content, managed file is different.
	writeFile(t, target+BackupSuffix, original)
	writeFile(t, target, []byte("managed content"))

	if err := RestoreOriginal(target); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Errorf("restored content mismatch: got %q", data)
	}

	// Backup should be removed.
	if _, err := os.Stat(target + BackupSuffix); !os.IsNotExist(err) {
		t.Error("backup file should be removed after restore")
	}
}

func TestRestoreOriginal_EmptySentinel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "newfile")

	// Simulate: empty sentinel backup, managed file exists.
	writeFile(t, target+BackupSuffix, nil)
	writeFile(t, target, []byte("managed content"))

	if err := RestoreOriginal(target); err != nil {
		t.Fatal(err)
	}

	// Managed file should be deleted (no original existed).
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("managed file should be deleted when backup is empty sentinel")
	}
	// Backup should be removed.
	if _, err := os.Stat(target + BackupSuffix); !os.IsNotExist(err) {
		t.Error("backup file should be removed after restore")
	}
}

func TestRestoreOriginal_NoBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nobackup")
	writeFile(t, target, []byte("untouched"))

	// Should be a no-op.
	if err := RestoreOriginal(target); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(target)
	if string(data) != "untouched" {
		t.Errorf("file should be untouched, got %q", data)
	}
}

func TestManagedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kdeglobals"+BackupSuffix), nil)
	writeFile(t, filepath.Join(dir, "kwinrc"+BackupSuffix), []byte("orig"))
	writeFile(t, filepath.Join(dir, "unrelated.conf"), []byte("x"))

	managed, err := ManagedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{"kdeglobals": true, "kwinrc": true}
	if len(managed) != len(expected) {
		t.Fatalf("expected %d managed files, got %d: %v", len(expected), len(managed), managed)
	}
	for _, name := range managed {
		if !expected[name] {
			t.Errorf("unexpected managed file: %s", name)
		}
	}
}

func TestSyncKConfigFiles_BackupAndHeader(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kdeglobals")
	original := []byte("[General]\noriginal=true\n")
	writeFile(t, target, original)

	files := map[string][]byte{
		"kdeglobals": []byte("[KDE Action Restrictions][$i]\nshell_access=false\n"),
	}

	if err := SyncKConfigFiles(dir, files); err != nil {
		t.Fatal(err)
	}

	// Backup should contain original content.
	backup, err := os.ReadFile(target + BackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Errorf("backup mismatch: got %q", backup)
	}

	// Managed file should start with header.
	data, _ := os.ReadFile(target)
	if !strings.HasPrefix(string(data), ManagedFileHeader) {
		t.Errorf("managed file should start with header, got:\n%s", data)
	}
	if !strings.Contains(string(data), "shell_access=false") {
		t.Errorf("managed file should contain policy content, got:\n%s", data)
	}
}

func TestSyncKConfigFiles_CleanupRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kdeglobals")
	original := []byte("[General]\noriginal=true\n")

	// First sync: write managed file.
	writeFile(t, target, original)
	files := map[string][]byte{
		"kdeglobals": []byte("[KDE Action Restrictions][$i]\nshell_access=false\n"),
	}
	if err := SyncKConfigFiles(dir, files); err != nil {
		t.Fatal(err)
	}

	// Second sync: empty map - should restore original.
	if err := SyncKConfigFiles(dir, nil); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Errorf("expected original content restored, got %q", data)
	}

	// Backup should be gone.
	if _, err := os.Stat(target + BackupSuffix); !os.IsNotExist(err) {
		t.Error("backup should be removed after cleanup")
	}
}

func TestSyncKConfigFiles_CleanupDeletesWhenNoOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "brandnew")

	// First sync: file didn't exist before.
	files := map[string][]byte{
		"brandnew": []byte("[Section]\nkey=value\n"),
	}
	if err := SyncKConfigFiles(dir, files); err != nil {
		t.Fatal(err)
	}

	// Verify file was created.
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("managed file should exist: %v", err)
	}

	// Second sync: empty map - should delete (no original).
	if err := SyncKConfigFiles(dir, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("managed file should be deleted when no original existed")
	}
	if _, err := os.Stat(target + BackupSuffix); !os.IsNotExist(err) {
		t.Error("backup should be removed after cleanup")
	}
}

func TestSyncKConfigFiles_PartialCleanup(t *testing.T) {
	dir := t.TempDir()

	// Write originals for two files.
	writeFile(t, filepath.Join(dir, "kdeglobals"), []byte("orig-kde"))
	writeFile(t, filepath.Join(dir, "kwinrc"), []byte("orig-kwin"))

	// First sync: manage both files.
	files := map[string][]byte{
		"kdeglobals": []byte("[G1]\nk1=v1\n"),
		"kwinrc":     []byte("[G2]\nk2=v2\n"),
	}
	if err := SyncKConfigFiles(dir, files); err != nil {
		t.Fatal(err)
	}

	// Second sync: only kdeglobals - kwinrc should be restored.
	files2 := map[string][]byte{
		"kdeglobals": []byte("[G1]\nk1=v1-updated\n"),
	}
	if err := SyncKConfigFiles(dir, files2); err != nil {
		t.Fatal(err)
	}

	// kdeglobals should still be managed.
	kdeData, _ := os.ReadFile(filepath.Join(dir, "kdeglobals"))
	if !strings.HasPrefix(string(kdeData), ManagedFileHeader) {
		t.Error("kdeglobals should still have managed header")
	}
	if !strings.Contains(string(kdeData), "k1=v1-updated") {
		t.Error("kdeglobals should have updated content")
	}

	// kwinrc should be restored to original.
	kwinData, _ := os.ReadFile(filepath.Join(dir, "kwinrc"))
	if string(kwinData) != "orig-kwin" {
		t.Errorf("kwinrc should be restored to original, got %q", kwinData)
	}

	// kwinrc backup should be gone.
	if _, err := os.Stat(filepath.Join(dir, "kwinrc"+BackupSuffix)); !os.IsNotExist(err) {
		t.Error("kwinrc backup should be removed")
	}

	// kdeglobals backup should still exist.
	if _, err := os.Stat(filepath.Join(dir, "kdeglobals"+BackupSuffix)); err != nil {
		t.Error("kdeglobals backup should still exist")
	}
}

func TestMergeKConfigEntries_URLRestrictionsSinglePolicy(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_count", Value: "2", Enforced: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_1", Value: "open,,,,http,example.com,,true", Enforced: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_2", Value: "list,,,,file,,,false", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	content := string(files["kdeglobals"])
	if !strings.Contains(content, "[KDE URL Restrictions]\n") {
		t.Errorf("expected the group header, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_1[$i]=open,,,,http,example.com,,true") {
		t.Errorf("expected rule_1, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_2[$i]=list,,,,file,,,false") {
		t.Errorf("expected rule_2, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_count[$i]=2") {
		t.Errorf("expected rule_count=2, got:\n%s", content)
	}
}

func TestMergeKConfigEntries_URLRestrictionsMultiplePolicies(t *testing.T) {
	// Simulate entries from two policies combined: both have rule_1 and rule_2.
	entries := []*pb.KConfigEntry{
		// Policy A
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_count", Value: "2", Enforced: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_1", Value: "open,,,,http,a.com,,true", Enforced: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_2", Value: "list,,,,https,a.com,,false", Enforced: true},
		// Policy B
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_count", Value: "1", Enforced: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_1", Value: "redirect,,,,file,b.com,,true", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	content := string(files["kdeglobals"])

	// Should have 3 rules renumbered sequentially.
	// rule_1 entries from both policies sort by original index (1),
	// stable sort preserves insertion order: a.com first, then b.com.
	if !strings.Contains(content, "rule_1[$i]=open,,,,http,a.com,,true") {
		t.Errorf("expected rule_1=a.com, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_2[$i]=redirect,,,,file,b.com,,true") {
		t.Errorf("expected rule_2=b.com, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_3[$i]=list,,,,https,a.com,,false") {
		t.Errorf("expected rule_3 from policy A rule_2, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_count[$i]=3") {
		t.Errorf("expected rule_count=3, got:\n%s", content)
	}

	// Should NOT contain duplicate rule_count entries.
	if strings.Count(content, "rule_count[$i]=") != 1 {
		t.Errorf("expected exactly one rule_count, got:\n%s", content)
	}
}

func TestMergeKConfigEntries_URLRestrictionsWithOtherGroups(t *testing.T) {
	entries := []*pb.KConfigEntry{
		// URL restriction rules
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_count", Value: "1", Enforced: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_1", Value: "open,,,,http,example.com,,true", Enforced: true},
		// Other group in same file
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	content := string(files["kdeglobals"])

	// URL restrictions should be present and correct.
	if !strings.Contains(content, "rule_1[$i]=open,,,,http,example.com,,true") {
		t.Errorf("expected URL restriction rule, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_count[$i]=1") {
		t.Errorf("expected rule_count=1, got:\n%s", content)
	}

	// Action restrictions should be unaffected.
	if !strings.Contains(content, "[KDE Action Restrictions]\n") {
		t.Errorf("expected Action Restrictions group, got:\n%s", content)
	}
	if !strings.Contains(content, "shell_access[$i]=false") {
		t.Errorf("expected shell_access=false, got:\n%s", content)
	}
}

func TestKCMRestrictionsForKde5rc(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Control Module Restrictions", Key: "kcm_access", Value: "false", Enforced: true},
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Enforced: true},
		{File: "kdeglobals", Group: "KDE Control Module Restrictions", Key: "kcm_bluetooth", Value: "false", Enforced: true},
		{File: "kwinrc", Group: "Windows", Key: "BorderlessMaximizedWindows", Value: "true", Enforced: false},
	}

	kcm := KCMRestrictionsForKde5rc(entries)
	if len(kcm) != 2 {
		t.Fatalf("expected 2 KCM entries, got %d", len(kcm))
	}
	for _, e := range kcm {
		if e.File != "kde5rc" || e.Group != "KDE Control Module Restrictions" || e.Value != "false" || !e.Enforced {
			t.Errorf("unexpected kde5rc entry: %+v", e)
		}
	}
	// The originals are untouched (copies were made).
	if entries[0].File != "kdeglobals" {
		t.Error("source entries must not be modified")
	}

	if got := KCMRestrictionsForKde5rc(nil); len(got) != 0 {
		t.Errorf("expected no entries for nil input, got %d", len(got))
	}
}

// useTempKde5rc points the /etc rc paths at a temp dir for the test.
func useTempKde5rc(t *testing.T) (kde5, kde6 string) {
	t.Helper()
	dir := t.TempDir()
	orig5, orig6 := kde5rcPath, legacyKde6rcPath
	kde5rcPath = filepath.Join(dir, "kde5rc")
	legacyKde6rcPath = filepath.Join(dir, "kde6rc")
	t.Cleanup(func() { kde5rcPath, legacyKde6rcPath = orig5, orig6 })
	return kde5rcPath, legacyKde6rcPath
}

func TestSyncKde5rc_WriteAndRestore(t *testing.T) {
	kde5, kde6 := useTempKde5rc(t)
	content := []byte("[KDE Control Module Restrictions][$i]\nkcm_access=false\n")

	if err := SyncKde5rc(content); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(kde5)
	if err != nil {
		t.Fatalf("expected kde5rc to exist: %v", err)
	}
	if !strings.HasPrefix(string(data), ManagedFileHeader) || !strings.Contains(string(data), "kcm_access=false") {
		t.Errorf("kde5rc should have managed header and content, got:\n%s", data)
	}
	if _, err := os.Stat(kde6); !os.IsNotExist(err) {
		t.Error("kde6rc must never be written")
	}

	// Restore (nil content): removed because no original existed.
	if err := SyncKde5rc(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kde5); !os.IsNotExist(err) {
		t.Error("kde5rc should be removed after restore with no original")
	}
}

func TestSyncKde5rc_PreservesOriginal(t *testing.T) {
	kde5, _ := useTempKde5rc(t)
	writeFile(t, kde5, []byte("[General]\noriginal=true\n"))

	if err := SyncKde5rc([]byte("[KDE Control Module Restrictions][$i]\nkcm_access=false\n")); err != nil {
		t.Fatal(err)
	}
	if err := SyncKde5rc(nil); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(kde5)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[General]\noriginal=true\n" {
		t.Errorf("expected original content restored, got %q", data)
	}
}

func TestSyncKde5rc_RestoresLegacyKde6rc(t *testing.T) {
	_, kde6 := useTempKde5rc(t)

	// An older agent wrote kde6rc (sentinel backup: it did not exist before).
	writeFile(t, kde6+BackupSuffix, nil)
	writeFile(t, kde6, []byte(ManagedFileHeader+"[KDE Control Module Restrictions][$i]\nkcm_access=false\n"))

	// Any sync, even one that writes nothing, cleans it up.
	if err := SyncKde5rc(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kde6); !os.IsNotExist(err) {
		t.Error("legacy kde6rc should be removed")
	}
	if _, err := os.Stat(kde6 + BackupSuffix); !os.IsNotExist(err) {
		t.Error("legacy kde6rc backup should be removed")
	}

	// A distro-shipped kde6rc without a Bor backup is left alone.
	writeFile(t, kde6, []byte("[General]\ndistro=true\n"))
	if err := SyncKde5rc(nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(kde6); string(data) != "[General]\ndistro=true\n" {
		t.Errorf("unmanaged kde6rc must not be touched, got %q", data)
	}
}

func TestEnvScriptContent(t *testing.T) {
	got := envScriptContent("/etc/bor/xdg", "/etc/bor/share", nil)
	if !strings.Contains(got, `export XDG_CONFIG_DIRS="/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}"`) {
		t.Errorf("expected export line, got:\n%s", got)
	}
	if strings.Contains(got, "readonly") {
		t.Errorf("readonly breaks session scripts and must not be used, got:\n%s", got)
	}
	// The guard makes running both hooks idempotent.
	if !strings.Contains(got, `*":/etc/bor/xdg:"*) ;;`) {
		t.Errorf("expected idempotency guard, got:\n%s", got)
	}
}

func TestEnsureEnvScripts(t *testing.T) {
	dir := t.TempDir()
	origProfile, origPlasma, origLegacy := profileScriptPath, plasmaEnvScriptPath, legacyPlasmaEnvScriptPath
	profileScriptPath = filepath.Join(dir, "profile.d", "99-bor.sh")
	plasmaEnvScriptPath = filepath.Join(dir, "xdg", "plasma-workspace", "env", "zz-bor.sh")
	legacyPlasmaEnvScriptPath = filepath.Join(dir, "xdg", "plasma-workspace", "env", "00-bor.sh")
	t.Cleanup(func() {
		profileScriptPath, plasmaEnvScriptPath, legacyPlasmaEnvScriptPath = origProfile, origPlasma, origLegacy
	})

	// The hook name of earlier releases goes away on the first sync.
	if err := os.MkdirAll(filepath.Dir(legacyPlasmaEnvScriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, legacyPlasmaEnvScriptPath, []byte("# old hook\n"))

	if err := EnsureEnvScripts("/etc/bor/xdg", "/etc/bor/share", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPlasmaEnvScriptPath); !os.IsNotExist(err) {
		t.Errorf("legacy hook should be removed, stat err = %v", err)
	}

	want := map[string]string{
		profileScriptPath:   profileScriptContent("/etc/bor/xdg", "/etc/bor/share", nil),
		plasmaEnvScriptPath: envScriptContent("/etc/bor/xdg", "/etc/bor/share", nil),
	}
	for _, path := range EnvScriptPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s should exist: %v", path, err)
		}
		if string(data) != want[path] {
			t.Errorf("%s content mismatch:\n%s", path, data)
		}
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("%s should be 0755, got %o", path, fi.Mode().Perm())
		}
	}

	// A second run with the same path is a no-op; a changed path rewrites.
	if err := EnsureEnvScripts("/etc/bor/xdg", "/etc/bor/share", nil); err != nil {
		t.Fatal(err)
	}
	if err := EnsureEnvScripts("/opt/bor/xdg", "/opt/bor/share", nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(profileScriptPath)
	if !strings.Contains(string(data), "/opt/bor/xdg") {
		t.Errorf("script should be rewritten for the new path, got:\n%s", data)
	}
}
