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

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
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

func systemdSyncOptions(cfg *config.Config) policy.SystemdSyncOptions {
	return policy.SystemdSyncOptions{
		SystemDir: cfg.Systemd.SystemDir,
		StatePath: cfg.Systemd.StatePath,
		Analyzer:  systemd.NewAnalyzer(cfg.Systemd.AnalyzeBinary),
	}
}

// triggerSystemdSync serialises concurrent Systemd sync requests.
func triggerSystemdSync(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	systemdSyncMu.Lock()
	defer systemdSyncMu.Unlock()
	syncAllSystemd(ctx, client, cfg)
}

// syncAllSystemd applies the merged Systemd policies, reports per-policy
// compliance, keeps the reconcile ticker in step with the cache and
// refreshes the unit catalogue after a change. Must be called with
// systemdSyncMu held.
func syncAllSystemd(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	entries := systemdCacheEntries()
	desired := policy.MergeSystemdPolicies(entries)

	// Suppress the watcher for the drop-ins this sync writes or removes
	// and for the state file.
	existing, _ := policy.ListBorManagedSystemdFiles(cfg.Systemd.SystemDir)
	toSuppress := append(policy.SystemdDesiredFiles(cfg.Systemd.SystemDir, desired), existing...)
	toSuppress = append(toSuppress, cfg.Systemd.StatePath)
	suppressManagedWrites(cfg, toSuppress...)
	defer updateWatcher(cfg)

	var mgr systemd.Manager
	if bus := systemdManager(cfg); bus != nil {
		mgr = bus
	}
	result := policy.SyncSystemd(ctx, mgr, entries, systemdSyncOptions(cfg))

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
	syncAllSystemd(ctx, client, cfg)
}

// reportSystemdCatalogue scans the unit inventory and sends it to the
// server. It reports whether the report was delivered.
func reportSystemdCatalogue(ctx context.Context, client *policyclient.Client, cfg *config.Config) bool {
	bus := systemdManager(cfg)
	if bus == nil {
		return false
	}
	units, err := policy.ScanSystemdUnits(ctx, bus)
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
		if files, err := policy.ListBorManagedSystemdFiles(cfg.Systemd.SystemDir); err == nil {
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

// isSystemdManagedPath reports whether a tampered path belongs to this type.
func isSystemdManagedPath(cfg *config.Config, path string) bool {
	if cfg.Systemd.StatePath != "" && path == cfg.Systemd.StatePath {
		return true
	}
	dir := cfg.Systemd.SystemDir
	if dir == "" {
		dir = policy.SystemdSystemDir
	}
	if !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return false
	}
	return filepath.Base(path) == policy.SystemdDropInName && strings.HasSuffix(filepath.Dir(path), ".d")
}
