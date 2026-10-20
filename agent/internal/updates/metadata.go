// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"os"
	"path/filepath"
	"time"
)

// MetadataRefreshTime finds when the package metadata was last refreshed
// from the package manager's cache on disk, for nodes whose timers have
// not fired yet or whose engine has none: apt's success stamp and lists,
// dnf's repodata caches, zypper's raw cache. Zero when nothing is found.
func MetadataRefreshTime(p *Platform) time.Time {
	var candidates []string
	switch p.Engine {
	case EngineApt:
		candidates = append(candidates, p.Path("/var/lib/apt/periodic/update-success-stamp"))
		candidates = append(candidates, globAll(p.Path("/var/lib/apt/lists/*_Packages"))...)
	case EngineDnf:
		candidates = append(candidates, globAll(p.Path("/var/cache/libdnf5/*/repodata/*.xml*"))...)
		candidates = append(candidates, globAll(p.Path("/var/cache/dnf/*/repodata/*.xml*"))...)
	case EngineOsUpdate, EngineTransactional:
		candidates = append(candidates, globAll(p.Path("/var/cache/zypp/raw/*/repodata/repomd.xml"))...)
		candidates = append(candidates, globAll(p.Path("/var/cache/zypp/raw/*/*/repodata/repomd.xml"))...)
	}
	var newest time.Time
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	return newest
}

func globAll(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	return matches
}
