// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateKConfigPolicy_SettingsValid(t *testing.T) {
	content := `{"settings": [
		{"file": "dolphinrc", "groupPath": ["General"], "key": "ShowFullPath", "value": "true", "immutable": true, "catalogueId": "dolphin.ShowFullPath"},
		{"file": "kscreenlockerrc", "groupPath": ["Greeter", "Wallpaper", "org.kde.image", "General"], "key": "Image", "value": "$HOME/lock.png", "expand": true},
		{"file": "kdeglobals", "groupPath": ["General"], "key": "ColorScheme", "delete": true},
		{"file": "kdeglobals", "groupPath": ["General"], "key": "Name", "value": "Firma", "locale": "de_DE"},
		{"file": "kioslaverc", "groupPath": ["Proxy Settings"], "key": "Proxy Config Script", "value": "http://wpad/proxy.pac"}
	]}`
	if err := ValidateKConfigPolicy(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_SettingsInvalid(t *testing.T) {
	cases := map[string]string{
		"path in file name":   `{"settings": [{"file": "../passwd", "groupPath": ["General"], "key": "x", "value": "1"}]}`,
		"hidden file":         `{"settings": [{"file": ".bashrc", "groupPath": ["General"], "key": "x", "value": "1"}]}`,
		"empty group path":    `{"settings": [{"file": "kdeglobals", "groupPath": [], "key": "x", "value": "1"}]}`,
		"bracket in group":    `{"settings": [{"file": "kdeglobals", "groupPath": ["Gen]eral"], "key": "x", "value": "1"}]}`,
		"equals in key":       `{"settings": [{"file": "kdeglobals", "groupPath": ["General"], "key": "a=b", "value": "1"}]}`,
		"line break in value": `{"settings": [{"file": "kdeglobals", "groupPath": ["General"], "key": "x", "value": "1\n[Other]"}]}`,
		"bad locale":          `{"settings": [{"file": "kdeglobals", "groupPath": ["General"], "key": "x", "value": "1", "locale": "DE-de"}]}`,
		"bad catalogue id":    `{"settings": [{"file": "kdeglobals", "groupPath": ["General"], "key": "x", "value": "1", "catalogueId": "a b"}]}`,
		"duplicate target": `{"settings": [
			{"file": "kdeglobals", "groupPath": ["General"], "key": "x", "value": "1"},
			{"file": "kdeglobals", "groupPath": ["General"], "key": "x", "value": "2"}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateKConfigPolicy(content)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "settings[") {
				t.Errorf("error should point at the setting, got: %v", err)
			}
		})
	}
}

func TestValidateKConfigPolicy_SettingsTooMany(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"settings": [`)
	for i := 0; i <= maxKConfigSettings; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"file": "kdeglobals", "groupPath": ["General"], "key": "k` + strings.Repeat("x", i%7) + `", "value": "1"}`)
	}
	sb.WriteString(`]}`)
	if err := ValidateKConfigPolicy(sb.String()); err == nil {
		t.Fatal("expected an error for too many settings")
	}
}

func TestValidateKConfigPolicy_ShowOnlyNeedsModules(t *testing.T) {
	if err := ValidateKConfigPolicy(`{"kcmMode": "KCM_SHOW_ONLY_LISTED", "shellAccess": false}`); err == nil {
		t.Fatal("expected an error for show-only mode without modules")
	}
	if err := ValidateKConfigPolicy(`{"kcmMode": "KCM_SHOW_ONLY_LISTED", "kcmRestrictions": ["kcm_access"]}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_FileImmutableAlone(t *testing.T) {
	if err := ValidateKConfigPolicy(`{"fileImmutableKdeglobals": true}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigTarget(t *testing.T) {
	good := []struct {
		file string
		path []string
		key  string
	}{
		{"kdeglobals", []string{"KDE Action Restrictions"}, "shell_access"},
		{"plasma-org.kde.plasma.desktop-appletsrc", []string{"Containments", "1"}, "wallpaperplugin"},
		{"kioslaverc", []string{"Proxy Settings"}, "Proxy Config Script"},
		{"kactivitymanagerd-pluginsrc", []string{"Plugin-org.kde.ActivityManager.Resources.Scoring"}, "what-to-remember"},
	}
	for _, g := range good {
		if err := ValidateKConfigTarget(g.file, g.path, g.key); err != nil {
			t.Errorf("%s/%v/%s should be valid: %v", g.file, g.path, g.key, err)
		}
	}
	bad := []struct {
		file string
		path []string
		key  string
	}{
		{"", []string{"General"}, "k"},
		{"a/b", []string{"General"}, "k"},
		{"kdeglobals", nil, "k"},
		{"kdeglobals", []string{""}, "k"},
		{"kdeglobals", []string{"General"}, ""},
		{"kdeglobals", []string{"General"}, "k[$i]"},
	}
	for _, b := range bad {
		if err := ValidateKConfigTarget(b.file, b.path, b.key); err == nil {
			t.Errorf("%q/%v/%q should be rejected", b.file, b.path, b.key)
		}
	}
}
