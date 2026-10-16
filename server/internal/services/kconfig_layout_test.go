// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateKConfigPolicy_LayoutValid(t *testing.T) {
	for _, content := range []string{
		`{"layout": {"wallpaperImage": "/usr/share/backgrounds/corp.png"}}`,
		`{"layout": {"wallpaperImage": "file:///usr/share/backgrounds/corp.png", "wallpaperFillMode": "1", "applyOnce": true, "applyLive": true, "lockPackage": true}}`,
		`{"layout": {"layoutScript": "loadTemplate(\"org.kde.plasma.desktop.defaultPanel\")"}}`,
	} {
		if err := ValidateKConfigPolicy(content); err != nil {
			t.Errorf("%s: unexpected error: %v", content, err)
		}
	}
}

func TestValidateKConfigPolicy_LayoutInvalid(t *testing.T) {
	cases := map[string]string{
		"relative wallpaper":    `{"layout": {"wallpaperImage": "corp.png"}}`,
		"http wallpaper":        `{"layout": {"wallpaperImage": "http://x/corp.png"}}`,
		"quote in wallpaper":    `{"layout": {"wallpaperImage": "/a\"b.png"}}`,
		"bad fill mode":         `{"layout": {"wallpaperImage": "/a.png", "wallpaperFillMode": "4"}}`,
		"flags without content": `{"layout": {"applyOnce": true}}`,
		"script too long":       `{"layout": {"layoutScript": "` + strings.Repeat("x", maxKConfigLayoutScript+1) + `"}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateKConfigPolicy(content); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
