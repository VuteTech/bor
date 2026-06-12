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

// EdgeManagedFilename is the filename Bor writes in the Edge policy directory.
const EdgeManagedFilename = "bor_managed.json"

// SyncEdgeFromProto merges multiple EdgePolicy protos and syncs the result to
// each Edge managed-policy directory. It uses protojson to convert each proto
// to Edge-compatible JSON (respecting json_name options), deep-merges them,
// then writes bor_managed.json to each directory.
// When policies is empty or all nil, bor_managed.json is removed from every dir.
func SyncEdgeFromProto(policies []*pb.EdgePolicy, dirPaths []string) error {
	merged := make(map[string]interface{})

	opts := protojson.MarshalOptions{EmitUnpopulated: false}
	for _, pol := range policies {
		if pol == nil {
			continue
		}
		jsonBytes, err := opts.Marshal(pol)
		if err != nil {
			return fmt.Errorf("failed to marshal Edge policy proto: %w", err)
		}
		var partial map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &partial); err != nil {
			return fmt.Errorf("failed to parse marshalled Edge policy: %w", err)
		}
		deepMerge(merged, partial)
	}

	if len(merged) == 0 {
		for _, dir := range dirPaths {
			if dir == "" {
				continue
			}
			_ = removeEdgeManaged(dir)
		}
		return nil
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged Edge policies: %w", err)
	}
	data = append(data, '\n')

	for _, dir := range dirPaths {
		if dir == "" {
			continue
		}
		if err := writeEdgeManaged(dir, data); err != nil {
			return err
		}
	}
	return nil
}

func writeEdgeManaged(dirPath string, data []byte) error {
	if err := os.MkdirAll(dirPath, 0o755); err != nil { //nolint:gosec // G301: Edge policy directories must be world-readable
		return fmt.Errorf("failed to create Edge policy directory %s: %w", dirPath, err)
	}
	target := filepath.Join(dirPath, EdgeManagedFilename)
	if err := WriteFileAtomically(target, data); err != nil {
		return fmt.Errorf("failed to write Edge policies to %s: %w", target, err)
	}
	return nil
}

func removeEdgeManaged(dirPath string) error {
	target := filepath.Join(dirPath, EdgeManagedFilename)
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove Edge managed file %s: %w", target, err)
	}
	return nil
}
