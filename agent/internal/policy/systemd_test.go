// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/proto"
)

func sdUnit(name string, en pb.SystemdEnablement, ac pb.SystemdActivity) *pb.SystemdUnit {
	return &pb.SystemdUnit{Name: name, Enablement: en, Activity: ac}
}

const (
	enEnabled  = pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED
	enDisabled = pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED
	enMasked   = pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED
	enKeep     = pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED
	acRunning  = pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING
	acStopped  = pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED
	acKeep     = pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED
)

func TestMergeSystemdPolicies(t *testing.T) {
	low := SystemdEntry{ID: "low", Name: "Low", Priority: 10, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		sdUnit("bluetooth.service", enEnabled, acRunning),
		{Name: "fstrim.timer", DropIn: &pb.SystemdDropIn{Timer: &pb.SystemdTimerSettings{OnCalendar: []string{"daily"}}}},
		{Name: "cups.service", DropIn: &pb.SystemdDropIn{Service: &pb.SystemdServiceSettings{Restart: "always", MemoryMax: "1G", Environment: []string{"A=low", "B=low"}}}},
		sdUnit("bor-agent.service", enMasked, acKeep),
		sdUnit("../x.service", enMasked, acKeep),
	}}}
	high := SystemdEntry{ID: "high", Name: "High", Priority: 20, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		sdUnit("bluetooth.service", enDisabled, acStopped),
		{Name: "fstrim.timer", DropIn: &pb.SystemdDropIn{Timer: &pb.SystemdTimerSettings{OnCalendar: []string{"Sat *-*-* 03:00:00"}, Persistent: boolPtr(true)}}, RequireLastRunSuccess: true},
		{Name: "cups.service", DropIn: &pb.SystemdDropIn{Service: &pb.SystemdServiceSettings{Restart: "on-failure", Environment: []string{"A=high"}}}},
	}}}
	tieA := SystemdEntry{ID: "a", Name: "A", Priority: 5, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{sdUnit("avahi-daemon.service", enDisabled, acRunning)}}}
	tieB := SystemdEntry{ID: "b", Name: "B", Priority: 5, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{sdUnit("avahi-daemon.service", enMasked, acKeep)}}}

	d := MergeSystemdPolicies([]SystemdEntry{low, high, tieA, tieB})

	bt := d.Units["bluetooth.service"]
	if bt == nil || bt.Enablement != enDisabled || bt.EnablementBy.ID != "high" || bt.Activity != acStopped {
		t.Fatalf("bluetooth: highest priority must win: %+v", bt)
	}
	ft := d.Units["fstrim.timer"]
	if ft == nil || ft.TimerBy.ID != "high" || len(ft.Timer.GetOnCalendar()) != 1 || ft.Timer.GetOnCalendar()[0] != "Sat *-*-* 03:00:00" || !ft.RequireLastRunSuccess {
		t.Fatalf("fstrim: timer block is atomic, highest wins: %+v", ft)
	}
	cs := d.Units["cups.service"]
	if cs == nil || cs.Service.GetRestart() != "on-failure" || cs.Service.GetMemoryMax() != "1G" {
		t.Fatalf("cups: per-directive merge: %+v", cs.Service)
	}
	if cs.ServiceBy["Restart"].ID != "high" || cs.ServiceBy["MemoryMax"].ID != "low" {
		t.Errorf("cups: directive sources: %+v", cs.ServiceBy)
	}
	env := strings.Join(cs.Service.GetEnvironment(), ",")
	if env != "A=high,B=low" {
		t.Errorf("cups: environment union with the higher priority winning duplicates, got %q", env)
	}
	av := d.Units["avahi-daemon.service"]
	if av == nil || av.Enablement != enMasked || av.EnablementBy.ID != "b" {
		t.Fatalf("avahi: tie resolves to the more restrictive value: %+v", av)
	}
	if av.Activity != acStopped || av.ActivityBy.ID != "b" {
		t.Errorf("avahi: masked implies stopped: %+v", av)
	}
	if _, ok := d.Units["bor-agent.service"]; ok {
		t.Error("protected unit must not enter the merged state")
	}
	if _, ok := d.Units["../x.service"]; ok {
		t.Error("invalid unit name must not enter the merged state")
	}
	if got := d.Names(); strings.Join(got, ",") != "avahi-daemon.service,bluetooth.service,cups.service,fstrim.timer" {
		t.Errorf("Names = %v", got)
	}
}

