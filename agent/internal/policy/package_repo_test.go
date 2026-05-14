// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// ── MergePackageRepos ─────────────────────────────────────────────────────────

func TestMergePackageRepos_Empty(t *testing.T) {
	result := MergePackageRepos(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 repos, got %d", len(result))
	}
}

func TestMergePackageRepos_NilPolicySkipped(t *testing.T) {
	result := MergePackageRepos([]*pb.PackagePolicy{nil})
	if len(result) != 0 {
		t.Errorf("expected 0 repos for nil policy, got %d", len(result))
	}
}

func TestMergePackageRepos_SinglePolicy(t *testing.T) {
	pol := &pb.PackagePolicy{
		Repositories: []*pb.RepositoryEntry{
			{Id: "chrome", Name: "Google Chrome"},
		},
	}
	result := MergePackageRepos([]*pb.PackagePolicy{pol})
	if len(result) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(result))
	}
	if result[0].GetId() != "chrome" {
		t.Errorf("unexpected repo id: %q", result[0].GetId())
	}
}

func TestMergePackageRepos_LaterEntryOverrides(t *testing.T) {
	p1 := &pb.PackagePolicy{
		Repositories: []*pb.RepositoryEntry{
			{Id: "chrome", Name: "Old Name"},
		},
	}
	p2 := &pb.PackagePolicy{
		Repositories: []*pb.RepositoryEntry{
			{Id: "chrome", Name: "New Name"},
		},
	}
	result := MergePackageRepos([]*pb.PackagePolicy{p1, p2})
	if len(result) != 1 {
		t.Fatalf("expected 1 repo after merge, got %d", len(result))
	}
	if result[0].GetName() != "New Name" {
		t.Errorf("expected last-wins merge; got name %q", result[0].GetName())
	}
}

func TestMergePackageRepos_DifferentIDsPreserved(t *testing.T) {
	p1 := &pb.PackagePolicy{
		Repositories: []*pb.RepositoryEntry{{Id: "chrome"}},
	}
	p2 := &pb.PackagePolicy{
		Repositories: []*pb.RepositoryEntry{{Id: "firefox"}},
	}
	result := MergePackageRepos([]*pb.PackagePolicy{p1, p2})
	if len(result) != 2 {
		t.Errorf("expected 2 repos, got %d", len(result))
	}
}

func TestMergePackageRepos_OrderPreservedFirstSeen(t *testing.T) {
	pol := &pb.PackagePolicy{
		Repositories: []*pb.RepositoryEntry{
			{Id: "z-repo"},
			{Id: "a-repo"},
		},
	}
	result := MergePackageRepos([]*pb.PackagePolicy{pol})
	if len(result) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(result))
	}
	if result[0].GetId() != "z-repo" || result[1].GetId() != "a-repo" {
		t.Errorf("insertion order not preserved: %q %q", result[0].GetId(), result[1].GetId())
	}
}

// ── DesiredRepoPaths ──────────────────────────────────────────────────────────

func TestDesiredRepoPaths_APT_NoGPG(t *testing.T) {
	entries := []*pb.RepositoryEntry{
		{Id: "myrepo", AptUri: "https://example.com/", AptSuites: "stable", AptComponents: "main"},
	}
	paths := DesiredRepoPaths(RepoFormatAPT, entries)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path (no gpg), got %d: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "bor-myrepo.sources") {
		t.Errorf("expected .sources file, got %q", paths[0])
	}
}

func TestDesiredRepoPaths_APT_WithGPG(t *testing.T) {
	entries := []*pb.RepositoryEntry{
		{Id: "myrepo", AptUri: "https://example.com/", AptSuites: "stable", AptComponents: "main", GpgKeyData: []byte("gpgdata")},
	}
	paths := DesiredRepoPaths(RepoFormatAPT, entries)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths (repo + gpg), got %d: %v", len(paths), paths)
	}
	var hasSource, hasGPG bool
	for _, p := range paths {
		if strings.HasSuffix(p, ".sources") {
			hasSource = true
		}
		if strings.HasSuffix(p, ".gpg") {
			hasGPG = true
		}
	}
	if !hasSource || !hasGPG {
		t.Errorf("missing expected paths: sources=%v gpg=%v in %v", hasSource, hasGPG, paths)
	}
}

func TestDesiredRepoPaths_DNF(t *testing.T) {
	entries := []*pb.RepositoryEntry{
		{Id: "fedora-extras", Baseurl: "https://mirrors.fedoraproject.org/"},
	}
	paths := DesiredRepoPaths(RepoFormatDNF, entries)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if !strings.HasSuffix(paths[0], "bor-fedora-extras.repo") {
		t.Errorf("expected .repo file, got %q", paths[0])
	}
}

