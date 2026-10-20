// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package mac holds what the SELinux and AppArmor policy types share on
// the agent: detection of the active mandatory access control system, the
// node metadata fields, and the collection and aggregation of denials from
// the journal.
package mac

import (
	"os"
	"path/filepath"
	"strings"
)

// Paths are the kernel interfaces detection reads; tests point them at a
// temporary directory.
type Paths struct {
	// LSMList is /sys/kernel/security/lsm.
	LSMList string
	// SelinuxFS is the selinuxfs mount (/sys/fs/selinux).
	SelinuxFS string
	// ApparmorEnabled is /sys/module/apparmor/parameters/enabled.
	ApparmorEnabled string
	// Cmdline is /proc/cmdline.
	Cmdline string
}

// DefaultPaths returns the real kernel paths.
func DefaultPaths() Paths {
	return Paths{
		LSMList:         "/sys/kernel/security/lsm",
		SelinuxFS:       "/sys/fs/selinux",
		ApparmorEnabled: "/sys/module/apparmor/parameters/enabled",
		Cmdline:         "/proc/cmdline",
	}
}

// Info is what detection found.
type Info struct {
	// LSMs is the kernel's active LSM list, in order.
	LSMs []string
	// SelinuxInList and ApparmorInList say whether the LSM is registered.
	SelinuxInList  bool
	ApparmorInList bool
	// SelinuxActive means selinuxfs is mounted with an enforce switch.
	SelinuxActive bool
	// ApparmorEnabled means the module parameter reads Y.
	ApparmorEnabled bool
	// SelinuxEnforcing is the runtime switch when SELinux is active.
	SelinuxEnforcing bool
}

// Detect reads the kernel interfaces.
func Detect(p Paths) Info {
	var info Info
	if data, err := os.ReadFile(p.LSMList); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(data)), ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			info.LSMs = append(info.LSMs, name)
			switch name {
			case "selinux":
				info.SelinuxInList = true
			case "apparmor":
				info.ApparmorInList = true
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(p.SelinuxFS, "enforce")); err == nil {
		info.SelinuxActive = true
		info.SelinuxEnforcing = strings.TrimSpace(string(data)) == "1"
	}
	if data, err := os.ReadFile(p.ApparmorEnabled); err == nil {
		info.ApparmorEnabled = strings.TrimSpace(string(data)) == "Y"
	}
	return info
}

// Active returns the mandatory access control system in use: "selinux",
// "apparmor" or "none".
func (i Info) Active() string {
	switch {
	case i.SelinuxActive:
		return "selinux"
	case i.ApparmorEnabled:
		return "apparmor"
	}
	return "none"
}

// Mode returns the state word reported with the node metadata: SELinux
// "enforcing" | "permissive" | "disabled", AppArmor "enabled" |
// "disabled", or "" when no LSM is present.
func (i Info) Mode() string {
	switch {
	case i.SelinuxActive && i.SelinuxEnforcing:
		return "enforcing"
	case i.SelinuxActive:
		return "permissive"
	case i.SelinuxInList:
		return "disabled"
	case i.ApparmorEnabled:
		return "enabled"
	case i.ApparmorInList:
		return "disabled"
	}
	return ""
}

// Summary returns the two node metadata fields.
func Summary(p Paths) (lsm, mode string) {
	info := Detect(p)
	return info.Active(), info.Mode()
}

// BootFlags returns the kernel command line switches that decide which
// LSM runs and how: selinux=, enforcing=, apparmor=, security= and lsm=.
func BootFlags(cmdline string) []string {
	var out []string
	for _, tok := range strings.Fields(cmdline) {
		key, _, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch key {
		case "selinux", "enforcing", "apparmor", "security", "lsm", "autorelabel":
			out = append(out, tok)
		}
	}
	return out
}

// ReadBootFlags reads and filters /proc/cmdline.
func ReadBootFlags(p Paths) []string {
	data, err := os.ReadFile(p.Cmdline)
	if err != nil {
		return nil
	}
	return BootFlags(string(data))
}
