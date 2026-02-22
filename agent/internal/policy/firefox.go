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
	"google.golang.org/protobuf/proto"
)

// FirefoxManagedComment is the comment written into policies.json when the
// file is under Bor management. Firefox ignores unknown root-level keys,
// so this is safe to include and serves as a human-visible marker.
const FirefoxManagedComment = "This file is managed by Bor. Do not edit manually. Changes will be overwritten by policy enforcement."

// FirefoxPoliciesJSON is the top-level wrapper for policies.json.
type FirefoxPoliciesJSON struct {
	Policies map[string]interface{} `json:"policies"`
}

// MergeFirefoxPolicies takes a list of policy content JSON strings (each
// representing a partial Firefox policy), deep-merges them into one combined
// policies.json structure, and returns the merged JSON bytes.
//
// This is the core requirement: multiple policies delegated from the server
// are parsed and merged into a single policies.json file on the workstation.
func MergeFirefoxPolicies(contents []string) ([]byte, error) {
	merged := make(map[string]interface{})

	for _, content := range contents {
		if content == "" || content == "{}" {
			continue
		}
		var partial map[string]interface{}
		if err := json.Unmarshal([]byte(content), &partial); err != nil {
			return nil, fmt.Errorf("failed to parse Firefox policy content: %w", err)
		}
		deepMerge(merged, partial)
	}

	wrapper := FirefoxPoliciesJSON{Policies: merged}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged Firefox policies: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

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

// SyncFirefoxPolicies writes a merged policies.json from the given policy
// content strings. Before the first write it backs up the original
// policies.json (creating an empty sentinel if no original exists).
// When contents is empty, the original file is restored from its backup.
func SyncFirefoxPolicies(targetPath string, contents []string) error {
	if len(contents) == 0 {
		// No active Firefox policies — restore original.
		return RestoreOriginal(targetPath)
	}

	// Backup original before first write (idempotent).
	if err := BackupOriginal(targetPath); err != nil {
		return fmt.Errorf("failed to backup Firefox policies: %w", err)
	}

	// Merge all policy contents, then re-marshal with the managed-by comment
	// as the first key so humans opening the file see it immediately.
	merged := make(map[string]interface{})
	for _, content := range contents {
		if content == "" || content == "{}" {
			continue
		}
		var partial map[string]interface{}
		if err := json.Unmarshal([]byte(content), &partial); err != nil {
			return fmt.Errorf("failed to parse Firefox policy content: %w", err)
		}
		deepMerge(merged, partial)
	}

	managed := struct {
		Comment  string                 `json:"_comment"`
		Policies map[string]interface{} `json:"policies"`
	}{
		Comment:  FirefoxManagedComment,
		Policies: merged,
	}

	data, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Firefox policies: %w", err)
	}
	data = append(data, '\n')

	if err := WriteFileAtomically(targetPath, data); err != nil {
		return fmt.Errorf("failed to write Firefox policies: %w", err)
	}

	return nil
}

// SyncFirefoxFlatpakPolicies writes a merged policies.json to the Flatpak
// Firefox extension directory so that the sandboxed browser can read it.
// Unlike SyncFirefoxPolicies, there is no backup/restore: the extension
// directory is fully managed by Bor, so the file is simply removed when
// no policies are active.
func SyncFirefoxFlatpakPolicies(targetPath string, contents []string) error {
	if len(contents) == 0 {
		// No active Firefox policies — remove the managed file if present.
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove Flatpak Firefox policies: %w", err)
		}
		return nil
	}

	merged := make(map[string]interface{})
	for _, content := range contents {
		if content == "" || content == "{}" {
			continue
		}
		var partial map[string]interface{}
		if err := json.Unmarshal([]byte(content), &partial); err != nil {
			return fmt.Errorf("failed to parse Firefox policy content: %w", err)
		}
		deepMerge(merged, partial)
	}

	managed := struct {
		Comment  string                 `json:"_comment"`
		Policies map[string]interface{} `json:"policies"`
	}{
		Comment:  FirefoxManagedComment,
		Policies: merged,
	}

	data, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Flatpak Firefox policies: %w", err)
	}
	data = append(data, '\n')

	if err := WriteFileAtomically(targetPath, data); err != nil {
		return fmt.Errorf("failed to write Flatpak Firefox policies: %w", err)
	}
	return nil
}

// WriteFileAtomically writes data to a temporary file and then renames it
// to the target path for an atomic update. Parent directories are created
// if they do not exist. The file is written with mode 0644.
func WriteFileAtomically(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".bor-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file to %s: %w", targetPath, err)
	}

	return nil
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
	merged := MergeFirefoxProtos(policies)
	managed := struct {
		Comment  string            `json:"_comment"`
		Policies *pb.FirefoxPolicy `json:"policies"`
	}{
		Comment:  FirefoxManagedComment,
		Policies: merged,
	}
	data, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Firefox policies: %w", err)
	}
	data = append(data, '\n')
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
	merged := MergeFirefoxProtos(policies)
	managed := struct {
		Comment  string            `json:"_comment"`
		Policies *pb.FirefoxPolicy `json:"policies"`
	}{
		Comment:  FirefoxManagedComment,
		Policies: merged,
	}
	data, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Flatpak Firefox policies: %w", err)
	}
	data = append(data, '\n')
	return WriteFileAtomically(targetPath, data)
}
