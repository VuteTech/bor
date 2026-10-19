// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
)

// systemdCacheEntry holds a Systemd policy alongside its binding priority
// and name.
type systemdCacheEntry struct {
	id       string
	name     string
	priority int32
	policy   *pb.SystemdPolicy
}

// systemdCache maps policy ID -> Systemd policy for all active Systemd
// policies. It is written by the stream goroutine and read by the async
// sync, so every access goes through the helpers below.
var (
	systemdCacheMu sync.Mutex
	systemdCache   = make(map[string]systemdCacheEntry)
)

// systemdSnapshotStaging accumulates Systemd policies during a SNAPSHOT
// (stream goroutine only).
var systemdSnapshotStaging map[string]systemdCacheEntry

// systemdSyncMu serialises syncs: starting or stopping a unit can block for
// its timeout, so every dispatch site calls triggerSystemdSync with `go`.
var systemdSyncMu sync.Mutex

// systemdCatalogueReported is set once the unit inventory reached the server.
var systemdCatalogueReported bool

// systemdReconcileCancel stops the reconcile ticker; nil when none runs.
// Guarded by systemdSyncMu.
var systemdReconcileCancel context.CancelFunc

// systemdBus is the shared manager connection, created on demand.
var (
	systemdBusMu sync.Mutex
	systemdBus   *systemd.Bus
)

// systemdUsers is the shared provider of user managers for policies in
// the user-manager scope, created on demand.
var (
	systemdUsersMu sync.Mutex
	systemdUsers   *systemd.BridgeUserManagers
)

// systemdDriftWindow is how far back the journal is searched for the
// person behind a unit change.
const systemdDriftWindow = 2 * time.Minute

// systemdQuietUntil (unix nanoseconds) tells the signal watcher to ignore
// the unit state changes a sync of our own causes.
var systemdQuietUntil atomic.Int64

// systemdSignalQuiet is how long after a sync the watcher stays quiet.
const systemdSignalQuiet = 3 * time.Second

func systemdCachePut(e systemdCacheEntry) {
	systemdCacheMu.Lock()
	defer systemdCacheMu.Unlock()
	systemdCache[e.id] = e
}

func systemdCacheRemove(id string) bool {
	systemdCacheMu.Lock()
	defer systemdCacheMu.Unlock()
	if _, ok := systemdCache[id]; !ok {
		return false
	}
	delete(systemdCache, id)
	return true
}

func systemdCacheReplace(m map[string]systemdCacheEntry) {
	systemdCacheMu.Lock()
	defer systemdCacheMu.Unlock()
	if m == nil {
		m = make(map[string]systemdCacheEntry)
	}
	systemdCache = m
}

func systemdCacheEntries() []policy.SystemdEntry {
	systemdCacheMu.Lock()
	defer systemdCacheMu.Unlock()
	entries := make([]policy.SystemdEntry, 0, len(systemdCache))
	for _, e := range systemdCache {
		entries = append(entries, policy.SystemdEntry{ID: e.id, Name: e.name, Priority: e.priority, Policy: e.policy})
	}
	return entries
}

// systemdManager returns the shared bus connection, connecting or
// reconnecting when needed. nil means systemd is not reachable.
func systemdManager(cfg *config.Config) *systemd.Bus {
	systemdBusMu.Lock()
	defer systemdBusMu.Unlock()
	if !systemd.IsInit() {
		return nil
	}
	if systemdBus != nil {
		if systemdBus.Available() {
			return systemdBus
		}
		_ = systemdBus.Close()
		systemdBus = nil
	}
	bus, err := systemd.Connect(cfg.Systemd.JobTimeout)
	if err != nil {
		log.Printf("Warning: systemd: %v", err)
		return nil
	}
	systemdBus = bus
	return bus
}

func closeSystemdManager() {
	systemdBusMu.Lock()
	defer systemdBusMu.Unlock()
	if systemdBus != nil {
		_ = systemdBus.Close()
		systemdBus = nil
	}
}

// systemdUserManagers returns the shared provider of user managers.
func systemdUserManagers(cfg *config.Config) systemd.UserManagers {
	systemdUsersMu.Lock()
	defer systemdUsersMu.Unlock()
	if systemdUsers == nil {
		systemdUsers = &systemd.BridgeUserManagers{JobTimeout: cfg.Systemd.JobTimeout}
	}
	return systemdUsers
}

func closeSystemdUserManagers() {
	systemdUsersMu.Lock()
	defer systemdUsersMu.Unlock()
	if systemdUsers != nil {
		systemdUsers.Close()
		systemdUsers = nil
	}
}