func TestRenderSystemdDropIn(t *testing.T) {
	if RenderSystemdDropIn(&SystemdUnitDesired{Name: "x.service"}) != nil {
		t.Error("no overrides must render nil")
	}
	timer := &SystemdUnitDesired{Name: "fstrim.timer", Timer: &pb.SystemdTimerSettings{
		OnCalendar:         []string{"Sat *-*-* 03:00:00", "Sun *-*-* 03:00:00"},
		OnBootSec:          "15min",
		Persistent:         boolPtr(true),
		RandomizedDelaySec: "1h",
		WakeSystem:         boolPtr(false),
	}}
	want := "# Managed by Bor - changes are overwritten\n# Unit: fstrim.timer\n\n[Timer]\nOnCalendar=\nOnCalendar=Sat *-*-* 03:00:00\nOnCalendar=Sun *-*-* 03:00:00\nOnBootSec=\nOnBootSec=15min\nPersistent=yes\nRandomizedDelaySec=1h\nWakeSystem=no\n"
	if got := string(RenderSystemdDropIn(timer)); got != want {
		t.Errorf("timer drop-in:\n got: %q\nwant: %q", got, want)
	}
	svc := &SystemdUnitDesired{Name: "cups.service", Service: &pb.SystemdServiceSettings{
		Restart:         "on-failure",
		RestartSec:      "5s",
		Environment:     []string{`Z=last`, `A=has "quotes" and \ backslash`},
		MemoryMax:       "512M",
		CpuQuota:        "50%",
		Nice:            "5",
		NoNewPrivileges: boolPtr(true),
		ProtectHome:     "read-only",
		PrivateTmp:      boolPtr(true),
	}}
	want = "# Managed by Bor - changes are overwritten\n# Unit: cups.service\n\n[Service]\nRestart=on-failure\nRestartSec=5s\nEnvironment=\"A=has \\\"quotes\\\" and \\\\ backslash\"\nEnvironment=\"Z=last\"\nMemoryMax=512M\nCPUQuota=50%\nNice=5\nNoNewPrivileges=yes\nProtectHome=read-only\nPrivateTmp=yes\n"
	if got := string(RenderSystemdDropIn(svc)); got != want {
		t.Errorf("service drop-in:\n got: %q\nwant: %q", got, want)
	}
	// Deterministic.
	if !bytes.Equal(RenderSystemdDropIn(svc), RenderSystemdDropIn(svc)) {
		t.Error("rendering must be deterministic")
	}
}

func TestListBorManagedSystemdFiles(t *testing.T) {
	dir := t.TempDir()
	bor := filepath.Join(dir, "fstrim.timer.d", SystemdDropInName)
	if err := WriteFileAtomically(bor, []byte(systemdDropInHeader+"\n[Timer]\n")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "cups.service.d", SystemdDropInName)
	if err := WriteFileAtomically(foreign, []byte("[Service]\nNice=1\n")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "cups.service.d", "10-distro.conf")
	if err := WriteFileAtomically(other, []byte(systemdDropInHeader+"\n")); err != nil {
		t.Fatal(err)
	}
	got, err := ListBorManagedSystemdFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != bor {
		t.Errorf("ListBorManagedSystemdFiles = %v, want [%s]", got, bor)
	}
	if systemdUnitFromDropInPath(bor) != "fstrim.timer" {
		t.Error("unit name recovery from the drop-in path failed")
	}
	if got, _ := ListBorManagedSystemdFiles(filepath.Join(dir, "missing")); got != nil {
		t.Error("a missing directory yields nothing")
	}
}

func TestSystemdStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := LoadSystemdState(path)
	if err != nil || len(st.Units) != 0 {
		t.Fatalf("missing state file: %v, %+v", err, st)
	}
	st.Units["bluetooth.service"] = SystemdOriginal{UnitFileState: "enabled", Active: true, RecordedAt: time.Now()}
	if err = SaveSystemdState(path, st); err != nil {
		t.Fatal(err)
	}
	again, err := LoadSystemdState(path)
	if err != nil || again.Units["bluetooth.service"].UnitFileState != "enabled" || !again.Units["bluetooth.service"].Active {
		t.Fatalf("round trip: %v, %+v", err, again)
	}
	delete(again.Units, "bluetooth.service")
	if err := SaveSystemdState(path, again); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an empty state must remove the file")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err := LoadSystemdState(path); err == nil || st == nil || st.Units == nil {
		t.Error("a corrupt file must return an error and an empty state")
	}
}

// syncEnv bundles a fake manager and temp paths for sync tests.
type syncEnv struct {
	f    *systemd.Fake
	opts SystemdSyncOptions
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	dir := t.TempDir()
	return &syncEnv{
		f: systemd.NewFake(),
		opts: SystemdSyncOptions{
			SystemDir: filepath.Join(dir, "system"),
			StatePath: filepath.Join(dir, "state.json"),
		},
	}
}

func (e *syncEnv) sync(entries ...SystemdEntry) SystemdSyncResult {
	return SyncSystemd(context.Background(), e.f, entries, e.opts)
}

func itemOf(t *testing.T, r SystemdPolicyResult, unit string) *pb.ComplianceItemResult {
	t.Helper()
	for _, it := range r.Items {
		if it.GetKey() == unit {
			return it
		}
	}
	t.Fatalf("no item for %s in %+v", unit, r.Items)
	return nil
}

