// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/godbus/dbus/v5"
)

const repoManagedComment = "# This file is managed by Bor. Do not edit manually.\n# Generated: %s\n\n"

// Repository directory paths.
const (
	aptSourcesDir  = "/etc/apt/sources.list.d"
	aptGPGDir      = "/etc/apt/trusted.gpg.d"
	dnfReposDir    = "/etc/yum.repos.d"
	rpmGPGDir      = "/etc/pki/rpm-gpg"
	zypperReposDir = "/etc/zypp/repos.d"
)

// ComplianceItem carries the result of a single repository or package check.
type ComplianceItem struct {
	Key     string
	Status  pb.ComplianceStatus
	Message string
}

// MergePackageRepos deduplicates RepositoryEntry slices by id.
// Entries are expected sorted ascending by priority; later (higher-priority)
// entries overwrite earlier ones for the same id.
func MergePackageRepos(policies []*pb.PackagePolicy) []*pb.RepositoryEntry {
	seen := make(map[string]*pb.RepositoryEntry)
	var order []string
	for _, pol := range policies {
		if pol == nil {
			continue
		}
		for _, repo := range pol.GetRepositories() {
			if _, exists := seen[repo.GetId()]; !exists {
				order = append(order, repo.GetId())
			}
			seen[repo.GetId()] = repo
		}
	}
	result := make([]*pb.RepositoryEntry, 0, len(order))
	for _, id := range order {
		result = append(result, seen[id])
	}
	return result
}

// SyncAllRepos writes managed repository and GPG key files for the given
// format, removes stale bor-* files, and optionally calls PackageKit
// RefreshCache. Returns the desired file paths and per-repo compliance items.
func SyncAllRepos(
	ctx context.Context,
	conn *dbus.Conn,
	format RepoFormat,
	entries []*pb.RepositoryEntry,
	updateCache bool,
) (desiredPaths []string, items []ComplianceItem, err error) {
	if format == RepoFormatUnknown {
		return nil, nil, fmt.Errorf("no supported package manager detected on this node")
	}

	desired := make(map[string][]byte) // path → content

	for _, repo := range entries {
		repoPath, repoContent, gpgPath, gpgContent, genErr := renderRepo(format, repo)
		if genErr != nil {
			items = append(items, ComplianceItem{
				Key:     "repo:" + repo.GetId(),
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "failed to render repo file: " + genErr.Error(),
			})
			continue
		}
		desired[repoPath] = repoContent
		if gpgPath != "" && gpgContent != nil {
			desired[gpgPath] = gpgContent
		}
	}

	// Collect desired paths for watcher suppression by caller.
	for p := range desired {
		desiredPaths = append(desiredPaths, p)
	}

	// Write desired files atomically.
	for path, content := range desired {
		if writeErr := atomicWrite(path, content, 0o644); writeErr != nil {
			// Find the repo ID this path belongs to.
			repoID := repoIDFromPath(path)
			items = append(items, ComplianceItem{
				Key:     "repo:" + repoID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "failed to write file " + path + ": " + writeErr.Error(),
			})
		}
	}

	// Remove stale bor-* files not in the desired set.
	existing, _ := ListBorManagedRepoFiles(format)
	desiredSet := make(map[string]bool, len(desiredPaths))
	for _, p := range desiredPaths {
		desiredSet[p] = true
	}
	for _, stalePath := range existing {
		if !desiredSet[stalePath] {
			_ = os.Remove(stalePath)
		}
	}

	// Build compliance items for each repo entry.
	for _, repo := range entries {
		repoPath, expected, _, _, genErr := renderRepo(format, repo)
		if genErr != nil {
			continue // already recorded above
		}
		actual, readErr := os.ReadFile(repoPath) //nolint:gosec // G304: path from renderRepo using trusted id
		if readErr != nil {
			items = append(items, ComplianceItem{
				Key:     "repo:" + repo.GetId(),
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "repo file missing or unreadable",
			})
			continue
		}
		if bytes.Equal(actual, expected) {
			items = append(items, ComplianceItem{
				Key:    "repo:" + repo.GetId(),
				Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			})
		} else {
			items = append(items, ComplianceItem{
				Key:     "repo:" + repo.GetId(),
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "repo file content does not match policy",
			})
		}

		// If gpg_check is false, record a persistent warning item.
		if !repo.GetGpgCheck() {
			items = append(items, ComplianceItem{
				Key:     "repo:" + repo.GetId() + ":gpg",
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "GPG verification is disabled for this repository (security risk)",
			})
		}
	}

	// Refresh cache after writing repos.
	if updateCache && conn != nil && len(entries) > 0 {
		if refreshErr := RefreshCache(ctx, conn); refreshErr != nil {
			items = append(items, ComplianceItem{
				Key:     "repo:cache_refresh",
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "PackageKit RefreshCache failed: " + refreshErr.Error(),
			})
		}
	}

	return desiredPaths, items, nil
}

