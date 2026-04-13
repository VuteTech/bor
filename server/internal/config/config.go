// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package config provides server configuration loading.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration.
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Security SecurityConfig
	LDAP     LDAPConfig
	Kerberos KerberosConfig
	TLS      TLSConfig
	CA       CAConfig
	WebAuthn WebAuthnConfig
	Metrics  MetricsConfig
	Audit    AuditConfig
	UI       UIConfig
}

// AuditConfig holds configuration for audit event forwarding.
type AuditConfig struct {
	Syslog        SyslogConfig
	RetentionDays int  // BOR_AUDIT_RETENTION_DAYS – purge audit logs older than N days (default 365; 0 disables)
	AnonymizeIPs  bool // BOR_AUDIT_ANONYMIZE_IPS – truncate IPs to /24 (v4) or /48 (v6) before storing
}

// SyslogConfig holds configuration for the syslog audit sink.
type SyslogConfig struct {
	Enabled   bool   // BOR_AUDIT_SYSLOG_ENABLED
	Network   string // BOR_AUDIT_SYSLOG_NETWORK  "udp" | "tcp" | "tcp+tls" (default: "udp")
	Addr      string // BOR_AUDIT_SYSLOG_ADDR      host:port (default: "localhost:514")
	Format    string // BOR_AUDIT_SYSLOG_FORMAT    "cef" | "ocsf" (default: "cef")
	Facility  int    // BOR_AUDIT_SYSLOG_FACILITY  0-23 (default: 16 = local0)
	TLSCAFile string // BOR_AUDIT_SYSLOG_TLS_CA    path to PEM CA cert for tcp+tls
}

// MetricsConfig holds Prometheus metrics endpoint configuration.
type MetricsConfig struct {
	// ListenAddr is the host:port for the Prometheus /metrics endpoint.
	// Defaults to "127.0.0.1:9090". Set BOR_METRICS_ADDR (or metrics.listen_addr
	// in server.yaml) to expose on a management network interface instead, e.g.
	// "192.168.1.10:9090". Leave the host part empty ("":9090) to bind all interfaces.
	// The endpoint serves plain HTTP — protect it with a firewall when not on localhost.
	ListenAddr string // BOR_METRICS_ADDR, default "127.0.0.1:9090"

	// BearerToken, when non-empty, requires every scrape request to carry
	// "Authorization: Bearer <token>". Leave unset for unauthenticated access
	// (appropriate when the listener is bound to localhost only).
	BearerToken string // BOR_METRICS_TOKEN, optional

	// TLSCertFile and TLSKeyFile, when both set, enable HTTPS on the metrics
	// endpoint. Useful when the scraper is on a different host.
	TLSCertFile string // BOR_METRICS_TLS_CERT_FILE, optional
	TLSKeyFile  string // BOR_METRICS_TLS_KEY_FILE, optional
}

