// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
)

// modhexAlphabet is the systemd-cryptenroll recovery-key alphabet,
// chosen to be typeable on most keyboard layouts (the boot prompt usually
// runs with a US keymap).
const modhexAlphabet = "cbdefghijklnrtuv"

// GenerateRecoveryKey returns a 256-bit recovery key in the exact format
// `systemd-cryptenroll --recovery-key` produces: 8 dash-separated groups of
// 8 modhex characters. The caller must Zero the slice after use. Bor
// generates the key itself so it can be escrowed BEFORE the keyslot exists
// (escrow-first two-phase commit); the format is identical, so boot
// prompts and help-desk procedures do not change.
func GenerateRecoveryKey() ([]byte, error) {
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return nil, fmt.Errorf("luks: generate recovery key: %w", err)
	}
	defer Zero(entropy)

	// 32 bytes -> 64 modhex chars -> 8 groups of 8 + 7 dashes = 71 bytes.
	key := make([]byte, 0, 71)
	for i, b := range entropy {
		if i > 0 && i%4 == 0 {
			key = append(key, '-')
		}
		key = append(key, modhexAlphabet[b>>4], modhexAlphabet[b&0x0f])
	}
	return key, nil
}

// AddKeySlot enrolls a new key on the device: the existing credential goes
// in on stdin, the new key on an inherited pipe (fd 3) - no secret ever
// touches the filesystem or argv. Bor recovery slots use
// PBKDF2-HMAC-SHA512 with 1000 iterations, exactly like systemd-cryptenroll
// recovery keys (a 256-bit random secret needs no KDF stretching; FIPS
// approves PBKDF2, SP 800-132). Returns the new slot index.
func AddKeySlot(ctx context.Context, cfg *Config, device string, credential, newKey []byte) (int, error) {
	before, err := DumpHeader(ctx, cfg.CryptsetupBinary(), device, "")
	if err != nil {
		return -1, err
	}
	occupied := map[int]bool{}
	for _, ks := range before.Keyslots {
		occupied[ks.Index] = true
	}

	_, err = runCommand(ctx, &runRequest{
		Name: cfg.CryptsetupBinary(),
		Args: []string{
			"luksAddKey",
			"--key-file=-",
			"--new-keyfile=/proc/self/fd/3",
			"--pbkdf", "pbkdf2", "--hash", "sha512", "--pbkdf-force-iterations", "1000",
			device,
		},
		Stdin: credential,
		FD3:   newKey,
	})
	if err != nil {
		return -1, fmt.Errorf("luks: add key slot on %s: %w", device, err)
	}

	after, err := DumpHeader(ctx, cfg.CryptsetupBinary(), device, "")
	if err != nil {
		return -1, err
	}
	for _, ks := range after.Keyslots {
		if !occupied[ks.Index] {
			return ks.Index, nil
		}
	}
	return -1, fmt.Errorf("luks: added a key on %s but found no new slot in the header", device)
}

// ImportRecoveryToken marks a slot as a recovery key by importing a
// systemd-recovery token, so boot prompts and `systemd-cryptenroll` list it
// exactly like a native enrollment (verified: the slot is then listed as
// "recovery").
func ImportRecoveryToken(ctx context.Context, cfg *Config, device string, slot int) error {
	token, err := json.Marshal(map[string]interface{}{
		"type":     "systemd-recovery",
		"keyslots": []string{strconv.Itoa(slot)},
	})
	if err != nil {
		return err
	}
	_, err = runCommand(ctx, &runRequest{
		Name:  cfg.CryptsetupBinary(),
		Args:  []string{"token", "import", device},
		Stdin: token,
	})
	if err != nil {
		return fmt.Errorf("luks: import systemd-recovery token on %s: %w", device, err)
	}
	return nil
}

// UpsertBorEscrowToken writes Bor's UNASSIGNED metadata token: escrow id,
// keyslot and server, with an empty keyslots list. Assigning a foreign token
// to a slot that already carries a systemd token makes systemd-cryptenroll
// report "conflict", so the marker stays unassigned - boot tooling
// ignores it, a reinstalled agent recognises its slot, and a human reading
// luksDump sees which slot Bor manages.
func UpsertBorEscrowToken(ctx context.Context, cfg *Config, device, oldTokenID, escrowID string, slot int, server string) error {
	if oldTokenID != "" {
		if _, err := runCommand(ctx, &runRequest{
			Name: cfg.CryptsetupBinary(),
			Args: []string{"token", "remove", "--token-id", oldTokenID, device},
		}); err != nil {
			log.Printf("luks: remove stale bor-escrow token %s on %s: %v", oldTokenID, device, err)
		}
	}
	token, err := json.Marshal(map[string]interface{}{
		"type":     BorEscrowTokenType,
		"keyslots": []string{},
		"bor": map[string]interface{}{
			"v":         1,
			"escrow_id": escrowID,
			"keyslot":   strconv.Itoa(slot),
			"server":    server,
		},
	})
	if err != nil {
		return err
	}
	_, err = runCommand(ctx, &runRequest{
		Name:  cfg.CryptsetupBinary(),
		Args:  []string{"token", "import", device},
		Stdin: token,
	})
	if err != nil {
		return fmt.Errorf("luks: import bor-escrow token on %s: %w", device, err)
	}
	return nil
}

