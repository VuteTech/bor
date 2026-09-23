// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

// Default paths.
const (
	// DefaultStateFile persists the per-volume rotation step (never keys).
	DefaultStateFile = "/var/lib/bor/agent/luks-state.json"
	// DefaultBootstrapDir holds one-time provisioning credentials
	// (<luks-uuid>.key, 0600, consumed during adoption).
	DefaultBootstrapDir = "/etc/bor/luks-bootstrap"
	// DefaultRunDir is a root-only tmpfs directory used as Clevis TMPDIR
	// (Clevis writes a full LUKS header backup there during a bind) and for
	// the adopt CLI lock. Cleaned after every run.
	DefaultRunDir = "/run/bor/luks"
	// DracutDropInPath is the Bor-managed dracut drop-in (MANAGE mode).
	DracutDropInPath = "/etc/dracut.conf.d/90-bor-disk-encryption.conf"
	// CrypttabPath is the system crypttab; Bor owns only its option tokens.
	CrypttabPath = "/etc/crypttab"
)

// Config carries the node-local settings of the luks package, sourced from
// the agent configuration's disk_encryption section.
type Config struct {
	// StateFile is the crash-recovery state path (default DefaultStateFile).
	StateFile string
	// BootstrapDir holds provisioning bootstrap keys.
	BootstrapDir string
	// RunDir is the root-only tmpfs work directory.
	RunDir string
	// Cryptsetup is the cryptsetup binary (default "cryptsetup").
	Cryptsetup string
	// Cryptenroll is the systemd-cryptenroll binary.
	Cryptenroll string
	// Clevis is the clevis binary (default "clevis").
	Clevis string
}

// CryptsetupBinary returns the cryptsetup binary name.
func (c *Config) CryptsetupBinary() string {
	if c == nil || c.Cryptsetup == "" {
		return "cryptsetup"
	}
	return c.Cryptsetup
}

// CryptenrollBinary returns the systemd-cryptenroll binary name.
func (c *Config) CryptenrollBinary() string {
	if c == nil || c.Cryptenroll == "" {
		return "systemd-cryptenroll"
	}
	return c.Cryptenroll
}

// ClevisBinary returns the clevis binary name.
func (c *Config) ClevisBinary() string {
	if c == nil || c.Clevis == "" {
		return "clevis"
	}
	return c.Clevis
}

// StateFilePath returns the state file path.
func (c *Config) StateFilePath() string {
	if c == nil || c.StateFile == "" {
		return DefaultStateFile
	}
	return c.StateFile
}

// BootstrapDirPath returns the bootstrap key directory.
func (c *Config) BootstrapDirPath() string {
	if c == nil || c.BootstrapDir == "" {
		return DefaultBootstrapDir
	}
	return c.BootstrapDir
}

// RunDirPath returns the tmpfs work directory.
func (c *Config) RunDirPath() string {
	if c == nil || c.RunDir == "" {
		return DefaultRunDir
	}
	return c.RunDir
}
