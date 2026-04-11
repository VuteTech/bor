// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// DiscoverPolkitActions runs `pkaction --verbose` and parses its output into
// a slice of PolkitActionDescription proto messages.
//
// Example pkaction --verbose output (one block per action, separated by blank lines):
//
//	org.freedesktop.NetworkManager.network.enable:
//	  description:       Enable or disable a network
//	  message:           System policy prevents enabling or disabling networks
//	  vendor:            NetworkManager
//	  vendor_url:        http://networkmanager.freedesktop.org/
//	  icon:
//	  implicit any:      auth_admin
//	  implicit inactive: auth_admin
//	  implicit active:   auth_admin
func DiscoverPolkitActions() ([]*pb.PolkitActionDescription, error) {
	out, err := exec.Command("pkaction", "--verbose").Output()
	if err != nil {
		return nil, fmt.Errorf("polkit: pkaction --verbose: %w", err)
	}
	return parsePolkitActions(out), nil
}

// ParsePolkitActionsForTest exports parsePolkitActions for white-box testing.
func ParsePolkitActionsForTest(data []byte) []*pb.PolkitActionDescription {
	return parsePolkitActions(data)
}

// parsePolkitActions parses the text output of `pkaction --verbose`.
func parsePolkitActions(data []byte) []*pb.PolkitActionDescription {
	var actions []*pb.PolkitActionDescription

	// Split on blank lines to get per-action blocks.
	blocks := bytes.Split(data, []byte("\n\n"))
	for _, block := range blocks {
		block = bytes.TrimSpace(block)
		if len(block) == 0 {
			continue
		}
		lines := strings.Split(string(block), "\n")
		if len(lines) == 0 {
			continue
		}

		// First line: "<action_id>:"
		firstLine := strings.TrimSpace(lines[0])
		actionID := strings.TrimSuffix(firstLine, ":")
		if actionID == "" || strings.Contains(actionID, " ") {
			continue
		}

		action := &pb.PolkitActionDescription{ActionId: actionID}

		for _, line := range lines[1:] {
			// Each line: "  key:  value"
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])

			switch key {
			case "description":
				action.Description = val
			case "message":
				action.Message = val
			case "vendor":
				action.Vendor = val
			case "implicit any":
				action.DefaultAny = val
			case "implicit inactive":
				action.DefaultInactive = val
			case "implicit active":
				action.DefaultActive = val
			}
		}

		actions = append(actions, action)
	}

	return actions
}
