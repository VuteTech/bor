// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"strings"
	"testing"
)

// ── ympSanitizeID ─────────────────────────────────────────────────────────────

func TestYMPSanitizeID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"home:snwint:ports", "home-snwint-ports"},
		{"openSUSE:Factory", "opensuse-factory"},
		{"Packman", "packman"},
		{"OBS:Server:Unstable", "obs-server-unstable"},
		{"My Repo!", "my-repo"},
		{"  spaces  ", "spaces"},
		{"", "repo"},
		{"---", "repo"},
	}
	for _, tc := range cases {
		got := ympSanitizeID(tc.input)
		if got != tc.want {
			t.Errorf("ympSanitizeID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── ympBestURL ────────────────────────────────────────────────────────────────

func TestYMPBestURL_Empty(t *testing.T) {
	if got := ympBestURL(nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestYMPBestURL_Single(t *testing.T) {
	urls := []ympURL{{Value: "http://example.com/"}}
	if got := ympBestURL(urls); got != "http://example.com/" {
		t.Errorf("unexpected: %q", got)
	}
}

func TestYMPBestURL_PicksHighestScore(t *testing.T) {
	urls := []ympURL{
		{Score: 5, Value: "http://low.example.com/"},
		{Score: 10, Value: "http://high.example.com/"},
	}
	if got := ympBestURL(urls); got != "http://high.example.com/" {
		t.Errorf("expected high-score URL, got %q", got)
	}
}

func TestYMPBestURL_TiebreakFirstWins(t *testing.T) {
	urls := []ympURL{
		{Score: 10, Value: "http://first.example.com/"},
		{Score: 10, Value: "http://second.example.com/"},
	}
	if got := ympBestURL(urls); got != "http://first.example.com/" {
		t.Errorf("first entry should win on tie, got %q", got)
	}
}

func TestYMPBestURL_TrimsWhitespace(t *testing.T) {
	urls := []ympURL{{Value: "  http://example.com/  "}}
	if got := ympBestURL(urls); got != "http://example.com/" {
		t.Errorf("expected trimmed URL, got %q", got)
	}
}

// ── parseYMP ──────────────────────────────────────────────────────────────────

var singleGroupYMP = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<metapackage xmlns:os="http://opensuse.org/Standards/One_Click_Install"
             xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group distversion="openSUSE Tumbleweed">
    <repositories>
      <repository recommended="true">
        <name>home:snwint:ports</name>
        <summary>Test repo</summary>
        <description>Test</description>
        <url>http://download.opensuse.org/repositories/home:/snwint:/ports/openSUSE_Factory/</url>
      </repository>
    </repositories>
    <software>
      <item>
        <name>mkdud</name>
        <summary>Create driver update from rpms</summary>
        <description>Desc</description>
      </item>
    </software>
  </group>
</metapackage>`)

func TestParseYMP_SingleGroup(t *testing.T) {
	result, err := parseYMP(singleGroupYMP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}
	g := result.Groups[0]
	if g.DistVersion != "openSUSE Tumbleweed" {
		t.Errorf("unexpected distVersion: %q", g.DistVersion)
	}
	if len(g.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(g.Repositories))
	}
	repo := g.Repositories[0]
	if repo.ID != "home-snwint-ports" {
		t.Errorf("unexpected repo ID: %q", repo.ID)
	}
	if repo.Type != "REPOSITORY_TYPE_ZYPPER" {
		t.Errorf("unexpected repo type: %q", repo.Type)
	}
	if !repo.Enabled {
		t.Error("expected repo to be enabled (recommended=true)")
	}
	if repo.GPGCheck {
		t.Error("gpgCheck should always be false for ymp imports")
	}
	if len(g.Packages) != 1 || g.Packages[0].Name != "mkdud" {
		t.Errorf("unexpected packages: %+v", g.Packages)
	}
	if g.Packages[0].State != "PACKAGE_STATE_PRESENT" {
		t.Errorf("unexpected state: %q", g.Packages[0].State)
	}
}

func TestParseYMP_MultipleGroups(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group distversion="openSUSE Tumbleweed">
    <repositories>
      <repository recommended="true">
        <name>repo-tw</name>
        <url>http://tw.example.com/</url>
      </repository>
    </repositories>
    <software><item><name>vim</name></item></software>
  </group>
  <group distversion="openSUSE Leap 15.1">
    <repositories>
      <repository recommended="false">
        <name>repo-leap</name>
        <url>http://leap.example.com/</url>
      </repository>
    </repositories>
    <software><item><name>vim</name></item></software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result.Groups))
	}
	if result.Groups[0].DistVersion != "openSUSE Tumbleweed" {
		t.Errorf("unexpected first distVersion: %q", result.Groups[0].DistVersion)
	}
	if result.Groups[1].DistVersion != "openSUSE Leap 15.1" {
		t.Errorf("unexpected second distVersion: %q", result.Groups[1].DistVersion)
	}
	// Leap repo is recommended=false → disabled.
	if result.Groups[1].Repositories[0].Enabled {
		t.Error("repo with recommended=false should be disabled")
	}
}

func TestParseYMP_MultipleURLs_BestScoreWins(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group>
    <repositories>
      <repository>
        <name>mirrors</name>
        <url score="5">http://slow.example.com/</url>
        <url score="10">http://fast.example.com/</url>
      </repository>
    </repositories>
    <software><item><name>pkg</name></item></software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo := result.Groups[0].Repositories[0]
	if repo.BaseURL != "http://fast.example.com/" {
		t.Errorf("expected fast URL, got %q", repo.BaseURL)
	}
}

func TestParseYMP_PatternSkipped(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group>
    <repositories>
      <repository><name>r</name><url>http://example.com/</url></repository>
    </repositories>
    <software>
      <item type="pattern"><name>base</name></item>
      <item><name>vim</name></item>
    </software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g := result.Groups[0]
	if len(g.Packages) != 1 || g.Packages[0].Name != "vim" {
		t.Errorf("expected only 'vim' package, got: %+v", g.Packages)
	}
	if len(g.Warnings) == 0 || !strings.Contains(g.Warnings[0], "pattern") {
		t.Errorf("expected pattern warning, got: %v", g.Warnings)
	}
}

func TestParseYMP_RemoveAction(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group>
    <repositories><repository><name>r</name><url>http://example.com/</url></repository></repositories>
    <software>
      <item action="remove"><name>oldpkg</name></item>
    </software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pkg := result.Groups[0].Packages[0]
	if pkg.State != "PACKAGE_STATE_ABSENT" {
		t.Errorf("remove action should map to ABSENT, got %q", pkg.State)
	}
}

func TestParseYMP_DuplicateRepoIDsSuffixed(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group>
    <repositories>
      <repository><name>openSUSE:Factory</name><url>http://a.example.com/</url></repository>
      <repository><name>openSUSE:Factory</name><url>http://b.example.com/</url></repository>
    </repositories>
    <software><item><name>pkg</name></item></software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repos := result.Groups[0].Repositories
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].ID == repos[1].ID {
		t.Errorf("duplicate IDs not resolved: both are %q", repos[0].ID)
	}
}

func TestParseYMP_RepoWithNoURLSkipped(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group>
    <repositories>
      <repository><name>no-url</name></repository>
      <repository><name>has-url</name><url>http://example.com/</url></repository>
    </repositories>
    <software><item><name>pkg</name></item></software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g := result.Groups[0]
	if len(g.Repositories) != 1 || g.Repositories[0].ID != "has-url" {
		t.Errorf("expected only 'has-url' repo, got: %+v", g.Repositories)
	}
	if len(g.Warnings) == 0 {
		t.Error("expected warning for repo with no URL")
	}
}

func TestParseYMP_DuplicatePackagesDeduped(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install">
  <group>
    <repositories><repository><name>r</name><url>http://example.com/</url></repository></repositories>
    <software>
      <item><name>vim</name></item>
      <item><name>vim</name></item>
    </software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Groups[0].Packages) != 1 {
		t.Errorf("expected duplicates to be deduplicated, got: %+v", result.Groups[0].Packages)
	}
}

func TestParseYMP_InvalidXML(t *testing.T) {
	if _, err := parseYMP([]byte("{not xml}")); err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

func TestParseYMP_NoGroups(t *testing.T) {
	data := []byte(`<metapackage xmlns="http://opensuse.org/Standards/One_Click_Install"></metapackage>`)
	_, err := parseYMP(data)
	if err == nil {
		t.Fatal("expected error for .ymp with no groups")
	}
}

func TestParseYMP_NoNamespace(t *testing.T) {
	// .ymp files without a namespace declaration should also parse correctly.
	data := []byte(`<metapackage>
  <group distversion="Tumbleweed">
    <repositories>
      <repository><name>r</name><url>http://example.com/</url></repository>
    </repositories>
    <software><item><name>vim</name></item></software>
  </group>
</metapackage>`)
	result, err := parseYMP(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}
}