// ListBorManagedRepoFiles returns the absolute paths of all repository and
// GPG key files in the package manager's directories that were created by Bor.
// Bor-managed files are identified by the "bor-" filename prefix.
func ListBorManagedRepoFiles(format RepoFormat) ([]string, error) {
	var dirs []string
	switch format {
	case RepoFormatAPT:
		dirs = []string{aptSourcesDir, aptGPGDir}
	case RepoFormatDNF:
		dirs = []string{dnfReposDir, rpmGPGDir}
	case RepoFormatZypper:
		dirs = []string{zypperReposDir, rpmGPGDir}
	default:
		return nil, nil
	}

	var managed []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "bor-") {
				managed = append(managed, filepath.Join(dir, e.Name()))
			}
		}
	}
	return managed, nil
}

// DesiredRepoPaths returns the file paths that would be written for the given
// entries and format, without performing any I/O. Used by the caller for
// watcher suppression.
func DesiredRepoPaths(format RepoFormat, entries []*pb.RepositoryEntry) []string {
	var paths []string
	for _, repo := range entries {
		repoPath, _, gpgPath, _, genErr := renderRepo(format, repo)
		if genErr != nil || repoPath == "" {
			// Invalid id (e.g. path-traversal attempt) — no managed path.
			continue
		}
		paths = append(paths, repoPath)
		if gpgPath != "" {
			paths = append(paths, gpgPath)
		}
	}
	return paths
}

