// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/agent/internal/logind"
	"github.com/VuteTech/Bor/agent/internal/systemd"
	"github.com/VuteTech/Bor/agent/internal/updates"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func bptr(b bool) *bool { return &b }

func updEntry(id string, prio int32, p *pb.UpdatesPolicy) UpdatesEntry {
	return UpdatesEntry{ID: id, Name: "policy " + id, Priority: prio, Policy: p}
}

func TestMergeUpdatesPolicies(t *testing.T) {
	a := updEntry("a", 10, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_SECURITY, Hold: []string{"kernel*"},
		Reboot: &pb.UpdatesReboot{Policy: pb.UpdatesRebootPolicy_UPDATES_REBOOT_NEVER}, Schedule: &pb.UpdatesSchedule{OnCalendar: "*-*-* 02:00"}})
	b := updEntry("b", 10, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_ALL, Hold: []string{"firefox"},
		Reboot: &pb.UpdatesReboot{Policy: pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED, Window: "02:00-04:00"}, Schedule: &pb.UpdatesSchedule{OnCalendar: "*-*-* 03:00"}, RequireEngine: true})
	c := updEntry("c", 5, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_OFF, InstallEngine: bptr(false)})
	d := MergeUpdatesPolicies([]UpdatesEntry{a, b, c})
	if d.Mode != pb.UpdatesMode_UPDATES_MODE_ALL || d.ModeBy.ID != "b" {
		t.Errorf("equal priority: the mode that installs more must win, got %v by %s", d.Mode, d.ModeBy.ID)
	}
	if d.Reboot.GetPolicy() != pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED || d.RebootBy.ID != "b" || d.Reboot.GetWindow() != "02:00-04:00" {
		t.Errorf("the reboot rule that reboots more must win as a block, got %+v by %s", d.Reboot, d.RebootBy.ID)
	}
	if len(d.Holds) != 2 || d.Holds["kernel*"].ID != "a" || d.Holds["firefox"].ID != "b" {
		t.Errorf("holds must be a union: %+v", d.Holds)
	}
	if d.ScheduleConflict == nil || d.ScheduleBy.ID != "a" || d.ScheduleConflict.ID != "b" {
		t.Errorf("two schedules at equal priority are a conflict: by=%s conflict=%+v", d.ScheduleBy.ID, d.ScheduleConflict)
	}
	if d.InstallEngine {
		t.Error("a policy switching the engine install off must win")
	}
	if !d.RequireEngine {
		t.Error("require_engine is any")
	}
	high := updEntry("h", 20, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_CHECK})
	d2 := MergeUpdatesPolicies([]UpdatesEntry{a, b, high})
	if d2.Mode != pb.UpdatesMode_UPDATES_MODE_CHECK || d2.ModeBy.ID != "h" {
		t.Error("higher priority must win over the install-more rule")
	}
}

// fakeRunner answers the commands the apt driver and the kernel check run.
func fakeRunner(t *testing.T, calls *[]string) updates.Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		*calls = append(*calls, line)
		switch {
		case strings.HasSuffix(name, "apt") && len(args) > 0 && args[0] == "list":
			return []byte("Listing...\nbind9-host/resolute-security 1:9.20.29-1 amd64 [upgradable from: 1:9.20.26-1]\nvim/resolute-updates 2:9.1 amd64 [upgradable from: 2:9.0]\n"), nil
		case strings.HasSuffix(name, "dpkg-query"):
			return []byte("linux-image-7.0.0-38-generic\tinstall ok installed\nlinux-image-7.0.0-34-generic\tinstall ok installed\n"), nil
		case strings.HasSuffix(name, "needrestart"):
			return []byte("NEEDRESTART-VER: 3.11\nNEEDRESTART-KCUR: 7.0.0-34-generic\nNEEDRESTART-KEXP: 7.0.0-38-generic\nNEEDRESTART-KSTA: 3\n"), nil
		case strings.HasSuffix(name, "fwupdmgr"):
			return []byte(`{"Devices":[{"Name":"System Firmware","Releases":[{"Version":"1.12","Urgency":"high","Summary":"Fixes a boot issue"}]}]}`), nil
		}
		t.Logf("unexpected command %s", line)
		return nil, nil
	}
}

