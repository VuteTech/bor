// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/proto"
)

func boolPtr(b bool) *bool { return &b }

func TestMergeFirefoxProtos_SinglePolicy(t *testing.T) {
	policies := []*pb.FirefoxPolicy{
		{DisableTelemetry: boolPtr(true), DisablePocket: boolPtr(true)},
	}
	merged := MergeFirefoxProtos(policies)
	if !merged.GetDisableTelemetry() {
		t.Error("expected DisableTelemetry to be true")
	}
	if !merged.GetDisablePocket() {
		t.Error("expected DisablePocket to be true")
	}
}

func TestMergeFirefoxProtos_LaterOverwritesEarlier(t *testing.T) {
	policies := []*pb.FirefoxPolicy{
		{DisableTelemetry: boolPtr(false)},
		{DisableTelemetry: boolPtr(true)},
	}
	merged := MergeFirefoxProtos(policies)
	if !merged.GetDisableTelemetry() {
		t.Error("expected later policy to overwrite earlier")
	}
}

func TestMergeFirefoxProtos_RepeatedFieldsAppended(t *testing.T) {
	policies := []*pb.FirefoxPolicy{
		{Extensions: &pb.FirefoxExtensions{Install: []string{"ext1@example.com"}}},
		{Extensions: &pb.FirefoxExtensions{Install: []string{"ext2@example.com"}}},
	}
	merged := MergeFirefoxProtos(policies)
	if len(merged.GetExtensions().GetInstall()) != 2 {
		t.Errorf("expected 2 extensions, got %d", len(merged.GetExtensions().GetInstall()))
	}
}

func TestMergeFirefoxProtos_NilSkipped(t *testing.T) {
	policies := []*pb.FirefoxPolicy{nil, {DisableTelemetry: boolPtr(true)}, nil}
	merged := MergeFirefoxProtos(policies)
	if !merged.GetDisableTelemetry() {
		t.Error("expected DisableTelemetry to be true")
	}
}

func TestMergeFirefoxProtos_Empty(t *testing.T) {
	merged := MergeFirefoxProtos(nil)
	if !proto.Equal(merged, &pb.FirefoxPolicy{}) {
		t.Error("expected empty merged policy")
	}
}

func TestSyncFirefoxPoliciesFromProto_WritesPoliciesJSON(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policies.json")

	policies := []*pb.FirefoxPolicy{
		{DisableTelemetry: boolPtr(true), DisablePocket: boolPtr(false)},
	}
	if err := SyncFirefoxPoliciesFromProto(target, policies); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Comment  string                 `json:"_comment"`
		Policies map[string]interface{} `json:"policies"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if result.Comment != FirefoxManagedComment {
		t.Error("expected managed comment")
	}
	if result.Policies["DisableTelemetry"] != true {
		t.Errorf("expected DisableTelemetry=true, got %v", result.Policies["DisableTelemetry"])
	}
	if result.Policies["DisablePocket"] != false {
		t.Errorf("expected DisablePocket=false, got %v", result.Policies["DisablePocket"])
	}
}

func TestSyncFirefoxPoliciesFromProto_PascalCaseKeys(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policies.json")

	policies := []*pb.FirefoxPolicy{
		{
			Homepage: &pb.FirefoxHomepage{
				URL:    "https://example.com",
				Locked: true,
			},
		},
	}
	if err := SyncFirefoxPoliciesFromProto(target, policies); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Policies map[string]interface{} `json:"policies"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}

	// Firefox requires PascalCase keys — must not be "homepage" (camelCase)
	if _, ok := result.Policies["Homepage"]; !ok {
		t.Error("expected PascalCase 'Homepage' key in policies.json")
	}
	if _, ok := result.Policies["homepage"]; ok {
		t.Error("unexpected camelCase 'homepage' key — Firefox would not recognise it")
	}

	hp, ok := result.Policies["Homepage"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Homepage to be a map")
	}
	if hp["URL"] != "https://example.com" {
		t.Errorf("expected Homepage.URL = https://example.com, got %v", hp["URL"])
	}
}

func TestSyncFirefoxPoliciesFromProto_RestoresOnEmpty(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policies.json")

	// Pre-write an "original" policies.json
	original := []byte(`{"policies":{},"_comment":"original"}`)
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write managed policies first so a backup is created
	if err := SyncFirefoxPoliciesFromProto(target, []*pb.FirefoxPolicy{
		{DisableTelemetry: boolPtr(true)},
	}); err != nil {
		t.Fatal(err)
	}

	// Now sync with empty list — should restore original
	if err := SyncFirefoxPoliciesFromProto(target, nil); err != nil {
		t.Fatal(err)
	}

	restored, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Errorf("expected original to be restored, got %q", restored)
	}
}

func TestSyncFirefoxFlatpakPoliciesFromProto_RemovesOnEmpty(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policies.json")

	// Write a managed file first
	if err := SyncFirefoxFlatpakPoliciesFromProto(target, []*pb.FirefoxPolicy{
		{DisableTelemetry: boolPtr(true)},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Empty policies — should remove the file
	if err := SyncFirefoxFlatpakPoliciesFromProto(target, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}

func TestWriteFileAtomically(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "policies.json")

	data := []byte(`{"policies": {}}`)
	if err := WriteFileAtomically(target, data); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("expected %q, got %q", data, got)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("expected permissions 0o644, got %o", info.Mode().Perm())
	}
}
