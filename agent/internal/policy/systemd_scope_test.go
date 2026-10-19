// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// globalEnv extends syncEnv with a user unit directory, a library
// directory of user units and two logged-in users with fake managers.
type globalEnv struct {
	*syncEnv
	userDir string
	libDir  string
	alice   *systemd.Fake
	bob     *systemd.Fake
}

func newGlobalEnv(t *testing.T) *globalEnv {
	t.Helper()
	env := newSyncEnv(t)
	base := filepath.Dir(env.opts.SystemDir)
	g := &globalEnv{syncEnv: env, userDir: filepath.Join(base, "user"), libDir: filepath.Join(base, "userlib")}
	if err := os.MkdirAll(g.libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env.opts.UserDir = g.userDir
	env.opts.UserPresetDir = filepath.Join(base, "user-preset")
	env.opts.Globals = &systemd.GlobalUnits{EtcDir: g.userDir, LibDirs: []string{g.libDir}}
	g.alice, g.bob = systemd.NewFake(), systemd.NewFake()
	for _, f := range []*systemd.Fake{g.alice, g.bob} {
		f.UnitDirs = []string{g.userDir}
	}
	env.opts.Users = &systemd.FakeUserManagers{
		Users:    []systemd.UserSession{{UID: 1000, User: "alice"}, {UID: 1001, User: "bob"}},
		Managers: map[uint32]*systemd.Fake{1000: g.alice, 1001: g.bob},
	}
	return g
}

func (g *globalEnv) libUnit(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.libDir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func globalUnit(name string, en pb.SystemdEnablement, ac pb.SystemdActivity) *pb.SystemdUnit {
	return &pb.SystemdUnit{Name: name, Enablement: en, Activity: ac, Scope: pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL}
}

func TestSyncSystemdGlobalUnit(t *testing.T) {
	g := newGlobalEnv(t)
	g.libUnit(t, "syncthing.service", "[Unit]\nDescription=Syncthing\n[Service]\nExecStart=/usr/bin/syncthing\n[Install]\nWantedBy=default.target\n")
	g.libUnit(t, "tracker.service", "[Service]\nExecStart=/usr/bin/tracker\n[Install]\nWantedBy=default.target\n")
	for _, f := range []*systemd.Fake{g.alice, g.bob} {
		f.AddUnit(systemd.UnitStatus{Name: "syncthing.service", UnitFileState: "disabled", FragmentPath: filepath.Join(g.libDir, "syncthing.service")})
		f.AddUnit(systemd.UnitStatus{Name: "tracker.service", UnitFileState: "enabled", ActiveState: "active", FragmentPath: filepath.Join(g.libDir, "tracker.service")})
	}
	pol := SystemdEntry{ID: "p", Name: "Users", Priority: 10, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		globalUnit("syncthing.service", enEnabled, acRunning),
		globalUnit("tracker.service", enMasked, acKeep),
		{Name: "syncthing.service", Scope: pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL, DropIn: &pb.SystemdDropIn{Service: &pb.SystemdServiceSettings{MemoryMax: "512M"}}},
		globalUnit("graphical-session.target", enDisabled, acKeep),
		globalUnit("ghost.service", enEnabled, acKeep),
	}}}
	res := g.sync(pol)
	r := res.Policies["p"]

	st := itemOf(t, r, "global/syncthing.service")
	if st.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("syncthing: %s %s", st.GetStatus(), st.GetMessage())
	}
	for _, want := range []string{"enabled for all users", "active (running) for alice, bob", "started for alice", "started for bob"} {
		if !strings.Contains(st.GetMessage(), want) {
			t.Errorf("syncthing message %q lacks %q", st.GetMessage(), want)
		}
	}
	if _, err := os.Readlink(filepath.Join(g.userDir, "default.target.wants", "syncthing.service")); err != nil {
		t.Error("global enablement link missing")
	}
	dropIn := filepath.Join(g.userDir, "syncthing.service.d", SystemdDropInName)
	if data, err := os.ReadFile(dropIn); err != nil || !strings.Contains(string(data), "MemoryMax=512M") {
		t.Errorf("user drop-in: %v %q", err, data)
	}
	for name, f := range map[string]*systemd.Fake{"alice": g.alice, "bob": g.bob} {
		if !f.HasCall("Start syncthing.service") || f.Reloads == 0 {
			t.Errorf("%s: calls %v reloads %d", name, f.Calls, f.Reloads)
		}
		if f.HasCall("Enable syncthing.service") {
			t.Errorf("%s: enablement is global, not per user", name)
		}
		if !f.HasCall("Stop tracker.service") {
			t.Errorf("%s: a masked unit is stopped in every session: %v", name, f.Calls)
		}
	}
	// The desired state is re-read from one policy entry per unit, so the
	// second syncthing entry (overrides only) reports against the same unit.
	tr := itemOf(t, r, "global/tracker.service")
	if tr.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT || !strings.Contains(tr.GetMessage(), "masked for all users") {
		t.Errorf("tracker: %s %s", tr.GetStatus(), tr.GetMessage())
	}
	if target, err := os.Readlink(filepath.Join(g.userDir, "tracker.service")); err != nil || target != os.DevNull {
		t.Errorf("mask link: %q %v", target, err)
	}
	if it := itemOf(t, r, "global/graphical-session.target"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(it.GetMessage(), "protected") {
		t.Errorf("protected user target: %s %s", it.GetStatus(), it.GetMessage())
	}
	if it := itemOf(t, r, "global/ghost.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("missing user unit: %s %s", it.GetStatus(), it.GetMessage())
	}
	state, _ := LoadSystemdState(g.opts.StatePath)
	if rec, ok := state.Units["global/syncthing.service"]; !ok || rec.UnitFileState != "disabled" {
		t.Errorf("original global state not recorded: %+v", state.Units)
	}

	// A user stops the service by hand: the next pass starts it for that
	// user only.
	g.bob.Unit("syncthing.service").ActiveState = "inactive"
	g.bob.Calls = nil
	g.alice.Calls = nil
	g.opts.Reconcile = true
	res = g.sync(pol)
	if !g.bob.HasCall("Start syncthing.service") || g.alice.HasCall("Start syncthing.service") {
		t.Errorf("reconcile: alice %v bob %v", g.alice.Calls, g.bob.Calls)
	}
	if len(res.Drifts) != 0 {
		t.Errorf("user-scope corrections are not drift events: %+v", res.Drifts)
	}

	// Unbinding restores the enablement and removes the files.
	res = g.sync()
	if _, err := os.Lstat(filepath.Join(g.userDir, "default.target.wants", "syncthing.service")); !os.IsNotExist(err) {
		t.Error("restore must disable syncthing again")
	}
	if _, err := os.Lstat(filepath.Join(g.userDir, "tracker.service")); !os.IsNotExist(err) {
		t.Error("restore must unmask tracker")
	}
	if _, err := os.Lstat(filepath.Join(g.userDir, "tracker.service")); !os.IsNotExist(err) {
		t.Error("restore must unmask tracker")
	}
	if _, err := os.Stat(dropIn); !os.IsNotExist(err) {
		t.Error("the user drop-in must be removed")
	}
	if _, err := os.Stat(g.opts.StatePath); !os.IsNotExist(err) {
		t.Error("the state file must be empty and removed")
	}
}

