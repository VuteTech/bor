// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
	"google.golang.org/protobuf/proto"
)

// SystemdSystemDir holds administrator unit files and drop-ins.
const SystemdSystemDir = "/etc/systemd/system"

// SystemdDropInName is the one drop-in Bor writes per unit. It sorts after
// the usual distribution drop-ins (10-, 50-) so Bor's values win.
const SystemdDropInName = "90-bor.conf"

// systemdDropInHeader marks a drop-in as Bor's; ListBorManagedSystemdFiles
// recognises files by it.
const systemdDropInHeader = "# Managed by Bor - changes are overwritten"

// Compliance item schema id for unit items.
const systemdItemSchema = "systemd:unit"

// SystemdEntry pairs a SystemdPolicy with the metadata the merge and the
// per-policy compliance reports need.
type SystemdEntry struct {
	ID       string
	Name     string
	Priority int32
	Policy   *pb.SystemdPolicy
}

// systemdSource identifies the policy that won an axis of a unit.
type systemdSource struct {
	ID       string
	Name     string
	Priority int32
}

// SystemdUnitDesired is the merged desired state of one unit.
type SystemdUnitDesired struct {
	Name                  string
	Enablement            pb.SystemdEnablement
	EnablementBy          systemdSource
	Activity              pb.SystemdActivity
	ActivityBy            systemdSource
	Timer                 *pb.SystemdTimerSettings
	TimerBy               systemdSource
	Service               *pb.SystemdServiceSettings
	ServiceBy             map[string]systemdSource
	RestartOnChange       bool
	RequireLastRunSuccess bool
}

// SystemdDesired is the merged desired state of every managed unit.
type SystemdDesired struct {
	Units map[string]*SystemdUnitDesired
}

