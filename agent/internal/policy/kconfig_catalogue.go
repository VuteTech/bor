// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// KConfigSchemasDir is where KDE applications install their KConfigXT
// schemas (*.kcfg). They describe the config file, groups, keys, types,
// defaults and choices, like GSettings schemas do for dconf.
const KConfigSchemasDir = "/usr/share/config.kcfg"

// Limits for the schema scan: the files are small, but the agent treats
// them as untrusted input.
const (
	maxKcfgFileSize  = 2 << 20
	maxKcfgEntries   = 5000
	maxKcfgTextLen   = 512
	maxKcfgLabelLen  = 160
	maxKcfgDescLen   = 1024
	kcfgSourceName   = "kcfg"
	kcfgCategoryPref = "Node: "
)

// kcfg XML structures. Namespaces are ignored by encoding/xml when the
// struct tags carry none, which is what we want.
type kcfgRoot struct {
	XMLName xml.Name    `xml:"kcfg"`
	File    *kcfgFile   `xml:"kcfgfile"`
	Groups  []kcfgGroup `xml:"group"`
}

type kcfgFile struct {
	Name string `xml:"name,attr"`
	Arg  string `xml:"arg,attr"`
}

type kcfgGroup struct {
	Name    string      `xml:"name,attr"`
	Parent  string      `xml:"parentGroupName,attr"`
	Entries []kcfgEntry `xml:"entry"`
}

type kcfgEntry struct {
	Name      string       `xml:"name,attr"`
	Key       string       `xml:"key,attr"`
	Type      string       `xml:"type,attr"`
	Label     string       `xml:"label"`
	Tooltip   string       `xml:"tooltip"`
	WhatsThis string       `xml:"whatsthis"`
	Default   *kcfgDefault `xml:"default"`
	Min       string       `xml:"min"`
	Max       string       `xml:"max"`
	Choices   []kcfgChoice `xml:"choices>choice"`
}

type kcfgDefault struct {
	Code  string `xml:"code,attr"`
	Value string `xml:",chardata"`
}

type kcfgChoice struct {
	Name string `xml:"name,attr"`
}

// kcfgArgFiles maps schemas whose config file is passed at runtime
// (<kcfgfile arg="true"/>) to the file the application actually uses.
var kcfgArgFiles = map[string]string{
	"kwin.kcfg":                "kwinrc",
	"nightlightsettings.kcfg":  "kwinrc",
	"okular.kcfg":              "okularpartrc",
	"okular_core.kcfg":         "okularpartrc",
	"partitionmanager.kcfg":    "partitionmanagerrc",
	"kcmaccessibilityshakecursor.kcfg": "kwinrc",
}

// kcfgTypes maps KConfigXT types to the catalogue types the editor renders.
var kcfgTypes = map[string]string{
	"Bool": "Bool", "Int": "Int", "UInt": "UInt", "Double": "Double",
	"String": "String", "StringList": "StringList", "Enum": "Enum",
	"Path": "Path", "Url": "Url", "Color": "Color", "Font": "Font",
	"Int64": "Int", "UInt64": "Int", "LongLong": "Int", "ULongLong": "Int",
	"PathList": "StringList",
}

var kcfgIDUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ScanKConfigSchemas reads every *.kcfg under KConfigSchemasDir and returns
// catalogue entries for the keys they describe. The scan is best effort:
// unreadable or malformed files, parameterised entries and unsupported
// types are skipped.
func ScanKConfigSchemas() ([]*pb.KConfigCatalogueEntry, error) {
	return ScanKConfigSchemasFrom(KConfigSchemasDir)
}

// ScanKConfigSchemasFrom is ScanKConfigSchemas with an injectable directory.
func ScanKConfigSchemasFrom(dir string) ([]*pb.KConfigCatalogueEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no KDE schemas installed; not an error
		}
		return nil, fmt.Errorf("kconfig catalogue: read dir %s: %w", dir, err)
	}

	var out []*pb.KConfigCatalogueEntry
	seenTarget := make(map[string]bool)
	for _, de := range dirEntries {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, ".kcfg") {
			continue
		}
		info, err := de.Info()
		if err != nil || info.Size() > maxKcfgFileSize {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: fixed schema directory
		if err != nil {
			continue
		}
		for _, e := range parseKcfg(name, data) {
			target := e.GetFile() + "\x00" + strings.Join(e.GetGroupPath(), "][") + "\x00" + e.GetKey()
			if seenTarget[target] {
				continue
			}
			seenTarget[target] = true
			out = append(out, e)
			if len(out) >= maxKcfgEntries {
				return out, nil
			}
		}
	}
	return out, nil
}