// UIConfig holds configuration for the web frontend.
type UIConfig struct {
	// PrivacyPolicyURL, when set, shows a privacy policy link in the sidebar footer.
	// Complies with GDPR Article 13 transparency requirements.
	PrivacyPolicyURL string // BOR_PRIVACY_POLICY_URL, optional
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
	JWTSecret       string
	JWTLifetime     time.Duration // BOR_JWT_LIFETIME, default 1h
	RefreshLifetime time.Duration // BOR_REFRESH_LIFETIME, default 24h
	TLSEnabled      bool
	TLSCertFile     string
	TLSKeyFile      string
	AdminToken      string // BOR_ADMIN_TOKEN – static admin token for gRPC enrollment calls
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
	Enabled bool
	Host    string
	Port    int
	// UseTLS enables LDAPS (TLS from the start, default port 636).
	UseTLS bool
	// StartTLS upgrades a plain LDAP connection (port 389) to TLS.
	// Mutually exclusive with UseTLS; UseTLS takes precedence when both are set.
	StartTLS bool
	// TLSCAFile is an optional path to a PEM CA certificate used to verify
	// the LDAP server certificate for LDAPS or StartTLS connections.
	// When empty the system cert pool is used.
	TLSCAFile string
	// TLSSkipVerify disables all TLS certificate verification for the LDAP
	// connection.  Intended for development / testing only.
	TLSSkipVerify bool
	BindDN        string
	BindPassword  string
	BaseDN        string
	// UserFilter is the LDAP search filter for locating users.
	// Use %s as a placeholder for the (escaped) username.
	// FreeIPA default: "(uid=%s)"
	// Active Directory default: "(sAMAccountName=%s)"
	UserFilter string
	// UPNSuffix, when set, attempts an additional bind as username+UPNSuffix
	// (e.g. "@EXAMPLE.COM") when the primary bind with a service account fails
	// to locate the user.  This is the UPN-style bind used by Active Directory.
	UPNSuffix    string
	AttrUsername string
	AttrEmail    string
	AttrFullName string
	// GroupBaseDN is the base DN for group membership searches.
	// Defaults to BaseDN when empty.
	GroupBaseDN string
	// GroupFilter is the LDAP filter used to retrieve the groups a user belongs
	// to.  Use %s as a placeholder for the (escaped) user DN.
	// Example (AD/FreeIPA): "(&(objectClass=group)(member=%s))"
	GroupFilter string
	// GroupMemberAttr is the attribute on a group object that lists its member
	// DNs.  Typically "member" (AD, FreeIPA) or "uniqueMember" (OpenLDAP).
	GroupMemberAttr string
	// AttrMemberOf is the user attribute that lists the group DNs the user
	// belongs to.  Typically "memberOf" (AD, FreeIPA).  When set, group
	// membership is resolved via this attribute instead of a separate search.
	AttrMemberOf string
	// PageSize controls LDAP result paging (RFC 2696).  0 disables paging.
	PageSize int
	// GroupRoleMap maps LDAP group CNs to Bor role names.
	// Set via BOR_LDAP_GROUP_ROLE_MAP="Domain Admins=Super Admin,IT Staff=Org Admin"
	// or the ldap.group_role_map YAML map.
	GroupRoleMap map[string]string
}

