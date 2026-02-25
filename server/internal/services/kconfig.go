// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"regexp"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// allowedKConfigFiles is the set of KDE config files that may be managed.
var allowedKConfigFiles = map[string]bool{
	"kdeglobals":      true,
	"kwinrc":          true,
	"plasmarc":        true,
	"kscreenlockerrc": true,
	"dolphinrc":       true,
	"konsolerc":       true,
	"plasma-org.kde.plasma.desktop-appletsrc": true,
}

// allowedAppletsrcGroupRe restricts which INI groups may be set inside
// the plasma-org.kde.plasma.desktop-appletsrc file. Only containment-level
// keys (e.g. wallpaperplugin) and wallpaper-plugin-level keys are permitted.
var allowedAppletsrcGroupRe = regexp.MustCompile(
	`^Containments\]\[\d+(\]\[Wallpaper\]\[.+\]\[.+)?$`,
)

// validKConfigTypes is the set of allowed value types.
var validKConfigTypes = map[string]bool{
	"bool":   true,
	"string": true,
	"int":    true,
}

// ValidateKConfigPolicy validates a KConfig policy content JSON string.
func ValidateKConfigPolicy(content string) error {
	if content == "" || content == "{}" {
		return fmt.Errorf("KConfig policy content is empty")
	}

	var kcp pb.KConfigPolicy
	if err := protojson.Unmarshal([]byte(content), &kcp); err != nil {
		return fmt.Errorf("invalid KConfig policy JSON: %w", err)
	}

	if len(kcp.Entries) == 0 {
		return fmt.Errorf("KConfig policy must have at least one entry")
	}

	for i, e := range kcp.Entries {
		if e.File == "" {
			return fmt.Errorf("entry %d: file is required", i)
		}
		if e.Group == "" {
			return fmt.Errorf("entry %d: group is required", i)
		}
		if e.Key == "" {
			return fmt.Errorf("entry %d: key is required", i)
		}

		// Reject path traversal attempts.
		if strings.Contains(e.File, "/") || strings.Contains(e.File, "\\") || strings.Contains(e.File, "..") {
			return fmt.Errorf("entry %d: file %q contains path separator or traversal", i, e.File)
		}

		if !allowedKConfigFiles[e.File] {
			return fmt.Errorf("entry %d: file %q is not in the allowed set", i, e.File)
		}

		// Restrict groups for plasma-org.kde.plasma.desktop-appletsrc to
		// containment-level and wallpaper-plugin-level groups only.
		if e.File == "plasma-org.kde.plasma.desktop-appletsrc" {
			if !allowedAppletsrcGroupRe.MatchString(e.Group) {
				return fmt.Errorf("entry %d: group %q is not allowed for %s", i, e.Group, e.File)
			}
		}

		if e.Type != "" && !validKConfigTypes[e.Type] {
			return fmt.Errorf("entry %d: type %q is not valid (must be bool, string, or int)", i, e.Type)
		}

		// Validate KDE URL Restriction rules (rule_N entries).
		if e.Group == "KDE URL Restrictions" && strings.HasPrefix(e.Key, "rule_") && e.Key != "rule_count" {
			fields := strings.Split(e.Value, ",")
			if len(fields) != 8 {
				return fmt.Errorf("entry %d: URL restriction %s must have exactly 8 comma-separated fields, got %d", i, e.Key, len(fields))
			}
			action := fields[0]
			if action != "open" && action != "list" && action != "redirect" {
				return fmt.Errorf("entry %d: URL restriction %s has invalid action %q (must be open, list, or redirect)", i, e.Key, action)
			}
			enabled := fields[7]
			if enabled != "true" && enabled != "false" {
				return fmt.Errorf("entry %d: URL restriction %s has invalid enabled value %q (must be true or false)", i, e.Key, enabled)
			}
		}
	}

	return nil
}

// ParseKConfigPolicyContent parses and validates a KConfig policy content string.
func ParseKConfigPolicyContent(content string) (*pb.KConfigPolicy, error) {
	if err := ValidateKConfigPolicy(content); err != nil {
		return nil, err
	}

	var kcp pb.KConfigPolicy
	if err := protojson.Unmarshal([]byte(content), &kcp); err != nil {
		return nil, fmt.Errorf("invalid KConfig policy JSON: %w", err)
	}

	return &kcp, nil
}