func aptPlatform(root string) *updates.Platform {
	return &updates.Platform{Engine: updates.EngineApt, OSID: "ubuntu", Codename: "resolute", Installed: true, Package: "unattended-upgrades",
		ConfigPath: "/etc/apt/apt.conf.d/90bor-updates", TimerUnit: "apt-daily-upgrade.timer", ServiceUnit: "apt-daily-upgrade.service",
		AptGetBin: "/usr/bin/apt-get", DpkgQueryBin: "/usr/bin/dpkg-query", NeedrestartBin: "/usr/sbin/needrestart", Root: root}
}

func TestSyncUpdatesApt(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"etc/apt/apt.conf.d", "proc/sys/kernel", "run", "boot"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "osrelease"), []byte("7.0.0-34-generic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"vmlinuz-7.0.0-34-generic", "vmlinuz-7.0.0-38-generic"} {
		if err := os.WriteFile(filepath.Join(root, "boot", k), []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fake := systemd.NewFake()
	fake.AddUnit(systemd.UnitStatus{Name: "apt-daily-upgrade.timer", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"})
	fake.AddUnit(systemd.UnitStatus{Name: "apt-daily.timer", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"})
	var calls []string
	deps := &UpdatesDeps{Platform: aptPlatform(root), Run: fakeRunner(t, &calls), Systemd: fake, Now: func() time.Time { return time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local) }}
	systemDir := filepath.Join(root, "etc", "systemd", "system")
	opts := UpdatesSyncOptions{StatePath: filepath.Join(root, "state.json"), BackupDir: filepath.Join(root, "backups"), SystemDir: systemDir, AllowInstall: true, AllowReboot: true}

	pol := &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_SECURITY, Hold: []string{"vim"},
		Schedule: &pb.UpdatesSchedule{OnCalendar: "*-*-* 02:00", RandomizedDelay: "30m"},
		Reboot:   &pb.UpdatesReboot{Policy: pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED, Window: "01:00-05:00"}}
	entries := []UpdatesEntry{updEntry("p", 10, pol)}
	res := SyncUpdates(context.Background(), deps, entries, opts)
	r := res.Policies["p"]
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("expected compliant, got %s: %s (%v)", r.Status, r.Message, r.Items)
	}
	conf, err := os.ReadFile(filepath.Join(root, "etc", "apt", "apt.conf.d", "90bor-updates"))
	if err != nil || !strings.Contains(string(conf), `"^vim$";`) || !strings.Contains(string(conf), "resolute") && !strings.Contains(string(conf), "${distro_codename}-security") {
		t.Errorf("apt configuration not written as expected: %v\n%s", err, conf)
	}
	dropIn, err := os.ReadFile(updates.TimerDropInPath(systemDir, "apt-daily-upgrade.timer"))
	if err != nil || !strings.Contains(string(dropIn), "OnCalendar=*-*-* 02:00") {
		t.Errorf("timer drop-in missing: %v", err)
	}
	if !res.Changed {
		t.Error("first sync must report a change")
	}
	keys := map[string]string{}
	for _, it := range r.Items {
		keys[it.SchemaId+"/"+it.Key] = it.Status.String()
	}
	for _, k := range []string{"updates:engine/unattended-upgrades", "updates:mode/mode", "updates:schedule/schedule", "updates:reboot/reboot", "updates:hold/vim"} {
		if keys[k] != "COMPLIANCE_STATUS_COMPLIANT" {
			t.Errorf("item %s = %s, want compliant (%v)", k, keys[k], r.Items)
		}
	}

	// Second sync changes nothing.
	res2 := SyncUpdates(context.Background(), deps, entries, opts)
	if res2.Changed {
		t.Error("an unchanged policy must not report a change")
	}

	// Someone disables the timer: the reconcile pass turns it on and reports drift.
	_ = fake.Disable(context.Background(), []string{"apt-daily-upgrade.timer"})
	opts.Reconcile = true
	res3 := SyncUpdates(context.Background(), deps, entries, opts)
	if len(res3.Drifts) != 1 || !strings.Contains(res3.Drifts[0].Resource, "apt-daily-upgrade.timer") {
		t.Errorf("drift not reported: %+v", res3.Drifts)
	}
	if st, _ := fake.Status(context.Background(), "apt-daily-upgrade.timer"); st.UnitFileState != "enabled" {
		t.Error("timer not re-enabled")
	}
	opts.Reconcile = false

	// Mode OFF disables the timer and keeps the configuration with the
	// upgrade switched off.
	off := []UpdatesEntry{updEntry("p", 10, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_OFF})}
	SyncUpdates(context.Background(), deps, off, opts)
	if st, _ := fake.Status(context.Background(), "apt-daily-upgrade.timer"); st.UnitFileState != "disabled" {
		t.Error("OFF must disable the upgrade timer")
	}
	if _, err := os.Stat(updates.TimerDropInPath(systemDir, "apt-daily-upgrade.timer")); err == nil {
		t.Error("OFF must remove the schedule drop-in")
	}

	// Unbinding restores the timer and removes the file.
	SyncUpdates(context.Background(), deps, nil, opts)
	if st, _ := fake.Status(context.Background(), "apt-daily-upgrade.timer"); st.UnitFileState != "enabled" {
		t.Error("restore must re-enable the timer as it was")
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "apt", "apt.conf.d", "90bor-updates")); err == nil {
		t.Error("restore must remove Bor's apt file")
	}
	if _, err := os.Stat(opts.StatePath); err == nil {
		t.Error("state file must be removed when nothing is recorded")
	}
}

