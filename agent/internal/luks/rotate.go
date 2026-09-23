// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// mustRandRead fills b from crypto/rand; the CSPRNG never fails in practice.
func mustRandRead(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("luks: crypto/rand: " + err.Error())
	}
}

// RotateRecoveryKey runs the escrow-first two-phase rotation:
//
//	generate K -> escrow (server stores PENDING) -> luksAddKey -> import
//	systemd-recovery token -> verify slot -> confirm (PENDING->ACTIVE) ->
//	wipe old Bor recovery slots -> update the bor-escrow marker.
//
// At no time does a live recovery slot exist whose secret the server does
// not hold, and the old slot is wiped only after the server confirmed the
// new one. Progress (never keys) is persisted in the state file so a crash
// resumes instead of leaking slots. rotationID links a server-assisted
// release to the rotation that consumes it.
func RotateRecoveryKey(ctx context.Context, cfg *Config, client EscrowClient, vol *Volume, header *Header,
	cred *Credential, reason pb.RecoveryKeyReason, rotationID, serverName string, state *State) error {
	vs := state.Volume(vol.LuksUUID)

	// Resume: a slot from a crashed run only needs confirm + cleanup.
	if vs.Step == RotationStepSlotAdded && vs.EscrowID != "" {
		if resumeErr := resumeRotation(ctx, cfg, client, vol, header, vs, state); resumeErr == nil {
			return nil
		}
		// The pending escrow expired (>24 h): the un-escrowed slot is an
		// orphan. Wipe it (credential-free) when the invariants allow, then
		// start over.
		cleanupOrphanSlot(ctx, cfg, vol, header, vs)
		vs.ClearRotation()
		_ = state.Save(cfg)
		var reloadErr error
		header, reloadErr = DumpHeader(ctx, cfg.CryptsetupBinary(), vol.DevicePath, vol.LuksUUID)
		if reloadErr != nil {
			return reloadErr
		}
	}

	// 1. Generate the key in memory only.
	key, err := GenerateRecoveryKey()
	if err != nil {
		return err
	}
	defer Zero(key)

	// 2. Escrow BEFORE any slot exists.
	escrowID := newEscrowID()
	if escrowErr := client.EscrowRecoveryKey(ctx, vol.LuksUUID, escrowID, key, reason, rotationID); escrowErr != nil {
		return fmt.Errorf("escrow recovery key: %w", escrowErr)
	}

	// Old Bor recovery slots to retire once the new one is ACTIVE.
	var oldSlots []int
	for _, ks := range header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY) {
		oldSlots = append(oldSlots, ks.Index)
	}

	// 3. Enroll the slot: credential in, key on fd 3.
	keyCopy := append([]byte(nil), key...)
	slot, err := addSlotWithCredential(ctx, cfg, vol.DevicePath, cred, keyCopy)
	if err != nil {
		return err
	}
	if tokenErr := ImportRecoveryToken(ctx, cfg, vol.DevicePath, slot); tokenErr != nil {
		return tokenErr
	}

	// 4. Verify the slot really opens with the escrowed key.
	verifyCopy := append([]byte(nil), key...)
	if verifyErr := TestKeySlot(ctx, cfg, vol.DevicePath, slot, verifyCopy); verifyErr != nil {
		return fmt.Errorf("verify new recovery slot: %w", verifyErr)
	}

	// Recompute the fingerprint from the fresh header.
	newHeader, err := DumpHeader(ctx, cfg.CryptsetupBinary(), vol.DevicePath, vol.LuksUUID)
	if err != nil {
		return err
	}
	newSlot := newHeader.FindSlot(slot)
	if newSlot == nil {
		return fmt.Errorf("luks: new recovery slot %d vanished from the header", slot)
	}

	vs.Step = RotationStepSlotAdded
	vs.EscrowID = escrowID
	vs.NewSlot = slot
	vs.Fingerprint = newSlot.Fingerprint
	vs.RotationID = rotationID
	vs.OldSlots = oldSlots
	vs.StartedAt = time.Now()
	if err := state.Save(cfg); err != nil {
		log.Printf("luks: persist rotation state: %v", err)
	}

	// 5. Confirm: the server promotes PENDING -> ACTIVE.
	if err := client.ConfirmRecoveryKey(ctx, vol.LuksUUID, escrowID, slot, newSlot.Fingerprint); err != nil {
		return fmt.Errorf("confirm recovery key: %w", err)
	}
	vs.Step = RotationStepConfirmed
	_ = state.Save(cfg)

	// 6. Only now wipe the old Bor recovery slots, unlocking with the new
	// key. Invariants: the confirmed slot stays, and it is never the last
	// protector.
	finishRotation(ctx, cfg, vol, newHeader, vs, key, serverName)
	_ = state.Save(cfg)
	return nil
}

// addSlotWithCredential dispatches on the credential type.
func addSlotWithCredential(ctx context.Context, cfg *Config, device string, cred *Credential, newKey []byte) (int, error) {
	if cred.TokenType != "" {
		return addSlotWithToken(ctx, cfg, device, cred.TokenType, newKey)
	}
	credCopy := append([]byte(nil), cred.Key...)
	return AddKeySlot(ctx, cfg, device, credCopy, newKey)
}

