// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/logind"
	"github.com/VuteTech/Bor/agent/internal/notify"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	"github.com/VuteTech/Bor/agent/internal/updates"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// updatesActionTextLimit caps the engine output sent back with an action.
const updatesActionTextLimit = 32 * 1024

// updatesCacheEntry holds a Software updates policy alongside its binding
// priority and name.
type updatesCacheEntry struct {
	id       string
	name     string
	priority int32
	policy   *pb.UpdatesPolicy
}

var (
	updatesCacheMu sync.Mutex
	updatesCache   = make(map[string]updatesCacheEntry)
)

// updatesSnapshotStaging accumulates Software updates policies during a
// SNAPSHOT.
var updatesSnapshotStaging map[string]updatesCacheEntry

// updatesSyncMu serialises syncs and the reboot rule.
var updatesSyncMu sync.Mutex

// Tickers, guarded by updatesSyncMu.
var (
	updatesReconcileCancel context.CancelFunc
	updatesRebootCancel    context.CancelFunc
)

var (
	updatesInventoryMu     sync.Mutex
	updatesTickerMu        sync.Mutex
	updatesInventoryCancel context.CancelFunc
)

// updatesPlatform is detected once; the engine does not change while the
// agent runs, only whether its package is installed.
var (
	updatesPlatformOnce   sync.Once
	updatesPlatform       *updates.Platform
	updatesEngineOverride string
)

// setUpdatesEngineOverride records the engine the configuration forces
// (updates.engine); unknown names fall back to detection with a warning.
func setUpdatesEngineOverride(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "auto" {
		return
	}
	for _, e := range updates.KnownEngines {
		if string(e) == name {
			updatesEngineOverride = name
			log.Printf("software updates: engine forced to %s by updates.engine", name)
			return
		}
	}
	log.Printf("software updates: unknown updates.engine %q ignored; detecting the engine", name)
}

func updatesPlatformCached() *updates.Platform {
	updatesPlatformOnce.Do(func() { updatesPlatform = updates.DetectEngine(updatesEngineOverride) })
	return updatesPlatform
}

func updatesCachePut(e updatesCacheEntry) {
	updatesCacheMu.Lock()
	defer updatesCacheMu.Unlock()
	updatesCache[e.id] = e
}

func updatesCacheRemove(id string) bool {
	updatesCacheMu.Lock()
	defer updatesCacheMu.Unlock()
	if _, ok := updatesCache[id]; !ok {
		return false
	}
	delete(updatesCache, id)
	return true
}

func updatesCacheReplace(m map[string]updatesCacheEntry) {
	updatesCacheMu.Lock()
	defer updatesCacheMu.Unlock()
	if m == nil {
		m = make(map[string]updatesCacheEntry)
	}
	updatesCache = m
}

func updatesCacheEntries() []policy.UpdatesEntry {
	updatesCacheMu.Lock()
	defer updatesCacheMu.Unlock()
	entries := make([]policy.UpdatesEntry, 0, len(updatesCache))
	for _, e := range updatesCache {
		entries = append(entries, policy.UpdatesEntry{ID: e.id, Name: e.name, Priority: e.priority, Policy: e.policy})
	}
	return entries
}

// updatesDeps assembles the node facilities of a sync.
func updatesDeps(cfg *config.Config) *policy.UpdatesDeps {
	deps := &policy.UpdatesDeps{Platform: updatesPlatformCached(), Run: updates.DefaultRunner}
	if bus := systemdManager(cfg); bus != nil {
		deps.Systemd = bus
	}
	if l := logind.New(); l.Available() {
		deps.Logind = l
	}
	if pkDBusConn != nil && policy.IsPackageKitAvailable(pkDBusConn) {
		deps.InstallPackage = func(ctx context.Context, name string) error {
			// Another package manager (an inventory scan, a vendor timer)
			// may hold the lock; retry a few times before giving up.
			var err error
			for attempt := 0; attempt < 4; attempt++ {
				if attempt > 0 {
					select {
					case <-ctx.Done():
						return err
					case <-time.After(15 * time.Second):
					}
				}
				var id string
				var found bool
				id, found, err = policy.ResolvePackage(ctx, pkDBusConn, name, policy.PkFilterAvailable)
				if err == nil && !found {
					return fmt.Errorf("package %s is not available from the configured repositories", name)
				}
				if err == nil {
					err = policy.InstallPackage(ctx, pkDBusConn, id, false)
				}
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "lock") {
					return err
				}
			}
			return err
		}
	}
	deps.Notify = func(s updates.UserSession, summary, body string) error {
		_, err := notify.SendAlert(notify.AlertTarget{UID: s.UID, GID: s.GID, User: s.User, Summary: summary, Body: body, Critical: true})
		return err
	}
	deps.Reboot = func(ctx context.Context) error { return rebootNode(ctx, cfg) }
	return deps
}

