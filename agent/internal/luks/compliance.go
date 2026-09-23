// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"fmt"
	"slices"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Item is one per-volume (or per-platform) compliance result.
type Item struct {
	SchemaID string // "luks:encryption", "luks:recovery", "platform:tpm2", …
	Key      string // mountpoint, volume UUID, "tpm", "secureboot"
	Status   pb.ComplianceStatus
	Message  string
}

// Rollup derives the overall status shared by every bound policy: all
// INAPPLICABLE -> INAPPLICABLE; any ERROR -> ERROR; any NON_COMPLIANT ->
// NON_COMPLIANT; else COMPLIANT (the RollupFlatpakCompliance rules).
func Rollup(items []Item) (status pb.ComplianceStatus, message string) {
	if len(items) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no LUKS volumes in scope on this node"
	}
	allInapplicable := true
	var hasError, hasNonCompliant bool
	var msgs []string
	for _, it := range items {
		switch it.Status {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
			// stays inapplicable
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			allInapplicable = false
			hasError = true
			msgs = append(msgs, fmt.Sprintf("%s/%s: %s", it.SchemaID, it.Key, it.Message))
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			allInapplicable = false
			hasNonCompliant = true
			msgs = append(msgs, fmt.Sprintf("%s/%s: %s", it.SchemaID, it.Key, it.Message))
		default:
			allInapplicable = false
		}
	}
	switch {
	case allInapplicable:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "disk encryption is not applicable on this node"
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "disk encryption enforced"
	}
}

// ToProtoItems converts items to wire compliance results.
func ToProtoItems(items []Item) []*pb.ComplianceItemResult {
	out := make([]*pb.ComplianceItemResult, 0, len(items))
	for _, it := range items {
		out = append(out, &pb.ComplianceItemResult{
			SchemaId: it.SchemaID,
			Key:      it.Key,
			Status:   it.Status,
			Message:  it.Message,
		})
	}
	return out
}

// CheckStatic computes the read-only items: encryption
// coverage, cipher/key-size, and the platform requirements. Enforcement
// items (recovery, tpm2, tang, initramfs, bootstrap, adoption) come from
// Enforce.
func CheckStatic(inv *Inventory, pol *pb.DiskEncryptionPolicy) []Item {
	var items []Item

	// ── luks:encryption per in-scope mount ──────────────────────────────
	if pol.GetRequireEncryption() {
		for _, m := range ScopeMounts(inv.Mounts, pol.GetVolumeScope(), pol.GetMountpoints()) {
			switch {
			case m.Volume == nil:
				items = append(items, Item{
					SchemaID: "luks:encryption", Key: m.Mountpoint,
					Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
					Message: fmt.Sprintf("%s (%s) is not encrypted", m.Source, m.Fstype),
				})
			case m.Volume.IsPlain:
				// Swap on plain dm-crypt with a random key counts as
				// encrypted ("ephemeral key").
				items = append(items, Item{
					SchemaID: "luks:encryption", Key: m.Mountpoint,
					Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
					Message: "plain dm-crypt with an ephemeral key",
				})
			default:
				vi := inv.ByUUID(m.Volume.LuksUUID)
				switch {
				case vi != nil && vi.Header == nil:
					items = append(items, Item{
						SchemaID: "luks:encryption", Key: m.Mountpoint,
						Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
						Message: "the LUKS header could not be read",
					})
				case vi != nil && vi.LuksVersionNumber() == 1:
					items = append(items, Item{
						SchemaID: "luks:encryption", Key: m.Mountpoint,
						Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
						Message: "LUKS1 volume: convert offline with `cryptsetup convert --type luks2`",
					})
				default:
					items = append(items, Item{
						SchemaID: "luks:encryption", Key: m.Mountpoint,
						Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
					})
				}
			}
		}
	}

	// ── luks:cipher per in-scope volume ─────────────────────────────────
	allowed := pol.GetAllowedCiphers()
	if len(allowed) == 0 {
		allowed = []string{"aes-xts-plain64"}
	}
	minBits := pol.GetMinVolumeKeyBits()
	if minBits == 0 {
		minBits = 512 // AES-256-XTS
	}
	for _, vi := range inv.InScope(pol) {
		if vi.Header == nil || vi.IsPlain {
			continue
		}
		switch {
		case !slices.Contains(allowed, vi.Header.Cipher):
			items = append(items, Item{
				SchemaID: "luks:cipher", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: fmt.Sprintf("cipher %s is not in the allowed set %v", vi.Header.Cipher, allowed),
			})
		case uint32(vi.Header.VolumeKeyBits) < minBits: //nolint:gosec // key bits are small positive values
			items = append(items, Item{
				SchemaID: "luks:cipher", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: fmt.Sprintf("volume key is %d bits; the policy requires at least %d", vi.Header.VolumeKeyBits, minBits),
			})
		default:
			items = append(items, Item{
				SchemaID: "luks:cipher", Key: vi.LuksUUID,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
				Message: fmt.Sprintf("%s, %d-bit volume key", vi.Header.Cipher, vi.Header.VolumeKeyBits),
			})
		}
	}

	// ── platform:tpm2 ───────────────────────────────────────────────────
	switch {
	case pol.GetRequireTpm2() && !inv.Platform.TPM2Present:
		items = append(items, Item{
			SchemaID: "platform:tpm2", Key: "tpm",
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
			Message: "no TPM 2.0 present but the policy requires one",
		})
	case pol.GetRequireTpm2():
		items = append(items, Item{
			SchemaID: "platform:tpm2", Key: "tpm",
			Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		})
	}

	// ── platform:secureboot ─────────────────────────────────────────────
	if pol.GetRequireSecureBoot() {
		switch inv.Platform.SecureBoot {
		case pb.SecureBootState_SECURE_BOOT_STATE_ENABLED:
			items = append(items, Item{
				SchemaID: "platform:secureboot", Key: "secureboot",
				Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			})
		case pb.SecureBootState_SECURE_BOOT_STATE_UNSPECIFIED:
			items = append(items, Item{
				SchemaID: "platform:secureboot", Key: "secureboot",
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: "the Secure Boot state could not be read from efivars",
			})
		default:
			items = append(items, Item{
				SchemaID: "platform:secureboot", Key: "secureboot",
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "Secure Boot is " + secureBootLabel(inv.Platform.SecureBoot) + " but the policy requires it enabled",
			})
		}
	}

	return items
}

func secureBootLabel(s pb.SecureBootState) string {
	switch s {
	case pb.SecureBootState_SECURE_BOOT_STATE_DISABLED:
		return "disabled"
	case pb.SecureBootState_SECURE_BOOT_STATE_SETUP_MODE:
		return "in setup mode"
	case pb.SecureBootState_SECURE_BOOT_STATE_LEGACY_BIOS:
		return "unavailable (legacy BIOS)"
	default:
		return "unknown"
	}
}
