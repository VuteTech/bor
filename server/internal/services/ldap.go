// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"crypto/tls"
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

// LDAPConfig holds LDAP connection configuration
type LDAPConfig struct {
	Enabled      bool   `json:"enabled"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	UseTLS       bool   `json:"use_tls"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"-"`
	BaseDN       string `json:"base_dn"`
	UserFilter   string `json:"user_filter"`
	AttrUsername  string `json:"attr_username"`
	AttrEmail    string `json:"attr_email"`
	AttrFullName string `json:"attr_full_name"`
}

// LDAPUser represents user information retrieved from LDAP
type LDAPUser struct {
	Username string
	Email    string
	FullName string
}

// LDAPService handles LDAP authentication
type LDAPService struct {
	config LDAPConfig
}

// NewLDAPService creates a new LDAPService
func NewLDAPService(config LDAPConfig) *LDAPService {
	return &LDAPService{config: config}
}

// IsEnabled returns whether LDAP is enabled
func (s *LDAPService) IsEnabled() bool {
	return s.config.Enabled
}

// Authenticate verifies user credentials against LDAP
func (s *LDAPService) Authenticate(username, password string) (*LDAPUser, error) {
	if !s.config.Enabled {
		return nil, fmt.Errorf("LDAP is not enabled")
	}

	conn, err := s.connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}
	defer conn.Close()

	// Bind with service account to search for the user
	if err := conn.Bind(s.config.BindDN, s.config.BindPassword); err != nil {
		return nil, fmt.Errorf("failed to bind with service account: %w", err)
	}

	// Search for the user
	filter := fmt.Sprintf(s.config.UserFilter, ldap.EscapeFilter(username))
	searchRequest := ldap.NewSearchRequest(
		s.config.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		filter,
		[]string{s.config.AttrUsername, s.config.AttrEmail, s.config.AttrFullName},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("user not found in LDAP")
	}

	entry := result.Entries[0]

	// Bind as the user to verify password
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("invalid LDAP credentials")
	}

	return &LDAPUser{
		Username: entry.GetAttributeValue(s.config.AttrUsername),
		Email:    entry.GetAttributeValue(s.config.AttrEmail),
		FullName: entry.GetAttributeValue(s.config.AttrFullName),
	}, nil
}

// connect establishes a connection to the LDAP server
func (s *LDAPService) connect() (*ldap.Conn, error) {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	if s.config.UseTLS {
		return ldap.DialTLS("tcp", addr, &tls.Config{
			MinVersion: tls.VersionTLS12,
		})
	}

	return ldap.DialURL(fmt.Sprintf("ldap://%s", addr))
}
