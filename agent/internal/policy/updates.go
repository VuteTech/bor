// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VuteTech/Bor/agent/internal/logind"
	"github.com/VuteTech/Bor/agent/internal/systemd"
	"github.com/VuteTech/Bor/agent/internal/updates"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compliance item schema ids of the Software updates type.
const (
	updatesItemEngine   = "updates:engine"
	updatesItemMode     = "updates:mode"
	updatesItemSchedule = "updates:schedule"
	updatesItemReboot   = "updates:reboot"
	updatesItemHold     = "updates:hold"
	updatesItemLastRun  = "updates:last-run"
)

// UpdatesEntry is one bound Software updates policy with its binding
// priority.
type UpdatesEntry struct {
	ID       string
	Name     string
	Priority int32
	Policy   *pb.UpdatesPolicy
}

// UpdatesDesired is the merged state of every bound policy.
type UpdatesDesired struct {
	Mode   pb.UpdatesMode
	ModeBy macSource

	Schedule         *pb.UpdatesSchedule
	ScheduleBy       macSource
	ScheduleConflict *macSource

	// The reboot rule comes from one policy as a whole.
	Reboot   *pb.UpdatesReboot
	RebootBy macSource

	// Holds maps a pattern to the first policy that lists it (union).
	Holds        map[string]macSource
	RemoveUnused bool
	// InstallEngine is true unless a policy switches it off.
	InstallEngine bool
	RequireEngine bool
}

func updatesModeRank(m pb.UpdatesMode) int {
	switch m {
	case pb.UpdatesMode_UPDATES_MODE_ALL:
		return 4
	case pb.UpdatesMode_UPDATES_MODE_SECURITY:
		return 3
	case pb.UpdatesMode_UPDATES_MODE_CHECK:
		return 2
	case pb.UpdatesMode_UPDATES_MODE_OFF:
		return 1
	}
	return 0
}

func updatesRebootRank(r pb.UpdatesRebootPolicy) int {
	switch r {
	case pb.UpdatesRebootPolicy_UPDATES_REBOOT_ALWAYS:
		return 3
	case pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED:
		return 2
	case pb.UpdatesRebootPolicy_UPDATES_REBOOT_NEVER:
		return 1
	}
	return 0
}

// updatesModeWord maps the proto mode to the engine driver's word.
func updatesModeWord(m pb.UpdatesMode) updates.Mode {
	switch m {
	case pb.UpdatesMode_UPDATES_MODE_OFF:
		return updates.ModeOff
	case pb.UpdatesMode_UPDATES_MODE_CHECK:
		return updates.ModeCheck
	case pb.UpdatesMode_UPDATES_MODE_SECURITY:
		return updates.ModeSecurity
	case pb.UpdatesMode_UPDATES_MODE_ALL:
		return updates.ModeAll
	}
	return updates.ModeKeep
}

