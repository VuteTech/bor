// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/apparmor"
	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/macrules"
)

// apparmorCacheEntry holds an AppArmor policy alongside its binding
// priority and name.
type apparmorCacheEntry struct {
	id       string
	name     string
	priority int32
	policy   *pb.ApparmorPolicy
}

var (
	apparmorCacheMu sync.Mutex
	apparmorCache   = make(map[string]apparmorCacheEntry)
)

// apparmorSnapshotStaging accumulates AppArmor policies during a SNAPSHOT.
var apparmorSnapshotStaging map[string]apparmorCacheEntry

// apparmorSyncMu serialises syncs.
var apparmorSyncMu sync.Mutex

// apparmorReconcileCancel stops the reconcile ticker. Guarded by apparmorSyncMu.
var apparmorReconcileCancel context.CancelFunc

func apparmorCachePut(e apparmorCacheEntry) {
	apparmorCacheMu.Lock()
	defer apparmorCacheMu.Unlock()
	apparmorCache[e.id] = e
}

func apparmorCacheRemove(id string) bool {
	apparmorCacheMu.Lock()
	defer apparmorCacheMu.Unlock()
	if _, ok := apparmorCache[id]; !ok {
		return false
	}
	delete(apparmorCache, id)
	return true
}

func apparmorCacheReplace(m map[string]apparmorCacheEntry) {
	apparmorCacheMu.Lock()
	defer apparmorCacheMu.Unlock()
	if m == nil {
		m = make(map[string]apparmorCacheEntry)
	}
	apparmorCache = m
}

func apparmorCacheEntries() []policy.ApparmorEntry {
	apparmorCacheMu.Lock()
	defer apparmorCacheMu.Unlock()
	entries := make([]policy.ApparmorEntry, 0, len(apparmorCache))
	for _, e := range apparmorCache {
		entries = append(entries, policy.ApparmorEntry{ID: e.id, Name: e.name, Priority: e.priority, Policy: e.policy})
	}
	return entries
}

func apparmorSyncOptions(cfg *config.Config, reconcile bool) policy.ApparmorSyncOptions {
	return policy.ApparmorSyncOptions{
		SysctlPath:           cfg.Apparmor.SysctlPath,
		StatePath:            cfg.Apparmor.StatePath,
		BackupDir:            cfg.Apparmor.BackupDir,
		AllowCustomProfiles:  cfg.Apparmor.AllowCustomProfiles,
		AllowVendorFlagEdits: cfg.Apparmor.AllowVendorFlagEdits,
		Reconcile:            reconcile,
	}
}

// triggerApparmorSync serialises concurrent AppArmor sync requests.
func triggerApparmorSync(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	apparmorSyncMu.Lock()
	defer apparmorSyncMu.Unlock()
	syncAllApparmor(ctx, client, cfg, false)
}

// syncAllApparmor applies the merged AppArmor policies, reports
// per-policy compliance, keeps the reconcile ticker in step with the
// cache and refreshes the inventory after a change. Must be called with
// apparmorSyncMu held.
func syncAllApparmor(ctx context.Context, client *policyclient.Client, cfg *config.Config, reconcile bool) {
	entries := apparmorCacheEntries()
	// A reload can take many seconds (a tunable change recompiles every
	// profile) and the state file is written last: keep the watcher quiet
	// for the whole run, then shorten the window to the usual two seconds.
	writable := apparmorWritablePaths(cfg, entries)
	suppressManagedWritesFor(cfg, cfg.Apparmor.ToolTimeout+2*time.Second, writable...)
	defer updateWatcher(cfg)
	defer resetManagedWriteWindow(cfg, writable...)

	toolCtx, cancel := context.WithTimeout(ctx, cfg.Apparmor.ToolTimeout)
	defer cancel()
	macQuietUntil.Store(time.Now().Add(cfg.Apparmor.ToolTimeout + macSyncQuiet).UnixNano())
	result := policy.SyncApparmor(toolCtx, apparmorDeps(cfg), entries, apparmorSyncOptions(cfg, reconcile))
	macQuietUntil.Store(time.Now().Add(macSyncQuiet).UnixNano())
	if reconcile && len(result.Drifts) > 0 {
		go reportMacDrifts(ctx, client, result.Drifts, nil)
	}
	if len(entries) == 0 {
		stopApparmorReconcilerLocked()
		log.Println("AppArmor policies synced (0 policies): Bor profiles removed and original states restored")
	} else {
		ensureApparmorReconcilerLocked(ctx, client, cfg)
		for _, e := range entries {
			r := result.Policies[e.ID]
			log.Printf("AppArmor policy %q synced: %s - %s", e.Name, r.Status.String(), r.Message)
			_ = client.ReportComplianceWithStatus(ctx, e.ID, r.Status, r.Message, r.Items)
		}
	}
	if result.Changed {
		go reportMacInventory(ctx, client, cfg)
	}
}

