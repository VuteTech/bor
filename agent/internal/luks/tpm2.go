// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Credential is an unlock credential for keyslot additions. Exactly
// one of Key or TokenType is set: a raw secret delivered via stdin, or a
// LUKS2 token type that the libcryptsetup plugins can satisfy without one
// (e.g. "systemd-tpm2" when the TPM unseals in the current PCR state).
type Credential struct {
	// Key is the secret (passphrase, recovery or bootstrap key). Zeroed by
	// the operation that consumes it.
	Key []byte
	// TokenType selects `--token-only --token-type <T>` unlocking.
	TokenType string
	// Source describes where the credential came from (for logging only).
	Source string
}

// PCRList resolves a policy's PCR profile to the explicit list Bor always
// passes: an implicit empty `--tpm2-pcrs` would bind to NO PCRs at all
// , so the list is never empty.
func PCRList(t *pb.Tpm2Protector) []uint32 {
	switch t.GetPcrProfile() {
	case pb.TpmPcrProfile_TPM_PCR_PROFILE_SECURE_BOOT_SHIM:
		return []uint32{7, 14}
	case pb.TpmPcrProfile_TPM_PCR_PROFILE_CUSTOM:
		return t.GetPcrs()
	default: // UNSPECIFIED, SECURE_BOOT
		return []uint32{7}
	}
}

// pcrArg renders a PCR list for --tpm2-pcrs ("7" or "7+14").
func pcrArg(pcrs []uint32) string {
	parts := make([]string, 0, len(pcrs))
	for _, p := range pcrs {
		parts = append(parts, strconv.FormatUint(uint64(p), 10))
	}
	return strings.Join(parts, "+")
}

// EnrollTPM2 enrolls a TPM2 keyslot with systemd-cryptenroll, optionally
// wiping stale slots in the same call - the wipe runs only after the
// enrollment succeeded and never touches the new slot. Bor always
// passes an explicit PCR list and an empty --tpm2-pcrlock= so a stray
// pcrlock.json is never picked up implicitly.
func EnrollTPM2(ctx context.Context, cfg *Config, device string, cred *Credential, pcrs []uint32, wipeSlots []int) error {
	if len(pcrs) == 0 {
		return fmt.Errorf("luks: refusing a TPM2 enrollment with an empty PCR list")
	}
	args := []string{}
	switch {
	case cred.TokenType == "systemd-tpm2":
		// systemd ≥ 256: unlock with the existing TPM2 enrollment.
		args = append(args, "--unlock-tpm2-device=auto")
	case len(cred.Key) > 0:
		args = append(args, "--unlock-key-file=/proc/self/fd/3")
	default:
		return fmt.Errorf("luks: TPM2 enrollment needs an unlock credential")
	}
	args = append(args,
		"--tpm2-device=auto",
		"--tpm2-pcrs="+pcrArg(pcrs),
		"--tpm2-pcrlock=",
	)
	if len(wipeSlots) > 0 {
		args = append(args, "--wipe-slot="+joinSlots(wipeSlots))
	}
	args = append(args, device)

	_, err := runCommand(ctx, &runRequest{
		Name: cfg.CryptenrollBinary(),
		Args: args,
		FD3:  cred.Key,
	})
	if err != nil {
		return fmt.Errorf("luks: TPM2 enrollment on %s: %w", device, err)
	}
	return nil
}

// TPM2Healthy reports whether the TPM2 enrollment would unseal in the
// CURRENT PCR state: after a firmware or dbx update this fails and
// auto-reseal re-enrolls once the system is up.
func TPM2Healthy(ctx context.Context, cfg *Config, device string) bool {
	_, err := runCommand(ctx, &runRequest{
		Name: cfg.CryptsetupBinary(),
		Args: []string{
			"open", "--test-passphrase",
			"--token-only", "--token-type", "systemd-tpm2",
			device,
		},
	})
	return err == nil
}

// WipeSlotsWithKey wipes keyslots via systemd-cryptenroll, authenticating
// with a key on fd 3; bound tokens are removed with the slots.
func WipeSlotsWithKey(ctx context.Context, cfg *Config, device string, key []byte, slots []int) error {
	if len(slots) == 0 {
		Zero(key)
		return nil
	}
	_, err := runCommand(ctx, &runRequest{
		Name: cfg.CryptenrollBinary(),
		Args: []string{
			"--unlock-key-file=/proc/self/fd/3",
			"--wipe-slot=" + joinSlots(slots),
			device,
		},
		FD3: key,
	})
	if err != nil {
		return fmt.Errorf("luks: wipe slots %v on %s: %w", slots, device, err)
	}
	return nil
}

func joinSlots(slots []int) string {
	parts := make([]string, 0, len(slots))
	for _, s := range slots {
		parts = append(parts, strconv.Itoa(s))
	}
	return strings.Join(parts, ",")
}

// PCRsMatch reports whether an enrolled slot is bound to the desired PCRs.
func PCRsMatch(slot *Keyslot, want []uint32) bool {
	if len(slot.TPM2PCRs) != len(want) {
		return false
	}
	have := map[uint32]bool{}
	for _, p := range slot.TPM2PCRs {
		have[p] = true
	}
	for _, p := range want {
		if !have[p] {
			return false
		}
	}
	return true
}
