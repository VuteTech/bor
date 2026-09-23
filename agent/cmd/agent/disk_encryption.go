// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/luks"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// diskEncCacheEntry holds a DiskEncryption policy with its binding priority
// and name.
type diskEncCacheEntry struct {
	id       string
	name     string
	priority int32
	policy   *pb.DiskEncryptionPolicy
}

// diskEncCache maps policy ID -> DiskEncryption policy for all active
// DiskEncryption policies.
var diskEncCache = make(map[string]diskEncCacheEntry)

// diskEncSnapshotStaging accumulates DiskEncryption policies during a SNAPSHOT.
var diskEncSnapshotStaging map[string]diskEncCacheEntry

// diskEncSyncMu ensures only one disk-encryption sync runs at a time
// (enrollments and initramfs rebuilds take a while and must not overlap or
// block the gRPC stream goroutine). The ticker takes the same mutex.
var diskEncSyncMu sync.Mutex

// diskEncTickerCancel stops the periodic ticker; nil when not running.
// Guarded by diskEncSyncMu.
var diskEncTickerCancel context.CancelFunc

// diskEncInventoryEnabled mirrors the server's disk_encryption_inventory
// agent setting: report the LUKS inventory even without a bound policy.
var diskEncInventoryEnabled bool

// diskEncTickInterval is the header re-read cadence; every
// diskEncFullSyncTicks-th tick runs the full task/TPM health pass.
const (
	diskEncTickInterval  = 15 * time.Minute
	diskEncFullSyncTicks = 24 // 24 × 15 min = 6 h
)

// luksConfig maps the agent configuration to the luks package settings.
func luksConfig(cfg *config.Config) *luks.Config {
	return &luks.Config{
		StateFile:    cfg.DiskEncryption.StateFile,
		BootstrapDir: cfg.DiskEncryption.BootstrapDir,
		RunDir:       cfg.DiskEncryption.RunDir,
		Cryptsetup:   cfg.DiskEncryption.Cryptsetup,
		Cryptenroll:  cfg.DiskEncryption.Cryptenroll,
		Clevis:       cfg.DiskEncryption.Clevis,
	}
}

// triggerDiskEncryptionSync serialises sync requests; every dispatch site
// calls this with `go` so the stream goroutine never blocks.
func triggerDiskEncryptionSync(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	diskEncSyncMu.Lock()
	defer diskEncSyncMu.Unlock()
	syncAllDiskEncryption(ctx, client, cfg)
}

