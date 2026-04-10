// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package main is the entry point for the Bor agent daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/filewatcher"
	"github.com/VuteTech/Bor/agent/internal/notify"
	"github.com/VuteTech/Bor/agent/internal/policy"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	"github.com/VuteTech/Bor/agent/internal/procinfo"
	"github.com/VuteTech/Bor/agent/internal/sysinfo"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/proto"
)

const defaultConfigPath = "/etc/bor/config.yaml"

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

// kconfigCache maps policy ID → typed KConfig policy for all active Kconfig policies.
// It is maintained across streaming events so that a full re-merge and
// sync can be performed whenever any single policy changes or is deleted.
var kconfigCache = make(map[string]*pb.KConfigPolicy)

// kconfigSnapshotStaging accumulates KConfig policies during a SNAPSHOT.
// It is nil when not inside a snapshot sequence.
var kconfigSnapshotStaging map[string]*pb.KConfigPolicy

// kdeNotifier handles desktop notifications and app reconfigure via D-Bus.
var kdeNotifier = notify.New()

// notifyConfig holds the current server-provided notification settings.
// It is refreshed on each stream connect.
var notifyConfig = notify.Config{
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
var firefoxNotifyConfig = notify.Config{
	Enabled:  true,
	Cooldown: 5 * time.Minute,
	Message:  "Firefox policies have been updated. Please restart Firefox for all changes to take effect.",
}

// chromeCache maps policy ID → proto policy for all active Chrome policies.
var chromeCache = make(map[string]*pb.ChromePolicy)

// chromeSnapshotStaging accumulates Chrome proto policies during a SNAPSHOT.
// It is nil when not inside a snapshot sequence.
var chromeSnapshotStaging map[string]*pb.ChromePolicy

// chromeNotifier handles desktop notifications for Chrome policy changes.
var chromeNotifier = notify.New()

// chromeNotifyConfig holds Chrome-specific notification settings.
var chromeNotifyConfig = notify.Config{
	Enabled:  true,
	Cooldown: 5 * time.Minute,
	Message:  "Chrome/Chromium policies have been updated. Please restart your browser for all changes to take effect.",
}

// dconfCache maps policy ID → typed DConf policy for all active Dconf policies.
var dconfCache = make(map[string]*pb.DConfPolicy)

// dconfSnapshotStaging accumulates DConf policies during a SNAPSHOT.
var dconfSnapshotStaging map[string]*pb.DConfPolicy

// dconfSchemaIndex caches the set of schema IDs available on this node.
// Built once at startup by ScanGSettingsSchemas and used for compliance checks.
var dconfSchemaIndex map[string]struct{}

// fileWatcher monitors Bor-managed files and restores them when modified externally.
var fileWatcher *filewatcher.FileWatcher

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

	log.Printf("Server enrollment: %s  policy: %s", cfg.Server.EnrollmentAddr(), cfg.Server.PolicyAddr())
	log.Printf("Client ID: %s", cfg.Agent.ClientID)

	// ─── Enrollment / mTLS bootstrap ──────────────────────────────────
	paths := policyclient.DefaultPaths(cfg.Enrollment.DataDir)

	// If a token is supplied and the agent is already enrolled, remove the
	// existing certificates so that re-enrollment proceeds cleanly. This
	// covers intentional re-enrollment (moving a node to a different group,
	// CA rotation, etc.).
	if *enrollToken != "" && policyclient.IsEnrolled(paths) {
		log.Println("--token provided for an already-enrolled agent – removing old certificates for re-enrollment")
		if removeErr := policyclient.RemoveEnrollmentCerts(paths); removeErr != nil {
			log.Fatalf("Failed to remove old enrollment certificates: %v", removeErr)
		}
	}

	if !policyclient.IsEnrolled(paths) {
		if *enrollToken == "" {
			log.Fatal("Agent is not enrolled and no enrollment token was provided.\n" +
				"Run with: bor-agent --token <TOKEN>\n" +
				"Generate a token from the Node Groups page in the Bor web UI.")
		}
		log.Println("Not yet enrolled – starting enrollment...")
		if enrollErr := policyclient.Enroll(
			cfg.Server.EnrollmentAddr(),
			*enrollToken,
			cfg.Agent.ClientID,
			cfg.Server.InsecureSkipVerify,
			paths,
		); enrollErr != nil {
			log.Fatalf("Enrollment failed: %v", enrollErr)
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
	}
	log.Println("Agent is enrolled – using mTLS credentials")

	agentAddr := cfg.Server.PolicyAddr()

	// ─── Certificate renewal check ────────────────────────────────────
	// Renew the agent certificate if it expires within 30 days.
	const renewThreshold = 30 * 24 * time.Hour
	expiring, expiryErr := policyclient.CertExpiringSoon(paths.CertFile, renewThreshold)
	if expiryErr != nil {
		log.Printf("Warning: could not check cert expiry: %v", expiryErr)
	} else if expiring {
		log.Println("Certificate expires within 30 days, renewing...")
		if renewErr := policyclient.RenewCertificate(agentAddr, paths.CACert, paths.CertFile, paths.KeyFile); renewErr != nil {
			log.Printf("Warning: certificate renewal failed: %v — will retry next cycle", renewErr)
		}
	}

	// ─── Connect with mTLS credentials ────────────────────────────────
	client, err := policyclient.New(
		agentAddr,
		cfg.Agent.ClientID,
		paths.CACert,   // CA cert received during enrollment
		paths.CertFile, // agent client cert signed by CA
		paths.KeyFile,  // agent private key
		false,          // never skip verify after enrollment – we have the CA cert
	)
	if err != nil {
		log.Fatalf("Failed to create policy client: %v", err)
	}
	defer func() { _ = client.Close() }()

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

	// Start the file watcher to restore managed files if tampered externally.
	var watcherErr error
	fileWatcher, watcherErr = filewatcher.New(func(path string) {
		onTamperedFile(ctx, client, cfg, path)
	})
	if watcherErr != nil {
		log.Printf("Warning: failed to create file watcher (tamper protection disabled): %v", watcherErr)
	} else {
		defer func() { _ = fileWatcher.Close() }()
		go fileWatcher.Run(ctx)
		log.Println("File watcher started")
	}

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
			notifyConfig = notify.Config{
				Enabled:  agentCfg.NotifyUsers,
				Cooldown: time.Duration(agentCfg.NotifyCooldown) * time.Second,
				Message:  agentCfg.NotifyMessage,
			}
			log.Printf("Agent notification config: enabled=%v cooldown=%v", notifyConfig.Enabled, notifyConfig.Cooldown)
			firefoxNotifyConfig = notify.Config{
				Enabled:  agentCfg.NotifyUsers,
				Cooldown: time.Duration(agentCfg.NotifyCooldown) * time.Second,
				Message:  agentCfg.NotifyMessageFirefox,
			}
			chromeNotifyConfig = notify.Config{
				Enabled:  agentCfg.NotifyUsers,
				Cooldown: time.Duration(agentCfg.NotifyCooldown) * time.Second,
				Message:  agentCfg.NotifyMessageChrome,
			}
		}

		// Send heartbeat on connect to report current metadata.
		go sendHeartbeat(ctx, client)

		// Scan GSettings schemas and report the catalogue on first connect.
		// This is best-effort; errors are logged but do not block policy streaming.
		if dconfSchemaIndex == nil {
			go func() {
				schemas, err := policy.ScanGSettingsSchemas()
				if err != nil {
					log.Printf("dconf: schema scan failed (non-fatal): %v", err)
					return
				}
				// Build local schema index for compliance checks.
				idx := make(map[string]struct{}, len(schemas))
				for _, s := range schemas {
					idx[s.GetSchemaId()] = struct{}{}
				}
				dconfSchemaIndex = idx

				gnomeVer := detectGNOMEVersion()
				if err := client.ReportSchemaCatalogue(ctx, schemas, gnomeVer); err != nil {
					log.Printf("dconf: ReportSchemaCatalogue failed (non-fatal): %v", err)
				} else {
					log.Printf("dconf: reported %d schemas to server", len(schemas))
				}
			}()
		}

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
		backoff *= 2
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
				kconfigCache = make(map[string]*pb.KConfigPolicy)
				kconfigSnapshotStaging = nil
				firefoxCache = make(map[string]*pb.FirefoxPolicy)
				firefoxSnapshotStaging = nil
				chromeCache = make(map[string]*pb.ChromePolicy)
				chromeSnapshotStaging = nil
				dconfCache = make(map[string]*pb.DConfPolicy)
				dconfSnapshotStaging = nil
				syncAllKConfig(ctx, client, cfg)
				syncAllFirefox(ctx, client, cfg)
				syncAllChrome(ctx, client, cfg)
				syncAllDConf(ctx, client)
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
				chromeSnapshotStaging = make(map[string]*pb.ChromePolicy)
			}
			chromeSnapshotStaging[pi.ID] = pi.ChromePolicy
		case "Kconfig":
			if kconfigSnapshotStaging == nil {
				kconfigSnapshotStaging = make(map[string]*pb.KConfigPolicy)
			}
			kconfigSnapshotStaging[pi.ID] = pi.KConfigPolicy
		case "Dconf":
			if dconfSnapshotStaging == nil {
				dconfSnapshotStaging = make(map[string]*pb.DConfPolicy)
			}
			dconfSnapshotStaging[pi.ID] = pi.DConfPolicy
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
				kconfigCache = make(map[string]*pb.KConfigPolicy)
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
				chromeCache = make(map[string]*pb.ChromePolicy)
			}
			chromeSnapshotStaging = nil

			// Swap DConf staging into cache.
			if dconfSnapshotStaging != nil {
				dconfCache = dconfSnapshotStaging
			} else {
				dconfCache = make(map[string]*pb.DConfPolicy)
			}
			dconfSnapshotStaging = nil

			kconfigChanged := syncAllKConfig(ctx, client, cfg)
			syncAllFirefox(ctx, client, cfg)
			syncAllChrome(ctx, client, cfg)
			syncAllDConf(ctx, client)

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
			chromeCache[pi.ID] = pi.ChromePolicy
			if syncAllChrome(ctx, client, cfg) {
				chromeNotifier.ScheduleNotification(chromeNotifyConfig, map[string]bool{"bor_managed.json": true})
			}
		case "Kconfig":
			kconfigCache[pi.ID] = pi.KConfigPolicy
			if changed := syncAllKConfig(ctx, client, cfg); len(changed) > 0 {
				kdeNotifier.ScheduleNotification(notifyConfig, changed)
			}
		case "Dconf":
			dconfCache[pi.ID] = pi.DConfPolicy
			syncAllDConf(ctx, client)
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
		} else if _, ok := dconfCache[pi.ID]; ok {
			delete(dconfCache, pi.ID)
			syncAllDConf(ctx, client)
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
// identical policy IDs and proto content. Used to detect whether a
// SNAPSHOT resync actually changed the Chrome policy set.
func chromeCachesEqual(a, b map[string]*pb.ChromePolicy) bool {
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

// syncAllKConfig re-merges all cached KConfig policies and syncs the
// resulting files to disk. KCM (Control Module) restriction entries are
// split out and written directly to /etc/kde5rc and /etc/kde6rc; all
// other entries go to the XDG overlay. When the cache is empty,
// SyncKConfigFiles restores all previously managed files from backups.
//
// Returns the set of written file basenames (nil when nothing was
// written). The caller decides whether to schedule a notification.
func syncAllKConfig(ctx context.Context, client *policyclient.Client, cfg *config.Config) map[string]bool {
	var allEntries []*pb.KConfigEntry
	var ids []string
	for id, pol := range kconfigCache {
		allEntries = append(allEntries, policy.KConfigPolicyToEntries(pol)...)
		ids = append(ids, id)
	}

	// Split KCM restriction entries from other KConfig entries.
	// KCM restrictions go to /etc/kde5rc and /etc/kde6rc directly.
	kcmEntries, otherEntries := policy.SplitKCMRestrictions(allEntries)

	files, err := policy.MergeKConfigEntries(otherEntries)
	if err != nil {
		log.Printf("Error merging KConfig policies: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to merge policies: "+err.Error())
		}
		return nil
	}

	if err := policy.EnsureProfileScript(cfg.KConfig.ConfigPath); err != nil {
		log.Printf("Warning: failed to ensure profile.d script: %v", err)
	}

	// Suppress watcher events for all files about to be written (current and new).
	var incomingPaths []string
	for name := range files {
		incomingPaths = append(incomingPaths, filepath.Join(cfg.KConfig.ConfigPath, name))
	}
	incomingPaths = append(incomingPaths, "/etc/kde5rc", "/etc/kde6rc")
	suppressManagedWrites(cfg, incomingPaths...)
	defer updateWatcher(cfg)

	if err := policy.SyncKConfigFiles(cfg.KConfig.ConfigPath, files); err != nil {
		log.Printf("Error syncing KConfig files: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to sync KConfig files: "+err.Error())
		}
		return nil
	}

	// Sync KCM restrictions to /etc/kde5rc and /etc/kde6rc.
	var kcmContent []byte
	if len(kcmEntries) > 0 {
		kcmFiles, err := policy.MergeKConfigEntries(kcmEntries)
		if err != nil {
			log.Printf("Error merging KCM restriction entries: %v", err)
			for _, id := range ids {
				_ = client.ReportCompliance(ctx, id, false, "failed to merge KCM restrictions: "+err.Error())
			}
			return nil
		}
		kcmContent = kcmFiles["kde5rc"]
	}

	if err := policy.SyncKCMRestrictions(kcmContent); err != nil {
		log.Printf("Error syncing KCM restrictions: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to sync KCM restrictions: "+err.Error())
		}
		return nil
	}

	log.Printf("KConfig policies synced to %s (%d policies, %d files)", cfg.KConfig.ConfigPath, len(ids), len(files))
	if len(kcmEntries) > 0 {
		log.Printf("KCM restrictions synced to /etc/kde5rc and /etc/kde6rc")
	}
	for _, id := range ids {
		_ = client.ReportCompliance(ctx, id, true, "Deployed")
	}

	if len(files) == 0 && len(kcmEntries) == 0 {
		return nil
	}
	changedFiles := make(map[string]bool, len(files)+2)
	for name := range files {
		changedFiles[name] = true
	}
	if len(kcmEntries) > 0 {
		changedFiles["kde5rc"] = true
		changedFiles["kde6rc"] = true
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

	suppressManagedWrites(cfg, cfg.Firefox.PoliciesPath, cfg.Firefox.FlatpakPoliciesPath)
	defer updateWatcher(cfg)

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

// syncAllChrome re-merges all cached Chrome proto policies and syncs
// bor_managed.json to each configured Chrome/Chromium policy directory.
// Returns true when the sync succeeded (for notification scheduling).
func syncAllChrome(ctx context.Context, client *policyclient.Client, cfg *config.Config) bool {
	var policies []*pb.ChromePolicy
	var ids []string
	for id, pol := range chromeCache {
		if pol != nil {
			policies = append(policies, pol)
		}
		ids = append(ids, id)
	}

	// Collect active (non-empty) Chrome/Chromium paths.
	chromePaths := []string{
		cfg.Chrome.ChromePoliciesPath,
		cfg.Chrome.ChromiumPoliciesPath,
		cfg.Chrome.ChromiumBrowserPoliciesPath,
	}
	var activePaths []string
	for _, p := range chromePaths {
		if p != "" {
			activePaths = append(activePaths, p)
		}
	}

	// Suppress watcher events for all Chrome managed files about to be written.
	var chromeManagedFiles []string
	for _, dir := range append(activePaths, cfg.Chrome.FlatpakChromiumPoliciesPath) {
		if dir != "" {
			chromeManagedFiles = append(chromeManagedFiles, filepath.Join(dir, policy.ChromeManagedFilename))
		}
	}
	suppressManagedWrites(cfg, chromeManagedFiles...)
	defer updateWatcher(cfg)

	if err := policy.SyncChromeFromProto(policies, activePaths); err != nil {
		log.Printf("Error syncing Chrome policies: %v", err)
		for _, id := range ids {
			_ = client.ReportCompliance(ctx, id, false, "failed to sync Chrome policies: "+err.Error())
		}
		return false
	}

	// Flatpak Chromium is best-effort — log warning but don't fail.
	if cfg.Chrome.FlatpakChromiumPoliciesPath != "" {
		if err := policy.SyncChromeFromProto(policies, []string{cfg.Chrome.FlatpakChromiumPoliciesPath}); err != nil {
			log.Printf("Warning: failed to sync Flatpak Chromium policies: %v", err)
		} else if len(policies) > 0 {
			log.Printf("Flatpak Chromium policies synced to %s", cfg.Chrome.FlatpakChromiumPoliciesPath)
		}
	}

	log.Printf("Chrome policies synced (%d policies)", len(ids))
	for _, id := range ids {
		_ = client.ReportCompliance(ctx, id, true, "Deployed")
	}
	return true
}

// syncAllDConf re-merges all cached DConf policies, writes the keyfile and
// locks file under /etc/dconf/db/local.d/, and runs dconf update.
// Reports compliance back to the server for each affected policy ID.
func syncAllDConf(ctx context.Context, client *policyclient.Client) {
	var policies []*pb.DConfPolicy
	var ids []string
	for id, pol := range dconfCache {
		policies = append(policies, pol)
		ids = append(ids, id)
	}

	merged := policy.MergeDConfPolicies(policies)
	keyfile, locksfile := policy.DConfPolicyToFiles(merged)

	dbName := merged.GetDbName()
	if dbName == "" {
		dbName = "local"
	}

	if err := policy.SyncDConfFiles(dbName, keyfile, locksfile); err != nil {
		log.Printf("Error syncing dconf files: %v", err)
		for _, id := range ids {
			_ = client.ReportComplianceWithStatus(ctx, id,
				pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				"failed to sync dconf files: "+err.Error(), nil)
		}
		return
	}

	log.Printf("dconf policies synced (db=%s, %d policies)", dbName, len(ids))

	// Compliance check uses the locally cached schema index.
	idx := dconfSchemaIndex
	if idx == nil {
		idx = make(map[string]struct{})
	}
	results := policy.CheckDConfCompliance(merged, idx)
	overallStatus, msg := policy.RollupDConfCompliance(results)

	// Convert per-item results to proto for structured reporting.
	protoItems := make([]*pb.ComplianceItemResult, 0, len(results))
	for _, r := range results {
		protoItems = append(protoItems, &pb.ComplianceItemResult{
			SchemaId: r.SchemaID,
			Key:      r.Key,
			Status:   r.Status,
			Message:  r.Message,
		})
	}

	for _, id := range ids {
		_ = client.ReportComplianceWithStatus(ctx, id, overallStatus, msg, protoItems)
	}
}

// detectGNOMEVersion tries to determine the installed GNOME version.
// Returns an empty string if not detectable.
func detectGNOMEVersion() string {
	data, err := os.ReadFile("/usr/share/gnome/gnome-version.xml")
	if err == nil {
		// Very rough extraction: look for the platform/minor/micro tags.
		// Good enough for informational reporting.
		content := string(data)
		start := len("<platform>")
		si := len(content)
		if idx := lastIndex(content, "<platform>"); idx >= 0 {
			si = idx + start
			ei := si
			for ei < len(content) && content[ei] != '<' {
				ei++
			}
			return content[si:ei]
		}
	}
	// Fall back to gnome-shell --version
	if out, err := exec.Command("gnome-shell", "--version").Output(); err == nil {
		ver := strings.TrimPrefix(strings.TrimSpace(string(out)), "GNOME Shell ")
		if ver != "" {
			return ver
		}
	}
	return ""
}

func lastIndex(s, substr string) int {
	idx := -1
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			idx = i
		}
	}
	return idx
}