// TestKeySlot verifies a key against exactly one slot without creating a dm
// device (`--test-passphrase --key-slot N`).
func TestKeySlot(ctx context.Context, cfg *Config, device string, slot int, key []byte) error {
	_, err := runCommand(ctx, &runRequest{
		Name: cfg.CryptsetupBinary(),
		Args: []string{
			"open", "--test-passphrase",
			"--key-slot", strconv.Itoa(slot),
			"--key-file=-",
			device,
		},
		Stdin: key,
	})
	if err != nil {
		return fmt.Errorf("luks: verify key against slot %d on %s: %w", slot, device, err)
	}
	return nil
}

// TestKeyAnySlot verifies a key against any slot and returns the slot index
// that accepted it (used to locate the bootstrap slot).
func TestKeyAnySlot(ctx context.Context, cfg *Config, device string, header *Header, key []byte) (int, error) {
	for _, ks := range header.Keyslots {
		keyCopy := append([]byte(nil), key...)
		err := TestKeySlot(ctx, cfg, device, ks.Index, keyCopy)
		if err == nil {
			return ks.Index, nil
		}
	}
	return -1, fmt.Errorf("luks: no keyslot on %s accepts this key", device)
}

// KillSlot wipes a keyslot without a credential (batch mode skips the
// passphrase check) and removes any tokens left orphaned on it - plain
// luksKillSlot leaves them behind. Guard rails are the caller's
// responsibility (see CanWipeSlot).
func KillSlot(ctx context.Context, cfg *Config, device string, slot int) error {
	if _, err := runCommand(ctx, &runRequest{
		Name: cfg.CryptsetupBinary(),
		Args: []string{"-q", "luksKillSlot", device, strconv.Itoa(slot)},
	}); err != nil {
		return fmt.Errorf("luks: kill slot %d on %s: %w", slot, device, err)
	}
	// Remove tokens that were bound to the wiped slot.
	header, err := DumpHeader(ctx, cfg.CryptsetupBinary(), device, "")
	if err != nil {
		return err
	}
	for _, ks := range header.Keyslots {
		_ = ks // tokens of live slots stay
	}
	// Re-read raw tokens: orphaned tokens reference the killed slot only.
	res, err := runCommand(ctx, &runRequest{
		Name: cfg.CryptsetupBinary(),
		Args: []string{"luksDump", "--dump-json-metadata", device},
	})
	if err != nil {
		return err
	}
	var meta struct {
		Tokens map[string]struct {
			Keyslots []string `json:"keyslots"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &meta); err != nil {
		return nil //nolint:nilerr // best-effort cleanup; the slot itself is gone
	}
	slotStr := strconv.Itoa(slot)
	for tokenID, t := range meta.Tokens {
		if len(t.Keyslots) == 1 && t.Keyslots[0] == slotStr {
			if _, err := runCommand(ctx, &runRequest{
				Name: cfg.CryptsetupBinary(),
				Args: []string{"token", "remove", "--token-id", tokenID, device},
			}); err != nil {
				log.Printf("luks: remove orphaned token %s on %s: %v", tokenID, device, err)
			}
		}
	}
	return nil
}

// CanWipeSlot enforces the wipe invariants before any destructive step:
// the volume must keep a server-confirmed recovery slot (confirmedSlot) and
// at least one other protector, and the wiped slot is never the last one.
func CanWipeSlot(header *Header, wipeSlot, confirmedRecoverySlot int) error {
	if wipeSlot == confirmedRecoverySlot {
		return fmt.Errorf("luks: refusing to wipe the confirmed recovery slot %d", wipeSlot)
	}
	if header.FindSlot(confirmedRecoverySlot) == nil {
		return fmt.Errorf("luks: refusing to wipe slot %d: no confirmed recovery slot %d on the volume", wipeSlot, confirmedRecoverySlot)
	}
	others := 0
	for _, ks := range header.Keyslots {
		if ks.Index != wipeSlot && ks.Index != confirmedRecoverySlot {
			others++
		}
	}
	if others == 0 {
		return fmt.Errorf("luks: refusing to wipe slot %d: the recovery slot would be the only remaining protector", wipeSlot)
	}
	return nil
}