func TestSyncUpdatesNoEngineAndRequire(t *testing.T) {
	root := t.TempDir()
	deps := &UpdatesDeps{Platform: &updates.Platform{Engine: updates.EngineNone, Root: root}}
	opts := UpdatesSyncOptions{StatePath: filepath.Join(root, "state.json"), BackupDir: root}
	res := SyncUpdates(context.Background(), deps, []UpdatesEntry{updEntry("a", 1, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_ALL})}, opts)
	if res.Policies["a"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("no engine: expected inapplicable, got %s", res.Policies["a"].Status)
	}
	res = SyncUpdates(context.Background(), deps, []UpdatesEntry{updEntry("a", 1, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_ALL, RequireEngine: true})}, opts)
	if res.Policies["a"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("required engine: expected non-compliant, got %s", res.Policies["a"].Status)
	}
}

type fakeLogind struct{ sessions []logind.Session }

func (f *fakeLogind) ListSessions() ([]logind.Session, error) { return f.sessions, nil }
func (f *fakeLogind) Lock(string) error                       { return nil }
func (f *fakeLogind) Terminate(string) error                  { return nil }

func TestEvaluateReboot(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"proc/sys/kernel", "run", "boot"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "osrelease"), []byte("7.0.0-34-generic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"vmlinuz-7.0.0-34-generic", "vmlinuz-7.0.0-38-generic"} {
		if err := os.WriteFile(filepath.Join(root, "boot", k), []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var calls []string
	now := time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local)
	rebooted, warned := 0, 0
	p := aptPlatform(root)
	p.NeedrestartBin = ""
	deps := &UpdatesDeps{Platform: p, Run: fakeRunner(t, &calls), Now: func() time.Time { return now },
		Reboot: func(context.Context) error { rebooted++; return nil },
		Notify: func(updates.UserSession, string, string) error { warned++; return nil },
		Logind: &fakeLogind{sessions: []logind.Session{{ID: "2", Name: "blago", UID: 1000, Seat: "seat0", Type: "wayland", Class: "user", Active: true}}}}
	opts := UpdatesSyncOptions{AllowReboot: true}
	state := newUpdatesState()
	rule := &UpdatesDesired{Reboot: &pb.UpdatesReboot{Policy: pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED, Window: "02:00-05:00", ForceAfterDays: 2, WarnMinutes: 10}}

	dec := EvaluateReboot(context.Background(), deps, rule, state, opts)
	if !dec.Required || dec.Rebooted || !strings.Contains(dec.Blocked, "session") || state.RebootPendingSince == nil {
		t.Fatalf("with a user logged in the reboot must wait: %+v", dec)
	}
	// Two days later the reboot is forced: first the warning, then the reboot.
	now = now.Add(49 * time.Hour)
	dec = EvaluateReboot(context.Background(), deps, rule, state, opts)
	if !dec.Warned || warned != 1 || rebooted != 0 {
		t.Fatalf("expected a warning first: %+v warned=%d rebooted=%d", dec, warned, rebooted)
	}
	now = now.Add(5 * time.Minute)
	dec = EvaluateReboot(context.Background(), deps, rule, state, opts)
	if dec.Rebooted || rebooted != 0 {
		t.Fatalf("must wait for the warning period: %+v", dec)
	}
	now = now.Add(6 * time.Minute)
	dec = EvaluateReboot(context.Background(), deps, rule, state, opts)
	if !dec.Rebooted || rebooted != 1 || state.LastPolicyReboot == nil || !strings.Contains(state.LastPolicyRebootReason, "forced") {
		t.Fatalf("expected the forced reboot: %+v", dec)
	}
	// Outside the window nothing happens even without sessions.
	deps.Logind = &fakeLogind{}
	state = newUpdatesState()
	now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	dec = EvaluateReboot(context.Background(), deps, rule, state, opts)
	if dec.Rebooted || !strings.Contains(dec.Blocked, "window") {
		t.Errorf("outside the window: %+v", dec)
	}
	// Inside the window and nobody logged in: reboot at once, no warning.
	now = time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local)
	dec = EvaluateReboot(context.Background(), deps, rule, state, opts)
	if !dec.Rebooted || rebooted != 2 || warned != 1 {
		t.Errorf("empty node must reboot without warning: %+v", dec)
	}
	// NEVER reports but does nothing.
	never := &UpdatesDesired{Reboot: &pb.UpdatesReboot{Policy: pb.UpdatesRebootPolicy_UPDATES_REBOOT_NEVER}}
	state = newUpdatesState()
	dec = EvaluateReboot(context.Background(), deps, never, state, opts)
	if dec.Rebooted || !dec.Required || state.RebootPendingSince == nil {
		t.Errorf("NEVER must only track the pending reboot: %+v", dec)
	}
}

