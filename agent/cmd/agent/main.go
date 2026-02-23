// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/notify"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	"github.com/VuteTech/Bor/agent/internal/sysinfo"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/proto"
)

const defaultConfigPath = "/etc/bor/config.yaml"

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

// kconfigCache maps policy ID → proto entries for all active KConfig policies.
// It is maintained across streaming events so that a full re-merge and
// sync can be performed whenever any single policy changes or is deleted.
var kconfigCache = make(map[string][]*pb.KConfigEntry)

// kconfigSnapshotStaging accumulates KConfig entries during a SNAPSHOT.
// It is nil when not inside a snapshot sequence.
var kconfigSnapshotStaging map[string][]*pb.KConfigEntry

// kdeNotifier handles desktop notifications and app reconfigure via D-Bus.
var kdeNotifier = notify.New()

// notifyConfig holds the current server-provided notification settings.
// It is refreshed on each stream connect.
var notifyConfig = notify.NotifyConfig{
	Enabled:  true,
	Cooldown: 5 * time.Minute,
	Message:  "Desktop policies have been updated. Please log out and log back in for all changes to take effect.",
}

// firefoxCache maps policy ID → proto policy for all active Firefox policies.
var firefoxCache = make(map[string]*pb.FirefoxPolicy)

// firefoxSnapshotStaging accumulates Firefox proto policies during a SNAPSHOT.
var firefoxSnapshotStaging map[string]*pb.FirefoxPolicy

// firefoxNotifier handles desktop notifications for Firefox policy changes.
var firefoxNotifier = notify.New()

// firefoxNotifyConfig holds Firefox-specific notification settings.
var firefoxNotifyConfig = notify.NotifyConfig{
	Enabled:  true,
	Cooldown: 5 * time.Minute,
	Message:  "Firefox policies have been updated. Please restart Firefox for all changes to take effect.",
}

// chromeCache maps policy ID → content JSON for all active Chrome policies.
var chromeCache = make(map[string]string)

// chromeSnapshotStaging accumulates Chrome contents during a SNAPSHOT.
// It is nil when not inside a snapshot sequence.
var chromeSnapshotStaging map[string]string

// chromeNotifier handles desktop notifications for Chrome policy changes.
var chromeNotifier = notify.New()

// chromeNotifyConfig holds Chrome-specific notification settings.
var chromeNotifyConfig = notify.NotifyConfig{
	Enabled:  true,
	Cooldown: 5 * time.Minute,
	Message:  "Chrome/Chromium policies have been updated. Please restart your browser for all changes to take effect.",
}

func main() {
	configPath := flag.String("config", defaultConfigPath, "path to configuration file")
	enrollToken := flag.String("token", "", "one-time enrollment token (from Node Groups UI)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Bor Agent starting")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Server: %s", cfg.Server.Address)
	log.Printf("Client ID: %s", cfg.Agent.ClientID)

	// ─── Enrollment / mTLS bootstrap ──────────────────────────────────
	paths := policyclient.DefaultPaths(cfg.Enrollment.DataDir)

	if !policyclient.IsEnrolled(paths) {
		if *enrollToken == "" {
			log.Fatal("Agent is not enrolled and no enrollment token was provided.\n" +
				"Run with: bor-agent --token <TOKEN>\n" +
				"Generate a token from the Node Groups page in the Bor web UI.")
		}
		log.Println("Not yet enrolled – starting enrollment...")
		if err := policyclient.Enroll(
			cfg.Server.Address,
			*enrollToken,
			cfg.Agent.ClientID,
			cfg.Server.InsecureSkipVerify,
			paths,
		); err != nil {
			log.Fatalf("Enrollment failed: %v", err)
		}
		fmt.Printf(`
Enrollment successful. Certificates stored in %s

To enable and start the Bor agent service, run:

    sudo systemctl enable --now bor-agent

To check the agent status:

    sudo systemctl status bor-agent

To follow the agent logs:

    sudo journalctl -u bor-agent -f

`, cfg.Enrollment.DataDir)
		os.Exit(0)
	} else if *enrollToken != "" {
		log.Println("Agent is already enrolled – ignoring --token flag")
	} else {
		log.Println("Agent is enrolled – using mTLS credentials")
	}

	// ─── Connect with mTLS credentials ────────────────────────────────
	client, err := policyclient.New(
		cfg.Server.Address,
		cfg.Agent.ClientID,
		paths.CACert,      // CA cert received during enrollment
		paths.CertFile,    // agent client cert signed by CA
		paths.KeyFile,     // agent private key
		false,             // never skip verify after enrollment – we have the CA cert
	)
	if err != nil {
		log.Fatalf("Failed to create policy client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s, shutting down...", sig)
		cancel()
	}()

	// Run the policy enforcement loop — prefer streaming, fall back to polling.
	runStreamingLoop(ctx, client, cfg)

	log.Println("Bor Agent stopped")
}

