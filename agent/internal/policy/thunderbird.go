// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"fmt"
	"os"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ThunderbirdManagedComment is written into policies.json when the file is
// under Bor management. Thunderbird ignores unknown root-level keys, so this
// is safe to include and serves as a human-visible marker.
const ThunderbirdManagedComment = "This file is managed by Bor. Do not edit manually. Changes will be overwritten by policy enforcement."

// MergeThunderbirdProtos merges multiple ThunderbirdPolicy proto messages into one.
// Uses proto.Merge semantics: singular optional fields from later policies
// overwrite earlier ones; repeated fields are appended.
func MergeThunderbirdProtos(policies []*pb.ThunderbirdPolicy) *pb.ThunderbirdPolicy {
	merged := &pb.ThunderbirdPolicy{}
	for _, p := range policies {
		if p != nil {
			proto.Merge(merged, p)
		}
	}
	return merged
}

// SyncThunderbirdPoliciesFromProto merges the given proto policies and writes
// policies.json to targetPath. When policies is empty, restores the original.
func SyncThunderbirdPoliciesFromProto(targetPath string, policies []*pb.ThunderbirdPolicy) error {
	if len(policies) == 0 {
		return RestoreOriginal(targetPath)
	}
	if err := BackupOriginal(targetPath); err != nil {
		return fmt.Errorf("failed to backup Thunderbird policies: %w", err)
	}
	data, err := marshalThunderbirdPolicies(policies)
	if err != nil {
		return err
	}
	return WriteFileAtomically(targetPath, data)
}

// SyncThunderbirdFlatpakPoliciesFromProto writes merged Thunderbird policies
// to the Flatpak extension directory. No backup/restore — Bor owns this file.
func SyncThunderbirdFlatpakPoliciesFromProto(targetPath string, policies []*pb.ThunderbirdPolicy) error {
	if len(policies) == 0 {
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove Flatpak Thunderbird policies: %w", err)
		}
		return nil
	}
	data, err := marshalThunderbirdPolicies(policies)
	if err != nil {
		return err
	}
	return WriteFileAtomically(targetPath, data)
}

// marshalThunderbirdPolicies merges the given policies and marshals them into
// the policies.json format Thunderbird expects: {"_comment": "...", "policies": {...}}.
func marshalThunderbirdPolicies(policies []*pb.ThunderbirdPolicy) ([]byte, error) {
	merged := MergeThunderbirdProtos(policies)

	opts := protojson.MarshalOptions{EmitUnpopulated: false}
	jsonBytes, err := opts.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Thunderbird policy proto: %w", err)
	}

	var policiesMap map[string]interface{}
	if unmarshalErr := json.Unmarshal(jsonBytes, &policiesMap); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse marshalled Thunderbird policy: %w", unmarshalErr)
	}

	managed := struct {
		Comment  string                 `json:"_comment"`
		Policies map[string]interface{} `json:"policies"`
	}{
		Comment:  ThunderbirdManagedComment,
		Policies: policiesMap,
	}

	data, err := json.MarshalIndent(managed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Thunderbird policies: %w", err)
	}
	return append(data, '\n'), nil
}
