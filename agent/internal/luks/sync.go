// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// VolumeInfo pairs a discovered volume with its parsed header.
type VolumeInfo struct {
	*Volume
	Header *Header
	// HeaderErr records why the header could not be read.
	HeaderErr error
}

// LuksVersionNumber returns 1 or 2, best-effort (2 when unknown).
func (vi *VolumeInfo) LuksVersionNumber() int {
	// The dm uuid prefix distinguishes CRYPT-LUKS1- from CRYPT-LUKS2-; the
	// discovery keeps only the parsed UUID, so LUKS1 detection happens via
	// the header dump failing the JSON parse. cryptsetup reports LUKS1
	// headers without a JSON area.
	if vi.Header == nil {
		return 1
	}
	return 2
}

// Inventory is the read-only snapshot of the node's disk encryption state.
type Inventory struct {
	Platform *Platform
	Volumes  []*VolumeInfo
	Mounts   []*Mount
	// ExternallyManaged names the vendor FDE stack owning the TPM, or "".
	ExternallyManaged string
}

// ByUUID finds a volume by LUKS UUID.
func (inv *Inventory) ByUUID(uuid string) *VolumeInfo {
	for _, vi := range inv.Volumes {
		if vi.LuksUUID == uuid {
			return vi
		}
	}
	return nil
}

// InScope filters the volumes to the policy's scope (plain swap excluded:
// it has no header to manage).
func (inv *Inventory) InScope(pol *pb.DiskEncryptionPolicy) []*VolumeInfo {
	scoped := ScopeVolumes(volumesOf(inv.Volumes), pol.GetVolumeScope(), pol.GetMountpoints())
	inScope := map[string]bool{}
	for _, v := range scoped {
		if v.LuksUUID != "" {
			inScope[v.LuksUUID] = true
		}
	}
	var out []*VolumeInfo
	for _, vi := range inv.Volumes {
		if vi.LuksUUID != "" && inScope[vi.LuksUUID] {
			out = append(out, vi)
		}
	}
	return out
}

func volumesOf(vis []*VolumeInfo) []*Volume {
	out := make([]*Volume, 0, len(vis))
	for _, vi := range vis {
		out = append(out, vi.Volume)
	}
	return out
}

// BuildInventory discovers volumes, reads headers and collects platform
// facts. Read-only: safe to run without any policy bound.
func BuildInventory(ctx context.Context, cfg *Config) (*Inventory, error) {
	volumes, mounts, err := Discover()
	if err != nil {
		return nil, err
	}
	inv := &Inventory{
		Platform:          CollectPlatform(ctx, cfg),
		Mounts:            mounts,
		ExternallyManaged: ExternallyManagedBy(),
	}
	for _, v := range volumes {
		vi := &VolumeInfo{Volume: v}
		if v.LuksUUID != "" && lookPath(cfg.CryptsetupBinary()) == nil {
			header, err := DumpHeader(ctx, cfg.CryptsetupBinary(), v.DevicePath, v.LuksUUID)
			if err != nil {
				vi.HeaderErr = err
				log.Printf("luks: header of %s (%s): %v", v.MappingName, v.DevicePath, err)
			} else {
				vi.Header = header
			}
		}
		inv.Volumes = append(inv.Volumes, vi)
	}
	return inv, nil
}

