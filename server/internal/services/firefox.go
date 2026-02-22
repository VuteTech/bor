// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

var validSSLVersions = map[string]bool{
	"tls1":   true,
	"tls1.1": true,
	"tls1.2": true,
	"tls1.3": true,
}

var validStartPages = map[string]bool{
	"none":             true,
	"homepage":         true,
	"previous-session": true,
	"homepage-locked":  true,
}

var validProxyModes = map[string]bool{
	"none":       true,
	"system":     true,
	"manual":     true,
	"autoDetect": true,
	"autoConfig": true,
}

// ValidateFirefoxContent parses content JSON into pb.FirefoxPolicy and validates it.
func ValidateFirefoxContent(content string) error {
	if content == "" || content == "{}" {
		return nil
	}
	var fp pb.FirefoxPolicy
	if err := json.Unmarshal([]byte(content), &fp); err != nil {
		return fmt.Errorf("invalid firefox policy JSON: %w", err)
	}
	return validateFirefoxProto(&fp)
}

func validateFirefoxProto(p *pb.FirefoxPolicy) error {
	if p.SSLVersionMin != nil && !validSSLVersions[*p.SSLVersionMin] {
		return fmt.Errorf("invalid SSLVersionMin: %q", *p.SSLVersionMin)
	}
	if p.SSLVersionMax != nil && !validSSLVersions[*p.SSLVersionMax] {
		return fmt.Errorf("invalid SSLVersionMax: %q", *p.SSLVersionMax)
	}

	if p.Homepage != nil {
		if p.Homepage.StartPage != "" && !validStartPages[p.Homepage.StartPage] {
			return fmt.Errorf("invalid Homepage.StartPage: %q", p.Homepage.StartPage)
		}
		if p.Homepage.URL != "" {
			if err := validateSafeURL(p.Homepage.URL); err != nil {
				return fmt.Errorf("invalid Homepage.URL: %w", err)
			}
		}
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

// validateSafeURL checks that a URL uses http or https scheme.
func validateSafeURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	return nil
}
