// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package config loads and provides the agent configuration.
package config

import (
	"fmt"
	"os"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config holds the agent configuration.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Agent      AgentConfig      `yaml:"agent"`
	Firefox    FirefoxConfig    `yaml:"firefox"`
	Chrome     ChromeConfig     `yaml:"chrome"`
	KConfig    KConfigConfig    `yaml:"kconfig"`
	Enrollment EnrollmentConfig `yaml:"enrollment"`
	Kerberos   KerberosConfig   `yaml:"kerberos"`
}

// ServerConfig holds server connection settings.
// ServerConfig holds server connection settings.
// The server runs on two ports: one for enrollment + UI (no mandatory client cert),
// and one for policy streaming / cert renewal (RequireAndVerifyClientCert).
type ServerConfig struct {
	Address            string `yaml:"address"`              // server hostname or IP (no port)
	EnrollmentPort     int    `yaml:"enrollment_port"`      // port for enrollment RPC and admin UI (default 8443)
	PolicyPort         int    `yaml:"policy_port"`          // port for mTLS policy streaming and cert renewal (default 8444)
	CACert             string `yaml:"ca_cert"`              // optional path to CA cert for TLS verification
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"` // skip TLS verification during enrollment
}

// EnrollmentAddr returns the host:port for the enrollment / UI server.
func (s ServerConfig) EnrollmentAddr() string {
	return fmt.Sprintf("%s:%d", s.Address, s.EnrollmentPort)
}

// PolicyAddr returns the host:port for the mTLS policy streaming server.
func (s ServerConfig) PolicyAddr() string {
	return fmt.Sprintf("%s:%d", s.Address, s.PolicyPort)
}

// AgentConfig holds agent identification settings.
type AgentConfig struct {
	ClientID string `yaml:"client_id"`
}

// FirefoxConfig holds Firefox policy file settings.
type FirefoxConfig struct {
	PoliciesPath        string `yaml:"policies_path"`
	FlatpakPoliciesPath string `yaml:"flatpak_policies_path"`
}

// ChromeConfig holds Chrome/Chromium policy directory settings.
// The agent writes bor_managed.json into each configured directory.
type ChromeConfig struct {
	// Google Chrome (all channels: stable, beta, dev, unstable)
	ChromePoliciesPath string `yaml:"chrome_policies_path"`
	// Chromium (upstream build: Arch Linux, self-built, etc.)
	ChromiumPoliciesPath string `yaml:"chromium_policies_path"`
	// Chromium (Debian/Ubuntu deb package: chromium-browser)
	ChromiumBrowserPoliciesPath string `yaml:"chromium_browser_policies_path"`
	// Flatpak Chromium (org.chromium.Chromium) — set empty to disable
	FlatpakChromiumPoliciesPath string `yaml:"flatpak_chromium_policies_path"`
}

// KConfigConfig holds KDE Kiosk (KConfig) policy settings.
type KConfigConfig struct {
	ConfigPath string `yaml:"config_path"` // base directory for KDE config files (default /etc/bor/xdg)
}

// EnrollmentConfig holds enrollment and mTLS settings.
type EnrollmentConfig struct {
	DataDir string `yaml:"data_dir"` // directory for persisted certs/keys (default /var/lib/bor/agent)
}

// KerberosConfig holds agent-side Kerberos configuration for token-free enrollment.
// When enabled, the agent authenticates to the Bor server using the machine
// keytab instead of requiring a manually generated enrollment token.
// Supported on FreeIPA and Active Directory (Samba AD) domain-joined hosts.
type KerberosConfig struct {
	// Enabled activates Kerberos-based enrollment.  When true, the agent
	// attempts Kerberos enrollment first; it falls back to token-based
	// enrollment only when KeytabFile is not present on disk.
	Enabled bool `yaml:"enabled"`
	// KeytabFile is the path to the machine keytab.
	// FreeIPA: /etc/krb5.keytab (created by ipa-client-install)
	// AD:      /etc/krb5.keytab (created by realm join / adcli)
	// Default: /etc/krb5.keytab
	KeytabFile string `yaml:"keytab_file"`
	// ServicePrincipal is the Kerberos principal of the Bor server service,
	// e.g. "HTTP/bor.example.com@EXAMPLE.COM".  The agent requests a service
	// ticket for this principal from the KDC before contacting the server.
	ServicePrincipal string `yaml:"service_principal"`
	// KDC is the hostname or IP address of the Key Distribution Center.
	// When set, the agent builds its Kerberos configuration from this address
	// and the realm extracted from ServicePrincipal, bypassing /etc/krb5.conf.
	// This is useful when the system krb5.conf does not list the realm or when
	// its syntax is incompatible with the Kerberos library used by the agent.
	// Example: "dc1.example.com" or "192.0.2.10"
	KDC string `yaml:"kdc"`
	// MachinePrincipal is the Kerberos principal for this host's machine account
	// as it appears in the keytab (keytab_file).  When empty the agent defaults
	// to "host/<hostname>" which is correct for FreeIPA.  AD / Samba AD hosts
	// joined with realm(1) or adcli typically use "<HOSTNAME>$" (with a dollar
	// sign) or "host/<fqdn>@REALM".  Run "klist -kte /etc/krb5.keytab" to find
	// the exact principal name.
	// Examples:
	//   "FEDORA$"                          (AD short form — realm auto-appended)
	//   "host/fedora.example.com@EXAMPLE.COM"  (FreeIPA / AD long form)
	MachinePrincipal string `yaml:"machine_principal"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:        "localhost",
			EnrollmentPort: 8443,
			PolicyPort:     8444,
		},
		Agent: AgentConfig{},
		Firefox: FirefoxConfig{
			PoliciesPath:        "/etc/firefox/policies/policies.json",
			FlatpakPoliciesPath: "/var/lib/flatpak/extension/org.mozilla.firefox.systemconfig/" + flatpakArch() + "/stable/policies/policies.json",
		},
		Chrome: ChromeConfig{
			ChromePoliciesPath:          "/etc/opt/chrome/policies/managed",
			ChromiumPoliciesPath:        "/etc/chromium/policies/managed",
			ChromiumBrowserPoliciesPath: "/etc/chromium-browser/policies/managed",
			FlatpakChromiumPoliciesPath: "/var/lib/flatpak/extension/org.chromium.Chromium.Extension.system-policies/" + flatpakArch() + "/1/policies/managed",
		},
		KConfig: KConfigConfig{
			ConfigPath: "/etc/bor/xdg",
		},
		Enrollment: EnrollmentConfig{
			DataDir: "/var/lib/bor/agent",
		},
		Kerberos: KerberosConfig{
			KeytabFile: "/etc/krb5.keytab",
		},
	}
}

// Load reads the YAML configuration file at the given path.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path) //nolint:gosec // G304: path is user-supplied config file path
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if cfg.Agent.ClientID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("client_id not set and failed to get hostname: %w", err)
		}
		cfg.Agent.ClientID = hostname
	}

	return cfg, nil
}

// flatpakArch maps the Go runtime architecture to the Flatpak architecture
// string used in extension directory paths (e.g. x86_64, aarch64).
func flatpakArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	default:
		return runtime.GOARCH
	}
}
