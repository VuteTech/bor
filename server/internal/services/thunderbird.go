// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// ValidateThunderbirdContent parses content JSON into pb.ThunderbirdPolicy and validates it.
func ValidateThunderbirdContent(content string) error {
	if content == "" || content == "{}" {
		return nil
	}
	var tp pb.ThunderbirdPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &tp); err != nil {
		return fmt.Errorf("invalid thunderbird policy JSON: %w", err)
	}
	return validateThunderbirdProto(&tp)
}

func validateThunderbirdProto(p *pb.ThunderbirdPolicy) error {
	if p.SSLVersionMin != nil && !validSSLVersions[*p.SSLVersionMin] {
		return fmt.Errorf("invalid SSLVersionMin: %q", *p.SSLVersionMin)
	}
	if p.SSLVersionMax != nil && !validSSLVersions[*p.SSLVersionMax] {
		return fmt.Errorf("invalid SSLVersionMax: %q", *p.SSLVersionMax)
	}

	if p.Proxy != nil && p.Proxy.Mode != "" {
		if !validProxyModes[p.Proxy.Mode] {
			return fmt.Errorf("invalid Proxy.Mode: %q", p.Proxy.Mode)
		}
	}

	if p.DNSOverHTTPS != nil && p.DNSOverHTTPS.ProviderURL != "" {
		if err := validateSafeURL(p.DNSOverHTTPS.ProviderURL); err != nil {
			return fmt.Errorf("invalid DNSOverHTTPS.ProviderURL: %w", err)
		}
	}

	return nil
}
