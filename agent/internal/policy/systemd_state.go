// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// SystemdStatePath is the default location of the record of original unit
// states, kept so that a unit returns to what it was when it leaves every
// policy.
const SystemdStatePath = "/var/lib/bor/agent/systemd-state.json"

// SystemdOriginal is the state a unit had the first time Bor touched it.
type SystemdOriginal struct {
	UnitFileState string `json:"unit_file_state"`
	Active        bool   `json:"active"`
	// LinkTarget is the target of the symlink /etc/systemd/system/<unit>
	// that a mask replaced (a distribution's redirect), recreated when
	// the unit is restored.
	LinkTarget string    `json:"link_target,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// SystemdState is the content of the state file.
type SystemdState struct {
	Units map[string]SystemdOriginal `json:"units"`
}

// LoadSystemdState reads the state file; a missing file yields an empty
// state.
func LoadSystemdState(path string) (*SystemdState, error) {
	st := &SystemdState{Units: map[string]SystemdOriginal{}}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from agent configuration
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, fmt.Errorf("read systemd state %s: %w", path, err)
	}
	if err := json.Unmarshal(data, st); err != nil {
		return &SystemdState{Units: map[string]SystemdOriginal{}}, fmt.Errorf("parse systemd state %s: %w", path, err)
	}
	if st.Units == nil {
		st.Units = map[string]SystemdOriginal{}
	}
	return st, nil
}

// SaveSystemdState writes the state file atomically, or removes it when
// no unit is recorded.
func SaveSystemdState(path string, st *SystemdState) error {
	if st == nil || len(st.Units) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove systemd state %s: %w", path, err)
		}
		return nil
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode systemd state: %w", err)
	}
	return WriteFileAtomically(path, append(data, '\n'))
}