// addSlotWithToken enrolls a new key using a LUKS2 token (e.g. the TPM2
// plugin) as the unlock credential - no secret needed.
func addSlotWithToken(ctx context.Context, cfg *Config, device, tokenType string, newKey []byte) (int, error) {
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
			"--token-only", "--token-type", tokenType,
			"--new-keyfile=/proc/self/fd/3",
			"--pbkdf", "pbkdf2", "--hash", "sha512", "--pbkdf-force-iterations", "1000",
			device,
		},
		FD3: newKey,
	})
	if err != nil {
		return -1, fmt.Errorf("luks: add key slot via %s token on %s: %w", tokenType, device, err)
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

// resumeRotation retries the confirm step for a slot created before a crash.
func resumeRotation(ctx context.Context, cfg *Config, client EscrowClient, vol *Volume, header *Header, vs *VolumeState, state *State) error {
	slot := header.FindSlot(vs.NewSlot)
	if slot == nil || slot.Fingerprint != vs.Fingerprint {
		return fmt.Errorf("luks: recorded rotation slot %d no longer matches the header", vs.NewSlot)
	}
	if err := client.ConfirmRecoveryKey(ctx, vol.LuksUUID, vs.EscrowID, vs.NewSlot, vs.Fingerprint); err != nil {
		return err
	}
	log.Printf("luks: resumed rotation of %s (escrow %s confirmed after restart)", vol.LuksUUID, vs.EscrowID)
	vs.Step = RotationStepConfirmed
	_ = state.Save(cfg)
	// The key itself is gone (memory only), so old slots are wiped
	// credential-free under the invariants.
	for _, old := range vs.OldSlots {
		if err := CanWipeSlot(header, old, vs.NewSlot); err != nil {
			log.Printf("luks: keeping old slot %d on %s: %v", old, vol.DevicePath, err)
			continue
		}
		if err := KillSlot(ctx, cfg, vol.DevicePath, old); err != nil {
			log.Printf("luks: wipe old recovery slot %d on %s: %v", old, vol.DevicePath, err)
		}
	}
	oldToken := ""
	if header.BorEscrow != nil {
		oldToken = header.BorEscrow.TokenID
	}
	if err := UpsertBorEscrowToken(ctx, cfg, vol.DevicePath, oldToken, vs.EscrowID, vs.NewSlot, ""); err != nil {
		log.Printf("luks: update bor-escrow token on %s: %v", vol.DevicePath, err)
	}
	vs.ClearRotation()
	_ = state.Save(cfg)
	return nil
}

// finishRotation wipes the old Bor slots with the fresh key and updates the
// marker token.
func finishRotation(ctx context.Context, cfg *Config, vol *Volume, header *Header, vs *VolumeState, key []byte, serverName string) {
	var wipe []int
	for _, old := range vs.OldSlots {
		if err := CanWipeSlot(header, old, vs.NewSlot); err != nil {
			log.Printf("luks: keeping old slot %d on %s: %v", old, vol.DevicePath, err)
			continue
		}
		wipe = append(wipe, old)
	}
	if len(wipe) > 0 {
		keyCopy := append([]byte(nil), key...)
		if err := WipeSlotsWithKey(ctx, cfg, vol.DevicePath, keyCopy, wipe); err != nil {
			log.Printf("luks: wipe old recovery slots on %s: %v", vol.DevicePath, err)
		}
	}
	oldToken := ""
	if header.BorEscrow != nil {
		oldToken = header.BorEscrow.TokenID
	}
	if err := UpsertBorEscrowToken(ctx, cfg, vol.DevicePath, oldToken, vs.EscrowID, vs.NewSlot, serverName); err != nil {
		log.Printf("luks: update bor-escrow token on %s: %v", vol.DevicePath, err)
	}
	vs.ClearRotation()
}

// cleanupOrphanSlot wipes a slot whose escrow expired before confirmation.
// The wipe is credential-free and only runs when another protector
// remains and Bor still holds a confirmed slot or the volume has other keys.
func cleanupOrphanSlot(ctx context.Context, cfg *Config, vol *Volume, header *Header, vs *VolumeState) {
	slot := header.FindSlot(vs.NewSlot)
	if slot == nil || slot.Fingerprint != vs.Fingerprint {
		return
	}
	others := 0
	for _, ks := range header.Keyslots {
		if ks.Index != vs.NewSlot {
			others++
		}
	}
	if others == 0 {
		log.Printf("luks: orphaned recovery slot %d on %s is the last keyslot; keeping it", vs.NewSlot, vol.DevicePath)
		return
	}
	log.Printf("luks: wiping orphaned recovery slot %d on %s (escrow %s never confirmed)", vs.NewSlot, vol.DevicePath, vs.EscrowID)
	if err := KillSlot(ctx, cfg, vol.DevicePath, vs.NewSlot); err != nil {
		log.Printf("luks: wipe orphaned slot: %v", err)
	}
}

// newEscrowID generates the agent-chosen idempotency key.
var newEscrowID = func() string {
	// UUIDv4 from crypto/rand without an extra dependency.
	var b [16]byte
	mustRandRead(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
