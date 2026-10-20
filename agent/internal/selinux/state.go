// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package selinux reads and changes the SELinux state of a node: the
// runtime and boot-time mode, booleans, the policy store, and the
// distribution tools the SELinux policy type drives (setsebool, semodule,
// restorecon, semanage, audit2allow) with constant argv.
package selinux

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Paths are the files the package reads and writes; tests point them at
// a temporary directory.
type Paths struct {
	// SysFS is the selinuxfs mount (/sys/fs/selinux).
	SysFS string
	// Config is /etc/selinux/config.
	Config string
	// SemanageConf is /etc/selinux/semanage.conf (store-root).
	SemanageConf string
	// Autorelabel is the flag file a relabel at boot is requested with.
	Autorelabel string
}

// DefaultPaths returns the real paths.
func DefaultPaths() Paths {
	return Paths{
		SysFS:        "/sys/fs/selinux",
		Config:       "/etc/selinux/config",
		SemanageConf: "/etc/selinux/semanage.conf",
		Autorelabel:  "/.autorelabel",
	}
}

// Active reports whether selinuxfs is mounted with a policy loaded.
func (p Paths) Active() bool {
	_, err := os.Stat(filepath.Join(p.SysFS, "enforce"))
	return err == nil
}

// Enforcing returns the runtime mode switch.
func (p Paths) Enforcing() (bool, error) {
	data, err := os.ReadFile(filepath.Join(p.SysFS, "enforce"))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == "1", nil
}

// SetEnforcing switches the runtime mode (what setenforce does).
func (p Paths) SetEnforcing(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return os.WriteFile(filepath.Join(p.SysFS, "enforce"), []byte(v), 0o600)
}

// PolicyVersion returns the loaded policy version, 0 when unknown.
func (p Paths) PolicyVersion() int {
	data, err := os.ReadFile(filepath.Join(p.SysFS, "policyvers"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

// BoolState is the current and pending value of a boolean.
type BoolState struct {
	Current bool
	Pending bool
}

// Booleans reads every boolean of the loaded policy from selinuxfs
// ("<current> <pending>" per file).
func (p Paths) Booleans() (map[string]BoolState, error) {
	dir := filepath.Join(p.SysFS, "booleans")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]BoolState, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: entry of the selinuxfs booleans directory
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 {
			continue
		}
		st := BoolState{Current: fields[0] == "1"}
		st.Pending = st.Current
		if len(fields) > 1 {
			st.Pending = fields[1] == "1"
		}
		out[e.Name()] = st
	}
	return out, nil
}

// PolicyloadLocked reports whether secure_mode_policyload is on, in which
// case no policy change is possible until reboot.
func (p Paths) PolicyloadLocked() bool {
	data, err := os.ReadFile(filepath.Join(p.SysFS, "booleans", "secure_mode_policyload"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 0 && fields[0] == "1"
}

// Config is the content of /etc/selinux/config.
type Config struct {
	// Mode is SELINUX= (enforcing, permissive, disabled); empty when unset.
	Mode string
	// Type is SELINUXTYPE= ("targeted").
	Type string
}

// ReadConfig parses /etc/selinux/config. A missing file yields an empty
// Config and no error.
func (p Paths) ReadConfig() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(p.Config)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "SELINUX":
			cfg.Mode = strings.ToLower(val)
		case "SELINUXTYPE":
			cfg.Type = val
		}
	}
	return cfg, nil
}

// WriteConfigMode sets SELINUX= in /etc/selinux/config, keeping every
// other line. It reports whether the file changed.
func (p Paths) WriteConfigMode(mode string) (bool, error) {
	switch mode {
	case "enforcing", "permissive":
	default:
		return false, fmt.Errorf("refusing to write SELINUX=%s", mode)
	}
	data, err := os.ReadFile(p.Config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	var out []string
	replaced := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "SELINUX=") {
			if replaced {
				continue
			}
			line = "SELINUX=" + mode
			replaced = true
		}
		out = append(out, line)
	}
	if !replaced {
		// Keep a trailing newline before the appended line.
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		out = append(out, "SELINUX="+mode, "")
	}
	next := strings.Join(out, "\n")
	if next == string(data) {
		return false, nil
	}
	return true, writeFileAtomic(p.Config, []byte(next), 0o644)
}

// writeFileAtomic writes through a temporary file in the same directory.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".bor-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// StoreRoot returns the policy store root: store-root from semanage.conf,
// or /var/lib/selinux.
func (p Paths) StoreRoot() string {
	root := "/var/lib/selinux"
	data, err := os.ReadFile(p.SemanageConf)
	if err != nil {
		return root
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "store-root" {
			if v := strings.TrimSpace(val); v != "" {
				root = v
			}
		}
	}
	return root
}

// RelabelPending reports whether a relabel is requested for the next boot.
func (p Paths) RelabelPending() bool {
	_, err := os.Stat(p.Autorelabel)
	return err == nil
}

// RequestRelabel creates the flag file fixfiles -F onboot creates: the
// next boot relabels every file system and reboots.
func (p Paths) RequestRelabel() error {
	if p.RelabelPending() {
		return nil
	}
	return os.WriteFile(p.Autorelabel, []byte("-F\n"), 0o644) //nolint:gosec // G306: the flag file is read by the boot process
}

// ToolPresent reports whether an executable can be found.
func ToolPresent(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// SortedNames returns the keys of a boolean map in order.
func SortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
