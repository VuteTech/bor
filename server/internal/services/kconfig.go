// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// ValidateKConfigPolicy validates a KConfig policy content JSON string.
func ValidateKConfigPolicy(content string) error {
	if content == "" || content == "{}" {
		return fmt.Errorf("KConfig policy content is empty")
	}

	var kcp pb.KConfigPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &kcp); err != nil {
		return fmt.Errorf("invalid KConfig policy JSON: %w", err)
	}

	// At least one setting must be configured.
	if !kconfigPolicyHasSettings(&kcp) {
		return fmt.Errorf("KConfig policy must configure at least one setting")
	}

	// Validate URL restriction action values.
	for i, r := range kcp.UrlRestrictions {
		switch r.GetAction() {
		case "open", "list", "redirect":
			// valid
		default:
			return fmt.Errorf("url_restrictions[%d]: invalid action %q (must be open, list, or redirect)", i, r.GetAction())
		}
	}

	return nil
}

// kconfigPolicyHasSettings reports whether the policy has at least one
// setting configured (any non-nil optional field or non-empty repeated field).
func kconfigPolicyHasSettings(kcp *pb.KConfigPolicy) bool {
	return kcp.ShellAccess != nil ||
		kcp.RunCommand != nil ||
		kcp.ActionLogout != nil ||
		kcp.ActionFileNew != nil ||
		kcp.ActionFileOpen != nil ||
		kcp.ActionFileSave != nil ||
		kcp.RestrictWallpaper != nil ||
		kcp.RestrictIcons != nil ||
		kcp.RestrictAutostart != nil ||
		kcp.RestrictColors != nil ||
		kcp.RestrictCursors != nil ||
		kcp.BorderlessMaximizedWindows != nil ||
		kcp.PlasmoidUnlockedDesktop != nil ||
		kcp.AllowConfigureWhenLocked != nil ||
		kcp.AutoLock != nil ||
		kcp.LockOnResume != nil ||
		kcp.LockTimeout != nil ||
		kcp.IconTheme != nil ||
		kcp.WallpaperPlugin != nil ||
		kcp.WallpaperImage != nil ||
		kcp.WallpaperFillMode != nil ||
		kcp.WallpaperColor != nil ||
		len(kcp.UrlRestrictions) > 0 ||
		len(kcp.KcmRestrictions) > 0
}

// ParseKConfigPolicyContent parses and validates a KConfig policy content string.
func ParseKConfigPolicyContent(content string) (*pb.KConfigPolicy, error) {
	if err := ValidateKConfigPolicy(content); err != nil {
		return nil, err
	}

	var kcp pb.KConfigPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &kcp); err != nil {
		return nil, fmt.Errorf("invalid KConfig policy JSON: %w", err)
	}

	return &kcp, nil
}