// KerberosConfig holds Kerberos/GSSAPI configuration for token-free agent enrollment.
// Domain-joined nodes authenticate using the machine keytab (/etc/krb5.keytab)
// and a Kerberos service ticket, eliminating the need for manually generated
// enrollment tokens.  Supported by FreeIPA and Active Directory (Samba AD).
type KerberosConfig struct {
	// Enabled activates the KerberosEnroll gRPC endpoint.
	Enabled bool // BOR_KERBEROS_ENABLED
	// Realm is the Kerberos realm, e.g. "EXAMPLE.COM".
	Realm string // BOR_KERBEROS_REALM
	// KeytabFile is the path to the service keytab file on disk.
	// The keytab must contain a key for ServicePrincipal.
	KeytabFile string // BOR_KERBEROS_KEYTAB
	// ServicePrincipal is the full Kerberos principal for the Bor service,
	// e.g. "HTTP/bor.example.com@EXAMPLE.COM".
	ServicePrincipal string // BOR_KERBEROS_PRINCIPAL
	// DefaultNodeGroupID is the node group ID that newly-enrolled Kerberos
	// agents are placed into when no group can be inferred from the principal.
	DefaultNodeGroupID string // BOR_KERBEROS_DEFAULT_NODE_GROUP
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
		JWTSecret       string `yaml:"jwt_secret"`
		JWTLifetime     string `yaml:"jwt_lifetime"`
		RefreshLifetime string `yaml:"refresh_lifetime"`
		AdminToken      string `yaml:"admin_token"`
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
		Enabled         bool              `yaml:"enabled"`
		Host            string            `yaml:"host"`
		Port            int               `yaml:"port"`
		UseTLS          bool              `yaml:"use_tls"`
		StartTLS        bool              `yaml:"start_tls"`
		TLSCAFile       string            `yaml:"tls_ca_file"`
		TLSSkipVerify   bool              `yaml:"tls_skip_verify"`
		BindDN          string            `yaml:"bind_dn"`
		BindPassword    string            `yaml:"bind_password"`
		BaseDN          string            `yaml:"base_dn"`
		UserFilter      string            `yaml:"user_filter"`
		UPNSuffix       string            `yaml:"upn_suffix"`
		AttrUsername    string            `yaml:"attr_username"`
		AttrEmail       string            `yaml:"attr_email"`
		AttrFullName    string            `yaml:"attr_full_name"`
		GroupBaseDN     string            `yaml:"group_base_dn"`
		GroupFilter     string            `yaml:"group_filter"`
		GroupMemberAttr string            `yaml:"group_member_attr"`
		AttrMemberOf    string            `yaml:"attr_member_of"`
		PageSize        int               `yaml:"page_size"`
		GroupRoleMap    map[string]string `yaml:"group_role_map"`
	} `yaml:"ldap"`
	Kerberos struct {
		Enabled            bool   `yaml:"enabled"`
		Realm              string `yaml:"realm"`
		KeytabFile         string `yaml:"keytab_file"`
		ServicePrincipal   string `yaml:"service_principal"`
		DefaultNodeGroupID string `yaml:"default_node_group_id"`
	} `yaml:"kerberos"`
	WebAuthn struct {
		RPID        string   `yaml:"rpid"`
		Origins     []string `yaml:"origins"`
		DisplayName string   `yaml:"display_name"`
	} `yaml:"webauthn"`
	Metrics struct {
		ListenAddr  string `yaml:"listen_addr"`
		BearerToken string `yaml:"bearer_token"`
		TLSCertFile string `yaml:"tls_cert_file"`
		TLSKeyFile  string `yaml:"tls_key_file"`
	} `yaml:"metrics"`
	UI struct {
		PrivacyPolicyURL string `yaml:"privacy_policy_url"`
	} `yaml:"ui"`
	Audit struct {
		RetentionDays int `yaml:"retention_days"`
		Syslog        struct {
			Enabled   bool   `yaml:"enabled"`
			Network   string `yaml:"network"`
			Addr      string `yaml:"addr"`
			Format    string `yaml:"format"`
			Facility  int    `yaml:"facility"`
			TLSCAFile string `yaml:"tls_ca"`
		} `yaml:"syslog"`
	} `yaml:"audit"`
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
	ldapStartTLS := getEnvBool("LDAP_START_TLS", fc.LDAP.StartTLS)
	ldapTLSSkipVerify := getEnvBool("LDAP_TLS_SKIP_VERIFY", fc.LDAP.TLSSkipVerify)
	ldapPageSizeStr := getEnv("LDAP_PAGE_SIZE", strconv.Itoa(fc.LDAP.PageSize))
	ldapPageSize, err := strconv.Atoi(ldapPageSizeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LDAP_PAGE_SIZE: %w", err)
	}
	// BOR_LDAP_GROUP_ROLE_MAP overrides the YAML map entirely when set.
	ldapGroupRoleMap := fc.LDAP.GroupRoleMap
	if envMap := os.Getenv("BOR_LDAP_GROUP_ROLE_MAP"); envMap != "" {
		ldapGroupRoleMap = parseGroupRoleMap(envMap)
	}

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

	// ─── Metrics ───────────────────────────────────────────────────────────
	metricsAddr := getEnv("BOR_METRICS_ADDR", fc.Metrics.ListenAddr)
	metricsToken := getEnv("BOR_METRICS_TOKEN", fc.Metrics.BearerToken)
	metricsTLSCert := getEnv("BOR_METRICS_TLS_CERT_FILE", fc.Metrics.TLSCertFile)
	metricsTLSKey := getEnv("BOR_METRICS_TLS_KEY_FILE", fc.Metrics.TLSKeyFile)
	if (metricsTLSCert != "" && metricsTLSKey == "") || (metricsTLSCert == "" && metricsTLSKey != "") {
		return nil, fmt.Errorf("both BOR_METRICS_TLS_CERT_FILE and BOR_METRICS_TLS_KEY_FILE must be set, or neither")
	}

	// ─── Audit syslog ──────────────────────────────────────────────────────
	syslogEnabled := getEnvBool("BOR_AUDIT_SYSLOG_ENABLED", fc.Audit.Syslog.Enabled)
	syslogNetwork := getEnv("BOR_AUDIT_SYSLOG_NETWORK", fc.Audit.Syslog.Network)
	syslogAddr := getEnv("BOR_AUDIT_SYSLOG_ADDR", fc.Audit.Syslog.Addr)
	syslogFormat := getEnv("BOR_AUDIT_SYSLOG_FORMAT", fc.Audit.Syslog.Format)
	syslogFacilityStr := getEnv("BOR_AUDIT_SYSLOG_FACILITY", strconv.Itoa(fc.Audit.Syslog.Facility))
	syslogFacility, err := strconv.Atoi(syslogFacilityStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BOR_AUDIT_SYSLOG_FACILITY: %w", err)
	}
	syslogTLSCA := getEnv("BOR_AUDIT_SYSLOG_TLS_CA", fc.Audit.Syslog.TLSCAFile)

	// ─── Audit retention ───────────────────────────────────────────────────
	auditRetentionStr := getEnv("BOR_AUDIT_RETENTION_DAYS", strconv.Itoa(fc.Audit.RetentionDays))
	auditRetentionDays, err := strconv.Atoi(auditRetentionStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BOR_AUDIT_RETENTION_DAYS: %w", err)
	}

	// ─── JWT lifetimes ────────────────────────────────────────────────────
	jwtLifetimeStr := getEnv("BOR_JWT_LIFETIME", fc.Security.JWTLifetime)
	jwtLifetime, err := time.ParseDuration(jwtLifetimeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BOR_JWT_LIFETIME: %w", err)
	}
	refreshLifetimeStr := getEnv("BOR_REFRESH_LIFETIME", fc.Security.RefreshLifetime)
	refreshLifetime, err := time.ParseDuration(refreshLifetimeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BOR_REFRESH_LIFETIME: %w", err)
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
			JWTSecret:       resolveJWTSecret(getEnv("JWT_SECRET", fc.Security.JWTSecret)),
			JWTLifetime:     jwtLifetime,
			RefreshLifetime: refreshLifetime,
			TLSEnabled:      getEnvBool("TLS_ENABLED", false),
			TLSCertFile:     getEnv("TLS_CERT_FILE", ""),
			TLSKeyFile:      getEnv("TLS_KEY_FILE", ""),
			AdminToken:      getEnv("BOR_ADMIN_TOKEN", fc.Security.AdminToken),
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
			Enabled:         ldapEnabled,
			Host:            getEnv("LDAP_HOST", fc.LDAP.Host),
			Port:            ldapPort,
			UseTLS:          ldapUseTLS,
			StartTLS:        ldapStartTLS,
			TLSCAFile:       getEnv("LDAP_TLS_CA_FILE", fc.LDAP.TLSCAFile),
			TLSSkipVerify:   ldapTLSSkipVerify,
			BindDN:          getEnv("LDAP_BIND_DN", fc.LDAP.BindDN),
			BindPassword:    getEnv("LDAP_BIND_PASSWORD", fc.LDAP.BindPassword),
			BaseDN:          getEnv("LDAP_BASE_DN", fc.LDAP.BaseDN),
			UserFilter:      getEnv("LDAP_USER_FILTER", fc.LDAP.UserFilter),
			UPNSuffix:       getEnv("LDAP_UPN_SUFFIX", fc.LDAP.UPNSuffix),
			AttrUsername:    getEnv("LDAP_ATTR_USERNAME", fc.LDAP.AttrUsername),
			AttrEmail:       getEnv("LDAP_ATTR_EMAIL", fc.LDAP.AttrEmail),
			AttrFullName:    getEnv("LDAP_ATTR_FULL_NAME", fc.LDAP.AttrFullName),
			GroupBaseDN:     getEnv("LDAP_GROUP_BASE_DN", fc.LDAP.GroupBaseDN),
			GroupFilter:     getEnv("LDAP_GROUP_FILTER", fc.LDAP.GroupFilter),
			GroupMemberAttr: getEnv("LDAP_GROUP_MEMBER_ATTR", fc.LDAP.GroupMemberAttr),
			AttrMemberOf:    getEnv("LDAP_ATTR_MEMBER_OF", fc.LDAP.AttrMemberOf),
			PageSize:        ldapPageSize,
			GroupRoleMap:    ldapGroupRoleMap,
		},
		Kerberos: KerberosConfig{
			Enabled:            getEnvBool("BOR_KERBEROS_ENABLED", fc.Kerberos.Enabled),
			Realm:              getEnv("BOR_KERBEROS_REALM", fc.Kerberos.Realm),
			KeytabFile:         getEnv("BOR_KERBEROS_KEYTAB", fc.Kerberos.KeytabFile),
			ServicePrincipal:   getEnv("BOR_KERBEROS_PRINCIPAL", fc.Kerberos.ServicePrincipal),
			DefaultNodeGroupID: getEnv("BOR_KERBEROS_DEFAULT_NODE_GROUP", fc.Kerberos.DefaultNodeGroupID),
		},
		WebAuthn: WebAuthnConfig{
			RPID:        webAuthnRPID,
			RPOrigins:   webAuthnOrigins,
			DisplayName: webAuthnDisplayName,
		},
		Metrics: MetricsConfig{
			ListenAddr:  metricsAddr,
			BearerToken: metricsToken,
			TLSCertFile: metricsTLSCert,
			TLSKeyFile:  metricsTLSKey,
		},
		UI: UIConfig{
			PrivacyPolicyURL: getEnv("BOR_PRIVACY_POLICY_URL", fc.UI.PrivacyPolicyURL),
		},
		Audit: AuditConfig{
			RetentionDays: auditRetentionDays,
			AnonymizeIPs:  getEnvBool("BOR_AUDIT_ANONYMIZE_IPS", false),
			Syslog: SyslogConfig{
				Enabled:   syslogEnabled,
				Network:   syslogNetwork,
				Addr:      syslogAddr,
				Format:    syslogFormat,
				Facility:  syslogFacility,
				TLSCAFile: syslogTLSCA,
			},
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
	fc.Database.SSLMode = "require"
	fc.Security.JWTSecret = defaultJWTSecret
	fc.Security.JWTLifetime = "1h"
	fc.Security.RefreshLifetime = "24h"
	fc.TLS.AutogenDir = "/var/lib/bor/pki/ui"
	fc.CA.AutogenDir = "/var/lib/bor/pki/ca"
	fc.LDAP.Host = "localhost"
	fc.LDAP.Port = 389
	fc.LDAP.UserFilter = "(uid=%s)"
	fc.LDAP.AttrUsername = "uid"
	fc.LDAP.AttrEmail = "mail"
	fc.LDAP.AttrFullName = "cn"
	fc.LDAP.GroupMemberAttr = "member"
	fc.LDAP.AttrMemberOf = "memberOf"
	fc.LDAP.PageSize = 500
	fc.Metrics.ListenAddr = "127.0.0.1:9090"
	fc.Audit.RetentionDays = 365
	fc.Audit.Syslog.Network = "udp"
	fc.Audit.Syslog.Addr = "localhost:514"
	fc.Audit.Syslog.Format = "cef"
	fc.Audit.Syslog.Facility = 16 // local0
	return fc
}