func TestEffectiveModeAndInventory(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"etc/apt/apt.conf.d", "proc/sys/kernel", "boot", "var/log/unattended-upgrades"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "osrelease"), []byte("7.0.0-38-generic\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "proc", "stat"), []byte("cpu 1 2 3\nbtime 1792400000\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "boot", "vmlinuz-7.0.0-38-generic"), []byte("k"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "etc", "apt", "apt.conf.d", "90bor-updates"), updates.RenderAptConf(updates.ModeSecurity, "ubuntu", nil, false), 0o600)
	_ = os.WriteFile(filepath.Join(root, "var", "log", "unattended-upgrades", "unattended-upgrades.log"), []byte("2026-10-20 12:52:47,100 INFO Starting unattended upgrades script\n2026-10-20 12:55:09,400 INFO All upgrades installed\n"), 0o600)
	fake := systemd.NewFake()
	fake.AddUnit(systemd.UnitStatus{Name: "apt-daily-upgrade.timer", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled", LastTrigger: time.Date(2026, 10, 20, 6, 31, 0, 0, time.Local)})
	var calls []string
	p := aptPlatform(root)
	p.NeedrestartBin = ""
	deps := &UpdatesDeps{Platform: p, Run: fakeRunner(t, &calls), Systemd: fake}
	state := newUpdatesState()
	inv := ScanUpdatesInventory(context.Background(), deps, state, filepath.Join(root, "etc", "systemd", "system"))
	if inv.Engine != "unattended-upgrades" || !inv.EngineInstalled || inv.EngineManagedBy != "bor" || inv.EffectiveMode != "security" || !inv.EngineTimerEnabled {
		t.Errorf("inventory engine fields wrong: %+v", inv)
	}
	if inv.PendingTotal != 2 || inv.PendingSecurity != 1 || len(inv.Pending) != 2 || inv.Pending[0].Name != "bind9-host" || !inv.Pending[0].Security {
		t.Errorf("pending wrong: %+v", inv.Pending)
	}
	if inv.LastRun == nil || !inv.LastRun.Success || inv.LastCheck == nil {
		t.Errorf("last run/check missing: %+v %+v", inv.LastRun, inv.LastCheck)
	}
	if inv.Reboot == nil || inv.Reboot.Required {
		t.Errorf("no reboot expected with the newest kernel running: %+v", inv.Reboot)
	}
	if inv.RunningKernel != "7.0.0-38-generic" || inv.NewestKernel != "7.0.0-38-generic" || inv.BootTime == nil {
		t.Errorf("kernel/boot fields wrong: %+v", inv)
	}
}

func TestTrackPendingSecurityAndAgeItems(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	st := newUpdatesState()
	if oldest := st.TrackPendingSecurity([]string{"openssl", "curl"}, now.Add(-20*24*time.Hour)); !oldest.Equal(now.Add(-20 * 24 * time.Hour)) {
		t.Errorf("first seen must be the oldest, got %v", oldest)
	}
	// curl was installed, vim is new.
	oldest := st.TrackPendingSecurity([]string{"openssl", "vim"}, now)
	if _, ok := st.PendingSince["curl"]; ok || len(st.PendingSince) != 2 || !oldest.Equal(now.Add(-20*24*time.Hour)) {
		t.Errorf("tracking wrong: %+v", st.PendingSince)
	}
	st.LastScan = &now
	lastCheck := now.Add(-5 * 24 * time.Hour)
	st.LastCheck = &lastCheck
	since := now.Add(-3 * 24 * time.Hour)
	st.RebootPendingSince = &since
	st.RebootReasons = []string{"kernel 7.0.0-38 installed, 7.0.0-34 running"}

	items := UpdatesAgeItems(&pb.UpdatesLimits{SecurityMaxAgeDays: 14, RebootMaxAgeDays: 7, CheckMaxAgeDays: 3}, st, now)
	got := map[string]*pb.ComplianceItemResult{}
	for _, it := range items {
		got[it.SchemaId] = it
	}
	if it := got["updates:security-age"]; it == nil || it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(it.Message, "openssl") || !strings.Contains(it.Message, "oldest 20 days") {
		t.Errorf("security age item wrong: %+v", it)
	}
	if it := got["updates:reboot-age"]; it == nil || it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT || !strings.Contains(it.Message, "3 days") {
		t.Errorf("reboot age item wrong: %+v", it)
	}
	if it := got["updates:check-age"]; it == nil || it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(it.Message, "5 days") {
		t.Errorf("check age item wrong: %+v", it)
	}
	// Unchecked limits produce no items; a fresh state is inapplicable.
	if items := UpdatesAgeItems(&pb.UpdatesLimits{}, st, now); len(items) != 0 {
		t.Errorf("zero limits must produce no items, got %v", items)
	}
	fresh := UpdatesAgeItems(&pb.UpdatesLimits{SecurityMaxAgeDays: 1, CheckMaxAgeDays: 1, RebootMaxAgeDays: 1}, newUpdatesState(), now)
	for _, it := range fresh {
		want := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
		if it.SchemaId == "updates:reboot-age" {
			want = pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
		}
		if it.Status != want {
			t.Errorf("fresh state: %s = %s, want %s", it.SchemaId, it.Status, want)
		}
	}
	// Saving keeps the scan facts; the file is not empty.
	if st.Empty() {
		t.Error("a state with scan facts is not empty")
	}
}

func TestSyncUpdatesLimitsAndFirmware(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"etc/apt/apt.conf.d", "proc/sys/kernel", "boot"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "osrelease"), []byte("7.0.0-38-generic\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "boot", "vmlinuz-7.0.0-38-generic"), []byte("k"), 0o600)
	fake := systemd.NewFake()
	fake.AddUnit(systemd.UnitStatus{Name: "apt-daily-upgrade.timer", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"})
	fake.AddUnit(systemd.UnitStatus{Name: "fwupd-refresh.timer", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"})
	var calls []string
	p := aptPlatform(root)
	p.NeedrestartBin = ""
	p.FwupdmgrBin = "/usr/bin/fwupdmgr"
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	deps := &UpdatesDeps{Platform: p, Run: fakeRunner(t, &calls), Systemd: fake, Now: func() time.Time { return now }}
	statePath := filepath.Join(root, "state.json")
	opts := UpdatesSyncOptions{StatePath: statePath, BackupDir: filepath.Join(root, "backups"), SystemDir: filepath.Join(root, "etc", "systemd", "system"), AllowInstall: true, AllowReboot: true}
	pol := &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_SECURITY, Limits: &pb.UpdatesLimits{SecurityMaxAgeDays: 14, CheckMaxAgeDays: 3},
		Firmware: &pb.UpdatesFirmware{RefreshMetadata: bptr(true), ReportPending: true}}
	entries := []UpdatesEntry{updEntry("p", 10, pol)}

	// Before any scan the age and firmware items are inapplicable.
	res := SyncUpdates(context.Background(), deps, entries, opts)
	r := res.Policies["p"]
	status := map[string]string{}
	for _, it := range r.Items {
		status[it.SchemaId+"/"+it.Key] = it.Status.String()
	}
	if status["updates:security-age/security"] != "COMPLIANCE_STATUS_INAPPLICABLE" || status["updates:firmware/pending"] != "COMPLIANCE_STATUS_INAPPLICABLE" {
		t.Errorf("before a scan the scan items must be inapplicable: %v", status)
	}
	if status["updates:firmware/refresh"] != "COMPLIANCE_STATUS_COMPLIANT" || !fake.HasCall("Enable fwupd-refresh.timer") {
		t.Errorf("fwupd-refresh.timer must be enabled: %v (%v)", status, calls)
	}
	// A scan records the facts; the next sync evaluates them.
	state, _ := LoadUpdatesState(statePath)
	if state.Timers["fwupd-refresh.timer"] != false {
		t.Errorf("the timer's prior state must be recorded for the restore: %+v", state.Timers)
	}
	inv := ScanUpdatesInventory(context.Background(), deps, state, opts.SystemDir)
	if inv.PendingFirmware != 1 || len(inv.Pending) != 3 || inv.Pending[2].Kind != "firmware" || inv.OldestSecuritySince == nil {
		t.Errorf("inventory firmware/oldest fields wrong: %+v", inv)
	}
	if err := SaveUpdatesState(statePath, state); err != nil {
		t.Fatal(err)
	}
	res = SyncUpdates(context.Background(), deps, entries, opts)
	r = res.Policies["p"]
	status = map[string]string{}
	msg := map[string]string{}
	for _, it := range r.Items {
		status[it.SchemaId+"/"+it.Key] = it.Status.String()
		msg[it.SchemaId+"/"+it.Key] = it.Message
	}
	if status["updates:security-age/security"] != "COMPLIANCE_STATUS_COMPLIANT" || !strings.Contains(msg["updates:security-age/security"], "1 security update(s) pending, oldest 0 days") {
		t.Errorf("security age after scan wrong: %v %v", status, msg)
	}
	if status["updates:check-age/check"] != "COMPLIANCE_STATUS_NON_COMPLIANT" {
		t.Errorf("no last check means the check-age item fails: %v %v", status, msg)
	}
	if status["updates:firmware/pending"] != "COMPLIANCE_STATUS_NON_COMPLIANT" || !strings.Contains(msg["updates:firmware/pending"], "System Firmware") {
		t.Errorf("pending firmware must be reported: %v %v", status, msg)
	}
	// Unbinding restores the fwupd timer.
	SyncUpdates(context.Background(), deps, nil, opts)
	if !fake.HasCall("Disable fwupd-refresh.timer") {
		t.Errorf("restore must disable fwupd-refresh.timer again: %v", calls)
	}
}

func TestSyncUpdatesOffline(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"etc/dnf", "proc/sys/kernel", "boot", "usr/lib/sysimage/libdnf5/offline"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "osrelease"), []byte("7.2.11-100.fc43.x86_64\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "boot", "vmlinuz-7.2.11-100.fc43.x86_64"), []byte("k"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "usr", "lib", "sysimage", "libdnf5", "offline", "transaction.json"), []byte(`{"rpms":[{"name":"gnupg2"},{"name":"gnupg2-smime"}]}`), 0o600)
	staged := false
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		calls = append(calls, name+" "+line)
		switch {
		case line == "-q offline status":
			if staged {
				return []byte("An offline transaction was initiated by the following command:\n\tdnf5 upgrade --offline --security -y\n"), nil
			}
			return []byte("No offline transaction is stored.\n"), nil
		case strings.HasPrefix(line, "upgrade --offline"):
			staged = true
			return []byte("Transaction stored to be performed offline.\n"), nil
		case line == "-y offline clean":
			staged = false
			return nil, nil
		case strings.Contains(line, "check-upgrade"):
			return []byte("gnupg2.x86_64 2.4.9-5.fc43 updates\n"), &fakeExitErr{code: 100}
		case strings.Contains(line, "advisory list"):
			return []byte("[]"), nil
		}
		return nil, nil
	}
	fake := systemd.NewFake()
	fake.AddUnit(systemd.UnitStatus{Name: "dnf5-automatic.timer", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"})
	p := &updates.Platform{Engine: updates.EngineDnf, DNF5: true, Installed: true, Package: "dnf5-plugin-automatic", DnfBin: "/usr/bin/dnf5",
		ConfigPath: "/etc/dnf/automatic.conf", TimerUnit: "dnf5-automatic.timer", ServiceUnit: "dnf5-automatic.service", Root: root}
	deps := &UpdatesDeps{Platform: p, Run: run, Systemd: fake}
	opts := UpdatesSyncOptions{StatePath: filepath.Join(root, "state.json"), BackupDir: filepath.Join(root, "backups"), SystemDir: filepath.Join(root, "etc", "systemd", "system"), AllowInstall: true, AllowReboot: true}
	pol := &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_SECURITY, Offline: true}
	res := SyncUpdates(context.Background(), deps, []UpdatesEntry{updEntry("p", 10, pol)}, opts)
	r := res.Policies["p"]
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("expected compliant, got %s: %s (%v)", r.Status, r.Message, r.Items)
	}
	conf, _ := os.ReadFile(filepath.Join(root, "etc", "dnf", "automatic.conf"))
	if !strings.Contains(string(conf), "apply_updates = no") || !strings.Contains(string(conf), updates.DnfOfflineMarker) {
		t.Errorf("offline conf must download only and carry the marker:\n%s", conf)
	}
	var offlineItem *pb.ComplianceItemResult
	for _, it := range r.Items {
		if it.SchemaId == "updates:offline" {
			offlineItem = it
		}
	}
	if offlineItem == nil || offlineItem.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT || !strings.Contains(offlineItem.Message, "2 package(s) staged") {
		t.Errorf("offline item wrong: %+v", offlineItem)
	}
	if !staged {
		t.Error("the transaction must be stored")
	}
	state, _ := LoadUpdatesState(opts.StatePath)
	if !state.OfflineStaged {
		t.Error("state must record the stored transaction")
	}
	// The effective mode still reads as the policy's mode.
	if m := effectiveMode(p, conf, true, true); m != updates.ModeSecurity {
		t.Errorf("effective mode with the offline marker = %s, want security", m)
	}
	// The reboot signals see the staged transaction.
	if required, reasons, isStaged := RebootSignals(context.Background(), deps, true); !required || !isStaged || len(reasons) == 0 || !strings.Contains(reasons[0], "offline update transaction staged") {
		t.Errorf("reboot signals must report the staged transaction: %v %v %v", required, reasons, isStaged)
	}
	// Unbinding removes the transaction Bor stored.
	SyncUpdates(context.Background(), deps, nil, opts)
	if staged {
		t.Errorf("restore must clean the stored transaction: %v", calls)
	}
}

type fakeExitErr struct{ code int }

func (f *fakeExitErr) Error() string { return "exit status " + strconv.Itoa(f.code) }
func (f *fakeExitErr) ExitCode() int { return f.code }

func TestSyncUpdatesEngineChangeRestoresOldConfig(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"etc", "proc/sys/kernel", "boot"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "osrelease"), []byte("6.17.1-default\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "boot", "vmlinuz-6.17.1-default"), []byte("k"), 0o600)
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	fake := systemd.NewFake()
	fake.AddUnit(systemd.UnitStatus{Name: "transactional-update.timer", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"})
	fake.AddUnit(systemd.UnitStatus{Name: "os-update.timer", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"})
	tu := &updates.Platform{Engine: updates.EngineTransactional, OSID: "opensuse-microos", Installed: true, Package: "transactional-update",
		ConfigPath: "/etc/transactional-update.conf", TimerUnit: "transactional-update.timer", ServiceUnit: "transactional-update.service",
		ZypperBin: "/usr/bin/zypper", EngineRunBin: "/usr/sbin/transactional-update", Root: root}
	osu := &updates.Platform{Engine: updates.EngineOsUpdate, OSID: "opensuse-tumbleweed", Installed: true, Package: "os-update",
		ConfigPath: "/etc/os-update.conf", TimerUnit: "os-update.timer", ServiceUnit: "os-update.service", ZypperBin: "/usr/bin/zypper", Root: root}
	opts := UpdatesSyncOptions{StatePath: filepath.Join(root, "state.json"), BackupDir: filepath.Join(root, "backups"), SystemDir: filepath.Join(root, "etc", "systemd", "system"), AllowInstall: true, AllowReboot: true}
	entries := []UpdatesEntry{updEntry("p", 10, &pb.UpdatesPolicy{Mode: pb.UpdatesMode_UPDATES_MODE_SECURITY, Hold: []string{"vim"}})}

	res := SyncUpdates(context.Background(), &UpdatesDeps{Platform: tu, Run: run, Systemd: fake}, entries, opts)
	if r := res.Policies["p"]; r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("transactional sync: %s %s %v", r.Status, r.Message, r.Items)
	}
	tuConf, err := os.ReadFile(filepath.Join(root, "etc", "transactional-update.conf"))
	if err != nil || !strings.Contains(string(tuConf), "UPDATE_METHOD=patch") {
		t.Fatalf("transactional conf not written: %v %s", err, tuConf)
	}
	state, _ := LoadUpdatesState(opts.StatePath)
	if state.ConfigPath != "/etc/transactional-update.conf" {
		t.Errorf("state must record the configuration path, got %q", state.ConfigPath)
	}

	// The engine changes under the agent: the old engine's file goes, the
	// new engine's file comes.
	res = SyncUpdates(context.Background(), &UpdatesDeps{Platform: osu, Run: run, Systemd: fake}, entries, opts)
	if r := res.Policies["p"]; r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("os-update sync: %s %s %v", r.Status, r.Message, r.Items)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "transactional-update.conf")); !os.IsNotExist(err) {
		t.Errorf("the transactional-update conf must be removed on the engine change (err=%v)", err)
	}
	if osConf, err := os.ReadFile(filepath.Join(root, "etc", "os-update.conf")); err != nil || !strings.Contains(string(osConf), `UPDATE_CMD="security"`) {
		t.Errorf("os-update conf not written: %v %s", err, osConf)
	}
	state, _ = LoadUpdatesState(opts.StatePath)
	if state.Engine != "os-update" || state.ConfigPath != "/etc/os-update.conf" {
		t.Errorf("state after the engine change wrong: %+v", state)
	}
}