// Names returns the managed unit names, sorted.
func (d *SystemdDesired) Names() []string {
	names := make([]string, 0, len(d.Units))
	for n := range d.Units {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ── merge ───────────────────────────────────────────────────────────────

// systemdEnablementRank orders enablement values by restrictiveness.
func systemdEnablementRank(e pb.SystemdEnablement) int {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return 3
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return 2
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return 1
	}
	return 0
}

func systemdActivityRank(a pb.SystemdActivity) int {
	switch a {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return 2
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		return 1
	}
	return 0
}

// sortSystemdEntries orders entries by priority (highest first), then by
// name and id so the merge is deterministic.
func sortSystemdEntries(entries []SystemdEntry) []SystemdEntry {
	sorted := make([]SystemdEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

// MergeSystemdPolicies combines every bound policy into one desired state
// per unit. Highest binding priority wins each axis; on equal priority
// the more restrictive value wins (masked > disabled > enabled, stopped >
// running). A masked unit is always stopped. The timer block is atomic;
// service directives merge one by one; environment entries are a union
// keyed by variable name. The record of which policy won each axis feeds
// the per-policy compliance reports.
func MergeSystemdPolicies(entries []SystemdEntry) *SystemdDesired {
	d := &SystemdDesired{Units: map[string]*SystemdUnitDesired{}}
	for _, e := range sortSystemdEntries(entries) {
		if e.Policy == nil {
			continue
		}
		src := systemdSource{ID: e.ID, Name: e.Name, Priority: e.Priority}
		for _, u := range e.Policy.GetUnits() {
			name := u.GetName()
			if systemdunits.ValidateName(name) != nil || systemdunits.IsProtected(name) {
				continue
			}
			cur := d.Units[name]
			if cur == nil {
				cur = &SystemdUnitDesired{Name: name, ServiceBy: map[string]systemdSource{}}
				d.Units[name] = cur
			}
			if en := u.GetEnablement(); en != pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
				if cur.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED ||
					(cur.EnablementBy.Priority == e.Priority && systemdEnablementRank(en) > systemdEnablementRank(cur.Enablement)) {
					cur.Enablement = en
					cur.EnablementBy = src
				}
			}
			if ac := u.GetActivity(); ac != pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED {
				if cur.Activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED ||
					(cur.ActivityBy.Priority == e.Priority && systemdActivityRank(ac) > systemdActivityRank(cur.Activity)) {
					cur.Activity = ac
					cur.ActivityBy = src
				}
			}
			if t := u.GetDropIn().GetTimer(); !systemdTimerEmpty(t) && cur.Timer == nil {
				cur.Timer = proto.Clone(t).(*pb.SystemdTimerSettings)
				cur.TimerBy = src
			}
			if s := u.GetDropIn().GetService(); !systemdServiceEmpty(s) {
				mergeSystemdService(cur, s, src)
			}
			cur.RestartOnChange = cur.RestartOnChange || u.GetRestartOnChange()
			cur.RequireLastRunSuccess = cur.RequireLastRunSuccess || u.GetRequireLastRunSuccess()
		}
	}
	for _, cur := range d.Units {
		if cur.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED &&
			cur.Activity != pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED {
			cur.Activity = pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED
			cur.ActivityBy = cur.EnablementBy
		}
	}
	return d
}

// mergeSystemdService merges one policy's service settings into the
// unit's merged settings. Entries are processed from the highest
// priority down, so a directive that is already set keeps its value.
func mergeSystemdService(cur *SystemdUnitDesired, s *pb.SystemdServiceSettings, src systemdSource) {
	if cur.Service == nil {
		cur.Service = &pb.SystemdServiceSettings{}
	}
	m := cur.Service
	setStr := func(key string, dst *string, val string) {
		if val != "" && *dst == "" {
			*dst = val
			cur.ServiceBy[key] = src
		}
	}
	setBool := func(key string, dst **bool, val *bool) {
		if val != nil && *dst == nil {
			v := *val
			*dst = &v
			cur.ServiceBy[key] = src
		}
	}
	setStr("Restart", &m.Restart, s.GetRestart())
	setStr("RestartSec", &m.RestartSec, s.GetRestartSec())
	setStr("TimeoutStartSec", &m.TimeoutStartSec, s.GetTimeoutStartSec())
	setStr("TimeoutStopSec", &m.TimeoutStopSec, s.GetTimeoutStopSec())
	setStr("MemoryMax", &m.MemoryMax, s.GetMemoryMax())
	setStr("CPUQuota", &m.CpuQuota, s.GetCpuQuota())
	setStr("TasksMax", &m.TasksMax, s.GetTasksMax())
	setStr("Nice", &m.Nice, s.GetNice())
	setStr("ProtectSystem", &m.ProtectSystem, s.GetProtectSystem())
	setStr("ProtectHome", &m.ProtectHome, s.GetProtectHome())
	setBool("NoNewPrivileges", &m.NoNewPrivileges, s.NoNewPrivileges)
	setBool("PrivateTmp", &m.PrivateTmp, s.PrivateTmp)
	for _, env := range s.GetEnvironment() {
		key, _, _ := strings.Cut(env, "=")
		if key == "" {
			continue
		}
		if _, taken := cur.ServiceBy["Environment:"+key]; taken {
			continue
		}
		m.Environment = append(m.Environment, env)
		cur.ServiceBy["Environment:"+key] = src
	}
}

func systemdTimerEmpty(t *pb.SystemdTimerSettings) bool {
	if t == nil {
		return true
	}
	return len(t.GetOnCalendar()) == 0 && t.GetOnBootSec() == "" && t.GetOnStartupSec() == "" &&
		t.GetOnActiveSec() == "" && t.GetOnUnitActiveSec() == "" && t.GetOnUnitInactiveSec() == "" &&
		t.Persistent == nil && t.GetRandomizedDelaySec() == "" && t.GetAccuracySec() == "" &&
		t.WakeSystem == nil && t.FixedRandomDelay == nil && t.OnClockChange == nil && t.OnTimezoneChange == nil
}

func systemdServiceEmpty(s *pb.SystemdServiceSettings) bool {
	if s == nil {
		return true
	}
	return s.GetRestart() == "" && s.GetRestartSec() == "" && s.GetTimeoutStartSec() == "" &&
		s.GetTimeoutStopSec() == "" && len(s.GetEnvironment()) == 0 && s.GetMemoryMax() == "" &&
		s.GetCpuQuota() == "" && s.GetTasksMax() == "" && s.GetNice() == "" && s.NoNewPrivileges == nil &&
		s.GetProtectSystem() == "" && s.GetProtectHome() == "" && s.PrivateTmp == nil
}

// ── rendering ───────────────────────────────────────────────────────────

func systemdYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// systemdQuote renders a value as a double-quoted unit file string.
func systemdQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// RenderSystemdDropIn renders the 90-bor.conf drop-in for a unit, or nil
// when the unit has no override. Trigger directives are reset before they
// are set so the Bor schedule replaces the vendor schedule instead of
// adding to it. Output is deterministic for a given desired state.
func RenderSystemdDropIn(u *SystemdUnitDesired) []byte {
	if u == nil || (u.Timer == nil && u.Service == nil) {
		return nil
	}
	var b bytes.Buffer
	b.WriteString(systemdDropInHeader + "\n")
	b.WriteString("# Unit: " + u.Name + "\n")
	if t := u.Timer; t != nil {
		b.WriteString("\n[Timer]\n")
		if len(t.GetOnCalendar()) > 0 {
			b.WriteString("OnCalendar=\n")
			for _, c := range t.GetOnCalendar() {
				b.WriteString("OnCalendar=" + c + "\n")
			}
		}
		for _, kv := range []struct{ k, v string }{
			{"OnBootSec", t.GetOnBootSec()},
			{"OnStartupSec", t.GetOnStartupSec()},
			{"OnActiveSec", t.GetOnActiveSec()},
			{"OnUnitActiveSec", t.GetOnUnitActiveSec()},
			{"OnUnitInactiveSec", t.GetOnUnitInactiveSec()},
		} {
			if kv.v != "" {
				b.WriteString(kv.k + "=\n" + kv.k + "=" + kv.v + "\n")
			}
		}
		if t.Persistent != nil {
			b.WriteString("Persistent=" + systemdYesNo(t.GetPersistent()) + "\n")
		}
		if v := t.GetRandomizedDelaySec(); v != "" {
			b.WriteString("RandomizedDelaySec=" + v + "\n")
		}
		if v := t.GetAccuracySec(); v != "" {
			b.WriteString("AccuracySec=" + v + "\n")
		}
		if t.WakeSystem != nil {
			b.WriteString("WakeSystem=" + systemdYesNo(t.GetWakeSystem()) + "\n")
		}
		if t.FixedRandomDelay != nil {
			b.WriteString("FixedRandomDelay=" + systemdYesNo(t.GetFixedRandomDelay()) + "\n")
		}
		if t.OnClockChange != nil {
			b.WriteString("OnClockChange=" + systemdYesNo(t.GetOnClockChange()) + "\n")
		}
		if t.OnTimezoneChange != nil {
			b.WriteString("OnTimezoneChange=" + systemdYesNo(t.GetOnTimezoneChange()) + "\n")
		}
	}
	if s := u.Service; s != nil {
		b.WriteString("\n[Service]\n")
		for _, kv := range []struct{ k, v string }{
			{"Restart", s.GetRestart()},
			{"RestartSec", s.GetRestartSec()},
			{"TimeoutStartSec", s.GetTimeoutStartSec()},
			{"TimeoutStopSec", s.GetTimeoutStopSec()},
		} {
			if kv.v != "" {
				b.WriteString(kv.k + "=" + kv.v + "\n")
			}
		}
		env := append([]string(nil), s.GetEnvironment()...)
		sort.Strings(env)
		for _, e := range env {
			b.WriteString("Environment=" + systemdQuote(e) + "\n")
		}
		for _, kv := range []struct{ k, v string }{
			{"MemoryMax", s.GetMemoryMax()},
			{"CPUQuota", s.GetCpuQuota()},
			{"TasksMax", s.GetTasksMax()},
			{"Nice", s.GetNice()},
		} {
			if kv.v != "" {
				b.WriteString(kv.k + "=" + kv.v + "\n")
			}
		}
		if s.NoNewPrivileges != nil {
			b.WriteString("NoNewPrivileges=" + systemdYesNo(s.GetNoNewPrivileges()) + "\n")
		}
		if v := s.GetProtectSystem(); v != "" {
			b.WriteString("ProtectSystem=" + v + "\n")
		}
		if v := s.GetProtectHome(); v != "" {
			b.WriteString("ProtectHome=" + v + "\n")
		}
		if s.PrivateTmp != nil {
			b.WriteString("PrivateTmp=" + systemdYesNo(s.GetPrivateTmp()) + "\n")
		}
	}
	return b.Bytes()
}

// SystemdDropInPath returns the path of Bor's drop-in for a unit.
func SystemdDropInPath(systemDir, unit string) string {
	if systemDir == "" {
		systemDir = SystemdSystemDir
	}
	return filepath.Join(systemDir, unit+".d", SystemdDropInName)
}

// ListBorManagedSystemdFiles returns the drop-ins under systemDir that
// carry Bor's header.
func ListBorManagedSystemdFiles(systemDir string) ([]string, error) {
	if systemDir == "" {
		systemDir = SystemdSystemDir
	}
	entries, err := os.ReadDir(systemDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", systemDir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".d") {
			continue
		}
		p := filepath.Join(systemDir, e.Name(), SystemdDropInName)
		if isBorSystemdDropIn(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func isBorSystemdDropIn(path string) bool {
	f, err := os.Open(path) //nolint:gosec // G304: path under the configured unit directory
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, len(systemdDropInHeader))
	n, _ := f.Read(buf)
	return string(buf[:n]) == systemdDropInHeader
}

// systemdUnitFromDropInPath recovers the unit name from
// <dir>/<unit>.d/90-bor.conf.
func systemdUnitFromDropInPath(path string) string {
	return strings.TrimSuffix(filepath.Base(filepath.Dir(path)), ".d")
}

// removeSystemdDropIn deletes the drop-in and its directory when empty.
func removeSystemdDropIn(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Dir(path)) // only succeeds when empty
	return nil
}

// ── sync ────────────────────────────────────────────────────────────────

// SystemdSyncOptions are the node-specific inputs of a sync.
type SystemdSyncOptions struct {
	SystemDir string
	StatePath string
	// Analyzer validates calendar expressions before a drop-in is written;
	// nil skips the check.
	Analyzer *systemd.Analyzer
}

func (o *SystemdSyncOptions) systemDir() string {
	if o.SystemDir == "" {
		return SystemdSystemDir
	}
	return o.SystemDir
}

func (o *SystemdSyncOptions) statePath() string {
	if o.StatePath == "" {
		return SystemdStatePath
	}
	return o.StatePath
}

// SystemdPolicyResult is one policy's compliance after a sync.
type SystemdPolicyResult struct {
	Status  pb.ComplianceStatus
	Message string
	Items   []*pb.ComplianceItemResult
}

// SystemdSyncResult is the outcome of a sync.
type SystemdSyncResult struct {
	// Policies maps policy id to its result.
	Policies map[string]SystemdPolicyResult
	// Changed reports whether the sync changed a file or a unit state, so
	// the caller can refresh the unit catalogue.
	Changed bool
}

// SystemdDesiredFiles returns the drop-in paths a desired state renders,
// for the caller to suppress the file watcher before writing.
func SystemdDesiredFiles(systemDir string, d *SystemdDesired) []string {
	var out []string
	for _, name := range d.Names() {
		if u := d.Units[name]; u.Timer != nil || u.Service != nil {
			out = append(out, SystemdDropInPath(systemDir, name))
		}
	}
	return out
}

// systemdUnitRun is the per-unit working state of one sync.
type systemdUnitRun struct {
	status        *systemd.UnitStatus
	notFound      bool
	queryErr      string
	dropInErr     string
	dropInChanged bool
	dropInWanted  bool
	errs          []string
	notes         []string
	startSkipped  bool
}

type systemdSyncer struct {
	ctx     context.Context
	mgr     systemd.Manager
	opts    *SystemdSyncOptions
	desired *SystemdDesired
	state   *SystemdState
	units   map[string]*systemdUnitRun
	changed bool
}

// SyncSystemd applies the merged desired state of the entries to the node
// and returns per-policy compliance. With no entries it removes every Bor
// drop-in and restores the recorded original states.
func SyncSystemd(ctx context.Context, mgr systemd.Manager, entries []SystemdEntry, opts SystemdSyncOptions) SystemdSyncResult {
	res := SystemdSyncResult{Policies: map[string]SystemdPolicyResult{}}
	desired := MergeSystemdPolicies(entries)

	fail := func(status pb.ComplianceStatus, msg string) SystemdSyncResult {
		for _, e := range entries {
			res.Policies[e.ID] = SystemdPolicyResult{Status: status, Message: msg}
		}
		return res
	}
	if mgr == nil {
		return fail(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "cannot reach systemd on the system bus")
	}
	if !mgr.Available() {
		return fail(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "systemd is not the init system on this node")
	}

	state, err := LoadSystemdState(opts.statePath())
	if err != nil {
		log.Printf("Warning: systemd: %v (starting with an empty record)", err)
	}
	s := &systemdSyncer{ctx: ctx, mgr: mgr, opts: &opts, desired: desired, state: state, units: map[string]*systemdUnitRun{}}
	for _, name := range desired.Names() {
		s.units[name] = &systemdUnitRun{}
	}

	if err := s.syncFiles(); err != nil {
		return fail(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error())
	}
	s.restoreReleased()
	s.applyEnablement()
	s.applyActivity()
	if err := SaveSystemdState(opts.statePath(), s.state); err != nil {
		log.Printf("Warning: systemd: %v", err)
	}
	s.refreshStatus()

	for _, e := range entries {
		res.Policies[e.ID] = s.compliance(e)
	}
	res.Changed = s.changed
	return res
}

// syncFiles writes, updates and removes drop-ins and reloads systemd when
// anything changed; a drop-in systemd rejects is rolled back.
func (s *systemdSyncer) syncFiles() error {
	dir := s.opts.systemDir()
	reload := false
	existing, err := ListBorManagedSystemdFiles(dir)
	if err != nil {
		return err
	}
	for _, name := range s.desired.Names() {
		u := s.desired.Units[name]
		run := s.units[name]
		path := SystemdDropInPath(dir, name)
		content := RenderSystemdDropIn(u)
		if content == nil {
			if _, err := os.Stat(path); err == nil && isBorSystemdDropIn(path) {
				if err := removeSystemdDropIn(path); err != nil {
					run.errs = append(run.errs, "remove drop-in: "+err.Error())
					continue
				}
				run.dropInChanged = true
				reload = true
			}
			continue
		}
		run.dropInWanted = true
		if u.Timer != nil && s.opts.Analyzer != nil && s.opts.Analyzer.Available() {
			for _, expr := range u.Timer.GetOnCalendar() {
				if err := s.opts.Analyzer.ValidateCalendar(s.ctx, expr); err != nil {
					run.dropInErr = err.Error()
					break
				}
			}
			if run.dropInErr != "" {
				if isBorSystemdDropIn(path) {
					_ = removeSystemdDropIn(path)
					reload = true
				}
				continue
			}
		}
		cur, _ := os.ReadFile(path) //nolint:gosec // G304: path built from a validated unit name under the configured directory
		if bytes.Equal(cur, content) {
			continue
		}
		if err := WriteFileAtomically(path, content); err != nil {
			run.errs = append(run.errs, "write drop-in: "+err.Error())
			continue
		}
		run.dropInChanged = true
		reload = true
	}
	for _, p := range existing {
		if _, wanted := s.desired.Units[systemdUnitFromDropInPath(p)]; wanted {
			continue
		}
		if err := removeSystemdDropIn(p); err != nil {
			log.Printf("Warning: systemd: remove stale drop-in %s: %v", p, err)
			continue
		}
		log.Printf("systemd: removed stale drop-in %s", p)
		reload = true
	}
	if !reload {
		return nil
	}
	s.changed = true
	if err := s.mgr.Reload(s.ctx); err != nil {
		return err
	}
	// A drop-in systemd cannot load must not stay on disk.
	rollback := false
	for _, name := range s.desired.Names() {
		run := s.units[name]
		if !run.dropInChanged || !run.dropInWanted {
			continue
		}
		st, err := s.mgr.Status(s.ctx, name)
		if err != nil {
			continue
		}
		if st.LoadState == "bad-setting" || st.LoadState == "error" {
			run.dropInErr = fmt.Sprintf("systemd rejected the drop-in (load state %s); it was removed", st.LoadState)
			_ = removeSystemdDropIn(SystemdDropInPath(dir, name))
			rollback = true
		}
	}
	if rollback {
		if err := s.mgr.Reload(s.ctx); err != nil {
			return err
		}
	}
	return nil
}

// restoreReleased returns units that left every policy to their recorded
// original state and drops their records.
func (s *systemdSyncer) restoreReleased() {
	if len(s.state.Units) == 0 {
		return
	}
	names := make([]string, 0, len(s.state.Units))
	for n := range s.state.Units {
		if _, still := s.desired.Units[n]; !still {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	reload := false
	for _, name := range names {
		rec := s.state.Units[name]
		if err := s.restoreUnit(name, rec, &reload); err != nil {
			log.Printf("Warning: systemd: restore %s: %v", name, err)
		} else {
			log.Printf("systemd: restored %s to its original state (%s, active=%v)", name, rec.UnitFileState, rec.Active)
		}
		delete(s.state.Units, name)
		s.changed = true
	}
	if reload {
		if err := s.mgr.Reload(s.ctx); err != nil {
			log.Printf("Warning: systemd: %v", err)
		}
	}
}

func (s *systemdSyncer) restoreUnit(name string, rec SystemdOriginal, reload *bool) error {
	if systemdunits.ValidateName(name) != nil || systemdunits.IsProtected(name) {
		return fmt.Errorf("refusing to touch %q", name)
	}
	st, err := s.mgr.Status(s.ctx, name)
	if err != nil {
		return err
	}
	if !st.Exists() {
		return nil
	}
	names := []string{name}
	wasMasked := rec.UnitFileState == "masked" || rec.UnitFileState == "masked-runtime"
	switch {
	case wasMasked && !st.IsMasked():
		if mErr := s.mgr.Mask(s.ctx, names); mErr != nil {
			return mErr
		}
		*reload = true
	case !wasMasked && st.IsMasked():
		if uErr := s.mgr.Unmask(s.ctx, names); uErr != nil {
			return uErr
		}
		*reload = true
		// The unit can only be enabled or started once systemd sees the
		// mask gone.
		if rErr := s.mgr.Reload(s.ctx); rErr != nil {
			return rErr
		}
		*reload = false
		if st, err = s.mgr.Status(s.ctx, name); err != nil {
			return err
		}
	}
	if !wasMasked {
		switch rec.UnitFileState {
		case "enabled":
			if st.UnitFileState != "enabled" && st.HasInstall() {
				if err := s.mgr.Enable(s.ctx, names); err != nil && !errors.Is(err, systemd.ErrNoInstall) {
					return err
				}
				*reload = true
			}
		case "disabled":
			if st.UnitFileState != "disabled" && st.HasInstall() {
				if err := s.mgr.Disable(s.ctx, names); err != nil {
					return err
				}
				*reload = true
			}
		}
	}
	if wasMasked {
		return nil
	}
	switch {
	case rec.Active && !st.IsActive() && !st.IsOneshot():
		if r, err := s.mgr.Start(s.ctx, name); err != nil {
			return err
		} else if !r.OK() {
			return fmt.Errorf("start: %s", r)
		}
	case !rec.Active && st.IsActive():
		if r, err := s.mgr.Stop(s.ctx, name); err != nil {
			return err
		} else if !r.OK() {
			return fmt.Errorf("stop: %s", r)
		}
	}
	return nil
}

// loadStatus queries a unit and records its original state on first touch.
func (s *systemdSyncer) loadStatus(name string) *systemd.UnitStatus {
	run := s.units[name]
	st, err := s.mgr.Status(s.ctx, name)
	if err != nil {
		run.queryErr = err.Error()
		return nil
	}
	run.status = st
	if !st.Exists() {
		run.notFound = true
		return nil
	}
	if _, ok := s.state.Units[name]; !ok {
		s.state.Units[name] = SystemdOriginal{UnitFileState: st.UnitFileState, Active: st.IsActive(), RecordedAt: time.Now().UTC()}
	}
	return st
}

// applyEnablement enables, disables or masks every unit as desired, then
// reloads once when anything changed.
func (s *systemdSyncer) applyEnablement() {
	reload := false
	for _, name := range s.desired.Names() {
		u := s.desired.Units[name]
		run := s.units[name]
		st := s.loadStatus(name)
		if st == nil || u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
			continue
		}
		names := []string{name}
		switch u.Enablement {
		case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED, pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
			if st.IsMasked() {
				if err := s.mgr.Unmask(s.ctx, names); err != nil {
					run.errs = append(run.errs, "unmask: "+err.Error())
					continue
				}
				reload = true
				run.notes = append(run.notes, "unmasked")
				// Enable and Disable need the unmasked file visible.
				if err := s.mgr.Reload(s.ctx); err != nil {
					run.errs = append(run.errs, err.Error())
					continue
				}
				if st = s.loadStatus(name); st == nil {
					continue
				}
			}
			switch st.UnitFileState {
			case "static", "generated", "transient", "":
				run.errs = append(run.errs, fmt.Sprintf("unit is %s and has no [Install] section; manage its activity instead", st.UnitFileState))
				continue
			}
			if u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED {
				if systemdEnabledState(st.UnitFileState) {
					continue
				}
				if err := s.mgr.Enable(s.ctx, names); err != nil {
					run.errs = append(run.errs, "enable: "+err.Error())
					continue
				}
				run.notes = append(run.notes, "enabled")
			} else {
				if st.UnitFileState == "disabled" {
					continue
				}
				if err := s.mgr.Disable(s.ctx, names); err != nil {
					run.errs = append(run.errs, "disable: "+err.Error())
					continue
				}
				run.notes = append(run.notes, "disabled")
			}
			reload = true
		case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
			if st.IsActive() {
				if r, err := s.mgr.Stop(s.ctx, name); err != nil {
					run.errs = append(run.errs, "stop: "+err.Error())
					continue
				} else if !r.OK() {
					run.errs = append(run.errs, "stop: job "+string(r))
					continue
				}
				run.notes = append(run.notes, "stopped")
				s.changed = true
			}
			if st.IsMasked() {
				continue
			}
			if err := s.mgr.Mask(s.ctx, names); err != nil {
				run.errs = append(run.errs, "mask: "+err.Error())
				continue
			}
			run.notes = append(run.notes, "masked")
			reload = true
		}
	}
	if reload {
		s.changed = true
		if err := s.mgr.Reload(s.ctx); err != nil {
			log.Printf("Warning: systemd: %v", err)
		}
	}
}

// systemdEnabledState reports whether a unit-file state counts as enabled.
func systemdEnabledState(state string) bool {
	switch state {
	case "enabled", "enabled-runtime", "alias", "indirect", "linked", "linked-runtime":
		return true
	}
	return false
}

// applyActivity starts, stops or restarts every unit as desired.
func (s *systemdSyncer) applyActivity() {
	for _, name := range s.desired.Names() {
		u := s.desired.Units[name]
		run := s.units[name]
		if run.notFound || run.queryErr != "" {
			continue
		}
		if run.status == nil || run.dropInChanged || len(run.notes) > 0 {
			if st := s.loadStatus(name); st == nil {
				continue
			}
		}
		st := run.status
		if st.IsMasked() && u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED {
			continue
		}
		typ := st.Type()
		restartWanted := run.dropInChanged && (typ == "timer" || u.RestartOnChange)
		switch u.Activity {
		case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
			switch {
			case st.IsOneshot():
				if st.Result == "success" && !st.ExecMainStart.IsZero() && !run.dropInChanged {
					continue
				}
				s.runJob(name, "start")
			case st.IsActive() && restartWanted:
				s.runJob(name, "restart")
			case st.IsActive():
				continue
			default:
				s.runJob(name, "start")
			}
		case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
			if st.IsActive() {
				s.runJob(name, "stop")
			}
		default:
			if st.IsActive() && restartWanted {
				s.runJob(name, "restart")
			}
		}
	}
}

func (s *systemdSyncer) runJob(name, verb string) {
	run := s.units[name]
	var (
		r   systemd.JobResult
		err error
	)
	switch verb {
	case "start":
		r, err = s.mgr.Start(s.ctx, name)
	case "stop":
		r, err = s.mgr.Stop(s.ctx, name)
	default:
		r, err = s.mgr.Restart(s.ctx, name)
	}
	s.changed = true
	if err != nil {
		run.errs = append(run.errs, verb+": "+err.Error())
		return
	}
	switch {
	case r == systemd.JobSkipped && verb == "start":
		run.startSkipped = true
		run.notes = append(run.notes, "start skipped: a condition of the unit is not met")
	case !r.OK():
		run.errs = append(run.errs, fmt.Sprintf("%s failed (job result %s)", verb, r))
	default:
		run.notes = append(run.notes, map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[verb])
	}
	log.Printf("systemd: %s %s: %s", verb, name, r)
}

// refreshStatus re-reads every unit after the actions for the reports.
func (s *systemdSyncer) refreshStatus() {
	for _, name := range s.desired.Names() {
		run := s.units[name]
		if run.notFound || run.queryErr != "" {
			continue
		}
		if st, err := s.mgr.Status(s.ctx, name); err == nil {
			run.status = st
		}
	}
}

// ── compliance ──────────────────────────────────────────────────────────

func systemdSourceLabel(src systemdSource) string {
	return fmt.Sprintf("%s (priority %d)", src.Name, src.Priority)
}

// compliance builds one policy's items against the merged state and the
// live unit states.
func (s *systemdSyncer) compliance(e SystemdEntry) SystemdPolicyResult {
	if e.Policy == nil {
		return SystemdPolicyResult{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "policy has no content"}
	}
	var items []*pb.ComplianceItemResult
	for _, u := range e.Policy.GetUnits() {
		items = append(items, s.unitItem(e, u))
	}
	status, msg := rollupSystemdItems(items)
	return SystemdPolicyResult{Status: status, Message: msg, Items: items}
}

func (s *systemdSyncer) unitItem(e SystemdEntry, u *pb.SystemdUnit) *pb.ComplianceItemResult {
	name := u.GetName()
	item := &pb.ComplianceItemResult{SchemaId: systemdItemSchema, Key: name}
	set := func(st pb.ComplianceStatus, msg string) *pb.ComplianceItemResult {
		item.Status = st
		item.Message = msg
		return item
	}
	if err := systemdunits.ValidateName(name); err != nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error())
	}
	if systemdunits.IsProtected(name) {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "protected unit; nothing applied")
	}
	d := s.desired.Units[name]
	run := s.units[name]
	if d == nil || run == nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "unit missing from the merged state")
	}
	if run.queryErr != "" {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, run.queryErr)
	}
	if run.notFound {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "unit is not installed on this node")
	}
	st := run.status
	if st == nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "unit state unknown")
	}

	// Which of this policy's axes won the merge.
	var overridden []string
	wins := 0
	checks := 0
	var problems []string

	if en := u.GetEnablement(); en != pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
		checks++
		if d.EnablementBy.ID != e.ID && d.Enablement != en {
			overridden = append(overridden, "enablement by "+systemdSourceLabel(d.EnablementBy))
		} else {
			wins++
			if !systemdEnablementSatisfied(en, st) {
				problems = append(problems, fmt.Sprintf("expected %s, unit file is %s", systemdEnablementWord(en), st.UnitFileState))
			}
		}
	}
	if ac := u.GetActivity(); ac != pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED {
		checks++
		if d.ActivityBy.ID != e.ID && d.Activity != ac {
			overridden = append(overridden, "activity by "+systemdSourceLabel(d.ActivityBy))
		} else {
			wins++
			if !run.startSkipped && !systemdActivitySatisfied(ac, st) {
				problems = append(problems, fmt.Sprintf("expected %s, unit is %s (%s)", systemdActivityWord(ac), st.ActiveState, st.SubState))
			}
		}
	}
	if t := u.GetDropIn().GetTimer(); !systemdTimerEmpty(t) {
		checks++
		if d.TimerBy.ID != e.ID && !proto.Equal(t, d.Timer) {
			overridden = append(overridden, "timer settings by "+systemdSourceLabel(d.TimerBy))
		} else {
			wins++
			if msg := s.dropInProblem(name, d); msg != "" {
				problems = append(problems, msg)
			}
		}
	}
	if sv := u.GetDropIn().GetService(); !systemdServiceEmpty(sv) {
		checks++
		if lost := systemdServiceOverridden(sv, d, e.ID); len(lost) > 0 {
			overridden = append(overridden, "service settings "+strings.Join(lost, ", "))
		} else {
			wins++
			if msg := s.dropInProblem(name, d); msg != "" {
				problems = append(problems, msg)
			}
		}
	}
	if u.GetRequireLastRunSuccess() {
		checks++
		wins++
		if msg := s.lastRunProblem(st); msg != "" {
			problems = append(problems, msg)
		}
	}

	summary := systemdStateSummary(st)
	if checks > 0 && wins == 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "overridden: "+strings.Join(overridden, "; ")+"; now "+summary)
	}
	var notes []string
	notes = append(notes, run.notes...)
	if len(overridden) > 0 {
		notes = append(notes, "overridden: "+strings.Join(overridden, "; "))
	}
	if run.dropInErr != "" && (d.TimerBy.ID == e.ID || len(d.ServiceBy) > 0) {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, run.dropInErr+"; "+summary)
	}
	if len(run.errs) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(run.errs, "; ")+"; "+summary)
	}
	if run.startSkipped && u.GetActivity() == pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING && !st.IsActive() {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "start skipped: a condition of the unit is not met; "+summary)
	}
	msg := summary
	if len(notes) > 0 {
		msg += "; " + strings.Join(notes, "; ")
	}
	if len(problems) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(problems, "; ")+"; "+msg)
	}
	return set(pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, msg)
}

