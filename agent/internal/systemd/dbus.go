// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	busName       = "org.freedesktop.systemd1"
	managerPath   = dbus.ObjectPath("/org/freedesktop/systemd1")
	managerIface  = "org.freedesktop.systemd1.Manager"
	unitIface     = "org.freedesktop.systemd1.Unit"
	serviceIface  = "org.freedesktop.systemd1.Service"
	timerIface    = "org.freedesktop.systemd1.Timer"
	propsIface    = "org.freedesktop.DBus.Properties"
	unitPathBase  = "/org/freedesktop/systemd1/unit/"
	callTimeout   = 30 * time.Second
	reloadTimeout = 2 * time.Minute

	// DefaultJobTimeout bounds the wait for a start, stop or restart job.
	DefaultJobTimeout = 90 * time.Second
)

// Bus is the Manager over the system bus. One Bus holds one connection;
// a lost connection surfaces as errors and the caller reconnects by
// creating a new Bus.
type Bus struct {
	conn       *dbus.Conn
	jobTimeout time.Duration
	jobMu      sync.Mutex
	// user marks a connection to a user's manager (see ConnectUser).
	user bool
}

// IsUser reports whether the Bus drives a user's manager rather than the
// system manager.
func (b *Bus) IsUser() bool {
	return b != nil && b.user
}

// Connect opens a system bus connection and subscribes to manager
// signals (systemd emits JobRemoved only to subscribers).
func Connect(jobTimeout time.Duration) (*Bus, error) {
	if jobTimeout <= 0 {
		jobTimeout = DefaultJobTimeout
	}
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	b := &Bus{conn: conn, jobTimeout: jobTimeout}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := b.manager().CallWithContext(ctx, managerIface+".Subscribe", 0).Err; err != nil {
		// AlreadySubscribed is not an error.
		if !strings.Contains(err.Error(), "AlreadySubscribed") {
			_ = conn.Close()
			return nil, fmt.Errorf("subscribe to systemd signals: %w", err)
		}
	}
	return b, nil
}

// Close closes the bus connection.
func (b *Bus) Close() error {
	if b == nil || b.conn == nil {
		return nil
	}
	return b.conn.Close()
}

func (b *Bus) manager() dbus.BusObject {
	return b.conn.Object(busName, managerPath)
}

// Available reports whether systemd is PID 1 and the manager answers.
func (b *Bus) Available() bool {
	if b == nil || b.conn == nil || !IsInit() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := b.Version(ctx)
	return err == nil
}

// Version returns the manager's Version property.
func (b *Bus) Version(ctx context.Context) (string, error) {
	var v dbus.Variant
	if err := b.manager().CallWithContext(ctx, propsIface+".Get", 0, managerIface, "Version").Store(&v); err != nil {
		return "", fmt.Errorf("systemd Version: %w", err)
	}
	s, _ := v.Value().(string)
	return s, nil
}

// Status loads the unit and reads its Unit properties plus the Service or
// Timer properties when the type has them.
func (b *Bus) Status(ctx context.Context, name string) (*UnitStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var path dbus.ObjectPath
	if err := b.manager().CallWithContext(ctx, managerIface+".LoadUnit", 0, name).Store(&path); err != nil {
		return nil, fmt.Errorf("LoadUnit %s: %w", name, err)
	}
	obj := b.conn.Object(busName, path)
	props, err := getAll(ctx, obj, unitIface)
	if err != nil {
		return nil, fmt.Errorf("unit properties of %s: %w", name, err)
	}
	st := &UnitStatus{
		Name:             name,
		Description:      propString(props, "Description"),
		LoadState:        propString(props, "LoadState"),
		ActiveState:      propString(props, "ActiveState"),
		SubState:         propString(props, "SubState"),
		UnitFileState:    propString(props, "UnitFileState"),
		UnitFilePreset:   propString(props, "UnitFilePreset"),
		FragmentPath:     propString(props, "FragmentPath"),
		DropInPaths:      propStrings(props, "DropInPaths"),
		NeedDaemonReload: propBool(props, "NeedDaemonReload"),
		Triggers:         propStrings(props, "Triggers"),
	}
	if id := propString(props, "Id"); id != "" {
		st.Name = id
	}
	if !st.Exists() {
		return st, nil
	}
	switch st.Type() {
	case "service":
		sp, err := getAll(ctx, obj, serviceIface)
		if err != nil {
			return nil, fmt.Errorf("service properties of %s: %w", name, err)
		}
		st.ServiceType = propString(sp, "Type")
		st.Result = propString(sp, "Result")
		st.ExecMainStatus = propInt32(sp, "ExecMainStatus")
		st.NRestarts = propUint32(sp, "NRestarts")
		st.RemainAfterExit = propBool(sp, "RemainAfterExit")
		st.ExecMainStart = usecToTime(propUint64(sp, "ExecMainStartTimestamp"))
	case "timer":
		tp, err := getAll(ctx, obj, timerIface)
		if err != nil {
			return nil, fmt.Errorf("timer properties of %s: %w", name, err)
		}
		st.TimerUnit = propString(tp, "Unit")
		st.TimerResult = propString(tp, "Result")
		st.LastTrigger = usecToTime(propUint64(tp, "LastTriggerUSec"))
		st.NextElapse = usecToTime(propUint64(tp, "NextElapseUSecRealtime"))
		if st.NextElapse.IsZero() {
			st.NextElapse = monotonicUsecToTime(propUint64(tp, "NextElapseUSecMonotonic"), readUptime())
		}
		st.TimerCalendar, st.TimerMonotonic = timerEntries(tp)
	}
	return st, nil
}

