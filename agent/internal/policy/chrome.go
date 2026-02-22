// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ChromeManagedFilename is the filename Bor writes in each Chrome/Chromium
// policy directory. Chrome logs warnings for unknown policy keys, so Bor
// does not add a _comment key — only real policy keys are written.
const ChromeManagedFilename = "bor_managed.json"

// MergeChromePolicies deep-merges multiple Chrome policy JSON objects into
// one and returns the merged JSON bytes ready to write. Chrome policy JSON
// is a flat-ish object (may have nested dicts for some policies).
func MergeChromePolicies(contents []string) ([]byte, error) {
	merged := make(map[string]interface{})

	for _, content := range contents {
		if content == "" || content == "{}" {
			continue
		}
		var partial map[string]interface{}
		if err := json.Unmarshal([]byte(content), &partial); err != nil {
			return nil, fmt.Errorf("failed to parse Chrome policy content: %w", err)
		}
		deepMerge(merged, partial)
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged Chrome policies: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

// SyncChromeDir writes the merged Chrome policies as bor_managed.json into
// the given managed policy directory. The directory is created with mode 0755
// if it does not exist.
// When contents is empty, bor_managed.json is removed if present (no
// backup/restore needed — Bor owns this file exclusively).
func SyncChromeDir(dirPath string, contents []string) error {
	if len(contents) == 0 {
		// No active Chrome policies — remove the managed file if present.
		target := filepath.Join(dirPath, ChromeManagedFilename)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove Chrome managed file %s: %w", target, err)
		}
		return nil
	}

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create Chrome policy directory %s: %w", dirPath, err)
	}

	data, err := MergeChromePolicies(contents)
	if err != nil {
		return fmt.Errorf("failed to merge Chrome policies for %s: %w", dirPath, err)
	}

	target := filepath.Join(dirPath, ChromeManagedFilename)
	if err := WriteFileAtomically(target, data); err != nil {
		return fmt.Errorf("failed to write Chrome policies to %s: %w", target, err)
	}

	return nil
}
