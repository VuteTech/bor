// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Application packs: files for KDE applications that live in the data
// directory (first in XDG_DATA_DIRS) rather than in a config file.
//
//	konsole/<Name>.profile       a Konsole profile, selected through
//	                             konsolerc [Desktop Entry] DefaultProfile
//	applications/<id>.desktop    a copy of an installed desktop entry with
//	                             NoDisplay=true, which shadows the original
//	                             for menus, KRunner and launchers
//
// Both exist once per scope: node-wide at the root of the data directory,
// per group under groups/<slug>/.
const (
	konsoleProfileDir         = "konsole"
	applicationsDir           = "applications"
	konsoleDefaultProfileName = "Managed"
	konsoleRC                 = "konsolerc"
	konsoleProfileHistoryMax  = 1_000_000
)

// konsoleLockedActions are the Konsole actions a locked profile restricts
// through [KDE Action Restrictions] (KXMLGUI hides unauthorised actions).
var konsoleLockedActions = []string{"manage-profiles", "edit-current-profile", "switch-profile"}

// desktopFileIDPattern is a desktop file id such as org.kde.konsole.desktop.
var desktopFileIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.desktop$`)

// desktopFileDirs are the places installed desktop entries come from, in
// the order the ids are looked up. The managed data directory is never
// consulted, so a shadow entry is always built from the real one.
var desktopFileDirs = []string{
	"/usr/local/share/applications",
	"/usr/share/applications",
	"/var/lib/flatpak/exports/share/applications",
	"/var/lib/snapd/desktop/applications",
}

// KConfigAppFiles are the application files of one scope.
type KConfigAppFiles struct {
	KonsoleProfile *pb.KonsoleProfile
	// KonsoleProfilePolicy is the name of the policy the profile came from
	// (written into the file header).
	KonsoleProfilePolicy string
	HiddenApplications   []string
}

// Empty reports whether the scope has no application files at all.
func (a KConfigAppFiles) Empty() bool {
	return !KonsoleProfileHasContent(a.KonsoleProfile) && len(a.HiddenApplications) == 0
}

// KonsoleProfileHasContent reports whether a profile asks for anything.
func KonsoleProfileHasContent(p *pb.KonsoleProfile) bool {
	if p == nil {
		return false
	}
	return strings.TrimSpace(p.GetCommand()) != "" ||
		strings.TrimSpace(p.GetHistoryMode()) != "" ||
		strings.TrimSpace(p.GetColorScheme()) != "" ||
		strings.TrimSpace(p.GetFont()) != "" ||
		strings.TrimSpace(p.GetExtraIni()) != "" ||
		p.GetSetDefault() || p.GetLock()
}

// KonsoleProfileName returns the profile's display and file name: the
// configured name reduced to letters, digits, space, dot, underscore and
// dash, or "Managed".
func KonsoleProfileName(p *pb.KonsoleProfile) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(p.GetName()) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '.' || r == '_' || r == '-') {
			b.WriteRune(r)
		}
	}
	name := strings.Trim(b.String(), " .")
	if name == "" {
		return konsoleDefaultProfileName
	}
	return name
}

// KonsoleProfileFile is the profile's file name, as DefaultProfile expects.
func KonsoleProfileFile(p *pb.KonsoleProfile) string {
	return KonsoleProfileName(p) + ".profile"
}

// RenderKonsoleProfile renders the profile file. Groups and keys follow
// Konsole's Profile.cpp; FALLBACK/ as parent inherits Konsole's built-in
// defaults for everything the profile does not set.
func RenderKonsoleProfile(p *pb.KonsoleProfile, policyName string) []byte {
	var b strings.Builder
	b.WriteString("# Managed by Bor. Do not edit manually.\n")
	if name := strings.TrimSpace(policyName); name != "" {
		fmt.Fprintf(&b, "# Konsole profile of policy %s.\n", strings.ReplaceAll(name, "\n", " "))
	}
	b.WriteString("\n[General]\n")
	fmt.Fprintf(&b, "Name=%s\n", KonsoleProfileName(p))
	b.WriteString("Parent=FALLBACK/\n")
	if cmd := iniValue(p.GetCommand()); cmd != "" {
		fmt.Fprintf(&b, "Command=%s\n", cmd)
	}

	mode := strings.TrimSpace(p.GetHistoryMode())
	if mode != "" {
		b.WriteString("\n[Scrolling]\n")
		fmt.Fprintf(&b, "HistoryMode=%s\n", mode)
		if mode == "1" {
			size := p.GetHistorySize()
			if size <= 0 {
				size = 1000
			}
			if size > konsoleProfileHistoryMax {
				size = konsoleProfileHistoryMax
			}
			fmt.Fprintf(&b, "HistorySize=%d\n", size)
		}
	}

	scheme, font := iniValue(p.GetColorScheme()), iniValue(p.GetFont())
	if scheme != "" || font != "" {
		b.WriteString("\n[Appearance]\n")
		if scheme != "" {
			fmt.Fprintf(&b, "ColorScheme=%s\n", scheme)
		}
		if font != "" {
			fmt.Fprintf(&b, "Font=%s\n", font)
		}
	}

	if extra := konsoleExtraINILines(p.GetExtraIni()); len(extra) > 0 {
		b.WriteString("\n")
		for _, line := range extra {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// iniValue trims a value and drops line breaks, which would end the entry.
func iniValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\r", "")
	return strings.ReplaceAll(v, "\n", " ")
}

// konsoleExtraINILines keeps the lines of the extra INI text that KConfig
// understands: blank lines, comments, [Group] headers and key=value.
func konsoleExtraINILines(text string) []string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "", strings.HasPrefix(line, "#"):
			out = append(out, line)
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) > 2:
			out = append(out, line)
		default:
			k, _, ok := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			if ok && k != "" && !strings.ContainsAny(k, "[]") {
				out = append(out, line)
			}
		}
	}
	// Trim leading and trailing blank lines.
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// KonsoleProfileEntries returns the config entries that activate a
// profile: konsolerc DefaultProfile (enforced when locked) and, when
// locked, the Kiosk restrictions on Konsole's profile actions.
func KonsoleProfileEntries(p *pb.KonsoleProfile) []*pb.KConfigEntry {
	if !KonsoleProfileHasContent(p) {
		return nil
	}
	var out []*pb.KConfigEntry
	if p.GetSetDefault() || p.GetLock() {
		out = append(out, &pb.KConfigEntry{File: konsoleRC, Group: "Desktop Entry", Key: "DefaultProfile", Value: KonsoleProfileFile(p), Type: "string", Enforced: p.GetLock()})
	}
	if p.GetLock() {
		for _, a := range konsoleLockedActions {
			out = append(out, &pb.KConfigEntry{File: KConfigFileGlobals, Group: KConfigGroupActionRestrictions, Key: "action/" + a, Value: "false", Type: "bool", Enforced: true})
		}
	}
	return out
}

// ValidDesktopFileID reports whether id names a desktop entry file.
func ValidDesktopFileID(id string) bool {
	return len(id) <= 255 && desktopFileIDPattern.MatchString(id) && !strings.Contains(id, "..")
}

// DesktopFileLookup returns the installed desktop entry for an id, or nil.
type DesktopFileLookup func(id string) []byte

// InstalledDesktopFile reads the installed desktop entry for id from the
// standard application directories (first match wins).
func InstalledDesktopFile(id string) []byte {
	if !ValidDesktopFileID(id) {
		return nil
	}
	for _, dir := range desktopFileDirs {
		data, err := os.ReadFile(filepath.Join(dir, id)) //nolint:gosec // G304: id validated, fixed directories
		if err == nil && len(data) > 0 && len(data) <= 1<<20 {
			return data
		}
	}
	return nil
}

// HideDesktopEntry returns the desktop entry that shadows id with
// NoDisplay=true. With the installed entry available, it is copied so
// that file associations and actions keep working; otherwise a minimal
// entry is generated.
func HideDesktopEntry(id string, original []byte) []byte {
	header := "# Managed by Bor. Hides " + id + " from menus and launchers.\n"
	text := strings.ReplaceAll(string(original), "\r\n", "\n")
	if !strings.Contains(text, "[Desktop Entry]") {
		return []byte(header +
			"[Desktop Entry]\nType=Application\nName=" + strings.TrimSuffix(id, ".desktop") + "\nExec=true\nNoDisplay=true\n")
	}
	var b strings.Builder
	b.WriteString(header)
	inMain := false
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inMain = trimmed == "[Desktop Entry]"
			b.WriteString(line)
			b.WriteByte('\n')
			if inMain {
				b.WriteString("NoDisplay=true\n")
			}
			continue
		}
		if inMain {
			k, _, ok := strings.Cut(trimmed, "=")
			if ok && strings.TrimSpace(k) == "NoDisplay" {
				continue
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// RenderAppFiles renders the application files of a scope, keyed by path
// relative to the data directory (or to the scope's directory below it).
func RenderAppFiles(a KConfigAppFiles, lookup DesktopFileLookup) map[string][]byte {
	files := make(map[string][]byte)
	if KonsoleProfileHasContent(a.KonsoleProfile) {
		files[konsoleProfileDir+"/"+KonsoleProfileFile(a.KonsoleProfile)] = RenderKonsoleProfile(a.KonsoleProfile, a.KonsoleProfilePolicy)
	}
	for _, id := range a.HiddenApplications {
		if !ValidDesktopFileID(id) {
			continue
		}
		var original []byte
		if lookup != nil {
			original = lookup(id)
		}
		files[applicationsDir+"/"+id] = HideDesktopEntry(id, original)
	}
	return files
}

// AppFilesChanged reports whether any of the changed data paths belongs to
// the application files (as opposed to the look-and-feel package).
func AppFilesChanged(changed []string) bool {
	for _, p := range changed {
		if strings.Contains(p, "/"+applicationsDir+"/") || strings.Contains(p, "/"+konsoleProfileDir+"/") {
			return true
		}
	}
	return false
}

// LayoutFilesChanged reports whether any of the changed data paths belongs
// to the look-and-feel package or the Plasma update scripts.
func LayoutFilesChanged(changed []string) bool {
	for _, p := range changed {
		if strings.Contains(p, "/"+lookAndFeelDir+"/") || strings.Contains(p, "/"+shellUpdatesDir+"/") {
			return true
		}
	}
	return false
}

// historyModeLabel is used in compliance messages.
func historyModeLabel(mode string) string {
	switch strings.TrimSpace(mode) {
	case "0":
		return "no scrollback"
	case "1":
		return "fixed scrollback"
	case "2":
		return "unlimited scrollback"
	}
	if n, err := strconv.Atoi(strings.TrimSpace(mode)); err == nil {
		return "scrollback mode " + strconv.Itoa(n)
	}
	return "default scrollback"
}
