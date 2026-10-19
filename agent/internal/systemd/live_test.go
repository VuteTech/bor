// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveBus exercises the read-only half of the bus manager against the
// systemd of the machine running the tests. It is skipped unless
// BOR_SYSTEMD_LIVE=1 is set, because it needs a system bus and PID 1.
func TestLiveBus(t *testing.T) {
	if os.Getenv("BOR_SYSTEMD_LIVE") != "1" {
		t.Skip("set BOR_SYSTEMD_LIVE=1 to run against the local systemd")
	}
	if !IsInit() {
		t.Skip("systemd is not PID 1 here")
	}
	bus, err := Connect(10 * time.Second)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = bus.Close() }()
	ctx := context.Background()

	if !bus.Available() {
		t.Fatal("bus reports unavailable")
	}
	ver, err := bus.Version(ctx)
	if err != nil || ver == "" {
		t.Fatalf("Version = %q, %v", ver, err)
	}
	t.Logf("systemd %s", ver)

	files, err := bus.ListUnitFiles(ctx, UnitPatterns())
	if err != nil {
		t.Fatalf("ListUnitFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no unit files listed")
	}
	t.Logf("%d unit files, first %+v", len(files), files[0])

	units, err := bus.ListUnits(ctx, UnitPatterns())
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	if len(units) == 0 || units[0].ActiveState == "" {
		t.Fatalf("ListUnits decoded nothing useful: %+v", units)
	}
	t.Logf("%d loaded units, first %s %s/%s/%s", len(units), units[0].Name, units[0].LoadState, units[0].ActiveState, units[0].SubState)

	// A timer every distribution ships; fall back to the tmpfiles one.
	timerName := "fstrim.timer"
	if probe, probeErr := bus.Status(ctx, timerName); probeErr != nil || !probe.Exists() {
		timerName = "systemd-tmpfiles-clean.timer"
	}
	st, err := bus.Status(ctx, timerName)
	if err != nil {
		t.Fatalf("Status(%s): %v", timerName, err)
	}
	t.Logf("%s: load=%s active=%s/%s file=%s unit=%s calendar=%v monotonic=%v next=%v last=%v result=%q",
		st.Name, st.LoadState, st.ActiveState, st.SubState, st.UnitFileState, st.TimerUnit,
		st.TimerCalendar, st.TimerMonotonic, st.NextElapse, st.LastTrigger, st.TimerResult)
	if st.Type() != "timer" || st.TimerUnit == "" {
		t.Errorf("timer properties not decoded: %+v", st)
	}

	svc, err := bus.Status(ctx, "dbus.service")
	if err != nil {
		t.Fatalf("Status(dbus.service): %v", err)
	}
	t.Logf("dbus.service: type=%s result=%s active=%s/%s exec=%v restarts=%d file=%s",
		svc.ServiceType, svc.Result, svc.ActiveState, svc.SubState, svc.ExecMainStart, svc.NRestarts, svc.UnitFileState)
	if svc.ServiceType == "" || !svc.IsActive() {
		t.Errorf("service properties not decoded: %+v", svc)
	}

	missing, err := bus.Status(ctx, "bor-no-such-unit.service")
	if err != nil {
		t.Fatalf("Status(missing): %v", err)
	}
	if missing.Exists() {
		t.Errorf("missing unit reported as existing: %+v", missing)
	}
}

// TestLiveUserBus connects to the manager of every user with a running
// session through systemd-stdio-bridge, which needs root. Skipped unless
// BOR_SYSTEMD_LIVE=1 and the test runs as root.
func TestLiveUserBus(t *testing.T) {
	if os.Getenv("BOR_SYSTEMD_LIVE") != "1" {
		t.Skip("set BOR_SYSTEMD_LIVE=1 to run against the local systemd")
	}
	if os.Geteuid() != 0 {
		t.Skip("user managers are reachable from root only")
	}
	sessions := ListUserSessions("")
	if len(sessions) == 0 {
		t.Skip("no user manager runs")
	}
	ctx := context.Background()
	for _, sess := range sessions {
		bus, err := ConnectUser(sess, "", 10*time.Second)
		if err != nil {
			t.Fatalf("ConnectUser(%s): %v", sess, err)
		}
		ver, err := bus.Version(ctx)
		if err != nil || ver == "" {
			t.Fatalf("%s: Version = %q, %v", sess, ver, err)
		}
		st, err := bus.Status(ctx, "dbus.service")
		if err != nil {
			t.Fatalf("%s: Status: %v", sess, err)
		}
		t.Logf("%s: systemd %s, dbus.service %s (%s)", sess, ver, st.ActiveState, st.SubState)
		units, err := bus.ListUnits(ctx, UnitPatterns())
		if err != nil || len(units) == 0 {
			t.Fatalf("%s: ListUnits: %d, %v", sess, len(units), err)
		}
		_ = bus.Close()
	}
}
