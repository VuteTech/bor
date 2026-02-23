// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestSyncChromeFromProto_SinglePolicy(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	homepageIsNewTab := false
	pol := &pb.ChromePolicy{
		HomepageLocation:    &homepageLoc,
		HomepageIsNewTabPage: &homepageIsNewTab,
	}

	if err := SyncChromeFromProto([]*pb.ChromePolicy{pol}, []string{managedDir}); err != nil {
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
		t.Errorf("expected HomepageLocation to be https://example.com, got %v", result["HomepageLocation"])
	}
	if result["HomepageIsNewTabPage"] != false {
		t.Errorf("expected HomepageIsNewTabPage to be false, got %v", result["HomepageIsNewTabPage"])
	}
}

func TestSyncChromeFromProto_MultiplePolicies(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	homepageIsNewTab := false
	searchEnabled := true

	extMap, err := structpb.NewValue(map[string]interface{}{
		"abcdefghijklmnopabcdefghijklmnop": map[string]interface{}{
			"installation_mode": "force_installed",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	policies := []*pb.ChromePolicy{
		{HomepageLocation: &homepageLoc},
		{HomepageIsNewTabPage: &homepageIsNewTab, DefaultSearchProviderEnabled: &searchEnabled},
		{ExtensionSettings: extMap},
	}

	if err := SyncChromeFromProto(policies, []string{managedDir}); err != nil {
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

func TestSyncChromeFromProto_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	// With no policies and no pre-existing file, removal is a no-op.
	if err := SyncChromeFromProto(nil, []string{managedDir}); err != nil {
		t.Fatalf("SyncChromeFromProto with nil policies failed: %v", err)
	}

	target := filepath.Join(managedDir, ChromeManagedFilename)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected bor_managed.json to not exist after empty sync")
	}
}

func TestSyncChromeFromProto_NoCommentKey(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.ChromePolicy{HomepageLocation: &homepageLoc}

	if err := SyncChromeFromProto([]*pb.ChromePolicy{pol}, []string{managedDir}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(managedDir, ChromeManagedFilename)
	data, err := os.ReadFile(target)
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

func TestSyncChromeFromProto_WritesFile(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.ChromePolicy{HomepageLocation: &homepageLoc}

	if err := SyncChromeFromProto([]*pb.ChromePolicy{pol}, []string{managedDir}); err != nil {
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

func TestSyncChromeFromProto_RemovesFileOnEmpty(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	// First write a file.
	homepageLoc := "https://example.com"
	pol := &pb.ChromePolicy{HomepageLocation: &homepageLoc}
	if err := SyncChromeFromProto([]*pb.ChromePolicy{pol}, []string{managedDir}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(managedDir, ChromeManagedFilename)
	if _, err := os.Stat(target); err != nil {
		t.Fatal("expected file to exist after write")
	}

	// Now sync with nil policies — file should be removed.
	if err := SyncChromeFromProto(nil, []string{managedDir}); err != nil {
		t.Fatalf("SyncChromeFromProto with nil policies failed: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected bor_managed.json to be removed after empty sync")
	}
}

func TestSyncChromeFromProto_RemovesFileOnEmpty_NoErrorIfMissing(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	// Sync with nil policies when no file exists — should not error.
	if err := SyncChromeFromProto(nil, []string{managedDir}); err != nil {
		t.Fatalf("SyncChromeFromProto with nil policies and no existing file failed: %v", err)
	}
}

func TestSyncChromeFromProto_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	// Use a deeply nested directory that does not exist yet.
	managedDir := filepath.Join(dir, "etc", "opt", "chrome", "policies", "managed")

	searchEnabled := true
	pol := &pb.ChromePolicy{DefaultSearchProviderEnabled: &searchEnabled}
	if err := SyncChromeFromProto([]*pb.ChromePolicy{pol}, []string{managedDir}); err != nil {
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

func TestSyncChromeFromProto_MultipleDirs(t *testing.T) {
	dir := t.TempDir()
	dir1 := filepath.Join(dir, "chrome", "managed")
	dir2 := filepath.Join(dir, "chromium", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.ChromePolicy{HomepageLocation: &homepageLoc}

	if err := SyncChromeFromProto([]*pb.ChromePolicy{pol}, []string{dir1, dir2}); err != nil {
		t.Fatal(err)
	}

	for _, d := range []string{dir1, dir2} {
		target := filepath.Join(d, ChromeManagedFilename)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("expected bor_managed.json in %s: %v", d, err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if result["HomepageLocation"] != "https://example.com" {
			t.Errorf("dir %s: expected HomepageLocation, got %v", d, result["HomepageLocation"])
		}
	}
}

func TestSyncChromeFromProto_NilPoliciesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.ChromePolicy{HomepageLocation: &homepageLoc}

	// Mix of nil and non-nil policies — nil entries must be silently skipped.
	if err := SyncChromeFromProto([]*pb.ChromePolicy{nil, pol, nil}, []string{managedDir}); err != nil {
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
}
