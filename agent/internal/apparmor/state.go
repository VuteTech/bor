// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package apparmor reads and changes the AppArmor state of a node: the
// kernel switches, loaded profiles and process labels, the profile files
// under /etc/apparmor.d with their disable and force-complain links, the
// site-local override files, and apparmor_parser with constant argv.
package apparmor

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Paths are the kernel and file system locations the package uses; tests
// point them at a temporary directory.
type Paths struct {
	// EnabledParam is /sys/module/apparmor/parameters/enabled.
	EnabledParam string
	// SecurityFS is /sys/kernel/security/apparmor.
	SecurityFS string
	// ProfilesDir is /etc/apparmor.d.
	ProfilesDir string
	// Proc is /proc, for process labels.
	Proc string
	// ProcSysKernel is /proc/sys/kernel, for the apparmor_* sysctls.
	ProcSysKernel string
	// LSMList is /sys/kernel/security/lsm.
	LSMList string
}

// DefaultPaths returns the real paths.
func DefaultPaths(profilesDir string) Paths {
	if profilesDir == "" {
		profilesDir = "/etc/apparmor.d"
	}
	return Paths{
		EnabledParam:  "/sys/module/apparmor/parameters/enabled",
		SecurityFS:    "/sys/kernel/security/apparmor",
		ProfilesDir:   profilesDir,
		Proc:          "/proc",
		ProcSysKernel: "/proc/sys/kernel",
		LSMList:       "/sys/kernel/security/lsm",
	}
}

// Enabled reports whether the AppArmor LSM is active.
func (p *Paths) Enabled() bool {
	data, err := os.ReadFile(p.EnabledParam)
	return err == nil && strings.TrimSpace(string(data)) == "Y"
}

// InLSMList reports whether apparmor is registered in the kernel's LSM
// list (it may still be disabled).
func (p *Paths) InLSMList() bool {
	data, err := os.ReadFile(p.LSMList)
	if err != nil {
		return false
	}
	for _, name := range strings.Split(strings.TrimSpace(string(data)), ",") {
		if strings.TrimSpace(name) == "apparmor" {
			return true
		}
	}
	return false
}

// LoadedProfiles returns the profiles the kernel has loaded, name to mode.
func (p *Paths) LoadedProfiles() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(p.SecurityFS, "profiles"))
	if err != nil {
		return nil, err
	}
	return ParseProfilesList(data), nil
}

// ParseProfilesList parses "<name> (<mode>)" lines.
func ParseProfilesList(data []byte) map[string]string {
	out := make(map[string]string)
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		name, mode := line, ""
		if i := strings.LastIndex(line, " ("); i >= 0 && strings.HasSuffix(line, ")") {
			name = line[:i]
			mode = line[i+2 : len(line)-1]
		}
		out[name] = mode
	}
	return out
}

// UsernsMediation reports whether the kernel mediates user namespace
// creation (features/namespaces/mask lists userns_create).
func (p *Paths) UsernsMediation() bool {
	data, err := os.ReadFile(filepath.Join(p.SecurityFS, "features", "namespaces", "mask"))
	return err == nil && strings.Contains(string(data), "userns_create")
}

// Sysctl names of the user namespace restriction.
const (
	SysctlRestrictUserns     = "apparmor_restrict_unprivileged_userns"
	SysctlRestrictUnconfined = "apparmor_restrict_unprivileged_unconfined"
)

// Sysctl reads an apparmor_* sysctl; ok is false when the kernel has none.
func (p *Paths) Sysctl(name string) (value int, ok bool) {
	data, err := os.ReadFile(filepath.Join(p.ProcSysKernel, name)) //nolint:gosec // G304: fixed sysctl names under /proc/sys/kernel
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return n, true
}

// SetSysctl writes an apparmor_* sysctl.
func (p *Paths) SetSysctl(name string, value int) error {
	return os.WriteFile(filepath.Join(p.ProcSysKernel, name), []byte(strconv.Itoa(value)+"\n"), 0o600)
}

// ProcessLabel is the confinement of one process.
type ProcessLabel struct {
	PID   int
	Exe   string
	Label string
	Mode  string
}

// ProcessLabels reads the confinement label and executable of every
// process. Processes that vanish while scanning are skipped.
func (p *Paths) ProcessLabels() ([]ProcessLabel, error) {
	entries, err := os.ReadDir(p.Proc)
	if err != nil {
		return nil, err
	}
	var out []ProcessLabel
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.Proc, e.Name(), "attr", "apparmor", "current")) //nolint:gosec // G304: /proc/<pid> entries
		if err != nil {
			data, err = os.ReadFile(filepath.Join(p.Proc, e.Name(), "attr", "current"))
			if err != nil {
				continue
			}
		}
		label := strings.TrimSpace(string(data))
		mode := ""
		if i := strings.LastIndex(label, " ("); i >= 0 && strings.HasSuffix(label, ")") {
			mode = label[i+2 : len(label)-1]
			label = label[:i]
		}
		exe, _ := os.Readlink(filepath.Join(p.Proc, e.Name(), "exe"))
		exe = strings.TrimSuffix(exe, " (deleted)")
		out = append(out, ProcessLabel{PID: pid, Exe: exe, Label: label, Mode: mode})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

// Link subdirectories of the profile directory.
const (
	DisableDir       = "disable"
	ForceComplainDir = "force-complain"
	LocalDir         = "local"
	HomeTunablesDir  = "tunables/home.d"
)

// LinkState reports whether a profile file has a disable or
// force-complain link.
func (p *Paths) LinkState(file string) (disabled, complain bool) {
	if _, err := os.Lstat(filepath.Join(p.ProfilesDir, DisableDir, file)); err == nil {
		disabled = true
	}
	if _, err := os.Lstat(filepath.Join(p.ProfilesDir, ForceComplainDir, file)); err == nil {
		complain = true
	}
	return disabled, complain
}

// SetLink creates or removes the disable or force-complain link of a
// profile file and reports whether anything changed.
func (p *Paths) SetLink(sub, file string, want bool) (bool, error) {
	link := filepath.Join(p.ProfilesDir, sub, file)
	_, err := os.Lstat(link)
	exists := err == nil
	switch {
	case want && !exists:
		if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
			return false, err
		}
		return true, os.Symlink(filepath.Join(p.ProfilesDir, file), link)
	case !want && exists:
		return true, os.Remove(link)
	}
	return false, nil
}

// ProfilePath returns the path of a profile file.
func (p *Paths) ProfilePath(file string) string {
	return filepath.Join(p.ProfilesDir, file)
}

// LocalPath returns the path of a profile's local override file.
func (p *Paths) LocalPath(file string) string {
	return filepath.Join(p.ProfilesDir, LocalDir, file)
}

// HomeTunablePath returns the path of Bor's home directory tunable.
func (p *Paths) HomeTunablePath() string {
	return filepath.Join(p.ProfilesDir, HomeTunablesDir, "bor")
}
