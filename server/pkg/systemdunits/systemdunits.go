// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package systemdunits holds the unit-name rules shared by the server
// (policy validation) and the agent (re-validation of server data before a
// name becomes a path component or a D-Bus argument): the name syntax, the
// unit types Bor manages, the protected units no policy may touch, the
// risky units the editor warns about, and the syntax of the time spans and
// calendar expressions a drop-in may carry.
package systemdunits

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxNameLength is systemd's own limit for a unit name.
const MaxNameLength = 255

// Types lists the unit types a policy may manage, by suffix.
var Types = []string{"service", "timer", "socket", "target", "path"}

// nameRE matches the characters systemd allows in a unit name. Path
// separators are excluded, so a validated name is a single path component.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9:_.\\@-]+$`)

// ValidateName reports whether name is a well-formed unit name of a type
// Bor manages. It does not check whether the unit exists.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("unit name must not be empty")
	}
	if len(name) > MaxNameLength {
		return fmt.Errorf("unit name %q is longer than %d characters", name, MaxNameLength)
	}
	if name == "." || name == ".." || !nameRE.MatchString(name) {
		return fmt.Errorf("unit name %q contains characters that are not permitted", name)
	}
	t := Type(name)
	if t == "" {
		return fmt.Errorf("unit name %q must end in one of .%s", name, strings.Join(Types, ", ."))
	}
	if len(name) == len(t)+1 {
		return fmt.Errorf("unit name %q has no name before the suffix", name)
	}
	return nil
}

// Type returns the unit type (the suffix without the dot) when it is one
// Bor manages, or "" otherwise.
func Type(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	suffix := name[i+1:]
	for _, t := range Types {
		if suffix == t {
			return t
		}
	}
	return ""
}

// IsTemplate reports whether name is a template unit ("getty@.service")
// rather than an instance ("getty@tty1.service") or a plain unit.
func IsTemplate(name string) bool {
	i := strings.IndexByte(name, '@')
	if i < 0 {
		return false
	}
	return strings.HasPrefix(name[i+1:], ".")
}

// protectedExact are units no policy may manage: Bor's own agent, the
// buses and daemons the agent and the login session depend on, and the
// targets that define the boot. ctrl-alt-del.target is deliberately not
// in this list; masking it is a common hardening step.
var protectedExact = map[string]struct{}{
	"bor-agent.service":               {},
	"dbus.service":                    {},
	"dbus.socket":                     {},
	"dbus-broker.service":             {},
	"systemd-logind.service":          {},
	"polkit.service":                  {},
	"init.scope":                      {},
	"-.slice":                         {},
	"-.mount":                         {},
	"user@.service":                   {},
	"user-runtime-dir@.service":       {},
	"systemd-user-sessions.service":   {},
	"getty.target":                    {},
	"basic.target":                    {},
	"sysinit.target":                  {},
	"multi-user.target":               {},
	"graphical.target":                {},
	"default.target":                  {},
	"rescue.target":                   {},
	"emergency.target":                {},
	"shutdown.target":                 {},
	"final.target":                    {},
	"umount.target":                   {},
	"reboot.target":                   {},
	"poweroff.target":                 {},
	"halt.target":                     {},
	"kexec.target":                    {},
	"systemd-journald.service":        {},
	"systemd-journald.socket":         {},
	"systemd-journald-dev-log.socket": {},
	"systemd-udevd.service":           {},
	"systemd-udevd-control.socket":    {},
	"systemd-udevd-kernel.socket":     {},
}

// protectedPrefixes extend the exact list to instance and companion units
// ("systemd-journald@foo.service", "user@1000.service").
var protectedPrefixes = []string{
	"systemd-journald",
	"systemd-udevd",
	"user@",
	"user-runtime-dir@",
}

// IsProtected reports whether no policy may manage the unit.
func IsProtected(name string) bool {
	if _, ok := protectedExact[name]; ok {
		return true
	}
	for _, p := range protectedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// riskyExact are units whose stop or mask can cut users or administrators
// off from the node. The editor asks for a confirmation; the server does
// not reject them.
var riskyExact = map[string]struct{}{
	"NetworkManager.service":    {},
	"systemd-resolved.service":  {},
	"sshd.service":              {},
	"ssh.service":               {},
	"sshd.socket":               {},
	"ssh.socket":                {},
	"gdm.service":               {},
	"sddm.service":              {},
	"lightdm.service":           {},
	"display-manager.service":   {},
	"firewalld.service":         {},
	"chronyd.service":           {},
	"chrony.service":            {},
	"systemd-timesyncd.service": {},
	"auditd.service":            {},
	"cron.service":              {},
	"crond.service":             {},
	"cronie.service":            {},
	"wpa_supplicant.service":    {},
	"iwd.service":               {},
}

// IsRisky reports whether the editor should ask for a confirmation before
// a policy manages the unit.
func IsRisky(name string) bool {
	if _, ok := riskyExact[name]; ok {
		return true
	}
	for _, p := range []string{"systemd-networkd", "systemd-resolved", "systemd-timesyncd"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// timeSpanRE matches the systemd.time(7) time span grammar in ASCII: a
// sequence of number-unit pairs ("1h 30min", "90s", "1.5h"), a bare number
// (seconds) or "infinity".
var timeSpanRE = regexp.MustCompile(`^(infinity|(\d+(\.\d+)?[ ]*(usec|us|nsec|ns|msec|ms|seconds|second|sec|s|minutes|minute|min|m|hours|hour|hr|h|days|day|d|weeks|week|w|months|month|M|years|year|y)?[ ]*)+)$`)

// ValidateTimeSpan reports whether s is a well-formed systemd time span.
// The value is checked as is: it is written verbatim into a drop-in, so
// surrounding or embedded whitespace other than spaces is rejected.
func ValidateTimeSpan(s string) error {
	if s == "" {
		return fmt.Errorf("time span must not be empty")
	}
	if len(s) > 64 || !timeSpanRE.MatchString(s) {
		return fmt.Errorf("invalid time span %q: use systemd syntax such as 15min, 1h 30min or 90s", s)
	}
	return nil
}

// MaxCalendarLength caps a calendar expression.
const MaxCalendarLength = 128

// calendarRE matches the characters a systemd.time(7) calendar expression
// can contain. The grammar itself is checked with systemd-analyze on the
// node (and on the server when it runs on a systemd host).
var calendarRE = regexp.MustCompile(`^[A-Za-z0-9 *,.:~/@-]+$`)

// ValidateCalendar reports whether expr looks like a calendar expression.
// The value is checked as is (see ValidateTimeSpan).
func ValidateCalendar(expr string) error {
	if expr == "" {
		return fmt.Errorf("calendar expression must not be empty")
	}
	if len(expr) > MaxCalendarLength {
		return fmt.Errorf("calendar expression %q is longer than %d characters", expr, MaxCalendarLength)
	}
	if !calendarRE.MatchString(expr) {
		return fmt.Errorf("invalid calendar expression %q: use systemd syntax such as daily, Sat *-*-* 03:00:00 or Mon..Fri 07:00", expr)
	}
	return nil
}
