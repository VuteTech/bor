// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package logind is a thin wrapper over `loginctl` for the session access
// policy enforcer: enumerate seated user sessions with their lock state,
// and lock or terminate them at a schedule boundary. It shells out to
// loginctl rather than talking to org.freedesktop.login1 over D-Bus to
// avoid a cgo/dbus dependency in the enforcement path and to match how the
// feature was validated in phase 0.
package logind

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Session is a single logind session with the properties the enforcer needs.
type Session struct {
	ID         string
	Name       string // username
	UID        uint32
	Seat       string
	Type       string // "x11" | "wayland" | "tty" | …
	Class      string // "user" | "manager" | "greeter" | …
	Active     bool
	LockedHint bool
}

// IsGraphicalUser reports whether the session is an active graphical user
// session — the only kind the enforcer locks or terminates.
func (s *Session) IsGraphicalUser() bool {
	return s.Class == "user" && (s.Type == "x11" || s.Type == "wayland")
}

// Manager enumerates and acts on logind sessions. Abstracted for testing.
type Manager interface {
	ListSessions() ([]Session, error)
	Lock(id string) error
	Terminate(id string) error
}

// CLI is a Manager backed by the loginctl binary.
type CLI struct {
	// run executes loginctl with args and returns combined stdout. Injected
	// so tests can supply canned output without a real login1.
	run func(args ...string) (string, error)
}

// New returns a CLI Manager that shells out to loginctl.
func New() *CLI {
	return &CLI{run: runLoginctl}
}

// Available reports whether loginctl is usable on this host.
func (c *CLI) Available() bool {
	if _, err := exec.LookPath("loginctl"); err != nil {
		return false
	}
	_, err := c.run("--version")
	return err == nil
}

func runLoginctl(args ...string) (string, error) {
	out, err := exec.Command("loginctl", args...).CombinedOutput() //nolint:gosec // G204: fixed binary, args are session IDs/flags
	if err != nil {
		return "", fmt.Errorf("loginctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// sessionProps are the properties fetched per session.
var sessionProps = []string{"Id", "Name", "User", "Seat", "Type", "Class", "Active", "LockedHint"}

// ListSessions returns the current user-class sessions with their lock state.
//
// Sessions are queried one at a time and a session that has vanished between
// the list and the show is skipped rather than failing the whole pass: a
// single multi-id `show-session` aborts entirely if any id is gone (verified),
// and sessions churn constantly (every ssh/sudo/cron login is one), which
// would otherwise open an enforcement gap on every transient session.
func (c *CLI) ListSessions() ([]Session, error) {
	ids, err := c.listUserSessionIDs()
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, id := range ids {
		args := []string{"show-session", id}
		for _, p := range sessionProps {
			args = append(args, "-p", p)
		}
		out, err := c.run(args...)
		if err != nil {
			continue // session ended between list and show
		}
		sessions = append(sessions, parseShowSession(out)...)
	}
	return sessions, nil
}

// listUserSessionIDs parses `loginctl list-sessions --no-legend` and returns
// the ids of user-class sessions (skipping manager/greeter/background
// sessions when the CLASS column is present; older loginctl without that
// column yields every id).
func (c *CLI) listUserSessionIDs() ([]string, error) {
	out, err := c.run("list-sessions", "--no-legend", "--no-pager")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Columns: SESSION UID USER SEAT LEADER CLASS TTY IDLE SINCE (systemd ≥ 256);
		// older versions omit LEADER/CLASS.
		if len(fields) >= 6 && fields[5] != "user" {
			continue
		}
		ids = append(ids, fields[0])
	}
	return ids, nil
}

// Lock locks the given session (screen locker engages; apps keep running).
func (c *CLI) Lock(id string) error {
	_, err := c.run("lock-session", id)
	return err
}

// Terminate ends the given session (logs the user off).
func (c *CLI) Terminate(id string) error {
	_, err := c.run("terminate-session", id)
	return err
}

// parseShowSession parses `loginctl show-session` output: Key=Value lines in
// blocks separated by a blank line, one block per session.
func parseShowSession(out string) []Session {
	var sessions []Session
	block := map[string]string{}
	flush := func() {
		if len(block) == 0 {
			return
		}
		uid, _ := strconv.ParseUint(block["User"], 10, 32)
		sessions = append(sessions, Session{
			ID:         block["Id"],
			Name:       block["Name"],
			UID:        uint32(uid),
			Seat:       block["Seat"],
			Type:       block["Type"],
			Class:      block["Class"],
			Active:     block["Active"] == "yes",
			LockedHint: block["LockedHint"] == "yes",
		})
		block = map[string]string{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			block[k] = v
		}
	}
	flush()
	return sessions
}
