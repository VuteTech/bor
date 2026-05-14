// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

// ── ValidatePackagePolicy ─────────────────────────────────────────────────────

func validAPTPolicy() string {
	return `{
		"repositories": [{
			"id": "google-chrome",
			"name": "Google Chrome",
			"type": "REPOSITORY_TYPE_APT_DEB822",
			"enabled": true,
			"aptUri": "https://dl.google.com/linux/chrome/deb/",
			"aptSuites": "stable",
			"aptComponents": "main",
			"gpgCheck": false
		}],
		"packages": [{
			"name": "google-chrome-stable",
			"state": "PACKAGE_STATE_PRESENT"
		}]
	}`
}

func TestValidatePackagePolicy_ValidAPT(t *testing.T) {
	if err := ValidatePackagePolicy(validAPTPolicy()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_ValidDNF(t *testing.T) {
	content := `{
		"repositories": [{
			"id": "fedora-extras",
			"type": "REPOSITORY_TYPE_DNF",
			"baseurl": "https://mirrors.fedoraproject.org/metalink",
			"gpgCheck": false
		}],
		"packages": []
	}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_ValidZypper(t *testing.T) {
	content := `{
		"repositories": [{
			"id": "opensuse-oss",
			"type": "REPOSITORY_TYPE_ZYPPER",
			"baseurl": "http://download.opensuse.org/distribution/leap/15.6/repo/oss/",
			"gpgCheck": false
		}]
	}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_ValidDNF_MirrorlistOnly(t *testing.T) {
	content := `{
		"packages": [{
			"name": "curl",
			"state": "PACKAGE_STATE_LATEST"
		}],
		"repositories": [{
			"id": "centos-appstream",
			"type": "REPOSITORY_TYPE_DNF",
			"mirrorlist": "https://mirrorlist.centos.org/appstream",
			"gpgCheck": false
		}]
	}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_PackageOnly(t *testing.T) {
	content := `{"packages": [{"name": "vim", "state": "PACKAGE_STATE_PRESENT"}]}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("packages-only policy should be valid: %v", err)
	}
}

func TestValidatePackagePolicy_RepoOnly(t *testing.T) {
	content := `{"repositories": [{"id": "myrepo", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false}]}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("repos-only policy should be valid: %v", err)
	}
}

func TestValidatePackagePolicy_EmptyBothSections(t *testing.T) {
	content := `{"repositories": [], "packages": []}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for empty policy")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("error should mention 'at least one', got: %v", err)
	}
}

func TestValidatePackagePolicy_InvalidJSON(t *testing.T) {
	if err := ValidatePackagePolicy("{not json}"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ── Repository validation ─────────────────────────────────────────────────────

func TestValidatePackagePolicy_RepoMissingID(t *testing.T) {
	content := `{"repositories": [{"type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for missing repo id")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_RepoInvalidIDPattern(t *testing.T) {
	cases := []string{"My Repo", "UPPERCASE", "repo!", "-repo"}
	for _, id := range cases {
		content := `{"repositories": [{"id": "` + id + `", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false}]}`
		err := ValidatePackagePolicy(content)
		if err == nil {
			t.Errorf("expected error for invalid id %q", id)
		}
	}
}

func TestValidatePackagePolicy_RepoTypeUnspecified(t *testing.T) {
	content := `{"repositories": [{"id": "myrepo", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for unspecified repo type")
	}
}

func TestValidatePackagePolicy_DuplicateRepoIDs(t *testing.T) {
	content := `{"repositories": [
		{"id": "same", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://a.com/", "aptSuites": "s", "aptComponents": "m", "gpgCheck": false},
		{"id": "same", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://b.com/", "aptSuites": "s", "aptComponents": "m", "gpgCheck": false}
	]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for duplicate repo IDs")
	}
	if !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_APT_MissingURI(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for missing apt_uri")
	}
	if !strings.Contains(err.Error(), "apt_uri") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_APT_MissingSuites(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptComponents": "main", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for missing apt_suites")
	}
	if !strings.Contains(err.Error(), "apt_suites") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_APT_MissingComponents(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for missing apt_components")
	}
	if !strings.Contains(err.Error(), "apt_components") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_DNF_BothBaseurlAndMirrorlist(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_DNF", "baseurl": "https://a.com/", "mirrorlist": "https://m.com/", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for both baseurl and mirrorlist")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_DNF_NeitherBaseurlNorMirrorlist(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_DNF", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for neither baseurl nor mirrorlist")
	}
}

func TestValidatePackagePolicy_Zypper_MissingBaseurl(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_ZYPPER", "gpgCheck": false}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for missing zypper baseurl")
	}
	if !strings.Contains(err.Error(), "baseurl") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_GPGCheck_RequiresKeyData(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": true}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error when gpg_check is true but no key data provided")
	}
	if !strings.Contains(err.Error(), "gpg_key_data") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_GPGKeyID_InvalidFingerprint_TooShort(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false, "gpgKeyId": "DEADBEEF"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for short GPG fingerprint")
	}
	if !strings.Contains(err.Error(), "40-character") {
		t.Errorf("expected 40-character mention in error, got: %v", err)
	}
}

func TestValidatePackagePolicy_GPGKeyID_InvalidFingerprint_NonHex(t *testing.T) {
	content := `{"repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false, "gpgKeyId": "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for non-hex GPG fingerprint")
	}
}

func TestValidatePackagePolicy_GPGKeyID_Valid40HexChars(t *testing.T) {
	content := `{"packages": [{"name": "vim", "state": "PACKAGE_STATE_PRESENT"}], "repositories": [{"id": "r", "type": "REPOSITORY_TYPE_APT_DEB822", "aptUri": "https://example.com/", "aptSuites": "stable", "aptComponents": "main", "gpgCheck": false, "gpgKeyId": "ABCDEF1234567890ABCDEF1234567890ABCDEF12"}]}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("unexpected error for valid 40-char hex fingerprint: %v", err)
	}
}

// ── Package entry validation ──────────────────────────────────────────────────

func TestValidatePackagePolicy_PkgMissingName(t *testing.T) {
	content := `{"packages": [{"state": "PACKAGE_STATE_PRESENT"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for missing package name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_PkgInvalidName(t *testing.T) {
	content := `{"packages": [{"name": "my package!", "state": "PACKAGE_STATE_PRESENT"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for invalid package name")
	}
	if !strings.Contains(err.Error(), "invalid characters") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_PkgStateUnspecified(t *testing.T) {
	content := `{"packages": [{"name": "vim"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for unspecified package state")
	}
	if !strings.Contains(err.Error(), "state must be set") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_PkgVersionWithAbsentState(t *testing.T) {
	content := `{"packages": [{"name": "vim", "state": "PACKAGE_STATE_ABSENT", "version": "9.0"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for version pin with state=absent")
	}
	if !strings.Contains(err.Error(), "version pin") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_PkgVersionWithLatestState(t *testing.T) {
	content := `{"packages": [{"name": "vim", "state": "PACKAGE_STATE_LATEST", "version": "9.0"}]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for version pin with state=latest")
	}
}

func TestValidatePackagePolicy_PkgVersionWithPresentState(t *testing.T) {
	content := `{"packages": [{"name": "vim", "state": "PACKAGE_STATE_PRESENT", "version": "9.0.1000-1.fc39"}]}`
	if err := ValidatePackagePolicy(content); err != nil {
		t.Fatalf("version pin with state=present should be valid: %v", err)
	}
}

func TestValidatePackagePolicy_DuplicatePackageNames(t *testing.T) {
	content := `{"packages": [
		{"name": "vim", "state": "PACKAGE_STATE_PRESENT"},
		{"name": "vim", "state": "PACKAGE_STATE_ABSENT"}
	]}`
	err := ValidatePackagePolicy(content)
	if err == nil {
		t.Fatal("expected error for duplicate package names")
	}
	if !strings.Contains(err.Error(), "duplicate name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePackagePolicy_AllPackageStates(t *testing.T) {
	for _, state := range []string{"PACKAGE_STATE_PRESENT", "PACKAGE_STATE_ABSENT", "PACKAGE_STATE_LATEST"} {
		content := `{"packages": [{"name": "vim", "state": "` + state + `"}]}`
		if err := ValidatePackagePolicy(content); err != nil {
			t.Errorf("state %q should be valid, got error: %v", state, err)
		}
	}
}

func TestValidatePackagePolicy_ValidVersionFormats(t *testing.T) {
	versions := []string{
		"1.0",
		"1.0.0",
		"1.0-1.fc39",
		"2:1.0.0-1",
		"1.0+dfsg-1",
		"1.0~rc1",
	}
	for _, ver := range versions {
		content := `{"packages": [{"name": "pkg", "state": "PACKAGE_STATE_PRESENT", "version": "` + ver + `"}]}`
		if err := ValidatePackagePolicy(content); err != nil {
			t.Errorf("version %q should be valid, got error: %v", ver, err)
		}
	}
}
