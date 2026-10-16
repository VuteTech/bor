// SPDX-License-Identifier: AGPL-3.0-only
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
		{"borderlessMaximizedWindows", `{"borderlessMaximizedWindows": true}`},
		{"plasmoidUnlockedDesktop", `{"plasmoidUnlockedDesktop": false}`},
		{"allowConfigureWhenLocked", `{"allowConfigureWhenLocked": false}`},
		{"autoLock", `{"autoLock": true}`},
		{"lockOnResume", `{"lockOnResume": true}`},
		{"lockOnStart", `{"lockOnStart": true}`},
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

func TestValidateKConfigPolicy_LegacyOnlyRejected(t *testing.T) {
	// The resource-restriction fields parse but have no effect on Plasma 5/6;
	// a policy made only of them is rejected with a message that names them.
	for _, field := range []string{"restrictWallpaper", "restrictIcons", "restrictAutostart", "restrictColors", "restrictCursors"} {
		t.Run(field, func(t *testing.T) {
			err := ValidateKConfigPolicy(`{"` + field + `": true, "enforcedFields": ["` + field + `"]}`)
			if err == nil {
				t.Fatal("expected error for legacy-only policy")
			}
			if !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "no effect") {
				t.Errorf("error should name the legacy field and say it has no effect, got: %v", err)
			}
		})
	}
}

func TestValidateKConfigPolicy_LegacyWithEffectiveAccepted(t *testing.T) {
	// Old content that still carries a legacy field alongside real settings
	// keeps parsing, so it can be opened and the legacy field removed.
	err := ValidateKConfigPolicy(`{"restrictWallpaper": true, "shellAccess": false}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_IntField(t *testing.T) {
	err := ValidateKConfigPolicy(`{"lockTimeout": 300}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_ScreenLockFields(t *testing.T) {
	content := `{"autoLock": true, "lockTimeout": 10, "lockGrace": 0, "lockOnStart": true, "lockScreenImage": "/usr/share/backgrounds/lock.png"}`
	if err := ValidateKConfigPolicy(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_NegativeLockValues(t *testing.T) {
	for _, content := range []string{`{"lockTimeout": -1}`, `{"lockGrace": -5}`} {
		if err := ValidateKConfigPolicy(content); err == nil {
			t.Errorf("expected error for %s", content)
		}
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
		{"lockScreenImage", `{"lockScreenImage": "/usr/share/wallpapers/Lock.jpg"}`},
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

func TestValidateKConfigPolicy_StringLineBreakRejected(t *testing.T) {
	// A line break would let a value inject extra INI keys.
	err := ValidateKConfigPolicy(`{"iconTheme": "breeze\n[KDE Action Restrictions]\nshell_access=true"}`)
	if err == nil {
		t.Fatal("expected error for value with line break")
	}
	if !strings.Contains(err.Error(), "iconTheme") {
		t.Errorf("error should name the field, got: %v", err)
	}
}

func TestValidateKConfigPolicy_StringTooLong(t *testing.T) {
	err := ValidateKConfigPolicy(`{"iconTheme": "` + strings.Repeat("a", maxKConfigStringLen+1) + `"}`)
	if err == nil {
		t.Fatal("expected error for over-long value")
	}
}

func TestValidateKConfigPolicy_KcmRestrictions(t *testing.T) {
	err := ValidateKConfigPolicy(`{"kcmRestrictions": ["kcm_bluetooth", "kcm_wifi", "kcmspellchecking", "kcm_about-distro"]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_KcmInvalidID(t *testing.T) {
	for _, id := range []string{"kcm bad", "../x", "kcm=1", "", "a\nb"} {
		err := ValidateKConfigPolicy(`{"kcmRestrictions": [` + strconvQuote(id) + `]}`)
		if err == nil {
			t.Errorf("expected error for module id %q", id)
		}
	}
}

// strconvQuote is a tiny JSON string quoter for the test inputs above.
func strconvQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

func TestValidateKConfigPolicy_URLRestrictionValid(t *testing.T) {
	content := `{
		"urlRestrictions": [
			{"action": "open", "protocol": "http", "host": "example.com", "enabled": true},
			{"action": "list", "protocol": "file", "enabled": false},
			{"action": "open", "protocol": "file", "path": "$HOME", "enabled": true}
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

func TestValidateKConfigPolicy_URLRestrictionCommaRejected(t *testing.T) {
	// Rules are rendered as eight comma-separated fields; a comma in a field
	// would shift every following field.
	content := `{"urlRestrictions": [{"action": "open", "protocol": "http", "host": "a.com,b.com", "enabled": false}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for comma in host")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("error should name the field, got: %v", err)
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
		"lockGrace": 5,
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
	if kcp.LockGrace == nil || *kcp.LockGrace != 5 {
		t.Errorf("expected LockGrace 5, got %v", kcp.LockGrace)
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