// ListUnitFiles calls ListUnitFilesByPatterns with no state filter.
func (b *Bus) ListUnitFiles(ctx context.Context, patterns []string) ([]UnitFile, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var raw [][]interface{}
	if err := b.manager().CallWithContext(ctx, managerIface+".ListUnitFilesByPatterns", 0, []string{}, patterns).Store(&raw); err != nil {
		return nil, fmt.Errorf("ListUnitFilesByPatterns: %w", err)
	}
	out := make([]UnitFile, 0, len(raw))
	for _, r := range raw {
		if len(r) < 2 {
			continue
		}
		path, _ := r[0].(string)
		state, _ := r[1].(string)
		out = append(out, UnitFile{Path: path, State: state})
	}
	return out, nil
}

// ListUnits calls ListUnitsByPatterns with no state filter.
func (b *Bus) ListUnits(ctx context.Context, patterns []string) ([]UnitStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var raw [][]interface{}
	if err := b.manager().CallWithContext(ctx, managerIface+".ListUnitsByPatterns", 0, []string{}, patterns).Store(&raw); err != nil {
		return nil, fmt.Errorf("ListUnitsByPatterns: %w", err)
	}
	out := make([]UnitStatus, 0, len(raw))
	for _, r := range raw {
		if len(r) < 5 {
			continue
		}
		name, _ := r[0].(string)
		desc, _ := r[1].(string)
		load, _ := r[2].(string)
		active, _ := r[3].(string)
		sub, _ := r[4].(string)
		out = append(out, UnitStatus{Name: name, Description: desc, LoadState: load, ActiveState: active, SubState: sub})
	}
	return out, nil
}

// Enable enables the unit files (persistently, without force).
func (b *Bus) Enable(ctx context.Context, names []string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var carriesInstall bool
	var changes [][]interface{}
	if err := b.manager().CallWithContext(ctx, managerIface+".EnableUnitFiles", 0, names, false, false).Store(&carriesInstall, &changes); err != nil {
		return fmt.Errorf("EnableUnitFiles %v: %w", names, err)
	}
	if !carriesInstall && len(changes) == 0 {
		return ErrNoInstall
	}
	return nil
}

// Disable disables the unit files persistently.
func (b *Bus) Disable(ctx context.Context, names []string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var changes [][]interface{}
	if err := b.manager().CallWithContext(ctx, managerIface+".DisableUnitFiles", 0, names, false).Store(&changes); err != nil {
		return fmt.Errorf("DisableUnitFiles %v: %w", names, err)
	}
	return nil
}

// Mask masks the unit files persistently. force is true so that an
// existing symlink in /etc/systemd/system (a distribution's redirect such
// as ctrl-alt-del.target to reboot.target) is replaced by the mask; the
// policy code records such a link and puts it back on restore. A regular
// unit file in /etc is still refused by systemd rather than replaced.
func (b *Bus) Mask(ctx context.Context, names []string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var changes [][]interface{}
	if err := b.manager().CallWithContext(ctx, managerIface+".MaskUnitFiles", 0, names, false, true).Store(&changes); err != nil {
		return fmt.Errorf("MaskUnitFiles %v: %w", names, err)
	}
	return nil
}

// Unmask removes persistent masks.
func (b *Bus) Unmask(ctx context.Context, names []string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var changes [][]interface{}
	if err := b.manager().CallWithContext(ctx, managerIface+".UnmaskUnitFiles", 0, names, false).Store(&changes); err != nil {
		return fmt.Errorf("UnmaskUnitFiles %v: %w", names, err)
	}
	return nil
}

// Reload performs a daemon-reload.
func (b *Bus) Reload(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, reloadTimeout)
	defer cancel()
	if err := b.manager().CallWithContext(ctx, managerIface+".Reload", 0).Err; err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

// Start starts the unit and waits for the job to finish.
func (b *Bus) Start(ctx context.Context, name string) (JobResult, error) {
	return b.runJob(ctx, "StartUnit", name)
}

