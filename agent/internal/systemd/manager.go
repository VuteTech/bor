// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package systemd drives the system manager over D-Bus
// (org.freedesktop.systemd1) for the Services & timers policy: unit state
// queries, enable / disable / mask, start / stop / restart with job
// completion, daemon reloads and the unit inventory. The Manager interface
// keeps the enforcement code testable with the in-memory Fake.
package systemd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunDir exists when systemd is the init system of the node.
const RunDir = "/run/systemd/system"

// IsInit reports whether systemd runs as PID 1 on this node.
func IsInit() bool {
	st, err := os.Stat(RunDir)
	return err == nil && st.IsDir()
}

// UnitStatus is the subset of unit, service and timer properties the
// policy code reads.
type UnitStatus struct {
	Name             string
	Description      string
	LoadState        string // loaded | not-found | masked | bad-setting | error
	ActiveState      string // active | reloading | inactive | failed | activating | deactivating
	SubState         string
	UnitFileState    string // enabled | disabled | static | masked | indirect | generated | transient | ...
	UnitFilePreset   string
	FragmentPath     string
	DropInPaths      []string
	NeedDaemonReload bool
	Triggers         []string

	// Services.
	ServiceType     string
	Result          string
	ExecMainStatus  int32
	NRestarts       uint32
	RemainAfterExit bool
	ExecMainStart   time.Time

	// Timers.
	TimerCalendar  []string // calendar expressions of the loaded timer
	TimerMonotonic []string // "OnBootSec=15min" style entries
	TimerUnit      string   // the unit the timer activates
	NextElapse     time.Time
	LastTrigger    time.Time
	TimerResult    string
}

// Type returns the unit type by suffix ("service", "timer", ...).
func (s *UnitStatus) Type() string {
	if i := strings.LastIndexByte(s.Name, '.'); i >= 0 {
		return s.Name[i+1:]
	}
	return ""
}

// Exists reports whether the unit is installed on the node.
func (s *UnitStatus) Exists() bool {
	return s.LoadState != "not-found"
}

// IsActive reports whether the unit is active or on its way to it.
func (s *UnitStatus) IsActive() bool {
	switch s.ActiveState {
	case "active", "reloading", "activating", "refreshing":
		return true
	}
	return false
}

// IsMasked reports whether the unit file is masked.
func (s *UnitStatus) IsMasked() bool {
	return s.UnitFileState == "masked" || s.UnitFileState == "masked-runtime" || s.LoadState == "masked"
}

// IsOneshot reports whether the unit is a oneshot service that does not
// stay active after it ran.
func (s *UnitStatus) IsOneshot() bool {
	return s.Type() == "service" && s.ServiceType == "oneshot" && !s.RemainAfterExit
}

// HasInstall reports whether the unit file can be enabled or disabled,
// judged from its unit-file state.
func (s *UnitStatus) HasInstall() bool {
	switch s.UnitFileState {
	case "enabled", "enabled-runtime", "disabled", "indirect", "linked", "linked-runtime", "alias":
		return true
	}
	return false
}

// UnitFile is one entry of the unit file inventory.
type UnitFile struct {
	Path  string
	State string
}

// JobResult is the outcome of a start, stop or restart job as systemd
// reports it in JobRemoved.
type JobResult string

// Job results.
const (
	JobDone       JobResult = "done"
	JobFailed     JobResult = "failed"
	JobSkipped    JobResult = "skipped"
	JobTimeout    JobResult = "timeout"
	JobCanceled   JobResult = "canceled"
	JobDependency JobResult = "dependency"
)

// OK reports whether the job reached its goal; a skipped job means a
// condition of the unit was not met, which is not a failure.
func (r JobResult) OK() bool {
	return r == JobDone || r == JobSkipped
}

// ErrNoInstall is returned by Enable when the unit has no [Install]
// section and cannot be enabled.
var ErrNoInstall = fmt.Errorf("unit has no [Install] section")

// Manager is what the policy code needs from systemd.
type Manager interface {
	// Available reports whether systemd runs as PID 1 and answers on the bus.
	Available() bool
	// Version returns the manager version string, e.g. "252".
	Version(ctx context.Context) (string, error)
	// Status loads the unit (without activating it) and returns its state.
	// A unit that is not installed has LoadState "not-found".
	Status(ctx context.Context, name string) (*UnitStatus, error)
	// ListUnitFiles returns the installed unit files matching the glob
	// patterns, loaded or not.
	ListUnitFiles(ctx context.Context, patterns []string) ([]UnitFile, error)
	// ListUnits returns the loaded units matching the glob patterns with
	// their load, active and sub states.
	ListUnits(ctx context.Context, patterns []string) ([]UnitStatus, error)
	Enable(ctx context.Context, names []string) error
	Disable(ctx context.Context, names []string) error
	Mask(ctx context.Context, names []string) error
	Unmask(ctx context.Context, names []string) error
	// Reload makes systemd re-read unit files and drop-ins.
	Reload(ctx context.Context) error
	Start(ctx context.Context, name string) (JobResult, error)
	Stop(ctx context.Context, name string) (JobResult, error)
	Restart(ctx context.Context, name string) (JobResult, error)
}

// UnitTypes are the unit types the policy manages, by suffix.
var UnitTypes = []string{"service", "timer", "socket", "target", "path"}

// UnitPatterns returns the glob patterns for UnitTypes.
func UnitPatterns() []string {
	out := make([]string, 0, len(UnitTypes))
	for _, t := range UnitTypes {
		out = append(out, "*."+t)
	}
	return out
}

// EscapeObjectPath encodes a unit name the way systemd derives unit
// object paths: every byte outside [A-Za-z0-9] becomes _XX (lower-case
// hex). "bluetooth.service" -> "bluetooth_2eservice".
func EscapeObjectPath(name string) string {
	var b strings.Builder
	b.Grow(len(name) * 3)
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "_%02x", c)
		}
	}
	return b.String()
}

// usecToTime converts a CLOCK_REALTIME microsecond timestamp; 0 and the
// "infinity" marker yield the zero time.
func usecToTime(usec uint64) time.Time {
	if usec == 0 || usec == ^uint64(0) {
		return time.Time{}
	}
	return time.UnixMicro(int64(usec)) //nolint:gosec // G115: realtime timestamps fit in int64 for the foreseeable future
}

// monotonicUsecToTime converts a CLOCK_MONOTONIC microsecond deadline to
// wall-clock time using the node's uptime.
func monotonicUsecToTime(usec uint64, uptime time.Duration) time.Time {
	if usec == 0 || usec == ^uint64(0) {
		return time.Time{}
	}
	deadline := time.Duration(usec) * time.Microsecond //nolint:gosec // G115: monotonic deadlines are far below the int64 range
	return time.Now().Add(deadline - uptime)
}

// readUptime returns the node's uptime from /proc/uptime.
func readUptime() time.Duration {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	var secs float64
	if _, err := fmt.Sscanf(fields[0], "%f", &secs); err != nil {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}
