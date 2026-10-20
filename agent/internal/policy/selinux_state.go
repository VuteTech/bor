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

// SelinuxStatePath is the default record of the SELinux values Bor
// changed, kept so they return to what they were when they leave every
// policy.
const SelinuxStatePath = "/var/lib/bor/agent/selinux-state.json"

// SelinuxModeOriginal is the mode the node had before a policy set one.
type SelinuxModeOriginal struct {
	ConfigMode string    `json:"config_mode"`
	Enforcing  bool      `json:"enforcing"`
	RecordedAt time.Time `json:"recorded_at"`
}

// SelinuxLoginOriginal is a login mapping before Bor changed it.
type SelinuxLoginOriginal struct {
	Existed bool   `json:"existed"`
	Seuser  string `json:"seuser,omitempty"`
	Range   string `json:"range,omitempty"`
}

// SelinuxState is the content of the state file.
type SelinuxState struct {
	Mode *SelinuxModeOriginal `json:"mode,omitempty"`
	// Booleans maps a name to its persistent value before Bor set it.
	Booleans map[string]bool `json:"booleans,omitempty"`
	// Modules maps a module name to its enabled flag before Bor changed it.
	Modules map[string]bool                 `json:"modules,omitempty"`
	Logins  map[string]SelinuxLoginOriginal `json:"logins,omitempty"`
	// Installed maps a Bor module name to the SHA-256 of the CIL source
	// last installed.
	Installed map[string]string `json:"installed,omitempty"`
	// FileContextPrefixes are the paths relabelled by the file contexts
	// module, relabelled again after it is removed.
	FileContextPrefixes []string `json:"file_context_prefixes,omitempty"`
	// RelabelRequested records that Bor created /.autorelabel.
	RelabelRequested bool `json:"relabel_requested,omitempty"`
}

func newSelinuxState() *SelinuxState {
	return &SelinuxState{Booleans: map[string]bool{}, Modules: map[string]bool{}, Logins: map[string]SelinuxLoginOriginal{}, Installed: map[string]string{}}
}

// Empty reports whether nothing is recorded.
func (s *SelinuxState) Empty() bool {
	return s.Mode == nil && len(s.Booleans) == 0 && len(s.Modules) == 0 && len(s.Logins) == 0 && len(s.Installed) == 0 && len(s.FileContextPrefixes) == 0 && !s.RelabelRequested
}

// LoadSelinuxState reads the state file; a missing file yields an empty
// state.
func LoadSelinuxState(path string) (*SelinuxState, error) {
	st := newSelinuxState()
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from agent configuration
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read selinux state: %w", err)
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse selinux state: %w", err)
	}
	if st.Booleans == nil {
		st.Booleans = map[string]bool{}
	}
	if st.Modules == nil {
		st.Modules = map[string]bool{}
	}
	if st.Logins == nil {
		st.Logins = map[string]SelinuxLoginOriginal{}
	}
	if st.Installed == nil {
		st.Installed = map[string]string{}
	}
	return st, nil
}

// SaveSelinuxState writes the state file, or removes it when nothing is
// recorded.
func SaveSelinuxState(path string, st *SelinuxState) error {
	if st.Empty() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove selinux state: %w", err)
		}
		return nil
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode selinux state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return WriteFileAtomically(path, data)
}