func TestSyncSystemdBaseline(t *testing.T) {
	e := newSyncEnv(t)
	e.f.AddUnit(systemd.UnitStatus{Name: "bluetooth.service", UnitFileState: "enabled", ActiveState: "active", FragmentPath: "/usr/lib/systemd/system/bluetooth.service"})
	e.f.AddUnit(systemd.UnitStatus{Name: "avahi-daemon.service", UnitFileState: "enabled", ActiveState: "active"})
	e.f.AddUnit(systemd.UnitStatus{Name: "fstrim.timer", UnitFileState: "disabled", ActiveState: "inactive", TimerUnit: "fstrim.service"})
	e.f.AddUnit(systemd.UnitStatus{Name: "fstrim.service", UnitFileState: "static", ServiceType: "oneshot"})
	e.f.AddUnit(systemd.UnitStatus{Name: "ctrl-alt-del.target", UnitFileState: "static"})

	pol := SystemdEntry{ID: "p1", Name: "Baseline", Priority: 10, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		sdUnit("bluetooth.service", enDisabled, acStopped),
		sdUnit("avahi-daemon.service", enMasked, acKeep),
		{Name: "fstrim.timer", Enablement: enEnabled, Activity: acRunning, RequireLastRunSuccess: true,
			DropIn: &pb.SystemdDropIn{Timer: &pb.SystemdTimerSettings{OnCalendar: []string{"Sat *-*-* 03:00:00"}, Persistent: boolPtr(true)}}},
		sdUnit("ctrl-alt-del.target", enMasked, acKeep),
		sdUnit("missing.service", enDisabled, acStopped),
	}}}

	res := e.sync(pol)
	r := res.Policies["p1"]
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("status = %s (%s); items %+v", r.Status, r.Message, r.Items)
	}
	if !res.Changed {
		t.Error("a sync that changed states must report Changed")
	}
	for _, call := range []string{"Stop bluetooth.service", "Disable bluetooth.service", "Stop avahi-daemon.service", "Mask avahi-daemon.service", "Enable fstrim.timer", "Start fstrim.timer", "Mask ctrl-alt-del.target", "Reload"} {
		if !e.f.HasCall(call) {
			t.Errorf("expected call %q; calls: %v", call, e.f.Calls)
		}
	}
	if e.f.HasCall("Disable missing.service") {
		t.Error("a unit that is not installed must not be touched")
	}
	if e.f.Unit("bluetooth.service").UnitFileState != "disabled" || e.f.Unit("bluetooth.service").IsActive() {
		t.Error("bluetooth should be disabled and stopped")
	}
	if e.f.Unit("avahi-daemon.service").LoadState != "masked" {
		t.Error("avahi should be masked after the reload")
	}
	if ft := e.f.Unit("fstrim.timer"); ft.UnitFileState != "enabled" || ft.SubState != "waiting" {
		t.Errorf("fstrim.timer should be enabled and waiting: %+v", ft)
	}
	// Drop-in written with the expected content.
	data, err := os.ReadFile(SystemdDropInPath(e.opts.SystemDir, "fstrim.timer"))
	if err != nil || !strings.Contains(string(data), "OnCalendar=Sat *-*-* 03:00:00") || !strings.Contains(string(data), "Persistent=yes") {
		t.Errorf("drop-in: %v %q", err, data)
	}
	// Items.
	if it := itemOf(t, r, "missing.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("missing unit item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if it := itemOf(t, r, "fstrim.timer"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT || !strings.Contains(it.GetMessage(), "enabled, active (waiting)") {
		t.Errorf("fstrim item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if it := itemOf(t, r, "bluetooth.service"); it.GetSchemaId() != "systemd:unit" || !strings.Contains(it.GetMessage(), "disabled, inactive (dead)") {
		t.Errorf("bluetooth item = %+v", it)
	}
	// Original states recorded.
	st, _ := LoadSystemdState(e.opts.StatePath)
	if rec := st.Units["bluetooth.service"]; rec.UnitFileState != "enabled" || !rec.Active {
		t.Errorf("original state of bluetooth not recorded: %+v", st.Units)
	}
	if _, ok := st.Units["missing.service"]; ok {
		t.Error("units that do not exist must not be recorded")
	}

	// Second sync: idempotent, nothing changes.
	calls := len(e.f.Calls)
	res = e.sync(pol)
	if res.Changed {
		t.Error("a sync with nothing to do must not report Changed")
	}
	for _, c := range e.f.Calls[calls:] {
		if c != "Reload" {
			t.Errorf("unexpected call on an idempotent sync: %s", c)
		}
	}
	if res.Policies["p1"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("second sync status = %s", res.Policies["p1"].Status)
	}

	// Drift: someone starts bluetooth by hand; the next sync stops it again.
	e.f.Unit("bluetooth.service").ActiveState = "active"
	e.f.Unit("bluetooth.service").SubState = "running"
	res = e.sync(pol)
	if e.f.Unit("bluetooth.service").IsActive() {
		t.Error("drift must be reverted")
	}
	if it := itemOf(t, res.Policies["p1"], "bluetooth.service"); !strings.Contains(it.GetMessage(), "stopped") {
		t.Errorf("drift reverting must be mentioned: %q", it.GetMessage())
	}

	// Unbind: everything removed and restored.
	res = e.sync()
	if len(res.Policies) != 0 {
		t.Error("no entries, no reports")
	}
	if _, err := os.Stat(SystemdDropInPath(e.opts.SystemDir, "fstrim.timer")); !os.IsNotExist(err) {
		t.Error("drop-in must be removed when the unit leaves every policy")
	}
	if _, err := os.Stat(e.opts.StatePath); !os.IsNotExist(err) {
		t.Error("state file must be removed once every unit is restored")
	}
	if bt := e.f.Unit("bluetooth.service"); bt.UnitFileState != "enabled" || !bt.IsActive() {
		t.Errorf("bluetooth must be restored to enabled and running: %+v", bt)
	}
	if av := e.f.Unit("avahi-daemon.service"); av.UnitFileState != "enabled" || !av.IsActive() {
		t.Errorf("avahi must be unmasked, enabled and started again: %+v", av)
	}
	if ft := e.f.Unit("fstrim.timer"); ft.UnitFileState != "disabled" || ft.IsActive() {
		t.Errorf("fstrim.timer must be disabled and stopped again: %+v", ft)
	}
	if ct := e.f.Unit("ctrl-alt-del.target"); ct.UnitFileState != "static" {
		t.Errorf("ctrl-alt-del.target must be unmasked: %+v", ct)
	}
}

func TestSyncSystemdOverridden(t *testing.T) {
	e := newSyncEnv(t)
	e.f.AddUnit(systemd.UnitStatus{Name: "bluetooth.service", UnitFileState: "enabled", ActiveState: "active"})
	low := SystemdEntry{ID: "low", Name: "Low", Priority: 1, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{sdUnit("bluetooth.service", enEnabled, acRunning)}}}
	high := SystemdEntry{ID: "high", Name: "High", Priority: 9, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{sdUnit("bluetooth.service", enDisabled, acStopped)}}}
	res := e.sync(low, high)
	if res.Policies["high"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("high = %s %s", res.Policies["high"].Status, res.Policies["high"].Message)
	}
	lowRes := res.Policies["low"]
	if lowRes.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("low = %s %s", lowRes.Status, lowRes.Message)
	}
	if it := itemOf(t, lowRes, "bluetooth.service"); !strings.Contains(it.GetMessage(), "High (priority 9)") {
		t.Errorf("overridden message must name the winner: %q", it.GetMessage())
	}
	// Same value at a lower priority is not overridden.
	same := SystemdEntry{ID: "same", Name: "Same", Priority: 1, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{sdUnit("bluetooth.service", enDisabled, acKeep)}}}
	res = e.sync(same, high)
	if res.Policies["same"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("same value must be compliant: %s %s", res.Policies["same"].Status, res.Policies["same"].Message)
	}
}

