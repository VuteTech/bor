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
	"sync/atomic"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// selinuxCacheEntry holds an SELinux policy alongside its binding priority
// and name.
type selinuxCacheEntry struct {
	id       string
	name     string
	priority int32
	policy   *pb.SelinuxPolicy
}

var (
	selinuxCacheMu sync.Mutex
	selinuxCache   = make(map[string]selinuxCacheEntry)
)

// selinuxSnapshotStaging accumulates SELinux policies during a SNAPSHOT.
var selinuxSnapshotStaging map[string]selinuxCacheEntry

// selinuxSyncMu serialises syncs: a policy rebuild takes seconds.
var selinuxSyncMu sync.Mutex

// selinuxReconcileCancel stops the reconcile ticker. Guarded by selinuxSyncMu.
var selinuxReconcileCancel context.CancelFunc

// macQuietUntil (unix nanoseconds) tells the audit follower to ignore the
// records a sync of our own causes.
var macQuietUntil atomic.Int64

const macSyncQuiet = 10 * time.Second

func selinuxCachePut(e selinuxCacheEntry) {
	selinuxCacheMu.Lock()
	defer selinuxCacheMu.Unlock()
	selinuxCache[e.id] = e
}

func selinuxCacheRemove(id string) bool {
	selinuxCacheMu.Lock()
	defer selinuxCacheMu.Unlock()
	if _, ok := selinuxCache[id]; !ok {
		return false
	}
	delete(selinuxCache, id)
	return true
}

func selinuxCacheReplace(m map[string]selinuxCacheEntry) {
	selinuxCacheMu.Lock()
	defer selinuxCacheMu.Unlock()
	if m == nil {
		m = make(map[string]selinuxCacheEntry)
	}
	selinuxCache = m
}

func selinuxCacheEntries() []policy.SelinuxEntry {
	selinuxCacheMu.Lock()
	defer selinuxCacheMu.Unlock()
	entries := make([]policy.SelinuxEntry, 0, len(selinuxCache))
	for _, e := range selinuxCache {
		entries = append(entries, policy.SelinuxEntry{ID: e.id, Name: e.name, Priority: e.priority, Policy: e.policy})
	}
	return entries
}

func selinuxSyncOptions(cfg *config.Config, reconcile bool) policy.SelinuxSyncOptions {
	return policy.SelinuxSyncOptions{
		ModulesDir:         cfg.Selinux.ModulesDir,
		ModulePriority:     cfg.Selinux.ModulePriority,
		StatePath:          cfg.Selinux.StatePath,
		AllowModeChange:    cfg.Selinux.AllowModeChange,
		AllowCustomModules: cfg.Selinux.AllowCustomModules,
		Reconcile:          reconcile,
	}
}

// triggerSelinuxSync serialises concurrent SELinux sync requests.
func triggerSelinuxSync(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	selinuxSyncMu.Lock()
	defer selinuxSyncMu.Unlock()
	syncAllSelinux(ctx, client, cfg, false, nil)
}

// syncAllSelinux applies the merged SELinux policies, reports per-policy
// compliance, keeps the reconcile ticker in step with the cache and
// refreshes the inventory after a change. Must be called with
// selinuxSyncMu held.
func syncAllSelinux(ctx context.Context, client *policyclient.Client, cfg *config.Config, reconcile bool, actors []policyclient.TamperProcess) {
	entries := selinuxCacheEntries()
	toSuppress := []string{cfg.Selinux.StatePath, cfg.Selinux.ConfigPath}
	if files, err := policy.ListBorSelinuxSources(cfg.Selinux.ModulesDir); err == nil {
		toSuppress = append(toSuppress, files...)
	}
	for _, name := range selinuxDesiredSourceNames(entries) {
		toSuppress = append(toSuppress, filepath.Join(cfg.Selinux.ModulesDir, name+".cil"))
	}
	// semodule rebuilds the policy for seconds and the state file is
	// written last: keep the watcher quiet for the whole run, then shorten
	// the window to the usual two seconds.
	suppressManagedWritesFor(cfg, cfg.Selinux.ToolTimeout+2*time.Second, toSuppress...)
	defer resetManagedWriteWindow(cfg, toSuppress...)
	defer updateWatcher(cfg)

	toolCtx, cancel := context.WithTimeout(ctx, cfg.Selinux.ToolTimeout)
	defer cancel()
	macQuietUntil.Store(time.Now().Add(cfg.Selinux.ToolTimeout + macSyncQuiet).UnixNano())
	result := policy.SyncSelinux(toolCtx, selinuxDeps(cfg), entries, selinuxSyncOptions(cfg, reconcile))
	macQuietUntil.Store(time.Now().Add(macSyncQuiet).UnixNano())
	if reconcile && len(result.Drifts) > 0 {
		go reportMacDrifts(ctx, client, result.Drifts, actors)
	}
	if len(entries) == 0 {
		stopSelinuxReconcilerLocked()
		log.Println("SELinux policies synced (0 policies): original values restored")
	} else {
		ensureSelinuxReconcilerLocked(ctx, client, cfg)
		for _, e := range entries {
			r := result.Policies[e.ID]
			log.Printf("SELinux policy %q synced: %s - %s", e.Name, r.Status.String(), r.Message)
			_ = client.ReportComplianceWithStatus(ctx, e.ID, r.Status, r.Message, r.Items)
		}
	}
	if result.Changed {
		go reportMacInventory(ctx, client, cfg)
	}
}

