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
)

// ApparmorStatePath is the default record of the AppArmor state Bor
// changed, kept so it returns to what it was when it leaves every policy.
const ApparmorStatePath = "/var/lib/bor/agent/apparmor-state.json"

// ApparmorLinkOriginal is the link state of a profile file before Bor
// touched it.
type ApparmorLinkOriginal struct {
	Disabled bool `json:"disabled"`
	Complain bool `json:"complain"`
}

// ApparmorOverrideOriginal records whether Bor created a local/ file.
type ApparmorOverrideOriginal struct {
	Created bool   `json:"created"`
	Hash    string `json:"hash"`
}

// ApparmorState is the content of the state file.
type ApparmorState struct {
	Links map[string]ApparmorLinkOriginal `json:"links,omitempty"`
	// FlagEdits maps a vendor profile file to the backup of its original.
	FlagEdits map[string]string `json:"flag_edits,omitempty"`
	// Customs maps a custom profile name to the SHA-256 of its text.
	Customs   map[string]string                   `json:"customs,omitempty"`
	Overrides map[string]ApparmorOverrideOriginal `json:"overrides,omitempty"`
	// HomeDirs records that the home directory tunable was written.
	HomeDirs bool `json:"home_dirs,omitempty"`
	// Sysctls maps a sysctl name to its value before Bor set it.
	Sysctls map[string]int `json:"sysctls,omitempty"`
	// SysctlFile records that the sysctl.d file was written.
	SysctlFile bool `json:"sysctl_file,omitempty"`
}

func newApparmorState() *ApparmorState {
	return &ApparmorState{Links: map[string]ApparmorLinkOriginal{}, FlagEdits: map[string]string{}, Customs: map[string]string{}, Overrides: map[string]ApparmorOverrideOriginal{}, Sysctls: map[string]int{}}
}

// Empty reports whether nothing is recorded.
func (s *ApparmorState) Empty() bool {
	return len(s.Links) == 0 && len(s.FlagEdits) == 0 && len(s.Customs) == 0 && len(s.Overrides) == 0 && !s.HomeDirs && len(s.Sysctls) == 0 && !s.SysctlFile
}

// LoadApparmorState reads the state file; a missing file yields an empty
// state.
func LoadApparmorState(path string) (*ApparmorState, error) {
	st := newApparmorState()
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from agent configuration
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read apparmor state: %w", err)
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse apparmor state: %w", err)
	}
	if st.Links == nil {
		st.Links = map[string]ApparmorLinkOriginal{}
	}
	if st.FlagEdits == nil {
		st.FlagEdits = map[string]string{}
	}
	if st.Customs == nil {
		st.Customs = map[string]string{}
	}
	if st.Overrides == nil {
		st.Overrides = map[string]ApparmorOverrideOriginal{}
	}
	if st.Sysctls == nil {
		st.Sysctls = map[string]int{}
	}
	return st, nil
}

// SaveApparmorState writes the state file, or removes it when nothing is
// recorded.
func SaveApparmorState(path string, st *ApparmorState) error {
	if st.Empty() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove apparmor state: %w", err)
		}
		return nil
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode apparmor state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return WriteFileAtomically(path, data)
}
