// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// LDAPConfig holds LDAP connection configuration.
// The same struct mirrors config.LDAPConfig; it is kept here so that
// the services package does not import the config package directly.
type LDAPConfig struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	UseTLS        bool   `json:"use_tls"`
	StartTLS      bool   `json:"start_tls"`
	TLSCAFile     string `json:"tls_ca_file"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
	BindDN        string `json:"bind_dn"`
	BindPassword  string `json:"-"`
	BaseDN        string `json:"base_dn"`
	// UserFilter is the LDAP search filter; use %s for the escaped username.
	// FreeIPA: "(uid=%s)"
	// Active Directory: "(sAMAccountName=%s)"
	UserFilter string `json:"user_filter"`
	// UPNSuffix enables AD-style UPN binds: username + "@EXAMPLE.COM".
	// When set, authentication falls back to a direct UPN bind if the service
	// account search finds no matching entry.
	UPNSuffix    string `json:"upn_suffix"`
	AttrUsername string `json:"attr_username"`
	AttrEmail    string `json:"attr_email"`
	AttrFullName string `json:"attr_full_name"`
	// GroupBaseDN is the base for group searches (defaults to BaseDN).
	GroupBaseDN string `json:"group_base_dn"`
	// GroupFilter finds the groups a user DN belongs to.
	// Example: "(&(objectClass=group)(member=%s))"
	GroupFilter string `json:"group_filter"`
	// GroupMemberAttr is the attribute on a group object that lists members.
	// Typically "member" (AD/FreeIPA) or "uniqueMember" (OpenLDAP).
	GroupMemberAttr string `json:"group_member_attr"`
	// AttrMemberOf is the user attribute listing group DNs.
	// Typically "memberOf".  When set, group lookups use this attribute
	// instead of a separate group search.
	AttrMemberOf string `json:"attr_member_of"`
	// PageSize controls LDAP result paging (RFC 2696).  0 disables paging.
	PageSize int `json:"page_size"`
	// GroupRoleMap maps LDAP group CNs to Bor role names.
	// Users are automatically granted / revoked these roles on every login
	// based on their current LDAP group membership.
	// Example: {"Domain Admins": "Super Admin", "IT Staff": "Org Admin"}
	GroupRoleMap map[string]string `json:"group_role_map"`
}

// LDAPUser represents user information retrieved from LDAP.
type LDAPUser struct {
	Username string
	Email    string
	FullName string
	// Groups contains the CNs of the groups the user is a member of.
	Groups []string
}

// LDAPService handles LDAP authentication.
type LDAPService struct {
	config *LDAPConfig
}

// NewLDAPService creates a new LDAPService.
func NewLDAPService(config *LDAPConfig) *LDAPService {
	return &LDAPService{config: config}
}

// IsEnabled returns whether LDAP is enabled.
func (s *LDAPService) IsEnabled() bool {
	return s.config.Enabled
}

// Authenticate verifies user credentials against LDAP and returns the user's
// attributes and group memberships on success.
//
// Flow (compatible with both FreeIPA and Active Directory / Samba AD):
//  1. Bind with the service account (BindDN / BindPassword).
//  2. Search for the user entry using UserFilter.
//  3. If not found and UPNSuffix is set, attempt a direct UPN bind.
//  4. Re-bind as the user to verify the supplied password.
//  5. Retrieve group memberships via AttrMemberOf or a separate search.
func (s *LDAPService) Authenticate(username, password string) (*LDAPUser, error) {
	if !s.config.Enabled {
		return nil, fmt.Errorf("LDAP is not enabled")
	}

	conn, err := s.connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// ── Step 1: service account bind ─────────────────────────────────────────
	if bindErr := conn.Bind(s.config.BindDN, s.config.BindPassword); bindErr != nil {
		return nil, fmt.Errorf("failed to bind with service account: %w", bindErr)
	}

	// ── Step 2: search for the user ──────────────────────────────────────────
	searchAttrs := []string{
		"dn",
		s.config.AttrUsername,
		s.config.AttrEmail,
		s.config.AttrFullName,
	}
	if s.config.AttrMemberOf != "" {
		searchAttrs = append(searchAttrs, s.config.AttrMemberOf)
	}

	filter := fmt.Sprintf(s.config.UserFilter, ldap.EscapeFilter(username))

	var entry *ldap.Entry
	entry, err = s.searchUser(conn, filter, searchAttrs)
	if err != nil {
		return nil, err
	}

	// ── Step 3: UPN fallback bind (AD-style) ─────────────────────────────────
	// When the service account search finds no entry but a UPN suffix is
	// configured, try binding directly as username@SUFFIX.  AD allows this even
	// when anonymous/bind-account search fails due to restrictive ACLs.
	if entry == nil && s.config.UPNSuffix != "" {
		upn := username + s.config.UPNSuffix
		if upnErr := conn.Bind(upn, password); upnErr != nil {
			return nil, fmt.Errorf("user not found and UPN bind failed: invalid credentials")
		}
		// Re-search now that we're bound as the user.
		entry, err = s.searchUser(conn, filter, searchAttrs)
		if err != nil || entry == nil {
			return nil, fmt.Errorf("user not found in LDAP")
		}
		// Password already verified via the UPN bind above.
		return s.buildUser(conn, entry, username), nil
	}

	if entry == nil {
		return nil, fmt.Errorf("user not found in LDAP")
	}

	// ── Step 4: user bind to verify password ─────────────────────────────────
	if userBindErr := conn.Bind(entry.DN, password); userBindErr != nil {
		return nil, fmt.Errorf("invalid LDAP credentials")
	}

	// ── Step 5: build user with group info ───────────────────────────────────
	return s.buildUser(conn, entry, username), nil
}

// searchUser runs a paged (or non-paged) LDAP search and returns the first
// matching entry, or nil when no entries match.
func (s *LDAPService) searchUser(conn *ldap.Conn, filter string, attrs []string) (*ldap.Entry, error) {
	base := s.config.BaseDN

	if s.config.PageSize > 0 {
		paging := ldap.NewControlPaging(uint32(s.config.PageSize)) //nolint:gosec // page size is configured by admin
		req := ldap.NewSearchRequest(
			base,
			ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
			filter, attrs, []ldap.Control{paging},
		)
		result, err := conn.SearchWithPaging(req, uint32(s.config.PageSize)) //nolint:gosec // page size is admin-configured
		if err != nil {
			return nil, fmt.Errorf("LDAP search failed: %w", err)
		}
		if len(result.Entries) == 0 {
			return nil, nil
		}
		return result.Entries[0], nil
	}

	req := ldap.NewSearchRequest(
		base,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		filter, attrs, nil,
	)
	result, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}
	if len(result.Entries) == 0 {
		return nil, nil
	}
	return result.Entries[0], nil
}

// buildUser constructs an LDAPUser from the search entry and resolves groups.
func (s *LDAPService) buildUser(conn *ldap.Conn, entry *ldap.Entry, fallbackUsername string) *LDAPUser {
	username := entry.GetAttributeValue(s.config.AttrUsername)
	if username == "" {
		username = fallbackUsername
	}

	user := &LDAPUser{
		Username: username,
		Email:    entry.GetAttributeValue(s.config.AttrEmail),
		FullName: entry.GetAttributeValue(s.config.AttrFullName),
	}

	// ── Group resolution ─────────────────────────────────────────────────────
	// Prefer the direct memberOf attribute when configured (one round-trip).
	if s.config.AttrMemberOf != "" {
		for _, dn := range entry.GetAttributeValues(s.config.AttrMemberOf) {
			user.Groups = append(user.Groups, cnFromDN(dn))
		}
		return user
	}

	// Fall back to a separate group search using GroupFilter.
	if s.config.GroupFilter != "" {
		groupBase := s.config.GroupBaseDN
		if groupBase == "" {
			groupBase = s.config.BaseDN
		}
		groupFilter := fmt.Sprintf(s.config.GroupFilter, ldap.EscapeFilter(entry.DN))
		req := ldap.NewSearchRequest(
			groupBase,
			ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
			groupFilter, []string{"cn"}, nil,
		)
		result, err := conn.Search(req)
		if err == nil {
			for _, g := range result.Entries {
				if cn := g.GetAttributeValue("cn"); cn != "" {
					user.Groups = append(user.Groups, cn)
				}
			}
		}
	}

	return user
}

// cnFromDN extracts the CN value from a Distinguished Name string.
// e.g. "CN=Domain Admins,DC=example,DC=com" → "Domain Admins"
func cnFromDN(dn string) string {
	for _, part := range strings.Split(dn, ",") {
		part = strings.TrimSpace(part)
		if upper := strings.ToUpper(part); strings.HasPrefix(upper, "CN=") {
			return part[3:]
		}
	}
	return dn
}

// connect establishes a connection to the LDAP server, applying TLS settings.
func (s *LDAPService) connect() (*ldap.Conn, error) {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	tlsCfg, err := s.tlsConfig()
	if err != nil {
		return nil, err
	}

	// LDAPS: full TLS from connection start.
	if s.config.UseTLS {
		return ldap.DialURL(fmt.Sprintf("ldaps://%s", addr),
			ldap.DialWithTLSConfig(tlsCfg))
	}

	conn, err := ldap.DialURL(fmt.Sprintf("ldap://%s", addr))
	if err != nil {
		return nil, err
	}

	// StartTLS: upgrade a plain connection to TLS.
	if s.config.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("StartTLS failed: %w", err)
		}
	}

	return conn, nil
}

// tlsConfig builds a *tls.Config, optionally loading a custom CA certificate.
//
// Samba AD (and some older LDAP servers) generate certificates that use only
// the Common Name field for the hostname instead of Subject Alternative Names.
// Go's default TLS verification rejects these.  When a custom CA file is
// provided we therefore verify the certificate chain ourselves (ensuring the
// cert is signed by the trusted CA) while allowing CN-based hostname matching
// via VerifyHostname on the parsed certificate.
func (s *LDAPService) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: s.config.Host,
	}

	if s.config.TLSSkipVerify {
		cfg.InsecureSkipVerify = true //nolint:gosec // G402: admin-configured, dev/testing use
		return cfg, nil
	}

	if s.config.TLSCAFile != "" {
		pemData, err := os.ReadFile(s.config.TLSCAFile) //nolint:gosec // G304: path is admin-configured
		if err != nil {
			return nil, fmt.Errorf("failed to read LDAP CA cert %s: %w", s.config.TLSCAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("no valid certificates found in %s", s.config.TLSCAFile)
		}
		cfg.RootCAs = pool

		// Custom verification: check the chain against our CA pool but
		// tolerate certificates that lack SANs (common with Samba AD).
		// Session tickets are disabled so that VerifyPeerCertificate is
		// always invoked — resumed sessions would otherwise skip it (G123).
		cfg.InsecureSkipVerify = true //nolint:gosec // G402: we verify manually below
		cfg.SessionTicketsDisabled = true
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("server presented no TLS certificates")
			}
			cert, parseErr := x509.ParseCertificate(rawCerts[0])
			if parseErr != nil {
				return fmt.Errorf("failed to parse server certificate: %w", parseErr)
			}

			// Build the intermediate chain (if any).
			intermediates := x509.NewCertPool()
			for _, raw := range rawCerts[1:] {
				if ic, icErr := x509.ParseCertificate(raw); icErr == nil {
					intermediates.AddCert(ic)
				}
			}

			// Verify the chain against our trusted CA pool.
			_, verifyErr := cert.Verify(x509.VerifyOptions{
				Roots:         pool,
				Intermediates: intermediates,
				// Skip hostname check here; we do it below with the
				// more lenient VerifyHostname that accepts CN.
			})
			if verifyErr != nil {
				return fmt.Errorf("certificate chain verification failed: %w", verifyErr)
			}

			// Try standard SAN-based hostname check first.
			// If it fails and the cert has no SANs, fall back to
			// matching the Common Name (legacy Samba AD certs).
			if hostErr := cert.VerifyHostname(s.config.Host); hostErr != nil {
				if len(cert.DNSNames) == 0 && len(cert.IPAddresses) == 0 {
					if !strings.EqualFold(cert.Subject.CommonName, s.config.Host) {
						return fmt.Errorf("certificate CN %q does not match host %q", cert.Subject.CommonName, s.config.Host)
					}
				} else {
					return fmt.Errorf("hostname verification failed: %w", hostErr)
				}
			}
			return nil
		}
	}

	return cfg, nil
}