// getManagedPaths returns the absolute paths of all files currently managed
// by Bor, based on the active policy caches and on-disk backup sentinels.
// This is used to keep the file watcher's watch list up to date.
func getManagedPaths(cfg *config.Config) []string {
	var paths []string

	// Firefox.
	if len(firefoxCache) > 0 {
		if cfg.Firefox.PoliciesPath != "" {
			paths = append(paths, cfg.Firefox.PoliciesPath)
		}
		if cfg.Firefox.FlatpakPoliciesPath != "" {
			paths = append(paths, cfg.Firefox.FlatpakPoliciesPath)
		}
	}

	// Chrome.
	if len(chromeCache) > 0 {
		for _, dir := range []string{
			cfg.Chrome.ChromePoliciesPath,
			cfg.Chrome.ChromiumPoliciesPath,
			cfg.Chrome.ChromiumBrowserPoliciesPath,
			cfg.Chrome.FlatpakChromiumPoliciesPath,
		} {
			if dir != "" {
				paths = append(paths, filepath.Join(dir, policy.ChromeManagedFilename))
			}
		}
	}

	// KConfig: discover currently managed files from .bor-backup sentinels.
	if managed, err := policy.ManagedFiles(cfg.KConfig.ConfigPath); err == nil {
		for _, name := range managed {
			paths = append(paths, filepath.Join(cfg.KConfig.ConfigPath, name))
		}
	}
	// KCM restriction files in /etc.
	for _, pol := range kconfigCache {
		if len(pol.GetKcmRestrictions()) > 0 {
			paths = append(paths, "/etc/kde5rc", "/etc/kde6rc")
			break
		}
	}

	return paths
}

