// SPDX-License-Identifier: AGPL-3.0-only
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

	// Only principals of the configured realm may enroll; a ticket from a
	// trusted foreign realm is refused.
	if s.config.Realm != "" && !strings.EqualFold(creds.Realm(), s.config.Realm) {
		return "", fmt.Errorf("principal realm %q is not the configured realm %q", creds.Realm(), s.config.Realm)
	}

	// Build the full principal string: primary/instance@REALM
	// For machine accounts this is typically "host/hostname.example.com@REALM".
	principal := creds.CName().PrincipalNameString() + "@" + creds.Realm()
	return principal, nil
}

// KerberosNodeName returns the node name a Kerberos-enrolled agent gets,
// bound to the machine identity in its principal:
//
//   - host/<fqdn>@REALM (FreeIPA, MIT): the FQDN or its first label.
//   - <NAME>$@REALM (Active Directory, Samba AD machine accounts): NAME, or
//     an FQDN whose first label is NAME.
//
// requested is the name the agent asked for; empty selects the name from
// the principal. Comparison ignores case. Any other principal (users,
// service principals) cannot enroll a node.
func KerberosNodeName(principal, requested string) (string, error) {
	noRealm := principal
	if idx := strings.LastIndex(noRealm, "@"); idx >= 0 {
		noRealm = noRealm[:idx]
	}

	var fqdn, short string
	switch {
	case strings.HasPrefix(noRealm, "host/"):
		fqdn = strings.TrimSuffix(strings.TrimPrefix(noRealm, "host/"), ".")
		short, _, _ = strings.Cut(fqdn, ".")
	case strings.HasSuffix(noRealm, "$") && !strings.ContainsAny(noRealm, "/"):
		short = strings.TrimSuffix(noRealm, "$")
	default:
		return "", fmt.Errorf("principal %q is not a machine account (host/<fqdn> or <NAME>$)", principal)
	}
	if short == "" {
		return "", fmt.Errorf("principal %q names no host", principal)
	}

	if requested == "" {
		if fqdn != "" {
			return fqdn, nil
		}
		return strings.ToLower(short), nil
	}
	reqShort, _, _ := strings.Cut(requested, ".")
	switch {
	case fqdn != "" && strings.EqualFold(requested, fqdn):
	case strings.EqualFold(requested, short):
	case fqdn == "" && strings.EqualFold(reqShort, short):
	default:
		return "", fmt.Errorf("node name %q does not match principal %q", requested, principal)
	}
	return requested, nil
}

// RealmFromPrincipal returns the realm portion of a Kerberos principal string,
// e.g. "EXAMPLE.COM" from "host/worker01.example.com@EXAMPLE.COM".
func RealmFromPrincipal(principal string) string {
	if idx := strings.LastIndex(principal, "@"); idx >= 0 {
		return principal[idx+1:]
	}
	return ""
}
