// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package config loads and provides the agent configuration.
package config

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the agent configuration.
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Agent       AgentConfig       `yaml:"agent"`
	Firefox     FirefoxConfig     `yaml:"firefox"`
	Thunderbird ThunderbirdConfig `yaml:"thunderbird"`
	Chrome      ChromeConfig      `yaml:"chrome"`
	Edge        EdgeConfig        `yaml:"edge"`
	Firewalld   FirewalldConfig   `yaml:"firewalld"`
	Flatpak     FlatpakConfig     `yaml:"flatpak"`
	Systemd     SystemdConfig     `yaml:"systemd"`
	Selinux     SelinuxConfig     `yaml:"selinux"`
	Apparmor    ApparmorConfig    `yaml:"apparmor"`
	Mac         MacConfig         `yaml:"mac"`
	Updates     UpdatesConfig     `yaml:"updates"`
	KConfig     KConfigConfig     `yaml:"kconfig"`
	Enrollment  EnrollmentConfig  `yaml:"enrollment"`
	Kerberos    KerberosConfig    `yaml:"kerberos"`

	DiskEncryption DiskEncryptionConfig `yaml:"disk_encryption"`
}

// DiskEncryptionConfig holds LUKS enforcement settings
// .
type DiskEncryptionConfig struct {
	StateFile    string `yaml:"state_file"`    // rotation crash-recovery state (default /var/lib/bor/agent/luks-state.json; never holds keys)
	BootstrapDir string `yaml:"bootstrap_dir"` // one-time provisioning credentials (default /etc/bor/luks-bootstrap)
	RunDir       string `yaml:"run_dir"`       // root-only tmpfs work dir, Clevis TMPDIR (default /run/bor/luks)
	Cryptsetup   string `yaml:"cryptsetup"`    // default "cryptsetup" (PATH)
	Cryptenroll  string `yaml:"cryptenroll"`   // default "systemd-cryptenroll"
	Clevis       string `yaml:"clevis"`        // default "clevis"
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

// ThunderbirdConfig holds Thunderbird policy file settings.
type ThunderbirdConfig struct {
	PoliciesPath        string `yaml:"policies_path"`
	FlatpakPoliciesPath string `yaml:"flatpak_policies_path"`
}

// EdgeConfig holds Microsoft Edge policy directory settings.
// The agent writes bor_managed.json into the configured directory.
// A single path covers all Edge channels (stable, beta, dev) and the
// community Edge Flatpak, which uses --filesystem=host-etc to symlink
// /run/host/etc/opt/edge/policies/ into the sandbox automatically.
type EdgeConfig struct {
	EdgePoliciesPath string `yaml:"edge_policies_path"`
}

// FirewalldConfig holds firewalld zone management settings. The agent renders
// managed zone XML into ZonesDir and reloads firewalld.
type FirewalldConfig struct {
	ZonesDir string `yaml:"zones_dir"`
}

// SystemdConfig holds the settings of the Services & timers policy. The
// agent writes one drop-in per managed unit and the unit files of Bor
// tasks under SystemDir, task scripts under ScriptsDir, the preset file
// under PresetDir, keeps the original unit states in StatePath so they can
// be restored, and drives systemd over D-Bus.
type SystemdConfig struct {
	SystemDir         string        `yaml:"system_dir"`         // administrator unit directory (default /etc/systemd/system)
	PresetDir         string        `yaml:"preset_dir"`         // preset directory for 10-bor.preset (default /etc/systemd/system-preset)
	ScriptsDir        string        `yaml:"scripts_dir"`        // task scripts (default /etc/bor/systemd)
	StatePath         string        `yaml:"state_path"`         // record of original unit states (default /var/lib/bor/agent/systemd-state.json)
	JobTimeout        time.Duration `yaml:"job_timeout"`        // wait for a start/stop/restart job (default 90s)
	ReconcileInterval time.Duration `yaml:"reconcile_interval"` // re-assert unit states this often while policies apply; 0 disables (default 10m)
	WatchSignals      bool          `yaml:"watch_signals"`      // react to unit state changes signalled by systemd (default true)
	AllowTasks        bool          `yaml:"allow_tasks"`        // deploy Bor tasks (scripts run as root); false reports them as errors (default true)
	AnalyzeBinary     string        `yaml:"analyze_binary"`     // systemd-analyze executable for calendar checks (default "systemd-analyze")
	UserDir           string        `yaml:"user_dir"`           // unit directory of the user-manager scope (default /etc/systemd/user)
	UserPresetDir     string        `yaml:"user_preset_dir"`    // preset directory of the user-manager scope (default /etc/systemd/user-preset)
	AllowActions      bool          `yaml:"allow_actions"`      // run start/stop/restart requested from the node page (default true)
}

// SelinuxConfig holds the settings of the SELinux policy. The agent sets
// the mode through selinuxfs and /etc/selinux/config, booleans with
// setsebool, writes Bor's CIL modules under ModulesDir and installs them
// with semodule at ModulePriority, and keeps the original values in
// StatePath so they can be restored.
type SelinuxConfig struct {
	ConfigPath         string        `yaml:"config_path"`          // /etc/selinux/config
	ModulesDir         string        `yaml:"modules_dir"`          // Bor CIL module sources (default /etc/bor/selinux)
	ModulePriority     int           `yaml:"module_priority"`      // semodule -X priority of Bor modules (default 450)
	StatePath          string        `yaml:"state_path"`           // record of original values (default /var/lib/bor/agent/selinux-state.json)
	ReconcileInterval  time.Duration `yaml:"reconcile_interval"`   // re-assert the state this often while policies apply; 0 disables (default 5m)
	AllowModeChange    bool          `yaml:"allow_mode_change"`    // let policies switch between enforcing and permissive (default true)
	AllowCustomModules bool          `yaml:"allow_custom_modules"` // install Bor CIL modules and file contexts (default true)
	FollowAudit        bool          `yaml:"follow_audit"`         // follow the audit log so a manual change is reconciled within seconds (default true)
	ToolTimeout        time.Duration `yaml:"tool_timeout"`         // maximum run time of one semodule or setsebool call (default 5m)
}

// ApparmorConfig holds the settings of the AppArmor policy. The agent
// manages profile modes through the disable and force-complain links of
// ProfilesDir, writes Bor profiles there, local overrides under its
// local/ directory and the home directory tunable, and drives
// apparmor_parser.
type ApparmorConfig struct {
	ProfilesDir          string        `yaml:"profiles_dir"`            // /etc/apparmor.d
	ParserBinary         string        `yaml:"parser_binary"`           // apparmor_parser executable (default "apparmor_parser")
	SysctlPath           string        `yaml:"sysctl_path"`             // sysctl.d file for the user namespace switches (default /etc/sysctl.d/zz-bor-apparmor.conf)
	StatePath            string        `yaml:"state_path"`              // record of original states (default /var/lib/bor/agent/apparmor-state.json)
	BackupDir            string        `yaml:"backup_dir"`              // backups of vendor files whose flags were edited (default /var/lib/bor/agent/apparmor-backups)
	ReconcileInterval    time.Duration `yaml:"reconcile_interval"`      // re-assert the state this often while policies apply; 0 disables (default 5m)
	AllowCustomProfiles  bool          `yaml:"allow_custom_profiles"`   // write Bor profiles and local overrides (default true)
	AllowVendorFlagEdits bool          `yaml:"allow_vendor_flag_edits"` // let policies edit the mode flags inside vendor profile files (default false)
	FollowAudit          bool          `yaml:"follow_audit"`            // follow the audit log so a manual change is reconciled within seconds (default true)
	ToolTimeout          time.Duration `yaml:"tool_timeout"`            // maximum run time of one apparmor_parser call (default 2m)
}

// MacConfig holds what both mandatory access control types share: the
// inventory report cadence and the denial summary.
type MacConfig struct {
	InventoryInterval time.Duration `yaml:"inventory_interval"` // how often the inventory is re-sent while connected (default 1h)
	DenialWindow      time.Duration `yaml:"denial_window"`      // how far back denials are summarised (default 24h)
	CollectDenials    bool          `yaml:"collect_denials"`    // read the audit records of the journal for the denial summary (default true)
	AllowActions      bool          `yaml:"allow_actions"`      // answer proposal requests from the node page (default true)
}

// UpdatesConfig holds the settings of the Software updates policy. The
// agent configures the distribution's update engine (dnf-automatic,
// unattended-upgrades, os-update, rpm-ostree), keeps its timer in step
// with the policy, decides reboots and reports the update state.
type UpdatesConfig struct {
	StatePath           string        `yaml:"state_path"`            // record of original states (default /var/lib/bor/agent/updates-state.json)
	BackupDir           string        `yaml:"backup_dir"`            // backups of engine configuration files Bor replaced (default /var/lib/bor/agent/updates-backups)
	ReconcileInterval   time.Duration `yaml:"reconcile_interval"`    // re-assert the engine state this often while policies apply; 0 disables (default 10m)
	InventoryInterval   time.Duration `yaml:"inventory_interval"`    // how often the update state is re-sent while connected (default 1h)
	RebootCheckInterval time.Duration `yaml:"reboot_check_interval"` // how often the reboot rule runs while a policy applies (default 1m)
	ToolTimeout         time.Duration `yaml:"tool_timeout"`          // maximum run time of one engine or package manager call (default 10m)
	AllowInstall        bool          `yaml:"allow_install"`         // let policies install the engine package and unattended updates (default true)
	AllowReboot         bool          `yaml:"allow_reboot"`          // let policies reboot the node (default true)
}

// FlatpakConfig holds Flatpak enforcement settings. The agent renders the
// managed remote definitions (.flatpakrepo, .filter, .gpg) into StateDir and
// drives the flatpak CLI for remotes and applications.
type FlatpakConfig struct {
	Binary    string `yaml:"binary"`     // flatpak executable (default "flatpak", resolved via PATH)
	StateDir  string `yaml:"state_dir"`  // managed remote files (default /etc/bor/flatpak)
	StateFile string `yaml:"state_file"` // created/adopted remote record (default /var/lib/bor/agent/flatpak-state.json)
	ProxyURL  string `yaml:"proxy_url"`  // optional http(s) proxy exported to flatpak for downloads
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
	// WriteKde5rc also mirrors System Settings module restrictions into
	// /etc/kde5rc. Plasma 5 and 6 read them from kdeglobals, so this is only
	// for very old KDE Frameworks releases (default false).
	WriteKde5rc bool `yaml:"write_kde5rc"`
	// ProbeEffective reads written settings back with kreadconfig in a
	// logged-in user's session after each sync and reports user overrides
	// of non-enforced defaults as non-compliant (default false).
	ProbeEffective bool `yaml:"probe_effective"`
	// DataPath is the directory for generated data files (look-and-feel
	// packages, Plasma update scripts), put first in XDG_DATA_DIRS by the
	// hook scripts (default /etc/bor/share).
	DataPath string `yaml:"data_path"`
	// FlatpakOverride exposes the config directory to Flatpak applications
	// through a system-wide `flatpak override` while KDE policies apply,
	// so sandboxed KDE applications read the managed settings (default true;
	// only when flatpak is installed).
	FlatpakOverride bool `yaml:"flatpak_override"`
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
		Thunderbird: ThunderbirdConfig{
			PoliciesPath:        "/etc/thunderbird/policies/policies.json",
			FlatpakPoliciesPath: "/var/lib/flatpak/extension/net.thunderbird.Thunderbird.systemconfig/" + flatpakArch() + "/stable/policies/policies.json",
		},
		Chrome: ChromeConfig{
			ChromePoliciesPath:          "/etc/opt/chrome/policies/managed",
			ChromiumPoliciesPath:        "/etc/chromium/policies/managed",
			ChromiumBrowserPoliciesPath: "/etc/chromium-browser/policies/managed",
			FlatpakChromiumPoliciesPath: "/var/lib/flatpak/extension/org.chromium.Chromium.Extension.system-policies/" + flatpakArch() + "/1/policies/managed",
		},
		Edge: EdgeConfig{
			EdgePoliciesPath: "/etc/opt/edge/policies/managed",
		},
		Firewalld: FirewalldConfig{
			ZonesDir: "/etc/firewalld/zones",
		},
		Flatpak: FlatpakConfig{
			Binary:    "flatpak",
			StateDir:  "/etc/bor/flatpak",
			StateFile: "/var/lib/bor/agent/flatpak-state.json",
		},
		Systemd: SystemdConfig{
			SystemDir:         "/etc/systemd/system",
			PresetDir:         "/etc/systemd/system-preset",
			ScriptsDir:        "/etc/bor/systemd",
			StatePath:         "/var/lib/bor/agent/systemd-state.json",
			JobTimeout:        90 * time.Second,
			ReconcileInterval: 10 * time.Minute,
			WatchSignals:      true,
			AllowTasks:        true,
			AnalyzeBinary:     "systemd-analyze",
			UserDir:           "/etc/systemd/user",
			UserPresetDir:     "/etc/systemd/user-preset",
			AllowActions:      true,
		},
		Selinux: SelinuxConfig{
			ConfigPath:         "/etc/selinux/config",
			ModulesDir:         "/etc/bor/selinux",
			ModulePriority:     450,
			StatePath:          "/var/lib/bor/agent/selinux-state.json",
			ReconcileInterval:  5 * time.Minute,
			AllowModeChange:    true,
			AllowCustomModules: true,
			FollowAudit:        true,
			ToolTimeout:        5 * time.Minute,
		},
		Apparmor: ApparmorConfig{
			ProfilesDir:         "/etc/apparmor.d",
			ParserBinary:        "apparmor_parser",
			SysctlPath:          "/etc/sysctl.d/zz-bor-apparmor.conf",
			StatePath:           "/var/lib/bor/agent/apparmor-state.json",
			BackupDir:           "/var/lib/bor/agent/apparmor-backups",
			ReconcileInterval:   5 * time.Minute,
			AllowCustomProfiles: true,
			FollowAudit:         true,
			ToolTimeout:         2 * time.Minute,
		},
		Mac: MacConfig{
			InventoryInterval: time.Hour,
			DenialWindow:      24 * time.Hour,
			CollectDenials:    true,
			AllowActions:      true,
		},
		Updates: UpdatesConfig{
			StatePath:           "/var/lib/bor/agent/updates-state.json",
			BackupDir:           "/var/lib/bor/agent/updates-backups",
			ReconcileInterval:   10 * time.Minute,
			InventoryInterval:   time.Hour,
			RebootCheckInterval: time.Minute,
			ToolTimeout:         10 * time.Minute,
			AllowInstall:        true,
			AllowReboot:         true,
		},
		KConfig: KConfigConfig{
			ConfigPath:      "/etc/bor/xdg",
			DataPath:        "/etc/bor/share",
			FlatpakOverride: true,
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
