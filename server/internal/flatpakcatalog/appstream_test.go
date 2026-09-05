// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func loadFixture(t *testing.T) []models.FlatpakCatalogEntry {
	t.Helper()
	f, err := os.Open("testdata/appstream-mini.xml.gz")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	entries, err := ParseAppStreamGzip(f, 0)
	if err != nil {
		t.Fatalf("ParseAppStreamGzip: %v", err)
	}
	return entries
}

func byID(entries []models.FlatpakCatalogEntry, id string) *models.FlatpakCatalogEntry {
	for i := range entries {
		if entries[i].AppID == id {
			return &entries[i]
		}
	}
	return nil
}

func TestParseAppStream_Fixture(t *testing.T) {
	entries := loadFixture(t)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (component without bundle skipped), got %d", len(entries))
	}

	kinds := map[string]int{}
	for _, e := range entries {
		kinds[e.Kind]++
	}
	if kinds["desktop-application"] != 2 || kinds["console-application"] != 1 || kinds["addon"] != 1 || kinds["runtime"] != 1 {
		t.Errorf("unexpected kind distribution: %v", kinds)
	}

	ff := byID(entries, "org.mozilla.firefox")
	if ff == nil {
		t.Fatal("firefox missing")
	}
	if ff.Name != "Firefox" || ff.Summary != "Fast, Private & Safe Web Browser" {
		t.Errorf("untranslated name/summary wrong: %q / %q", ff.Name, ff.Summary)
	}
	if ff.Arch != "x86_64" || ff.Branch != "stable" || ff.Ref != "app/org.mozilla.firefox/x86_64/stable" {
		t.Errorf("bundle parse wrong: %+v", ff)
	}
	if ff.Runtime != "org.freedesktop.Platform/x86_64/24.08" {
		t.Errorf("runtime = %q", ff.Runtime)
	}
	if ff.Description != "Firefox is a free browser. Fast Private" {
		t.Errorf("description = %q", ff.Description)
	}
	if ff.Developer != "Mozilla" || ff.ProjectLicense != "MPL-2.0" || ff.Homepage != "https://www.mozilla.org/firefox/" {
		t.Errorf("developer/license/homepage: %q %q %q", ff.Developer, ff.ProjectLicense, ff.Homepage)
	}
	if strings.Join(ff.Categories, ";") != "Network;WebBrowser" {
		t.Errorf("categories = %v", ff.Categories)
	}
	if strings.Join(ff.Keywords, " ") != "web browser" {
		t.Errorf("keywords (localized must be skipped) = %v", ff.Keywords)
	}
	if ff.LatestVersion != "141.0" || ff.LatestReleaseAt == nil || ff.LatestReleaseAt.Unix() != 1756000000 {
		t.Errorf("release = %q %v", ff.LatestVersion, ff.LatestReleaseAt)
	}
	if !ff.Verified || ff.IconFile != "org.mozilla.firefox.png" || ff.ContentRating != "oars-1.1" {
		t.Errorf("verified/icon/rating: %v %q %q", ff.Verified, ff.IconFile, ff.ContentRating)
	}

	tool := byID(entries, "org.example.Tool")
	if tool == nil || tool.Developer != "Example Devs" || tool.LatestReleaseAt == nil || tool.LatestReleaseAt.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("console tool: %+v", tool)
	}
	if tool.IconFile != "" || tool.Verified {
		t.Errorf("tool should have no icon and not be verified: %+v", tool)
	}

	legacy := byID(entries, "org.example.Legacy")
	if legacy == nil {
		t.Fatal("legacy app must use the bundle id, not the .desktop component id")
	}
	if legacy.Kind != "desktop-application" || legacy.Arch != "aarch64" || legacy.Branch != "beta" || legacy.Verified {
		t.Errorf("legacy: %+v", legacy)
	}
	if legacy.IconFile != "org.example.Legacy.png" {
		t.Errorf("remote-only icon should fall back to <id>.png, got %q", legacy.IconFile)
	}
	if rt := byID(entries, "org.freedesktop.Platform"); rt == nil || rt.Kind != "runtime" {
		t.Errorf("runtime: %+v", rt)
	}
}

func TestParseAppStreamGzip_SizeCap(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(`<components><component type="desktop-application"><id>a.b.c</id><name>` + strings.Repeat("x", 5000) + `</name><bundle type="flatpak">app/a.b.c/x86_64/stable</bundle></component></components>`))
	_ = gz.Close()
	if _, err := ParseAppStreamGzip(bytes.NewReader(buf.Bytes()), 1024); !errors.Is(err, ErrCatalogTooLarge) {
		t.Fatalf("expected ErrCatalogTooLarge, got %v", err)
	}
	if _, err := ParseAppStreamGzip(bytes.NewReader([]byte("not gzip")), 0); err == nil {
		t.Fatal("expected error for non-gzip input")
	}
}

func TestParseAppStream_MalformedXML(t *testing.T) {
	if _, err := ParseAppStream(strings.NewReader(`<components><component type="desktop-application"><id>x`)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestStripTags(t *testing.T) {
	got := stripTags("<p>Hello &amp; <b>world</b></p>\n<ul>\n<li>one</li></ul>")
	if got != "Hello & world one" {
		t.Errorf("stripTags = %q", got)
	}
	long := stripTags(strings.Repeat("a", maxDescriptionRunes+100))
	if len([]rune(long)) != maxDescriptionRunes {
		t.Errorf("stripTags cap = %d runes", len([]rune(long)))
	}
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		ref string
		ok  bool
	}{
		{"app/org.x.y/x86_64/stable", true},
		{"runtime/org.freedesktop.Platform/x86_64/24.08", true},
		{"app/org.x.y/x86_64", false},
		{"weird/org.x.y/x86_64/stable", false},
		{"app//x86_64/stable", false},
	}
	for _, tt := range tests {
		if _, _, _, _, ok := splitRef(tt.ref); ok != tt.ok {
			t.Errorf("splitRef(%q) ok = %v, want %v", tt.ref, ok, tt.ok)
		}
	}
}
