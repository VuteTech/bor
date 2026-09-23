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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/VuteTech/Bor/agent/internal/config"
	"github.com/VuteTech/Bor/agent/internal/luks"
	"github.com/VuteTech/Bor/agent/internal/policyclient"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// runLuksCommand implements `bor-agent luks status|adopt`. It runs
// before the daemon start-up path and exits the process.
func runLuksCommand(args []string, configPath string) {
	fs := flag.NewFlagSet("luks", flag.ExitOnError)
	device := fs.String("device", "", "adopt only the volume on this device or with this LUKS UUID")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: bor-agent luks <command> [options]

Commands:
  status   Show the LUKS volumes, keyslots and platform facts of this node.
  adopt    Type an existing passphrase once so Bor can escrow a recovery key
           for volumes it has no other credential for.

Options:
`)
		fs.PrintDefaults()
	}
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	command := args[0]
	_ = fs.Parse(args[1:])

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	lcfg := luksConfig(cfg)
	ctx := context.Background()

	switch command {
	case "status":
		luksStatus(ctx, lcfg)
	case "adopt":
		if err := luksAdopt(ctx, cfg, lcfg, *device); err != nil {
			log.Fatalf("Adoption failed: %v", err)
		}
	default:
		fs.Usage()
		os.Exit(2)
	}
}

// luksStatus prints the node's disk-encryption inventory.
func luksStatus(ctx context.Context, lcfg *luks.Config) {
	inv, err := luks.BuildInventory(ctx, lcfg)
	if err != nil {
		log.Fatalf("Inventory failed: %v", err)
	}
	p := inv.Platform
	tpm := "absent"
	if p.TPM2Present {
		tpm = "present"
	}
	sb := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(p.SecureBoot.String(), "SECURE_BOOT_STATE_"), "_", " "))
	fmt.Printf("Platform: TPM2 %s · Secure Boot %s · initramfs %s · cryptsetup %s · clevis %s\n",
		tpm, sb, p.InitramfsGenerator, orUnknown(p.CryptsetupVersion), orUnknown(p.ClevisVersion))
	if inv.ExternallyManaged != "" {
		fmt.Printf("TPM enrollment externally managed by: %s\n", inv.ExternallyManaged)
	}
	if len(inv.Volumes) == 0 {
		fmt.Println("No dm-crypt volumes found.")
		return
	}
	for _, vi := range inv.Volumes {
		fmt.Printf("\n%s  %s\n", vi.MappingName, vi.LuksUUID)
		fmt.Printf("  device %s · mounts %s · system=%v\n", vi.DevicePath, strings.Join(vi.Mountpoints, " "), vi.IsSystem)
		if vi.Header == nil {
			if vi.IsPlain {
				fmt.Println("  plain dm-crypt (ephemeral key)")
			} else {
				fmt.Printf("  header unreadable: %v\n", vi.HeaderErr)
			}
			continue
		}
		fmt.Printf("  LUKS2 · %s · %d-bit volume key · %d bytes JSON area free\n",
			vi.Header.Cipher, vi.Header.VolumeKeyBits, vi.Header.JSONAreaFree)
		for _, ks := range vi.Header.Keyslots {
			extra := ""
			switch ks.Kind {
			case pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_TPM2:
				extra = fmt.Sprintf(" (PCRs %v)", ks.TPM2PCRs)
			case pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS:
				extra = fmt.Sprintf(" (%s %v)", ks.ClevisPin, ks.TangURLs)
			case pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY:
				extra = fmt.Sprintf(" (escrow %s)", ks.BorEscrowID)
			}
			fmt.Printf("  slot %d  %-14s %s%s\n", ks.Index, keyslotKindLabel(ks.Kind), ks.KDF, extra)
		}
	}
}

func keyslotKindLabel(k pb.LuksKeyslotKind) string {
	return strings.ToLower(strings.TrimPrefix(k.String(), "LUKS_KEYSLOT_KIND_"))
}

// luksAdopt reads a passphrase once (systemd-ask-password) and runs the
// escrow-first rotation for every volume Bor cannot unlock otherwise
// . It shares the flock with any concurrently running daemon.
func luksAdopt(ctx context.Context, cfg *config.Config, lcfg *luks.Config, device string) error {
	paths := policyclient.DefaultPaths(cfg.Enrollment.DataDir)
	if !policyclient.IsEnrolled(paths) {
		return fmt.Errorf("the agent is not enrolled; enroll it first")
	}
	client, err := policyclient.New(cfg.Server.PolicyAddr(), cfg.Agent.ClientID,
		paths.CACert, paths.CertFile, paths.KeyFile, false)
	if err != nil {
		return fmt.Errorf("create policy client: %w", err)
	}
	defer func() { _ = client.Close() }()

	unlock, err := lockLuksRun(lcfg)
	if err != nil {
		return fmt.Errorf("another Bor LUKS operation is running: %w", err)
	}
	defer unlock()

	inv, err := luks.BuildInventory(ctx, lcfg)
	if err != nil {
		return fmt.Errorf("inventory failed: %w", err)
	}

	var targets []*luks.VolumeInfo
	for _, vi := range inv.Volumes {
		if vi.LuksUUID == "" || vi.Header == nil {
			continue
		}
		if device != "" && vi.DevicePath != device &&
			!strings.EqualFold(vi.LuksUUID, device) && vi.MappingName != device {
			continue
		}
		targets = append(targets, vi)
	}
	if len(targets) == 0 {
		return fmt.Errorf("no matching LUKS2 volume found")
	}

	passphrase, err := askPassword(ctx, "Enter the current LUKS passphrase for adoption:")
	if err != nil {
		return fmt.Errorf("read the passphrase: %w", err)
	}
	defer luks.Zero(passphrase)

	state, err := luks.LoadState(lcfg)
	if err != nil {
		return fmt.Errorf("load the agent state: %w", err)
	}

	for _, vi := range targets {
		keyCopy := append([]byte(nil), passphrase...)
		if _, err := luks.TestKeyAnySlot(ctx, lcfg, vi.DevicePath, vi.Header, keyCopy); err != nil {
			fmt.Printf("%s: the passphrase does not open this volume - skipped\n", vi.MappingName)
			continue
		}
		cred := &luks.Credential{Key: append([]byte(nil), passphrase...), Source: "interactive adoption"}
		err := luks.RotateRecoveryKey(ctx, lcfg, client, vi.Volume, vi.Header, cred,
			pb.RecoveryKeyReason_RECOVERY_KEY_REASON_INITIAL, "", cfg.Server.Address, state)
		if err != nil {
			fmt.Printf("%s: adoption failed: %v\n", vi.MappingName, err)
			continue
		}
		fmt.Printf("%s: recovery key escrowed on the Bor server\n", vi.MappingName)
	}
	fmt.Println("Done. The user passphrase stays in place; the daemon reports compliance on its next sync.")
	return nil
}

// askPassword reads a secret via systemd-ask-password (no TTY handling in
// the agent, no secret on any command line but the prompt text).
func askPassword(ctx context.Context, prompt string) ([]byte, error) {
	res, err := luks.RunForOutput(ctx, "systemd-ask-password", []string{"--timeout=120", prompt}, 3*time.Minute)
	if err != nil {
		return nil, err
	}
	pass := strings.TrimSuffix(res, "\n")
	if pass == "" {
		return nil, fmt.Errorf("empty passphrase")
	}
	return []byte(pass), nil
}

// lockLuksRun takes an exclusive flock on <run_dir>/luks.lock, shared with
// the daemon's own operations.
func lockLuksRun(lcfg *luks.Config) (func(), error) {
	if err := os.MkdirAll(lcfg.RunDirPath(), 0o700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(lcfg.RunDirPath(), "luks.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // fixed run-dir path
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