// selinuxDesiredSourceNames lists the module source files a sync may write.
func selinuxDesiredSourceNames(entries []policy.SelinuxEntry) []string {
	var names []string
	d := policy.MergeSelinuxPolicies(entries)
	if len(d.Permissive) > 0 {
		names = append(names, "bor-permissive")
	}
	if len(d.FileContexts) > 0 {
		names = append(names, "bor-filecontexts")
	}
	for name := range d.Custom {
		names = append(names, "bor-"+name)
	}
	return names
}

func ensureSelinuxReconcilerLocked(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	interval := cfg.Selinux.ReconcileInterval
	if interval <= 0 || selinuxReconcileCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	selinuxReconcileCancel = cancel
	log.Printf("SELinux reconcile enabled (every %v)", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				selinuxReconcileTick(tickCtx, client, cfg, nil)
			}
		}
	}()
}

func stopSelinuxReconcilerLocked() {
	if selinuxReconcileCancel != nil {
		selinuxReconcileCancel()
		selinuxReconcileCancel = nil
		log.Println("SELinux reconcile stopped")
	}
}

func stopSelinuxReconciler() {
	selinuxSyncMu.Lock()
	defer selinuxSyncMu.Unlock()
	stopSelinuxReconcilerLocked()
}

// selinuxReconcileTick re-runs the sync so a mode, boolean or module
// changed by hand returns to the desired state.
func selinuxReconcileTick(ctx context.Context, client *policyclient.Client, cfg *config.Config, actors []policyclient.TamperProcess) {
	selinuxSyncMu.Lock()
	defer selinuxSyncMu.Unlock()
	if ctx.Err() != nil || len(selinuxCacheEntries()) == 0 {
		return
	}
	syncAllSelinux(ctx, client, cfg, true, actors)
}

// selinuxManagedPaths returns the files the watcher guards for this type.
func selinuxManagedPaths(cfg *config.Config) []string {
	var paths []string
	if len(selinuxCacheEntries()) > 0 {
		if files, err := policy.ListBorSelinuxSources(cfg.Selinux.ModulesDir); err == nil {
			paths = append(paths, files...)
		}
		if cfg.Selinux.ConfigPath != "" {
			if _, err := os.Stat(cfg.Selinux.ConfigPath); err == nil {
				paths = append(paths, cfg.Selinux.ConfigPath)
			}
		}
	}
	if cfg.Selinux.StatePath != "" {
		if _, err := os.Stat(cfg.Selinux.StatePath); err == nil {
			paths = append(paths, cfg.Selinux.StatePath)
		}
	}
	return paths
}

// isSelinuxManagedPath reports whether a tampered path belongs to this type.
func isSelinuxManagedPath(cfg *config.Config, path string) bool {
	if path == cfg.Selinux.StatePath || (cfg.Selinux.ConfigPath != "" && path == cfg.Selinux.ConfigPath) {
		return true
	}
	return cfg.Selinux.ModulesDir != "" && strings.HasPrefix(path, cfg.Selinux.ModulesDir+string(filepath.Separator))
}
