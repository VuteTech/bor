// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// BackupSuffix is appended to the original file path to create a backup.
const BackupSuffix = ".bor-backup"

// ManagedFileHeader is prepended to every file written by SyncKConfigFiles.
const ManagedFileHeader = "# This file is managed by Bor. Do not edit manually.\n# Changes will be overwritten by policy enforcement.\n\n"

// kconfigGroup holds entries for a single INI [Group] within a file.
type kconfigGroup struct {
	name    string
	entries []*pb.KConfigEntry
}

// enforcedSet builds a fast-lookup set from the KConfigPolicy.EnforcedFields list.
func enforcedSet(pol *pb.KConfigPolicy) map[string]bool {
	s := make(map[string]bool, len(pol.EnforcedFields))
	for _, f := range pol.EnforcedFields {
		s[f] = true
	}
	return s
}

// boolVal converts an optional bool proto pointer to an INI "true"/"false" string.
func boolVal(v *bool) string {
	if v != nil && *v {
		return "true"
	}
	return "false"
}

// KConfigPolicyToEntries converts a typed KConfigPolicy to the flat
// []*KConfigEntry slice expected by MergeKConfigEntries and SplitKCMRestrictions.
// Absent optional fields (nil pointers, empty repeated) are skipped.
func KConfigPolicyToEntries(pol *pb.KConfigPolicy) []*pb.KConfigEntry {
	if pol == nil {
		return nil
	}

	enforced := enforcedSet(pol)

	var entries []*pb.KConfigEntry
	add := func(e *pb.KConfigEntry) {
		if e != nil {
			entries = append(entries, e)
		}
	}

	boolE := func(file, group, key, jsonKey string, val *bool) *pb.KConfigEntry {
		if val == nil {
			return nil
		}
		return &pb.KConfigEntry{File: file, Group: group, Key: key, Value: boolVal(val), Type: "bool", Enforced: enforced[jsonKey]}
	}

	strE := func(file, group, key, jsonKey string, val *string) *pb.KConfigEntry {
		if val == nil {
			return nil
		}
		return &pb.KConfigEntry{File: file, Group: group, Key: key, Value: *val, Type: "string", Enforced: enforced[jsonKey]}
	}

	intE := func(file, group, key, jsonKey string, val *int32) *pb.KConfigEntry {
		if val == nil {
			return nil
		}
		return &pb.KConfigEntry{File: file, Group: group, Key: key, Value: strconv.Itoa(int(*val)), Type: "int", Enforced: enforced[jsonKey]}
	}

	// Action Restrictions (kdeglobals, [KDE Action Restrictions])
	add(boolE("kdeglobals", "KDE Action Restrictions", "shell_access", "shellAccess", pol.ShellAccess))
	add(boolE("kdeglobals", "KDE Action Restrictions", "run_command", "runCommand", pol.RunCommand))
	add(boolE("kdeglobals", "KDE Action Restrictions", "action/logout", "actionLogout", pol.ActionLogout))
	add(boolE("kdeglobals", "KDE Action Restrictions", "action/file_new", "actionFileNew", pol.ActionFileNew))
	add(boolE("kdeglobals", "KDE Action Restrictions", "action/file_open", "actionFileOpen", pol.ActionFileOpen))
	add(boolE("kdeglobals", "KDE Action Restrictions", "action/file_save", "actionFileSave", pol.ActionFileSave))

	// Resource Restrictions (kdeglobals, [KDE Resource Restrictions])
	add(boolE("kdeglobals", "KDE Resource Restrictions", "wallpaper", "restrictWallpaper", pol.RestrictWallpaper))
	add(boolE("kdeglobals", "KDE Resource Restrictions", "icons", "restrictIcons", pol.RestrictIcons))
	add(boolE("kdeglobals", "KDE Resource Restrictions", "autostart", "restrictAutostart", pol.RestrictAutostart))
	add(boolE("kdeglobals", "KDE Resource Restrictions", "colors", "restrictColors", pol.RestrictColors))
	add(boolE("kdeglobals", "KDE Resource Restrictions", "cursors", "restrictCursors", pol.RestrictCursors))

	// Window Manager (kwinrc, [Windows])
	add(boolE("kwinrc", "Windows", "BorderlessMaximizedWindows", "borderlessMaximizedWindows", pol.BorderlessMaximizedWindows))

	// Desktop (plasmarc, [General])
	add(boolE("plasmarc", "General", "plasmoidUnlockedDesktop", "plasmoidUnlockedDesktop", pol.PlasmoidUnlockedDesktop))
	add(boolE("plasmarc", "General", "allow_configure_when_locked", "allowConfigureWhenLocked", pol.AllowConfigureWhenLocked))

	// Screen Lock (kscreenlockerrc, [Daemon])
	add(boolE("kscreenlockerrc", "Daemon", "AutoLock", "autoLock", pol.AutoLock))
	add(boolE("kscreenlockerrc", "Daemon", "LockOnResume", "lockOnResume", pol.LockOnResume))
	add(intE("kscreenlockerrc", "Daemon", "Timeout", "lockTimeout", pol.LockTimeout))

	// Appearance
	add(strE("kdeglobals", "Icons", "Theme", "iconTheme", pol.IconTheme))
	add(strE("plasma-org.kde.plasma.desktop-appletsrc", "Containments][1", "wallpaperplugin", "wallpaperPlugin", pol.WallpaperPlugin))
	add(strE("plasma-org.kde.plasma.desktop-appletsrc", "Containments][1][Wallpaper][org.kde.image][General", "Image", "wallpaperImage", pol.WallpaperImage))
	add(strE("plasma-org.kde.plasma.desktop-appletsrc", "Containments][1][Wallpaper][org.kde.image][General", "FillMode", "wallpaperFillMode", pol.WallpaperFillMode))
	add(strE("plasma-org.kde.plasma.desktop-appletsrc", "Containments][1][Wallpaper][org.kde.image][General", "Color", "wallpaperColor", pol.WallpaperColor))

	// URL Restrictions (kdeglobals, [KDE URL Restrictions]) — always enforced
	for i, r := range pol.UrlRestrictions {
		val := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%v",
			r.GetAction(), r.GetReferrerProtocol(), r.GetReferrerHost(), r.GetReferrerPath(),
			r.GetProtocol(), r.GetHost(), r.GetPath(), r.GetEnabled())
		entries = append(entries, &pb.KConfigEntry{
			File:     "kdeglobals",
			Group:    "KDE URL Restrictions",
			Key:      fmt.Sprintf("rule_%d", i+1),
			Value:    val,
			Type:     "string",
			Enforced: true,
		})
	}
	if len(pol.UrlRestrictions) > 0 {
		entries = append(entries, &pb.KConfigEntry{
			File:     "kdeglobals",
			Group:    "KDE URL Restrictions",
			Key:      "rule_count",
			Value:    strconv.Itoa(len(pol.UrlRestrictions)),
			Type:     "string",
			Enforced: true,
		})
	}

	// KCM Restrictions (kde5rc, [KDE Control Module Restrictions]) — always enforced
	for _, mod := range pol.KcmRestrictions {
		entries = append(entries, &pb.KConfigEntry{
			File:     "kde5rc",
			Group:    "KDE Control Module Restrictions",
			Key:      mod,
			Value:    "false",
			Type:     "bool",
			Enforced: true,
		})
	}

	return entries
}