func TestSyncSystemdErrors(t *testing.T) {
	e := newSyncEnv(t)
	e.f.AddUnit(systemd.UnitStatus{Name: "static.service", UnitFileState: "static"})
	e.f.AddUnit(systemd.UnitStatus{Name: "local.service", UnitFileState: "enabled", FragmentPath: "/etc/systemd/system/local.service"})
	e.f.AddUnit(systemd.UnitStatus{Name: "broken.service", UnitFileState: "enabled"})
	e.f.AddUnit(systemd.UnitStatus{Name: "cond.service", UnitFileState: "enabled"})
	e.f.JobResults["Start broken.service"] = systemd.JobFailed
	e.f.JobResults["Start cond.service"] = systemd.JobSkipped

	pol := SystemdEntry{ID: "p", Name: "P", Priority: 1, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		sdUnit("static.service", enEnabled, acRunning),
		sdUnit("local.service", enMasked, acKeep),
		sdUnit("broken.service", enKeep, acRunning),
		sdUnit("cond.service", enKeep, acRunning),
	}}}
	res := e.sync(pol)
	r := res.Policies["p"]
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Fatalf("status = %s", r.Status)
	}
	if it := itemOf(t, r, "static.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(it.GetMessage(), "[Install]") {
		t.Errorf("static item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if !e.f.HasCall("Start static.service") {
		t.Error("activity must still be applied to a static unit")
	}
	if it := itemOf(t, r, "local.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(it.GetMessage(), "already exists") {
		t.Errorf("local item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if it := itemOf(t, r, "broken.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(it.GetMessage(), "failed") {
		t.Errorf("broken item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if it := itemOf(t, r, "cond.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE || !strings.Contains(it.GetMessage(), "condition") {
		t.Errorf("cond item = %s %q", it.GetStatus(), it.GetMessage())
	}

	// Unavailable manager: every policy inapplicable; nil manager: error.
	e.f.Unavailable = true
	if got := e.sync(pol).Policies["p"].Status; got != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("unavailable = %s", got)
	}
	if got := SyncSystemd(context.Background(), nil, []SystemdEntry{pol}, e.opts).Policies["p"].Status; got != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Errorf("nil manager = %s", got)
	}
}

func TestSyncSystemdDropInRollbackAndStale(t *testing.T) {
	e := newSyncEnv(t)
	e.f.AddUnit(systemd.UnitStatus{Name: "cups.service", UnitFileState: "enabled", ActiveState: "active"})
	e.f.AddUnit(systemd.UnitStatus{Name: "fstrim.timer", UnitFileState: "enabled", ActiveState: "active", SubState: "waiting"})
	// A stale Bor drop-in for a unit no policy names.
	stale := SystemdDropInPath(e.opts.SystemDir, "old.service")
	if err := WriteFileAtomically(stale, []byte(systemdDropInHeader+"\n[Service]\nNice=1\n")); err != nil {
		t.Fatal(err)
	}
	e.f.NextLoadState["cups.service"] = "bad-setting"

	pol := SystemdEntry{ID: "p", Name: "P", Priority: 1, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		{Name: "cups.service", RestartOnChange: true, DropIn: &pb.SystemdDropIn{Service: &pb.SystemdServiceSettings{Nice: "5"}}},
		{Name: "fstrim.timer", DropIn: &pb.SystemdDropIn{Timer: &pb.SystemdTimerSettings{OnCalendar: []string{"daily"}}}},
	}}}
	res := e.sync(pol)
	r := res.Policies["p"]
	if it := itemOf(t, r, "cups.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(it.GetMessage(), "rejected") {
		t.Errorf("cups item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if _, err := os.Stat(SystemdDropInPath(e.opts.SystemDir, "cups.service")); !os.IsNotExist(err) {
		t.Error("a rejected drop-in must be removed")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale drop-in must be removed")
	}
	if e.f.Reloads < 2 {
		t.Errorf("expected a reload after the write and one after the rollback, got %d", e.f.Reloads)
	}
	if it := itemOf(t, r, "fstrim.timer"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("fstrim item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if !e.f.HasCall("Restart fstrim.timer") {
		t.Error("a timer must be restarted after its schedule changed")
	}

	// Service restart on change, then tamper with the drop-in.
	delete(e.f.NextLoadState, "cups.service")
	e.f.Unit("cups.service").LoadState = "loaded"
	calls := len(e.f.Calls)
	res = e.sync(pol)
	if !e.f.HasCall("Restart cups.service") {
		t.Errorf("service with restart_on_change must be restarted after the drop-in changed; calls %v", e.f.Calls[calls:])
	}
	if res.Policies["p"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("status = %s %s", res.Policies["p"].Status, res.Policies["p"].Message)
	}
	path := SystemdDropInPath(e.opts.SystemDir, "cups.service")
	if err := os.WriteFile(path, []byte(systemdDropInHeader+"\n[Service]\nNice=19\n"), 0o644); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}
	res = e.sync(pol)
	data, _ := os.ReadFile(path) //nolint:gosec // test file
	if !strings.Contains(string(data), "Nice=5") {
		t.Error("a tampered drop-in must be rewritten")
	}
	if res.Policies["p"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("after rewrite status = %s %s", res.Policies["p"].Status, res.Policies["p"].Message)
	}
}

func TestSyncSystemdCalendarValidation(t *testing.T) {
	e := newSyncEnv(t)
	e.f.AddUnit(systemd.UnitStatus{Name: "fstrim.timer", UnitFileState: "enabled", ActiveState: "active", SubState: "waiting"})
	e.opts.Analyzer = &systemd.Analyzer{Binary: "/bin/true", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(args[len(args)-1], "Funday") {
			return []byte("Failed to parse calendar specification"), errors.New("exit status 1")
		}
		return nil, nil
	}}
	pol := SystemdEntry{ID: "p", Name: "P", Priority: 1, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		{Name: "fstrim.timer", DropIn: &pb.SystemdDropIn{Timer: &pb.SystemdTimerSettings{OnCalendar: []string{"Funday 03:00"}}}},
	}}}
	res := e.sync(pol)
	it := itemOf(t, res.Policies["p"], "fstrim.timer")
	if it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(it.GetMessage(), "Failed to parse") {
		t.Errorf("item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if _, err := os.Stat(SystemdDropInPath(e.opts.SystemDir, "fstrim.timer")); !os.IsNotExist(err) {
		t.Error("an invalid schedule must not be written")
	}
}