// syncAllDiskEncryption merges the cached policies, reports the inventory
// (phase A: fast, read-only), enforces the protectors (phase B: mutations)
// and reports the final state and compliance. Must hold diskEncSyncMu.
func syncAllDiskEncryption(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	entries := make([]diskEncCacheEntry, 0, len(diskEncCache))
	ids := make([]string, 0, len(diskEncCache))
	for _, e := range diskEncCache {
		entries = append(entries, e)
		ids = append(ids, e.id)
	}
	lcfg := luksConfig(cfg)

	// Suppress the watcher for the files this sync may write (MANAGE mode).
	suppressManagedWrites(cfg, luks.DracutDropInPath, luks.CrypttabPath)
	defer updateWatcher(cfg)

	if len(entries) == 0 {
		stopDiskEncTickerLocked()
		// Removing the policy never removes protectors or escrowed keys:
		// only Bor's own dracut drop-in is withdrawn. Bor's crypttab
		// option tokens stay - removing tpm2-device=auto could strand a
		// TPM-only volume at the boot prompt.
		if changed, err := luks.WriteDracutDropIn(""); err != nil {
			log.Printf("disk encryption: remove dracut drop-in: %v", err)
		} else if changed {
			log.Println("disk encryption: removed the Bor dracut drop-in (no policies bound)")
		}
		reportDiskEncryptionInventory(ctx, client, cfg)
		return
	}

	// Ascending priority: the merge lets higher-priority policies win.
	sortDiskEncEntries(entries)
	policies := make([]*pb.DiskEncryptionPolicy, 0, len(entries))
	for _, e := range entries {
		policies = append(policies, e.policy)
	}
	merged := luks.MergePolicies(policies)

	report := func(status pb.ComplianceStatus, msg string, items []*pb.ComplianceItemResult) {
		for _, id := range ids {
			_ = client.ReportComplianceWithStatus(ctx, id, status, msg, items)
		}
	}

	if err := checkLuksTooling(lcfg); err != "" {
		log.Printf("disk encryption: %s", err)
		report(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err, nil)
		return
	}

	// ── Phase A: inventory + read-only checks (fast) ────────────────────
	inv, err := luks.BuildInventory(ctx, lcfg)
	if err != nil {
		log.Printf("disk encryption: inventory failed: %v", err)
		report(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "inventory failed: "+err.Error(), nil)
		return
	}
	tasks, err := client.ReportDiskEncryptionState(ctx, inv.ToProtoReport("", merged))
	if err != nil {
		log.Printf("disk encryption: state report failed: %v", err)
	}
	staticItems := luks.CheckStatic(inv, merged)
	if status, msg := luks.Rollup(staticItems); status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		report(status, msg+" (protectors being applied)", luks.ToProtoItems(staticItems))
	}

	// ── Phase B: mutations ──────────────────────────────────────────────
	enforceItems := luks.Enforce(ctx, lcfg, client, merged, tasks, inv, cfg.Server.Address)
	initramfsItems, _ := luks.EnforceInitramfs(ctx, merged, inv)

	// Final state: re-read headers and report both inventory and compliance.
	finalInv, err := luks.BuildInventory(ctx, lcfg)
	if err != nil {
		finalInv = inv
	} else if _, rerr := client.ReportDiskEncryptionState(ctx, finalInv.ToProtoReport("", merged)); rerr != nil {
		log.Printf("disk encryption: final state report failed: %v", rerr)
	}
	allItems := append(append(luks.CheckStatic(finalInv, merged), enforceItems...), initramfsItems...)
	status, msg := luks.Rollup(allItems)
	log.Printf("Disk encryption policies synced (%d policies, %d volumes): %s - %s",
		len(ids), len(finalInv.Volumes), status.String(), msg)
	report(status, msg, luks.ToProtoItems(allItems))

	ensureDiskEncTickerLocked(ctx, client, cfg)
}

// sortDiskEncEntries sorts ascending by priority (stable).
func sortDiskEncEntries(entries []diskEncCacheEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].priority < entries[j-1].priority; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// checkLuksTooling verifies cryptsetup is present ("" = ok).
func checkLuksTooling(lcfg *luks.Config) string {
	if err := luksLookPath(lcfg.CryptsetupBinary()); err != nil {
		return "cryptsetup is not installed on this node"
	}
	return ""
}

// luksLookPath is a small indirection for tests.
var luksLookPath = func(binary string) error {
	_, err := exec.LookPath(binary)
	return err
}

// reportDiskEncryptionInventory sends a read-only inventory report (no
// policy required). Server setting disk_encryption_inventory gates it.
func reportDiskEncryptionInventory(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	if !diskEncInventoryEnabled {
		return
	}
	lcfg := luksConfig(cfg)
	if luksLookPath(lcfg.CryptsetupBinary()) != nil {
		return // nothing useful to report without cryptsetup
	}
	inv, err := luks.BuildInventory(ctx, lcfg)
	if err != nil {
		log.Printf("disk encryption: inventory failed: %v", err)
		return
	}
	tasks, err := client.ReportDiskEncryptionState(ctx, inv.ToProtoReport("", nil))
	if err != nil {
		log.Printf("disk encryption: inventory report failed: %v", err)
		return
	}
	log.Printf("disk encryption: inventory reported (%d volumes)", len(inv.Volumes))
	if len(tasks) > 0 && len(diskEncCache) > 0 {
		go triggerDiskEncryptionSync(ctx, client, cfg)
	}
}