func sortUpdatesEntries(entries []UpdatesEntry) []UpdatesEntry {
	sorted := append([]UpdatesEntry(nil), entries...)
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

// MergeUpdatesPolicies merges bound policies by binding priority: the
// highest priority wins every key; on equal priority the mode that
// installs more and the reboot rule that reboots more win, holds are a
// union, and two schedules are a conflict.
func MergeUpdatesPolicies(entries []UpdatesEntry) *UpdatesDesired {
	d := &UpdatesDesired{Holds: map[string]macSource{}, InstallEngine: true}
	for _, e := range sortUpdatesEntries(entries) {
		p := e.Policy
		if p == nil {
			continue
		}
		src := macSource{ID: e.ID, Name: e.Name, Priority: e.Priority}
		if m := p.GetMode(); m != pb.UpdatesMode_UPDATES_MODE_UNSPECIFIED {
			if d.Mode == pb.UpdatesMode_UPDATES_MODE_UNSPECIFIED || (d.ModeBy.Priority == e.Priority && updatesModeRank(m) > updatesModeRank(d.Mode)) {
				d.Mode, d.ModeBy = m, src
			}
		}
		if s := p.GetSchedule(); s != nil && s.GetOnCalendar() != "" {
			switch {
			case d.Schedule == nil:
				d.Schedule, d.ScheduleBy = s, src
			case d.ScheduleBy.Priority == e.Priority && d.Schedule.GetOnCalendar() != s.GetOnCalendar() && d.ScheduleConflict == nil:
				c := src
				d.ScheduleConflict = &c
			}
		}
		if r := p.GetReboot(); r != nil && r.GetPolicy() != pb.UpdatesRebootPolicy_UPDATES_REBOOT_UNSPECIFIED {
			if d.Reboot == nil || (d.RebootBy.Priority == e.Priority && updatesRebootRank(r.GetPolicy()) > updatesRebootRank(d.Reboot.GetPolicy())) {
				d.Reboot, d.RebootBy = r, src
			}
		}
		for _, h := range p.GetHold() {
			if _, ok := d.Holds[h]; !ok {
				d.Holds[h] = src
			}
		}
		if p.GetRemoveUnusedDependencies() {
			d.RemoveUnused = true
		}
		if p.InstallEngine != nil && !p.GetInstallEngine() {
			d.InstallEngine = false
		}
		if p.GetRequireEngine() {
			d.RequireEngine = true
		}
	}
	return d
}

// SortedHolds returns the holds in order.
func (d *UpdatesDesired) SortedHolds() []string {
	return sortedKeys(d.Holds)
}

// UpdatesDeps are the node facilities a sync uses.
type UpdatesDeps struct {
	Platform *updates.Platform
	Run      updates.Runner
	// Systemd drives the engine timer; nil when systemd is unreachable.
	Systemd systemd.Manager
	// Logind lists logged-in users for the reboot rule; nil means unknown.
	Logind logind.Manager
	// InstallPackage installs the engine package; nil when PackageKit is
	// unavailable.
	InstallPackage func(ctx context.Context, name string) error
	// Notify warns one logged-in user; nil disables warnings.
	Notify func(s updates.UserSession, summary, body string) error
	// Reboot performs the reboot; nil records the decision only.
	Reboot func(ctx context.Context) error
	// Now is the clock (tests).
	Now func() time.Time
}

func (d *UpdatesDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// UpdatesSyncOptions are the agent settings a sync honours.
type UpdatesSyncOptions struct {
	StatePath    string
	BackupDir    string
	SystemDir    string
	AllowInstall bool
	AllowReboot  bool
	// Reconcile marks a pass that re-asserts an unchanged desired state;
	// its corrections are drift.
	Reconcile bool
}

// UpdatesPolicyResult is the compliance of one policy after a sync.
type UpdatesPolicyResult struct {
	Status  pb.ComplianceStatus
	Message string
	Items   []*pb.ComplianceItemResult
}

// UpdatesSyncResult is the outcome of a sync.
type UpdatesSyncResult struct {
	Policies map[string]UpdatesPolicyResult
	Drifts   []MacDrift
	Changed  bool
}

type updatesSyncer struct {
	ctx     context.Context
	deps    *UpdatesDeps
	opts    UpdatesSyncOptions
	desired *UpdatesDesired
	state   *UpdatesState
	result  *UpdatesSyncResult
	p       *updates.Platform

	installErr   string
	configErr    string
	timerErr     string
	dropInErr    string
	lockErr      map[string]string
	timerEnabled map[string]bool
	effective    updates.Mode
	lastRun      *updates.Run
}

func (s *updatesSyncer) drift(resource, description string) {
	if s.opts.Reconcile {
		s.result.Drifts = append(s.result.Drifts, MacDrift{Resource: resource, Description: description})
	}
}

func (s *updatesSyncer) changed() { s.result.Changed = true }

// SyncUpdates applies the merged policies to the node's engine, restores
// what left every policy, and computes per-policy compliance.
func SyncUpdates(ctx context.Context, deps *UpdatesDeps, entries []UpdatesEntry, opts UpdatesSyncOptions) UpdatesSyncResult {
	result := UpdatesSyncResult{Policies: make(map[string]UpdatesPolicyResult, len(entries))}
	state, err := LoadUpdatesState(opts.StatePath)
	if err != nil {
		state = newUpdatesState()
	}
	if deps.Run == nil {
		deps.Run = updates.DefaultRunner
	}
	s := &updatesSyncer{ctx: ctx, deps: deps, opts: opts, desired: MergeUpdatesPolicies(entries), state: state, result: &result,
		p: deps.Platform, lockErr: map[string]string{}, timerEnabled: map[string]bool{}}
	if s.p == nil {
		s.p = &updates.Platform{Engine: updates.EngineNone}
	}
	if len(entries) == 0 {
		s.restoreAll()
		_ = SaveUpdatesState(opts.StatePath, state)
		return result
	}
	if s.p.Engine == updates.EngineNone {
		for _, e := range entries {
			result.Policies[e.ID] = s.noEngine(e)
		}
		_ = SaveUpdatesState(opts.StatePath, state)
		return result
	}
	if state.Engine != "" && state.Engine != string(s.p.Engine) {
		// The engine changed under us (an image switch); forget the record.
		s.restoreAll()
	}
	state.Engine = string(s.p.Engine)
	s.ensureEngine()
	s.applyConfig()
	s.applyTimers()
	s.applyDropIn()
	s.applyLocks()
	s.lastRun = s.readLastRun()
	_ = SaveUpdatesState(opts.StatePath, state)
	for _, e := range entries {
		result.Policies[e.ID] = s.compliance(e)
	}
	return result
}

// noEngine is the result on a node without a known package manager.
func (s *updatesSyncer) noEngine(e UpdatesEntry) UpdatesPolicyResult {
	st := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
	msg := "no supported update engine on this node (dnf, apt, zypper or rpm-ostree)"
	if e.Policy.GetRequireEngine() {
		st = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
	}
	items := []*pb.ComplianceItemResult{macItem(updatesItemEngine, "engine", st, msg)}
	status, message := rollupMacItems(items, msg, msg)
	return UpdatesPolicyResult{Status: status, Message: message, Items: items}
}

// ensureEngine installs the engine package when a policy asks and it is
// missing.
func (s *updatesSyncer) ensureEngine() {
	if s.p.Installed || s.p.Package == "" {
		return
	}
	if !s.desired.InstallEngine {
		return
	}
	if !s.opts.AllowInstall {
		s.installErr = "installing the engine is disabled in the agent configuration (updates.allow_install)"
		return
	}
	if s.deps.InstallPackage == nil {
		s.installErr = "PackageKit is not available to install " + s.p.Package
		return
	}
	if err := s.deps.InstallPackage(s.ctx, s.p.Package); err != nil {
		s.installErr = "installing " + s.p.Package + ": " + err.Error()
		return
	}
	s.p.Installed = true
	s.changed()
}

func (s *updatesSyncer) mode() updates.Mode { return updatesModeWord(s.desired.Mode) }

// backupPath is where the original engine configuration is kept.
func (s *updatesSyncer) backupPath() string {
	return filepath.Join(s.opts.BackupDir, filepath.Base(s.p.ConfigPath))
}

// writeOwnedFile writes a file Bor owns, backing the original up once
// when another party wrote it.
func (s *updatesSyncer) writeOwnedFile(path string, data []byte) error {
	existing, err := os.ReadFile(path) //nolint:gosec // G304: engine configuration path
	switch {
	case err == nil && !updates.HasHeader(existing) && !s.state.ConfigWritten:
		if mkErr := os.MkdirAll(s.opts.BackupDir, 0o700); mkErr != nil {
			return mkErr
		}
		if cpErr := os.WriteFile(s.backupPath(), existing, 0o600); cpErr != nil { //nolint:gosec // G703: the backup path is BackupDir joined with the base name of the engine's configuration file
			return cpErr
		}
		s.state.ConfigBackup = s.backupPath()
		s.state.ConfigExisted = true
	case errors.Is(err, os.ErrNotExist) && !s.state.ConfigWritten:
		s.state.ConfigExisted = false
	}
	if err == nil && bytes.Equal(existing, data) {
		s.state.ConfigWritten = true
		return nil
	}
	if err == nil && updates.HasHeader(existing) && s.state.ConfigWritten {
		s.drift("updates:config:"+filepath.Base(path), "configuration differed from the policy; rewritten")
	}
	if werr := os.MkdirAll(filepath.Dir(path), 0o755); werr != nil { //nolint:gosec // G301: configuration directories are world-readable
		return werr
	}
	if werr := WriteFileAtomically(path, data); werr != nil {
		return werr
	}
	s.state.ConfigWritten = true
	s.changed()
	return nil
}

// restoreOwnedFile puts the original configuration back, or removes the
// file Bor created.
func (s *updatesSyncer) restoreOwnedFile(path string) {
	if !s.state.ConfigWritten {
		return
	}
	switch {
	case s.state.ConfigBackup != "":
		if data, err := os.ReadFile(s.state.ConfigBackup); err == nil { //nolint:gosec // G304: path recorded by Bor
			_ = WriteFileAtomically(path, data)
			_ = os.Remove(s.state.ConfigBackup)
		}
	default:
		if data, err := os.ReadFile(path); err == nil && updates.HasHeader(data) { //nolint:gosec // G304: engine configuration path
			_ = os.Remove(path)
		}
	}
	s.state.ConfigBackup, s.state.ConfigExisted, s.state.ConfigWritten = "", false, false
	s.changed()
}

// applyConfig renders the engine configuration for the desired mode.
func (s *updatesSyncer) applyConfig() {
	mode := s.mode()
	holds := s.desired.SortedHolds()
	switch s.p.Engine {
	case updates.EngineDnf:
		switch mode {
		case updates.ModeCheck, updates.ModeSecurity, updates.ModeAll:
			if err := s.writeOwnedFile(s.p.Path(s.p.ConfigPath), updates.RenderDnfAutomaticConf(mode, holds)); err != nil {
				s.configErr = err.Error()
			}
			s.effective = mode
		default:
			s.restoreOwnedFile(s.p.Path(s.p.ConfigPath))
			if mode == updates.ModeOff {
				s.effective = updates.ModeOff
			}
		}
	case updates.EngineApt:
		if mode == updates.ModeKeep {
			s.restoreOwnedFile(s.p.Path(s.p.ConfigPath))
			return
		}
		if err := s.writeOwnedFile(s.p.Path(s.p.ConfigPath), updates.RenderAptConf(mode, s.p.OSID, holds, s.desired.RemoveUnused)); err != nil {
			s.configErr = err.Error()
		}
		s.effective = mode
	case updates.EngineRpmOstree:
		if mode == updates.ModeKeep {
			s.restoreOwnedFile(s.p.Path(s.p.ConfigPath))
			return
		}
		path := s.p.Path(s.p.ConfigPath)
		existing, _ := os.ReadFile(path) //nolint:gosec // G304: engine configuration path
		want := updates.RpmOstreePolicyForMode(mode)
		if updates.RpmOstreePolicy(existing) == want && s.state.ConfigWritten {
			s.effective = s.rpmOstreeEffective(want)
			return
		}
		data := updates.SetRpmOstreePolicy(existing, want)
		if !updates.HasHeader(data) {
			data = append([]byte("# "+updates.Header+"\n"), data...)
		}
		if err := s.writeOwnedFile(path, data); err != nil {
			s.configErr = err.Error()
			return
		}
		if _, err := s.deps.Run(s.ctx, s.p.RpmOstreeBin, "reload"); err != nil {
			s.configErr = "rpm-ostree reload: " + err.Error()
		}
		s.effective = s.rpmOstreeEffective(want)
	case updates.EngineOsUpdate:
		switch mode {
		case updates.ModeSecurity, updates.ModeAll:
			if err := s.writeOwnedFile(s.p.Path(s.p.ConfigPath), updates.RenderOsUpdateConf(mode)); err != nil {
				s.configErr = err.Error()
			}
			s.effective = mode
		default:
			s.restoreOwnedFile(s.p.Path(s.p.ConfigPath))
			if mode == updates.ModeOff || mode == updates.ModeCheck {
				s.effective = updates.ModeOff
			}
		}
	}
}

func (s *updatesSyncer) rpmOstreeEffective(policy string) updates.Mode {
	switch policy {
	case "check":
		return updates.ModeCheck
	case "stage", "apply":
		return updates.ModeAll
	}
	return updates.ModeOff
}

// timersWanted says which timers run for the mode.
func (s *updatesSyncer) timersWanted() (timers []string, enabled bool, reason string) {
	mode := s.mode()
	if mode == updates.ModeKeep {
		return nil, false, ""
	}
	timers = []string{s.p.TimerUnit}
	switch mode {
	case updates.ModeOff:
		return timers, false, ""
	case updates.ModeCheck:
		if s.p.Engine == updates.EngineOsUpdate {
			return timers, false, "os-update has no check-only mode; the timer stays off and Bor reports the pending updates"
		}
	case updates.ModeSecurity:
		if s.p.Engine == updates.EngineRpmOstree {
			return timers, true, "image systems install every update or none; the node checks for updates and reports them"
		}
	}
	return timers, true, ""
}

// applyTimers enables or disables the engine timer for the mode.
func (s *updatesSyncer) applyTimers() {
	timers, want, _ := s.timersWanted()
	if len(timers) == 0 || !s.p.Installed {
		return
	}
	if s.deps.Systemd == nil || !s.deps.Systemd.Available() {
		s.timerErr = "systemd is not reachable; the timer state cannot be set"
		return
	}
	for _, t := range timers {
		st, err := s.deps.Systemd.Status(s.ctx, t)
		if err != nil || st == nil || !st.Exists() {
			s.timerErr = t + " is not installed"
			continue
		}
		cur := systemd.IsEnabledState(st.UnitFileState)
		if _, recorded := s.state.Timers[t]; !recorded {
			s.state.Timers[t] = cur
		}
		if cur == want {
			s.timerEnabled[t] = cur
			continue
		}
		s.drift("updates:timer:"+t, fmt.Sprintf("found %s, expected %s", enabledWord(cur), enabledWord(want)))
		if want {
			err = s.deps.Systemd.Enable(s.ctx, []string{t})
			if err == nil {
				_, err = s.deps.Systemd.Start(s.ctx, t)
			}
		} else {
			err = s.deps.Systemd.Disable(s.ctx, []string{t})
			if err == nil {
				_, err = s.deps.Systemd.Stop(s.ctx, t)
			}
		}
		if err != nil {
			s.timerErr = t + ": " + err.Error()
			continue
		}
		s.timerEnabled[t] = want
		s.changed()
	}
}

// applyDropIn writes the schedule drop-in of the engine timer.
func (s *updatesSyncer) applyDropIn() {
	want := s.desired.Schedule
	path := updates.TimerDropInPath(s.opts.SystemDir, s.p.TimerUnit)
	mode := s.mode()
	if want == nil || want.GetOnCalendar() == "" || mode == updates.ModeKeep || mode == updates.ModeOff {
		s.removeDropIn(path)
		return
	}
	data := updates.RenderTimerDropIn(want.GetOnCalendar(), want.GetRandomizedDelay(), want.Persistent)
	existing, err := os.ReadFile(path) //nolint:gosec // G304: path built from the agent configuration
	if err == nil && bytes.Equal(existing, data) {
		return
	}
	if err == nil && updates.HasHeader(existing) {
		s.drift("updates:schedule", "timer drop-in differed from the policy; rewritten")
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil { //nolint:gosec // G301: unit directories are world-readable
		s.dropInErr = mkErr.Error()
		return
	}
	if werr := WriteFileAtomically(path, data); werr != nil {
		s.dropInErr = werr.Error()
		return
	}
	if !containsString(s.state.DropIns, path) {
		s.state.DropIns = append(s.state.DropIns, path)
	}
	s.changed()
	if s.deps.Systemd != nil && s.deps.Systemd.Available() {
		if rerr := s.deps.Systemd.Reload(s.ctx); rerr != nil {
			s.dropInErr = "daemon-reload: " + rerr.Error()
		}
	}
}

func (s *updatesSyncer) removeDropIn(path string) {
	if !containsString(s.state.DropIns, path) {
		return
	}
	if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(filepath.Dir(path))
		s.state.DropIns = removeString(s.state.DropIns, path)
		s.changed()
		if s.deps.Systemd != nil && s.deps.Systemd.Available() {
			_ = s.deps.Systemd.Reload(s.ctx)
		}
	}
}

// applyLocks keeps zypper's package locks in step with the holds (dnf and
// apt carry holds in their configuration files).
func (s *updatesSyncer) applyLocks() {
	if s.p.Engine != updates.EngineOsUpdate {
		return
	}
	holds := s.desired.SortedHolds()
	if s.mode() == updates.ModeKeep {
		holds = nil
	}
	for _, h := range holds {
		if containsString(s.state.Locks, h) {
			continue
		}
		if _, err := s.deps.Run(s.ctx, s.p.ZypperBin, "--non-interactive", "-q", "addlock", h); err != nil {
			s.lockErr[h] = err.Error()
			continue
		}
		s.state.Locks = append(s.state.Locks, h)
		s.changed()
	}
	for _, l := range append([]string(nil), s.state.Locks...) {
		if containsString(holds, l) {
			continue
		}
		if _, err := s.deps.Run(s.ctx, s.p.ZypperBin, "--non-interactive", "-q", "removelock", l); err == nil {
			s.state.Locks = removeString(s.state.Locks, l)
			s.changed()
		}
	}
}

// readLastRun finds the engine's last run where that is cheap.
func (s *updatesSyncer) readLastRun() *updates.Run {
	switch s.p.Engine {
	case updates.EngineApt:
		return updates.AptLastRun(s.p.Path(updates.AptLogPath))
	case updates.EngineDnf:
		if !s.p.Installed || s.p.DnfBin == "" {
			return nil
		}
		return updates.DnfLastRun(s.ctx, s.deps.Run, s.p)
	}
	return nil
}

// restoreAll undoes everything the state records.
func (s *updatesSyncer) restoreAll() {
	st := s.state
	if s.p.ConfigPath != "" {
		s.restoreOwnedFile(s.p.Path(s.p.ConfigPath))
		if s.p.Engine == updates.EngineRpmOstree && s.p.RpmOstreeBin != "" {
			_, _ = s.deps.Run(s.ctx, s.p.RpmOstreeBin, "reload")
		}
	}
	for _, path := range append([]string(nil), st.DropIns...) {
		s.removeDropIn(path)
	}
	if s.deps.Systemd != nil && s.deps.Systemd.Available() {
		for t, wasEnabled := range st.Timers {
			cur, err := s.deps.Systemd.Status(s.ctx, t)
			if err == nil && cur != nil && cur.Exists() && systemd.IsEnabledState(cur.UnitFileState) != wasEnabled {
				if wasEnabled {
					_ = s.deps.Systemd.Enable(s.ctx, []string{t})
					_, _ = s.deps.Systemd.Start(s.ctx, t)
				} else {
					_ = s.deps.Systemd.Disable(s.ctx, []string{t})
					_, _ = s.deps.Systemd.Stop(s.ctx, t)
				}
				s.changed()
			}
			delete(st.Timers, t)
		}
	}
	if s.p.ZypperBin != "" {
		for _, l := range append([]string(nil), st.Locks...) {
			_, _ = s.deps.Run(s.ctx, s.p.ZypperBin, "--non-interactive", "-q", "removelock", l)
		}
	}
	st.Locks = nil
	st.Engine = ""
	st.RebootWarnedAt = nil
}

// compliance builds the items of one policy.
func (s *updatesSyncer) compliance(e UpdatesEntry) UpdatesPolicyResult {
	p := e.Policy
	if p == nil {
		return UpdatesPolicyResult{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "policy has no content"}
	}
	var items []*pb.ComplianceItemResult
	ok := pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
	bad := pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
	errSt := pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
	na := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
	engine := string(s.p.Engine)

	// Engine.
	switch {
	case s.p.Installed && s.timerErr == "":
		items = append(items, macItem(updatesItemEngine, engine, ok, engine+" is installed"))
	case s.p.Installed:
		items = append(items, macItem(updatesItemEngine, engine, errSt, s.timerErr))
	case s.installErr != "":
		items = append(items, macItem(updatesItemEngine, engine, errSt, s.installErr))
	case !s.desired.InstallEngine && p.GetRequireEngine():
		items = append(items, macItem(updatesItemEngine, engine, bad, s.p.Package+" is not installed and the policy does not install it"))
	default:
		items = append(items, macItem(updatesItemEngine, engine, na, s.p.Package+" is not installed; the policy does not install it"))
	}

	// Mode.
	if m := p.GetMode(); m != pb.UpdatesMode_UPDATES_MODE_UNSPECIFIED {
		word := string(updatesModeWord(m))
		_, _, note := s.timersWanted()
		switch {
		case s.desired.ModeBy.ID != e.ID && s.desired.Mode != m:
			items = append(items, macItem(updatesItemMode, "mode", na, "overridden by policy "+s.desired.ModeBy.label()))
		case !s.p.Installed:
			items = append(items, macItem(updatesItemMode, "mode", na, "the engine is not installed"))
		case s.configErr != "":
			items = append(items, macItem(updatesItemMode, "mode", errSt, s.configErr))
		case note != "":
			items = append(items, macItem(updatesItemMode, "mode", na, note))
		case s.timerErr != "":
			items = append(items, macItem(updatesItemMode, "mode", errSt, s.timerErr))
		default:
			items = append(items, macItem(updatesItemMode, "mode", ok, word))
		}
	}

	// Schedule.
	if sch := p.GetSchedule(); sch != nil && sch.GetOnCalendar() != "" {
		switch {
		case s.desired.ScheduleConflict != nil && (s.desired.ScheduleConflict.ID == e.ID || s.desired.ScheduleBy.ID == e.ID):
			other := s.desired.ScheduleBy
			if other.ID == e.ID {
				other = *s.desired.ScheduleConflict
			}
			items = append(items, macItem(updatesItemSchedule, "schedule", errSt, conflictMessage(macSource{ID: e.ID, Name: e.Name, Priority: e.Priority}, other)))
		case s.desired.ScheduleBy.ID != e.ID:
			items = append(items, macItem(updatesItemSchedule, "schedule", na, "overridden by policy "+s.desired.ScheduleBy.label()))
		case s.mode() == updates.ModeKeep || s.mode() == updates.ModeOff:
			items = append(items, macItem(updatesItemSchedule, "schedule", na, "no engine run is scheduled in this mode"))
		case !s.p.Installed:
			items = append(items, macItem(updatesItemSchedule, "schedule", na, "the engine is not installed"))
		case s.dropInErr != "":
			items = append(items, macItem(updatesItemSchedule, "schedule", errSt, s.dropInErr))
		default:
			items = append(items, macItem(updatesItemSchedule, "schedule", ok, sch.GetOnCalendar()))
		}
	}

	// Reboot rule.
	if r := p.GetReboot(); r != nil && r.GetPolicy() != pb.UpdatesRebootPolicy_UPDATES_REBOOT_UNSPECIFIED {
		word := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(r.GetPolicy().String(), "UPDATES_REBOOT_"), "_", " "))
		switch {
		case s.desired.RebootBy.ID != e.ID:
			items = append(items, macItem(updatesItemReboot, "reboot", na, "overridden by policy "+s.desired.RebootBy.label()))
		case !s.opts.AllowReboot && r.GetPolicy() != pb.UpdatesRebootPolicy_UPDATES_REBOOT_NEVER:
			items = append(items, macItem(updatesItemReboot, "reboot", na, "policy reboots are disabled in the agent configuration (updates.allow_reboot)"))
		default:
			msg := word
			if w := r.GetWindow(); w != "" {
				msg += ", window " + w
			}
			if s.state.RebootPendingSince != nil {
				msg += "; reboot pending since " + s.state.RebootPendingSince.Format("2006-01-02 15:04")
			}
			items = append(items, macItem(updatesItemReboot, "reboot", ok, msg))
		}
	}

	// Holds.
	for _, h := range p.GetHold() {
		switch {
		case s.mode() == updates.ModeKeep:
			items = append(items, macItem(updatesItemHold, h, na, "holds apply with a mode"))
		case !s.p.Installed:
			items = append(items, macItem(updatesItemHold, h, na, "the engine is not installed"))
		case s.p.Engine == updates.EngineRpmOstree:
			items = append(items, macItem(updatesItemHold, h, na, "image systems update as a whole; a hold cannot apply"))
		case s.lockErr[h] != "":
			items = append(items, macItem(updatesItemHold, h, errSt, s.lockErr[h]))
		case s.configErr != "":
			items = append(items, macItem(updatesItemHold, h, errSt, s.configErr))
		default:
			items = append(items, macItem(updatesItemHold, h, ok, "held back"))
		}
	}

	// Last run.
	if s.lastRun != nil && s.p.Installed {
		when := s.lastRun.Time.Format("2006-01-02 15:04")
		if s.lastRun.Success {
			items = append(items, macItem(updatesItemLastRun, "run", ok, fmt.Sprintf("%s: %s", when, s.lastRun.Message)))
		} else {
			items = append(items, macItem(updatesItemLastRun, "run", errSt, fmt.Sprintf("%s: %s", when, s.lastRun.Message)))
		}
	}

	status, message := rollupMacItems(items, "policy has no settings", "nothing applies on this node")
	return UpdatesPolicyResult{Status: status, Message: message, Items: items}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func removeString(list []string, s string) []string {
	out := list[:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// ─── reboot ──────────────────────────────────────────────────────────────────

// RebootSignals collects why the node needs a reboot. quick skips the
// slow checks (dnf needs-restarting loads the repository metadata).
func RebootSignals(ctx context.Context, deps *UpdatesDeps, quick bool) (required bool, reasons []string, staged bool) {
	p := deps.Platform
	run := deps.Run
	if run == nil {
		run = updates.DefaultRunner
	}
	if p == nil {
		return false, nil, false
	}
	running := updates.RunningKernel(p.Root)
	if p.Engine != updates.EngineRpmOstree {
		newest := updates.NewestKernel(updates.InstalledKernels(ctx, run, p))
		if updates.KernelRebootNeeded(running, newest) {
			reasons = append(reasons, "kernel "+newest+" installed, "+running+" running")
		}
	}
	switch p.Engine {
	case updates.EngineApt:
		if ok, pkgs := updates.RebootRequiredFile(p.Root); ok {
			msg := "/run/reboot-required"
			if len(pkgs) > 0 {
				msg += " (" + strings.Join(pkgs, ", ") + ")"
			}
			reasons = append(reasons, msg)
		}
		if ok, reason, _ := updates.NeedrestartKernel(ctx, run, p); ok && reason != "" {
			reasons = append(reasons, reason)
		}
	case updates.EngineDnf:
		if !quick && p.DnfBin != "" {
			if ok, reason := updates.DnfNeedsRestarting(ctx, run, p); ok {
				reasons = append(reasons, reason)
			}
		}
	case updates.EngineOsUpdate:
		if p.ZypperBin != "" && updates.ZypperNeedsRebooting(ctx, run, p) {
			reasons = append(reasons, "zypper needs-rebooting")
		}
	case updates.EngineRpmOstree:
		if st, err := updates.RpmOstreeReadStatus(ctx, run, p); err == nil && st.Staged != "" {
			staged = true
			reasons = append(reasons, "deployment "+st.Staged+" staged for the next boot")
		}
	}
	return len(reasons) > 0, uniqueStrings(reasons), staged
}

// RebootDecision is what the reboot rule decided for this tick.
type RebootDecision struct {
	// Required says a reboot is pending.
	Required bool
	Reasons  []string
	// Warned says users were warned this tick; Rebooted says the reboot
	// was started.
	Warned   bool
	Rebooted bool
	// Why nothing happened.
	Blocked string
}

// EvaluateReboot runs the reboot rule once: tracks since when a reboot is
// pending, and reboots when the policy, the window and the sessions
// allow. The state is updated and must be saved by the caller.
func EvaluateReboot(ctx context.Context, deps *UpdatesDeps, desired *UpdatesDesired, state *UpdatesState, opts UpdatesSyncOptions) RebootDecision {
	now := deps.now()
	required, reasons, _ := RebootSignals(ctx, deps, true)
	if !required && len(state.RebootReasons) > 0 && state.RebootPendingSince != nil && now.Sub(*state.RebootPendingSince) < 24*time.Hour {
		// A slow signal found earlier still stands until the next full scan
		// clears it.
		required, reasons = true, state.RebootReasons
	}
	dec := RebootDecision{Required: required, Reasons: reasons}
	if !required {
		state.RebootPendingSince, state.RebootReasons, state.RebootWarnedAt = nil, nil, nil
		return dec
	}
	if state.RebootPendingSince == nil {
		t := now
		state.RebootPendingSince = &t
	}
	state.RebootReasons = reasons
	if desired == nil || desired.Reboot == nil {
		dec.Blocked = "no reboot rule"
		return dec
	}
	r := desired.Reboot
	switch r.GetPolicy() {
	case pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED, pb.UpdatesRebootPolicy_UPDATES_REBOOT_ALWAYS:
	default:
		dec.Blocked = "the policy never reboots"
		return dec
	}
	if !opts.AllowReboot {
		dec.Blocked = "policy reboots are disabled in the agent configuration"
		return dec
	}
	if state.LastPolicyReboot != nil && now.Sub(*state.LastPolicyReboot) < 10*time.Minute {
		dec.Blocked = "a policy reboot was started minutes ago"
		return dec
	}
	w, err := updates.ParseWindow(r.GetWindow())
	if err != nil {
		dec.Blocked = err.Error()
		return dec
	}
	if !w.Contains(now) {
		dec.Blocked = "outside the reboot window " + w.String()
		return dec
	}
	sessions := updates.Sessions(deps.Logind)
	onlyWithout := r.OnlyWithoutSessions == nil || r.GetOnlyWithoutSessions()
	forced := false
	if len(sessions) > 0 && onlyWithout {
		days := r.GetForceAfterDays()
		if days <= 0 || now.Sub(*state.RebootPendingSince) < time.Duration(days)*24*time.Hour {
			dec.Blocked = fmt.Sprintf("%d user session(s) active", len(sessions))
			return dec
		}
		forced = true
	}
	// Warn before a reboot that affects users.
	if len(sessions) > 0 && r.GetWarnMinutes() > 0 {
		warn := time.Duration(r.GetWarnMinutes()) * time.Minute
		if state.RebootWarnedAt == nil {
			msg := r.GetMessage()
			if msg == "" {
				msg = fmt.Sprintf("This computer will restart in %d minutes to finish installing updates. Save your work.", r.GetWarnMinutes())
			}
			if deps.Notify != nil {
				for _, s := range sessions {
					if s.Graphical {
						_ = deps.Notify(s, "Restart required", msg)
					}
				}
			}
			t := now
			state.RebootWarnedAt = &t
			dec.Warned = true
			dec.Blocked = fmt.Sprintf("users warned; rebooting in %d minutes", r.GetWarnMinutes())
			return dec
		}
		if now.Sub(*state.RebootWarnedAt) < warn {
			dec.Blocked = "warning period running"
			return dec
		}
	}
	reason := strings.Join(reasons, "; ")
	if forced {
		reason += fmt.Sprintf(" (forced after %d days with users logged in)", r.GetForceAfterDays())
	}
	t := now
	state.LastPolicyReboot = &t
	state.LastPolicyRebootReason = reason
	state.RebootWarnedAt = nil
	dec.Rebooted = true
	if deps.Reboot != nil {
		if err := deps.Reboot(ctx); err != nil {
			dec.Rebooted = false
			dec.Blocked = "reboot failed: " + err.Error()
			state.LastPolicyReboot = nil
		}
	}
	return dec
}

// ─── inventory ───────────────────────────────────────────────────────────────

// UpdatesInventoryLimit caps the pending list a node reports.
const UpdatesInventoryLimit = 200

// ScanUpdatesInventory reads the update state of the node. It runs the
// slow reboot signal too and records the result in the state file so the
// quick reboot rule can use it.
func ScanUpdatesInventory(ctx context.Context, deps *UpdatesDeps, state *UpdatesState, systemDir string) *pb.UpdatesInventory {
	p := deps.Platform
	run := deps.Run
	if run == nil {
		run = updates.DefaultRunner
	}
	inv := &pb.UpdatesInventory{Engine: string(updates.EngineNone)}
	if p == nil {
		return inv
	}
	inv.Engine = string(p.Engine)
	inv.EngineInstalled = p.Installed
	inv.RunningKernel = updates.RunningKernel(p.Root)
	if bt := updates.BootTime(p.Root); !bt.IsZero() {
		inv.BootTime = timestamppb.New(bt)
	}
	if p.Engine == updates.EngineNone {
		return inv
	}
	if p.Engine != updates.EngineRpmOstree {
		inv.NewestKernel = updates.NewestKernel(updates.InstalledKernels(ctx, run, p))
	}
	// Engine configuration ownership and effective mode.
	cfgData, cfgErr := os.ReadFile(p.Path(p.ConfigPath)) //nolint:gosec // G304: engine configuration path
	switch {
	case cfgErr == nil && updates.HasHeader(cfgData):
		inv.EngineManagedBy = "bor"
	case cfgErr == nil:
		inv.EngineManagedBy = "foreign"
	default:
		inv.EngineManagedBy = "vendor"
	}
	if p.Engine == updates.EngineApt && inv.EngineManagedBy == "vendor" {
		inv.EngineManagedBy = "vendor"
	}
	// Timer state and last check.
	if deps.Systemd != nil && deps.Systemd.Available() && p.TimerUnit != "" {
		if st, err := deps.Systemd.Status(ctx, p.TimerUnit); err == nil && st != nil && st.Exists() {
			inv.EngineTimerEnabled = systemd.IsEnabledState(st.UnitFileState)
			if !st.LastTrigger.IsZero() {
				inv.LastCheck = timestamppb.New(st.LastTrigger)
			}
		}
		checkTimer := map[updates.Engine]string{updates.EngineDnf: "dnf5-makecache.timer", updates.EngineApt: "apt-daily.timer"}[p.Engine]
		if p.Engine == updates.EngineDnf && !p.DNF5 {
			checkTimer = "dnf-makecache.timer"
		}
		if checkTimer != "" {
			if st, err := deps.Systemd.Status(ctx, checkTimer); err == nil && st != nil && st.Exists() && !st.LastTrigger.IsZero() {
				if inv.LastCheck == nil || st.LastTrigger.After(inv.LastCheck.AsTime()) {
					inv.LastCheck = timestamppb.New(st.LastTrigger)
				}
			}
		}
	}
	inv.EffectiveMode = string(effectiveMode(p, cfgData, cfgErr == nil, inv.EngineTimerEnabled))
	// Pending updates.
	var pending []updates.Pending
	var lastRun *updates.Run
	switch p.Engine {
	case updates.EngineDnf:
		if p.DnfBin != "" {
			pending, _ = updates.DnfPending(ctx, run, p)
			if p.Installed {
				lastRun = updates.DnfLastRun(ctx, run, p)
			}
		}
	case updates.EngineApt:
		pending, _ = updates.AptPending(ctx, run, p)
		lastRun = updates.AptLastRun(p.Path(updates.AptLogPath))
	case updates.EngineOsUpdate:
		if p.ZypperBin != "" {
			pending, _ = updates.ZypperPending(ctx, run, p)
		}
	case updates.EngineRpmOstree:
		if st, err := updates.RpmOstreeReadStatus(ctx, run, p); err == nil {
			pending = st.Pending
			if !st.CachedUpdate.IsZero() {
				inv.LastCheck = timestamppb.New(st.CachedUpdate)
			}
		}
	}
	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].Security != pending[j].Security {
			return pending[i].Security
		}
		if r := severityOrder(pending[i].Severity) - severityOrder(pending[j].Severity); r != 0 {
			return r > 0
		}
		return pending[i].Name < pending[j].Name
	})
	inv.PendingTotal = clampInt32(len(pending))
	for _, pe := range pending {
		if pe.Security {
			inv.PendingSecurity++
		}
	}
	for i, pe := range pending {
		if i >= UpdatesInventoryLimit {
			break
		}
		inv.Pending = append(inv.Pending, &pb.UpdatesPending{Name: pe.Name, Version: pe.Version, Advisory: pe.Advisory, Severity: pe.Severity, Cves: uniqueStrings(pe.CVEs), Security: pe.Security, Kind: "package"})
	}
	if lastRun != nil {
		inv.LastRun = &pb.UpdatesRun{Time: timestamppb.New(lastRun.Time), Success: lastRun.Success, Packages: clampInt32(lastRun.Packages), Message: lastRun.Message}
	}
	// Reboot state, with the slow signal, recorded for the quick rule.
	required, reasons, staged := RebootSignals(ctx, deps, false)
	inv.Reboot = &pb.UpdatesRebootState{Required: required, Reasons: reasons, Staged: staged}
	if state != nil {
		now := deps.now()
		if required {
			if state.RebootPendingSince == nil {
				state.RebootPendingSince = &now
			}
			state.RebootReasons = reasons
			inv.Reboot.Since = timestamppb.New(*state.RebootPendingSince)
		} else {
			state.RebootPendingSince, state.RebootReasons = nil, nil
		}
		if state.LastPolicyReboot != nil {
			inv.LastPolicyReboot = timestamppb.New(*state.LastPolicyReboot)
		}
	}
	inv.ForeignMechanisms = foreignMechanisms(ctx, deps, p)
	_ = systemDir
	return inv
}

