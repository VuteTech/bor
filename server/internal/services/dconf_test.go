// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateDConfPolicy_Empty(t *testing.T) {
	err := ValidateDConfPolicy("{}")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestValidateDConfPolicy_EmptyString(t *testing.T) {
	err := ValidateDConfPolicy("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestValidateDConfPolicy_InvalidJSON(t *testing.T) {
	err := ValidateDConfPolicy("{bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateDConfPolicy_NoEntries(t *testing.T) {
	err := ValidateDConfPolicy(`{"dbName": "local"}`)
	if err == nil {
		t.Fatal("expected error when no entries are configured")
	}
}

func TestValidateDConfPolicy_MissingSchemaID(t *testing.T) {
	content := `{"entries": [{"key": "lock-enabled", "value": "true"}]}`
	err := ValidateDConfPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing schema_id")
	}
	if !strings.Contains(err.Error(), "schema_id") {
		t.Errorf("expected schema_id error, got: %v", err)
	}
}

func TestValidateDConfPolicy_MissingKey(t *testing.T) {
	content := `{"entries": [{"schemaId": "org.gnome.desktop.screensaver", "value": "true"}]}`
	err := ValidateDConfPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("expected key error, got: %v", err)
	}
}

func TestValidateDConfPolicy_MissingValue(t *testing.T) {
	content := `{"entries": [{"schemaId": "org.gnome.desktop.screensaver", "key": "lock-enabled"}]}`
	err := ValidateDConfPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing value")
	}
	if !strings.Contains(err.Error(), "value") {
		t.Errorf("expected value error, got: %v", err)
	}
}

func TestValidateDConfPolicy_Valid(t *testing.T) {
	content := `{
		"entries": [
			{"schemaId": "org.gnome.desktop.screensaver", "key": "lock-enabled", "value": "true", "lock": true},
			{"schemaId": "org.gnome.desktop.screensaver", "key": "lock-delay", "value": "uint32 300"}
		],
		"dbName": "local"
	}`
	err := ValidateDConfPolicy(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDConfPolicy_ValidMinimal(t *testing.T) {
	content := `{"entries": [{"schemaId": "org.gnome.desktop.interface", "key": "color-scheme", "value": "'prefer-dark'"}]}`
	err := ValidateDConfPolicy(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDConfPolicyContent_Valid(t *testing.T) {
	content := `{
		"entries": [
			{"schemaId": "org.gnome.desktop.screensaver", "key": "lock-enabled", "value": "true", "lock": true}
		]
	}`
	dp, err := ParseDConfPolicyContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(dp.Entries))
	}
	if dp.Entries[0].GetSchemaId() != "org.gnome.desktop.screensaver" {
		t.Errorf("unexpected schema_id: %s", dp.Entries[0].GetSchemaId())
	}
	if dp.Entries[0].GetKey() != "lock-enabled" {
		t.Errorf("unexpected key: %s", dp.Entries[0].GetKey())
	}
	if !dp.Entries[0].GetLock() {
		t.Error("expected lock to be true")
	}
}

func TestParseDConfPolicyContent_Invalid(t *testing.T) {
	_, err := ParseDConfPolicyContent(`{}`)
	if err == nil {
		t.Fatal("expected validation error for empty content")
	}
}