func systemdEnablementWord(e pb.SystemdEnablement) string {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return "enabled"
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return "disabled"
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return "masked"
	}
	return "unchanged"
}

func systemdActivityWord(a pb.SystemdActivity) string {
	switch a {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		return "running"
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return "stopped"
	}
	return "unchanged"
}

func systemdEnablementSatisfied(e pb.SystemdEnablement, st *systemd.UnitStatus) bool {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return systemdEnabledState(st.UnitFileState)
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return st.UnitFileState == "disabled"
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return st.IsMasked()
	}
	return true
}

func systemdActivitySatisfied(a pb.SystemdActivity, st *systemd.UnitStatus) bool {
	switch a {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		if st.IsOneshot() {
			return st.IsActive() || (st.Result == "success" && !st.ExecMainStart.IsZero())
		}
		return st.IsActive()
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return !st.IsActive()
	}
	return true
}

// systemdServiceOverridden lists the directives of a policy's service
// settings that a higher-priority policy won with a different value.
func systemdServiceOverridden(sv *pb.SystemdServiceSettings, d *SystemdUnitDesired, policyID string) []string {
	if d.Service == nil {
		return nil
	}
	var lost []string
	check := func(key, mine, merged string) {
		if mine == "" {
			return
		}
		if src, ok := d.ServiceBy[key]; ok && src.ID != policyID && merged != mine {
			lost = append(lost, key+" by "+systemdSourceLabel(src))
		}
	}
	checkBool := func(key string, mine, merged *bool) {
		if mine == nil {
			return
		}
		if src, ok := d.ServiceBy[key]; ok && src.ID != policyID && (merged == nil || *merged != *mine) {
			lost = append(lost, key+" by "+systemdSourceLabel(src))
		}
	}
	m := d.Service
	check("Restart", sv.GetRestart(), m.GetRestart())
	check("RestartSec", sv.GetRestartSec(), m.GetRestartSec())
	check("TimeoutStartSec", sv.GetTimeoutStartSec(), m.GetTimeoutStartSec())
	check("TimeoutStopSec", sv.GetTimeoutStopSec(), m.GetTimeoutStopSec())
	check("MemoryMax", sv.GetMemoryMax(), m.GetMemoryMax())
	check("CPUQuota", sv.GetCpuQuota(), m.GetCpuQuota())
	check("TasksMax", sv.GetTasksMax(), m.GetTasksMax())
	check("Nice", sv.GetNice(), m.GetNice())
	check("ProtectSystem", sv.GetProtectSystem(), m.GetProtectSystem())
	check("ProtectHome", sv.GetProtectHome(), m.GetProtectHome())
	checkBool("NoNewPrivileges", sv.NoNewPrivileges, m.NoNewPrivileges)
	checkBool("PrivateTmp", sv.PrivateTmp, m.PrivateTmp)
	merged := map[string]string{}
	for _, env := range m.GetEnvironment() {
		k, v, _ := strings.Cut(env, "=")
		merged[k] = v
	}
	for _, env := range sv.GetEnvironment() {
		k, v, _ := strings.Cut(env, "=")
		if src, ok := d.ServiceBy["Environment:"+k]; ok && src.ID != policyID && merged[k] != v {
			lost = append(lost, "Environment "+k+" by "+systemdSourceLabel(src))
		}
	}
	return lost
}

