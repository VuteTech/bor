// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

	data, err := os.ReadFile(targetPath)
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

	data, err := os.ReadFile(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to restore
		}
		return fmt.Errorf("failed to read backup %s: %w", backupPath, err)
	}

	if len(data) == 0 {
		// Empty sentinel — no original existed; remove the managed file.
		os.Remove(targetPath)
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