// runStreamingLoop connects to the server's SubscribePolicyUpdates
// stream and applies policies as they arrive. On stream failure it
// reconnects with exponential backoff. The last known revision is
// sent on each reconnect so the server can send a delta or snapshot.
func runStreamingLoop(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
	var lastRevision int64
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("Connecting to policy stream (last_known_revision=%d)...", lastRevision)

		// Fetch notification settings from the server on each connect.
		if agentCfg, err := client.GetAgentConfig(ctx); err != nil {
			log.Printf("Failed to fetch agent config (using defaults): %v", err)
		} else {
			notifyConfig = notify.NotifyConfig{
				Enabled:  agentCfg.NotifyUsers,
				Cooldown: time.Duration(agentCfg.NotifyCooldown) * time.Second,
				Message:  agentCfg.NotifyMessage,
			}
			log.Printf("Agent notification config: enabled=%v cooldown=%v", notifyConfig.Enabled, notifyConfig.Cooldown)
			firefoxNotifyConfig = notify.NotifyConfig{
				Enabled:  agentCfg.NotifyUsers,
				Cooldown: time.Duration(agentCfg.NotifyCooldown) * time.Second,
				Message:  agentCfg.NotifyMessageFirefox,
			}
			chromeNotifyConfig = notify.NotifyConfig{
				Enabled:  agentCfg.NotifyUsers,
				Cooldown: time.Duration(agentCfg.NotifyCooldown) * time.Second,
				Message:  agentCfg.NotifyMessageChrome,
			}
		}

		// Send heartbeat on connect to report current metadata.
		go sendHeartbeat(ctx, client)

		var postInitialSync bool
		err := client.SubscribePolicyUpdates(ctx, lastRevision,
			func(updateType string, pi *policyclient.PolicyInfo, revision int64, snapshotComplete bool) {
				// Don't let METADATA_REQUEST overwrite the last known revision.
				if updateType != "METADATA_REQUEST" {
					lastRevision = revision
				}
				handlePolicyUpdate(ctx, client, cfg, updateType, pi, snapshotComplete, &postInitialSync)
			},
		)

		if ctx.Err() != nil {
			return // parent context cancelled — shutting down
		}

		log.Printf("Policy stream disconnected: %v — reconnecting in %v", err, backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff capped at 60 s.
		backoff = backoff * 2
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
}

// handlePolicyUpdate processes a single event from the streaming RPC.
// postInitialSync tracks whether the first SNAPSHOT for this connection has
// already completed; subsequent SNAPSHOTs are server-side resyncs triggered
// by admin changes and should produce notifications if the content changed.
func handlePolicyUpdate(ctx context.Context, client *policyclient.Client, cfg *config.Config, updateType string, pi *policyclient.PolicyInfo, snapshotComplete bool, postInitialSync *bool) {
	switch updateType {
	case "METADATA_REQUEST":
		// Server is requesting fresh system metadata.
		go sendHeartbeat(ctx, client)
		return

	case "SNAPSHOT":
		if pi == nil {
			if snapshotComplete {
				log.Println("Received empty snapshot (no policies assigned)")
				firefoxChanged := len(firefoxCache) > 0
				hadKconfigPolicies := len(kconfigCache) > 0
				chromeChanged := len(chromeCache) > 0
				kconfigCache = make(map[string][]*pb.KConfigEntry)
				kconfigSnapshotStaging = nil
				firefoxCache = make(map[string]*pb.FirefoxPolicy)
				firefoxSnapshotStaging = nil
				chromeCache = make(map[string]string)
				chromeSnapshotStaging = nil
				syncAllKConfig(ctx, client, cfg)
				syncAllFirefox(ctx, client, cfg)
				syncAllChrome(ctx, client, cfg)
				if *postInitialSync {
					if hadKconfigPolicies {
						kdeNotifier.ScheduleNotification(notifyConfig, map[string]bool{"kwinrc": true, "kdeglobals": true})
					}
					if firefoxChanged {
						firefoxNotifier.ScheduleNotification(firefoxNotifyConfig, map[string]bool{"policies.json": true})
					}
					if chromeChanged {
						chromeNotifier.ScheduleNotification(chromeNotifyConfig, map[string]bool{"bor_managed.json": true})
					}
				}
				*postInitialSync = true
			}
			return
		}

		log.Printf("Policy update: type=%s id=%s name=%s version=%d",
			updateType, pi.ID, pi.Name, pi.Version)

		switch pi.Type {
		case "Firefox":
			if firefoxSnapshotStaging == nil {
				firefoxSnapshotStaging = make(map[string]*pb.FirefoxPolicy)
			}
			firefoxSnapshotStaging[pi.ID] = pi.FirefoxPolicy
		case "Chrome":
			if chromeSnapshotStaging == nil {
				chromeSnapshotStaging = make(map[string]string)
			}
			chromeSnapshotStaging[pi.ID] = pi.Content
		case "Kconfig":
			if kconfigSnapshotStaging == nil {
				kconfigSnapshotStaging = make(map[string][]*pb.KConfigEntry)
			}
			kconfigSnapshotStaging[pi.ID] = pi.KConfigEntries
		default:
			log.Printf("Unknown policy type %q for policy %s, skipping", pi.Type, pi.Name)
			_ = client.ReportCompliance(ctx, pi.ID, false,
				"unsupported policy type: "+pi.Type)
		}

		if snapshotComplete {
			// Compare Firefox and Chrome content before swapping to detect changes.
			firefoxChanged := !firefoxCachesEqual(firefoxCache, firefoxSnapshotStaging)
			chromeChanged := !chromeCachesEqual(chromeCache, chromeSnapshotStaging)

			// Swap KConfig staging into cache.
			if kconfigSnapshotStaging != nil {
				kconfigCache = kconfigSnapshotStaging
			} else {
				kconfigCache = make(map[string][]*pb.KConfigEntry)
			}
			kconfigSnapshotStaging = nil

			// Swap Firefox staging into cache.
			if firefoxSnapshotStaging != nil {
				firefoxCache = firefoxSnapshotStaging
			} else {
				firefoxCache = make(map[string]*pb.FirefoxPolicy)
			}
			firefoxSnapshotStaging = nil

			// Swap Chrome staging into cache.
			if chromeSnapshotStaging != nil {
				chromeCache = chromeSnapshotStaging
			} else {
				chromeCache = make(map[string]string)
			}
			chromeSnapshotStaging = nil

			kconfigChanged := syncAllKConfig(ctx, client, cfg)
			syncAllFirefox(ctx, client, cfg)
			syncAllChrome(ctx, client, cfg)

			if *postInitialSync {
				// Resync from a live admin change — notify if content changed.
				if len(kconfigChanged) > 0 {
					kdeNotifier.ScheduleNotification(notifyConfig, kconfigChanged)
				}
				if firefoxChanged {
					firefoxNotifier.ScheduleNotification(firefoxNotifyConfig, map[string]bool{"policies.json": true})
				}
				if chromeChanged {
					chromeNotifier.ScheduleNotification(chromeNotifyConfig, map[string]bool{"bor_managed.json": true})
				}
			}
			*postInitialSync = true
		}

	case "CREATED", "UPDATED":
		if pi == nil {
			return
		}
		log.Printf("Policy update: type=%s id=%s name=%s version=%d",
			updateType, pi.ID, pi.Name, pi.Version)

		switch pi.Type {
		case "Firefox":
			firefoxCache[pi.ID] = pi.FirefoxPolicy
			if syncAllFirefox(ctx, client, cfg) {
				firefoxNotifier.ScheduleNotification(firefoxNotifyConfig, map[string]bool{"policies.json": true})
			}
		case "Chrome":
			chromeCache[pi.ID] = pi.Content
			if syncAllChrome(ctx, client, cfg) {
				chromeNotifier.ScheduleNotification(chromeNotifyConfig, map[string]bool{"bor_managed.json": true})
			}
		case "Kconfig":
			kconfigCache[pi.ID] = pi.KConfigEntries
			if changed := syncAllKConfig(ctx, client, cfg); len(changed) > 0 {
				kdeNotifier.ScheduleNotification(notifyConfig, changed)
			}
		default:
			log.Printf("Unknown policy type %q for policy %s, skipping", pi.Type, pi.Name)
			_ = client.ReportCompliance(ctx, pi.ID, false,
				"unsupported policy type: "+pi.Type)
		}

	case "DELETED":
		if pi == nil {
			return
		}
		log.Printf("Policy update: type=%s id=%s name=%s version=%d",
			updateType, pi.ID, pi.Name, pi.Version)

		if _, ok := kconfigCache[pi.ID]; ok {
			delete(kconfigCache, pi.ID)
			if changed := syncAllKConfig(ctx, client, cfg); len(changed) > 0 {
				kdeNotifier.ScheduleNotification(notifyConfig, changed)
			}
		} else if _, ok := firefoxCache[pi.ID]; ok {
			delete(firefoxCache, pi.ID)
			if syncAllFirefox(ctx, client, cfg) {
				firefoxNotifier.ScheduleNotification(firefoxNotifyConfig, map[string]bool{"policies.json": true})
			}
		} else if _, ok := chromeCache[pi.ID]; ok {
			delete(chromeCache, pi.ID)
			if syncAllChrome(ctx, client, cfg) {
				chromeNotifier.ScheduleNotification(chromeNotifyConfig, map[string]bool{"bor_managed.json": true})
			}
		} else {
			log.Printf("Policy %s deleted (not in any policy cache)", pi.ID)
		}
	}
}

// firefoxCachesEqual returns true when two Firefox policy caches contain
// identical policy IDs and proto content. Used to detect whether a
// SNAPSHOT resync actually changed the Firefox policy set.
func firefoxCachesEqual(a, b map[string]*pb.FirefoxPolicy) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if !proto.Equal(va, vb) {
			return false
		}
	}
	return true
}

