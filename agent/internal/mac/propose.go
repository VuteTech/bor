// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package mac

import (
	"fmt"
	"sort"
	"strings"
)

// ProposeApparmorRules turns the denial records of one profile into
// AppArmor rules an administrator can paste into a local override or a
// custom profile. It is a mechanical translation for review, not a
// generated policy: paths keep user names collapsed, and operations the
// translation does not know become comments.
func ProposeApparmorRules(records []Record) string {
	rules := make(map[string]struct{})
	notes := make(map[string]struct{})
	for i := range records {
		r := &records[i]
		if r.Kind != "apparmor" || !r.IsDenial() {
			continue
		}
		op := r.Fields["operation"]
		switch {
		case op == "userns_create":
			rules["  userns,"] = struct{}{}
		case op == "capable" || r.Fields["capname"] != "":
			if capName := r.Fields["capname"]; capName != "" {
				rules["  capability "+capName+","] = struct{}{}
			}
		case r.Fields["name"] != "" && (r.Fields["requested_mask"] != "" || r.Fields["denied_mask"] != ""):
			mask := r.Fields["denied_mask"]
			if mask == "" {
				mask = r.Fields["requested_mask"]
			}
			name := homeDirRE.ReplaceAllString(r.Fields["name"], "@{HOME}")
			rules[fmt.Sprintf("  %s %s,", quoteIfNeeded(name), apparmorMask(mask))] = struct{}{}
		case r.Fields["family"] != "":
			rule := "  network " + r.Fields["family"]
			if st := r.Fields["sock_type"]; st != "" {
				rule += " " + st
			}
			rules[rule+","] = struct{}{}
		case op == "signal":
			rule := "  signal"
			if set := r.Fields["requested"]; set != "" {
				rule += " (" + set + ")"
			}
			if sig := r.Fields["signal"]; sig != "" {
				rule += " set=(" + sig + ")"
			}
			if peer := r.Fields["peer"]; peer != "" {
				rule += " peer=" + peer
			}
			rules[rule+","] = struct{}{}
		case op == "ptrace":
			rule := "  ptrace"
			if set := r.Fields["requested"]; set != "" {
				rule += " (" + set + ")"
			}
			if peer := r.Fields["peer"]; peer != "" {
				rule += " peer=" + peer
			}
			rules[rule+","] = struct{}{}
		case strings.HasPrefix(op, "dbus"):
			notes["  # D-Bus: "+Redact(r.Raw)] = struct{}{}
		default:
			notes["  # "+Redact(r.Raw)] = struct{}{}
		}
	}
	lines := make([]string, 0, len(rules)+len(notes))
	for k := range rules {
		lines = append(lines, k)
	}
	sort.Strings(lines)
	noteLines := make([]string, 0, len(notes))
	for k := range notes {
		noteLines = append(noteLines, k)
	}
	sort.Strings(noteLines)
	var b strings.Builder
	b.WriteString("# Rules derived from the denials of the last report window. Review every\n# line; collapse paths with globs where the application needs them.\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if len(noteLines) > 0 {
		b.WriteString("  # Not translated:\n")
		for _, l := range noteLines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// apparmorMask orders the permission letters the way profiles write them
// and turns a bare execute into an inherit execute.
func apparmorMask(mask string) string {
	order := "rwalkmix"
	var b strings.Builder
	seen := make(map[rune]bool)
	hasX := false
	for _, c := range mask {
		if c == 'x' {
			hasX = true
		}
	}
	for _, c := range order {
		if strings.ContainsRune(mask, c) && !seen[c] {
			if c == 'x' {
				if hasX && !strings.ContainsAny(mask, "iPpCcUu") {
					b.WriteString("ix")
				} else {
					b.WriteRune(c)
				}
				seen[c] = true
				continue
			}
			b.WriteRune(c)
			seen[c] = true
		}
	}
	if b.Len() == 0 {
		return mask
	}
	return b.String()
}

func quoteIfNeeded(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}