// MergeKConfigEntries takes already-parsed proto entries (flattened from
// all policies), groups them by target file and INI group, renders INI
// content with [$i] enforcement suffixes, and returns a map of file→INI bytes.
func MergeKConfigEntries(entries []*pb.KConfigEntry) (map[string][]byte, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Group entries by file, then by group name within each file.
	// Use ordered maps to produce deterministic output.
	type fileData struct {
		groups map[string]*kconfigGroup
		order  []string // insertion order of group names
	}
	files := make(map[string]*fileData)
	var fileOrder []string

	for _, e := range entries {
		fd, ok := files[e.File]
		if !ok {
			fd = &fileData{groups: make(map[string]*kconfigGroup)}
			files[e.File] = fd
			fileOrder = append(fileOrder, e.File)
		}
		g, ok := fd.groups[e.Group]
		if !ok {
			g = &kconfigGroup{name: e.Group}
			fd.groups[e.Group] = g
			fd.order = append(fd.order, e.Group)
		}
		g.entries = append(g.entries, e)
	}

	sort.Strings(fileOrder)

	// Renumber URL restriction rules when multiple policies contribute
	// rule_N entries to the same [KDE URL Restrictions] group.
	for _, fd := range files {
		for _, g := range fd.groups {
			if g.name == "KDE URL Restrictions" {
				renumberURLRestrictions(g)
			}
		}
	}

	result := make(map[string][]byte, len(files))
	for _, fileName := range fileOrder {
		fd := files[fileName]
		var buf strings.Builder

		sortedGroups := make([]string, len(fd.order))
		copy(sortedGroups, fd.order)
		sort.Strings(sortedGroups)

		for i, groupName := range sortedGroups {
			g := fd.groups[groupName]
			if i > 0 {
				buf.WriteString("\n")
			}
			renderINIGroup(&buf, g)
		}

		result[fileName] = []byte(buf.String())
	}

	return result, nil
}