// ToProtoReport builds the inventory report.
func (inv *Inventory) ToProtoReport(clientID string, pol *pb.DiskEncryptionPolicy) *pb.ReportDiskEncryptionStateRequest {
	req := &pb.ReportDiskEncryptionStateRequest{
		ClientId: clientID,
		Platform: inv.Platform.ToProto(),
	}
	for _, vi := range inv.Volumes {
		if vi.LuksUUID == "" {
			continue // plain swap is not escrow-relevant
		}
		state := &pb.LuksVolumeState{
			LuksUuid:            vi.LuksUUID,
			MappingName:         vi.MappingName,
			Mountpoints:         vi.Mountpoints,
			IsSystem:            vi.IsSystem,
			LuksVersion:         uint32(vi.LuksVersionNumber()), //nolint:gosec // 1 or 2
			ExternallyManagedBy: inv.ExternallyManaged,
		}
		if vi.Header != nil {
			state.Cipher = vi.Header.Cipher
			state.VolumeKeyBits = uint32(vi.Header.VolumeKeyBits) //nolint:gosec // small positive
			state.Keyslots = vi.Header.ToProto()
			state.JsonAreaFreeBytes = uint32(vi.Header.JSONAreaFree) //nolint:gosec // bounded by json_size
		}
		req.Volumes = append(req.Volumes, state)
	}
	// Unencrypted in-scope mounts (only meaningful with a policy).
	if pol != nil && pol.GetRequireEncryption() {
		for _, m := range ScopeMounts(inv.Mounts, pol.GetVolumeScope(), pol.GetMountpoints()) {
			if m.Volume == nil {
				req.UnencryptedSystemMounts = append(req.UnencryptedSystemMounts, &pb.UnencryptedMount{
					Mountpoint: m.Mountpoint,
					Source:     m.Source,
					Fstype:     m.Fstype,
				})
			}
		}
	}
	return req
}

// Enforce applies the effective policy to every in-scope volume: recovery escrow/rotation, TPM2 enrollment/reseal, Tang bindings and
// bootstrap-slot retirement. It returns the per-volume items; the caller
// re-reads the inventory afterwards for the phase-B report.
func Enforce(ctx context.Context, cfg *Config, client EscrowClient, pol *pb.DiskEncryptionPolicy,
	tasks []*pb.DiskEncryptionTask, inv *Inventory, serverName string) []Item {
	var items []Item

	state, err := LoadState(cfg)
	if err != nil {
		log.Printf("luks: load state: %v", err)
		state = &State{Volumes: map[string]*VolumeState{}}
	}

	rotationRequested := map[string]pb.RecoveryKeyReason{}
	for _, t := range tasks {
		if t.GetKind() == pb.DiskEncryptionTask_ROTATE_RECOVERY_KEY {
			rotationRequested[strings.ToLower(t.GetLuksUuid())] = t.GetReason()
		}
	}

	for _, vi := range inv.InScope(pol) {
		if vi.Header == nil {
			items = append(items, Item{
				SchemaID: "luks:recovery", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "the LUKS header could not be read",
			})
			continue
		}
		items = append(items, enforceVolume(ctx, cfg, client, pol, vi, rotationRequested, inv, state, serverName)...)
	}
	return items
}

