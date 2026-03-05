// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package notify provides desktop notification and KDE application
// reconfigure support via D-Bus. It is used by the agent to inform
// logged-in users when KDE policies have been updated.
//
// Because the agent runs as root and dbus-broker rejects root's
// SO_PEERCRED on the user's session bus, all D-Bus calls are executed
// as subprocesses running under the target user's UID/GID via
// SysProcAttr.Credential.
package notify

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// NotifyConfig holds server-provided notification settings.
type NotifyConfig struct {
	Enabled  bool
	Cooldown time.Duration
	Message  string
}

// NotifyDebounce is the delay before sending a notification after the
// last change. Rapid successive changes reset the timer so that a
// single notification covers an entire batch of admin edits.
const NotifyDebounce = 10 * time.Second

// Notifier sends desktop notifications and KDE reconfigure signals
// to active user sessions via D-Bus.
type Notifier struct {
	mu             sync.Mutex
	lastSent       map[uint32]time.Time // UID → last notification time
	lastNotifyID   map[uint32]uint32    // UID → last notify-send notification ID

	debounceMu    sync.Mutex
	debounceTimer *time.Timer
	pendingFiles  map[string]bool
	pendingConfig NotifyConfig
}

// New creates a Notifier.
func New() *Notifier {
	return &Notifier{
		lastSent:     make(map[uint32]time.Time),
		lastNotifyID: make(map[uint32]uint32),
	}
}

// ScheduleNotification accumulates changed files and resets the
// debounce timer. When the timer fires (after NotifyDebounce of
// inactivity), a single NotifyAndReconfigure call is made covering
// all accumulated files.
func (n *Notifier) ScheduleNotification(cfg NotifyConfig, changedFiles map[string]bool) {
	n.debounceMu.Lock()
	defer n.debounceMu.Unlock()

	if n.pendingFiles == nil {
		n.pendingFiles = make(map[string]bool)
	}
	for k, v := range changedFiles {
		n.pendingFiles[k] = v
	}
	n.pendingConfig = cfg

	if n.debounceTimer != nil {
		n.debounceTimer.Stop()
	}
	n.debounceTimer = time.AfterFunc(NotifyDebounce, n.flush)
}

// flush sends the accumulated notification and clears the pending state.
func (n *Notifier) flush() {
	n.debounceMu.Lock()
	files := n.pendingFiles
	cfg := n.pendingConfig
	n.pendingFiles = nil
	n.debounceTimer = nil
	n.debounceMu.Unlock()

	if len(files) > 0 {
		n.NotifyAndReconfigure(cfg, files)
	}
}

// session holds information about an active graphical login session.
type session struct {
	UID  uint32
	GID  uint32
	User string
}

// NotifyAndReconfigure sends desktop notifications to all active
// graphical sessions and triggers reconfigure signals for KDE apps
// whose config files have changed.
//
// changedFiles is the set of KConfig file basenames that were written
// (e.g. "kwinrc", "kdeglobals"). cfg controls whether notifications
// are sent, the cooldown, and the message text.
//
// All errors are logged but never returned — notification is
// best-effort and must not block policy enforcement.
func (n *Notifier) NotifyAndReconfigure(cfg NotifyConfig, changedFiles map[string]bool) {
	if !cfg.Enabled && len(changedFiles) == 0 {
		return
	}

	sessions, err := activeGraphicalSessions()
	if err != nil {
		log.Printf("notify: failed to enumerate sessions: %v", err)
		return
	}

	if len(sessions) == 0 {
		log.Println("notify: no active graphical sessions found")
		return
	}

	// Deduplicate sessions by UID (a user may have multiple sessions).
	seen := make(map[uint32]bool)
	var unique []session
	for _, s := range sessions {
		if !seen[s.UID] {
			seen[s.UID] = true
			unique = append(unique, s)
		}
	}

	now := time.Now()

	for _, s := range unique {
		// Send desktop notification (with cooldown).
		if cfg.Enabled {
			if n.shouldNotify(s.UID, now, cfg.Cooldown) {
				if err := n.sendNotification(s, cfg.Message); err != nil {
					log.Printf("notify: failed to send notification to UID %d: %v", s.UID, err)
				} else {
					log.Printf("notify: sent desktop notification to user %s (UID %d)", s.User, s.UID)
					n.recordNotification(s.UID, now)
				}
			} else {
				log.Printf("notify: skipping notification for UID %d (cooldown)", s.UID)
			}
		}

		// Trigger app-specific reconfigure signals.
		reconfigureApps(s, changedFiles)
	}
}

// shouldNotify checks whether enough time has elapsed since the last
// notification to this UID.
func (n *Notifier) shouldNotify(uid uint32, now time.Time, cooldown time.Duration) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	last, ok := n.lastSent[uid]
	return !ok || now.Sub(last) >= cooldown
}

// recordNotification records the timestamp of the last notification.
func (n *Notifier) recordNotification(uid uint32, t time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastSent[uid] = t
}