// parseKcfg converts one schema file into catalogue entries.
func parseKcfg(fileName string, data []byte) []*pb.KConfigCatalogueEntry {
	var root kcfgRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil
	}

	target := ""
	switch {
	case root.File != nil && root.File.Name != "":
		target = root.File.Name
	case root.File != nil && root.File.Arg == "true":
		target = kcfgArgFiles[fileName]
	}
	if target == "" || strings.ContainsAny(target, "/\\\x00 \r\n") || strings.HasPrefix(target, ".") {
		return nil
	}

	reload := "app"
	switch target {
	case "kwinrc":
		reload = "kwin"
	case "plasmarc":
		reload = "plasmashell"
	case "kscreenlockerrc":
		reload = "screenlocker"
	case "kdeglobals":
		reload = "logout"
	}

	var out []*pb.KConfigCatalogueEntry
	for _, g := range root.Groups {
		if g.Name == "" || strings.Contains(g.Name, "$(") || strings.Contains(g.Parent, "$(") {
			continue
		}
		groupPath := []string{g.Name}
		if g.Parent != "" {
			groupPath = []string{g.Parent, g.Name}
		}
		if strings.ContainsAny(strings.Join(groupPath, ""), "[]\r\n") {
			continue
		}
		for _, en := range g.Entries {
			key := en.Key
			if key == "" {
				key = en.Name
			}
			if key == "" || strings.Contains(key, "$(") || strings.ContainsAny(key, "=[]\r\n") || len(key) > maxKcfgTextLen {
				continue
			}
			typ, ok := kcfgTypes[en.Type]
			if !ok {
				continue
			}
			entry := &pb.KConfigCatalogueEntry{
				Id:          kcfgEntryID(target, groupPath, key),
				File:        target,
				GroupPath:   groupPath,
				Key:         key,
				Type:        typ,
				Label:       truncate(collapseSpace(firstNonEmpty(en.Label, key)), maxKcfgLabelLen),
				Description: truncate(collapseSpace(firstNonEmpty(en.Tooltip, en.WhatsThis)), maxKcfgDescLen),
				Category:    kcfgCategoryPref + target,
				Reload:      reload,
				Source:      kcfgSourceName,
			}
			for _, c := range en.Choices {
				if c.Name != "" && !strings.ContainsAny(c.Name, ",\r\n") {
					entry.Choices = append(entry.Choices, truncate(c.Name, maxKcfgTextLen))
				}
			}
			if typ == "Enum" && len(entry.Choices) == 0 {
				continue
			}
			if en.Default != nil && en.Default.Code != "true" {
				def := strings.TrimSpace(en.Default.Value)
				// Enum defaults may be given as a choice index.
				if typ == "Enum" {
					if idx, err := strconv.Atoi(def); err == nil && idx >= 0 && idx < len(entry.Choices) {
						def = entry.Choices[idx]
					}
				}
				if !strings.ContainsAny(def, "\r\n") {
					entry.DefaultValue = truncate(def, maxKcfgTextLen)
				}
			}
			if !strings.ContainsAny(en.Min+en.Max, "\r\n") {
				entry.RangeMin = truncate(strings.TrimSpace(en.Min), 32)
				entry.RangeMax = truncate(strings.TrimSpace(en.Max), 32)
			}
			out = append(out, entry)
		}
	}
	return out
}

// kcfgEntryID builds a stable, pattern-safe id: kcfg/<file>/<group.path>/<key>
// with anything outside [A-Za-z0-9._-] folded to '-'.
func kcfgEntryID(file string, groupPath []string, key string) string {
	safe := func(s string) string { return strings.Trim(kcfgIDUnsafe.ReplaceAllString(s, "-"), "-") }
	return "kcfg/" + safe(file) + "/" + safe(strings.Join(groupPath, ".")) + "/" + safe(key)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// InstalledKCMModules lists the System Settings modules installed on this
// node with their display names (from kcm_*.desktop files; plugin-only
// modules use their id as the name), sorted by id.
func InstalledKCMModules() []*pb.KcmModule {
	return InstalledKCMModulesFrom(kcmDesktopDirs, kcmPluginGlobs)
}

// InstalledKCMModulesFrom is InstalledKCMModules with injectable locations.
func InstalledKCMModulesFrom(desktopDirs, pluginGlobs []string) []*pb.KcmModule {
	names := make(map[string]string)
	for _, dir := range desktopDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := e.Name()
			if !strings.HasPrefix(n, "kcm") || !strings.HasSuffix(n, ".desktop") {
				continue
			}
			id := strings.TrimSuffix(n, ".desktop")
			names[id] = desktopEntryName(filepath.Join(dir, n))
		}
	}
	for _, id := range InstalledKCMsFrom(nil, pluginGlobs) {
		if _, ok := names[id]; !ok {
			names[id] = ""
		}
	}
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*pb.KcmModule, 0, len(ids))
	for _, id := range ids {
		name := names[id]
		if name == "" {
			name = id
		}
		out = append(out, &pb.KcmModule{Id: id, Name: name})
	}
	return out
}

