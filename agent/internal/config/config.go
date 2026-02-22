// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

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
}

// ServerConfig holds server connection settings.
type ServerConfig struct {
	Address            string `yaml:"address"`
	CACert             string `yaml:"ca_cert"`              // optional path to CA certificate for TLS verification
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"` // skip server certificate verification (for self-signed)
}

// AgentConfig holds agent identification settings.
type AgentConfig struct {
	ClientID string `yaml:"client_id"`
}

// FirefoxConfig holds Firefox policy file settings.
type FirefoxConfig struct {
	PoliciesPath       string `yaml:"policies_path"`
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
	ConfigPath string `yaml:"config_path"` // base directory for KDE config files (default /etc/xdg)
}

// EnrollmentConfig holds enrollment and mTLS settings.
type EnrollmentConfig struct {
	DataDir string `yaml:"data_dir"` // directory for persisted certs/keys (default /var/lib/bor/agent)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address: "localhost:8443",
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
			ConfigPath: "/etc/xdg",
		},
		Enrollment: EnrollmentConfig{
			DataDir: "/var/lib/bor/agent",
		},
	}
}

// Load reads the YAML configuration file at the given path.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
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
