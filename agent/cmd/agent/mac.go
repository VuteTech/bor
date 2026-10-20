// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/apparmor"
	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/mac"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	"github.com/VuteTech/Bor/agent/internal/selinux"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/macrules"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// macInventoryMu serialises inventory reports; the inventory reads the
// journal and the policy store, which takes a few seconds.
var macInventoryMu sync.Mutex

// macInventoryCancel stops the periodic inventory ticker.
var (
	macTickerMu        sync.Mutex
	macInventoryCancel context.CancelFunc
)

// macActionMu serialises on-demand proposals.
var macActionMu sync.Mutex

// macLSM returns the node's LSM and mode for the heartbeat.
func macLSM() (lsm, mode string) {
	return mac.Summary(mac.DefaultPaths())
}

func selinuxDeps(cfg *config.Config) *policy.SelinuxDeps {
	paths := selinux.DefaultPaths()
	if cfg.Selinux.ConfigPath != "" {
		paths.Config = cfg.Selinux.ConfigPath
	}
	return &policy.SelinuxDeps{Paths: paths, Tools: selinux.NewTools(), Mac: mac.DefaultPaths()}
}

func apparmorDeps(cfg *config.Config) *policy.ApparmorDeps {
	paths := apparmor.DefaultPaths(cfg.Apparmor.ProfilesDir)
	deps := &policy.ApparmorDeps{Paths: paths, Parser: apparmor.NewParser(cfg.Apparmor.ParserBinary, paths.ProfilesDir), Mac: mac.DefaultPaths()}
	if bus := systemdManager(cfg); bus != nil {
		deps.Systemd = bus
	}
	return deps
}

// reportMacInventory scans the active LSM and sends the inventory with the
// denial summary. It reports whether the report was delivered.
func reportMacInventory(ctx context.Context, client *policyclient.Client, cfg *config.Config) bool {
	macInventoryMu.Lock()
	defer macInventoryMu.Unlock()
	info := mac.Detect(mac.DefaultPaths())
	req := &pb.ReportMacInventoryRequest{Lsm: info.Active()}
	switch req.Lsm {
	case "selinux":
		inv := policy.ScanSelinuxInventory(ctx, selinuxDeps(cfg))
		req.Selinux, req.Booleans, req.Modules = inv.Status, inv.Booleans, inv.Modules
	case "apparmor":
		state, err := policy.LoadApparmorState(cfg.Apparmor.StatePath)
		if err != nil {
			state = &policy.ApparmorState{}
		}
		inv := policy.ScanApparmorInventory(ctx, apparmorDeps(cfg), state)
		req.Apparmor, req.Profiles = inv.Status, inv.Profiles
	}
	if cfg.Mac.CollectDenials && req.Lsm != "none" {
		since := time.Now().Add(-cfg.Mac.DenialWindow)
		if records, err := mac.NewCollector().Records(ctx, since); err == nil {
			req.Denials = mac.Group(records, 50)
			req.DenialsSince = timestamppb.New(since)
		} else {
			log.Printf("mac: denial collection skipped: %v", err)
		}
	}
	if err := client.ReportMacInventory(ctx, req); err != nil {
		log.Printf("mac: ReportMacInventory failed (non-fatal): %v", err)
		return false
	}
	log.Printf("mac: reported %s inventory (%d booleans, %d modules, %d profiles, %d denial groups)", req.Lsm, len(req.Booleans), len(req.Modules), len(req.Profiles), len(req.Denials))
	return true
}

// startMacInventoryTicker re-sends the inventory periodically while the
// agent runs, so the denial summary stays current.
func startMacInventoryTicker(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	macTickerMu.Lock()
	defer macTickerMu.Unlock()
	if macInventoryCancel != nil || cfg.Mac.InventoryInterval <= 0 {
		return
	}
	tickCtx, cancel := context.WithCancel(ctx)
	macInventoryCancel = cancel
	go func() {
		ticker := time.NewTicker(cfg.Mac.InventoryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				reportMacInventory(tickCtx, client, cfg)
			}
		}
	}()
}

func stopMacInventoryTicker() {
	macTickerMu.Lock()
	defer macTickerMu.Unlock()
	if macInventoryCancel != nil {
		macInventoryCancel()
		macInventoryCancel = nil
	}
}

// reportMacDrifts records the corrections of a reconcile pass as tamper
// events. SELinux records carry the actor's login uid; the journal
// follower attaches it when it triggered the pass.
func reportMacDrifts(ctx context.Context, client *policyclient.Client, drifts []policy.MacDrift, actors []policyclient.TamperProcess) {
	for _, d := range drifts {
		log.Printf("Tamper protection: %s %s", d.Resource, d.Description)
		if err := client.ReportUnitTamperEvent(ctx, d.Resource, d.Description, actors); err != nil {
			log.Printf("Failed to report drift to server: %v", err)
		}
	}
}

