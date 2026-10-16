// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Desktop layout and branding. The agent renders one look-and-feel
// package per node into the data directory (first in XDG_DATA_DIRS):
//
//	plasma/look-and-feel/org.bor.<policy>.desktop/metadata.json
//	plasma/look-and-feel/org.bor.<policy>.desktop/contents/defaults
//	plasma/look-and-feel/org.bor.<policy>.desktop/contents/layouts/org.kde.plasma.desktop-layout.js
//	plasma/shells/org.kde.plasma.desktop/contents/updates/bor-<hash>.js
//
// Plasma reads the wallpaper and the layout from the selected package when
// a user logs in for the first time; the update script runs once per
// existing user (plasmashell records it in plasmashellrc [Updates]).
const (
	layoutPackagePrefix = "org.bor."
	layoutPackageSuffix = ".desktop"
	lookAndFeelDir      = "plasma/look-and-feel"
	shellUpdatesDir     = "plasma/shells/org.kde.plasma.desktop/contents/updates"
	layoutScriptPath    = "contents/layouts/org.kde.plasma.desktop-layout.js"
	defaultFillMode     = "2"
)

// LayoutHasContent reports whether a layout asks for anything at all.
func LayoutHasContent(l *pb.KConfigLayout) bool {
	return l != nil && (strings.TrimSpace(l.GetWallpaperImage()) != "" || strings.TrimSpace(l.GetLayoutScript()) != "")
}

// LayoutPackageID returns the look-and-feel package id generated for a
// policy: org.bor.<policy id letters and digits, up to 12>.desktop.
func LayoutPackageID(policyID string) string {
	var b strings.Builder
	for _, r := range policyID {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(unicode.ToLower(r))
			if b.Len() >= 12 {
				break
			}
		}
	}
	id := b.String()
	if id == "" {
		id = "policy"
	}
	return layoutPackagePrefix + id + layoutPackageSuffix
}

// WallpaperURL normalises an image reference to the file:// form KDE's
// image wallpaper plugin stores. Wallpaper package names are kept as is.
func WallpaperURL(image string) string {
	image = strings.TrimSpace(image)
	switch {
	case image == "", strings.Contains(image, "://"):
		return image
	case strings.HasPrefix(image, "/"):
		return "file://" + image
	}
	return image
}

// WallpaperPath returns the local path for a wallpaper reference, for
// tools that take a file rather than a URL.
func WallpaperPath(image string) string {
	return strings.TrimPrefix(strings.TrimSpace(image), "file://")
}

// WallpaperFillModeName maps KDE's numeric FillMode to the name that
// plasma-apply-wallpaperimage accepts; "" when the tool has no name for it.
func WallpaperFillModeName(mode string) string {
	switch strings.TrimSpace(mode) {
	case "0":
		return "stretch"
	case "1":
		return "preserveAspectFit"
	case "", "2":
		return "preserveAspectCrop"
	}
	return ""
}

// lnfMetadata is the KPackage metadata of a look-and-feel package.
type lnfMetadata struct {
	KPackageStructure string    `json:"KPackageStructure"`
	KPlugin           lnfPlugin `json:"KPlugin"`
	APIVersion        string    `json:"X-Plasma-APIVersion"`
}

type lnfPlugin struct {
	Authors     []lnfAuthor `json:"Authors"`
	Description string      `json:"Description"`
	ID          string      `json:"Id"`
	License     string      `json:"License"`
	Name        string      `json:"Name"`
	Version     string      `json:"Version"`
}

type lnfAuthor struct {
	Name string `json:"Name"`
}

