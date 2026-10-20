// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// UpdatesStatePath is the default record of what the Software updates
// type changed on the node, kept so it returns to what it was when the
// node leaves every policy.
const UpdatesStatePath = "/var/lib/bor/agent/updates-state.json"

// UpdatesBackupDir is where the engine configuration files Bor replaced
// are kept.
const UpdatesBackupDir = "/var/lib/bor/agent/updates-backups"

// UpdatesState is the content of the state file.
type UpdatesState struct {
	// Engine is the engine the recorded changes belong to.
	Engine string `json:"engine,omitempty"`
	// ConfigBackup is the backup of the engine configuration Bor replaced
	// (dnf automatic.conf, rpm-ostreed.conf); ConfigExisted says whether
	// the file existed at all.
	ConfigBackup  string `json:"config_backup,omitempty"`
	ConfigExisted bool   `json:"config_existed,omitempty"`
	// ConfigWritten records that Bor owns the engine configuration file.
	ConfigWritten bool `json:"config_written,omitempty"`
	// Timers maps a timer unit to its enabled state before Bor changed it.
	Timers map[string]bool `json:"timers,omitempty"`
	// DropIns are the timer drop-ins Bor wrote.
	DropIns []string `json:"drop_ins,omitempty"`
	// Locks are the zypper package locks Bor added.
	Locks []string `json:"locks,omitempty"`
	// Reboot bookkeeping.
	RebootPendingSince     *time.Time `json:"reboot_pending_since,omitempty"`
	RebootReasons          []string   `json:"reboot_reasons,omitempty"`
	RebootWarnedAt         *time.Time `json:"reboot_warned_at,omitempty"`
	LastPolicyReboot       *time.Time `json:"last_policy_reboot,omitempty"`
	LastPolicyRebootReason string     `json:"last_policy_reboot_reason,omitempty"`
	// Inventory facts the age items read: when the last scan ran, the
	// last metadata refresh it found, and when each pending security
	// update was first seen on the node, by package name.
	LastScan     *time.Time           `json:"last_scan,omitempty"`
	LastCheck    *time.Time           `json:"last_check,omitempty"`
	PendingSince map[string]time.Time `json:"pending_since,omitempty"`
	// PendingFirmware names the firmware updates the last scan found.
	PendingFirmware []string `json:"pending_firmware,omitempty"`
}

// updatesPendingSinceLimit caps the first-seen map; a node with more
// pending security updates than this is tracked by its oldest ones.
const updatesPendingSinceLimit = 1000

// TrackPendingSecurity records when each pending security update was
// first seen and forgets the ones no longer pending. It returns the
// oldest first-seen time, zero when nothing is pending.
func (s *UpdatesState) TrackPendingSecurity(names []string, now time.Time) time.Time {
	if s.PendingSince == nil {
		s.PendingSince = map[string]time.Time{}
	}
	current := make(map[string]bool, len(names))
	for _, n := range names {
		current[n] = true
		if _, ok := s.PendingSince[n]; !ok && len(s.PendingSince) < updatesPendingSinceLimit {
			s.PendingSince[n] = now
		}
	}
	for n := range s.PendingSince {
		if !current[n] {
			delete(s.PendingSince, n)
		}
	}
	var oldest time.Time
	for _, t := range s.PendingSince {
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	if len(s.PendingSince) == 0 {
		s.PendingSince = nil
	}
	return oldest
}

func newUpdatesState() *UpdatesState {
	return &UpdatesState{Timers: map[string]bool{}}
}

// Empty reports whether nothing is recorded.
func (s *UpdatesState) Empty() bool {
	return s.ConfigBackup == "" && !s.ConfigWritten && len(s.Timers) == 0 && len(s.DropIns) == 0 && len(s.Locks) == 0 &&
		s.RebootPendingSince == nil && s.RebootWarnedAt == nil && s.LastPolicyReboot == nil &&
		s.LastScan == nil && len(s.PendingSince) == 0
}

// LoadUpdatesState reads the state file; a missing file yields an empty
// state.
func LoadUpdatesState(path string) (*UpdatesState, error) {
	st := newUpdatesState()
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from agent configuration
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read updates state: %w", err)
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse updates state: %w", err)
	}
	if st.Timers == nil {
		st.Timers = map[string]bool{}
	}
	return st, nil
}

// SaveUpdatesState writes the state file, or removes it when nothing is
// recorded.
func SaveUpdatesState(path string, st *UpdatesState) error {
	if st.Empty() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove updates state: %w", err)
		}
		return nil
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode updates state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return WriteFileAtomically(path, data)
}
