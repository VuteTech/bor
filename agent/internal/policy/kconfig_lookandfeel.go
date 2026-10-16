// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// lookAndFeelDirs are the directories distributions and vendors install
// global themes into. The Bor data directory is left out on purpose: the
// packages Bor generates are selected through the policy, not by id.
var lookAndFeelDirs = []string{
	"/usr/share/plasma/look-and-feel",
	"/usr/local/share/plasma/look-and-feel",
}

// InstalledLookAndFeelPackages lists the global themes installed on this
// node with their display names, sorted by id.
func InstalledLookAndFeelPackages() []*pb.LookAndFeelPackage {
	return InstalledLookAndFeelPackagesFrom(lookAndFeelDirs)
}

// InstalledLookAndFeelPackagesFrom is InstalledLookAndFeelPackages with
// injectable locations. A package is a directory with metadata.json
// (Plasma 6, also most Plasma 5 themes) or metadata.desktop (older Plasma
// 5 themes); the id is the KPlugin id, or the directory name when the
// metadata has none.
func InstalledLookAndFeelPackagesFrom(dirs []string) []*pb.LookAndFeelPackage {
	seen := make(map[string]string)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id, name := lookAndFeelMetadata(filepath.Join(dir, e.Name()))
			if id == "" {
				id = e.Name()
			}
			if _, ok := seen[id]; !ok {
				seen[id] = name
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*pb.LookAndFeelPackage, 0, len(ids))
	for _, id := range ids {
		name := seen[id]
		if name == "" {
			name = id
		}
		out = append(out, &pb.LookAndFeelPackage{Id: id, Name: truncate(name, maxKcfgLabelLen)})
	}
	return out
}

// lookAndFeelMetadata reads the plugin id and name of a look-and-feel
// package directory from metadata.json or metadata.desktop.
func lookAndFeelMetadata(dir string) (id, name string) {
	if data, err := os.ReadFile(filepath.Join(dir, "metadata.json")); err == nil { //nolint:gosec // G304: fixed theme directories
		var meta struct {
			KPlugin struct {
				ID   string `json:"Id"`
				Name string `json:"Name"`
			} `json:"KPlugin"`
		}
		if json.Unmarshal(data, &meta) == nil {
			return strings.TrimSpace(meta.KPlugin.ID), strings.TrimSpace(meta.KPlugin.Name)
		}
		return "", ""
	}
	f, err := os.Open(filepath.Join(dir, "metadata.desktop")) //nolint:gosec // G304: fixed theme directories
	if err != nil {
		return "", ""
	}
	defer func() { _ = f.Close() }()
	inEntry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		switch {
		case strings.HasPrefix(line, "X-KDE-PluginInfo-Name="):
			id = strings.TrimSpace(strings.TrimPrefix(line, "X-KDE-PluginInfo-Name="))
		case strings.HasPrefix(line, "Name="):
			name = strings.TrimSpace(strings.TrimPrefix(line, "Name="))
		}
	}
	return id, name
}