// rebootNode starts the reboot: into dnf5's stored offline transaction
// when one is staged, through rebootmgr when it runs (SUSE's maintenance
// windows), otherwise systemctl.
func rebootNode(ctx context.Context, cfg *config.Config) error {
	if p := updatesPlatformCached(); p.DNF5 && p.DnfBin != "" && updates.DnfOfflineStatus(ctx, updates.DefaultRunner, p).Staged {
		err := updates.DnfOfflineReboot(ctx, updates.DefaultRunner, p)
		if err == nil {
			return nil
		}
		log.Printf("updates: dnf5 offline reboot failed (%v); falling back to a plain reboot", err)
	}
	if path, err := exec.LookPath("rebootmgrctl"); err == nil {
		if bus := systemdManager(cfg); bus != nil {
			if st, serr := bus.Status(ctx, "rebootmgr.service"); serr == nil && st != nil && st.IsActive() {
				out, rerr := exec.CommandContext(ctx, path, "reboot").CombinedOutput() //nolint:gosec // G204: fixed argv
				if rerr == nil {
					return nil
				}
				log.Printf("updates: rebootmgrctl reboot failed (%v: %s); falling back to systemctl", rerr, strings.TrimSpace(string(out)))
			}
		}
	}
	out, err := exec.CommandContext(ctx, "systemctl", "reboot").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl reboot: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func updatesSyncOptions(cfg *config.Config, reconcile bool) policy.UpdatesSyncOptions {
	return policy.UpdatesSyncOptions{
		StatePath:    cfg.Updates.StatePath,
		BackupDir:    cfg.Updates.BackupDir,
		SystemDir:    cfg.Systemd.SystemDir,
		AllowInstall: cfg.Updates.AllowInstall,
		AllowReboot:  cfg.Updates.AllowReboot,
		Reconcile:    reconcile,
	}
}

// triggerUpdatesSync serialises concurrent sync requests.
func triggerUpdatesSync(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	syncAllUpdates(ctx, client, cfg, false)
}

// updatesWritablePaths lists what a sync may write or remove.
func updatesWritablePaths(cfg *config.Config) []string {
	p := updatesPlatformCached()
	out := []string{cfg.Updates.StatePath}
	if p.ConfigPath != "" {
		out = append(out, p.ConfigPath)
	}
	if p.TimerUnit != "" {
		out = append(out, updates.TimerDropInPath(cfg.Systemd.SystemDir, p.TimerUnit))
	}
	if state, err := policy.LoadUpdatesState(cfg.Updates.StatePath); err == nil {
		out = append(out, state.DropIns...)
	}
	return out
}

// syncAllUpdates applies the merged policies, reports per-policy
// compliance, keeps the tickers in step with the cache and refreshes the
// inventory after a change. Must be called with updatesSyncMu held.
func syncAllUpdates(ctx context.Context, client *policyclient.Client, cfg *config.Config, reconcile bool) {
	entries := updatesCacheEntries()
	writable := updatesWritablePaths(cfg)
	// Installing the engine or reloading units takes a while and the state
	// file is written last: keep the watcher quiet for the whole run.
	suppressManagedWritesFor(cfg, cfg.Updates.ToolTimeout+2*time.Second, writable...)
	defer updateWatcher(cfg)
	defer resetManagedWriteWindow(cfg, writable...)

	toolCtx, cancel := context.WithTimeout(ctx, cfg.Updates.ToolTimeout)
	defer cancel()
	result := policy.SyncUpdates(toolCtx, updatesDeps(cfg), entries, updatesSyncOptions(cfg, reconcile))
	if reconcile && len(result.Drifts) > 0 {
		go reportMacDrifts(ctx, client, result.Drifts, nil)
	}
	if len(entries) == 0 {
		stopUpdatesReconcilerLocked()
		stopUpdatesRebootTickerLocked()
		log.Println("Software updates policies synced (0 policies): engine configuration restored")
	} else {
		ensureUpdatesReconcilerLocked(ctx, client, cfg)
		ensureUpdatesRebootTickerLocked(ctx, client, cfg)
		for _, e := range entries {
			r := result.Policies[e.ID]
			log.Printf("Software updates policy %q synced: %s - %s", e.Name, r.Status.String(), r.Message)
			_ = client.ReportComplianceWithStatus(ctx, e.ID, r.Status, r.Message, r.Items)
		}
	}
	if result.Changed {
		go reportUpdatesInventory(ctx, client, cfg)
	}
}

func ensureUpdatesReconcilerLocked(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	interval := cfg.Updates.ReconcileInterval
	if interval <= 0 || updatesReconcileCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	updatesReconcileCancel = cancel
	log.Printf("Software updates reconcile enabled (every %v)", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				updatesReconcileTick(tickCtx, client, cfg)
			}
		}
	}()
}

