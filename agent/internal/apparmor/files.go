// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package apparmor

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/VuteTech/Bor/server/pkg/macrules"
)

// ProfileFile is what the header of a profile file under /etc/apparmor.d
// says: the top-level profile names and attachments, the mode flags of
// the first profile, the ABI, and whether it includes a local/ file.
type ProfileFile struct {
	// File is the name under the profile directory.
	File string
	// Names are the top-level profile names (a bare attachment profile is
	// named after its path).
	Names []string
	// Attachments are the executable paths the top-level profiles attach to.
	Attachments []string
	// Flags are the flags=(...) of the first top-level profile.
	Flags []string
	// LocalInclude is the local/ file the profile includes, "" when none.
	LocalInclude string
	// ABI is the abi <abi/X> declaration, "" when none.
	ABI string
}

// FileMode returns the mode flag of the file: "complain", "unconfined",
// "kill", "prompt" or "" for enforce.
func (f *ProfileFile) FileMode() string {
	for _, fl := range f.Flags {
		switch fl {
		case "complain", "unconfined", "kill", "prompt", "default_allow":
			return fl
		}
	}
	return ""
}

var (
	abiRE          = regexp.MustCompile(`^\s*abi\s+<abi/([^>]+)>\s*,`)
	localIncludeRE = regexp.MustCompile(`^\s*#?include\s+(?:if\s+exists\s+)?<local/([^>]+)>`)
	flagsRE        = regexp.MustCompile(`flags\s*=\s*\(([^)]*)\)`)
)

// ScanProfiles parses every profile file of a directory. Directories,
// reserved entries, hidden files and package manager leftovers are
// skipped; a file that cannot be read is skipped.
func ScanProfiles(dir string) ([]ProfileFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []ProfileFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || macrules.ValidateProfileFile(name) != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: entry of the profile directory, name validated
		if err != nil {
			continue
		}
		pf := ParseProfileText(string(data))
		pf.File = name
		out = append(out, pf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}

// ParseProfileText reads the preamble and the top-level profile headers of
// profile text. It does not validate the profile.
func ParseProfileText(text string) ProfileFile {
	var pf ProfileFile
	depth := 0
	first := true
	for _, rawLine := range strings.Split(text, "\n") {
		line := stripComment(rawLine)
		if line == "" {
			continue
		}
		if depth == 0 {
			if m := abiRE.FindStringSubmatch(line); m != nil && pf.ABI == "" {
				pf.ABI = m[1]
			}
			if strings.HasSuffix(line, "{") {
				header := strings.TrimSpace(line[:len(line)-1])
				name, attach, flags := parseHeader(header)
				if name != "" {
					pf.Names = append(pf.Names, name)
				}
				if attach != "" {
					pf.Attachments = append(pf.Attachments, attach)
				}
				if first {
					pf.Flags = flags
					first = false
				}
			}
		}
		if m := localIncludeRE.FindStringSubmatch(line); m != nil && pf.LocalInclude == "" {
			pf.LocalInclude = m[1]
		}
		for _, c := range line {
			switch c {
			case '{':
				depth++
			case '}':
				if depth > 0 {
					depth--
				}
			}
		}
	}
	return pf
}

// stripComment removes a trailing comment, keeping "#include".
func stripComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	inString := false
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c == '"' {
			inString = !inString
		}
		if c == '#' && !inString && !strings.HasPrefix(trimmed[i:], "#include") {
			return strings.TrimSpace(trimmed[:i])
		}
	}
	return trimmed
}

// parseHeader splits "profile NAME ATTACH flags=(...)" or "ATTACH
// flags=(...)" into its parts.
func parseHeader(header string) (name, attach string, flags []string) {
	if m := flagsRE.FindStringSubmatch(header); m != nil {
		for _, f := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if f != "" {
				flags = append(flags, f)
			}
		}
		header = strings.TrimSpace(flagsRE.ReplaceAllString(header, ""))
	}
	fields := splitHeaderFields(header)
	if len(fields) == 0 {
		return "", "", flags
	}
	if fields[0] == "profile" {
		if len(fields) < 2 {
			return "", "", flags
		}
		name = fields[1]
		if len(fields) > 2 && (strings.HasPrefix(fields[2], "/") || strings.HasPrefix(fields[2], "@{")) {
			attach = fields[2]
		}
		return name, attach, flags
	}
	if strings.HasPrefix(fields[0], "/") || strings.HasPrefix(fields[0], "@{") {
		return fields[0], fields[0], flags
	}
	return "", "", flags
}

// splitHeaderFields splits on whitespace while keeping quoted names and
// brace alternations together.
func splitHeaderFields(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.Trim(cur.String(), `"`))
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == '{' && !inQuote:
			depth++
			cur.WriteRune(r)
		case r == '}' && !inQuote:
			depth--
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && depth == 0 && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// SetEnforceFlags removes the complain flag from every top-level profile
// header of profile text, so the file loads in enforce mode. It reports
// whether the text changed.
func SetEnforceFlags(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	changed := false
	depth := 0
	for i, rawLine := range lines {
		line := stripComment(rawLine)
		if depth == 0 && strings.HasSuffix(line, "{") && flagsRE.MatchString(rawLine) {
			next := flagsRE.ReplaceAllStringFunc(rawLine, func(m string) string {
				sub := flagsRE.FindStringSubmatch(m)
				var keep []string
				for _, f := range strings.FieldsFunc(sub[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
					if f != "" && f != "complain" {
						keep = append(keep, f)
					}
				}
				if len(keep) == 0 {
					return ""
				}
				return "flags=(" + strings.Join(keep, ",") + ")"
			})
			next = strings.Replace(next, "  {", " {", 1)
			if next != rawLine {
				lines[i] = next
				changed = true
			}
		}
		for _, c := range line {
			switch c {
			case '{':
				depth++
			case '}':
				if depth > 0 {
					depth--
				}
			}
		}
	}
	return strings.Join(lines, "\n"), changed
}