func TestDesiredRepoPaths_Zypper(t *testing.T) {
	entries := []*pb.RepositoryEntry{
		{Id: "oss", Baseurl: "http://download.opensuse.org/distribution/leap/15.6/repo/oss/"},
	}
	paths := DesiredRepoPaths(RepoFormatZypper, entries)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if !strings.Contains(paths[0], "zypp") {
		t.Errorf("expected zypper path, got %q", paths[0])
	}
}

func TestDesiredRepoPaths_UnknownFormat(t *testing.T) {
	entries := []*pb.RepositoryEntry{{Id: "repo"}}
	paths := DesiredRepoPaths(RepoFormatUnknown, entries)
	for _, p := range paths {
		if p != "" {
			t.Errorf("expected empty path for unknown format, got %q", p)
		}
	}
}

// ── repoIDFromPath ────────────────────────────────────────────────────────────

func TestRepoIDFromPath_APTSource(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/etc/apt/sources.list.d/bor-google-chrome.sources", "google-chrome"},
		{"/etc/yum.repos.d/bor-my-repo.repo", "my-repo"},
		{"/etc/pki/rpm-gpg/bor-my-repo.gpg", "my-repo"},
		{"/etc/zypp/repos.d/bor-opensuse-oss.repo", "opensuse-oss"},
	}
	for _, tc := range cases {
		got := repoIDFromPath(tc.path)
		if got != tc.want {
			t.Errorf("repoIDFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ── renderAPTDeb822 ───────────────────────────────────────────────────────────

func TestRenderAPTDeb822_ValidRepo(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:            "chrome",
		AptUri:        "https://dl.google.com/linux/chrome/deb/",
		AptSuites:     "stable",
		AptComponents: "main",
		Enabled:       true,
		GpgCheck:      true,
	}
	content, gpgPath, err := renderAPTDeb822(repo, "# header\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("Types: deb")) {
		t.Error("missing 'Types: deb'")
	}
	if !bytes.Contains(content, []byte("URIs: https://dl.google.com/linux/chrome/deb/")) {
		t.Error("missing URI line")
	}
	if !bytes.Contains(content, []byte("Suites: stable")) {
		t.Error("missing Suites line")
	}
	if !bytes.Contains(content, []byte("Components: main")) {
		t.Error("missing Components line")
	}
	if !bytes.Contains(content, []byte("Enabled: yes")) {
		t.Error("missing Enabled: yes")
	}
	if !strings.HasSuffix(gpgPath, "bor-chrome.gpg") {
		t.Errorf("unexpected gpgPath: %q", gpgPath)
	}
}

func TestRenderAPTDeb822_DisabledRepo(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:            "chrome",
		AptUri:        "https://dl.google.com/",
		AptSuites:     "stable",
		AptComponents: "main",
		Enabled:       false,
	}
	content, _, err := renderAPTDeb822(repo, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("Enabled: no")) {
		t.Error("expected 'Enabled: no' for disabled repo")
	}
}

func TestRenderAPTDeb822_WithGPGKey_AddsSignedBy(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:            "chrome",
		AptUri:        "https://dl.google.com/",
		AptSuites:     "stable",
		AptComponents: "main",
		GpgKeyData:    []byte("fake-gpg-data"),
	}
	content, _, err := renderAPTDeb822(repo, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("Signed-By:")) {
		t.Error("expected 'Signed-By:' when GPG key data is present")
	}
}

func TestRenderAPTDeb822_MissingURI(t *testing.T) {
	repo := &pb.RepositoryEntry{Id: "r", AptSuites: "stable", AptComponents: "main"}
	_, _, err := renderAPTDeb822(repo, "")
	if err == nil {
		t.Error("expected error for missing apt_uri")
	}
}

func TestRenderAPTDeb822_MissingSuites(t *testing.T) {
	repo := &pb.RepositoryEntry{Id: "r", AptUri: "https://example.com/", AptComponents: "main"}
	_, _, err := renderAPTDeb822(repo, "")
	if err == nil {
		t.Error("expected error for missing apt_suites")
	}
}

func TestRenderAPTDeb822_MissingComponents(t *testing.T) {
	repo := &pb.RepositoryEntry{Id: "r", AptUri: "https://example.com/", AptSuites: "stable"}
	_, _, err := renderAPTDeb822(repo, "")
	if err == nil {
		t.Error("expected error for missing apt_components")
	}
}

func TestRenderAPTDeb822_Architectures(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:               "r",
		AptUri:           "https://example.com/",
		AptSuites:        "stable",
		AptComponents:    "main",
		AptArchitectures: "amd64 arm64",
	}
	content, _, err := renderAPTDeb822(repo, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("Architectures: amd64 arm64")) {
		t.Error("expected Architectures line")
	}
}

// ── renderDNFRepo ─────────────────────────────────────────────────────────────