func systemdDirs(cfg *config.Config) *policy.SystemdDirs {
	return &policy.SystemdDirs{
		SystemDir:     cfg.Systemd.SystemDir,
		PresetDir:     cfg.Systemd.PresetDir,
		ScriptsDir:    cfg.Systemd.ScriptsDir,
		UserDir:       cfg.Systemd.UserDir,
		UserPresetDir: cfg.Systemd.UserPresetDir,
	}
}

func systemdAnalyzer(cfg *config.Config) *systemd.Analyzer {
	a := systemd.NewAnalyzer(cfg.Systemd.AnalyzeBinary)
	if !a.Available() {
		return nil
	}
	return a
}

// systemdSyncOptions builds the inputs of a sync; reconcile marks a pass
// that re-asserts an unchanged desired state, whose corrections are drift.
func systemdSyncOptions(cfg *config.Config, reconcile bool) *policy.SystemdSyncOptions {
	return &policy.SystemdSyncOptions{
		SystemDir:     cfg.Systemd.SystemDir,
		PresetDir:     cfg.Systemd.PresetDir,
		ScriptsDir:    cfg.Systemd.ScriptsDir,
		UserDir:       cfg.Systemd.UserDir,
		UserPresetDir: cfg.Systemd.UserPresetDir,
		StatePath:     cfg.Systemd.StatePath,
		AllowTasks:    cfg.Systemd.AllowTasks,
		Analyzer:      systemd.NewAnalyzer(cfg.Systemd.AnalyzeBinary),
		Journal:       systemd.NewJournal(),
		Users:         systemdUserManagers(cfg),
		Reconcile:     reconcile,
	}
}

// systemdManagedUnitNames lists the units the signal watcher follows: the
// merged system-scope units of the cached policies and the units of their
// system-scope tasks (user managers emit no signals on the system bus).
func systemdManagedUnitNames() []string {
	entries := systemdCacheEntries()
	if len(entries) == 0 {
		return nil
	}
	desired := policy.MergeSystemdPolicies(entries)
	var names []string
	for _, key := range desired.Names() {
		if u := desired.Units[key]; !u.Global {
			names = append(names, u.Name)
		}
	}
	for _, task := range desired.TaskNames() {
		if desired.Tasks[task].Global {
			continue
		}
		names = append(names, systemdunits.TaskServiceUnit(task), systemdunits.TaskTimerUnit(task))
	}
	return names
}

// startSystemdWatcher follows systemd's signals and re-runs the sync when a
// managed unit drifts; our own changes are ignored for a few seconds after
// each sync.
func startSystemdWatcher(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	go systemd.WatchUnits(ctx, systemdManagedUnitNames, func(reason string) {
		if time.Now().UnixNano() < systemdQuietUntil.Load() {
			return
		}
		if len(systemdCacheEntries()) == 0 {
			return
		}
		log.Printf("systemd: %s; reconciling", reason)
		go systemdReconcileTick(ctx, client, cfg)
	})
}

// triggerSystemdSync serialises concurrent Systemd sync requests.
func triggerSystemdSync(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	systemdSyncMu.Lock()
	defer systemdSyncMu.Unlock()
	syncAllSystemd(ctx, client, cfg, false)
}

// syncAllSystemd applies the merged Systemd policies, reports per-policy
// compliance, keeps the reconcile ticker in step with the cache and
// refreshes the unit catalogue after a change. A reconcile pass also
// reports the corrections it made as tamper events. Must be called with
// systemdSyncMu held.
func syncAllSystemd(ctx context.Context, client *policyclient.Client, cfg *config.Config, reconcile bool) {
	entries := systemdCacheEntries()
	desired := policy.MergeSystemdPolicies(entries)

	// Suppress the watcher for the files this sync writes or removes
	// (drop-ins, task units and scripts, the preset file) and for the
	// state file.
	existing, _ := policy.ListBorManagedSystemdFiles(systemdDirs(cfg))
	toSuppress := append(policy.SystemdDesiredFiles(systemdDirs(cfg), desired), existing...)
	toSuppress = append(toSuppress, cfg.Systemd.StatePath)
	suppressManagedWrites(cfg, toSuppress...)
	defer updateWatcher(cfg)

	var mgr systemd.Manager
	if bus := systemdManager(cfg); bus != nil {
		mgr = bus
	}
	result := policy.SyncSystemd(ctx, mgr, entries, systemdSyncOptions(cfg, reconcile))
	systemdQuietUntil.Store(time.Now().Add(systemdSignalQuiet).UnixNano())
	if reconcile && len(result.Drifts) > 0 {
		go reportSystemdDrifts(ctx, client, result.Drifts)
	}

	if len(entries) == 0 {
		stopSystemdReconcilerLocked()
		log.Println("Systemd policies synced (0 policies): drop-ins removed and original unit states restored")
	} else {
		ensureSystemdReconcilerLocked(ctx, client, cfg)
		for _, e := range entries {
			r := result.Policies[e.ID]
			log.Printf("Systemd policy %q synced: %s - %s", e.Name, r.Status.String(), r.Message)
			_ = client.ReportComplianceWithStatus(ctx, e.ID, r.Status, r.Message, r.Items)
		}
	}
	if result.Changed {
		go reportSystemdCatalogue(ctx, client, cfg)
	}
}