func TestSyncSystemdGlobalTask(t *testing.T) {
	g := newGlobalEnv(t)
	task := &pb.SystemdTask{
		Name:     "cache-clean",
		Scope:    pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL,
		Trigger:  pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER,
		Schedule: &pb.SystemdTimerSettings{OnCalendar: []string{"daily"}},
		Script:   "#!/bin/sh\nrm -rf ~/.cache/thumbnails\n",
	}
	login := &pb.SystemdTask{Name: "greet", Scope: pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL, Trigger: pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_BOOT, ExecStart: []string{"/usr/bin/notify-send", "hello"}}
	pol := SystemdEntry{ID: "p", Name: "Users", Priority: 10, Policy: &pb.SystemdPolicy{Tasks: []*pb.SystemdTask{task, login}}}
	res := g.sync(pol)
	r := res.Policies["p"]

	svc, err := os.ReadFile(filepath.Join(g.userDir, "bor-cache-clean.service"))
	if err != nil {
		t.Fatalf("user task service: %v", err)
	}
	for _, want := range []string{"NoNewPrivileges=yes", "RestrictSUIDSGID=yes"} {
		if !strings.Contains(string(svc), want) {
			t.Errorf("user task lacks %s", want)
		}
	}
	for _, unwanted := range []string{"PrivateTmp=", "ProtectKernelTunables=", "User="} {
		if strings.Contains(string(svc), unwanted) {
			t.Errorf("user task must not set %s", unwanted)
		}
	}
	greet, _ := os.ReadFile(filepath.Join(g.userDir, "bor-greet.service"))
	if !strings.Contains(string(greet), "WantedBy=default.target") {
		t.Errorf("a login task is wanted by default.target: %s", greet)
	}
	if _, err := os.Stat(filepath.Join(g.opts.SystemDir, "bor-cache-clean.service")); !os.IsNotExist(err) {
		t.Error("a user task must not be written to the system directory")
	}
	for _, link := range []string{filepath.Join(g.userDir, "timers.target.wants", "bor-cache-clean.timer"), filepath.Join(g.userDir, "default.target.wants", "bor-greet.service")} {
		if _, err := os.Readlink(link); err != nil {
			t.Errorf("enablement link %s: %v", link, err)
		}
	}
	for name, f := range map[string]*systemd.Fake{"alice": g.alice, "bob": g.bob} {
		if !f.HasCall("Start bor-cache-clean.timer") {
			t.Errorf("%s: timer not started: %v", name, f.Calls)
		}
	}
	it := itemOf(t, r, "cache-clean")
	if it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("task: %s %s", it.GetStatus(), it.GetMessage())
	}
	for _, want := range []string{"bor-cache-clean.timer enabled for all users", "alice: timer active (waiting)", "timer started for bob"} {
		if !strings.Contains(it.GetMessage(), want) {
			t.Errorf("message %q lacks %q", it.GetMessage(), want)
		}
	}
	if it := itemOf(t, r, "greet"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("login task: %s %s", it.GetStatus(), it.GetMessage())
	}

	// Retiring the task stops the timers, drops the links and the files.
	g.sync()
	if !g.alice.HasCall("Stop bor-cache-clean.timer") {
		t.Errorf("alice: timer not stopped on retire: %v", g.alice.Calls)
	}
	for _, p := range []string{filepath.Join(g.userDir, "bor-cache-clean.service"), filepath.Join(g.userDir, "bor-cache-clean.timer"), filepath.Join(g.userDir, "timers.target.wants", "bor-cache-clean.timer"), g.opts.dirs().TaskScriptPath("cache-clean")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s must be removed", p)
		}
	}
}

