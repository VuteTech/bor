// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeChromePolicies_SinglePolicy(t *testing.T) {
	contents := []string{
		`{"HomepageLocation": "https://example.com", "HomepageIsNewTabPage": false}`,
	}

	data, err := MergeChromePolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation to be https://example.com, got %v", result["HomepageLocation"])
	}
	if result["HomepageIsNewTabPage"] != false {
		t.Errorf("expected HomepageIsNewTabPage to be false, got %v", result["HomepageIsNewTabPage"])
	}
}

func TestMergeChromePolicies_MultiplePolicies(t *testing.T) {
	contents := []string{
		`{"HomepageLocation": "https://example.com"}`,
		`{"HomepageIsNewTabPage": false, "DefaultSearchProviderEnabled": true}`,
		`{"ExtensionSettings": {"abcdefghijklmnopabcdefghijklmnop": {"installation_mode": "force_installed"}}}`,
	}

	data, err := MergeChromePolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation, got %v", result["HomepageLocation"])
	}
	if result["HomepageIsNewTabPage"] != false {
		t.Errorf("expected HomepageIsNewTabPage false, got %v", result["HomepageIsNewTabPage"])
	}
	if result["DefaultSearchProviderEnabled"] != true {
		t.Errorf("expected DefaultSearchProviderEnabled true, got %v", result["DefaultSearchProviderEnabled"])
	}

	extSettings, ok := result["ExtensionSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected ExtensionSettings to be a map")
	}
	extEntry, ok := extSettings["abcdefghijklmnopabcdefghijklmnop"].(map[string]interface{})
	if !ok {
		t.Fatal("expected extension entry to be a map")
	}
	if extEntry["installation_mode"] != "force_installed" {
		t.Errorf("expected installation_mode force_installed, got %v", extEntry["installation_mode"])
	}
}

func TestMergeChromePolicies_EmptyInput(t *testing.T) {
	data, err := MergeChromePolicies(nil)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d keys", len(result))
	}
}

func TestMergeChromePolicies_NoCommentKey(t *testing.T) {
	contents := []string{`{"HomepageLocation": "https://example.com"}`}

	data, err := MergeChromePolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if _, hasComment := result["_comment"]; hasComment {
		t.Error("Chrome policy must not contain _comment key (Chrome logs warnings for unknown keys)")
	}
}

func TestSyncChromeDir_WritesFile(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	contents := []string{`{"HomepageLocation": "https://example.com"}`}

	if err := SyncChromeDir(managedDir, contents); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(managedDir, ChromeManagedFilename)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation, got %v", result["HomepageLocation"])
	}

	// Verify file permissions.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("expected permissions 0644, got %o", info.Mode().Perm())
	}
}

func TestSyncChromeDir_RemovesFileOnEmpty(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	// First write a file.
	contents := []string{`{"HomepageLocation": "https://example.com"}`}
	if err := SyncChromeDir(managedDir, contents); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(managedDir, ChromeManagedFilename)
	if _, err := os.Stat(target); err != nil {
		t.Fatal("expected file to exist after write")
	}

	// Now sync with empty contents — file should be removed.
	if err := SyncChromeDir(managedDir, nil); err != nil {
		t.Fatalf("SyncChromeDir with empty contents failed: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected bor_managed.json to be removed after empty sync")
	}
}

func TestSyncChromeDir_RemovesFileOnEmpty_NoErrorIfMissing(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	// Sync with empty contents when no file exists — should not error.
	if err := SyncChromeDir(managedDir, nil); err != nil {
		t.Fatalf("SyncChromeDir with empty contents and no existing file failed: %v", err)
	}
}

func TestSyncChromeDir_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	// Use a deeply nested directory that does not exist yet.
	managedDir := filepath.Join(dir, "etc", "opt", "chrome", "policies", "managed")

	contents := []string{`{"DefaultSearchProviderEnabled": true}`}
	if err := SyncChromeDir(managedDir, contents); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(managedDir, ChromeManagedFilename)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	// Verify directory permissions.
	info, err := os.Stat(managedDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("expected directory permissions 0755, got %o", info.Mode().Perm())
	}
}