// RenderLayoutFiles renders the data-directory files for a layout, keyed by
// path relative to the data directory. Nil when the layout has no content.
func RenderLayoutFiles(layout *pb.KConfigLayout, policyID, policyName string) map[string][]byte {
	if !LayoutHasContent(layout) {
		return nil
	}
	pkg := LayoutPackageID(policyID)
	base := lookAndFeelDir + "/" + pkg
	wallpaper := WallpaperURL(layout.GetWallpaperImage())
	fill := strings.TrimSpace(layout.GetWallpaperFillMode())
	if fill == "" {
		fill = defaultFillMode
	}
	script := strings.TrimSpace(layout.GetLayoutScript())

	files := make(map[string][]byte, 4)

	name := strings.TrimSpace(policyName)
	if name == "" {
		name = "Managed desktop"
	}
	meta, _ := json.MarshalIndent(lnfMetadata{ //nolint:errcheck // plain struct, cannot fail
		KPackageStructure: "Plasma/LookAndFeel",
		KPlugin: lnfPlugin{
			Authors:     []lnfAuthor{{Name: "Bor"}},
			Description: "Desktop look deployed by Bor policy " + name,
			ID:          pkg,
			License:     "Proprietary",
			Name:        name + " (managed)",
			Version:     "1",
		},
		APIVersion: "2",
	}, "", "    ")
	files[base+"/metadata.json"] = append(meta, '\n')

	var defaults strings.Builder
	defaults.WriteString("# Managed by Bor. Defaults of the deployed look-and-feel package.\n")
	if wallpaper != "" {
		fmt.Fprintf(&defaults, "[Wallpaper]\nImage=%s\n", wallpaper)
	}
	files[base+"/contents/defaults"] = []byte(defaults.String())

	if script != "" {
		files[base+"/"+layoutScriptPath] = []byte("// Managed by Bor. Desktop layout of policy " + name + ".\n" + script + "\n")
	}

	if layout.GetApplyOnce() {
		update := renderLayoutUpdateScript(wallpaper, fill, script, name)
		sum := sha256.Sum256(update)
		files[shellUpdatesDir+"/bor-"+hex.EncodeToString(sum[:6])+".js"] = update
	}
	return files
}

// renderLayoutUpdateScript builds the Plasma script that applies the
// deployed look once to an existing user: wallpaper on every desktop and,
// when a layout script is present, the panels replaced by that layout.
func renderLayoutUpdateScript(wallpaper, fill, script, name string) []byte {
	var b strings.Builder
	b.WriteString("// Managed by Bor. Applied once per user: desktop look of policy " + name + ".\n")
	if wallpaper != "" {
		fmt.Fprintf(&b, "var borImage = %s;\nvar borFillMode = %s;\n", jsString(wallpaper), fill)
		b.WriteString(`var borDesktops = desktops();
for (var i = 0; i < borDesktops.length; i++) {
    var d = borDesktops[i];
    d.wallpaperPlugin = "org.kde.image";
    d.currentConfigGroup = ["Wallpaper", "org.kde.image", "General"];
    d.writeConfig("Image", borImage);
    d.writeConfig("FillMode", borFillMode);
}
`)
	}
	if script != "" {
		b.WriteString(`// Replace the user's panels with the policy layout.
var borPanels = panels();
for (var p = 0; p < borPanels.length; p++) {
    borPanels[p].remove();
}
`)
		b.WriteString(script)
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// jsString renders s as a JavaScript string literal.
func jsString(s string) string {
	data, _ := json.Marshal(s) //nolint:errcheck // strings always marshal
	return string(data)
}

// ListDataFiles returns the absolute paths of every regular file under
// dataPath. The data directory belongs to Bor, so everything in it is
// managed. Missing directory: no files.
func ListDataFiles(dataPath string) []string {
	var out []string
	_ = filepath.WalkDir(dataPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best effort: skip unreadable entries
		}
		if d.Type().IsRegular() {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// SyncDataFiles makes the data directory contain exactly files (paths
// relative to dataPath). Unchanged files are left alone; stale files are
// removed and empty directories pruned. It returns the absolute paths it
// wrote or removed. An empty map clears the directory.
func SyncDataFiles(dataPath string, files map[string][]byte) ([]string, error) {
	clean := filepath.Clean(dataPath)
	if !filepath.IsAbs(clean) || clean == "/" {
		return nil, fmt.Errorf("refusing to manage data directory %q", dataPath)
	}

	desired := make(map[string][]byte, len(files))
	for rel, data := range files {
		abs := filepath.Join(clean, rel)
		if !strings.HasPrefix(abs, clean+string(filepath.Separator)) {
			return nil, fmt.Errorf("data file %q escapes %s", rel, clean)
		}
		desired[abs] = data
	}

	var changed []string
	for _, existing := range ListDataFiles(clean) {
		if _, keep := desired[existing]; keep {
			continue
		}
		if err := os.Remove(existing); err != nil && !os.IsNotExist(err) {
			return changed, fmt.Errorf("failed to remove stale data file %s: %w", existing, err)
		}
		changed = append(changed, existing)
	}

	for abs, data := range desired {
		current, err := os.ReadFile(abs) //nolint:gosec // G304: path validated to be inside the data directory
		if err == nil && bytes.Equal(current, data) {
			continue
		}
		if err := WriteFileAtomically(abs, data); err != nil {
			return changed, fmt.Errorf("failed to write data file %s: %w", abs, err)
		}
		changed = append(changed, abs)
	}

	pruneEmptyDirs(clean)
	sort.Strings(changed)
	return changed, nil
}

// pruneEmptyDirs removes empty directories below root (root itself stays).
func pruneEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	// Deepest first.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(d)
		}
	}
}