// enforceVolume runs the per-volume mutations.
func enforceVolume(ctx context.Context, cfg *Config, client EscrowClient, pol *pb.DiskEncryptionPolicy,
	vi *VolumeInfo, rotations map[string]pb.RecoveryKeyReason, inv *Inventory, state *State, serverName string) []Item {
	var items []Item
	header := vi.Header

	wantRecovery := pol.GetRecovery().GetEscrow()
	wantTPM2 := pol.GetTpm2().GetEnabled() && inv.ExternallyManaged == ""
	wantTang := pol.GetTang().GetEnabled()

	// ── Credential ───────────────────────────────────────────────
	needsMutation := wantRecovery || wantTPM2 || wantTang
	var cred *Credential
	rotationID := ""
	if needsMutation {
		serverAssisted := effectiveServerAssisted(pol)
		cred, rotationID = ResolveCredential(ctx, cfg, client, vi.Volume, header, serverAssisted)
		if cred == nil {
			// A resumable rotation needs no credential; try it first.
			vs := state.Volume(vi.LuksUUID)
			if vs.Step == RotationStepSlotAdded {
				if err := resumeRotation(ctx, cfg, client, vi.Volume, header, vs, state); err == nil {
					items = append(items,
						Item{
							SchemaID: "luks:recovery", Key: vi.LuksUUID,
							Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
							Message: "recovery key escrowed (resumed after restart)",
						},
						Item{
							SchemaID: "luks:adoption", Key: vi.LuksUUID,
							Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
						})
					return items
				}
			}
			items = append(items, Item{
				SchemaID: "luks:adoption", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "adoption required - no unlock credential (run `bor-agent luks adopt`, see docs/disk-encryption.md#adoption)",
			})
			return items
		}
		defer func() {
			if cred.Key != nil {
				Zero(cred.Key)
			}
		}()
		items = append(items, Item{SchemaID: "luks:adoption", Key: vi.LuksUUID,
			Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: "credential: " + cred.Source})
	}

	// ── Recovery key ─────────────────────────────────────────────
	bootstrapUsed := cred != nil && cred.Source == "bootstrap"
	if wantRecovery {
		borSlots := header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY)
		reason, taskPending := rotations[strings.ToLower(vi.LuksUUID)]
		needRotation := len(borSlots) == 0 || taskPending || rotationID != "" ||
			state.Volume(vi.LuksUUID).Step == RotationStepSlotAdded
		if reason == pb.RecoveryKeyReason_RECOVERY_KEY_REASON_UNSPECIFIED {
			if len(borSlots) == 0 {
				reason = pb.RecoveryKeyReason_RECOVERY_KEY_REASON_INITIAL
			} else if rotationID != "" {
				reason = pb.RecoveryKeyReason_RECOVERY_KEY_REASON_AFTER_RELEASE
			}
		}
		if needRotation {
			credCopy := &Credential{TokenType: cred.TokenType, Source: cred.Source}
			if cred.Key != nil {
				credCopy.Key = append([]byte(nil), cred.Key...)
			}
			if err := RotateRecoveryKey(ctx, cfg, client, vi.Volume, header, credCopy, reason, rotationID, serverName, state); err != nil {
				log.Printf("luks: recovery rotation on %s: %v", vi.LuksUUID, err)
				items = append(items, Item{
					SchemaID: "luks:recovery", Key: vi.LuksUUID,
					Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
					Message: "recovery key escrow failed: " + err.Error(),
				})
			} else {
				items = append(items, Item{
					SchemaID: "luks:recovery", Key: vi.LuksUUID,
					Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
					Message: "recovery key escrowed",
				})
				// The header changed; re-read it for the following steps.
				if fresh, err := DumpHeader(ctx, cfg.CryptsetupBinary(), vi.DevicePath, vi.LuksUUID); err == nil {
					header = fresh
					vi.Header = fresh
				}
			}
		} else {
			items = append(items, Item{
				SchemaID: "luks:recovery", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
				Message: "recovery key escrowed",
			})
		}
	}

	// ── TPM2 ─────────────────────────────────────────────────────
	if pol.GetTpm2().GetEnabled() {
		items = append(items, enforceTPM2(ctx, cfg, pol, vi, header, cred, inv, state)...)
	}

	// ── Tang ─────────────────────────────────────────────────────
	if wantTang {
		items = append(items, enforceTang(ctx, cfg, pol, vi, header, cred)...)
		if fresh, err := DumpHeader(ctx, cfg.CryptsetupBinary(), vi.DevicePath, vi.LuksUUID); err == nil {
			header = fresh
			vi.Header = fresh
		}
	}

	// ── Bootstrap retirement ─────────────────────
	if bootstrapUsed {
		items = append(items, retireBootstrap(ctx, cfg, pol, vi, header, cred)...)
	}

	_ = state.Save(cfg)
	return items
}

// effectiveServerAssisted mirrors the server-side default (unset = true).
func effectiveServerAssisted(pol *pb.DiskEncryptionPolicy) bool {
	rec := pol.GetRecovery()
	if rec == nil || rec.ServerAssistedRotation == nil {
		return true
	}
	return rec.GetServerAssistedRotation()
}