// ensureSystemdReconcilerLocked starts the reconcile ticker when policies
// apply and it is not running yet. Must be called with systemdSyncMu held.
func ensureSystemdReconcilerLocked(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	interval := cfg.Systemd.ReconcileInterval
	if interval <= 0 || systemdReconcileCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	systemdReconcileCancel = cancel
	log.Printf("Systemd reconcile enabled (every %v)", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				systemdReconcileTick(tickCtx, client, cfg)
			}
		}
	}()
}

// stopSystemdReconcilerLocked cancels the ticker. Must be called with
// systemdSyncMu held.
func stopSystemdReconcilerLocked() {
	if systemdReconcileCancel != nil {
		systemdReconcileCancel()
		systemdReconcileCancel = nil
		log.Println("Systemd reconcile stopped")
	}
}

// stopSystemdReconciler cancels the ticker (agent shutdown).
func stopSystemdReconciler() {
	systemdSyncMu.Lock()
	defer systemdSyncMu.Unlock()
	stopSystemdReconcilerLocked()
}

// systemdReconcileTick re-runs the sync so a unit stopped or re-enabled
// by hand returns to the desired state. The sync is idempotent: files are
// rewritten only when they differ and jobs run only when a state differs.
func systemdReconcileTick(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	systemdSyncMu.Lock()
	defer systemdSyncMu.Unlock()
	if ctx.Err() != nil || len(systemdCacheEntries()) == 0 {
		return
	}
	syncAllSystemd(ctx, client, cfg, true)
}

// reportSystemdDrifts records every unit a reconcile pass found changed
// by hand as a tamper event, attributed from the journal when sudo or
// polkit logged who touched the unit.
func reportSystemdDrifts(ctx context.Context, client *policyclient.Client, drifts []policy.SystemdDrift) {
	journal := systemd.NewJournal()
	for _, d := range drifts {
		var procs []policyclient.TamperProcess
		for _, a := range journal.RecentActors(ctx, d.Unit, systemdDriftWindow) {
			procs = append(procs, policyclient.TamperProcess{PID: int(a.PID), Comm: a.Comm, User: a.User})
		}
		log.Printf("Tamper protection: %s %s (%d actors identified)", d.Unit, d.Description(), len(procs))
		if err := client.ReportUnitTamperEvent(ctx, d.Resource(), d.Description(), procs); err != nil {
			log.Printf("Failed to report unit drift to server: %v", err)
		}
	}
}

// systemdActionVerbs maps the command kinds to the verbs the manager runs.
var systemdActionVerbs = map[pb.SystemdUnitActionKind]string{
	pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_START:   systemdunits.UnitActionStart,
	pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_STOP:    systemdunits.UnitActionStop,
	pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_RESTART: systemdunits.UnitActionRestart,
}

// runSystemdUnitAction runs a start, stop or restart an operator requested
// from the node page and reports the outcome with the unit's state. It
// serialises with syncs; the signal watcher ignores the change it causes.
func runSystemdUnitAction(ctx context.Context, client *policyclient.Client, cfg *config.Config, action *pb.SystemdUnitAction) {
	unit, verb := action.GetUnit(), systemdActionVerbs[action.GetAction()]
	report := func(ok bool, msg string, st *systemd.UnitStatus) {
		log.Printf("systemd action %s %s: success=%v (%s)", verb, unit, ok, msg)
		if err := client.ReportSystemdUnitActionResult(ctx, action.GetId(), ok, msg, policy.SystemdUnitInfoFromStatus(st)); err != nil {
			log.Printf("Failed to report unit action result: %v", err)
		}
	}
	if err := systemdunits.ValidateUnitAction(unit, verb); err != nil {
		report(false, err.Error(), nil)
		return
	}
	if !cfg.Systemd.AllowActions {
		report(false, "on-demand unit actions are disabled on this node (systemd.allow_actions is off)", nil)
		return
	}
	systemdSyncMu.Lock()
	defer systemdSyncMu.Unlock()
	bus := systemdManager(cfg)
	if bus == nil {
		report(false, "cannot reach systemd on the system bus", nil)
		return
	}
	st, err := bus.Status(ctx, unit)
	if err != nil {
		report(false, err.Error(), nil)
		return
	}
	if !st.Exists() {
		report(false, "unit is not installed on this node", st)
		return
	}
	systemdQuietUntil.Store(time.Now().Add(cfg.Systemd.JobTimeout + systemdSignalQuiet).UnixNano())
	var r systemd.JobResult
	switch verb {
	case systemdunits.UnitActionStart:
		r, err = bus.Start(ctx, unit)
	case systemdunits.UnitActionStop:
		r, err = bus.Stop(ctx, unit)
	default:
		r, err = bus.Restart(ctx, unit)
	}
	systemdQuietUntil.Store(time.Now().Add(systemdSignalQuiet).UnixNano())
	after, _ := bus.Status(ctx, unit)
	switch {
	case err != nil:
		report(false, verb+": "+err.Error(), after)
		return
	case !r.OK():
		report(false, fmt.Sprintf("%s failed (job result %s)", verb, r), after)
		return
	}
	msg := fmt.Sprintf("%s: %s", verb, r)
	if note := systemdActionPolicyNote(unit, verb); note != "" {
		msg += "; " + note
	}
	report(true, msg, after)
}