// chromeCachesEqual returns true when two Chrome policy caches contain
// identical policy IDs and content strings. Used to detect whether a
// SNAPSHOT resync actually changed the Chrome policy set.
func chromeCachesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

// syncAllKConfig re-merges all cached KConfig policies and syncs the
// resulting files to disk. When the cache is empty, SyncKConfigFiles
// restores all previously managed files from backups.
//
// Returns the set of written file basenames (nil when nothing was
// written). The caller decides whether to schedule a notification.
func syncAllKConfig(ctx context.Context, client *policyclient.Client, cfg *config.Config) map[string]bool {
	var allEntries []*pb.KConfigEntry
	var ids []string
	for id, entries := range kconfigCache {
		allEntries = append(allEntries, entries...)
		ids = append(ids, id)
	}

	files, err := policy.MergeKConfigEntries(allEntries)
	if err != nil {
		log.Printf("Error merging KConfig policies: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to merge policies: "+err.Error())
		}
		return nil
	}

	if err := policy.SyncKConfigFiles(cfg.KConfig.ConfigPath, files); err != nil {
		log.Printf("Error syncing KConfig files: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to sync KConfig files: "+err.Error())
		}
		return nil
	}

	log.Printf("KConfig policies synced to %s (%d policies, %d files)", cfg.KConfig.ConfigPath, len(ids), len(files))
	for _, id := range ids {
		_ = client.ReportCompliance(ctx, id, true, "Deployed")
	}

	if len(files) == 0 {
		return nil
	}
	changedFiles := make(map[string]bool, len(files))
	for name := range files {
		changedFiles[name] = true
	}
	return changedFiles
}

