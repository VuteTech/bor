// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Rotation steps persisted for crash recovery. The state file never
// holds key material - only which step a volume's rotation reached, so a
// restarted agent can resume (confirm a slot that already exists) or clean
// an orphan up instead of leaking keyslots.
const (
	// RotationStepSlotAdded: the keyslot exists and was verified, but the
	// server has not confirmed the escrow yet.
	RotationStepSlotAdded = "slot_added"
	// RotationStepConfirmed: the server confirmed; old slots may still need
	// wiping.
	RotationStepConfirmed = "confirmed"
)

// VolumeState is the persisted per-volume rotation state.
type VolumeState struct {
	Step        string    `json:"step,omitempty"`
	EscrowID    string    `json:"escrow_id,omitempty"`
	NewSlot     int       `json:"new_slot,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	RotationID  string    `json:"rotation_id,omitempty"`
	OldSlots    []int     `json:"old_slots,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	// LastReseal rate-limits TPM auto-reseal to once per boot.
	LastResealBoot string `json:"last_reseal_boot,omitempty"`
}

// State is the content of luks-state.json.
type State struct {
	Volumes map[string]*VolumeState `json:"volumes"`
}

// LoadState reads the state file; a missing file yields an empty state.
func LoadState(cfg *Config) (*State, error) {
	st := &State{Volumes: map[string]*VolumeState{}}
	data, err := os.ReadFile(cfg.StateFilePath()) //nolint:gosec // fixed config path
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, fmt.Errorf("luks: read state file: %w", err)
	}
	if err := json.Unmarshal(data, st); err != nil {
		// A corrupt state file must not wedge enforcement; start fresh.
		return &State{Volumes: map[string]*VolumeState{}}, nil
	}
	if st.Volumes == nil {
		st.Volumes = map[string]*VolumeState{}
	}
	return st, nil
}

// Save writes the state file atomically (0600).
func (s *State) Save(cfg *Config) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := cfg.StateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("luks: create state dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("luks: write state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("luks: replace state file: %w", err)
	}
	return nil
}

// Volume returns (creating) the state of one volume.
func (s *State) Volume(luksUUID string) *VolumeState {
	if v, ok := s.Volumes[luksUUID]; ok {
		return v
	}
	v := &VolumeState{}
	s.Volumes[luksUUID] = v
	return v
}

// ClearRotation resets a volume's rotation bookkeeping.
func (v *VolumeState) ClearRotation() {
	v.Step = ""
	v.EscrowID = ""
	v.NewSlot = 0
	v.Fingerprint = ""
	v.RotationID = ""
	v.OldSlots = nil
	v.StartedAt = time.Time{}
}