func stopUpdatesReconcilerLocked() {
	if updatesReconcileCancel != nil {
		updatesReconcileCancel()
		updatesReconcileCancel = nil
		log.Println("Software updates reconcile stopped")
	}
}

func stopUpdatesReconciler() {
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	stopUpdatesReconcilerLocked()
	stopUpdatesRebootTickerLocked()
}

// updatesReconcileTick re-runs the sync so a timer or a configuration
// changed by hand returns to the desired state.
func updatesReconcileTick(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	if ctx.Err() != nil || len(updatesCacheEntries()) == 0 {
		return
	}
	syncAllUpdates(ctx, client, cfg, true)
}

// ensureUpdatesRebootTickerLocked runs the reboot rule every
// updates.reboot_check_interval while a policy applies.
func ensureUpdatesRebootTickerLocked(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	interval := cfg.Updates.RebootCheckInterval
	if interval <= 0 || updatesRebootCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	updatesRebootCancel = cancel
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				updatesRebootTick(tickCtx, client, cfg)
			}
		}
	}()
}

func stopUpdatesRebootTickerLocked() {
	if updatesRebootCancel != nil {
		updatesRebootCancel()
		updatesRebootCancel = nil
	}
}

// updatesRebootTick evaluates the reboot rule once.
func updatesRebootTick(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	entries := updatesCacheEntries()
	if ctx.Err() != nil || len(entries) == 0 {
		return
	}
	state, err := policy.LoadUpdatesState(cfg.Updates.StatePath)
	if err != nil {
		return
	}
	desired := policy.MergeUpdatesPolicies(entries)
	if desired.Reboot == nil {
		return
	}
	dec := policy.EvaluateReboot(ctx, updatesDeps(cfg), desired, state, updatesSyncOptions(cfg, false))
	// The signals above run package manager commands that can outlast the
	// watcher's two-second window: open it right before the write.
	suppressManagedWrites(cfg, cfg.Updates.StatePath)
	_ = policy.SaveUpdatesState(cfg.Updates.StatePath, state)
	switch {
	case dec.Rebooted:
		reason := strings.Join(dec.Reasons, "; ")
		log.Printf("Software updates: rebooting the node (%s)", reason)
		_ = client.ReportUnitTamperEvent(ctx, "updates:reboot", "rebooted by policy: "+reason, nil)
	case dec.Warned:
		log.Printf("Software updates: users warned of the coming reboot (%s)", strings.Join(dec.Reasons, "; "))
	}
}

