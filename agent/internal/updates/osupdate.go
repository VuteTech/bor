// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"encoding/xml"
	"strings"
)

// RenderOsUpdateConf renders /etc/os-update.conf, the administrator
// override file of SUSE's os-update. REBOOT_CMD=none: Bor owns the
// reboot.
func RenderOsUpdateConf(mode Mode) []byte {
	cmd := "auto"
	if mode == ModeSecurity {
		cmd = "security"
	}
	return []byte("# " + Header + "\n" +
		"UPDATE_CMD=\"" + cmd + "\"\n" +
		"REBOOT_CMD=\"none\"\n")
}

type zypperPatchXML struct {
	Updates []struct {
		Name     string `xml:"name,attr"`
		Edition  string `xml:"edition,attr"`
		Category string `xml:"category,attr"`
		Severity string `xml:"severity,attr"`
		Issues   []struct {
			Type string `xml:"type,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"issue-list>issue"`
	} `xml:"update-status>update-list>update"`
}

// ParseZypperPatches parses "zypper --xmlout list-patches".
func ParseZypperPatches(data []byte) ([]Pending, error) {
	var doc zypperPatchXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []Pending
	for _, u := range doc.Updates {
		p := Pending{Name: u.Name, Version: u.Edition, Advisory: u.Name, Security: strings.EqualFold(u.Category, "security")}
		switch strings.ToLower(u.Severity) {
		case "critical", "important", "moderate", "low":
			p.Severity = strings.ToLower(u.Severity)
		}
		for _, is := range u.Issues {
			if strings.EqualFold(is.Type, "cve") {
				p.CVEs = append(p.CVEs, is.ID)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

type zypperUpdateXML struct {
	Updates []struct {
		Name    string `xml:"name,attr"`
		Edition string `xml:"edition,attr"`
		Kind    string `xml:"kind,attr"`
	} `xml:"update-status>update-list>update"`
}

// ParseZypperUpdates parses "zypper --xmlout list-updates": the package
// updates, which on Tumbleweed are the only kind (it has no patches).
func ParseZypperUpdates(data []byte) ([]Pending, error) {
	var doc zypperUpdateXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []Pending
	for _, u := range doc.Updates {
		if u.Kind != "" && u.Kind != "package" {
			continue
		}
		out = append(out, Pending{Name: u.Name, Version: u.Edition})
	}
	return out, nil
}

// ZypperPending lists what zypper would install: the patches (with their
// category and CVEs) and the package updates that no patch covers.
func ZypperPending(ctx context.Context, run Runner, p *Platform) ([]Pending, error) {
	out, err := run(ctx, p.ZypperBin, "--non-interactive", "--xmlout", "-q", "list-patches")
	if err != nil && len(out) == 0 {
		return nil, err
	}
	patches, perr := ParseZypperPatches(out)
	if perr != nil {
		return nil, perr
	}
	seen := map[string]bool{}
	for _, pt := range patches {
		seen[pt.Name] = true
	}
	out, err = run(ctx, p.ZypperBin, "--non-interactive", "--xmlout", "-q", "list-updates")
	if err != nil && len(out) == 0 {
		return patches, nil
	}
	pkgs, uerr := ParseZypperUpdates(out)
	if uerr != nil {
		return patches, nil
	}
	for _, pk := range pkgs {
		if !seen[pk.Name] {
			patches = append(patches, pk)
		}
	}
	return patches, nil
}

// ZypperNeedsRebooting runs "zypper needs-rebooting": exit 102 means a
// reboot is needed.
func ZypperNeedsRebooting(ctx context.Context, run Runner, p *Platform) bool {
	_, err := run(ctx, p.ZypperBin, "--non-interactive", "needs-rebooting")
	return ExitCode(err) == 102
}

// OsUpdateMode reads UPDATE_CMD of an os-update.conf.
func OsUpdateMode(content []byte) Mode {
	for _, l := range strings.Split(string(content), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "UPDATE_CMD=") {
			v := strings.Trim(strings.TrimPrefix(l, "UPDATE_CMD="), `"'`)
			if v == "security" {
				return ModeSecurity
			}
			return ModeAll
		}
	}
	return ModeAll
}