// Stop stops the unit and waits for the job to finish.
func (b *Bus) Stop(ctx context.Context, name string) (JobResult, error) {
	return b.runJob(ctx, "StopUnit", name)
}

// Restart restarts the unit and waits for the job to finish.
func (b *Bus) Restart(ctx context.Context, name string) (JobResult, error) {
	return b.runJob(ctx, "RestartUnit", name)
}

// runJob queues a unit job in "replace" mode and waits for its JobRemoved
// signal. The match is registered before the call so a job that finishes
// immediately is not missed. Jobs run one at a time per Bus.
func (b *Bus) runJob(ctx context.Context, method, name string) (JobResult, error) {
	b.jobMu.Lock()
	defer b.jobMu.Unlock()

	match := []dbus.MatchOption{
		dbus.WithMatchSender(busName),
		dbus.WithMatchObjectPath(managerPath),
		dbus.WithMatchInterface(managerIface),
		dbus.WithMatchMember("JobRemoved"),
	}
	if err := b.conn.AddMatchSignal(match...); err != nil {
		return "", fmt.Errorf("subscribe to JobRemoved: %w", err)
	}
	defer func() { _ = b.conn.RemoveMatchSignal(match...) }()
	sigCh := make(chan *dbus.Signal, 64)
	b.conn.Signal(sigCh)
	defer b.conn.RemoveSignal(sigCh)

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var job dbus.ObjectPath
	if err := b.manager().CallWithContext(callCtx, managerIface+"."+method, 0, name, "replace").Store(&job); err != nil {
		return "", fmt.Errorf("%s %s: %w", method, name, err)
	}

	timer := time.NewTimer(b.jobTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return JobTimeout, fmt.Errorf("%s %s: no result after %v", method, name, b.jobTimeout)
		case sig, ok := <-sigCh:
			if !ok {
				return "", fmt.Errorf("%s %s: bus connection closed", method, name)
			}
			if sig.Name != managerIface+".JobRemoved" || len(sig.Body) < 4 {
				continue
			}
			jobPath, _ := sig.Body[1].(dbus.ObjectPath)
			if jobPath != job {
				continue
			}
			result, _ := sig.Body[3].(string)
			return JobResult(result), nil
		}
	}
}

// ── property helpers ─────────────────────────────────────────────────────

func getAll(ctx context.Context, obj dbus.BusObject, iface string) (map[string]dbus.Variant, error) {
	var props map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, propsIface+".GetAll", 0, iface).Store(&props); err != nil {
		return nil, err
	}
	return props, nil
}

func propString(props map[string]dbus.Variant, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.Value().(string); ok {
			return s
		}
	}
	return ""
}

func propStrings(props map[string]dbus.Variant, key string) []string {
	if v, ok := props[key]; ok {
		if s, ok := v.Value().([]string); ok {
			return s
		}
	}
	return nil
}

func propBool(props map[string]dbus.Variant, key string) bool {
	if v, ok := props[key]; ok {
		if b, ok := v.Value().(bool); ok {
			return b
		}
	}
	return false
}

func propInt32(props map[string]dbus.Variant, key string) int32 {
	if v, ok := props[key]; ok {
		if n, ok := v.Value().(int32); ok {
			return n
		}
	}
	return 0
}

func propUint32(props map[string]dbus.Variant, key string) uint32 {
	if v, ok := props[key]; ok {
		if n, ok := v.Value().(uint32); ok {
			return n
		}
	}
	return 0
}

func propUint64(props map[string]dbus.Variant, key string) uint64 {
	if v, ok := props[key]; ok {
		if n, ok := v.Value().(uint64); ok {
			return n
		}
	}
	return 0
}

// timerEntries decodes TimersCalendar a(sst) into calendar expressions and
// TimersMonotonic a(stt) into "Directive=value" strings.
func timerEntries(props map[string]dbus.Variant) (calendar, monotonic []string) {
	if v, ok := props["TimersCalendar"]; ok {
		if rows, ok := v.Value().([][]interface{}); ok {
			for _, r := range rows {
				if len(r) >= 2 {
					if expr, ok := r[1].(string); ok {
						calendar = append(calendar, expr)
					}
				}
			}
		}
	}
	if v, ok := props["TimersMonotonic"]; ok {
		if rows, ok := v.Value().([][]interface{}); ok {
			for _, r := range rows {
				if len(r) >= 2 {
					directive, _ := r[0].(string)
					usec, _ := r[1].(uint64)
					monotonic = append(monotonic, fmt.Sprintf("%s=%s", directive, (time.Duration(usec)*time.Microsecond).String())) //nolint:gosec // G115: timer offsets are far below the int64 range
				}
			}
		}
	}
	return calendar, monotonic
}
