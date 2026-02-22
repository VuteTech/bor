// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds application configuration
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Security SecurityConfig
	LDAP     LDAPConfig
	TLS      TLSConfig
	CA       CAConfig
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Addr string // BOR_ADDR – HTTPS listen address (default ":8443")
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	JWTSecret   string
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	AdminToken  string // BOR_ADMIN_TOKEN – static admin token for gRPC enrollment calls
}

// TLSConfig holds UI HTTPS TLS configuration
type TLSConfig struct {
	CertFile   string // BOR_TLS_CERT_FILE – path to TLS certificate
	KeyFile    string // BOR_TLS_KEY_FILE  – path to TLS private key
	AutogenDir string // BOR_TLS_AUTOGEN_DIR – dir for auto-generated self-signed cert
}

// CAConfig holds internal CA configuration for issuing agent certs (mTLS)
type CAConfig struct {
	CertFile   string // BOR_CA_CERT_FILE – path to CA certificate
	KeyFile    string // BOR_CA_KEY_FILE  – path to CA private key
	AutogenDir string // BOR_CA_AUTOGEN_DIR – dir for auto-generated CA
}

// LDAPConfig holds LDAP connection configuration
type LDAPConfig struct {
	Enabled      bool
	Host         string
	Port         int
	UseTLS       bool
	BindDN       string
	BindPassword string
	BaseDN       string
	UserFilter   string
	AttrUsername string
	AttrEmail    string
	AttrFullName string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	dbPort, err := strconv.Atoi(getEnv("DB_PORT", "5432"))
	if err != nil {
		return nil, fmt.Errorf("invalid DB_PORT: %w", err)
	}

	tlsEnabled := getEnv("TLS_ENABLED", "false") == "true"

	ldapEnabled := getEnv("LDAP_ENABLED", "false") == "true"
	ldapPort, err := strconv.Atoi(getEnv("LDAP_PORT", "389"))
	if err != nil {
		return nil, fmt.Errorf("invalid LDAP_PORT: %w", err)
	}
	ldapUseTLS := getEnv("LDAP_USE_TLS", "false") == "true"

	// UI TLS configuration – fail fast if only one of cert/key is provided
	tlsCertFile := getEnv("BOR_TLS_CERT_FILE", "")
	tlsKeyFile := getEnv("BOR_TLS_KEY_FILE", "")
	if (tlsCertFile != "" && tlsKeyFile == "") || (tlsCertFile == "" && tlsKeyFile != "") {
		return nil, fmt.Errorf("both BOR_TLS_CERT_FILE and BOR_TLS_KEY_FILE must be set, or neither")
	}

	// CA configuration – fail fast if only one of cert/key is provided
	caCertFile := getEnv("BOR_CA_CERT_FILE", "")
	caKeyFile := getEnv("BOR_CA_KEY_FILE", "")
	if (caCertFile != "" && caKeyFile == "") || (caCertFile == "" && caKeyFile != "") {
		return nil, fmt.Errorf("both BOR_CA_CERT_FILE and BOR_CA_KEY_FILE must be set, or neither")
	}

	return &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     dbPort,
			User:     getEnv("DB_USER", "bor"),
			Password: getEnv("DB_PASSWORD", "bor"),
			Database: getEnv("DB_NAME", "bor"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Server: ServerConfig{
			Addr: getEnv("BOR_ADDR", ":8443"),
		},
		Security: SecurityConfig{
			JWTSecret:   getEnv("JWT_SECRET", "change-me-in-production"),
			TLSEnabled:  tlsEnabled,
			TLSCertFile: getEnv("TLS_CERT_FILE", ""),
			TLSKeyFile:  getEnv("TLS_KEY_FILE", ""),
			AdminToken:  getEnv("BOR_ADMIN_TOKEN", ""),
		},
		TLS: TLSConfig{
			CertFile:   tlsCertFile,
			KeyFile:    tlsKeyFile,
			AutogenDir: getEnv("BOR_TLS_AUTOGEN_DIR", "/var/lib/bor/pki/ui"),
		},
		CA: CAConfig{
			CertFile:   caCertFile,
			KeyFile:    caKeyFile,
			AutogenDir: getEnv("BOR_CA_AUTOGEN_DIR", "/var/lib/bor/pki/ca"),
		},
		LDAP: LDAPConfig{
			Enabled:      ldapEnabled,
			Host:         getEnv("LDAP_HOST", "localhost"),
			Port:         ldapPort,
			UseTLS:       ldapUseTLS,
			BindDN:       getEnv("LDAP_BIND_DN", ""),
			BindPassword: getEnv("LDAP_BIND_PASSWORD", ""),
			BaseDN:       getEnv("LDAP_BASE_DN", ""),
			UserFilter:   getEnv("LDAP_USER_FILTER", "(uid=%s)"),
			AttrUsername: getEnv("LDAP_ATTR_USERNAME", "uid"),
			AttrEmail:    getEnv("LDAP_ATTR_EMAIL", "mail"),
			AttrFullName: getEnv("LDAP_ATTR_FULL_NAME", "cn"),
		},
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