// ensureDiskEncTickerLocked starts the periodic ticker: every 15 minutes a
// cheap header re-read + inventory report (server-side drift detection),
// every 6 hours a full sync (tasks, TPM health). Must hold
// diskEncSyncMu; the ticker stops with the last policy.
func ensureDiskEncTickerLocked(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	if diskEncTickerCancel != nil {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	diskEncTickerCancel = cancel
	go runDiskEncTicker(tickCtx, client, cfg)
}

// stopDiskEncTickerLocked cancels the ticker. Must hold diskEncSyncMu.
func stopDiskEncTickerLocked() {
	if diskEncTickerCancel != nil {
		diskEncTickerCancel()
		diskEncTickerCancel = nil
	}
}

// runDiskEncTicker is the ticker goroutine.
func runDiskEncTicker(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	ticker := time.NewTicker(diskEncTickInterval)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ticks++
			if ticks%diskEncFullSyncTicks == 0 {
				triggerDiskEncryptionSync(ctx, client, cfg)
				continue
			}
			diskEncInventoryTick(ctx, client, cfg)
		}
	}
}

// diskEncInventoryTick re-reads the headers and reports the inventory; a
// returned task triggers a full sync.
func diskEncInventoryTick(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	diskEncSyncMu.Lock()
	defer diskEncSyncMu.Unlock()
	if len(diskEncCache) == 0 {
		return
	}
	lcfg := luksConfig(cfg)
	inv, err := luks.BuildInventory(ctx, lcfg)
	if err != nil {
		log.Printf("disk encryption: ticker inventory failed: %v", err)
		return
	}
	entries := make([]*pb.DiskEncryptionPolicy, 0, len(diskEncCache))
	for _, e := range diskEncCache {
		entries = append(entries, e.policy)
	}
	tasks, err := client.ReportDiskEncryptionState(ctx, inv.ToProtoReport("", luks.MergePolicies(entries)))
	if err != nil {
		log.Printf("disk encryption: ticker report failed: %v", err)
		return
	}
	if len(tasks) > 0 {
		log.Printf("disk encryption: %d pending task(s); running a full sync", len(tasks))
		syncAllDiskEncryption(ctx, client, cfg)
	}
}

// stopDiskEncTicker cancels the ticker (agent shutdown).
func stopDiskEncTicker() {
	diskEncSyncMu.Lock()
	defer diskEncSyncMu.Unlock()
	stopDiskEncTickerLocked()
}

// diskEncManagedPaths returns the watcher paths for MANAGE-mode policies:
// the Bor dracut drop-in (when present) and /etc/crypttab.
func diskEncManagedPaths() []string {
	if len(diskEncCache) == 0 {
		return nil
	}
	manage := false
	for _, e := range diskEncCache {
		if e.policy.GetInitramfsMode() == pb.InitramfsMode_INITRAMFS_MODE_MANAGE {
			manage = true
			break
		}
	}
	if !manage {
		return nil
	}
	paths := []string{luks.CrypttabPath}
	if _, err := os.Stat(luks.DracutDropInPath); err == nil {
		paths = append(paths, luks.DracutDropInPath)
	}
	return paths
}

// logDiskEncryptionProbe writes the startup probe line.
func logDiskEncryptionProbe(ctx context.Context, cfg *config.Config) {
	lcfg := luksConfig(cfg)
	p := luks.CollectPlatform(ctx, lcfg)
	tpm := "absent"
	if p.TPM2Present {
		tpm = "present"
	}
	sb := "n/a"
	switch p.SecureBoot {
	case pb.SecureBootState_SECURE_BOOT_STATE_ENABLED:
		sb = "enabled"
	case pb.SecureBootState_SECURE_BOOT_STATE_DISABLED:
		sb = "disabled"
	case pb.SecureBootState_SECURE_BOOT_STATE_SETUP_MODE:
		sb = "setup mode"
	}
	clevis := p.ClevisVersion
	if clevis == "" {
		clevis = "absent"
	}
	log.Printf("Disk encryption probe: cryptsetup %s, systemd %s, clevis %s; TPM2 %s; Secure Boot %s; initramfs: %s",
		orUnknown(p.CryptsetupVersion), orUnknown(p.SystemdVersion), clevis, tpm, sb, p.InitramfsGenerator)
}

func orUnknown(s string) string {
	if s == "" {
		return "absent"
	}
	return s
}