// dropInProblem reports a drop-in that is missing or differs from the
// rendered content.
func (s *systemdSyncer) dropInProblem(name string, d *SystemdUnitDesired) string {
	run := s.units[name]
	if run.dropInErr != "" {
		return ""
	}
	want := RenderSystemdDropIn(d)
	if want == nil {
		return ""
	}
	path := SystemdDropInPath(s.opts.systemDir(), name)
	cur, err := os.ReadFile(path) //nolint:gosec // G304: path built from a validated unit name under the configured directory
	if err != nil {
		return "drop-in " + path + " is missing"
	}
	if !bytes.Equal(cur, want) {
		return "drop-in " + path + " differs from the policy"
	}
	if run.status != nil && run.status.NeedDaemonReload {
		return "systemd has not reloaded the drop-in yet"
	}
	return ""
}

// lastRunProblem checks the last result of a timer's service or of a
// service.
func (s *systemdSyncer) lastRunProblem(st *systemd.UnitStatus) string {
	switch st.Type() {
	case "timer":
		if st.LastTrigger.IsZero() {
			return ""
		}
		target := st.TimerUnit
		if target == "" {
			target = strings.TrimSuffix(st.Name, ".timer") + ".service"
		}
		ts, err := s.mgr.Status(s.ctx, target)
		if err != nil || !ts.Exists() {
			return ""
		}
		if ts.Result != "success" {
			return fmt.Sprintf("last run of %s failed (%s, exit status %d)", target, ts.Result, ts.ExecMainStatus)
		}
	case "service":
		if st.Result != "success" && st.Result != "" {
			return fmt.Sprintf("last run failed (%s, exit status %d)", st.Result, st.ExecMainStatus)
		}
	}
	return ""
}

