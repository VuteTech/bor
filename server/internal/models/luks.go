// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package models

import (
	"encoding/json"
	"time"
)

// Recovery key lifecycle states.
const (
	RecoveryKeyStatusPending    = "pending"
	RecoveryKeyStatusActive     = "active"
	RecoveryKeyStatusSuperseded = "superseded"
	RecoveryKeyStatusRetired    = "retired"
	RecoveryKeyStatusExpired    = "expired"
	RecoveryKeyStatusDestroyed  = "destroyed"
)

// LuksVolume is one LUKS volume reported by a node. Escrowed keys anchor to
// it; node_id is NULL for orphaned volumes (the node was deleted but the
// disk may still need recovery).
type LuksVolume struct {
	ID                  string          `json:"id" db:"id"`
	NodeID              *string         `json:"node_id" db:"node_id"`
	NodeName            string          `json:"node_name" db:"node_name"`
	MachineID           string          `json:"machine_id,omitempty" db:"machine_id"`
	LuksUUID            string          `json:"luks_uuid" db:"luks_uuid"`
	MappingName         string          `json:"mapping_name" db:"mapping_name"`
	Mountpoints         []string        `json:"mountpoints"`
	IsSystem            bool            `json:"is_system" db:"is_system"`
	LuksVersion         int             `json:"luks_version" db:"luks_version"`
	Cipher              string          `json:"cipher" db:"cipher"`
	VolumeKeyBits       int             `json:"volume_key_bits" db:"volume_key_bits"`
	StateJSON           json.RawMessage `json:"state,omitempty" db:"state_json"`
	RotationRequestedAt *time.Time      `json:"rotation_requested_at" db:"rotation_requested_at"`
	RotationReason      string          `json:"rotation_reason,omitempty" db:"rotation_reason"`
	LastReportedAt      time.Time       `json:"last_reported_at" db:"last_reported_at"`
	OrphanedAt          *time.Time      `json:"orphaned_at,omitempty" db:"orphaned_at"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`

	// ActiveKey is the metadata of the ACTIVE recovery key, when one exists.
	// Never carries key material.
	ActiveKey *LuksRecoveryKeyMeta `json:"active_key,omitempty"`
	// CloneSuspected is set when another node reports the same LUKS UUID
	// (shared volume key from a cloned image).
	CloneSuspected bool `json:"clone_suspected,omitempty"`
}

// LuksRecoveryKeyMeta is the metadata of an escrowed recovery key.
// Key material is never part of any model; reveals go through the dedicated
// reveal endpoint only.
type LuksRecoveryKeyMeta struct {
	ID                 string     `json:"id" db:"id"`
	VolumeID           string     `json:"volume_id" db:"volume_id"`
	Status             string     `json:"status" db:"status"`
	Reason             string     `json:"reason" db:"reason"`
	Keyslot            *int       `json:"keyslot" db:"keyslot"`
	KeyslotFingerprint string     `json:"keyslot_fingerprint,omitempty" db:"keyslot_fingerprint"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	ConfirmedAt        *time.Time `json:"confirmed_at" db:"confirmed_at"`
	RetiredAt          *time.Time `json:"retired_at,omitempty" db:"retired_at"`
	DestroyAfter       *time.Time `json:"destroy_after,omitempty" db:"destroy_after"`
	RevealCount        int        `json:"reveal_count" db:"reveal_count"`
	LastRevealedAt     *time.Time `json:"last_revealed_at,omitempty" db:"last_revealed_at"`
	LastRevealedBy     string     `json:"last_revealed_by,omitempty" db:"last_revealed_by"`
	ReleasedAt         *time.Time `json:"released_at,omitempty" db:"released_at"`
}

// NodeDiskEncryption holds per-node platform facts.
type NodeDiskEncryption struct {
	NodeID                  string          `json:"node_id" db:"node_id"`
	TPM2Present             *bool           `json:"tpm2_present" db:"tpm2_present"`
	SecureBoot              string          `json:"secure_boot" db:"secure_boot"`
	InitramfsGenerator      string          `json:"initramfs_generator" db:"initramfs_generator"`
	SystemdVersion          string          `json:"systemd_version" db:"systemd_version"`
	CryptsetupVersion       string          `json:"cryptsetup_version" db:"cryptsetup_version"`
	ClevisVersion           string          `json:"clevis_version" db:"clevis_version"`
	UnencryptedSystemMounts json.RawMessage `json:"unencrypted_system_mounts" db:"unencrypted_system_mounts"`
	ReportedAt              time.Time       `json:"reported_at" db:"reported_at"`
}

// DiskEncryptionSummary feeds the fleet page StatCards.
type DiskEncryptionSummary struct {
	Volumes           int  `json:"volumes"`
	EncryptedNodes    int  `json:"encrypted_nodes"`
	UnencryptedMounts int  `json:"unencrypted_mounts"`
	EscrowedActive    int  `json:"escrowed_active"`
	RotationsOverdue  int  `json:"rotations_overdue"`
	ExternallyManaged int  `json:"externally_managed"`
	CloneSuspects     int  `json:"clone_suspects"`
	EscrowConfigured  bool `json:"escrow_configured"`
}

// LuksVolumeListResponse is the paginated fleet directory response.
type LuksVolumeListResponse struct {
	Items      []*LuksVolume `json:"items"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	PerPage    int           `json:"per_page"`
	TotalPages int           `json:"total_pages"`
}

