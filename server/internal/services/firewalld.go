// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// firewalldZoneNameRE matches a valid firewalld zone name (letters, digits,
// underscore, hyphen). firewalld additionally limits the length to 17 chars.
var firewalldZoneNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// firewalldTargets is the set of valid zone targets. "default" (or empty) means
// firewalld's built-in default behaviour for the zone.
var firewalldTargets = map[string]struct{}{
	"default": {}, "ACCEPT": {}, "REJECT": {}, "DROP": {},
}

// firewalldProtocols is the set of valid transport protocols for ports.
var firewalldProtocols = map[string]struct{}{
	"tcp": {}, "udp": {}, "sctp": {}, "dccp": {},
}

// ValidateFirewalldContent validates firewalld zone policy JSON content.
func ValidateFirewalldContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("firewalld policy content is empty")
	}
	var pol pb.FirewalldPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid firewalld policy: %w", err)
	}

	if z := pol.GetZone(); z != "" {
		if len(z) > 17 || !firewalldZoneNameRE.MatchString(z) {
			return fmt.Errorf("invalid zone name %q: must match [A-Za-z0-9_-] and be at most 17 characters", z)
		}
	}

	if t := pol.GetTarget(); t != "" {
		if _, ok := firewalldTargets[t]; !ok {
			return fmt.Errorf("invalid target %q: must be one of default, ACCEPT, REJECT, DROP", t)
		}
	}

	for _, p := range pol.GetPorts() {
		if err := validateFirewalldPort(p.GetPort(), p.GetProtocol()); err != nil {
			return fmt.Errorf("invalid port: %w", err)
		}
	}
	for _, p := range pol.GetSourcePorts() {
		if err := validateFirewalldPort(p.GetPort(), p.GetProtocol()); err != nil {
			return fmt.Errorf("invalid source port: %w", err)
		}
	}
	for _, f := range pol.GetForwardPorts() {
		if err := validateFirewalldPort(f.GetPort(), f.GetProtocol()); err != nil {
			return fmt.Errorf("invalid forward port: %w", err)
		}
		if tp := f.GetToPort(); tp != "" {
			if err := validatePortSpec(tp); err != nil {
				return fmt.Errorf("invalid forward to_port: %w", err)
			}
		}
		if ta := f.GetToAddr(); ta != "" {
			if net.ParseIP(ta) == nil {
				return fmt.Errorf("invalid forward to_addr %q: not an IP address", ta)
			}
		}
	}

	for _, r := range pol.GetRichRules() {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("rich rule must not be empty")
		}
		if !strings.HasPrefix(strings.TrimSpace(r), "rule") {
			return fmt.Errorf("invalid rich rule %q: must begin with \"rule\"", r)
		}
	}

	return nil
}

// validateFirewalldPort validates a port spec and its protocol.
func validateFirewalldPort(port, protocol string) error {
	if err := validatePortSpec(port); err != nil {
		return err
	}
	if _, ok := firewalldProtocols[protocol]; !ok {
		return fmt.Errorf("protocol %q must be one of tcp, udp, sctp, dccp", protocol)
	}
	return nil
}

// validatePortSpec validates a single port "N" or a range "N-M" (1..65535).
func validatePortSpec(spec string) error {
	if spec == "" {
		return fmt.Errorf("port is required")
	}
	lo, hi, found := strings.Cut(spec, "-")
	loN, err := parsePort(lo)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	hiN, err := parsePort(hi)
	if err != nil {
		return err
	}
	if hiN < loN {
		return fmt.Errorf("port range %q is inverted", spec)
	}
	return nil
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range 1-65535", n)
	}
	return n, nil
}
