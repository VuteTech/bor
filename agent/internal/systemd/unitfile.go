// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// UnitFileInfo is what the catalogue reads from a unit file without
// loading it into systemd: the description, whether it can be enabled,
// the timer schedule and the unit a timer, socket or path activates.
type UnitFileInfo struct {
	Description string
	HasInstall  bool
	OnCalendar  []string
	// Triggers is the unit named by [Timer] Unit=, [Socket] Service= or
	// [Path] Unit=; empty when the file does not name one.
	Triggers    string
	ServiceType string
}

// maxUnitFileBytes bounds how much of a unit file the scanner reads.
const maxUnitFileBytes = 256 * 1024

// ScanUnitFile reads the unit file at path. Masked units (a symlink to
// /dev/null) and unreadable files yield an empty UnitFileInfo.
func ScanUnitFile(path string) UnitFileInfo {
	f, err := os.Open(path) //nolint:gosec // G304: path comes from systemd's own unit file listing
	if err != nil {
		return UnitFileInfo{}
	}
	defer func() { _ = f.Close() }()
	return ParseUnitFile(io.LimitReader(f, maxUnitFileBytes))
}

// ParseUnitFile parses unit file syntax: [Section] headers and Key=Value
// lines, with "#" and ";" comments and backslash line continuations.
func ParseUnitFile(r io.Reader) UnitFileInfo {
	var info UnitFileInfo
	section := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxUnitFileBytes)
	var pending string
	for sc.Scan() {
		line := sc.Text()
		if pending != "" {
			line = pending + line
			pending = ""
		}
		if strings.HasSuffix(line, "\\") {
			pending = strings.TrimSuffix(line, "\\") + " "
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			if end := strings.IndexByte(line, ']'); end > 0 {
				section = line[1:end]
				if section == "Install" {
					info.HasInstall = true
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch section {
		case "Unit":
			if key == "Description" {
				info.Description = value
			}
		case "Timer":
			switch key {
			case "OnCalendar":
				if value == "" {
					info.OnCalendar = nil
				} else {
					info.OnCalendar = append(info.OnCalendar, value)
				}
			case "Unit":
				info.Triggers = value
			}
		case "Socket":
			if key == "Service" {
				info.Triggers = value
			}
		case "Path":
			if key == "Unit" {
				info.Triggers = value
			}
		case "Service":
			if key == "Type" {
				info.ServiceType = value
			}
		}
	}
	return info
}