// reportUpdatesInventory scans the update state and sends it. The scan
// runs the package manager, which takes the same lock a sync's engine
// install needs, so the two are serialised.
func reportUpdatesInventory(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	updatesInventoryMu.Lock()
	defer updatesInventoryMu.Unlock()
	state, err := policy.LoadUpdatesState(cfg.Updates.StatePath)
	if err != nil {
		state = nil
	}
	toolCtx, cancel := context.WithTimeout(ctx, cfg.Updates.ToolTimeout)
	defer cancel()
	inv := policy.ScanUpdatesInventory(toolCtx, updatesDeps(cfg), state, cfg.Systemd.SystemDir)
	if state != nil {
		suppressManagedWrites(cfg, cfg.Updates.StatePath)
		_ = policy.SaveUpdatesState(cfg.Updates.StatePath, state)
	}
	if err := client.ReportUpdatesInventory(ctx, inv); err != nil {
		log.Printf("updates: ReportUpdatesInventory failed (non-fatal): %v", err)
		return
	}
	log.Printf("updates: reported inventory (engine %s, mode %q, %d pending, %d security, %d firmware, reboot required=%v)", inv.GetEngine(), inv.GetEffectiveMode(), inv.GetPendingTotal(), inv.GetPendingSecurity(), inv.GetPendingFirmware(), inv.GetReboot().GetRequired())
	// The age and firmware items read what the scan recorded: refresh the
	// compliance of policies that set them.
	if state != nil && updatesEntriesNeedScan(updatesCacheEntries()) {
		syncAllUpdates(ctx, client, cfg, true)
	}
}

// updatesEntriesNeedScan reports whether any bound policy has items that
// depend on the inventory scan.
func updatesEntriesNeedScan(entries []policy.UpdatesEntry) bool {
	for _, e := range entries {
		if e.Policy == nil {
			continue
		}
		if l := e.Policy.GetLimits(); l != nil && (l.GetSecurityMaxAgeDays() > 0 || l.GetRebootMaxAgeDays() > 0 || l.GetCheckMaxAgeDays() > 0) {
			return true
		}
		if e.Policy.GetFirmware().GetReportPending() {
			return true
		}
	}
	return false
}

// runUpdatesAction executes a check, install or reboot an operator asked
// for and reports the outcome.
func runUpdatesAction(ctx context.Context, client *policyclient.Client, cfg *config.Config, action *pb.UpdatesAction) {
	report := func(ok bool, msg, text string) {
		log.Printf("updates action %s (%s force=%v): success=%v %s", action.GetId(), action.GetKind(), action.GetForce(), ok, msg)
		if err := client.ReportUpdatesActionResult(ctx, action.GetId(), ok, msg, text); err != nil {
			log.Printf("Failed to report updates action result: %v", err)
		}
	}
	switch action.GetKind() {
	case pb.UpdatesActionKind_UPDATES_ACTION_CHECK:
		ok, msg, text := updatesActionCheck(ctx, cfg)
		report(ok, msg, text)
		if ok {
			reportUpdatesInventory(ctx, client, cfg)
		}
	case pb.UpdatesActionKind_UPDATES_ACTION_INSTALL:
		if !cfg.Updates.AllowInstall {
			report(false, "installing updates is disabled on this node (updates.allow_install is off)", "")
			return
		}
		ok, msg, text := updatesActionInstall(ctx, client, cfg)
		report(ok, msg, text)
		if ok {
			reportUpdatesInventory(ctx, client, cfg)
		}
	case pb.UpdatesActionKind_UPDATES_ACTION_REBOOT:
		if !cfg.Updates.AllowReboot {
			report(false, "reboots are disabled on this node (updates.allow_reboot is off)", "")
			return
		}
		ok, msg := updatesActionReboot(ctx, client, cfg, action.GetForce())
		report(ok, msg, "")
	default:
		report(false, "unknown action", "")
	}
}

