// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

const (
	trTimer = pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER
	trBoot  = pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_BOOT
)

func cleanupTask() *pb.SystemdTask {
	return &pb.SystemdTask{
		Name:        "nightly-cleanup",
		Description: "Nightly  cache\ncleanup",
		Trigger:     trTimer,
		Schedule:    &pb.SystemdTimerSettings{OnCalendar: []string{"*-*-* 02:00:00"}, Persistent: boolPtr(true), RandomizedDelaySec: "30min"},
		Script:      "#!/bin/sh\nrm -rf /var/cache/lab/*\n",
		Environment: []string{"LANG=C", "DEBUG=1"},
	}
}

func taskPolicy(id, name string, prio int32, tasks ...*pb.SystemdTask) SystemdEntry {
	return SystemdEntry{ID: id, Name: name, Priority: prio, Policy: &pb.SystemdPolicy{Tasks: tasks}}
}

func TestRenderSystemdTaskFiles(t *testing.T) {
	task := cleanupTask()
	svc := string(RenderSystemdTaskService(task, "/etc/bor/systemd/nightly-cleanup.sh"))
	want := "# Managed by Bor - changes are overwritten\n# Task: nightly-cleanup\n\n[Unit]\nDescription=Nightly cache cleanup\n\n[Service]\nType=oneshot\nExecStart=/etc/bor/systemd/nightly-cleanup.sh\nTimeoutStartSec=1h\nSyslogIdentifier=bor-task-nightly-cleanup\nEnvironment=\"DEBUG=1\"\nEnvironment=\"LANG=C\"\nNoNewPrivileges=yes\nPrivateTmp=yes\nProtectKernelTunables=yes\nProtectControlGroups=yes\nRestrictSUIDSGID=yes\nProtectClock=yes\nProtectHostname=yes\n"
	if svc != want {
		t.Errorf("timer task service:\n got: %q\nwant: %q", svc, want)
	}
	timer := string(RenderSystemdTaskTimer(task))
	want = "# Managed by Bor - changes are overwritten\n# Task: nightly-cleanup\n\n[Unit]\nDescription=Timer for Nightly cache cleanup\n\n[Timer]\nOnCalendar=*-*-* 02:00:00\nPersistent=yes\nRandomizedDelaySec=30min\n\n[Install]\nWantedBy=timers.target\n"
	if timer != want {
		t.Errorf("timer unit:\n got: %q\nwant: %q", timer, want)
	}

	boot := &pb.SystemdTask{
		Name: "fix-perms", Trigger: trBoot,
		ExecStart:        []string{"/usr/bin/chmod", "0755", "/srv/share dir", `say "hi"`, "100%", "$HOME"},
		User:             "nobody",
		Group:            "nogroup",
		WorkingDirectory: "/srv",
		TimeoutStartSec:  "10min",
		Hardened:         boolPtr(false),
	}
	svc = string(RenderSystemdTaskService(boot, ""))
	want = "# Managed by Bor - changes are overwritten\n# Task: fix-perms\n\n[Unit]\nDescription=Bor task fix-perms\n\n[Service]\nType=oneshot\nExecStart=/usr/bin/chmod 0755 \"/srv/share dir\" \"say \\\"hi\\\"\" \"100%%\" \"$$HOME\"\nTimeoutStartSec=10min\nSyslogIdentifier=bor-task-fix-perms\nUser=nobody\nGroup=nogroup\nWorkingDirectory=/srv\n\n[Install]\nWantedBy=multi-user.target\n"
	if svc != want {
		t.Errorf("boot task service:\n got: %q\nwant: %q", svc, want)
	}
	if RenderSystemdTaskTimer(boot) != nil {
		t.Error("a boot task has no timer")
	}
}