// sendNotification sends a freedesktop desktop notification using
// notify-send, executed as the target user. It uses --print-id to
// capture the notification ID and --replace-id on subsequent calls so
// that repeated notifications replace the previous one rather than
// creating new ones (which triggers ExcessNotificationGeneration).
func (n *Notifier) sendNotification(s session, message string) error {
	n.mu.Lock()
	replaceID := n.lastNotifyID[s.UID]
	n.mu.Unlock()

	args := []string{
		"-a", "Bor Policy Agent",
		"-i", "dialog-information",
		"-t", "0", // never auto-dismiss; stays until the user clears it
		"--print-id",
	}
	if replaceID > 0 {
		args = append(args, fmt.Sprintf("--replace-id=%d", replaceID))
	}
	args = append(args, "Desktop Policies Updated", message)

	output, err := runAsUserOutput(s, "notify-send", args...)
	if err != nil {
		return err
	}

	// Store the returned ID for replacement on the next notification.
	if id, err := strconv.ParseUint(strings.TrimSpace(output), 10, 32); err == nil && id > 0 {
		n.mu.Lock()
		n.lastNotifyID[s.UID] = uint32(id)
		n.mu.Unlock()
	}
	return nil
}

// reconfigureApps sends D-Bus reconfigure calls to KDE applications
// based on which config files changed.
func reconfigureApps(s session, changedFiles map[string]bool) {
	if changedFiles["kwinrc"] {
		if err := reconfigureKWin(s); err != nil {
			log.Printf("notify: KWin reconfigure failed for UID %d: %v", s.UID, err)
		} else {
			log.Printf("notify: KWin reconfigure sent to UID %d", s.UID)
		}
	}

	if changedFiles["plasma-org.kde.plasma.desktop-appletsrc"] {
		if err := reconfigurePlasmaShell(s); err != nil {
			log.Printf("notify: Plasma shell reconfigure failed for UID %d: %v", s.UID, err)
		} else {
			log.Printf("notify: Plasma shell reconfigure sent to UID %d", s.UID)
		}
	}

	if changedFiles["plasmarc"] || changedFiles["kdeglobals"] {
		if err := reconfigurePlasmaShell(s); err != nil {
			log.Printf("notify: Plasma shell reconfigure failed for UID %d: %v", s.UID, err)
		} else {
			log.Printf("notify: Plasma shell reconfigure sent to UID %d", s.UID)
		}
	}

	if changedFiles["kscreenlockerrc"] {
		if err := reconfigureScreenLocker(s); err != nil {
			log.Printf("notify: screen locker reconfigure failed for UID %d: %v", s.UID, err)
		} else {
			log.Printf("notify: screen locker reconfigure sent to UID %d", s.UID)
		}
	}
}

// reconfigureKWin tells KWin to reload its configuration.
func reconfigureKWin(s session) error {
	return dbusCall(s, "org.kde.KWin", "/KWin", "org.kde.KWin.reconfigure")
}

// reconfigurePlasmaShell tells Plasma shell to refresh.
func reconfigurePlasmaShell(s session) error {
	return dbusCall(s, "org.kde.plasmashell", "/PlasmaShell", "org.kde.PlasmaShell.refreshCurrentDesktop")
}

// reconfigureScreenLocker tells the KDE screen locker to reload config.
func reconfigureScreenLocker(s session) error {
	return dbusCall(s, "org.kde.screensaver", "/ScreenSaver", "org.kde.screensaver.configure")
}

// dbusCall sends a D-Bus method call via dbus-send running as the target user.
func dbusCall(s session, dest, objectPath, method string) error {
	return runAsUser(s, "dbus-send",
		"--session",
		"--type=method_call",
		"--dest="+dest,
		objectPath,
		method,
	)
}

// runAsUser executes a command as the target user with their session bus
// environment. The subprocess inherits the target UID/GID so that
// SO_PEERCRED matches the bus owner and dbus-broker accepts the connection.
func runAsUser(s session, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: s.UID,
			Gid: s.GID,
		},
	}
	cmd.Env = []string{
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", s.UID),
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", s.UID),
		"HOME=/home/" + s.User,
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runAsUserOutput is like runAsUser but returns the command's stdout.
func runAsUserOutput(s session, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: s.UID,
			Gid: s.GID,
		},
	}
	cmd.Env = []string{
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", s.UID),
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", s.UID),
		"HOME=/home/" + s.User,
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// activeGraphicalSessions enumerates active X11/Wayland login sessions
// by reading systemd session files from /run/systemd/sessions/.
func activeGraphicalSessions() ([]session, error) {
	sessDir := "/run/systemd/sessions"
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sessDir, err)
	}

	var sessions []session
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		props := parseSessionFile(string(data))

		// Only include active graphical sessions.
		sessionType := props["TYPE"]
		active := props["ACTIVE"]
		if (sessionType != "x11" && sessionType != "wayland") || active != "1" {
			continue
		}

		uidStr := props["UID"]
		uid, err := strconv.ParseUint(uidStr, 10, 32)
		if err != nil {
			continue
		}

		// Verify the user's session bus socket exists.
		busPath := fmt.Sprintf("/run/user/%d/bus", uid)
		if _, err := os.Stat(busPath); err != nil {
			continue
		}

		// Look up primary GID for the user.
		gid := uint32(uid) // fallback: assume GID == UID
		if u, err := user.LookupId(uidStr); err == nil {
			if g, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
				gid = uint32(g)
			}
		}

		sessions = append(sessions, session{
			UID:  uint32(uid),
			GID:  gid,
			User: props["USER"],
		})
	}

	return sessions, nil
}

// parseSessionFile parses a systemd session file (KEY=VALUE per line).
func parseSessionFile(content string) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			props[line[:idx]] = line[idx+1:]
		}
	}
	return props
}