// systemdActionPolicyNote says when a bound policy will undo the action.
func systemdActionPolicyNote(unit, verb string) string {
	entries := systemdCacheEntries()
	if len(entries) == 0 {
		return ""
	}
	desired := policy.MergeSystemdPolicies(entries)
	if systemdunits.IsTaskUnit(unit) {
		for _, name := range desired.TaskNames() {
			if !desired.Tasks[name].Global && (unit == systemdunits.TaskServiceUnit(name) || unit == systemdunits.TaskTimerUnit(name)) {
				return fmt.Sprintf("the unit belongs to Bor task %q; the next sync re-applies the task's state", name)
			}
		}
		return ""
	}
	u := desired.Units[policy.SystemdUnitKey(false, unit)]
	if u == nil {
		return ""
	}
	switch {
	case verb == systemdunits.UnitActionStop && u.Activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		return fmt.Sprintf("policy %q keeps this unit running; the next reconcile pass starts it again", u.ActivityBy.Name)
	case verb != systemdunits.UnitActionStop && u.Activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return fmt.Sprintf("policy %q keeps this unit stopped; the next reconcile pass stops it again", u.ActivityBy.Name)
	}
	return ""
}

// reportSystemdCatalogue scans the unit inventory and sends it to the
// server. It reports whether the report was delivered.
func reportSystemdCatalogue(ctx context.Context, client *policyclient.Client, cfg *config.Config) bool {
	bus := systemdManager(cfg)
	if bus == nil {
		return false
	}
	var scorer policy.SecurityScorer
	if a := systemdAnalyzer(cfg); a != nil {
		scorer = a
	}
	units, err := policy.ScanSystemdUnits(ctx, bus, scorer)
	if err != nil {
		log.Printf("systemd: unit scan failed (non-fatal): %v", err)
		return false
	}
	version, _ := bus.Version(ctx)
	if err := client.ReportSystemdCatalogue(ctx, units, version); err != nil {
		log.Printf("systemd: ReportSystemdCatalogue failed (non-fatal): %v", err)
		return false
	}
	log.Printf("systemd: reported %d units to server", len(units))
	return true
}

// systemdManagedPaths returns the files the watcher guards for this type.
func systemdManagedPaths(cfg *config.Config) []string {
	var paths []string
	if len(systemdCacheEntries()) > 0 {
		if files, err := policy.ListBorManagedSystemdFiles(systemdDirs(cfg)); err == nil {
			paths = append(paths, files...)
		}
	}
	if cfg.Systemd.StatePath != "" {
		if _, err := os.Stat(cfg.Systemd.StatePath); err == nil {
			paths = append(paths, cfg.Systemd.StatePath)
		}
	}
	return paths
}

// isSystemdManagedPath reports whether a tampered path belongs to this
// type: the state file, the preset file, a task script, a Bor drop-in or
// a task unit file.
func isSystemdManagedPath(cfg *config.Config, path string) bool {
	if cfg.Systemd.StatePath != "" && path == cfg.Systemd.StatePath {
		return true
	}
	dirs := systemdDirs(cfg)
	if path == dirs.PresetPath() || path == dirs.UserPresetPath() {
		return true
	}
	if strings.HasPrefix(path, dirs.ScriptsDirPath()+string(filepath.Separator)) {
		return true
	}
	for _, dir := range []string{dirs.UnitDir(false), dirs.UnitDir(true)} {
		if !strings.HasPrefix(path, dir+string(filepath.Separator)) {
			continue
		}
		if filepath.Dir(path) == dir && systemdunits.IsTaskUnit(filepath.Base(path)) {
			return true
		}
		return filepath.Base(path) == policy.SystemdDropInName && strings.HasSuffix(filepath.Dir(path), ".d")
	}
	return false
}