func TestSyncSystemdDrift(t *testing.T) {
	env := newSyncEnv(t)
	env.f.AddUnit(systemd.UnitStatus{Name: "bluetooth.service", UnitFileState: "enabled", ActiveState: "active"})
	env.f.AddUnit(systemd.UnitStatus{Name: "cups.service", UnitFileState: "enabled", ActiveState: "active"})
	env.f.AddUnit(systemd.UnitStatus{Name: "avahi-daemon.service", UnitFileState: "enabled", ActiveState: "inactive"})
	pol := SystemdEntry{ID: "p", Name: "Baseline", Priority: 10, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		sdUnit("bluetooth.service", enDisabled, acStopped),
		sdUnit("cups.service", enKeep, acRunning),
	}}}
	if res := env.sync(pol); len(res.Drifts) != 0 {
		t.Fatalf("the first application is not drift: %+v", res.Drifts)
	}
	env.opts.Reconcile = true
	if res := env.sync(pol); len(res.Drifts) != 0 {
		t.Fatalf("a quiet reconcile pass reports no drift: %+v", res.Drifts)
	}

	// Someone starts bluetooth and re-enables it, and stops cups.
	env.f.Unit("bluetooth.service").ActiveState = "active"
	env.f.Unit("bluetooth.service").SubState = "running"
	env.f.Unit("bluetooth.service").UnitFileState = "enabled"
	env.f.Unit("cups.service").ActiveState = "inactive"
	env.f.Unit("cups.service").SubState = "dead"
	res := env.sync(pol)
	got := map[string]SystemdDrift{}
	for _, d := range res.Drifts {
		got[d.Unit+"/"+d.Kind] = d
	}
	if len(got) != 3 {
		t.Fatalf("drifts = %+v", res.Drifts)
	}
	if d := got["bluetooth.service/activity"]; d.Action != "stopped" || d.Found != "active (running)" {
		t.Errorf("bluetooth activity drift = %+v", d)
	}
	if d := got["bluetooth.service/enablement"]; d.Action != "disabled" || d.Found != "enabled" {
		t.Errorf("bluetooth enablement drift = %+v", d)
	}
	if d := got["cups.service/activity"]; d.Action != "started" || d.Found != "inactive (dead)" || d.Resource() != "unit:cups.service" || !strings.Contains(d.Description(), "found inactive (dead); started") {
		t.Errorf("cups drift = %+v", d)
	}

	// A unit that enters the policy during a reconcile pass is applied,
	// not reported: it had no record yet.
	pol.Policy.Units = append(pol.Policy.Units, sdUnit("avahi-daemon.service", enKeep, acRunning))
	if res := env.sync(pol); len(res.Drifts) != 0 {
		t.Errorf("new unit must not be drift: %+v", res.Drifts)
	}
}

