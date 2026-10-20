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

// protectedGlobalExact are the user-manager units no policy may manage in
// the global scope, on top of the system list: the user session's exit
// path, its hook targets and the targets the desktop session hangs off.
var protectedGlobalExact = map[string]struct{}{
	"exit.target":                    {},
	"sockets.target":                 {},
	"timers.target":                  {},
	"paths.target":                   {},
	"graphical-session.target":       {},
	"graphical-session-pre.target":   {},
	"xdg-desktop-autostart.target":   {},
	"systemd-exit.service":           {},
	"systemd-tmpfiles-setup.service": {},
}

// IsProtectedGlobal reports whether no policy may manage the unit in the
// global (user manager) scope.
func IsProtectedGlobal(name string) bool {
	if IsProtected(name) {
		return true
	}
	_, ok := protectedGlobalExact[name]
	return ok
}

// riskyGlobalPrefixes are user-manager units whose stop can break the
// desktop session of every user: the audio stack, the desktop portals,
// the session buses and the session managers of the major desktops.
var riskyGlobalPrefixes = []string{
	"pipewire",
	"wireplumber",
	"pulseaudio",
	"xdg-desktop-portal",
	"plasma-",
	"gnome-session",
	"gnome-shell",
	"dbus-",
}

// IsRiskyGlobal reports whether the editor should ask for a confirmation
// before a policy manages the unit in the global scope.
func IsRiskyGlobal(name string) bool {
	if IsRisky(name) {
		return true
	}
	for _, p := range riskyGlobalPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Unit actions the node page may request.
const (
	UnitActionStart   = "start"
	UnitActionStop    = "stop"
	UnitActionRestart = "restart"
)

// ValidateUnitAction checks an on-demand action: a known verb on a valid,
// unprotected service, timer, socket or path.
func ValidateUnitAction(unit, action string) error {
	if err := ValidateName(unit); err != nil {
		return err
	}
	if IsProtected(unit) {
		return fmt.Errorf("%s is protected", unit)
	}
	if IsTemplate(unit) {
		return fmt.Errorf("%s is a template; name an instance", unit)
	}
	if Type(unit) == "target" {
		return fmt.Errorf("targets cannot be started or stopped on demand")
	}
	switch action {
	case UnitActionStart, UnitActionStop, UnitActionRestart:
		return nil
	}
	return fmt.Errorf("action must be start, stop or restart")
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

// ── Bor-deployed tasks ─────────────────────────────────────────────────────

// TaskUnitPrefix is prepended to a task name to form its unit names. The
// prefix is mandatory, so a task can never shadow a vendor unit.
const TaskUnitPrefix = "bor-"

// MaxTaskScriptBytes caps a task script; anything bigger belongs in a
// package.
const MaxTaskScriptBytes = 64 * 1024

// taskNameRE matches a task name: lower-case letters, digits and dashes,
// at most 48 characters, starting with a letter or digit.
var taskNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

// ValidateTaskName reports whether name is a well-formed task name (the
// bare name without the "bor-" prefix).
func ValidateTaskName(name string) error {
	if name == "" {
		return fmt.Errorf("task name must not be empty")
	}
	if !taskNameRE.MatchString(name) {
		return fmt.Errorf("task name %q must be 1-48 lower-case letters, digits or dashes and start with a letter or digit", name)
	}
	if strings.HasPrefix(name, TaskUnitPrefix) {
		return fmt.Errorf("task name %q must not start with %q; the prefix is added automatically", name, TaskUnitPrefix)
	}
	return nil
}

// TaskServiceUnit returns the service unit name of a task.
func TaskServiceUnit(name string) string {
	return TaskUnitPrefix + name + ".service"
}

// TaskTimerUnit returns the timer unit name of a task.
func TaskTimerUnit(name string) string {
	return TaskUnitPrefix + name + ".timer"
}

// IsTaskUnit reports whether a unit name belongs to a Bor task
// ("bor-<name>.service" or "bor-<name>.timer" with a valid task name).
func IsTaskUnit(unit string) bool {
	name, ok := strings.CutPrefix(unit, TaskUnitPrefix)
	if !ok {
		return false
	}
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return false
	}
	if t := name[i+1:]; t != "service" && t != "timer" {
		return false
	}
	return ValidateTaskName(name[:i]) == nil
}

// userNameRE matches a POSIX user or group name as the agent passes it into
// a unit file.
var userNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// ValidateUserName reports whether name is a well-formed user or group name.
func ValidateUserName(name string) error {
	if !userNameRE.MatchString(name) {
		return fmt.Errorf("user or group name %q must be 1-32 lower-case letters, digits, underscores or dashes and start with a letter or underscore", name)
	}
	return nil
}

// ── raw directives ─────────────────────────────────────────────────────────

// DirectiveSections lists the unit-file sections a raw directive may name.
var DirectiveSections = []string{"Unit", "Service", "Timer", "Socket", "Path", "Install"}

// ValidateDirectiveSection reports whether section is one of
// DirectiveSections.
func ValidateDirectiveSection(section string) error {
	for _, s := range DirectiveSections {
		if section == s {
			return nil
		}
	}
	return fmt.Errorf("section %q must be one of %s", section, strings.Join(DirectiveSections, ", "))
}

// directiveKeyRE matches a unit-file directive name.
var directiveKeyRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// MaxDirectiveKeyLength caps a directive name.
const MaxDirectiveKeyLength = 64

// ValidateDirectiveKey reports whether key is a well-formed directive name.
func ValidateDirectiveKey(key string) error {
	if key == "" {
		return fmt.Errorf("directive name must not be empty")
	}
	if len(key) > MaxDirectiveKeyLength || !directiveKeyRE.MatchString(key) {
		return fmt.Errorf("directive name %q must be a CamelCase systemd directive such as IPAddressDeny", key)
	}
	return nil
}

// IsExecDirective reports whether a directive runs a command (ExecStart,
// ExecStartPre, ExecStopPost, ExecReload, ExecCondition, ...). Policies
// that set one are command-running and need the systemd:exec permission.
func IsExecDirective(key string) bool {
	return strings.HasPrefix(key, "Exec")
}

// ValidateDirectiveValue reports whether a raw directive value can be
// written as one line of a unit file.
func ValidateDirectiveValue(value string) error {
	if strings.ContainsAny(value, "\n\r\x00") {
		return fmt.Errorf("directive value must be a single line")
	}
	return nil
}

var (
	apparmorProfileRE = regexp.MustCompile(`^-?[A-Za-z0-9_][A-Za-z0-9_.:/+-]{0,254}$`)
	selinuxContextRE  = regexp.MustCompile(`^-?[a-z0-9_]{1,64}:[a-z0-9_]{1,64}:[a-z0-9_]{1,128}(:[a-z0-9_.,:-]{1,128})?$`)
)

// ValidateAppArmorProfile checks an AppArmorProfile= value: the name of a
// profile as loaded on the node, optionally prefixed with "-" so a missing
// profile does not fail the unit.
func ValidateAppArmorProfile(s string) error {
	if !apparmorProfileRE.MatchString(s) {
		return fmt.Errorf("profile name %q: use the name the profile is loaded with (letters, digits, _ . : / + -), optionally prefixed with -", s)
	}
	return nil
}

// ValidateSELinuxContext checks a SELinuxContext= value: user:role:type
// with an optional level, optionally prefixed with "-" so a failed
// transition does not fail the unit.
func ValidateSELinuxContext(s string) error {
	if !selinuxContextRE.MatchString(s) {
		return fmt.Errorf("context %q: use user:role:type or user:role:type:level, optionally prefixed with -", s)
	}
	return nil
}