// enforceTPM2 enrolls or reseals the TPM2 slot.
func enforceTPM2(ctx context.Context, cfg *Config, pol *pb.DiskEncryptionPolicy, vi *VolumeInfo,
	header *Header, cred *Credential, inv *Inventory, state *State) []Item {
	t := pol.GetTpm2()
	switch {
	case inv.ExternallyManaged != "":
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
			Message: "TPM enrollment managed by " + inv.ExternallyManaged,
		}}
	case !inv.Platform.TPM2Present:
		status := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
		if pol.GetRequireTpm2() {
			status = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
		}
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status: status, Message: "no TPM 2.0 present",
		}}
	case t.GetBackend() == pb.TpmBackend_TPM_BACKEND_CLEVIS:
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
			Message: "the Clevis tpm2 backend is not implemented yet (Phase 2)",
		}}
	case inv.Platform.InitramfsGenerator != "dracut":
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
			Message: inv.Platform.InitramfsGenerator + " systems cannot unlock systemd TPM2 enrollments at boot (Phase 2)",
		}}
	}

	pcrs := PCRList(t)
	tpmSlots := header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_TPM2)
	autoReseal := t.AutoReseal == nil || t.GetAutoReseal()

	var current *Keyslot
	var stale []int
	for _, s := range tpmSlots {
		if current == nil && PCRsMatch(s, pcrs) {
			current = s
		} else {
			stale = append(stale, s.Index)
		}
	}

	healthy := false
	if current != nil {
		healthy = TPM2Healthy(ctx, cfg, vi.DevicePath)
	}

	needEnroll := current == nil || (!healthy && autoReseal && resealAllowed(vi.LuksUUID, state))
	if !needEnroll {
		if current != nil && !healthy {
			return []Item{{
				SchemaID: "luks:tpm2", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "the TPM2 enrollment does not unseal in the current PCR state and auto_reseal is off or rate-limited",
			}}
		}
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: fmt.Sprintf("TPM2 enrolled (PCR %s)", pcrArg(pcrs)),
		}}
	}

	// Enroll the new slot; the stale slots are wiped in the same call, only
	// after the enrollment succeeded.
	wipe := stale
	if current != nil && !healthy {
		wipe = append(wipe, current.Index)
	}
	credCopy := &Credential{TokenType: cred.TokenType, Source: cred.Source}
	if cred.Key != nil {
		credCopy.Key = append([]byte(nil), cred.Key...)
	}
	if credCopy.TokenType == "systemd-tpm2" && !healthy {
		// The stale TPM cannot unlock its own re-enrollment.
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: "TPM2 reseal needs a non-TPM credential; none was available",
		}}
	}
	if err := EnrollTPM2(ctx, cfg, vi.DevicePath, credCopy, pcrs, wipe); err != nil {
		return []Item{{
			SchemaID: "luks:tpm2", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: "TPM2 enrollment failed: " + err.Error(),
		}}
	}
	if current != nil && !healthy {
		markResealed(vi.LuksUUID, state)
		log.Printf("luks: TPM2 re-enrolled on %s after a PCR change (luks.tpm_reseal)", vi.LuksUUID)
	}
	return []Item{{
		SchemaID: "luks:tpm2", Key: vi.LuksUUID,
		Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		Message: fmt.Sprintf("TPM2 enrolled (PCR %s)", pcrArg(pcrs)),
	}}
}

// resealAllowed rate-limits auto-reseal to once per boot.
func resealAllowed(uuid string, state *State) bool {
	return state.Volume(uuid).LastResealBoot != bootID()
}

func markResealed(uuid string, state *State) {
	state.Volume(uuid).LastResealBoot = bootID()
}

