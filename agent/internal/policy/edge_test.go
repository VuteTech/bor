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
)

func TestSyncEdgeFromProto_SinglePolicy(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	homepageIsNewTab := false
	pol := &pb.EdgePolicy{
		HomepageLocation:     &homepageLoc,
		HomepageIsNewTabPage: &homepageIsNewTab,
	}

	if syncErr := SyncEdgeFromProto([]*pb.EdgePolicy{pol}, []string{managedDir}); syncErr != nil {
		t.Fatal(syncErr)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	var result map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}

	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation to be https://example.com, got %v", result["HomepageLocation"])
	}
	if result["HomepageIsNewTabPage"] != false {
		t.Errorf("expected HomepageIsNewTabPage to be false, got %v", result["HomepageIsNewTabPage"])
	}
}

func TestSyncEdgeFromProto_MultiplePolicies(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	homepageIsNewTab := false
	trackingPrevention := int32(2)

	policies := []*pb.EdgePolicy{
		{HomepageLocation: &homepageLoc},
		{HomepageIsNewTabPage: &homepageIsNewTab, TrackingPrevention: &trackingPrevention},
	}

	if syncErr := SyncEdgeFromProto(policies, []string{managedDir}); syncErr != nil {
		t.Fatal(syncErr)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	var result map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}

	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation, got %v", result["HomepageLocation"])
	}
	if result["HomepageIsNewTabPage"] != false {
		t.Errorf("expected HomepageIsNewTabPage false, got %v", result["HomepageIsNewTabPage"])
	}
	// TrackingPrevention is an int32 — JSON numbers decode as float64
	if result["TrackingPrevention"] != float64(2) {
		t.Errorf("expected TrackingPrevention 2, got %v", result["TrackingPrevention"])
	}
}

func TestSyncEdgeFromProto_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	if err := SyncEdgeFromProto(nil, []string{managedDir}); err != nil {
		t.Fatalf("SyncEdgeFromProto with nil policies failed: %v", err)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected bor_managed.json to not exist after empty sync")
	}
}

func TestSyncEdgeFromProto_WritesFile(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.EdgePolicy{HomepageLocation: &homepageLoc}

	if syncErr := SyncEdgeFromProto([]*pb.EdgePolicy{pol}, []string{managedDir}); syncErr != nil {
		t.Fatal(syncErr)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	var result map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}

	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation, got %v", result["HomepageLocation"])
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("expected permissions 0o644, got %o", info.Mode().Perm())
	}
}

func TestSyncEdgeFromProto_RemovesFileOnEmpty(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.EdgePolicy{HomepageLocation: &homepageLoc}
	if syncErr := SyncEdgeFromProto([]*pb.EdgePolicy{pol}, []string{managedDir}); syncErr != nil {
		t.Fatal(syncErr)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	if _, err := os.Stat(target); err != nil {
		t.Fatal("expected file to exist after write")
	}

	if err := SyncEdgeFromProto(nil, []string{managedDir}); err != nil {
		t.Fatalf("SyncEdgeFromProto with nil policies failed: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected bor_managed.json to be removed after empty sync")
	}
}

func TestSyncEdgeFromProto_RemovesFileOnEmpty_NoErrorIfMissing(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	if err := SyncEdgeFromProto(nil, []string{managedDir}); err != nil {
		t.Fatalf("SyncEdgeFromProto with nil policies and no existing file failed: %v", err)
	}
}

func TestSyncEdgeFromProto_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "etc", "opt", "edge", "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.EdgePolicy{HomepageLocation: &homepageLoc}
	if syncErr := SyncEdgeFromProto([]*pb.EdgePolicy{pol}, []string{managedDir}); syncErr != nil {
		t.Fatal(syncErr)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	info, err := os.Stat(managedDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected directory permissions 0o755, got %o", info.Mode().Perm())
	}
}

func TestSyncEdgeFromProto_MultipleDirs(t *testing.T) {
	dir := t.TempDir()
	dir1 := filepath.Join(dir, "edge", "managed")
	dir2 := filepath.Join(dir, "edge-beta", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.EdgePolicy{HomepageLocation: &homepageLoc}

	if err := SyncEdgeFromProto([]*pb.EdgePolicy{pol}, []string{dir1, dir2}); err != nil {
		t.Fatal(err)
	}

	for _, d := range []string{dir1, dir2} {
		target := filepath.Join(d, EdgeManagedFilename)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("expected bor_managed.json in %s: %v", d, err)
		}
		var result map[string]interface{}
		if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
			t.Fatal(err)
		}
		if result["HomepageLocation"] != "https://example.com" {
			t.Errorf("dir %s: expected HomepageLocation, got %v", d, result["HomepageLocation"])
		}
	}
}

func TestSyncEdgeFromProto_NilPoliciesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	managedDir := filepath.Join(dir, "policies", "managed")

	homepageLoc := "https://example.com"
	pol := &pb.EdgePolicy{HomepageLocation: &homepageLoc}

	if err := SyncEdgeFromProto([]*pb.EdgePolicy{nil, pol, nil}, []string{managedDir}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(managedDir, EdgeManagedFilename)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected bor_managed.json to exist: %v", err)
	}

	var result map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
		t.Fatal(err)
	}
	if result["HomepageLocation"] != "https://example.com" {
		t.Errorf("expected HomepageLocation, got %v", result["HomepageLocation"])
	}
}