// updatesActionCheck refreshes the package metadata.
func updatesActionCheck(ctx context.Context, cfg *config.Config) (ok bool, msg, text string) {
	p := updatesPlatformCached()
	bin, args := updates.RefreshCommand(p)
	if bin == "" {
		return false, "this node has no update engine to refresh", ""
	}
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	toolCtx, cancel := context.WithTimeout(ctx, cfg.Updates.ToolTimeout)
	defer cancel()
	out, err := updates.DefaultRunner(toolCtx, bin, args...)
	text = tailString(string(out), updatesActionTextLimit)
	if err != nil && (p.Engine != updates.EngineRpmOstree || updates.ExitCode(err) != 77) {
		return false, fmt.Sprintf("%s failed: %v", bin, err), text
	}
	return true, "metadata refreshed", text
}

// updatesActionInstall runs the engine once with the node's configuration.
func updatesActionInstall(ctx context.Context, client *policyclient.Client, cfg *config.Config) (ok bool, msg, text string) {
	entries := updatesCacheEntries()
	desired := policy.MergeUpdatesPolicies(entries)
	mode := updates.ModeKeep
	if len(entries) > 0 {
		mode = policy.UpdatesModeWord(desired.Mode)
	}
	p := updatesPlatformCached()
	bin, args, reason := updates.EngineRunCommand(p, mode)
	if bin == "" {
		return false, reason, ""
	}
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	toolCtx, cancel := context.WithTimeout(ctx, cfg.Updates.ToolTimeout)
	defer cancel()
	start := time.Now()
	if desired.Offline && p.DNF5 && p.DnfBin != "" {
		// The policy installs offline: store the transaction now; the
		// reboot rule (or the reboot action) runs it.
		if st := updates.DnfOfflineStatus(toolCtx, updates.DefaultRunner, p); st.Staged {
			return true, fmt.Sprintf("%d package(s) already staged for the next boot", st.Packages), ""
		}
		out, err := updates.DnfOfflineStage(toolCtx, updates.DefaultRunner, p, mode)
		text = tailString(string(out), updatesActionTextLimit)
		if err != nil {
			return false, fmt.Sprintf("storing the offline transaction failed after %s: %v", time.Since(start).Round(time.Second), err), text
		}
		st := updates.DnfOfflineStatus(toolCtx, updates.DefaultRunner, p)
		syncAllUpdates(ctx, client, cfg, true)
		if !st.Staged {
			return true, "nothing to stage", text
		}
		return true, fmt.Sprintf("%d package(s) staged for the next boot in %s", st.Packages, time.Since(start).Round(time.Second)), text
	}
	out, err := updates.DefaultRunner(toolCtx, bin, args...)
	text = tailString(string(out), updatesActionTextLimit)
	elapsed := time.Since(start).Round(time.Second)
	if err != nil {
		if p.Engine == updates.EngineRpmOstree && updates.ExitCode(err) == 77 {
			return true, fmt.Sprintf("no update available (%s)", elapsed), text
		}
		return false, fmt.Sprintf("%s failed after %s: %v", bin, elapsed, err), text
	}
	msg = fmt.Sprintf("engine run finished in %s", elapsed)
	if run := policy.ReadUpdatesLastRun(toolCtx, updatesDeps(cfg)); run != nil && time.Since(run.Time) < 2*elapsed+time.Minute {
		msg += ": " + run.Message
	}
	// The reboot rule sees what the run installed at its next tick; the
	// sync refreshes the last-run item now.
	syncAllUpdates(ctx, client, cfg, true)
	return true, msg, text
}

