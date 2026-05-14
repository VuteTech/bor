// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import "os"

// RepoFormat identifies which package manager's repository file format to use.
// This is used exclusively for writing repository configuration files; package
// installation always goes through PackageKit D-Bus regardless of this value.
type RepoFormat int

const (
	// RepoFormatUnknown indicates no supported package manager was detected.
	RepoFormatUnknown RepoFormat = iota
	// RepoFormatAPT writes deb822 .sources files to /etc/apt/sources.list.d/.
	RepoFormatAPT
	// RepoFormatDNF writes .repo INI files to /etc/yum.repos.d/.
	RepoFormatDNF
	// RepoFormatZypper writes .repo INI files to /etc/zypp/repos.d/.
	RepoFormatZypper
)

// DetectRepoFormat returns the repository file format appropriate for this
// node by checking for well-known package manager binaries. The result
// should be cached at startup; repeated calls read the filesystem each time.
func DetectRepoFormat() RepoFormat {
	switch {
	case fileExists("/usr/bin/apt-get") || fileExists("/usr/bin/apt"):
		return RepoFormatAPT
	case fileExists("/usr/bin/dnf") || fileExists("/usr/bin/dnf5") || fileExists("/usr/bin/yum"):
		return RepoFormatDNF
	case fileExists("/usr/bin/zypper"):
		return RepoFormatZypper
	default:
		return RepoFormatUnknown
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