func TestRenderSystemdPreset(t *testing.T) {
	d := MergeSystemdPolicies([]SystemdEntry{{ID: "a", Name: "A", Priority: 1, Policy: &pb.SystemdPolicy{
		WritePresets: true,
		Units: []*pb.SystemdUnit{
			sdUnit("fstrim.timer", enEnabled, acRunning),
			sdUnit("avahi-daemon.service", enMasked, acKeep),
			sdUnit("bluetooth.service", enDisabled, acStopped),
			sdUnit("cups.service", enKeep, acStopped),
		},
	}}})
	want := "# Managed by Bor - changes are overwritten\ndisable avahi-daemon.service\ndisable bluetooth.service\nenable fstrim.timer\n"
	if got := string(RenderSystemdPreset(d)); got != want {
		t.Errorf("preset:\n got: %q\nwant: %q", got, want)
	}
	d.WritePresets = false
	if RenderSystemdPreset(d) != nil {
		t.Error("presets off must render nil")
	}
	none := MergeSystemdPolicies([]SystemdEntry{{ID: "a", Name: "A", Priority: 1, Policy: &pb.SystemdPolicy{
		WritePresets: true, Units: []*pb.SystemdUnit{sdUnit("cups.service", enKeep, acStopped)},
	}}})
	if RenderSystemdPreset(none) != nil {
		t.Error("no enablement must render nil")
	}
}

func TestRenderSystemdDropInRaw(t *testing.T) {
	u := &SystemdUnitDesired{
		Name:    "cups.service",
		Service: &pb.SystemdServiceSettings{Restart: "always"},
		Raw: []*pb.SystemdDirective{
			{Section: "Service", Key: "ReadWritePaths", Values: []string{"/var/spool/cups", "/var/log/cups"}},
			{Section: "Unit", Key: "StartLimitBurst", Values: []string{"3"}},
			{Section: "Service", Key: "IPAddressDeny", Values: []string{}},
		},
	}
	want := "# Managed by Bor - changes are overwritten\n# Unit: cups.service\n\n[Unit]\nStartLimitBurst=\nStartLimitBurst=3\n\n[Service]\nRestart=always\nIPAddressDeny=\nReadWritePaths=\nReadWritePaths=/var/spool/cups\nReadWritePaths=/var/log/cups\n"
	if got := string(RenderSystemdDropIn(u)); got != want {
		t.Errorf("raw drop-in:\n got: %q\nwant: %q", got, want)
	}
	rawOnly := &SystemdUnitDesired{Name: "cups.socket", Raw: []*pb.SystemdDirective{{Section: "Socket", Key: "Backlog", Values: []string{"64"}}}}
	if got := string(RenderSystemdDropIn(rawOnly)); !strings.Contains(got, "[Socket]\nBacklog=\nBacklog=64\n") {
		t.Errorf("raw-only drop-in = %q", got)
	}
}

func TestMergeSystemdTasksAndRaw(t *testing.T) {
	low := taskPolicy("low", "Low", 10, cleanupTask(), &pb.SystemdTask{Name: "only-low", Trigger: trBoot, ExecStart: []string{"/bin/true"}})
	highTask := cleanupTask()
	highTask.Schedule.OnCalendar = []string{"Sat *-*-* 03:00:00"}
	high := taskPolicy("high", "High", 20, highTask, &pb.SystemdTask{Name: "Bad Name", Trigger: trBoot, ExecStart: []string{"/bin/true"}}, &pb.SystemdTask{Name: "no-program", Trigger: trBoot})
	high.Policy.WritePresets = true
	low.Policy.Units = []*pb.SystemdUnit{{Name: "cups.service", DropIn: &pb.SystemdDropIn{Raw: []*pb.SystemdDirective{
		{Section: "Service", Key: "IPAddressDeny", Values: []string{"any"}},
		{Section: "Service", Key: "Nice", Values: []string{"5"}},
		{Section: "Bogus", Key: "X", Values: []string{"1"}},
	}}}}
	high.Policy.Units = []*pb.SystemdUnit{{Name: "cups.service", DropIn: &pb.SystemdDropIn{Raw: []*pb.SystemdDirective{
		{Section: "Service", Key: "IPAddressDeny", Values: []string{"10.0.0.0/8"}},
	}}}}

	d := MergeSystemdPolicies([]SystemdEntry{low, high})
	if !d.WritePresets {
		t.Error("presets on when any policy asks")
	}
	if got := strings.Join(d.TaskNames(), ","); got != "nightly-cleanup,only-low" {
		t.Errorf("TaskNames = %q (invalid tasks must be dropped)", got)
	}
	if nc := d.Tasks["nightly-cleanup"]; nc.By.ID != "high" || nc.Task.GetSchedule().GetOnCalendar()[0] != "Sat *-*-* 03:00:00" {
		t.Errorf("the whole task comes from the highest priority: %+v", nc)
	}
	cups := d.Units["cups.service"]
	if len(cups.Raw) != 2 || cups.RawBy["Service/IPAddressDeny"].ID != "high" || cups.RawBy["Service/Nice"].ID != "low" {
		t.Errorf("raw directives merge per key: %+v / %+v", cups.Raw, cups.RawBy)
	}
	for _, r := range cups.Raw {
		if r.GetKey() == "IPAddressDeny" && r.GetValues()[0] != "10.0.0.0/8" {
			t.Error("highest priority wins a raw directive")
		}
	}
}

