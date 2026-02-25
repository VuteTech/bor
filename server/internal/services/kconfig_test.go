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

func TestValidateKConfigPolicy_NoEntries(t *testing.T) {
	err := ValidateKConfigPolicy(`{"entries": []}`)
	if err == nil {
		t.Fatal("expected error for empty entries array")
	}
}

func TestValidateKConfigPolicy_Valid(t *testing.T) {
	content := `{
		"entries": [
			{"file": "kdeglobals", "group": "KDE Action Restrictions", "key": "shell_access", "value": "false", "type": "bool", "enforced": true}
		]
	}`
	err := ValidateKConfigPolicy(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_MultipleEntries(t *testing.T) {
	content := `{
		"entries": [
			{"file": "kdeglobals", "group": "KDE Action Restrictions", "key": "shell_access", "value": "false", "type": "bool", "enforced": true},
			{"file": "kwinrc", "group": "Windows", "key": "BorderlessMaximizedWindows", "value": "true", "type": "bool", "enforced": false},
			{"file": "kscreenlockerrc", "group": "Daemon", "key": "Timeout", "value": "300", "type": "int", "enforced": true}
		]
	}`
	err := ValidateKConfigPolicy(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKConfigPolicy_MissingFile(t *testing.T) {
	content := `{"entries": [{"file": "", "group": "G", "key": "K", "value": "V", "type": "bool"}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "file is required") {
		t.Errorf("expected 'file is required' error, got: %v", err)
	}
}

func TestValidateKConfigPolicy_MissingGroup(t *testing.T) {
	content := `{"entries": [{"file": "kdeglobals", "group": "", "key": "K", "value": "V", "type": "bool"}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing group")
	}
	if !strings.Contains(err.Error(), "group is required") {
		t.Errorf("expected 'group is required' error, got: %v", err)
	}
}

func TestValidateKConfigPolicy_MissingKey(t *testing.T) {
	content := `{"entries": [{"file": "kdeglobals", "group": "G", "key": "", "value": "V", "type": "bool"}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "key is required") {
		t.Errorf("expected 'key is required' error, got: %v", err)
	}
}

func TestValidateKConfigPolicy_InvalidFile(t *testing.T) {
	content := `{"entries": [{"file": "badfile", "group": "G", "key": "K", "value": "V", "type": "bool"}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for invalid file name")
	}
	if !strings.Contains(err.Error(), "not in the allowed set") {
		t.Errorf("expected 'not in the allowed set' error, got: %v", err)
	}
}

func TestValidateKConfigPolicy_PathTraversal(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"dot-dot", "../etc/passwd"},
		{"slash", "subdir/kdeglobals"},
		{"backslash", "subdir\\kdeglobals"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `{"entries": [{"file": "` + tt.file + `", "group": "G", "key": "K", "value": "V", "type": "bool"}]}`
			err := ValidateKConfigPolicy(content)
			if err == nil {
				t.Fatalf("expected error for path traversal in file %q", tt.file)
			}
		})
	}
}

func TestValidateKConfigPolicy_InvalidType(t *testing.T) {
	content := `{"entries": [{"file": "kdeglobals", "group": "G", "key": "K", "value": "V", "type": "float"}]}`
	err := ValidateKConfigPolicy(content)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "not valid") {
		t.Errorf("expected 'not valid' error, got: %v", err)
	}
}

func TestValidateKConfigPolicy_EmptyTypeAllowed(t *testing.T) {
	content := `{"entries": [{"file": "kdeglobals", "group": "G", "key": "K", "value": "V"}]}`
	err := ValidateKConfigPolicy(content)
	if err != nil {
		t.Fatalf("empty type should be allowed, got: %v", err)
	}
}

func TestValidateKConfigPolicy_AllowedFiles(t *testing.T) {
	allowedFiles := []string{"kdeglobals", "kwinrc", "plasmarc", "kscreenlockerrc", "dolphinrc", "konsolerc"}
	for _, f := range allowedFiles {
		content := `{"entries": [{"file": "` + f + `", "group": "G", "key": "K", "value": "V", "type": "string"}]}`
		err := ValidateKConfigPolicy(content)
		if err != nil {
			t.Errorf("file %q should be allowed, got: %v", f, err)
		}
	}
}

func TestValidateKConfigPolicy_WallpaperValid(t *testing.T) {
	tests := []struct {
		name  string
		group string
	}{
		{"containment-level", "Containments][1"},
		{"containment-99", "Containments][99"},
		{"wallpaper-plugin-general", "Containments][1][Wallpaper][org.kde.image][General"},
		{"wallpaper-plugin-other", "Containments][2][Wallpaper][org.kde.potd][General"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `{"entries": [{"file": "plasma-org.kde.plasma.desktop-appletsrc", "group": "` + tt.group + `", "key": "K", "value": "V", "type": "string"}]}`
			err := ValidateKConfigPolicy(content)
			if err != nil {
				t.Errorf("group %q should be allowed, got: %v", tt.group, err)
			}
		})
	}
}

func TestValidateKConfigPolicy_WallpaperInvalidGroup(t *testing.T) {
	tests := []struct {
		name  string
		group string
	}{
		{"arbitrary-group", "General"},
		{"no-containment-prefix", "Wallpaper][org.kde.image][General"},
		{"missing-plugin-section", "Containments][1][General"},
		{"empty-containment-id", "Containments]["},
		{"non-numeric-containment", "Containments][abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `{"entries": [{"file": "plasma-org.kde.plasma.desktop-appletsrc", "group": "` + tt.group + `", "key": "K", "value": "V", "type": "string"}]}`
			err := ValidateKConfigPolicy(content)
			if err == nil {
				t.Errorf("group %q should be rejected for appletsrc file", tt.group)
			}
			if err != nil && !strings.Contains(err.Error(), "not allowed") {
				t.Errorf("expected 'not allowed' error, got: %v", err)
			}
		})
	}
}

func TestValidateKConfigPolicy_InvalidJSON(t *testing.T) {
	err := ValidateKConfigPolicy("{bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseKConfigPolicyContent_Valid(t *testing.T) {
	content := `{
		"entries": [
			{"file": "kdeglobals", "group": "KDE Action Restrictions", "key": "shell_access", "value": "false", "type": "bool", "enforced": true}
		]
	}`
	c, err := ParseKConfigPolicyContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(c.Entries))
	}
	if c.Entries[0].GetFile() != "kdeglobals" {
		t.Errorf("expected file 'kdeglobals', got %q", c.Entries[0].GetFile())
	}
	if !c.Entries[0].GetEnforced() {
		t.Error("expected entry to be enforced")
	}
}

func TestParseKConfigPolicyContent_Invalid(t *testing.T) {
	_, err := ParseKConfigPolicyContent(`{"entries": [{"file": "", "group": "G", "key": "K"}]}`)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