const defaultJWTSecret = "change-me-in-production"

// resolveJWTSecret validates the JWT secret and auto-generates one if the
// default placeholder is still in use. A generated secret does not survive
// server restarts (all sessions are invalidated on restart).
func resolveJWTSecret(secret string) string {
	if secret != defaultJWTSecret && len(secret) >= 32 {
		return secret
	}

	if secret == defaultJWTSecret {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			// Extremely unlikely; crypto/rand should always succeed.
			log.Fatalf("Failed to generate random JWT secret: %v", err)
		}
		generated := hex.EncodeToString(b)
		log.Println("WARNING: JWT_SECRET not set — using auto-generated secret (sessions will not survive restarts)")
		return generated
	}

	// Secret was set explicitly but is too short.
	log.Fatalf("JWT_SECRET is too short (%d bytes); minimum 32 bytes required", len(secret))
	return "" // unreachable
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

// parseGroupRoleMap parses a string like "Domain Admins=Super Admin,IT Staff=Org Admin"
// into a map[string]string. Entries with no '=' separator are silently skipped.
func parseGroupRoleMap(s string) map[string]string {
	if s == "" {
		return nil
	}
	m := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "=")
		if idx < 1 {
			continue
		}
		group := strings.TrimSpace(part[:idx])
		role := strings.TrimSpace(part[idx+1:])
		if group != "" && role != "" {
			m[group] = role
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