// renderRepo generates the file path and content for a repository entry
// in the given format. Also returns the GPG key path and data if applicable.
func renderRepo(format RepoFormat, repo *pb.RepositoryEntry) (repoPath string, repoContent []byte, gpgPath string, gpgData []byte, err error) {
	id := repo.GetId()
	// id is server-supplied and embedded in file paths under the system
	// repository/keyring directories — reject anything that could escape them.
	if vErr := validatePathIdentifier("repository id", id); vErr != nil {
		return "", nil, "", nil, vErr
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	header := fmt.Sprintf(repoManagedComment, ts)

	switch format {
	case RepoFormatAPT:
		repoPath = filepath.Join(aptSourcesDir, "bor-"+id+".sources")
		content, gpgFilePath, genErr := renderAPTDeb822(repo, header)
		if genErr != nil {
			return "", nil, "", nil, genErr
		}
		repoContent = content
		if len(repo.GetGpgKeyData()) > 0 {
			gpgPath = gpgFilePath
			gpgData = repo.GetGpgKeyData()
		}

	case RepoFormatDNF:
		repoPath = filepath.Join(dnfReposDir, "bor-"+id+".repo")
		gpgFilePath := filepath.Join(rpmGPGDir, "bor-"+id+".gpg")
		content, genErr := renderDNFRepo(repo, header, gpgFilePath)
		if genErr != nil {
			return "", nil, "", nil, genErr
		}
		repoContent = content
		if len(repo.GetGpgKeyData()) > 0 {
			gpgPath = gpgFilePath
			gpgData = repo.GetGpgKeyData()
		}

	case RepoFormatZypper:
		repoPath = filepath.Join(zypperReposDir, "bor-"+id+".repo")
		gpgFilePath := filepath.Join(rpmGPGDir, "bor-"+id+".gpg")
		content, genErr := renderZypperRepo(repo, header, gpgFilePath)
		if genErr != nil {
			return "", nil, "", nil, genErr
		}
		repoContent = content
		if len(repo.GetGpgKeyData()) > 0 {
			gpgPath = gpgFilePath
			gpgData = repo.GetGpgKeyData()
		}

	default:
		return "", nil, "", nil, fmt.Errorf("unsupported repo format %d", format)
	}

	return repoPath, repoContent, gpgPath, gpgData, nil
}

// renderAPTDeb822 renders an APT deb822 .sources file.
// Returns the file content and the path where the GPG key should be written.
func renderAPTDeb822(repo *pb.RepositoryEntry, header string) (content []byte, gpgKeyPath string, err error) {
	if repo.GetAptUri() == "" {
		return nil, "", fmt.Errorf("apt_uri is required")
	}
	if repo.GetAptSuites() == "" {
		return nil, "", fmt.Errorf("apt_suites is required")
	}
	if repo.GetAptComponents() == "" {
		return nil, "", fmt.Errorf("apt_components is required")
	}

	gpgKeyPath = filepath.Join(aptGPGDir, "bor-"+repo.GetId()+".gpg")

	var buf bytes.Buffer
	buf.WriteString(header)
	fmt.Fprintf(&buf, "Types: deb\n")
	fmt.Fprintf(&buf, "URIs: %s\n", repo.GetAptUri())
	fmt.Fprintf(&buf, "Suites: %s\n", repo.GetAptSuites())
	fmt.Fprintf(&buf, "Components: %s\n", repo.GetAptComponents())
	if repo.GetAptArchitectures() != "" {
		fmt.Fprintf(&buf, "Architectures: %s\n", repo.GetAptArchitectures())
	}
	if repo.GetEnabled() {
		fmt.Fprintf(&buf, "Enabled: yes\n")
	} else {
		fmt.Fprintf(&buf, "Enabled: no\n")
	}
	if len(repo.GetGpgKeyData()) > 0 {
		fmt.Fprintf(&buf, "Signed-By: %s\n", gpgKeyPath)
	}

	return buf.Bytes(), gpgKeyPath, nil
}

// renderDNFRepo renders a DNF/YUM .repo INI file.
func renderDNFRepo(repo *pb.RepositoryEntry, header, gpgFilePath string) ([]byte, error) {
	if repo.GetBaseurl() == "" && repo.GetMirrorlist() == "" {
		return nil, fmt.Errorf("baseurl or mirrorlist is required")
	}

	var buf bytes.Buffer
	buf.WriteString(header)
	fmt.Fprintf(&buf, "[bor-%s]\n", repo.GetId())
	fmt.Fprintf(&buf, "name=%s\n", repo.GetName())
	if repo.GetBaseurl() != "" {
		fmt.Fprintf(&buf, "baseurl=%s\n", repo.GetBaseurl())
	}
	if repo.GetMirrorlist() != "" {
		fmt.Fprintf(&buf, "mirrorlist=%s\n", repo.GetMirrorlist())
	}
	if repo.GetEnabled() {
		fmt.Fprintf(&buf, "enabled=1\n")
	} else {
		fmt.Fprintf(&buf, "enabled=0\n")
	}
	if repo.GetGpgCheck() {
		fmt.Fprintf(&buf, "gpgcheck=1\n")
	} else {
		fmt.Fprintf(&buf, "gpgcheck=0\n")
	}
	if len(repo.GetGpgKeyData()) > 0 {
		fmt.Fprintf(&buf, "gpgkey=file://%s\n", gpgFilePath)
	}
	if repo.GetMetadataExpire() > 0 {
		fmt.Fprintf(&buf, "metadata_expire=%d\n", repo.GetMetadataExpire())
	}

	return buf.Bytes(), nil
}

// renderZypperRepo renders a Zypper .repo INI file.
func renderZypperRepo(repo *pb.RepositoryEntry, header, gpgFilePath string) ([]byte, error) {
	if repo.GetBaseurl() == "" {
		return nil, fmt.Errorf("baseurl is required")
	}

	var buf bytes.Buffer
	buf.WriteString(header)
	fmt.Fprintf(&buf, "[bor-%s]\n", repo.GetId())
	fmt.Fprintf(&buf, "name=%s\n", repo.GetName())
	fmt.Fprintf(&buf, "baseurl=%s\n", repo.GetBaseurl())
	if repo.GetEnabled() {
		fmt.Fprintf(&buf, "enabled=1\n")
	} else {
		fmt.Fprintf(&buf, "enabled=0\n")
	}
	fmt.Fprintf(&buf, "autorefresh=1\n")
	fmt.Fprintf(&buf, "type=rpm-md\n")
	if repo.GetGpgCheck() {
		fmt.Fprintf(&buf, "gpgcheck=1\n")
	} else {
		fmt.Fprintf(&buf, "gpgcheck=0\n")
	}
	if len(repo.GetGpgKeyData()) > 0 {
		fmt.Fprintf(&buf, "gpgkey=file://%s\n", gpgFilePath)
	}
	priority := repo.GetZypperPriority()
	if priority <= 0 {
		priority = 99
	}
	fmt.Fprintf(&buf, "priority=%d\n", priority)

	return buf.Bytes(), nil
}

// atomicWrite writes content to path by writing to a temp file then renaming.
func atomicWrite(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: repo dirs must be world-readable
		return fmt.Errorf("create dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bor-repo-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil { //nolint:gosec // G302: repo files must be world-readable
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// repoIDFromPath extracts the repo id from a bor-managed path.
// e.g. /etc/apt/sources.list.d/bor-google-chrome.sources → "google-chrome"
func repoIDFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "bor-")
	// Remove extension.
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	return base
}
