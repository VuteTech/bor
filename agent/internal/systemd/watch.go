// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Watch options.
const (
	// watchDebounce coalesces a burst of unit signals into one callback.
	watchDebounce   = 2 * time.Second
	watchMinBackoff = 5 * time.Second
	watchMaxBackoff = 60 * time.Second
)

// WatchUnits follows systemd's D-Bus signals and calls onChange (with a
// short reason) when a unit named by managed() changes its active or
// unit-file state, or when unit files are enabled, disabled or masked by
// hand (UnitFilesChanged). Events are debounced; the watcher runs on its
// own bus connection so a burst of signals never delays a job wait on the
// shared connection, and it reconnects with backoff until ctx ends. A
// node without a reachable bus degrades to the reconcile ticker.
func WatchUnits(ctx context.Context, managed func() []string, onChange func(reason string)) {
	backoff := watchMinBackoff
	for ctx.Err() == nil {
		err := watchOnce(ctx, managed, onChange)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("systemd: signal watcher stopped (%v); retrying in %v", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > watchMaxBackoff {
			backoff = watchMaxBackoff
		}
	}
}

func watchOnce(ctx context.Context, managed func() []string, onChange func(reason string)) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	err = conn.Object(busName, managerPath).CallWithContext(callCtx, managerIface+".Subscribe", 0).Err
	cancel()
	if err != nil && !strings.Contains(err.Error(), "AlreadySubscribed") {
		return err
	}
	matches := [][]dbus.MatchOption{
		{
			dbus.WithMatchSender(busName),
			dbus.WithMatchInterface(propsIface),
			dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchPathNamespace(dbus.ObjectPath(strings.TrimSuffix(unitPathBase, "/"))),
			dbus.WithMatchArg(0, unitIface),
		},
		{
			dbus.WithMatchSender(busName),
			dbus.WithMatchObjectPath(managerPath),
			dbus.WithMatchInterface(managerIface),
			dbus.WithMatchMember("UnitFilesChanged"),
		},
	}
	for _, m := range matches {
		if err := conn.AddMatchSignal(m...); err != nil {
			return err
		}
	}
	sigCh := make(chan *dbus.Signal, 256)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	var (
		pending *time.Timer
		fire    = make(chan struct{}, 1)
		reason  string
	)
	schedule := func(why string) {
		if reason == "" {
			reason = why
		}
		if pending == nil {
			pending = time.AfterFunc(watchDebounce, func() {
				select {
				case fire <- struct{}{}:
				default:
				}
			})
		}
	}
	defer func() {
		if pending != nil {
			pending.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-fire:
			why := reason
			reason = ""
			pending = nil
			onChange(why)
		case sig, ok := <-sigCh:
			if !ok {
				return errBusClosed
			}
			if why := watchReason(sig, managed); why != "" {
				schedule(why)
			}
		}
	}
}

// errBusClosed reports a lost connection.
var errBusClosed = dbusError("bus connection closed")

type dbusError string

func (e dbusError) Error() string { return string(e) }

// watchReason classifies a signal: a non-empty reason means a managed unit
// may have drifted.
func watchReason(sig *dbus.Signal, managed func() []string) string {
	switch sig.Name {
	case managerIface + ".UnitFilesChanged":
		return "unit files changed"
	case propsIface + ".PropertiesChanged":
		if len(sig.Body) < 2 {
			return ""
		}
		iface, _ := sig.Body[0].(string)
		if iface != unitIface {
			return ""
		}
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		relevant := false
		for _, key := range []string{"ActiveState", "SubState", "UnitFileState", "LoadState"} {
			if _, ok := changed[key]; ok {
				relevant = true
				break
			}
		}
		if !relevant && len(sig.Body) > 2 {
			if inv, ok := sig.Body[2].([]string); ok {
				for _, key := range inv {
					if key == "ActiveState" || key == "UnitFileState" {
						relevant = true
						break
					}
				}
			}
		}
		if !relevant {
			return ""
		}
		name := unitNameFromPath(sig.Path, managed())
		if name == "" {
			return ""
		}
		if v, ok := changed["ActiveState"]; ok {
			if s, ok := v.Value().(string); ok {
				return name + " is now " + s
			}
		}
		return name + " changed"
	}
	return ""
}

// unitNameFromPath maps a unit object path back to one of the managed
// names, or "" when the path belongs to an unmanaged unit.
func unitNameFromPath(path dbus.ObjectPath, names []string) string {
	p := string(path)
	if !strings.HasPrefix(p, unitPathBase) {
		return ""
	}
	escaped := p[len(unitPathBase):]
	for _, n := range names {
		if EscapeObjectPath(n) == escaped {
			return n
		}
	}
	return ""
}