// syncAllFirefox re-merges all cached Firefox proto policies and syncs
// the resulting policies.json to disk. When the cache is empty,
// SyncFirefoxPoliciesFromProto restores the original file from backup.
//
// Returns true when the sync succeeded (for notification scheduling).
func syncAllFirefox(ctx context.Context, client *policyclient.Client, cfg *config.Config) bool {
	var policies []*pb.FirefoxPolicy
	var ids []string
	for id, pol := range firefoxCache {
		policies = append(policies, pol)
		ids = append(ids, id)
	}

	if err := policy.SyncFirefoxPoliciesFromProto(cfg.Firefox.PoliciesPath, policies); err != nil {
		log.Printf("Error syncing Firefox policies: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to sync Firefox policies: "+err.Error())
		}
		return false
	}

	// Flatpak Firefox: write to the system-wide extension directory.
	// This is best-effort — Flatpak Firefox may not be installed.
	if cfg.Firefox.FlatpakPoliciesPath != "" {
		if err := policy.SyncFirefoxFlatpakPoliciesFromProto(cfg.Firefox.FlatpakPoliciesPath, policies); err != nil {
			log.Printf("Warning: failed to sync Flatpak Firefox policies: %v", err)
		} else {
			log.Printf("Flatpak Firefox policies synced to %s", cfg.Firefox.FlatpakPoliciesPath)
		}
	}

	log.Printf("Firefox policies synced to %s (%d policies)", cfg.Firefox.PoliciesPath, len(ids))
	for _, id := range ids {
		_ = client.ReportCompliance(ctx, id, true, "Deployed")
	}
	return true
}

