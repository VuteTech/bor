// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEscapeObjectPath(t *testing.T) {
	cases := map[string]string{
		"bluetooth.service":          "bluetooth_2eservice",
		"fstrim.timer":               "fstrim_2etimer",
		"getty@tty1.service":         "getty_40tty1_2eservice",
		"systemd-logind.service":     "systemd_2dlogind_2eservice",
		"dev-disk-by\\x2duuid.mount": "dev_2ddisk_2dby_5cx2duuid_2emount",
		"a_b.service":                "a_5fb_2eservice",
	}
	for in, want := range cases {
		if got := EscapeObjectPath(in); got != want {
			t.Errorf("EscapeObjectPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnitStatusHelpers(t *testing.T) {
	s := UnitStatus{Name: "fstrim.timer", LoadState: "loaded", ActiveState: "active", SubState: "waiting", UnitFileState: "enabled"}
	if s.Type() != "timer" || !s.IsActive() || !s.Exists() || !s.HasInstall() || s.IsMasked() {
		t.Errorf("unexpected helper results for %+v", s)
	}
	nf := UnitStatus{Name: "x.service", LoadState: "not-found"}
	if nf.Exists() {
		t.Error("not-found unit should not exist")
	}
	m := UnitStatus{Name: "x.service", LoadState: "masked", UnitFileState: "masked"}
	if !m.IsMasked() || m.HasInstall() {
		t.Error("masked unit misreported")
	}
	o := UnitStatus{Name: "job.service", ServiceType: "oneshot"}
	if !o.IsOneshot() {
		t.Error("oneshot without RemainAfterExit should be oneshot")
	}
	o.RemainAfterExit = true
	if o.IsOneshot() {
		t.Error("oneshot with RemainAfterExit should not count as oneshot")
	}
}

func TestUsecToTime(t *testing.T) {
	if !usecToTime(0).IsZero() || !usecToTime(^uint64(0)).IsZero() {
		t.Error("zero and infinity must map to the zero time")
	}
	want := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if got := usecToTime(uint64(want.UnixMicro())); !got.Equal(want) {
		t.Errorf("usecToTime = %v, want %v", got, want)
	}
}

func TestParseUnitFile(t *testing.T) {
	src := `# comment
[Unit]
Description=Discard unused filesystem blocks once a week
Documentation=man:fstrim

[Timer]
OnCalendar=weekly
OnCalendar=
OnCalendar=Sat *-*-* 03:00:00
AccuracySec=1h
Persistent=true
Unit=fstrim.service

[Install]
WantedBy=timers.target
`
	info := ParseUnitFile(strings.NewReader(src))
	if info.Description != "Discard unused filesystem blocks once a week" {
		t.Errorf("description = %q", info.Description)
	}
	if !info.HasInstall {
		t.Error("expected HasInstall")
	}
	if len(info.OnCalendar) != 1 || info.OnCalendar[0] != "Sat *-*-* 03:00:00" {
		t.Errorf("OnCalendar = %v (an empty assignment resets the list)", info.OnCalendar)
	}
	if info.Triggers != "fstrim.service" {
		t.Errorf("Triggers = %q", info.Triggers)
	}

	svc := ParseUnitFile(strings.NewReader("[Service]\nType=oneshot\nExecStart=/bin/true \\\n  --flag\n"))
	if svc.ServiceType != "oneshot" || svc.HasInstall {
		t.Errorf("service parse: %+v", svc)
	}
	sock := ParseUnitFile(strings.NewReader("[Socket]\nListenStream=631\nService=cups.service\n"))
	if sock.Triggers != "cups.service" {
		t.Errorf("socket Triggers = %q", sock.Triggers)
	}
	if got := ScanUnitFile("/nonexistent/unit.service"); got.Description != "" || got.HasInstall {
		t.Error("missing file should yield an empty info")
	}
}

func TestAnalyzer(t *testing.T) {
	var gotArgs []string
	a := &Analyzer{Binary: "systemd-analyze", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		if strings.Contains(args[len(args)-1], "Funday") {
			return []byte("Failed to parse calendar specification 'Funday': Invalid argument\n"), errors.New("exit status 1")
		}
		return []byte("Normalized form: Sat *-*-* 03:00:00\n"), nil
	}}
	if err := a.ValidateCalendar(context.Background(), "Sat 03:00"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if strings.Join(gotArgs, " ") != "systemd-analyze calendar -- Sat 03:00" {
		t.Errorf("argv = %q", gotArgs)
	}
	err := a.ValidateCalendar(context.Background(), "Funday 03:00")
	if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
		t.Errorf("expected parser message, got %v", err)
	}
	if err := a.VerifyUnit(context.Background(), "fstrim.timer"); err != nil {
		t.Errorf("verify: %v", err)
	}
	if gotArgs[1] != "verify" || gotArgs[2] != "--" {
		t.Errorf("verify argv = %q", gotArgs)
	}
}

func TestFakeManager(t *testing.T) {
	f := NewFake()
	f.AddUnit(UnitStatus{Name: "bluetooth.service", UnitFileState: "enabled", ActiveState: "active"})
	f.AddUnit(UnitStatus{Name: "fstrim.timer", UnitFileState: "disabled"})
	ctx := context.Background()

	st, _ := f.Status(ctx, "missing.service")
	if st.Exists() {
		t.Error("unknown unit should be not-found")
	}
	if r, err := f.Stop(ctx, "bluetooth.service"); err != nil || r != JobDone {
		t.Errorf("Stop = %v, %v", r, err)
	}
	if f.Unit("bluetooth.service").IsActive() {
		t.Error("unit should be inactive after Stop")
	}
	if err := f.Mask(ctx, []string{"bluetooth.service"}); err != nil {
		t.Errorf("Mask: %v", err)
	}
	if f.Unit("bluetooth.service").LoadState != "loaded" {
		t.Error("mask must take effect on the load state only after Reload")
	}
	_ = f.Reload(ctx)
	if f.Unit("bluetooth.service").LoadState != "masked" || f.Reloads != 1 {
		t.Error("Reload did not apply the mask")
	}
	if err := f.Unmask(ctx, []string{"bluetooth.service"}); err != nil {
		t.Errorf("Unmask: %v", err)
	}
	if f.Unit("bluetooth.service").UnitFileState != "enabled" {
		t.Errorf("unmask should restore the previous state, got %q", f.Unit("bluetooth.service").UnitFileState)
	}
	if err := f.Enable(ctx, []string{"fstrim.timer"}); err != nil {
		t.Errorf("Enable: %v", err)
	}
	if r, _ := f.Start(ctx, "fstrim.timer"); r != JobDone || f.Unit("fstrim.timer").SubState != "waiting" {
		t.Error("timer should be waiting after Start")
	}
	f.AddUnit(UnitStatus{Name: "static.service", UnitFileState: "static"})
	if err := f.Enable(ctx, []string{"static.service"}); !errors.Is(err, ErrNoInstall) {
		t.Errorf("Enable static = %v, want ErrNoInstall", err)
	}
	f.JobResults["Start fstrim.timer"] = JobFailed
	if r, _ := f.Start(ctx, "fstrim.timer"); r != JobFailed {
		t.Error("JobResults override not honoured")
	}
	files, _ := f.ListUnitFiles(ctx, []string{"*.timer"})
	if len(files) != 1 || !strings.HasSuffix(files[0].Path, "fstrim.timer") {
		t.Errorf("ListUnitFiles = %+v", files)
	}
	if !f.HasCall("Mask bluetooth.service") || f.HasCall("Disable bluetooth.service") {
		t.Errorf("calls = %v", f.Calls)
	}
}