// effectiveMode reads what the node would install now.
func effectiveMode(p *updates.Platform, cfg []byte, cfgExists, timerEnabled bool) updates.Mode {
	if !p.Installed {
		return updates.ModeOff
	}
	switch p.Engine {
	case updates.EngineDnf:
		if !timerEnabled {
			return updates.ModeOff
		}
		apply, security := false, false
		for _, l := range strings.Split(string(cfg), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.ToLower(strings.TrimSpace(v))
			switch k {
			case "apply_updates":
				apply = v == "yes" || v == "true" || v == "1"
			case "upgrade_type":
				security = v == "security"
			}
		}
		switch {
		case apply && security:
			return updates.ModeSecurity
		case apply:
			return updates.ModeAll
		}
		return updates.ModeCheck
	case updates.EngineApt:
		if !timerEnabled {
			return updates.ModeOff
		}
		if cfgExists && updates.HasHeader(cfg) {
			if strings.Contains(string(cfg), `APT::Periodic::Unattended-Upgrade "0"`) {
				return updates.ModeCheck
			}
			if strings.Contains(string(cfg), "-security\";\n};") || strings.Count(string(cfg), "security") > 0 && !strings.Contains(string(cfg), "archive=${distro_codename}\"") && !strings.Contains(string(cfg), "label=Debian\"") {
				return updates.ModeSecurity
			}
			return updates.ModeAll
		}
		return updates.ModeKeep
	case updates.EngineRpmOstree:
		switch updates.RpmOstreePolicy(cfg) {
		case "check":
			return updates.ModeCheck
		case "stage", "apply":
			return updates.ModeAll
		}
		return updates.ModeOff
	case updates.EngineOsUpdate:
		if !timerEnabled {
			return updates.ModeOff
		}
		return updates.OsUpdateMode(cfg)
	}
	return updates.ModeKeep
}

// foreignMechanisms names other update mechanisms the node runs.
func foreignMechanisms(ctx context.Context, deps *UpdatesDeps, p *updates.Platform) []string {
	var out []string
	if deps.Systemd != nil && deps.Systemd.Available() {
		for _, t := range []string{"packagekit-background.timer", "yast-online-update.timer"} {
			if st, err := deps.Systemd.Status(ctx, t); err == nil && st != nil && st.Exists() && systemd.IsEnabledState(st.UnitFileState) {
				out = append(out, t)
			}
		}
	}
	if data, err := os.ReadFile(p.Path("/etc/sysconfig/automatic_online_update")); err == nil && strings.Contains(string(data), `AOU_ENABLE_CRONJOB="true"`) {
		out = append(out, "YaST automatic online update")
	}
	return out
}

func severityOrder(s string) int {
	switch s {
	case "critical":
		return 4
	case "important":
		return 3
	case "moderate":
		return 2
	case "low":
		return 1
	}
	return 0
}
