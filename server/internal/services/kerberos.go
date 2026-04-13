// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"strings"

	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/service"
	"github.com/jcmturner/gokrb5/v8/spnego"
)

// KerberosConfig holds the server-side Kerberos configuration.
// It mirrors config.KerberosConfig to avoid an import cycle.
type KerberosConfig struct {
	Enabled            bool
	Realm              string
	KeytabFile         string
	ServicePrincipal   string
	DefaultNodeGroupID string
}

// KerberosService validates Kerberos SPNEGO tokens presented by domain-joined
// agents during the token-free KerberosEnroll flow.
//
// Supported KDCs: MIT Kerberos 5 (FreeIPA), Microsoft AD (Windows / Samba AD).
type KerberosService struct {
	config KerberosConfig
	kt     *keytab.Keytab
}

// NewKerberosService loads the service keytab and returns a KerberosService.
// Returns an error if the keytab cannot be read.
func NewKerberosService(cfg KerberosConfig) (*KerberosService, error) {
	kt, err := keytab.Load(cfg.KeytabFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load Kerberos keytab %s: %w", cfg.KeytabFile, err)
	}
	return &KerberosService{config: cfg, kt: kt}, nil
}

// IsEnabled reports whether Kerberos enrollment is configured and active.
func (s *KerberosService) IsEnabled() bool {
	return s.config.Enabled && s.config.KeytabFile != "" && s.config.ServicePrincipal != ""
}

// DefaultNodeGroupID returns the configured default node group for auto-enrolled
// Kerberos agents.
func (s *KerberosService) DefaultNodeGroupID() string {
	return s.config.DefaultNodeGroupID
}

// ValidateToken parses and validates a raw DER-encoded SPNEGO NegTokenInit
// wrapping a Kerberos AP_REQ.  On success it returns the authenticated
// client principal name (e.g. "host/worker01.example.com@EXAMPLE.COM").
//
// The caller is responsible for checking that the returned principal is
// authorised to enroll as an agent (e.g. it matches the expected realm).
func (s *KerberosService) ValidateToken(tokenBytes []byte) (string, error) {
	// ── Parse SPNEGO wrapper ─────────────────────────────────────────────────
	var spnegoTok spnego.SPNEGOToken
	if err := spnegoTok.Unmarshal(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to parse SPNEGO token: %w", err)
	}

	if !spnegoTok.Init {
		return "", fmt.Errorf("expected SPNEGO NegTokenInit, got NegTokenResp")
	}

	// ── Extract the Kerberos AP_REQ from the mechToken ───────────────────────
	// MechTokenBytes contains a KRB5 token (OID header + AP_REQ); KRB5Token
	// handles stripping the header during Unmarshal.
	mechTokenBytes := spnegoTok.NegTokenInit.MechTokenBytes
	if len(mechTokenBytes) == 0 {
		return "", fmt.Errorf("SPNEGO token contains no mechToken (no AP_REQ)")
	}

	var krb5Tok spnego.KRB5Token
	if err := krb5Tok.Unmarshal(mechTokenBytes); err != nil {
		return "", fmt.Errorf("failed to unmarshal KRB5 token: %w", err)
	}

	// ── Validate the AP_REQ against our service keytab ───────────────────────
	settings := service.NewSettings(s.kt)
	ok, creds, err := service.VerifyAPREQ(&krb5Tok.APReq, settings)
	if err != nil {
		return "", fmt.Errorf("AP_REQ verification failed: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("kerberos AP_REQ rejected")
	}

	// Build the full principal string: primary/instance@REALM
	// For machine accounts this is typically "host/hostname.example.com@REALM".
	principal := creds.CName().PrincipalNameString() + "@" + creds.Realm()
	return principal, nil
}

// RealmFromPrincipal returns the realm portion of a Kerberos principal string,
// e.g. "EXAMPLE.COM" from "host/worker01.example.com@EXAMPLE.COM".
func RealmFromPrincipal(principal string) string {
	if idx := strings.LastIndex(principal, "@"); idx >= 0 {
		return principal[idx+1:]
	}
	return ""
}

// PrincipalToHostname returns the host component from a host-keytab principal,
// e.g. "worker01.example.com" from "host/worker01.example.com@EXAMPLE.COM".
// Returns the full primary/instance string for non-host principals.
func PrincipalToHostname(principal string) string {
	// Strip realm
	noRealm := principal
	if idx := strings.LastIndex(principal, "@"); idx >= 0 {
		noRealm = principal[:idx]
	}
	// Return instance part for host/... principals
	parts := strings.SplitN(noRealm, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return noRealm
}