func TestSyncSystemdTimerTask(t *testing.T) {
	env := newSyncEnv(t)
	journal := &systemd.Journal{Binary: "sh", Lines: 5, MaxBytes: 2048, Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("cleaning\nrm: cannot remove '/var/cache/lab/x': Permission denied\n"), nil
	}}
	env.opts.Journal = journal
	pol := taskPolicy("p", "Lab tasks", 10, cleanupTask())
	pol.Policy.Tasks[0].RequireLastRunSuccess = true
	dirs := env.opts.dirs()

	res := env.sync(pol)
	r := res.Policies["p"]
	item := itemOf(t, r, "nightly-cleanup")
	if item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("first sync: %s %s", item.GetStatus(), item.GetMessage())
	}
	if item.GetSchemaId() != systemdTaskSchema {
		t.Errorf("schema = %s", item.GetSchemaId())
	}
	for _, p := range []string{dirs.TaskServicePath("nightly-cleanup"), dirs.TaskTimerPath("nightly-cleanup"), dirs.TaskScriptPath("nightly-cleanup")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not written: %v", p, err)
		}
	}
	if fi, err := os.Stat(dirs.TaskScriptPath("nightly-cleanup")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("script mode = %v, %v", fi.Mode(), err)
	}
	for _, call := range []string{"Reload", "Enable bor-nightly-cleanup.timer", "Start bor-nightly-cleanup.timer"} {
		if !env.f.HasCall(call) {
			t.Errorf("missing call %q in %v", call, env.f.Calls)
		}
	}
	if tm := env.f.Unit("bor-nightly-cleanup.timer"); tm == nil || tm.UnitFileState != "enabled" || !tm.IsActive() {
		t.Errorf("timer state: %+v", tm)
	}
	if !strings.Contains(item.GetMessage(), "timer enabled, active (waiting)") || !strings.Contains(item.GetMessage(), "timer started") {
		t.Errorf("message = %q", item.GetMessage())
	}
	if !res.Changed {
		t.Error("Changed must be set")
	}

	// Idempotent.
	env.f.Calls = nil
	res = env.sync(pol)
	if len(env.f.Calls) != 0 || res.Changed {
		t.Errorf("second sync must do nothing: %v changed=%v", env.f.Calls, res.Changed)
	}

	// Schedule change rewrites the timer and restarts it.
	pol.Policy.Tasks[0].Schedule.OnCalendar = []string{"*-*-* 03:00:00"}
	env.f.Calls = nil
	env.sync(pol)
	if !env.f.HasCall("Restart bor-nightly-cleanup.timer") {
		t.Errorf("schedule change must restart the timer: %v", env.f.Calls)
	}
	if data, _ := os.ReadFile(dirs.TaskTimerPath("nightly-cleanup")); !strings.Contains(string(data), "OnCalendar=*-*-* 03:00:00") {
		t.Error("timer file not rewritten")
	}

	// A failed last run is non-compliant with the journal excerpt.
	svc := env.f.Unit("bor-nightly-cleanup.service")
	svc.Result = "exit-code"
	svc.ExecMainStatus = 1
	svc.ExecMainStart = time.Now()
	item = itemOf(t, env.sync(pol).Policies["p"], "nightly-cleanup")
	if item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(item.GetMessage(), "last run failed (exit-code, exit status 1)") || !strings.Contains(item.GetMessage(), "journal: cleaning") {
		t.Errorf("failed run: %s %s", item.GetStatus(), item.GetMessage())
	}
	svc.Result = "success"

	// Timer stopped by hand comes back.
	env.f.Unit("bor-nightly-cleanup.timer").ActiveState = "inactive"
	env.f.Calls = nil
	item = itemOf(t, env.sync(pol).Policies["p"], "nightly-cleanup")
	if !env.f.HasCall("Start bor-nightly-cleanup.timer") || item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("drift: %v / %s %s", env.f.Calls, item.GetStatus(), item.GetMessage())
	}

	// enabled=false keeps the files but stops and disables the timer.
	pol.Policy.Tasks[0].Enabled = boolPtr(false)
	env.f.Calls = nil
	item = itemOf(t, env.sync(pol).Policies["p"], "nightly-cleanup")
	if !env.f.HasCall("Stop bor-nightly-cleanup.timer") || !env.f.HasCall("Disable bor-nightly-cleanup.timer") || item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("disabled task: %v / %s %s", env.f.Calls, item.GetStatus(), item.GetMessage())
	}
	pol.Policy.Tasks[0].Enabled = nil
	env.sync(pol)

	// Removal retires the units and deletes every file.
	env.f.Calls = nil
	res = env.sync()
	for _, call := range []string{"Stop bor-nightly-cleanup.timer", "Disable bor-nightly-cleanup.timer", "Reload"} {
		if !env.f.HasCall(call) {
			t.Errorf("removal: missing %q in %v", call, env.f.Calls)
		}
	}
	for _, p := range []string{dirs.TaskServicePath("nightly-cleanup"), dirs.TaskTimerPath("nightly-cleanup"), dirs.TaskScriptPath("nightly-cleanup")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still present after removal", p)
		}
	}
	if env.f.Unit("bor-nightly-cleanup.timer") != nil {
		t.Error("the fake must forget units whose files are gone")
	}
	if len(res.Policies) != 0 {
		t.Errorf("no policies, no results: %+v", res.Policies)
	}
}

