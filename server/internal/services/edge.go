// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// ValidateEdgeContent validates Microsoft Edge policy JSON content.
// Uses DiscardUnknown: true so that Edge policy names not yet in our proto
// are accepted (forward compatibility).
func ValidateEdgeContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("edge policy content is empty")
	}
	var pol pb.EdgePolicy
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid Edge policy: %w", err)
	}
	return nil
}