// bootID reads the kernel boot id.
func bootID() string {
	data, err := readFile("proc/sys/kernel/random/boot_id")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func readFile(rel string) ([]byte, error) {
	return os.ReadFile(rooted(rel)) //nolint:gosec // fixed procfs path (test-rooted)
}

// enforceTang reconciles the volume's Clevis bindings with the policy's
// server set: bind new -> verify -> unbind stale.
func enforceTang(ctx context.Context, cfg *Config, pol *pb.DiskEncryptionPolicy, vi *VolumeInfo,
	header *Header, cred *Credential) []Item {
	tang := pol.GetTang()
	if !ClevisAvailable(cfg) {
		// Missing tooling is NON_COMPLIANT, not INAPPLICABLE: a required
		// security control is absent.
		return []Item{{
			SchemaID: "luks:tang", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
			Message: "clevis is not installed; install clevis-luks and clevis-dracut",
		}}
	}

	clevisSlots := header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS)
	var current *Keyslot
	var stale []*Keyslot
	for _, s := range clevisSlots {
		if current == nil && TangBindingCurrent(s, tang.GetServers(), tang.GetThreshold()) {
			current = s
		} else {
			stale = append(stale, s)
		}
	}
	if current != nil && len(stale) == 0 {
		return []Item{{
			SchemaID: "luks:tang", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: fmt.Sprintf("Tang binding current (%d server(s))", len(tang.GetServers())),
		}}
	}

	if current == nil {
		// LUKS2 JSON area budget: Clevis tokens embed the advertisement.
		if header.JSONAreaFree > 0 && header.JSONAreaFree < 2048 {
			return []Item{{
				SchemaID: "luks:tang", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: fmt.Sprintf("only %d bytes left in the LUKS2 metadata area; re-format with --luks2-metadata-size to bind Tang", header.JSONAreaFree),
			}}
		}
		credCopy := cred.Key
		if credCopy == nil && cred.TokenType == "systemd-tpm2" {
			return []Item{{
				SchemaID: "luks:tang", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "binding Tang needs a key credential (clevis reads it from stdin); none was available",
			}}
		}
		slot, err := ClevisBind(ctx, cfg, vi.DevicePath, append([]byte(nil), credCopy...), tang.GetServers(), tang.GetThreshold())
		if err != nil {
			return []Item{{
				SchemaID: "luks:tang", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "Tang bind failed: " + err.Error(),
			}}
		}
		// Verify the new binding end to end before touching stale slots.
		pass, err := ClevisPass(ctx, cfg, vi.DevicePath, slot)
		if err != nil {
			return []Item{{
				SchemaID: "luks:tang", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "the new Tang binding did not verify: " + err.Error(),
			}}
		}
		Zero(pass)
	}

	for _, s := range stale {
		if err := ClevisUnbind(ctx, cfg, vi.DevicePath, s.Index); err != nil {
			log.Printf("luks: unbind stale clevis slot %d on %s: %v", s.Index, vi.LuksUUID, err)
		} else {
			log.Printf("luks: re-bound Tang on %s (stale slot %d removed)", vi.LuksUUID, s.Index)
		}
	}
	return []Item{{
		SchemaID: "luks:tang", Key: vi.LuksUUID,
		Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		Message: fmt.Sprintf("Tang bound to %d server(s)", len(tang.GetServers())),
	}}
}

// retireBootstrap wipes the provisioning slot once the automatic protectors
// are in place (invariant 3: never before the escrowed recovery slot is
// confirmed), then deletes the bootstrap file.
func retireBootstrap(ctx context.Context, cfg *Config, pol *pb.DiskEncryptionPolicy, vi *VolumeInfo,
	header *Header, cred *Credential) []Item {
	if cred.Key == nil {
		return nil
	}
	borSlots := header.SlotsOfKind(pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY)
	if pol.GetRecovery().GetEscrow() && len(borSlots) == 0 {
		return []Item{{
			SchemaID: "luks:bootstrap", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
			Message: "the bootstrap slot stays until a recovery key is escrowed",
		}}
	}
	keyCopy := append([]byte(nil), cred.Key...)
	slot, err := TestKeyAnySlot(ctx, cfg, vi.DevicePath, header, keyCopy)
	Zero(keyCopy)
	if err != nil {
		// The bootstrap slot is already gone; just consume the file.
		ConsumeBootstrapKey(cfg, vi.LuksUUID)
		return []Item{{
			SchemaID: "luks:bootstrap", Key: vi.LuksUUID,
			Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		}}
	}
	confirmed := -1
	if len(borSlots) > 0 {
		confirmed = borSlots[0].Index
	}
	if confirmed >= 0 {
		if err := CanWipeSlot(header, slot, confirmed); err != nil {
			return []Item{{
				SchemaID: "luks:bootstrap", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: err.Error(),
			}}
		}
	}
	if err := KillSlot(ctx, cfg, vi.DevicePath, slot); err != nil {
		return []Item{{
			SchemaID: "luks:bootstrap", Key: vi.LuksUUID,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: "wiping the bootstrap slot failed: " + err.Error(),
		}}
	}
	ConsumeBootstrapKey(cfg, vi.LuksUUID)
	log.Printf("luks: bootstrap slot %d on %s consumed and wiped", slot, vi.LuksUUID)
	return []Item{{
		SchemaID: "luks:bootstrap", Key: vi.LuksUUID,
		Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
	}}
}