// desktopEntryName returns the Name= of the [Desktop Entry] group, or "".
func desktopEntryName(path string) string {
	f, err := os.Open(path) //nolint:gosec // G304: fixed application directories
	if err != nil {
		return ""
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
		if inEntry && strings.HasPrefix(line, "Name=") {
			return truncate(strings.TrimPrefix(line, "Name="), maxKcfgLabelLen)
		}
	}
	return ""
}

// CommandRunner runs a command and returns its standard output. The agent
// supplies one that runs inside a logged-in user's session.
type CommandRunner func(name string, args ...string) (string, error)

// KConfigProbeResult is the effective value of one written entry as KDE
// resolves it for a logged-in user.
type KConfigProbeResult struct {
	File     string
	ItemKey  string // same format as the compliance item key: "[group] key"
	Expected string
	Actual   string
	Mismatch bool
}

// maxKConfigProbes bounds how many entries one sync reads back.
const maxKConfigProbes = 200

// KConfigReadTool returns the kreadconfig binary available on the node
// (kreadconfig6, then kreadconfig5), or "" when none is installed.
func KConfigReadTool(lookPath func(string) (string, error)) string {
	for _, tool := range []string{"kreadconfig6", "kreadconfig5"} {
		if _, err := lookPath(tool); err == nil {
			return tool
		}
	}
	return ""
}

// ProbeKConfigEffective reads the written entries back through kreadconfig
// in a user session and reports those whose effective value differs from
// the policy value: either the user overrides a non-enforced default, or
// the session does not see the managed directory at all. Deleted entries
// and URL rules are not probed.
func ProbeKConfigEffective(entries []*pb.KConfigEntry, run CommandRunner, tool string) []KConfigProbeResult {
	if run == nil || tool == "" {
		return nil
	}
	var results []KConfigProbeResult
	probed := 0
	for _, e := range entries {
		if e.Delete || e.Group == KConfigGroupURLRestrictions || e.Locale != "" {
			continue
		}
		if probed >= maxKConfigProbes {
			break
		}
		probed++
		args := []string{"--file", e.File}
		for _, g := range strings.Split(e.Group, "][") {
			args = append(args, "--group", g)
		}
		args = append(args, "--key", e.Key)
		out, err := run(tool, args...)
		if err != nil {
			continue // the tool failing is not a policy violation
		}
		actual := strings.TrimSpace(out)
		results = append(results, KConfigProbeResult{
			File:     e.File,
			ItemKey:  fmt.Sprintf("[%s] %s", e.Group, e.Key),
			Expected: e.Value,
			Actual:   actual,
			Mismatch: !kconfigValuesEqual(e.Value, actual),
		})
	}
	return results
}

// kconfigValuesEqual compares a policy value with what kreadconfig prints,
// tolerating KConfig's canonical spellings of booleans and numbers.
func kconfigValuesEqual(expected, actual string) bool {
	e := strings.TrimSpace(expected)
	a := strings.TrimSpace(actual)
	if e == a {
		return true
	}
	eb, eok := parseKConfigBool(e)
	ab, aok := parseKConfigBool(a)
	if eok && aok {
		return eb == ab
	}
	ef, eErr := strconv.ParseFloat(e, 64)
	af, aErr := strconv.ParseFloat(a, 64)
	if eErr == nil && aErr == nil {
		return ef == af
	}
	return false
}

func parseKConfigBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "true", "on", "yes", "1":
		return true, true
	case "false", "off", "no", "0":
		return false, true
	}
	return false, false
}

// SessionSeesConfigDir reports whether a plasmashell process of the given
// user has the managed directory in its XDG_CONFIG_DIRS. running is false
// when no plasmashell of that user was found, in which case found is
// meaningless.
func SessionSeesConfigDir(uid uint32, configPath string) (found, running bool) {
	return sessionSeesConfigDirIn("/proc", uid, configPath)
}

func sessionSeesConfigDirIn(procDir string, uid uint32, configPath string) (found, running bool) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, false
	}
	want := strconv.FormatUint(uint64(uid), 10)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		base := filepath.Join(procDir, e.Name())
		comm, err := os.ReadFile(filepath.Join(base, "comm")) //nolint:gosec // G304: /proc traversal
		if err != nil || strings.TrimSpace(string(comm)) != "plasmashell" {
			continue
		}
		if procUID(filepath.Join(base, "status")) != want {
			continue
		}
		running = true
		environ, err := os.ReadFile(filepath.Join(base, "environ")) //nolint:gosec // G304: /proc traversal
		if err != nil {
			continue
		}
		for _, kv := range strings.Split(string(environ), "\x00") {
			if val, ok := strings.CutPrefix(kv, "XDG_CONFIG_DIRS="); ok {
				for _, dir := range strings.Split(val, ":") {
					if dir == configPath {
						return true, true
					}
				}
			}
		}
	}
	return false, running
}

// procUID returns the real UID from a /proc/<pid>/status file.
func procUID(statusPath string) string {
	data, err := os.ReadFile(statusPath) //nolint:gosec // G304: /proc traversal
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}
