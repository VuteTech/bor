// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestValidateDiskEncryptionContent(t *testing.T) {
	validThp := strings.Repeat("A", 43)
	tests := []struct {
		name    string
		content string
		wantErr string // expected error substring; empty means the content is valid
	}{
		{
			name:    "empty content",
			content: "{}",
			wantErr: "empty",
		},
		{
			name:    "invalid JSON",
			content: "{not json",
			wantErr: "invalid disk encryption policy",
		},
		{
			name:    "minimal escrow policy",
			content: `{"requireEncryption": true, "recovery": {"escrow": true}}`,
		},
		{
			name:    "no requirement and no protector",
			content: `{"volumeScope": "LUKS_VOLUME_SCOPE_SYSTEM"}`,
			wantErr: "at least one protector",
		},
		{
			name:    "mountpoints without MOUNTPOINTS scope",
			content: `{"requireEncryption": true, "mountpoints": ["/data"]}`,
			wantErr: "only allowed with the MOUNTPOINTS",
		},
		{
			name:    "MOUNTPOINTS scope without mountpoints",
			content: `{"requireEncryption": true, "volumeScope": "LUKS_VOLUME_SCOPE_MOUNTPOINTS"}`,
			wantErr: "requires at least one mountpoint",
		},
		{
			name:    "relative mountpoint",
			content: `{"requireEncryption": true, "volumeScope": "LUKS_VOLUME_SCOPE_MOUNTPOINTS", "mountpoints": ["data"]}`,
			wantErr: "absolute path",
		},
		{
			name:    "custom PCR profile without PCRs",
			content: `{"tpm2": {"enabled": true, "pcrProfile": "TPM_PCR_PROFILE_CUSTOM"}}`,
			wantErr: "non-empty tpm2.pcrs",
		},
		{
			name:    "PCR out of range",
			content: `{"tpm2": {"enabled": true, "pcrProfile": "TPM_PCR_PROFILE_CUSTOM", "pcrs": [24]}}`,
			wantErr: "PCRs are 0..23",
		},
		{
			name:    "firmware PCR without opt-in",
			content: `{"tpm2": {"enabled": true, "pcrProfile": "TPM_PCR_PROFILE_CUSTOM", "pcrs": [0]}}`,
			wantErr: "allow_firmware_pcrs",
		},
		{
			name:    "firmware PCR with opt-in",
			content: `{"tpm2": {"enabled": true, "pcrProfile": "TPM_PCR_PROFILE_CUSTOM", "pcrs": [0, 7], "allowFirmwarePcrs": true}}`,
		},
		{
			name:    "PCRs with SECURE_BOOT profile",
			content: `{"tpm2": {"enabled": true, "pcrProfile": "TPM_PCR_PROFILE_SECURE_BOOT", "pcrs": [7]}}`,
			wantErr: "only allowed with the CUSTOM",
		},
		{
			name:    "tang without servers",
			content: `{"tang": {"enabled": true}}`,
			wantErr: "at least one server",
		},
		{
			name:    "tang without thumbprint",
			content: `{"tang": {"enabled": true, "servers": [{"url": "http://tang1.corp"}]}}`,
			wantErr: "thumbprint is required",
		},
		{
			name:    "tang with invalid thumbprint",
			content: `{"tang": {"enabled": true, "servers": [{"url": "http://tang1.corp", "thumbprint": "short"}]}}`,
			wantErr: "invalid thumbprint",
		},
		{
			name:    "tang valid",
			content: `{"tang": {"enabled": true, "servers": [{"url": "http://tang1.corp", "thumbprint": "` + validThp + `"}]}}`,
		},
		{
			name: "tang threshold above server count",
			content: `{"tang": {"enabled": true, "threshold": 2, "servers": [` +
				`{"url": "http://tang1.corp", "thumbprint": "` + validThp + `"}]}}`,
			wantErr: "threshold",
		},
		{
			name: "too many tang servers",
			content: `{"tang": {"enabled": true, "servers": [` +
				`{"url": "http://t1", "thumbprint": "` + validThp + `"},` +
				`{"url": "http://t2", "thumbprint": "` + validThp + `"},` +
				`{"url": "http://t3", "thumbprint": "` + validThp + `"},` +
				`{"url": "http://t4", "thumbprint": "` + validThp + `"},` +
				`{"url": "http://t5", "thumbprint": "` + validThp + `"}]}}`,
			wantErr: "at most 4",
		},
		{
			name:    "tang https url ok",
			content: `{"tang": {"enabled": true, "servers": [{"url": "https://tang1.corp:8443/tang", "thumbprint": "` + validThp + `"}]}}`,
		},
		{
			name:    "tang bad scheme",
			content: `{"tang": {"enabled": true, "servers": [{"url": "ftp://tang1.corp", "thumbprint": "` + validThp + `"}]}}`,
			wantErr: "expected http(s)://",
		},
		{
			name:    "bor responder is phase 2",
			content: `{"tang": {"enabled": true, "borResponder": true}}`,
			wantErr: "Phase 2",
		},
		{
			name:    "rotation interval too small",
			content: `{"recovery": {"escrow": true, "rotationIntervalDays": 5}}`,
			wantErr: "rotation_interval_days",
		},
		{
			name:    "rotation interval zero (never) is valid",
			content: `{"recovery": {"escrow": true, "rotationIntervalDays": 0}}`,
		},
		{
			name:    "invalid cipher charset",
			content: `{"requireEncryption": true, "allowedCiphers": ["AES!XTS"]}`,
			wantErr: "invalid cipher",
		},
		{
			name:    "full policy",
			content: `{"requireEncryption": true, "requireTpm2": true, "requireSecureBoot": true, "minVolumeKeyBits": 512, "allowedCiphers": ["aes-xts-plain64"], "tpm2": {"enabled": true, "pcrProfile": "TPM_PCR_PROFILE_SECURE_BOOT"}, "tang": {"enabled": true, "servers": [{"url": "http://tang1.corp", "thumbprint": "` + validThp + `"}]}, "recovery": {"escrow": true, "rotationIntervalDays": 180, "rotateAfterReveal": true}, "initramfsMode": "INITRAMFS_MODE_MANAGE"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDiskEncryptionContent(tt.content)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateDiskEncryptionContent() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateDiskEncryptionContent() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecoveryKeyFormat(t *testing.T) {
	valid := "cbdefghi-jklnrtuv-cbdefghi-jklnrtuv-cbdefghi-jklnrtuv-cbdefghi-jklnrtuv"
	if !ValidateRecoveryKeyFormat(valid) {
		t.Error("valid modhex key rejected")
	}
	invalid := []string{
		"",
		strings.ToUpper(valid),              // upper case
		strings.ReplaceAll(valid, "c", "a"), // 'a' not in modhex alphabet
		valid[:70],                          // too short (a full key is 71 chars)
		valid + "c",                         // too long
		strings.ReplaceAll(valid, "-", ""),  // no groups
		valid[:8] + "_" + valid[9:],         // wrong separator
	}
	for _, k := range invalid {
		if ValidateRecoveryKeyFormat(k) {
			t.Errorf("invalid key accepted: %q", k)
		}
	}
}

func boolPtr(b bool) *bool       { return &b }
func uint32Ptr(v uint32) *uint32 { return &v }

func TestMergeDiskEncryptionPolicies(t *testing.T) {
	low := &pb.DiskEncryptionPolicy{
		RequireEncryption: true,
		VolumeScope:       pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_ALL_FIXED,
		Tang: &pb.TangProtector{
			Enabled: true,
			Servers: []*pb.TangServer{{Url: "http://tang-low.corp", Thumbprint: strings.Repeat("A", 43)}},
		},
		Recovery: &pb.RecoveryKeyPolicy{
			Escrow:                 true,
			RotationIntervalDays:   uint32Ptr(90),
			ServerAssistedRotation: boolPtr(false),
		},
	}
	high := &pb.DiskEncryptionPolicy{
		RequireTpm2: true,
		VolumeScope: pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_SYSTEM,
		Tang: &pb.TangProtector{
			Enabled: true,
			Servers: []*pb.TangServer{{Url: "http://tang-high.corp", Thumbprint: strings.Repeat("B", 43)}},
		},
		Recovery: &pb.RecoveryKeyPolicy{
			RotationIntervalDays: uint32Ptr(180),
			RotateAfterReveal:    boolPtr(false),
		},
	}

	merged := MergeDiskEncryptionPolicies([]*pb.DiskEncryptionPolicy{low, high})

	if !merged.GetRequireEncryption() || !merged.GetRequireTpm2() {
		t.Error("require_* flags must be OR-ed")
	}
	if merged.GetVolumeScope() != pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_SYSTEM {
		t.Errorf("volume scope = %v, want the higher-priority SYSTEM", merged.GetVolumeScope())
	}
	// Tang servers are NOT unioned: the higher-priority list wins entirely.
	if n := len(merged.GetTang().GetServers()); n != 1 {
		t.Fatalf("tang servers = %d, want 1 (no union)", n)
	}
	if merged.GetTang().GetServers()[0].GetUrl() != "http://tang-high.corp" {
		t.Errorf("tang server = %s, want the higher-priority one", merged.GetTang().GetServers()[0].GetUrl())
	}
	if !merged.GetRecovery().GetEscrow() {
		t.Error("recovery.escrow must be OR-ed")
	}
	if got := merged.GetRecovery().GetRotationIntervalDays(); got != 90 {
		t.Errorf("rotation interval = %d, want the smallest non-zero (90)", got)
	}
	if merged.GetRecovery().GetRotateAfterReveal() {
		t.Error("rotate_after_reveal must come from the higher-priority policy (false)")
	}
	if merged.GetRecovery().GetServerAssistedRotation() {
		t.Error("server_assisted_rotation=false from the lower policy must survive (high one does not set it)")
	}
}

func TestEffectiveRotationIntervalDays(t *testing.T) {
	if got := EffectiveRotationIntervalDays(nil); got != 0 {
		t.Errorf("nil policy interval = %d, want 0", got)
	}
	noEscrow := &pb.DiskEncryptionPolicy{Recovery: &pb.RecoveryKeyPolicy{}}
	if got := EffectiveRotationIntervalDays(noEscrow); got != 0 {
		t.Errorf("escrow-off interval = %d, want 0", got)
	}
	unset := &pb.DiskEncryptionPolicy{Recovery: &pb.RecoveryKeyPolicy{Escrow: true}}
	if got := EffectiveRotationIntervalDays(unset); got != 180 {
		t.Errorf("unset interval = %d, want default 180", got)
	}
	never := &pb.DiskEncryptionPolicy{Recovery: &pb.RecoveryKeyPolicy{Escrow: true, RotationIntervalDays: uint32Ptr(0)}}
	if got := EffectiveRotationIntervalDays(never); got != 0 {
		t.Errorf("explicit 0 interval = %d, want 0 (never)", got)
	}
}