func TestRenderSystemdPresetScope(t *testing.T) {
	d := MergeSystemdPolicies([]SystemdEntry{{ID: "p", Priority: 1, Policy: &pb.SystemdPolicy{WritePresets: true, Units: []*pb.SystemdUnit{
		sdUnit("cups.service", enDisabled, acKeep),
		globalUnit("syncthing.service", enEnabled, acKeep),
	}}}})
	if got := string(RenderSystemdPreset(d)); !strings.Contains(got, "disable cups.service") || strings.Contains(got, "syncthing") {
		t.Errorf("system preset = %q", got)
	}
	if got := string(RenderSystemdPresetScope(d, true)); !strings.Contains(got, "enable syncthing.service") || strings.Contains(got, "cups") {
		t.Errorf("user preset = %q", got)
	}
	if got := d.Names(); strings.Join(got, ",") != "cups.service,global/syncthing.service" {
		t.Errorf("Names = %v", got)
	}
}

func TestSystemdUnitInfoFromStatus(t *testing.T) {
	info := SystemdUnitInfoFromStatus(&systemd.UnitStatus{Name: "fstrim.timer", Description: "Trim", UnitFileState: "enabled", ActiveState: "active", SubState: "waiting", TimerCalendar: []string{"weekly"}})
	if info.GetType() != "timer" || info.GetTriggers() != "fstrim.service" || !info.GetHasInstall() || info.GetOnCalendar()[0] != "weekly" {
		t.Errorf("info = %+v", info)
	}
	if SystemdUnitInfoFromStatus(nil) != nil {
		t.Error("nil status")
	}
}

func TestSyncSystemdMaskReplacesRedirectLink(t *testing.T) {
	env := newSyncEnv(t)
	if err := os.MkdirAll(env.opts.SystemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fedora ships /etc/systemd/system/ctrl-alt-del.target as a symlink to
	// reboot.target; the unit therefore reports as enabled with the target's
	// fragment path.
	link := filepath.Join(env.opts.SystemDir, "ctrl-alt-del.target")
	if err := os.Symlink("/usr/lib/systemd/system/reboot.target", link); err != nil {
		t.Fatal(err)
	}
	env.f.AddUnit(systemd.UnitStatus{Name: "ctrl-alt-del.target", UnitFileState: "enabled", FragmentPath: "/usr/lib/systemd/system/reboot.target"})
	// A record written by an agent that did not know about links (the mask
	// was refused then) gains the link target on the next sync.
	if err := SaveSystemdState(env.opts.StatePath, &SystemdState{Units: map[string]SystemdOriginal{
		"ctrl-alt-del.target": {UnitFileState: "enabled", RecordedAt: time.Now()},
	}}); err != nil {
		t.Fatal(err)
	}
	pol := SystemdEntry{ID: "p", Name: "Hardening", Priority: 10, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{sdUnit("ctrl-alt-del.target", enMasked, acKeep)}}}
	r := env.sync(pol).Policies["p"]
	if it := itemOf(t, r, "ctrl-alt-del.target"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT || !strings.Contains(it.GetMessage(), "masked") {
		t.Fatalf("mask over a redirect link: %s %s", it.GetStatus(), it.GetMessage())
	}
	if !env.f.HasCall("Mask ctrl-alt-del.target") {
		t.Fatalf("calls %v", env.f.Calls)
	}
	state, _ := LoadSystemdState(env.opts.StatePath)
	if rec := state.Units["ctrl-alt-del.target"]; rec.LinkTarget != "/usr/lib/systemd/system/reboot.target" || rec.UnitFileState != "enabled" {
		t.Fatalf("record = %+v", rec)
	}
	// systemd replaced the link with the mask; model that on disk.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.DevNull, link); err != nil {
		t.Fatal(err)
	}
	// Unmasking on restore removes the mask (systemd's job) and recreates
	// the redirect (ours).
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	env.sync()
	if !env.f.HasCall("Unmask ctrl-alt-del.target") {
		t.Errorf("restore must unmask: %v", env.f.Calls)
	}
	if target, err := os.Readlink(link); err != nil || target != "/usr/lib/systemd/system/reboot.target" {
		t.Errorf("redirect link not restored: %q %v", target, err)
	}
}