// renderINIGroup writes a single INI group to the builder.
// If all entries in the group are enforced, the group header uses [$i].
// If only some entries are enforced, key-level [$i] suffixes are used.
func renderINIGroup(buf *strings.Builder, g *kconfigGroup) {
	allEnforced := true
	anyEnforced := false
	for _, e := range g.entries {
		if e.Enforced {
			anyEnforced = true
		} else {
			allEnforced = false
		}
	}

	// Write group header.
	if allEnforced && anyEnforced {
		fmt.Fprintf(buf, "[%s][$i]\n", g.name)
	} else {
		fmt.Fprintf(buf, "[%s]\n", g.name)
	}

	// Sort entries by key for deterministic output.
	sorted := make([]*pb.KConfigEntry, len(g.entries))
	copy(sorted, g.entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Key < sorted[j].Key
	})

	for _, e := range sorted {
		if !allEnforced && e.Enforced {
			// Key-level enforcement.
			fmt.Fprintf(buf, "%s[$i]=%s\n", e.Key, e.Value)
		} else {
			fmt.Fprintf(buf, "%s=%s\n", e.Key, e.Value)
		}
	}
}

// BackupOriginal creates a backup of the original file before policy
// enforcement overwrites it. If a backup already exists it is never
// overwritten (idempotent). When no original file exists an empty
// sentinel backup is created so that cleanup knows to delete the
// managed file rather than restore content.
func BackupOriginal(targetPath string) error {
	backupPath := targetPath + BackupSuffix
	if _, err := os.Stat(backupPath); err == nil {
		return nil // backup already exists — never overwrite
	}

	data, err := os.ReadFile(targetPath) //nolint:gosec // G304: path comes from server-managed policy config
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read original file %s: %w", targetPath, err)
		}
		// No original file — write empty sentinel so cleanup can delete.
		data = nil
	}

	if err := WriteFileAtomically(backupPath, data); err != nil {
		return fmt.Errorf("failed to write backup %s: %w", backupPath, err)
	}
	return nil
}

// RestoreOriginal restores the original file from its backup. If the
// backup is an empty sentinel the managed file is deleted (no original
// existed). If no backup exists the call is a no-op.
func RestoreOriginal(targetPath string) error {
	backupPath := targetPath + BackupSuffix

	data, err := os.ReadFile(backupPath) //nolint:gosec // G304: backup path derived from managed config path
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to restore
		}
		return fmt.Errorf("failed to read backup %s: %w", backupPath, err)
	}

	if len(data) == 0 {
		// Empty sentinel — no original existed; remove the managed file.
		_ = os.Remove(targetPath)
	} else {
		if err := WriteFileAtomically(targetPath, data); err != nil {
			return fmt.Errorf("failed to restore %s: %w", targetPath, err)
		}
	}

	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("failed to remove backup %s: %w", backupPath, err)
	}
	return nil
}

// ManagedFiles scans basePath for .bor-backup files and returns the
// base filenames (without the suffix) that are currently managed.
func ManagedFiles(basePath string) ([]string, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", basePath, err)
	}

	var managed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, BackupSuffix) {
			managed = append(managed, strings.TrimSuffix(name, BackupSuffix))
		}
	}
	return managed, nil
}

// SyncKConfigFiles synchronises the managed KConfig files under basePath
// with the desired state expressed by files. Files present in the desired
// state are backed up (first write only) and written with a managed
// header. Files that were previously managed but are no longer in the
// desired state are restored from their backups.
//
// Passing a nil or empty files map causes all previously managed files
// to be restored (full cleanup).
func SyncKConfigFiles(basePath string, files map[string][]byte) error {
	managed, err := ManagedFiles(basePath)
	if err != nil {
		return err
	}

	// Build set of previously managed filenames.
	prev := make(map[string]bool, len(managed))
	for _, name := range managed {
		prev[name] = true
	}

	// Write desired files.
	for name, data := range files {
		target := filepath.Join(basePath, name)
		if err := BackupOriginal(target); err != nil {
			return fmt.Errorf("failed to backup %s: %w", name, err)
		}

		withHeader := append([]byte(ManagedFileHeader), data...)
		if err := WriteFileAtomically(target, withHeader); err != nil {
			return fmt.Errorf("failed to write KConfig file %s: %w", name, err)
		}

		delete(prev, name) // still active — don't restore
	}

	// Restore files that are no longer in the desired state.
	for name := range prev {
		target := filepath.Join(basePath, name)
		if err := RestoreOriginal(target); err != nil {
			return fmt.Errorf("failed to restore %s: %w", name, err)
		}
	}

	return nil
}

// parseRuleNum extracts the numeric index from a "rule_N" key.
// Returns -1 if the key does not match the pattern.
func parseRuleNum(key string) int {
	if !strings.HasPrefix(key, "rule_") {
		return -1
	}
	n, err := strconv.Atoi(key[len("rule_"):])
	if err != nil {
		return -1
	}
	return n
}

