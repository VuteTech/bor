// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package config provides server configuration loading.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)


// Config holds application configuration.
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Security SecurityConfig
	LDAP     LDAPConfig
	TLS      TLSConfig
	CA       CAConfig
	WebAuthn WebAuthnConfig
}

// WebAuthnConfig holds WebAuthn (FIDO2) relying party configuration.
type WebAuthnConfig struct {
	RPID        string   // BOR_WEBAUTHN_RPID        — relying party ID (domain, e.g. "bor.example.com")
	RPOrigins   []string // BOR_WEBAUTHN_ORIGINS      — comma-separated allowed origins
	DisplayName string   // BOR_WEBAUTHN_DISPLAY_NAME — human-readable RP name
}

// DatabaseConfig holds database configuration.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// ServerConfig holds server configuration.
type ServerConfig struct {
	Address        string   // BOR_ADDRESS – server hostname or IP (no port; default "")
	EnrollmentPort int      // BOR_ENROLLMENT_PORT – UI + enrollment gRPC listen port (default 8443)
	PolicyPort     int      // BOR_POLICY_PORT – mTLS agent policy gRPC listen port (default 8444)
	Hostnames      []string // BOR_HOSTNAMES – additional SANs for the auto-generated TLS cert
}

// EnrollmentAddr returns the host:port for the UI + enrollment server.
func (s ServerConfig) EnrollmentAddr() string {
	return fmt.Sprintf("%s:%d", s.Address, s.EnrollmentPort)
}

// PolicyAddr returns the host:port for the mTLS agent policy server.
func (s ServerConfig) PolicyAddr() string {
	return fmt.Sprintf("%s:%d", s.Address, s.PolicyPort)
}

// SecurityConfig holds security configuration.
type SecurityConfig struct {
	JWTSecret   string
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	AdminToken  string // BOR_ADMIN_TOKEN – static admin token for gRPC enrollment calls
}

// TLSConfig holds UI HTTPS TLS configuration.
type TLSConfig struct {
	CertFile   string // BOR_TLS_CERT_FILE – path to TLS certificate
	KeyFile    string // BOR_TLS_KEY_FILE  – path to TLS private key
	AutogenDir string // BOR_TLS_AUTOGEN_DIR – dir for auto-generated self-signed cert
}

// PKCS11Config holds PKCS#11 HSM configuration for the CA private key.
// When set, the CA private key is accessed via the HSM rather than a file on disk.
// The binary must be compiled with -tags pkcs11 to activate HSM support.
type PKCS11Config struct {
	Lib        string // BOR_CA_PKCS11_LIB        – path to PKCS#11 shared library (.so)
	TokenLabel string // BOR_CA_PKCS11_TOKEN_LABEL – label of the HSM token
	KeyLabel   string // BOR_CA_PKCS11_KEY_LABEL   – label of the CA private key object on the token
	PIN        string // BOR_CA_PKCS11_PIN         – token PIN (prefer env var; never commit to YAML)
}

// IsConfigured returns true when the minimum required PKCS#11 fields are all non-empty.
func (p PKCS11Config) IsConfigured() bool {
	return p.Lib != "" && p.TokenLabel != "" && p.KeyLabel != ""
}

// CAConfig holds internal CA configuration for issuing agent certs (mTLS).
type CAConfig struct {
	CertFile   string       // BOR_CA_CERT_FILE   – path to CA certificate
	KeyFile    string       // BOR_CA_KEY_FILE    – path to CA private key (unused when PKCS11 is set)
	AutogenDir string       // BOR_CA_AUTOGEN_DIR – dir for auto-generated CA
	PKCS11     PKCS11Config // optional: load CA key from PKCS#11 HSM instead of a file
}

// LDAPConfig holds LDAP connection configuration.
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