func TestSyncSystemdLastRunAndOneshot(t *testing.T) {
	e := newSyncEnv(t)
	e.f.AddUnit(systemd.UnitStatus{Name: "backup.timer", UnitFileState: "enabled", ActiveState: "active", SubState: "waiting", TimerUnit: "backup.service", LastTrigger: time.Now().Add(-time.Hour)})
	e.f.AddUnit(systemd.UnitStatus{Name: "backup.service", UnitFileState: "static", ServiceType: "oneshot", Result: "exit-code", ExecMainStatus: 2})
	e.f.AddUnit(systemd.UnitStatus{Name: "job.service", UnitFileState: "static", ServiceType: "oneshot"})
	pol := SystemdEntry{ID: "p", Name: "P", Priority: 1, Policy: &pb.SystemdPolicy{Units: []*pb.SystemdUnit{
		{Name: "backup.timer", RequireLastRunSuccess: true},
		sdUnit("job.service", enKeep, acRunning),
	}}}
	res := e.sync(pol)
	r := res.Policies["p"]
	if it := itemOf(t, r, "backup.timer"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(it.GetMessage(), "exit status 2") {
		t.Errorf("timer item = %s %q", it.GetStatus(), it.GetMessage())
	}
	if !e.f.HasCall("Start job.service") {
		t.Error("a oneshot that never ran must be started")
	}
	if it := itemOf(t, r, "job.service"); it.GetStatus() != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("oneshot item = %s %q", it.GetStatus(), it.GetMessage())
	}
	// Once it ran successfully it is not started again.
	calls := len(e.f.Calls)
	e.sync(pol)
	for _, c := range e.f.Calls[calls:] {
		if c == "Start job.service" {
			t.Error("a oneshot that ran successfully must not be started again")
		}
	}
}