func TestSyncSystemdBootTaskAndOverride(t *testing.T) {
	env := newSyncEnv(t)
	boot := &pb.SystemdTask{Name: "fix-perms", Trigger: trBoot, ExecStart: []string{"/usr/bin/chmod", "0755", "/srv"}}
	low := taskPolicy("low", "Low", 10, boot)
	highBoot := &pb.SystemdTask{Name: "fix-perms", Trigger: trBoot, ExecStart: []string{"/usr/bin/chmod", "0750", "/srv"}}
	high := taskPolicy("high", "High", 20, highBoot)

	res := env.sync(low, high)
	hi := itemOf(t, res.Policies["high"], "fix-perms")
	lo := itemOf(t, res.Policies["low"], "fix-perms")
	if hi.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("high: %s %s", hi.GetStatus(), hi.GetMessage())
	}
	if lo.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE || !strings.Contains(lo.GetMessage(), "overridden by High (priority 20)") {
		t.Errorf("low: %s %s", lo.GetStatus(), lo.GetMessage())
	}
	if !env.f.HasCall("Enable bor-fix-perms.service") || env.f.HasCall("Start bor-fix-perms.service") {
		t.Errorf("a boot task is enabled, not started: %v", env.f.Calls)
	}
	if _, err := os.Stat(env.opts.dirs().TaskTimerPath("fix-perms")); !os.IsNotExist(err) {
		t.Error("a boot task has no timer file")
	}
	if data, _ := os.ReadFile(env.opts.dirs().TaskServicePath("fix-perms")); !strings.Contains(string(data), "ExecStart=/usr/bin/chmod 0750 /srv") {
		t.Errorf("service file = %q", data)
	}
	if !strings.Contains(hi.GetMessage(), "service enabled") {
		t.Errorf("message = %q", hi.GetMessage())
	}
}

func TestSyncSystemdTasksDisabledOnNode(t *testing.T) {
	env := newSyncEnv(t)
	pol := taskPolicy("p", "P", 10, cleanupTask())
	env.sync(pol)
	dirs := env.opts.dirs()
	if _, err := os.Stat(dirs.TaskTimerPath("nightly-cleanup")); err != nil {
		t.Fatal("task not deployed")
	}

	env.opts.AllowTasks = false
	env.f.Calls = nil
	item := itemOf(t, env.sync(pol).Policies["p"], "nightly-cleanup")
	if item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(item.GetMessage(), "allow_tasks") {
		t.Errorf("disabled node: %s %s", item.GetStatus(), item.GetMessage())
	}
	if !env.f.HasCall("Stop bor-nightly-cleanup.timer") {
		t.Errorf("existing task files must be retired: %v", env.f.Calls)
	}
	for _, p := range []string{dirs.TaskServicePath("nightly-cleanup"), dirs.TaskTimerPath("nightly-cleanup"), dirs.TaskScriptPath("nightly-cleanup")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still present", p)
		}
	}
}

