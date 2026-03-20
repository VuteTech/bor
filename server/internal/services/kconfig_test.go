// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateKConfigPolicy_Empty(t *testing.T) {
	err := ValidateKConfigPolicy("{}")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestValidateKConfigPolicy_EmptyString(t *testing.T) {
	err := ValidateKConfigPolicy("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestValidateKConfigPolicy_NoSettings(t *testing.T) {
	// enforcedFields alone does not count as a setting.
	err := ValidateKConfigPolicy(`{"enforcedFields": ["shellAccess"]}`)
	if err == nil {
		t.Fatal("expected error when no actual settings are configured")
	}
}

func TestValidateKConfigPolicy_InvalidJSON(t *testing.T) {
	err := ValidateKConfigPolicy("{bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateKConfigPolicy_SingleBoolFalse(t *testing.T) {
	// shellAccess: false is a valid, meaningful setting (restrict shell access).
	err := ValidateKConfigPolicy(`{"shellAccess": false}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_SingleBoolTrue(t *testing.T) {
	err := ValidateKConfigPolicy(`{"shellAccess": true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_MultipleSettings(t *testing.T) {
	content := `{
		"shellAccess": false,
		"runCommand": false,
		"lockTimeout": 300,
		"iconTheme": "breeze",
		"enforcedFields": ["shellAccess", "runCommand", "lockTimeout"]
	}`
	err := ValidateKConfigPolicy(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_AllBoolFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"shellAccess", `{"shellAccess": false}`},
		{"runCommand", `{"runCommand": false}`},
		{"actionLogout", `{"actionLogout": false}`},
		{"actionFileNew", `{"actionFileNew": false}`},
		{"actionFileOpen", `{"actionFileOpen": false}`},
		{"actionFileSave", `{"actionFileSave": false}`},
		{"restrictWallpaper", `{"restrictWallpaper": false}`},
		{"restrictIcons", `{"restrictIcons": false}`},
		{"restrictAutostart", `{"restrictAutostart": false}`},
		{"restrictColors", `{"restrictColors": false}`},
		{"restrictCursors", `{"restrictCursors": false}`},
		{"borderlessMaximizedWindows", `{"borderlessMaximizedWindows": true}`},
		{"plasmoidUnlockedDesktop", `{"plasmoidUnlockedDesktop": false}`},
		{"allowConfigureWhenLocked", `{"allowConfigureWhenLocked": false}`},
		{"autoLock", `{"autoLock": true}`},
		{"lockOnResume", `{"lockOnResume": true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKConfigPolicy(tt.content)
			if err != nil {
				t.Errorf("field %q should be valid, got: %v", tt.name, err)
			}
		})
	}
}

func TestValidateKConfigPolicy_IntField(t *testing.T) {
	err := ValidateKConfigPolicy(`{"lockTimeout": 300}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_StringFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"iconTheme", `{"iconTheme": "breeze"}`},
		{"wallpaperPlugin", `{"wallpaperPlugin": "org.kde.image"}`},
		{"wallpaperImage", `{"wallpaperImage": "/usr/share/wallpapers/Flow.jpg"}`},
		{"wallpaperFillMode", `{"wallpaperFillMode": "2"}`},
		{"wallpaperColor", `{"wallpaperColor": "0,0,0"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKConfigPolicy(tt.content)
			if err != nil {
				t.Errorf("field %q should be valid, got: %v", tt.name, err)
			}
		})
	}
}

func TestValidateKConfigPolicy_KcmRestrictions(t *testing.T) {
	err := ValidateKConfigPolicy(`{"kcmRestrictions": ["kcm_bluetooth", "kcm_wifi"]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_URLRestrictionValid(t *testing.T) {
	content := `{
		"urlRestrictions": [
			{"action": "open", "protocol": "http", "host": "example.com", "enabled": true},
			{"action": "list", "protocol": "file", "enabled": false}
		]
	}`
	err := ValidateKConfigPolicy(content)
	if err != nil {
		t.Fatalf("expected valid URL restrictions to pass, got: %v", err)
	}
}

func TestValidateKConfigPolicy_URLRestrictionInvalidAction(t *testing.T) {
	content := `{"urlRestrictions": [{"action": "block", "protocol": "http", "host": "example.com", "enabled": true}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for invalid action 'block'")
	}
	if !strings.Contains(err.Error(), "invalid action") {
		t.Errorf("expected 'invalid action' error, got: %v", err)
	}
}

func TestValidateKConfigPolicy_URLRestrictionEmptyAction(t *testing.T) {
	// Missing action defaults to empty string which is also invalid.
	content := `{"urlRestrictions": [{"protocol": "http", "host": "example.com", "enabled": true}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestValidateKConfigPolicy_AllURLRestrictionActions(t *testing.T) {
	for _, action := range []string{"open", "list", "redirect"} {
		content := `{"urlRestrictions": [{"action": "` + action + `", "protocol": "http", "enabled": true}]}`
		err := ValidateKConfigPolicy(content)
		if err != nil {
			t.Errorf("action %q should be valid, got: %v", action, err)
		}
	}
}

func TestValidateKConfigPolicy_EnforcedFieldsOnly(t *testing.T) {
	// enforcedFields without a real setting is invalid.
	err := ValidateKConfigPolicy(`{"enforcedFields": ["lockTimeout"]}`)
	if err == nil {
		t.Fatal("expected error when only enforcedFields is present")
	}
}

func TestParseKConfigPolicyContent_Valid(t *testing.T) {
	content := `{
		"shellAccess": false,
		"lockTimeout": 300,
		"enforcedFields": ["shellAccess", "lockTimeout"]
	}`
	kcp, err := ParseKConfigPolicyContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kcp.ShellAccess == nil || *kcp.ShellAccess != false {
		t.Error("expected ShellAccess to be false")
	}
	if kcp.LockTimeout == nil || *kcp.LockTimeout != 300 {
		t.Errorf("expected LockTimeout 300, got %v", kcp.LockTimeout)
	}
	if len(kcp.EnforcedFields) != 2 {
		t.Errorf("expected 2 enforced fields, got %d", len(kcp.EnforcedFields))
	}
}

func TestParseKConfigPolicyContent_Invalid(t *testing.T) {
	_, err := ParseKConfigPolicyContent(`{}`)
	if err == nil {
		t.Fatal("expected validation error for empty content")
	}
}
