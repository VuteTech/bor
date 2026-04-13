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
	"google.golang.org/protobuf/proto"
)

// FirefoxManagedComment is the comment written into policies.json when the
// file is under Bor management. Firefox ignores unknown root-level keys,
// so this is safe to include and serves as a human-visible marker.
const FirefoxManagedComment = "This file is managed by Bor. Do not edit manually. Changes will be overwritten by policy enforcement."

// deepMerge merges src into dst recursively. For map values, it recurses.
// For slice values in src, they are appended to existing slices in dst.
// For all other values, src overwrites dst.
func deepMerge(dst, src map[string]interface{}) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal
			continue
		}

		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstMap, dstIsMap := dstVal.(map[string]interface{})
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
			continue
		}

		srcSlice, srcIsSlice := srcVal.([]interface{})
		dstSlice, dstIsSlice := dstVal.([]interface{})
		if srcIsSlice && dstIsSlice {
			dst[key] = append(dstSlice, srcSlice...)
			continue
		}

		dst[key] = srcVal
	}
}

// MergeFirefoxProtos merges multiple FirefoxPolicy proto messages into one.
// Uses proto.Merge semantics: singular optional fields from later policies
// overwrite earlier ones; repeated fields are appended.
func MergeFirefoxProtos(policies []*pb.FirefoxPolicy) *pb.FirefoxPolicy {
	merged := &pb.FirefoxPolicy{}
	for _, p := range policies {
		if p != nil {
			proto.Merge(merged, p)
		}
	}
	return merged
}

// SyncFirefoxPoliciesFromProto merges the given proto policies and writes
// policies.json to targetPath. When policies is empty, restores the original.
func SyncFirefoxPoliciesFromProto(targetPath string, policies []*pb.FirefoxPolicy) error {
	if len(policies) == 0 {
		return RestoreOriginal(targetPath)
	}
	if err := BackupOriginal(targetPath); err != nil {
		return fmt.Errorf("failed to backup Firefox policies: %w", err)
	}
	data, err := marshalFirefoxPolicies(policies)
	if err != nil {
		return err
	}
	return WriteFileAtomically(targetPath, data)
}

// SyncFirefoxFlatpakPoliciesFromProto writes merged Firefox policies to the
// Flatpak extension directory. No backup/restore — Bor owns this file.
func SyncFirefoxFlatpakPoliciesFromProto(targetPath string, policies []*pb.FirefoxPolicy) error {
	if len(policies) == 0 {
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove Flatpak Firefox policies: %w", err)
		}
		return nil
	}
	data, err := marshalFirefoxPolicies(policies)
	if err != nil {
		return err
	}
	return WriteFileAtomically(targetPath, data)
}

// marshalFirefoxPolicies merges the given policies and marshals them into
// the policies.json format Firefox expects: {"_comment": "...", "policies": {...}}.
func marshalFirefoxPolicies(policies []*pb.FirefoxPolicy) ([]byte, error) {
	merged := MergeFirefoxProtos(policies)

	opts := protojson.MarshalOptions{EmitUnpopulated: false}
	jsonBytes, err := opts.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Firefox policy proto: %w", err)
	}

	var policiesMap map[string]interface{}
	if unmarshalErr := json.Unmarshal(jsonBytes, &policiesMap); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse marshalled Firefox policy: %w", unmarshalErr)
	}

	managed := struct {
		Comment  string                 `json:"_comment"`
		Policies map[string]interface{} `json:"policies"`
	}{
		Comment:  FirefoxManagedComment,
		Policies: policiesMap,
	}

	data, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Firefox policies: %w", err)
	}
	return append(data, '\n'), nil
}

// WriteFileAtomically writes data to a temporary file and then renames it
// to the target path for an atomic update. Parent directories are created
// if they do not exist. The file is written with mode 0644.
func WriteFileAtomically(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: policy directories must be world-readable
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".bor-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Chmod(tmpName, 0o644); err != nil { //nolint:gosec // G302: policy files must be world-readable
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file to %s: %w", targetPath, err)
	}

	return nil
}