func TestRenderDNFRepo_ValidBaseurl(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:       "fedora-extras",
		Name:     "Fedora Extras",
		Baseurl:  "https://mirrors.fedoraproject.org/metalink?repo=fedora-$releasever",
		Enabled:  true,
		GpgCheck: true,
	}
	content, err := renderDNFRepo(repo, "# header\n", "/etc/pki/rpm-gpg/bor-fedora-extras.gpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("[bor-fedora-extras]")) {
		t.Error("missing INI section header")
	}
	if !bytes.Contains(content, []byte("name=Fedora Extras")) {
		t.Error("missing name field")
	}
	if !bytes.Contains(content, []byte("enabled=1")) {
		t.Error("missing enabled=1")
	}
	if !bytes.Contains(content, []byte("gpgcheck=1")) {
		t.Error("missing gpgcheck=1")
	}
}

func TestRenderDNFRepo_DisabledNoGPG(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:       "r",
		Baseurl:  "https://example.com/",
		Enabled:  false,
		GpgCheck: false,
	}
	content, err := renderDNFRepo(repo, "", "/tmp/bor-r.gpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("enabled=0")) {
		t.Error("expected enabled=0")
	}
	if !bytes.Contains(content, []byte("gpgcheck=0")) {
		t.Error("expected gpgcheck=0")
	}
}

func TestRenderDNFRepo_GPGKeyAddsGPGKeyLine(t *testing.T) {
	gpgPath := "/etc/pki/rpm-gpg/bor-r.gpg"
	repo := &pb.RepositoryEntry{
		Id:         "r",
		Baseurl:    "https://example.com/",
		GpgCheck:   true,
		GpgKeyData: []byte("fakekey"),
	}
	content, err := renderDNFRepo(repo, "", gpgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("gpgkey=file://"+gpgPath)) {
		t.Errorf("expected gpgkey line pointing to %s", gpgPath)
	}
}

func TestRenderDNFRepo_MetadataExpire(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:             "r",
		Baseurl:        "https://example.com/",
		MetadataExpire: 3600,
	}
	content, err := renderDNFRepo(repo, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("metadata_expire=3600")) {
		t.Error("expected metadata_expire field")
	}
}

func TestRenderDNFRepo_MissingBaseurlAndMirrorlist(t *testing.T) {
	repo := &pb.RepositoryEntry{Id: "r"}
	_, err := renderDNFRepo(repo, "", "")
	if err == nil {
		t.Error("expected error for missing baseurl and mirrorlist")
	}
}

func TestRenderDNFRepo_MirrorlistOnly(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:         "r",
		Mirrorlist: "https://mirrors.example.com/list",
	}
	content, err := renderDNFRepo(repo, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("mirrorlist=https://mirrors.example.com/list")) {
		t.Error("expected mirrorlist line")
	}
}

// ── renderZypperRepo ──────────────────────────────────────────────────────────

func TestRenderZypperRepo_ValidRepo(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:             "opensuse-oss",
		Name:           "OpenSUSE OSS",
		Baseurl:        "http://download.opensuse.org/distribution/leap/15.6/repo/oss/",
		Enabled:        true,
		GpgCheck:       true,
		ZypperPriority: 99,
	}
	content, err := renderZypperRepo(repo, "# header\n", "/etc/pki/rpm-gpg/bor-opensuse-oss.gpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("[bor-opensuse-oss]")) {
		t.Error("missing INI section header")
	}
	if !bytes.Contains(content, []byte("autorefresh=1")) {
		t.Error("missing autorefresh=1")
	}
	if !bytes.Contains(content, []byte("type=rpm-md")) {
		t.Error("missing type=rpm-md")
	}
	if !bytes.Contains(content, []byte("priority=99")) {
		t.Error("missing priority line")
	}
}

func TestRenderZypperRepo_DefaultPriority(t *testing.T) {
	repo := &pb.RepositoryEntry{
		Id:      "r",
		Baseurl: "http://example.com/",
		// ZypperPriority zero → should default to 99
	}
	content, err := renderZypperRepo(repo, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(content, []byte("priority=99")) {
		t.Errorf("expected default priority=99, content:\n%s", content)
	}
}

func TestRenderZypperRepo_MissingBaseurl(t *testing.T) {
	repo := &pb.RepositoryEntry{Id: "r"}
	_, err := renderZypperRepo(repo, "", "")
	if err == nil {
		t.Error("expected error for missing baseurl")
	}
}

// ── ListBorManagedRepoFiles ───────────────────────────────────────────────────

func TestListBorManagedRepoFiles_UnknownFormat(t *testing.T) {
	paths, err := ListBorManagedRepoFiles(RepoFormatUnknown)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for unknown format, got %d", len(paths))
	}
}