// renumberURLRestrictions collects all rule_N entries in a
// [KDE URL Restrictions] group, renumbers them sequentially starting
// from rule_1, and sets a single rule_count entry with the total.
// Non-rule entries (other than rule_count) are preserved.
func renumberURLRestrictions(g *kconfigGroup) {
	type indexedRule struct {
		origNum int
		entry   *pb.KConfigEntry
	}

	var rules []indexedRule
	var other []*pb.KConfigEntry

	for _, e := range g.entries {
		if e.Key == "rule_count" {
			continue // drop old rule_count — we'll regenerate it
		}
		n := parseRuleNum(e.Key)
		if n > 0 {
			rules = append(rules, indexedRule{origNum: n, entry: e})
		} else {
			other = append(other, e)
		}
	}

	// Stable sort by original index so that rules from different
	// policies with the same index maintain insertion order.
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].origNum < rules[j].origNum
	})

	// Renumber sequentially.
	result := make([]*pb.KConfigEntry, 0, len(other)+len(rules)+1)
	result = append(result, other...)
	for i, r := range rules {
		r.entry.Key = fmt.Sprintf("rule_%d", i+1)
		result = append(result, r.entry)
	}

	// Add rule_count if there are any rules.
	if len(rules) > 0 {
		result = append(result, &pb.KConfigEntry{
			File:     g.entries[0].File,
			Group:    g.name,
			Key:      "rule_count",
			Value:    strconv.Itoa(len(rules)),
			Enforced: g.entries[0].Enforced,
		})
	}

	g.entries = result
}

// kcmRestrictionPaths are the system-wide KDE config files where KCM
// (Control Module) restrictions must be written. These live in /etc/
// directly rather than in the XDG overlay because KDE reads them as
// system-level immutable config.
var kcmRestrictionPaths = []string{"/etc/kde5rc", "/etc/kde6rc"}

// SplitKCMRestrictions separates KCM restriction entries from other
// KConfig entries. Entries with file="kde5rc" and group="KDE Control
// Module Restrictions" are returned in kcm; everything else in other.
func SplitKCMRestrictions(entries []*pb.KConfigEntry) (kcm, other []*pb.KConfigEntry) {
	for _, e := range entries {
		if e.File == "kde5rc" && e.Group == "KDE Control Module Restrictions" {
			kcm = append(kcm, e)
		} else {
			other = append(other, e)
		}
	}
	return
}

// SyncKCMRestrictions writes KCM restriction INI content to the system-wide
// /etc/kde5rc and /etc/kde6rc files with backup/restore support and a
// managed-file header. Passing nil content restores all previously backed-up
// originals.
func SyncKCMRestrictions(content []byte) error {
	for _, path := range kcmRestrictionPaths {
		if content == nil {
			if err := RestoreOriginal(path); err != nil {
				return fmt.Errorf("failed to restore %s: %w", path, err)
			}
			continue
		}

		if err := BackupOriginal(path); err != nil {
			return fmt.Errorf("failed to backup %s: %w", path, err)
		}

		withHeader := append([]byte(ManagedFileHeader), content...)
		if err := WriteFileAtomically(path, withHeader); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}
	return nil
}

// profileScriptPath is the path to the login profile script that
// prepends the Bor XDG config directory to XDG_CONFIG_DIRS.
const profileScriptPath = "/etc/profile.d/99-bor.sh"

// profileScriptContent returns the shell script that prepends basePath
// to XDG_CONFIG_DIRS so that KDE (and other XDG-aware apps) pick up
// the Bor-managed config files.
func profileScriptContent(basePath string) string {
	return fmt.Sprintf("export XDG_CONFIG_DIRS=%s:${XDG_CONFIG_DIRS:-/etc/xdg}\nreadonly XDG_CONFIG_DIRS\n", basePath)
}

// EnsureProfileScript creates or updates /etc/profile.d/99-bor.sh so
// that the Bor XDG config directory is prepended to XDG_CONFIG_DIRS
// for all login sessions.
func EnsureProfileScript(basePath string) error {
	desired := profileScriptContent(basePath)

	existing, err := os.ReadFile(profileScriptPath)
	if err == nil && string(existing) == desired {
		return nil // already up to date
	}

	if err := os.MkdirAll(filepath.Dir(profileScriptPath), 0o755); err != nil { //nolint:gosec // G301: profile.d must be world-readable
		return fmt.Errorf("failed to create profile.d directory: %w", err)
	}

	if err := WriteFileAtomically(profileScriptPath, []byte(desired)); err != nil {
		return fmt.Errorf("failed to write %s: %w", profileScriptPath, err)
	}

	// Ensure the script is executable.
	if err := os.Chmod(profileScriptPath, 0o755); err != nil { //nolint:gosec // G302: profile script must be executable
		return fmt.Errorf("failed to chmod %s: %w", profileScriptPath, err)
	}

	return nil
}
