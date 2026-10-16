// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Group-scoped KDE policies. A policy limited to Unix groups is rendered
// into <config dir>/groups/<slug>/ and the login hook adds that directory
// to XDG_CONFIG_DIRS, before the node-wide directory, for members of the
// group. KDE parses the less specific files first, so a group directory
// overrides node-wide defaults but cannot relax node-wide enforced ([$i])
// entries.

// GroupOverlayDir is the subdirectory of the config directory that holds
// the per-group overlays.
const GroupOverlayDir = "groups"

// KConfigGroupScope is one Unix group that scoped policies target.
type KConfigGroupScope struct {
	Name      string
	Slug      string
	Priority  int32 // highest priority among the policies in the scope
	PolicyIDs []string
}

// GroupDir is a resolved per-group overlay for the login hook: the config
// directory (XDG_CONFIG_DIRS) and the data directory (XDG_DATA_DIRS) of
// the group, and the update script of its enforced desktop layout, if any
// (see EnvHook.Reapply).
type GroupDir struct {
	GID     uint32
	Slug    string
	Dir     string
	DataDir string
	// Layout reports that the group deploys a desktop layout of its own:
	// members get its update scripts instead of the node-wide ones.
	Layout  bool
	Reapply string
}

// GroupSlug derives a directory name from a group name: lowercase, with
// everything outside [a-z0-9._-] replaced by '_'. Names that needed
// changing get a short hash suffix so that "Domain Users" and
// "domain_users" stay distinct.
func GroupSlug(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	slug := strings.Trim(b.String(), "._-")
	if slug == "" {
		slug = "group"
	}
	if slug != lower {
		sum := sha256.Sum256([]byte(lower))
		slug += "-" + hex.EncodeToString(sum[:3])
	}
	return slug
}

// uniqueGroupNames trims, drops empties and de-duplicates (by slug).
func uniqueGroupNames(groups []string) []string {
	seen := make(map[string]bool, len(groups))
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		slug := GroupSlug(g)
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, g)
	}
	return out
}

// LookupGroupGID resolves a group name through NSS (local files, LDAP,
// SSSD, winbind) to its numeric id.
func LookupGroupGID(name string) (uint32, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	gid, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("group %q has a non-numeric gid %q", name, g.Gid)
	}
	return uint32(gid), nil
}

// ResolveGroupDirs turns the merge's group scopes into hook directories,
// keeping their order. Groups the node cannot resolve are returned
// separately; their files are still written but not activated.
func ResolveGroupDirs(configPath, dataPath string, scopes []KConfigGroupScope, lookup func(string) (uint32, error)) (dirs []GroupDir, unresolved []KConfigGroupScope) {
	for _, s := range scopes {
		gid, err := lookup(s.Name)
		if err != nil {
			unresolved = append(unresolved, s)
			continue
		}
		dirs = append(dirs, GroupDir{
			GID:     gid,
			Slug:    s.Slug,
			Dir:     filepath.Join(configPath, GroupOverlayDir, s.Slug),
			DataDir: filepath.Join(dataPath, GroupOverlayDir, s.Slug),
		})
	}
	return dirs, unresolved
}

// UserInGroup reports whether username belongs to the group with the given
// numeric id, primary or supplementary. It asks `id -G`, which goes through
// the node's name service like the login hook does, so local, LDAP and
// SSSD groups are all seen; an unknown user is in no group.
func UserInGroup(username string, gid uint32) bool {
	if username == "" || strings.ContainsAny(username, " \t\n/") {
		return false
	}
	out, err := exec.Command("id", "-G", "--", username).Output() //nolint:gosec // G204: fixed binary; the name comes from systemd's session files
	if err != nil {
		return false
	}
	want := strconv.FormatUint(uint64(gid), 10)
	for _, f := range strings.Fields(string(out)) {
		if f == want {
			return true
		}
	}
	return false
}

// GroupDataPrefix is the path, relative to the data directory, under which
// a group's application files live.
func GroupDataPrefix(slug string) string {
	return GroupOverlayDir + "/" + slug + "/"
}

// GroupOverlayRoot is the directory that holds all per-group overlays.
func GroupOverlayRoot(configPath string) string {
	return filepath.Join(configPath, GroupOverlayDir)
}

// ListGroupOverlayFiles returns every file in the per-group overlays.
func ListGroupOverlayFiles(configPath string) []string {
	return ListDataFiles(GroupOverlayRoot(configPath))
}

// SyncGroupOverlays makes <config dir>/groups/ contain exactly the given
// files (slug -> basename -> content), each with the managed header, and
// returns the paths it wrote or removed. An empty map clears the overlays.
func SyncGroupOverlays(configPath string, groupFiles map[string]map[string][]byte) ([]string, error) {
	flat := make(map[string][]byte)
	for slug, files := range groupFiles {
		for name, data := range files {
			flat[slug+"/"+name] = append([]byte(ManagedFileHeader), data...)
		}
	}
	return SyncDataFiles(GroupOverlayRoot(configPath), flat)
}
