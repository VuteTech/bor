// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package procinfo locates processes that currently hold a file open by
// scanning /proc/<PID>/fd symlinks. This is inherently racy — processes
// that opened and closed the file before this scan runs will not appear —
// but it reliably captures interactive editors, long-running scripts, and
// other processes that keep the file descriptor open after modifying it.
package procinfo

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo describes a process that has a file open.
type ProcessInfo struct {
	PID  int
	Comm string // executable name from /proc/<PID>/comm
	User string // resolved username, or numeric UID if resolution fails
}

// FindFileHolders returns all processes associated with path at the time of
// the call. Two strategies are used:
//
//  1. Open-fd scan: checks /proc/<PID>/fd/ symlinks. Catches processes that
//     keep an explicit file descriptor open (e.g. tail -f, cat, daemons).
//
//  2. Cmdline scan: checks /proc/<PID>/cmdline arguments. Catches editors
//     like Vim that use atomic rename-on-save (write temp → rename into place)
//     and therefore never hold an fd to the final path, but are still running
//     with the filename as a command-line argument.
//
// It requires read access to /proc (available to root, which the agent
// runs as). Errors for individual processes are silently skipped.
func FindFileHolders(path string) []ProcessInfo {
	clean := filepath.Clean(path)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	var result []ProcessInfo

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		if hasFileOpen(pid, clean) || hasFileInCmdline(pid, clean) {
			result = append(result, collectInfo(pid))
		}
	}

	return result
}

// hasFileOpen reports whether process pid has path open as a file descriptor.
func hasFileOpen(pid int, path string) bool {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}

	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue
		}
		if link == path {
			return true
		}
	}
	return false
}

// hasFileInCmdline reports whether path appears as one of the command-line
// arguments of process pid. This catches editors like Vim that replace files
// via atomic rename and hold no open fd to the final path.
//
// Relative arguments are resolved against the process's cwd so that
// `vim kdeglobals` run from within the watched directory matches the
// absolute watched path.
func hasFileInCmdline(pid int, path string) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}

	// Resolve process cwd once, lazily — only needed for relative args.
	cwd := ""
	getCwd := func() string {
		if cwd == "" {
			if link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
				cwd = link
			}
		}
		return cwd
	}

	// /proc/<PID>/cmdline is NUL-separated; last entry may be empty.
	for _, arg := range bytes.Split(raw, []byte{0}) {
		if len(arg) == 0 {
			continue
		}
		candidate := string(arg)
		if !filepath.IsAbs(candidate) {
			if d := getCwd(); d != "" {
				candidate = filepath.Join(d, candidate)
			}
		}
		if filepath.Clean(candidate) == path {
			return true
		}
	}
	return false
}

// collectInfo reads the comm name and real UID for a process, resolving
// the UID to a username where possible.
func collectInfo(pid int) ProcessInfo {
	info := ProcessInfo{PID: pid}

	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		info.Comm = strings.TrimSpace(string(raw))
	}

	if uid := readRealUID(pid); uid != "" {
		if u, err := user.LookupId(uid); err == nil {
			info.User = u.Username
		} else {
			info.User = uid
		}
	}

	return info
}

// readRealUID returns the real UID (first field of the Uid: line) from
// /proc/<PID>/status, or an empty string on failure.
func readRealUID(pid int) string {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1] // real (not effective) UID
			}
		}
	}
	return ""
}