// EnforceInitramfs handles the initramfs modes: VERIFY_ONLY checks and reports; MANAGE
// writes the dracut drop-in, adds Bor's crypttab option tokens and rebuilds
// (rate-limited). Returns the items and the managed file paths written.
func EnforceInitramfs(ctx context.Context, pol *pb.DiskEncryptionPolicy, inv *Inventory) (items []Item, managedPaths []string) {
	wantTPM2 := pol.GetTpm2().GetEnabled() && inv.Platform.TPM2Present && inv.ExternallyManaged == ""
	wantTang := pol.GetTang().GetEnabled()
	if !wantTPM2 && !wantTang {
		return nil, nil
	}
	if inv.Platform.InitramfsGenerator != "dracut" {
		return []Item{{
			SchemaID: "luks:initramfs", Key: "initramfs",
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
			Message: inv.Platform.InitramfsGenerator + " systems are verified in Phase 2",
		}}, nil
	}

	inScope := inv.InScope(pol)
	var mappingNames []string
	tangOnRoot := false
	for _, vi := range inScope {
		mappingNames = append(mappingNames, vi.MappingName)
		if wantTang && vi.IsSystem {
			tangOnRoot = true
		}
	}

	manage := pol.GetInitramfsMode() == pb.InitramfsMode_INITRAMFS_MODE_MANAGE
	if manage {
		changed := false
		if c, err := WriteDracutDropIn(DracutDropInContent(wantTPM2, tangOnRoot)); err != nil {
			return []Item{{
				SchemaID: "luks:initramfs", Key: "initramfs",
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "writing the dracut drop-in failed: " + err.Error(),
			}}, nil
		} else if c {
			changed = true
		}
		managedPaths = append(managedPaths, DracutDropInPath, CrypttabPath)
		if wantTPM2 {
			for _, name := range mappingNames {
				c, err := EnsureCrypttabOptions(rooted(strings.TrimPrefix(CrypttabPath, "/")), name, []string{"tpm2-device=auto"})
				if err != nil {
					log.Printf("luks: crypttab options for %s: %v", name, err)
					continue
				}
				changed = changed || c
			}
		}
		if changed {
			if err := RebuildInitramfs(ctx); err != nil {
				return append(items, Item{
					SchemaID: "luks:initramfs", Key: "initramfs",
					Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
					Message: err.Error(),
				}), managedPaths
			}
		}
	}

	check := VerifyInitramfs(ctx, wantTPM2, wantTang, tangOnRoot, mappingNames)
	switch check.Status {
	case "ok":
		items = append(items, Item{
			SchemaID: "luks:initramfs", Key: "initramfs",
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: "boot-time unlock support present (takes effect at next boot)",
		})
	case "unknown":
		items = append(items, Item{
			SchemaID: "luks:initramfs", Key: "initramfs",
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: strings.Join(check.Missing, "; "),
		})
	default:
		items = append(items, Item{
			SchemaID: "luks:initramfs", Key: "initramfs",
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
			Message: strings.Join(check.Missing, "; "),
		})
	}
	return items, managedPaths
}