func TestSyncSystemdTaskVerifyFailure(t *testing.T) {
	env := newSyncEnv(t)
	env.opts.Analyzer = &systemd.Analyzer{Binary: "sh", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "verify" && strings.HasSuffix(args[2], ".timer") {
			return []byte("bor-nightly-cleanup.timer: Unknown key name 'Bogus' in section 'Timer'\n"), errors.New("exit status 1")
		}
		return nil, nil
	}}
	pol := taskPolicy("p", "P", 10, cleanupTask())
	item := itemOf(t, env.sync(pol).Policies["p"], "nightly-cleanup")
	if item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(item.GetMessage(), "systemd-analyze rejected bor-nightly-cleanup.timer") {
		t.Errorf("verify failure: %s %s", item.GetStatus(), item.GetMessage())
	}
	dirs := env.opts.dirs()
	for _, p := range []string{dirs.TaskServicePath("nightly-cleanup"), dirs.TaskTimerPath("nightly-cleanup"), dirs.TaskScriptPath("nightly-cleanup")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s must be removed after a failed verification", p)
		}
	}
	if env.f.HasCall("Start bor-nightly-cleanup.timer") {
		t.Error("a rejected task must not be started")
	}
}

func TestSyncSystemdPreset(t *testing.T) {
	env := newSyncEnv(t)
	env.f.AddUnit(systemd.UnitStatus{Name: "bluetooth.service", UnitFileState: "enabled", ActiveState: "active"})
	env.f.AddUnit(systemd.UnitStatus{Name: "fstrim.timer", UnitFileState: "disabled"})
	pol := SystemdEntry{ID: "p", Name: "P", Priority: 10, Policy: &pb.SystemdPolicy{
		WritePresets: true,
		Units: []*pb.SystemdUnit{
			sdUnit("bluetooth.service", enDisabled, acStopped),
			sdUnit("fstrim.timer", enEnabled, acRunning),
			sdUnit("avahi-daemon.service", enMasked, acKeep), // not installed: still presetted
		},
	}}
	r := env.sync(pol).Policies["p"]
	item := itemOf(t, r, SystemdPresetName)
	if item.GetSchemaId() != systemdPresetSchema || item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("preset item: %s %s %s", item.GetSchemaId(), item.GetStatus(), item.GetMessage())
	}
	path := env.opts.dirs().PresetPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := systemdDropInHeader + "\ndisable avahi-daemon.service\ndisable bluetooth.service\nenable fstrim.timer\n"
	if string(data) != want {
		t.Errorf("preset = %q", data)
	}
	if !strings.Contains(item.GetMessage(), "3 preset lines") {
		t.Errorf("message = %q", item.GetMessage())
	}

	// Edited by hand: reported and rewritten.
	if err := os.WriteFile(path, []byte("enable everything\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	item = itemOf(t, env.sync(pol).Policies["p"], SystemdPresetName)
	if item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("after rewrite: %s %s", item.GetStatus(), item.GetMessage())
	}
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Error("preset not rewritten")
	}

	// Presets off: the file goes.
	pol.Policy.WritePresets = false
	env.sync(pol)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("preset file must be removed when no policy asks for it")
	}
	if itemOf(t, env.sync(pol).Policies["p"], "bluetooth.service").GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Error("units still managed")
	}
}

func TestSyncSystemdRawDirectives(t *testing.T) {
	env := newSyncEnv(t)
	env.f.AddUnit(systemd.UnitStatus{Name: "cups.service", UnitFileState: "enabled", ActiveState: "active"})
	pol := SystemdEntry{ID: "p", Name: "P", Priority: 10, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		{Name: "cups.service", DropIn: &pb.SystemdDropIn{Raw: []*pb.SystemdDirective{{Section: "Service", Key: "IPAddressDeny", Values: []string{"any"}}}}},
	}}}
	item := itemOf(t, env.sync(pol).Policies["p"], "cups.service")
	if item.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("raw drop-in: %s %s", item.GetStatus(), item.GetMessage())
	}
	data, err := os.ReadFile(SystemdDropInPath(env.opts.SystemDir, "cups.service"))
	if err != nil || !strings.Contains(string(data), "[Service]\nIPAddressDeny=\nIPAddressDeny=any\n") {
		t.Errorf("drop-in = %q, %v", data, err)
	}
	if !env.f.HasCall("Reload") {
		t.Error("a new drop-in needs a reload")
	}
}