// apparmorWritablePaths lists every file a sync may write or remove: the
// files Bor manages now (from the state file) and the ones the merged
// policies want.
func apparmorWritablePaths(cfg *config.Config, entries []policy.ApparmorEntry) []string {
	paths := apparmor.DefaultPaths(cfg.Apparmor.ProfilesDir)
	out := []string{cfg.Apparmor.StatePath, cfg.Apparmor.SysctlPath, paths.HomeTunablePath()}
	if state, err := policy.LoadApparmorState(cfg.Apparmor.StatePath); err == nil {
		for name := range state.Customs {
			out = append(out, paths.ProfilePath(macrules.ProfileFileName(name)))
		}
		for file := range state.Overrides {
			out = append(out, paths.LocalPath(file))
		}
		for file := range state.FlagEdits {
			out = append(out, paths.ProfilePath(file))
		}
		for file := range state.Links {
			out = append(out, filepath.Join(paths.ProfilesDir, apparmor.DisableDir, file), filepath.Join(paths.ProfilesDir, apparmor.ForceComplainDir, file))
		}
	}
	d := policy.MergeApparmorPolicies(entries)
	for name := range d.Custom {
		out = append(out, paths.ProfilePath(macrules.ProfileFileName(name)))
	}
	for file := range d.Overrides {
		out = append(out, paths.LocalPath(file))
	}
	return out
}

func ensureApparmorReconcilerLocked(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	interval := cfg.Apparmor.ReconcileInterval
	if interval <= 0 || apparmorReconcileCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	apparmorReconcileCancel = cancel
	log.Printf("AppArmor reconcile enabled (every %v)", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				apparmorReconcileTick(tickCtx, client, cfg)
			}
		}
	}()
}

func stopApparmorReconcilerLocked() {
	if apparmorReconcileCancel != nil {
		apparmorReconcileCancel()
		apparmorReconcileCancel = nil
		log.Println("AppArmor reconcile stopped")
	}
}

func stopApparmorReconciler() {
	apparmorSyncMu.Lock()
	defer apparmorSyncMu.Unlock()
	stopApparmorReconcilerLocked()
}

// apparmorReconcileTick re-runs the sync so a profile mode changed by hand
// returns to the desired state.
func apparmorReconcileTick(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	apparmorSyncMu.Lock()
	defer apparmorSyncMu.Unlock()
	if ctx.Err() != nil || len(apparmorCacheEntries()) == 0 {
		return
	}
	syncAllApparmor(ctx, client, cfg, true)
}

// apparmorManagedPaths returns the files the watcher guards for this
// type: Bor profiles, local override files, the home tunable, the sysctl
// file, the state file and the link directories, while policies apply.
func apparmorManagedPaths(cfg *config.Config) []string {
	var paths []string
	if cfg.Apparmor.StatePath != "" {
		if _, err := os.Stat(cfg.Apparmor.StatePath); err == nil {
			paths = append(paths, cfg.Apparmor.StatePath)
		}
	}
	if len(apparmorCacheEntries()) == 0 {
		return paths
	}
	for _, p := range apparmorWritablePaths(cfg, nil) {
		if _, err := os.Lstat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// isApparmorManagedPath reports whether a tampered path belongs to this
// type: anything under the profile directory, the sysctl file or the
// state file.
func isApparmorManagedPath(cfg *config.Config, path string) bool {
	if path == cfg.Apparmor.StatePath || path == cfg.Apparmor.SysctlPath {
		return true
	}
	return cfg.Apparmor.ProfilesDir != "" && strings.HasPrefix(path, cfg.Apparmor.ProfilesDir+string(filepath.Separator))
}
