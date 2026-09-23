// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// MergePolicies merges bound DiskEncryption policies (ascending binding
// priority: later entries win the "highest-priority" fields) into the
// node's one effective configuration. Kept in exact parity with the
// server's merge: require_* and recovery.escrow are OR-ed;
// rotation_interval_days takes the smallest non-zero value; everything else
// comes from the highest-priority policy that sets it. Tang server lists
// are NOT unioned - a union would silently widen who can unlock the disk.
func MergePolicies(policies []*pb.DiskEncryptionPolicy) *pb.DiskEncryptionPolicy {
	if len(policies) == 0 {
		return nil
	}
	out := &pb.DiskEncryptionPolicy{}
	var smallestRotation *uint32
	for _, p := range policies {
		if p == nil {
			continue
		}
		out.RequireEncryption = out.RequireEncryption || p.GetRequireEncryption()
		out.RequireTpm2 = out.RequireTpm2 || p.GetRequireTpm2()
		out.RequireSecureBoot = out.RequireSecureBoot || p.GetRequireSecureBoot()
		if p.GetVolumeScope() != pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_UNSPECIFIED {
			out.VolumeScope = p.GetVolumeScope()
			out.Mountpoints = p.GetMountpoints()
		}
		if p.GetMinVolumeKeyBits() != 0 {
			out.MinVolumeKeyBits = p.GetMinVolumeKeyBits()
		}
		if len(p.GetAllowedCiphers()) > 0 {
			out.AllowedCiphers = p.GetAllowedCiphers()
		}
		if p.GetTpm2() != nil {
			out.Tpm2 = p.GetTpm2()
		}
		if p.GetTang() != nil {
			out.Tang = p.GetTang()
		}
		if rec := p.GetRecovery(); rec != nil {
			if out.Recovery == nil {
				out.Recovery = &pb.RecoveryKeyPolicy{}
			}
			out.Recovery.Escrow = out.Recovery.Escrow || rec.GetEscrow()
			if rec.RotationIntervalDays != nil {
				d := rec.GetRotationIntervalDays()
				if d != 0 && (smallestRotation == nil || d < *smallestRotation) {
					smallestRotation = &d
				}
			}
			if rec.RotateAfterReveal != nil {
				v := rec.GetRotateAfterReveal()
				out.Recovery.RotateAfterReveal = &v
			}
			if rec.ServerAssistedRotation != nil {
				v := rec.GetServerAssistedRotation()
				out.Recovery.ServerAssistedRotation = &v
			}
		}
		if p.GetPassphraseMode() != pb.PassphraseMode_PASSPHRASE_MODE_UNSPECIFIED {
			out.PassphraseMode = p.GetPassphraseMode()
		}
		if p.GetInitramfsMode() != pb.InitramfsMode_INITRAMFS_MODE_UNSPECIFIED {
			out.InitramfsMode = p.GetInitramfsMode()
		}
		if p.AdoptExisting != nil {
			v := p.GetAdoptExisting()
			out.AdoptExisting = &v
		}
	}
	if out.Recovery != nil && smallestRotation != nil {
		out.Recovery.RotationIntervalDays = smallestRotation
	}
	return out
}