// runMacAction answers a proposal request from the node page: the recent
// denials of one SELinux domain through audit2allow (and the CIL
// conversion when the tools exist), or AppArmor rules derived from the
// denials of one profile.
func runMacAction(ctx context.Context, client *policyclient.Client, cfg *config.Config, action *pb.MacAction) {
	report := func(ok bool, msg, text string) {
		log.Printf("mac action %s (%s %s): success=%v %s", action.GetId(), action.GetKind(), action.GetSubject(), ok, msg)
		if err := client.ReportMacActionResult(ctx, action.GetId(), ok, msg, text); err != nil {
			log.Printf("Failed to report mac action result: %v", err)
		}
	}
	if !cfg.Mac.AllowActions {
		report(false, "proposals are disabled on this node (mac.allow_actions is off)", "")
		return
	}
	macActionMu.Lock()
	defer macActionMu.Unlock()
	hours := action.GetWindowHours()
	if hours <= 0 || hours > 24*14 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	records, err := mac.NewCollector().Records(ctx, since)
	if err != nil {
		report(false, err.Error(), "")
		return
	}
	switch action.GetKind() {
	case pb.MacActionKind_MAC_ACTION_SELINUX_PROPOSE_MODULE:
		if err := macrules.ValidateDomain(action.GetSubject()); err != nil {
			report(false, err.Error(), "")
			return
		}
		subject := mac.ForSubject(records, "selinux", action.GetSubject())
		if len(subject) == 0 {
			report(false, fmt.Sprintf("no denials of %s in the last %d hours", action.GetSubject(), hours), "")
			return
		}
		tools := selinux.NewTools()
		if !tools.Present(tools.Audit2allowBin) {
			report(false, "audit2allow is not installed on this node (policycoreutils-python-utils)", "")
			return
		}
		module := "bor-" + strings.TrimSuffix(action.GetSubject(), "_t") + "-proposal"
		te, err := tools.Audit2allow(ctx, mac.AuditLines(subject), module)
		if err != nil {
			report(false, err.Error(), "")
			return
		}
		text := "# Type Enforcement source proposed by audit2allow from " + fmt.Sprint(len(subject)) + " denials.\n" + te
		msg := fmt.Sprintf("proposal from %d denials", len(subject))
		if cil, err := tools.TEToCIL(ctx, te, module); err == nil {
			text = "; CIL converted from the audit2allow proposal; review every rule before\n; adding it as a custom module.\n" + cil + "\n\n" + text
			msg += " (CIL and Type Enforcement)"
		} else {
			msg += " (Type Enforcement only: " + err.Error() + ")"
		}
		report(true, msg, text)
	case pb.MacActionKind_MAC_ACTION_APPARMOR_PROPOSE_RULES:
		subject := mac.ForSubject(records, "apparmor", action.GetSubject())
		if len(subject) == 0 {
			report(false, fmt.Sprintf("no denials of %s in the last %d hours", action.GetSubject(), hours), "")
			return
		}
		report(true, fmt.Sprintf("rules from %d records", len(subject)), mac.ProposeApparmorRules(subject))
	default:
		report(false, "unknown action kind", "")
	}
}

// macAuditFollower follows the journal's audit records and reconciles the
// SELinux or AppArmor state within seconds of a manual change (setenforce,
// setsebool, aa-complain, a profile unload). Our own changes are ignored
// for a few seconds after each sync.
func macAuditFollower(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	collector := mac.NewCollector()
	if !collector.Available() {
		return
	}
	last := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		since := last
		last = time.Now()
		if time.Now().UnixNano() < macQuietUntil.Load() {
			continue
		}
		records, err := collector.Records(ctx, since.Add(-time.Second))
		if err != nil {
			continue
		}
		selinuxChange, apparmorChange := false, false
		var actors []policyclient.TamperProcess
		for _, r := range records {
			switch {
			case strings.HasPrefix(r.Raw, "type=MAC_STATUS") || strings.HasPrefix(r.Raw, "type=MAC_CONFIG_CHANGE") || strings.HasPrefix(r.Raw, "type=MAC_POLICY_LOAD"):
				selinuxChange = true
				if uid := r.Fields["auid"]; uid != "" && uid != "4294967295" {
					actors = append(actors, policyclient.TamperProcess{User: "uid " + uid})
				}
			case r.Kind == "apparmor" && r.Type == "STATUS" && r.Fields["info"] == "" && r.Fields["comm"] != "":
				apparmorChange = true
			}
		}
		if selinuxChange && cfg.Selinux.FollowAudit && len(selinuxCacheEntries()) > 0 {
			log.Println("selinux: audit records show a policy or mode change; reconciling")
			go selinuxReconcileTick(ctx, client, cfg, dedupeActors(actors))
		}
		if apparmorChange && cfg.Apparmor.FollowAudit && len(apparmorCacheEntries()) > 0 {
			log.Println("apparmor: audit records show a profile change; reconciling")
			go apparmorReconcileTick(ctx, client, cfg)
		}
	}
}

func dedupeActors(in []policyclient.TamperProcess) []policyclient.TamperProcess {
	seen := map[string]bool{}
	var out []policyclient.TamperProcess
	for _, a := range in {
		if !seen[a.User] {
			seen[a.User] = true
			out = append(out, a)
		}
	}
	return out
}
