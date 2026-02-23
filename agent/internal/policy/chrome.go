// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// ChromeManagedFilename is the filename Bor writes in each Chrome/Chromium
// policy directory. Chrome logs warnings for unknown policy keys, so Bor
// does not add a _comment key — only real policy keys are written.
const ChromeManagedFilename = "bor_managed.json"

// SyncChromeFromProto merges multiple ChromePolicy protos and syncs the result
// to each Chrome managed-policy directory. It uses protojson to convert each
// proto to Chrome-compatible JSON (respecting json_name options), deep-merges
// them, then writes bor_managed.json to each directory.
// When policies is empty or all nil, bor_managed.json is removed from every dir.
func SyncChromeFromProto(policies []*pb.ChromePolicy, dirPaths []string) error {
	merged := make(map[string]interface{})

	opts := protojson.MarshalOptions{EmitUnpopulated: false}
	for _, pol := range policies {
		if pol == nil {
			continue
		}
		jsonBytes, err := opts.Marshal(pol)
		if err != nil {
			return fmt.Errorf("failed to marshal Chrome policy proto: %w", err)
		}
		var partial map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &partial); err != nil {
			return fmt.Errorf("failed to parse marshalled Chrome policy: %w", err)
		}
		deepMerge(merged, partial)
	}

	if len(merged) == 0 {
		// No policies — remove managed file from all dirs.
		for _, dir := range dirPaths {
			if dir == "" {
				continue
			}
			_ = removeChromeManaged(dir)
		}
		return nil
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged Chrome policies: %w", err)
	}
	data = append(data, '\n')

	for _, dir := range dirPaths {
		if dir == "" {
			continue
		}
		if err := writeChromeManaged(dir, data); err != nil {
			return err
		}
	}
	return nil
}

// writeChromeManaged creates dir (mode 0755) if needed and atomically writes
// data as bor_managed.json inside it.
func writeChromeManaged(dirPath string, data []byte) error {
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create Chrome policy directory %s: %w", dirPath, err)
	}
	target := filepath.Join(dirPath, ChromeManagedFilename)
	if err := WriteFileAtomically(target, data); err != nil {
		return fmt.Errorf("failed to write Chrome policies to %s: %w", target, err)
	}
	return nil
}

// removeChromeManaged removes bor_managed.json from dirPath if it exists.
// A missing file is not an error.
func removeChromeManaged(dirPath string) error {
	target := filepath.Join(dirPath, ChromeManagedFilename)
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove Chrome managed file %s: %w", target, err)
	}
	return nil
}