func TestScanSystemdUnits(t *testing.T) {
	dir := t.TempDir()
	timerPath := filepath.Join(dir, "fstrim.timer")
	if err := os.WriteFile(timerPath, []byte("[Unit]\nDescription=Weekly trim\n[Timer]\nOnCalendar=weekly\n[Install]\nWantedBy=timers.target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svcPath := filepath.Join(dir, "cups.service")
	if err := os.WriteFile(svcPath, []byte("[Service]\nType=notify\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := systemd.NewFake()
	f.AddUnit(systemd.UnitStatus{Name: "fstrim.timer", UnitFileState: "enabled", ActiveState: "active", SubState: "waiting", FragmentPath: timerPath})
	f.AddUnit(systemd.UnitStatus{Name: "cups.service", UnitFileState: "static", FragmentPath: svcPath, Description: "CUPS from the manager"})
	f.AddUnit(systemd.UnitStatus{Name: "home.mount", UnitFileState: "static"})
	units, err := ScanSystemdUnits(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("units = %+v", units)
	}
	cups, fstrim := units[0], units[1]
	if cups.GetName() != "cups.service" || cups.GetServiceType() != "notify" || cups.GetHasInstall() || cups.GetDescription() != "CUPS from the manager" {
		t.Errorf("cups = %+v", cups)
	}
	if fstrim.GetType() != "timer" || !fstrim.GetHasInstall() || fstrim.GetTriggers() != "fstrim.service" || fstrim.GetActiveState() != "active" || len(fstrim.GetOnCalendar()) != 1 || fstrim.GetDescription() != "Weekly trim" {
		t.Errorf("fstrim = %+v", fstrim)
	}
	if !proto.Equal(fstrim, fstrim) {
		t.Error("sanity")
	}
}
