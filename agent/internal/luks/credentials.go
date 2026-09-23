// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// EscrowClient is the subset of the policy client the luks package needs
// for the escrow flows. Implemented by policyclient.Client.
type EscrowClient interface {
	EscrowRecoveryKey(ctx context.Context, luksUUID, escrowID string, recoveryKey []byte, reason pb.RecoveryKeyReason, rotationID string) error
	ConfirmRecoveryKey(ctx context.Context, luksUUID, escrowID string, keyslot int, fingerprint string) error
	BeginRecoveryKeyRotation(ctx context.Context, luksUUID string) (rotationID string, recoveryKey []byte, err error)
}

// ResolveCredential finds an unlock credential for a volume, preferring
// local sources so a key leaves the server only when nothing else works
// . serverAssisted enables source #5 (the effective policy's
// server_assisted_rotation switch); it also requires a pending rotation
// task server-side. Returns nil when no credential is available - the
// caller reports luks:adoption NON_COMPLIANT.
func ResolveCredential(ctx context.Context, cfg *Config, client EscrowClient, vol *Volume, header *Header, serverAssisted bool) (cred *Credential, rotationID string) {
	// #2 - the TPM2 slot via the libcryptsetup plugin, when it unseals in
	// the current PCR state. (#1, a key created earlier in the same run, is
	// handled by the rotation flow itself.)
	if len(header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_TPM2)) > 0 &&
		TPM2Healthy(ctx, cfg, vol.DevicePath) {
		return &Credential{TokenType: "systemd-tpm2", Source: "tpm2"}, "" //nolint:gosec // G101: a LUKS2 token type name, not a credential
	}

	// #3 - an existing Clevis binding (Tang reachable). This is what makes
	// existing NBDE shops adopt without user interaction.
	if ClevisAvailable(cfg) {
		for _, slot := range header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS) {
			pass, err := ClevisPass(ctx, cfg, vol.DevicePath, slot.Index)
			if err == nil {
				return &Credential{Key: pass, Source: fmt.Sprintf("clevis slot %d", slot.Index)}, ""
			}
			log.Printf("luks: clevis credential from slot %d on %s unavailable: %v", slot.Index, vol.DevicePath, err)
		}
	}

	// #4 - a one-time bootstrap key file placed by provisioning.
	if key, err := readBootstrapKey(cfg, vol.LuksUUID); err != nil {
		log.Printf("luks: bootstrap key for %s: %v", vol.LuksUUID, err)
	} else if key != nil {
		return &Credential{Key: key, Source: "bootstrap"}, ""
	}

	// #5 - server-assisted rotation: the ACTIVE key is released only while
	// a rotation task exists, and every release IS a rotation.
	if serverAssisted && client != nil && hasBorRecoverySlot(header) {
		rotationID, key, err := client.BeginRecoveryKeyRotation(ctx, vol.LuksUUID)
		if err == nil {
			return &Credential{Key: key, Source: "server release"}, rotationID
		}
		log.Printf("luks: server-assisted credential for %s unavailable: %v", vol.LuksUUID, err)
	}

	// #6 - interactive adoption is the `bor-agent luks adopt` CLI.
	return nil, ""
}

// hasBorRecoverySlot reports whether the volume carries a Bor-managed
// recovery slot.
func hasBorRecoverySlot(header *Header) bool {
	return len(header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY)) > 0
}

// readBootstrapKey loads BootstrapDir/<uuid>.key, enforcing root-only mode.
// Returns (nil, nil) when no bootstrap file exists.
func readBootstrapKey(cfg *Config, luksUUID string) ([]byte, error) {
	path := BootstrapKeyPath(cfg, luksUUID)
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("bootstrap key %s is group- or world-readable (mode %04o); refusing to use it", path, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path) //nolint:gosec // fixed directory + validated uuid
	if err != nil {
		return nil, err
	}
	// The provisioning file may end with a newline the passphrase lacks.
	key := []byte(strings.TrimRight(string(data), "\n"))
	if len(key) == 0 {
		return nil, fmt.Errorf("bootstrap key %s is empty", path)
	}
	return key, nil
}

// BootstrapKeyPath returns the provisioning key path for a volume.
func BootstrapKeyPath(cfg *Config, luksUUID string) string {
	return filepath.Join(cfg.BootstrapDirPath(), luksUUID+".key")
}

// ConsumeBootstrapKey deletes a used bootstrap file. The slot it unlocked
// is wiped separately, which makes any leftover bytes on a CoW/SSD
// filesystem worthless.
func ConsumeBootstrapKey(cfg *Config, luksUUID string) {
	path := BootstrapKeyPath(cfg, luksUUID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("luks: remove bootstrap key %s: %v", path, err)
	}
}