// sendHeartbeat collects current system metadata and sends it to the server.
func sendHeartbeat(ctx context.Context, client *policyclient.Client) {
	si := sysinfo.Collect()

	desktopEnvs := make([]string, 0, len(si.DesktopEnvs))
	for _, de := range si.DesktopEnvs {
		desktopEnvs = append(desktopEnvs, de.String())
	}

	info := &policyclient.NodeInfo{
		FQDN:         si.FQDN,
		IPAddress:    si.IPAddress,
		OSName:       si.OS.Name,
		OSVersion:    si.OS.Version,
		DesktopEnvs:  desktopEnvs,
		AgentVersion: Version,
		MachineID:    si.MachineID,
	}

	if err := client.Heartbeat(ctx, info); err != nil {
		log.Printf("Heartbeat failed: %v", err)
	} else {
		log.Printf("Heartbeat sent (os=%s %s, de=%v)", info.OSName, info.OSVersion, info.DesktopEnvs)
	}
}

// syncAllChrome re-merges all cached Chrome policy contents and syncs
// bor_managed.json to each configured Chrome/Chromium policy directory.
// Returns true when the primary sync succeeded (for notification scheduling).
func syncAllChrome(ctx context.Context, client *policyclient.Client, cfg *config.Config) bool {
	var contents []string
	var ids []string
	for id, content := range chromeCache {
		contents = append(contents, content)
		ids = append(ids, id)
	}

	// Write to all configured Chrome/Chromium paths.
	chromePaths := []string{
		cfg.Chrome.ChromePoliciesPath,
		cfg.Chrome.ChromiumPoliciesPath,
		cfg.Chrome.ChromiumBrowserPoliciesPath,
	}

	success := true
	for _, p := range chromePaths {
		if p == "" {
			continue
		}
		if err := policy.SyncChromeDir(p, contents); err != nil {
			log.Printf("Error syncing Chrome policies to %s: %v", p, err)
			success = false
		}
	}

	// Flatpak Chromium is best-effort — log warning but don't fail.
	if cfg.Chrome.FlatpakChromiumPoliciesPath != "" {
		if err := policy.SyncChromeDir(cfg.Chrome.FlatpakChromiumPoliciesPath, contents); err != nil {
			log.Printf("Warning: failed to sync Flatpak Chromium policies: %v", err)
		} else if len(contents) > 0 {
			log.Printf("Flatpak Chromium policies synced to %s", cfg.Chrome.FlatpakChromiumPoliciesPath)
		}
	}

	if success {
		log.Printf("Chrome policies synced (%d policies)", len(ids))
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, true, "Deployed")
		}
	} else {
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to sync Chrome policies")
		}
	}
	return success
}