// NodeDiskEncryptionResponse is the node drawer payload.
type NodeDiskEncryptionResponse struct {
	Platform *NodeDiskEncryption `json:"platform"`
	Volumes  []*LuksVolume       `json:"volumes"`
}

// LuksVolumeDetailResponse is the fleet page row drill-down.
type LuksVolumeDetailResponse struct {
	Volume *LuksVolume            `json:"volume"`
	Keys   []*LuksRecoveryKeyMeta `json:"keys"`
}

// RevealRecoveryKeyRequest asks for the plaintext of an escrowed key.
type RevealRecoveryKeyRequest struct {
	// Reason is a mandatory free-text reason or ticket number (audited).
	Reason string `json:"reason"`
	// EscrowID selects a previous (superseded/retired) key; empty = ACTIVE.
	EscrowID string `json:"escrow_id,omitempty"`
}

// RevealRecoveryKeyResponse carries the plaintext exactly once.
type RevealRecoveryKeyResponse struct {
	RecoveryKey     string    `json:"recovery_key"`
	EscrowID        string    `json:"escrow_id"`
	Keyslot         *int      `json:"keyslot"`
	CreatedAt       time.Time `json:"created_at"`
	RotationPending bool      `json:"rotation_pending"`
}

// TangServer is a registered external Tang server (Settings -> Tang servers).
type TangServer struct {
	ID                    string          `json:"id" db:"id"`
	Name                  string          `json:"name" db:"name"`
	URL                   string          `json:"url" db:"url"`
	TrustedThumbprints    []string        `json:"trusted_thumbprints"`
	AdvertisedThumbprints []string        `json:"advertised_thumbprints"`
	Advertisement         json.RawMessage `json:"advertisement,omitempty" db:"advertisement"`
	LastCheckAt           *time.Time      `json:"last_check_at" db:"last_check_at"`
	LastCheckStatus       string          `json:"last_check_status" db:"last_check_status"`
	LastCheckError        string          `json:"last_check_error,omitempty" db:"last_check_error"`
	CreatedBy             string          `json:"created_by,omitempty" db:"created_by"`
	CreatedAt             time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at" db:"updated_at"`

	// BoundVolumes counts bound volumes per signing thumbprint, computed
	// from inventory reports (rotation fleet view).
	BoundVolumes map[string]int `json:"bound_volumes,omitempty"`
}

// TangServerRequest is the create/update payload for a Tang server.
type TangServerRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// TrustedThumbprints are RFC 7638 S256 thumbprints confirmed by the
	// admin; the first entry is the preferred signing key.
	TrustedThumbprints []string `json:"trusted_thumbprints"`
}

// TangProbeRequest asks the server to fetch a Tang advertisement.
type TangProbeRequest struct {
	URL string `json:"url"`
}

// TangProbeResponse returns the advertised signing thumbprints for the admin
// to compare against `tang-show-keys` on the Tang host.
type TangProbeResponse struct {
	URL                 string   `json:"url"`
	SigningThumbprints  []string `json:"signing_thumbprints"`
	ExchangeThumbprints []string `json:"exchange_thumbprints"`
}

// TangServerListResponse lists registered Tang servers.
type TangServerListResponse struct {
	Items []*TangServer `json:"items"`
}

// StepUpRequest is the body of POST /api/v1/auth/step-up.
type StepUpRequest struct {
	// Purpose scopes the token to one action, e.g. "reveal_recovery_key".
	Purpose  string `json:"purpose"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

// StepUpResponse carries the single-use step-up token.
type StepUpResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}
