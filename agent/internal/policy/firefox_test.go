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

func TestMergeFirefoxPolicies_SinglePolicy(t *testing.T) {
	contents := []string{
		`{"DisableTelemetry": true, "DisablePocket": true}`,
	}

	data, err := MergeFirefoxPolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result FirefoxPoliciesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.Policies["DisableTelemetry"] != true {
		t.Error("expected DisableTelemetry to be true")
	}
	if result.Policies["DisablePocket"] != true {
		t.Error("expected DisablePocket to be true")
	}
}

func TestMergeFirefoxPolicies_MultiplePolicies(t *testing.T) {
	contents := []string{
		`{"DisableTelemetry": true}`,
		`{"DisablePocket": true, "DontCheckDefaultBrowser": true}`,
		`{"Homepage": {"URL": "https://example.com", "Locked": true}}`,
	}

	data, err := MergeFirefoxPolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result FirefoxPoliciesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.Policies["DisableTelemetry"] != true {
		t.Error("expected DisableTelemetry")
	}
	if result.Policies["DisablePocket"] != true {
		t.Error("expected DisablePocket")
	}
	if result.Policies["DontCheckDefaultBrowser"] != true {
		t.Error("expected DontCheckDefaultBrowser")
	}

	homepage, ok := result.Policies["Homepage"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Homepage to be a map")
	}
	if homepage["URL"] != "https://example.com" {
		t.Errorf("expected Homepage.URL to be https://example.com, got %v", homepage["URL"])
	}
}

func TestMergeFirefoxPolicies_DeepMerge(t *testing.T) {
	contents := []string{
		`{"EnableTrackingProtection": {"Value": true, "Cryptomining": true}}`,
		`{"EnableTrackingProtection": {"Fingerprinting": true, "Locked": true}}`,
	}

	data, err := MergeFirefoxPolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result FirefoxPoliciesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	tp, ok := result.Policies["EnableTrackingProtection"].(map[string]interface{})
	if !ok {
		t.Fatal("expected EnableTrackingProtection to be a map")
	}
	if tp["Value"] != true {
		t.Error("expected Value")
	}
	if tp["Cryptomining"] != true {
		t.Error("expected Cryptomining")
	}
	if tp["Fingerprinting"] != true {
		t.Error("expected Fingerprinting")
	}
	if tp["Locked"] != true {
		t.Error("expected Locked")
	}
}

func TestMergeFirefoxPolicies_SliceAppend(t *testing.T) {
	contents := []string{
		`{"Extensions": {"Install": ["ext1@example.com"]}}`,
		`{"Extensions": {"Install": ["ext2@example.com"], "Uninstall": ["bad@example.com"]}}`,
	}

	data, err := MergeFirefoxPolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result FirefoxPoliciesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	ext, ok := result.Policies["Extensions"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Extensions to be a map")
	}
	install, ok := ext["Install"].([]interface{})
	if !ok {
		t.Fatal("expected Install to be a slice")
	}
	if len(install) != 2 {
		t.Errorf("expected 2 Install entries, got %d", len(install))
	}
	uninstall, ok := ext["Uninstall"].([]interface{})
	if !ok {
		t.Fatal("expected Uninstall to be a slice")
	}
	if len(uninstall) != 1 {
		t.Errorf("expected 1 Uninstall entry, got %d", len(uninstall))
	}
}

func TestMergeFirefoxPolicies_EmptyInput(t *testing.T) {
	data, err := MergeFirefoxPolicies(nil)
	if err != nil {
		t.Fatal(err)
	}

	var result FirefoxPoliciesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if len(result.Policies) != 0 {
		t.Errorf("expected empty policies, got %d keys", len(result.Policies))
	}
}

func TestMergeFirefoxPolicies_SkipsEmptyContent(t *testing.T) {
	contents := []string{"", "{}", `{"DisableTelemetry": true}`}
	data, err := MergeFirefoxPolicies(contents)
	if err != nil {
		t.Fatal(err)
	}

	var result FirefoxPoliciesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.Policies["DisableTelemetry"] != true {
		t.Error("expected DisableTelemetry")
	}
}

func TestMergeFirefoxPolicies_InvalidJSON(t *testing.T) {
	contents := []string{`not json`}
	_, err := MergeFirefoxPolicies(contents)
	if err == nil {
		t.Error("expected error for invalid JSON")
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
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("expected permissions 0644, got %o", info.Mode().Perm())
	}
}
