// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"fmt"
	"regexp"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	repoIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	pkgNamePattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+_-]*$`)
	pkgVersionPattern = regexp.MustCompile(`^[a-zA-Z0-9.+:~_-]+$`)
	gpgFingerprintRe  = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)
)

// ValidatePackagePolicy validates a PackagePolicy JSON string for release.
func ValidatePackagePolicy(content string) error {
	// First round-trip through encoding/json to confirm it is valid JSON.
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	var pol pb.PackagePolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid PackagePolicy: %w", err)
	}

	if len(pol.GetRepositories()) == 0 && len(pol.GetPackages()) == 0 {
		return fmt.Errorf("policy must contain at least one repository or one package entry")
	}

	seenRepoIDs := make(map[string]bool)
	for i, repo := range pol.GetRepositories() {
		if err := validateRepositoryEntry(repo, i); err != nil {
			return err
		}
		if seenRepoIDs[repo.GetId()] {
			return fmt.Errorf("repository[%d]: duplicate id %q", i, repo.GetId())
		}
		seenRepoIDs[repo.GetId()] = true
	}

	seenPkgNames := make(map[string]bool)
	for i, pkg := range pol.GetPackages() {
		if err := validatePackageEntry(pkg, i); err != nil {
			return err
		}
		if seenPkgNames[pkg.GetName()] {
			return fmt.Errorf("package[%d]: duplicate name %q", i, pkg.GetName())
		}
		seenPkgNames[pkg.GetName()] = true
	}

	return nil
}

func validateRepositoryEntry(repo *pb.RepositoryEntry, idx int) error {
	prefix := fmt.Sprintf("repository[%d]", idx)

	if repo.GetId() == "" {
		return fmt.Errorf("%s: id is required", prefix)
	}
	if !repoIDPattern.MatchString(repo.GetId()) {
		return fmt.Errorf("%s: id %q must match ^[a-z0-9][a-z0-9_-]*$", prefix, repo.GetId())
	}

	switch repo.GetType() {
	case pb.RepositoryType_REPOSITORY_TYPE_UNSPECIFIED:
		return fmt.Errorf("%s: type must be set (apt_deb822, dnf, or zypper)", prefix)

	case pb.RepositoryType_REPOSITORY_TYPE_APT_DEB822:
		if repo.GetAptUri() == "" {
			return fmt.Errorf("%s (apt): apt_uri is required", prefix)
		}
		if repo.GetAptSuites() == "" {
			return fmt.Errorf("%s (apt): apt_suites is required", prefix)
		}
		if repo.GetAptComponents() == "" {
			return fmt.Errorf("%s (apt): apt_components is required", prefix)
		}

	case pb.RepositoryType_REPOSITORY_TYPE_DNF:
		if repo.GetBaseurl() == "" && repo.GetMirrorlist() == "" {
			return fmt.Errorf("%s (dnf): exactly one of baseurl or mirrorlist is required", prefix)
		}
		if repo.GetBaseurl() != "" && repo.GetMirrorlist() != "" {
			return fmt.Errorf("%s (dnf): baseurl and mirrorlist are mutually exclusive", prefix)
		}

	case pb.RepositoryType_REPOSITORY_TYPE_ZYPPER:
		if repo.GetBaseurl() == "" {
			return fmt.Errorf("%s (zypper): baseurl is required", prefix)
		}
	}

	// GPG validation — enforced when gpg_check is true (the default on new entries).
	// The zero-value of bool in proto3 is false, but our UI defaults it to true.
	// We validate the key data whenever it is provided.
	if repo.GetGpgCheck() {
		if len(repo.GetGpgKeyData()) == 0 {
			return fmt.Errorf("%s: gpg_key_data is required when gpg_check is true", prefix)
		}
	}
	if repo.GetGpgKeyId() != "" && !gpgFingerprintRe.MatchString(repo.GetGpgKeyId()) {
		return fmt.Errorf("%s: gpg_key_id must be a 40-character hex fingerprint (got %d chars)", prefix, len(repo.GetGpgKeyId()))
	}

	return nil
}

func validatePackageEntry(pkg *pb.PackageEntry, idx int) error {
	prefix := fmt.Sprintf("package[%d]", idx)

	if pkg.GetName() == "" {
		return fmt.Errorf("%s: name is required", prefix)
	}
	if !pkgNamePattern.MatchString(pkg.GetName()) {
		return fmt.Errorf("%s: name %q contains invalid characters", prefix, pkg.GetName())
	}
	if pkg.GetState() == pb.PackageState_PACKAGE_STATE_UNSPECIFIED {
		return fmt.Errorf("%s: state must be set (present, absent, or latest)", prefix)
	}
	if pkg.GetVersion() != "" {
		if pkg.GetState() != pb.PackageState_PACKAGE_STATE_PRESENT {
			return fmt.Errorf("%s: version pin is only valid with state=present", prefix)
		}
		if !pkgVersionPattern.MatchString(pkg.GetVersion()) {
			return fmt.Errorf("%s: version %q contains invalid characters", prefix, pkg.GetVersion())
		}
	}

	return nil
}
