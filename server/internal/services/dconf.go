// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// ValidateDConfPolicy validates a DConf policy content JSON string.
func ValidateDConfPolicy(content string) error {
	if content == "" || content == "{}" {
		return fmt.Errorf("DConf policy content is empty")
	}

	var dp pb.DConfPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &dp); err != nil {
		return fmt.Errorf("invalid DConf policy JSON: %w", err)
	}

	if len(dp.Entries) == 0 {
		return fmt.Errorf("DConf policy must contain at least one entry")
	}

	for i, e := range dp.Entries {
		if e.GetSchemaId() == "" {
			return fmt.Errorf("entry[%d]: schema_id is required", i)
		}
		if e.GetKey() == "" {
			return fmt.Errorf("entry[%d]: key is required", i)
		}
		if e.GetValue() == "" {
			return fmt.Errorf("entry[%d]: value is required", i)
		}
	}

	return nil
}

// ParseDConfPolicyContent parses and validates a DConf policy content string.
func ParseDConfPolicyContent(content string) (*pb.DConfPolicy, error) {
	if err := ValidateDConfPolicy(content); err != nil {
		return nil, err
	}

	var dp pb.DConfPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &dp); err != nil {
		return nil, fmt.Errorf("invalid DConf policy JSON: %w", err)
	}

	return &dp, nil
}
