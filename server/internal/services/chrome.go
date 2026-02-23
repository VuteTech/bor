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

// ValidateChromeContent validates Chrome policy JSON content.
// Uses DiscardUnknown: true so that Chrome policy names not yet in our proto
// are accepted (forward compatibility).
func ValidateChromeContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("chrome policy content is empty")
	}
	var pol pb.ChromePolicy
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid Chrome policy: %w", err)
	}
	return nil
}
