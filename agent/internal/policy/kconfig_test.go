// SPDX-License-Identifier: LGPL-3.0-or-later
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

// writeFile is a test helper that writes data and fails the test on error.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
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
	// Both entries are enforced, so group header should have [$i]
	if !strings.Contains(content, "[KDE Action Restrictions][$i]") {
		t.Errorf("expected group-level [$i] enforcement, got:\n%s", content)
	}
	if !strings.Contains(content, "run_command=false") {
		t.Errorf("expected run_command=false, got:\n%s", content)
	}
	if !strings.Contains(content, "shell_access=false") {
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

func TestMergeKConfigEntries_MultiplePoliciesMerged(t *testing.T) {
	// Entries from two different policies combined into a single slice
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Type: "bool", Enforced: true},
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "run_command", Value: "false", Type: "bool", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	data := files["kdeglobals"]
	content := string(data)

	if !strings.Contains(content, "shell_access=false") {
		t.Errorf("expected shell_access from first policy, got:\n%s", content)
	}
	if !strings.Contains(content, "run_command=false") {
		t.Errorf("expected run_command from second policy, got:\n%s", content)
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
		{File: "kdeglobals", Group: "KDE Resource Restrictions", Key: "wallpaper", Value: "false", Type: "bool", Enforced: true},
	}

	files, err := MergeKConfigEntries(entries)
	if err != nil {
		t.Fatal(err)
	}

	content := string(files["kdeglobals"])
	// Should have both groups
	if !strings.Contains(content, "[KDE Action Restrictions][$i]") {
		t.Errorf("expected Action Restrictions group, got:\n%s", content)
	}
	if !strings.Contains(content, "[KDE Resource Restrictions][$i]") {
		t.Errorf("expected Resource Restrictions group, got:\n%s", content)
	}
	// Groups should be separated by a blank line
	if !strings.Contains(content, "\n\n[") {
		t.Errorf("expected blank line between groups, got:\n%s", content)
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
	if string(backup) != string(original) {
		t.Errorf("backup content mismatch: got %q", backup)
	}

	// Second call should be idempotent — overwrite original and verify
	// backup still has old content.
	writeFile(t, target, []byte("overwritten"))
	if err := BackupOriginal(target); err != nil {
		t.Fatal(err)
	}
	backup, _ = os.ReadFile(target + BackupSuffix)
	if string(backup) != string(original) {
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
	if string(data) != string(original) {
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
	if string(backup) != string(original) {
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

	// Second sync: empty map — should restore original.
	if err := SyncKConfigFiles(dir, nil); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
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

	// Second sync: empty map — should delete (no original).
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

	// Second sync: only kdeglobals — kwinrc should be restored.
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
	if !strings.Contains(content, "[KDE URL Restrictions][$i]") {
		t.Errorf("expected group header with [$i], got:\n%s", content)
	}
	if !strings.Contains(content, "rule_1=open,,,,http,example.com,,true") {
		t.Errorf("expected rule_1, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_2=list,,,,file,,,false") {
		t.Errorf("expected rule_2, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_count=2") {
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
	if !strings.Contains(content, "rule_1=open,,,,http,a.com,,true") {
		t.Errorf("expected rule_1=a.com, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_2=redirect,,,,file,b.com,,true") {
		t.Errorf("expected rule_2=b.com, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_3=list,,,,https,a.com,,false") {
		t.Errorf("expected rule_3 from policy A rule_2, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_count=3") {
		t.Errorf("expected rule_count=3, got:\n%s", content)
	}

	// Should NOT contain duplicate rule_count entries.
	if strings.Count(content, "rule_count=") != 1 {
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
	if !strings.Contains(content, "rule_1=open,,,,http,example.com,,true") {
		t.Errorf("expected URL restriction rule, got:\n%s", content)
	}
	if !strings.Contains(content, "rule_count=1") {
		t.Errorf("expected rule_count=1, got:\n%s", content)
	}

	// Action restrictions should be unaffected.
	if !strings.Contains(content, "[KDE Action Restrictions][$i]") {
		t.Errorf("expected Action Restrictions group, got:\n%s", content)
	}
	if !strings.Contains(content, "shell_access=false") {
		t.Errorf("expected shell_access=false, got:\n%s", content)
	}
}

func TestProfileScriptContent(t *testing.T) {
	got := profileScriptContent("/etc/bor/xdg")
	want := "export XDG_CONFIG_DIRS=/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}\nreadonly XDG_CONFIG_DIRS\n"
	if got != want {
		t.Errorf("profileScriptContent mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