// fileConfig mirrors Config for YAML unmarshalling.
// Field names use lowercase_underscore YAML keys.
type fileConfig struct {
	Server struct {
		Address        string   `yaml:"address"`
		EnrollmentPort int      `yaml:"enrollment_port"`
		PolicyPort     int      `yaml:"policy_port"`
		Hostnames      []string `yaml:"hostnames"`
	} `yaml:"server"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Name     string `yaml:"name"`
		SSLMode  string `yaml:"sslmode"`
	} `yaml:"database"`
	Security struct {
		JWTSecret  string `yaml:"jwt_secret"`
		AdminToken string `yaml:"admin_token"`
	} `yaml:"security"`
	TLS struct {
		CertFile   string `yaml:"cert_file"`
		KeyFile    string `yaml:"key_file"`
		AutogenDir string `yaml:"autogen_dir"`
	} `yaml:"tls"`
	CA struct {
		CertFile   string `yaml:"cert_file"`
		KeyFile    string `yaml:"key_file"`
		AutogenDir string `yaml:"autogen_dir"`
		PKCS11     struct {
			Lib        string `yaml:"lib"`
			TokenLabel string `yaml:"token_label"`
			KeyLabel   string `yaml:"key_label"`
			PIN        string `yaml:"pin"` // prefer BOR_CA_PKCS11_PIN env var; avoid storing PIN in YAML
		} `yaml:"pkcs11"`
	} `yaml:"ca"`
	LDAP struct {
		Enabled      bool   `yaml:"enabled"`
		Host         string `yaml:"host"`
		Port         int    `yaml:"port"`
		UseTLS       bool   `yaml:"use_tls"`
		BindDN       string `yaml:"bind_dn"`
		BindPassword string `yaml:"bind_password"`
		BaseDN       string `yaml:"base_dn"`
		UserFilter   string `yaml:"user_filter"`
		AttrUsername string `yaml:"attr_username"`
		AttrEmail    string `yaml:"attr_email"`
		AttrFullName string `yaml:"attr_full_name"`
	} `yaml:"ldap"`
	WebAuthn struct {
		RPID        string   `yaml:"rpid"`
		Origins     []string `yaml:"origins"`
		DisplayName string   `yaml:"display_name"`
	} `yaml:"webauthn"`
}

// Load loads configuration from a YAML file (optional) and environment
// variables. Environment variables take precedence over the YAML file.
// The YAML file path is taken from BOR_CONFIG, defaulting to
// /etc/bor/server.yaml. A missing file is silently ignored.
func Load() (*Config, error) {
	fc := defaultFileConfig()

	cfgPath := getEnv("BOR_CONFIG", "/etc/bor/server.yaml")
	if data, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // config file path is admin-controlled
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return nil, fmt.Errorf("failed to parse config file %s: %w", cfgPath, err)
		}
	}

	// ─── Database ──────────────────────────────────────────────────────────
	dbPortStr := getEnv("DB_PORT", strconv.Itoa(fc.Database.Port))
	dbPort, err := strconv.Atoi(dbPortStr)
	if err != nil {
		return nil, fmt.Errorf("invalid DB_PORT: %w", err)
	}

	// ─── Server ports ──────────────────────────────────────────────────────
	enrollPortStr := getEnv("BOR_ENROLLMENT_PORT", strconv.Itoa(fc.Server.EnrollmentPort))
	enrollPort, err := strconv.Atoi(enrollPortStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BOR_ENROLLMENT_PORT: %w", err)
	}
	policyPortStr := getEnv("BOR_POLICY_PORT", strconv.Itoa(fc.Server.PolicyPort))
	policyPort, err := strconv.Atoi(policyPortStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BOR_POLICY_PORT: %w", err)
	}

	// ─── LDAP ──────────────────────────────────────────────────────────────
	ldapEnabled := getEnvBool("LDAP_ENABLED", fc.LDAP.Enabled)
	ldapPortStr := getEnv("LDAP_PORT", strconv.Itoa(fc.LDAP.Port))
	ldapPort, err := strconv.Atoi(ldapPortStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LDAP_PORT: %w", err)
	}
	ldapUseTLS := getEnvBool("LDAP_USE_TLS", fc.LDAP.UseTLS)

	// ─── TLS ───────────────────────────────────────────────────────────────
	tlsCertFile := getEnv("BOR_TLS_CERT_FILE", fc.TLS.CertFile)
	tlsKeyFile := getEnv("BOR_TLS_KEY_FILE", fc.TLS.KeyFile)
	if (tlsCertFile != "" && tlsKeyFile == "") || (tlsCertFile == "" && tlsKeyFile != "") {
		return nil, fmt.Errorf("both BOR_TLS_CERT_FILE and BOR_TLS_KEY_FILE must be set, or neither")
	}

	// ─── CA ────────────────────────────────────────────────────────────────
	caCertFile := getEnv("BOR_CA_CERT_FILE", fc.CA.CertFile)
	caKeyFile := getEnv("BOR_CA_KEY_FILE", fc.CA.KeyFile)
	if (caCertFile != "" && caKeyFile == "") || (caCertFile == "" && caKeyFile != "") {
		return nil, fmt.Errorf("both BOR_CA_CERT_FILE and BOR_CA_KEY_FILE must be set, or neither")
	}

	// ─── CA PKCS#11 (optional HSM) ─────────────────────────────────────────
	pkcs11Lib := getEnv("BOR_CA_PKCS11_LIB", fc.CA.PKCS11.Lib)
	pkcs11TokenLabel := getEnv("BOR_CA_PKCS11_TOKEN_LABEL", fc.CA.PKCS11.TokenLabel)
	pkcs11KeyLabel := getEnv("BOR_CA_PKCS11_KEY_LABEL", fc.CA.PKCS11.KeyLabel)
	pkcs11PIN := getEnv("BOR_CA_PKCS11_PIN", fc.CA.PKCS11.PIN)

	// ─── Hostnames ─────────────────────────────────────────────────────────
	// BOR_HOSTNAMES env var accepts a comma-separated list and overrides the
	// YAML hostnames list entirely when set.
	hostnames := fc.Server.Hostnames
	if envHostnames := os.Getenv("BOR_HOSTNAMES"); envHostnames != "" {
		hostnames = splitComma(envHostnames)
	}

	// ─── WebAuthn ──────────────────────────────────────────────────────────
	webAuthnRPID := getEnv("BOR_WEBAUTHN_RPID", fc.WebAuthn.RPID)
	webAuthnOrigins := fc.WebAuthn.Origins
	if envOrigins := os.Getenv("BOR_WEBAUTHN_ORIGINS"); envOrigins != "" {
		webAuthnOrigins = splitComma(envOrigins)
	}
	webAuthnDisplayName := getEnv("BOR_WEBAUTHN_DISPLAY_NAME", fc.WebAuthn.DisplayName)
	if webAuthnDisplayName == "" {
		webAuthnDisplayName = "Bor Policy Manager"
	}

	return &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", fc.Database.Host),
			Port:     dbPort,
			User:     getEnv("DB_USER", fc.Database.User),
			Password: getEnv("DB_PASSWORD", fc.Database.Password),
			Database: getEnv("DB_NAME", fc.Database.Name),
			SSLMode:  getEnv("DB_SSLMODE", fc.Database.SSLMode),
		},
		Server: ServerConfig{
			Address:        getEnv("BOR_ADDRESS", fc.Server.Address),
			EnrollmentPort: enrollPort,
			PolicyPort:     policyPort,
			Hostnames:      hostnames,
		},
		Security: SecurityConfig{
			JWTSecret:   getEnv("JWT_SECRET", fc.Security.JWTSecret),
			TLSEnabled:  getEnvBool("TLS_ENABLED", false),
			TLSCertFile: getEnv("TLS_CERT_FILE", ""),
			TLSKeyFile:  getEnv("TLS_KEY_FILE", ""),
			AdminToken:  getEnv("BOR_ADMIN_TOKEN", fc.Security.AdminToken),
		},
		TLS: TLSConfig{
			CertFile:   tlsCertFile,
			KeyFile:    tlsKeyFile,
			AutogenDir: getEnv("BOR_TLS_AUTOGEN_DIR", fc.TLS.AutogenDir),
		},
		CA: CAConfig{
			CertFile:   caCertFile,
			KeyFile:    caKeyFile,
			AutogenDir: getEnv("BOR_CA_AUTOGEN_DIR", fc.CA.AutogenDir),
			PKCS11: PKCS11Config{
				Lib:        pkcs11Lib,
				TokenLabel: pkcs11TokenLabel,
				KeyLabel:   pkcs11KeyLabel,
				PIN:        pkcs11PIN,
			},
		},
		LDAP: LDAPConfig{
			Enabled:      ldapEnabled,
			Host:         getEnv("LDAP_HOST", fc.LDAP.Host),
			Port:         ldapPort,
			UseTLS:       ldapUseTLS,
			BindDN:       getEnv("LDAP_BIND_DN", fc.LDAP.BindDN),
			BindPassword: getEnv("LDAP_BIND_PASSWORD", fc.LDAP.BindPassword),
			BaseDN:       getEnv("LDAP_BASE_DN", fc.LDAP.BaseDN),
			UserFilter:   getEnv("LDAP_USER_FILTER", fc.LDAP.UserFilter),
			AttrUsername: getEnv("LDAP_ATTR_USERNAME", fc.LDAP.AttrUsername),
			AttrEmail:    getEnv("LDAP_ATTR_EMAIL", fc.LDAP.AttrEmail),
			AttrFullName: getEnv("LDAP_ATTR_FULL_NAME", fc.LDAP.AttrFullName),
		},
		WebAuthn: WebAuthnConfig{
			RPID:        webAuthnRPID,
			RPOrigins:   webAuthnOrigins,
			DisplayName: webAuthnDisplayName,
		},
	}, nil
}

// defaultFileConfig returns a fileConfig pre-populated with built-in defaults.
func defaultFileConfig() fileConfig {
	var fc fileConfig
	fc.Server.Address = ""
	fc.Server.EnrollmentPort = 8443
	fc.Server.PolicyPort = 8444
	fc.Database.Host = "localhost"
	fc.Database.Port = 5432
	fc.Database.User = "bor"
	fc.Database.Password = "bor"
	fc.Database.Name = "bor"
	fc.Database.SSLMode = "disable"
	fc.Security.JWTSecret = "change-me-in-production"
	fc.TLS.AutogenDir = "/var/lib/bor/pki/ui"
	fc.CA.AutogenDir = "/var/lib/bor/pki/ca"
	fc.LDAP.Host = "localhost"
	fc.LDAP.Port = 389
	fc.LDAP.UserFilter = "(uid=%s)"
	fc.LDAP.AttrUsername = "uid"
	fc.LDAP.AttrEmail = "mail"
	fc.LDAP.AttrFullName = "cn"
	return fc
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	val := os.Getenv(key)
	switch val {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultValue
	}
}

// splitComma splits a comma-separated string and trims whitespace from each token.
func splitComma(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}
