// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package notify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// AlertTarget describes a single-user desktop alert. Unlike
// NotifyAndReconfigure (which broadcasts a KConfig-update message to every
// session), this targets one user's session bus — used by the session
// access enforcer for schedule warnings.
type AlertTarget struct {
	UID, GID uint32
	User     string
	Summary  string
	Body     string
	// Critical raises urgency to level 2, which bypasses do-not-disturb in
	// GNOME and KDE — used for the final warning and the boundary notice.
	Critical bool
	// ReplaceID, when non-zero, replaces a prior notification so a countdown
	// updates in place instead of stacking. Pass the value returned by the
	// previous SendAlert for the same user.
	ReplaceID uint32
	// TimeoutMS is the expiry hint in milliseconds. 0 (the default) means
	// "never expire until dismissed or replaced" — the right behaviour for a
	// countdown that the next threshold replaces. A negative value is NOT
	// allowed: gdbus's option parser treats a leading "-" as a flag and
	// rejects the call, so any negative is clamped to 0.
	TimeoutMS int
}

// gdbusIDRE extracts the notification id from gdbus output "(uint32 7,)".
var gdbusIDRE = regexp.MustCompile(`uint32 (\d+)`)

// SendAlert posts a desktop notification to one user's session bus via
// gdbus, run as that user. Returns the notification id, reusable as ReplaceID
// next time.
//
// Why shell out to gdbus instead of a native D-Bus connection from the agent:
// the agent runs as root, and a user's *session* bus (/run/user/<uid>/bus)
// authenticates the peer by SO_PEERCRED and rejects root — so a notification
// must be delivered by a process running as the target user. (The agent does
// use godbus directly for PackageKit, but that is the *system* bus, where root
// is allowed.) Dropping privileges in this multi-threaded root process to send
// one message is not viable, so a short-lived child-as-user is unavoidable;
// once a child is required, gdbus is the pragmatic choice over embedding a
// D-Bus library or bundling a helper:
//   - it ships with glib, present on every GNOME/KDE desktop, so no extra
//     package dependency (notify-send/libnotify-bin is often absent — phase 0);
//   - it returns the notification id, which the replace-in-place countdown
//     needs (dbus-send does not surface the reply cleanly);
//   - it matches the child-as-user pattern the rest of this package already
//     uses (see runAsUserOutput).
//
// The tradeoff is string-based GVariant marshalling (the hints dict, the
// timeout), which is less safe than typed calls — the reason this is kept to
// one function. A native alternative would be a dedicated agent subcommand
// that drops to the user via SysProcAttr.Credential and uses godbus; worth it
// only if the string marshalling proves troublesome.
func SendAlert(a AlertTarget) (uint32, error) {
	urgency := byte(1) // normal
	if a.Critical {
		urgency = 2 // critical
	}
	timeout := a.TimeoutMS
	if timeout < 0 {
		timeout = 0 // gdbus rejects a leading "-"; 0 = never expire
	}
	hints := fmt.Sprintf("{'urgency': <byte %d>}", urgency)

	s := session{UID: a.UID, GID: a.GID, User: a.User}
	out, err := runAsUserOutput(s, "gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		"Bor Session Access",
		strconv.FormatUint(uint64(a.ReplaceID), 10),
		"dialog-warning",
		a.Summary,
		a.Body,
		"[]",
		hints,
		strconv.Itoa(timeout),
	)
	if err != nil {
		return 0, err
	}
	if m := gdbusIDRE.FindStringSubmatch(strings.TrimSpace(out)); m != nil {
		if id, perr := strconv.ParseUint(m[1], 10, 32); perr == nil {
			return uint32(id), nil
		}
	}
	return 0, nil
}