// updatesActionReboot reboots the node: at once when nobody is logged in,
// after a one-minute warning when forced, refused otherwise.
func updatesActionReboot(ctx context.Context, client *policyclient.Client, cfg *config.Config, force bool) (ok bool, msg string) {
	deps := updatesDeps(cfg)
	sessions := updates.Sessions(deps.Logind)
	updatesSyncMu.Lock()
	defer updatesSyncMu.Unlock()
	if len(sessions) > 0 && !force {
		names := make([]string, 0, len(sessions))
		for _, s := range sessions {
			names = append(names, s.User)
		}
		return false, fmt.Sprintf("%d user session(s) active (%s); force the reboot to warn them and restart in one minute", len(sessions), strings.Join(policy.UniqueStrings(names), ", "))
	}
	if state, err := policy.LoadUpdatesState(cfg.Updates.StatePath); err == nil {
		now := time.Now()
		state.LastPolicyReboot = &now
		state.LastPolicyRebootReason = "requested by an operator"
		suppressManagedWrites(cfg, cfg.Updates.StatePath)
		_ = policy.SaveUpdatesState(cfg.Updates.StatePath, state)
	}
	if len(sessions) == 0 {
		_ = client.ReportUnitTamperEvent(ctx, "updates:reboot", "rebooted by an operator action", nil)
		if err := rebootNode(ctx, cfg); err != nil {
			return false, err.Error()
		}
		return true, "rebooting now"
	}
	message := "This computer will restart in 1 minute to finish installing updates. Save your work."
	warned := 0
	for _, s := range sessions {
		if s.Graphical && deps.Notify != nil && deps.Notify(s, "Restart required", message) == nil {
			warned++
		}
	}
	out, err := exec.CommandContext(ctx, "shutdown", "-r", "+1", message).CombinedOutput()
	if err != nil {
		return false, fmt.Sprintf("shutdown -r +1: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_ = client.ReportUnitTamperEvent(ctx, "updates:reboot", "reboot scheduled by an operator action with users logged in", nil)
	return true, fmt.Sprintf("reboot scheduled in 1 minute; %d user(s) warned", warned)
}

// tailString keeps the last n bytes of s, on a line boundary.
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return "[...]\n" + cut
}

// startUpdatesInventoryTicker reports once now and then periodically.
func startUpdatesInventoryTicker(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	updatesTickerMu.Lock()
	defer updatesTickerMu.Unlock()
	if updatesInventoryCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	updatesInventoryCancel = cancel
	go func() {
		reportUpdatesInventory(tickCtx, client, cfg)
		if cfg.Updates.InventoryInterval <= 0 {
			return
		}
		ticker := time.NewTicker(cfg.Updates.InventoryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				reportUpdatesInventory(tickCtx, client, cfg)
			}
		}
	}()
}

func stopUpdatesInventoryTicker() {
	updatesTickerMu.Lock()
	defer updatesTickerMu.Unlock()
	if updatesInventoryCancel != nil {
		updatesInventoryCancel()
		updatesInventoryCancel = nil
	}
}

// updatesManagedPaths returns the files the watcher guards for this type.
func updatesManagedPaths(cfg *config.Config) []string {
	var paths []string
	state, err := policy.LoadUpdatesState(cfg.Updates.StatePath)
	if err != nil {
		return nil
	}
	if _, serr := os.Stat(cfg.Updates.StatePath); serr == nil {
		paths = append(paths, cfg.Updates.StatePath)
	}
	if len(updatesCacheEntries()) == 0 {
		return paths
	}
	p := updatesPlatformCached()
	if state.ConfigWritten && p.ConfigPath != "" {
		if _, serr := os.Stat(p.ConfigPath); serr == nil {
			paths = append(paths, p.ConfigPath)
		}
	}
	for _, d := range state.DropIns {
		if _, serr := os.Stat(d); serr == nil {
			paths = append(paths, d)
		}
	}
	return paths
}

// isUpdatesManagedPath reports whether a tampered path belongs to this type.
func isUpdatesManagedPath(cfg *config.Config, path string) bool {
	if path == cfg.Updates.StatePath {
		return true
	}
	p := updatesPlatformCached()
	if p.ConfigPath != "" && path == p.ConfigPath {
		return true
	}
	if p.TimerUnit != "" && path == updates.TimerDropInPath(cfg.Systemd.SystemDir, p.TimerUnit) {
		return true
	}
	if state, err := policy.LoadUpdatesState(cfg.Updates.StatePath); err == nil {
		for _, d := range state.DropIns {
			if d == path {
				return true
			}
		}
	}
	return false
}