// systemdStateSummary renders "enabled, active (running)" plus timer
// details for the compliance message.
func systemdStateSummary(st *systemd.UnitStatus) string {
	parts := []string{st.UnitFileState}
	if parts[0] == "" {
		parts[0] = st.LoadState
	}
	parts = append(parts, fmt.Sprintf("%s (%s)", st.ActiveState, st.SubState))
	if st.Type() == "timer" {
		if !st.NextElapse.IsZero() {
			parts = append(parts, "next "+st.NextElapse.Local().Format("Mon 2006-01-02 15:04"))
		}
		if !st.LastTrigger.IsZero() {
			last := "last " + st.LastTrigger.Local().Format("Mon 2006-01-02 15:04")
			if st.TimerResult != "" {
				last += " (" + st.TimerResult + ")"
			}
			parts = append(parts, last)
		}
	}
	return strings.Join(parts, ", ")
}

// rollupSystemdItems derives a policy status from its items: any error ->
// ERROR, any non-compliant -> NON_COMPLIANT, all inapplicable ->
// INAPPLICABLE, otherwise COMPLIANT.
func rollupSystemdItems(items []*pb.ComplianceItemResult) (status pb.ComplianceStatus, message string) {
	if len(items) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "policy lists no units"
	}
	allInapplicable := true
	var msgs []string
	var hasError, hasNonCompliant bool
	for _, it := range items {
		switch it.GetStatus() {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			allInapplicable = false
			hasError = true
			msgs = append(msgs, it.GetKey()+": "+it.GetMessage())
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			allInapplicable = false
			hasNonCompliant = true
			msgs = append(msgs, it.GetKey()+": "+it.GetMessage())
		default:
			allInapplicable = false
		}
	}
	switch {
	case allInapplicable:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no unit applies on this node (not installed or overridden)"
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"
	}
}
