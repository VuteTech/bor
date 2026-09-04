// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package logind

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	login1Service   = "org.freedesktop.login1"
	login1SessionNS = dbus.ObjectPath("/org/freedesktop/login1/session")
	dbusPropsIface  = "org.freedesktop.DBus.Properties"

	watchBackoffInitial = time.Second
	watchBackoffLimit   = 30 * time.Second
)

// WatchLockChanges subscribes to logind session LockedHint changes on the
// system bus and invokes onChange for each, reconnecting with backoff if the
// bus connection is lost. It blocks until ctx is cancelled.
//
// This is the event-driven half of the re-lock watchdog: when a user unlocks a
// session (the desktop locker reports it by clearing LockedHint via logind,
// which emits a PropertiesChanged signal), the enforcer is kicked to run an
// immediate pass and re-lock the session if it is outside allowed hours —
// instead of waiting up to one poll interval. Polling remains as a backstop,
// so a lost watcher degrades to slower re-locks rather than none; the
// reconnect loop keeps that degradation temporary.
func WatchLockChanges(ctx context.Context, onChange func()) {
	backoff := watchBackoffInitial
	degraded := false
	for {
		started := time.Now()
		err := watchOnce(ctx, onChange, func() {
			if degraded {
				log.Println("session access: unlock watcher re-established")
				degraded = false
			}
		})
		if ctx.Err() != nil {
			return
		}
		if !degraded {
			log.Printf("Warning: session unlock watcher lost (%v); re-lock falls back to polling until reconnected", err)
			degraded = true
		}
		// A connection that lasted a while earns a fresh backoff.
		if time.Since(started) > time.Minute {
			backoff = watchBackoffInitial
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, watchBackoffLimit)
	}
}

// watchOnce runs a single connect/subscribe/drain cycle. onReady is called
// once the subscription is live.
func watchOnce(ctx context.Context, onChange, onReady func()) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	defer func() { _ = conn.Close() }()

	match := []dbus.MatchOption{
		dbus.WithMatchSender(login1Service),
		dbus.WithMatchInterface(dbusPropsIface),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchPathNamespace(login1SessionNS),
	}
	if err := conn.AddMatchSignal(match...); err != nil {
		return fmt.Errorf("add signal match: %w", err)
	}

	ch := make(chan *dbus.Signal, 32)
	conn.Signal(ch)
	onReady()

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-ch:
			if !ok {
				return fmt.Errorf("signal channel closed")
			}
			if signalTouchesLockedHint(sig) {
				onChange()
			}
		}
	}
}

// signalTouchesLockedHint reports whether a login1 PropertiesChanged signal
// carries a LockedHint change, so the watcher ignores unrelated property churn
// (IdleHint, Active, …). The body is (interface, changed{}, invalidated[]).
func signalTouchesLockedHint(sig *dbus.Signal) bool {
	if sig == nil || len(sig.Body) < 3 {
		return false
	}
	if changed, ok := sig.Body[1].(map[string]dbus.Variant); ok {
		if _, has := changed["LockedHint"]; has {
			return true
		}
	}
	if invalidated, ok := sig.Body[2].([]string); ok {
		return slices.Contains(invalidated, "LockedHint")
	}
	return false
}