// updateWatcher synchronises the file watcher's managed-file set with the
// current policy state. Call after every sync operation.
func updateWatcher(cfg *config.Config) {
	if fileWatcher == nil {
		return
	}
	fileWatcher.SetManaged(getManagedPaths(cfg))
}

// suppressManagedWrites suppresses file watcher events for all currently
// managed paths plus any additional paths about to be written. Call before
// any Bor-initiated file write to avoid self-triggering restores.
func suppressManagedWrites(cfg *config.Config, extra ...string) {
	if fileWatcher == nil {
		return
	}
	paths := append(getManagedPaths(cfg), extra...)
	fileWatcher.Suppress(paths, 2*time.Second)
}

// onTamperedFile is called by the file watcher when a managed file is modified
// or removed externally. It re-applies the appropriate policy to restore the
// file to the Bor-managed state and reports the event to the server.
func onTamperedFile(ctx context.Context, client *policyclient.Client, cfg *config.Config, path string) {
	log.Printf("Tamper protection: restoring %s", path)

	// Collect process info before restoring — the modifying process may still
	// hold the file open (e.g. an editor), giving us user/comm attribution.
	holders := procinfo.FindFileHolders(path)
	procs := make([]policyclient.TamperProcess, len(holders))
	for i, h := range holders {
		procs[i] = policyclient.TamperProcess{PID: h.PID, Comm: h.Comm, User: h.User}
		log.Printf("Tamper protection: file held by pid=%d comm=%s user=%s", h.PID, h.Comm, h.User)
	}

	switch {
	case strings.HasPrefix(path, cfg.KConfig.ConfigPath+string(filepath.Separator)) ||
		path == "/etc/kde5rc" || path == "/etc/kde6rc":
		syncAllKConfig(ctx, client, cfg)
	case path == cfg.Firefox.PoliciesPath || path == cfg.Firefox.FlatpakPoliciesPath:
		syncAllFirefox(ctx, client, cfg)
	default:
		syncAllChrome(ctx, client, cfg)
	}

	if err := client.ReportTamperEvent(ctx, path, procs); err != nil {
		log.Printf("Failed to report tamper event to server: %v", err)
	}
}
