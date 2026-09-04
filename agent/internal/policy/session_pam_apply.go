// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PamManager identifies how the node's PAM account stack is managed, which
// determines whether Bor can enable pam_time itself.
type PamManager int

const (
	// PamManagerUnknown means the layout is not recognised — Bor never edits
	// the stack.
	PamManagerUnknown PamManager = iota
	// PamManagerDebian means pam-auth-update is present (Debian/Ubuntu family).
	PamManagerDebian
	// PamManagerAuthselect means the stack is authselect-managed (Fedora/RHEL)
	// — the v1 degraded path, where Bor does not edit system-auth behind
	// authselect's back.
	PamManagerAuthselect
)

func (m PamManager) String() string {
	switch m {
	case PamManagerDebian:
		return "pam-auth-update"
	case PamManagerAuthselect:
		return "authselect"
	default:
		return "unknown"
	}
}

// pamConfigProfilePath is the Debian pam-auth-update profile Bor manages.
const pamConfigProfilePath = "/usr/share/pam-configs/bor-session-access"

// pamConfigProfile is the pam-auth-update profile body adding pam_time to the
// account stack. Priority is mid-range so it runs after the primary modules.
const pamConfigProfile = `Name: Bor session access (pam_time)
Default: yes
Priority: 128
Account-Type: Additional
Account:
	required			pam_time.so
`

// pamTimeModuleDirs are the standard locations for pam_time.so. The multiarch
// dir is resolved separately at runtime.
var pamTimeModuleDirs = []string{
	"/lib/security", "/lib64/security",
	"/usr/lib/security", "/usr/lib64/security",
}

// DetectPamManager determines how this node's PAM stack is managed.
func DetectPamManager() PamManager {
	if authselectManaged() {
		return PamManagerAuthselect
	}
	if _, err := exec.LookPath("pam-auth-update"); err == nil {
		return PamManagerDebian
	}
	return PamManagerUnknown
}

// authselectManaged reports whether authselect owns the PAM stack: the tool is
// present and system-auth is a symlink into the authselect tree.
func authselectManaged() bool {
	if _, err := exec.LookPath("authselect"); err != nil {
		return false
	}
	target, err := os.Readlink("/etc/pam.d/system-auth")
	if err != nil {
		return false
	}
	return strings.Contains(target, "authselect")
}

// PamTimeModuleAvailable reports whether pam_time.so is installed. It checks the
// standard security dirs plus the Debian multiarch dir.
func PamTimeModuleAvailable() bool {
	dirs := append([]string(nil), pamTimeModuleDirs...)
	if matches, _ := filepath.Glob("/usr/lib/*/security"); matches != nil {
		dirs = append(dirs, matches...)
	}
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "pam_time.so")); err == nil {
			return true
		}
	}
	return false
}

// SystemGroupMembers returns the members of a group: its secondary members
// from `getent group`, plus any users whose *primary* group it is (found by
// scanning `getent passwd` for the gid), so Layer 2 agrees with Layer 1's
// getgrouplist-based matching. Runs only at policy-sync time, not per tick.
//
// Directory services (SSSD/LDAP) commonly disable passwd enumeration, in
// which case primary-group-only members of a directory group are not found —
// target such users as secondary members of the group.
func SystemGroupMembers(group string) []string {
	out, err := exec.Command("getent", "group", group).Output() //nolint:gosec // G204: group comes from validated policy content
	if err != nil {
		return nil
	}
	// Format: name:x:gid:member1,member2
	fields := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(fields) < 4 {
		return nil
	}
	seen := map[string]struct{}{}
	var members []string
	add := func(u string) {
		if u == "" {
			return
		}
		if _, dup := seen[u]; !dup {
			seen[u] = struct{}{}
			members = append(members, u)
		}
	}
	for _, m := range strings.Split(fields[3], ",") {
		add(m)
	}
	if gid := fields[2]; gid != "" {
		for _, u := range primaryGroupMembers(gid) {
			add(u)
		}
	}
	return members
}

// primaryGroupMembers returns users whose primary gid matches, via getent
// passwd (name:x:uid:gid:...).
func primaryGroupMembers(gid string) []string {
	out, err := exec.Command("getent", "passwd").Output()
	if err != nil {
		return nil
	}
	var users []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 4 && f[3] == gid {
			users = append(users, f[0])
		}
	}
	return users
}

// debianCommonAccount is the shared account stack pam-auth-update manages.
const debianCommonAccount = "/etc/pam.d/common-account"

// pamTimeEnabledDebian reports whether pam_time is already in the shared
// account stack, so re-running pam-auth-update (which rewrites the PAM config
// files) is skipped on every routine sync.
func pamTimeEnabledDebian() bool {
	data, err := os.ReadFile(debianCommonAccount)
	if err != nil {
		return false
	}
	profileOK := false
	if cur, err := os.ReadFile(pamConfigProfilePath); err == nil {
		profileOK = string(cur) == pamConfigProfile
	}
	return profileOK && strings.Contains(string(data), "pam_time.so")
}

// EnablePamTimeDebian installs the pam-auth-update profile and enables it,
// adding pam_time to the shared account stack. Idempotent, and a no-op when
// the stack already has it — PAM config is only rewritten when needed.
func EnablePamTimeDebian() error {
	if pamTimeEnabledDebian() {
		return nil
	}
	if err := os.WriteFile(pamConfigProfilePath, []byte(pamConfigProfile), 0o644); err != nil { //nolint:gosec // G306: pam-configs are world-readable by design
		return fmt.Errorf("write pam-config profile: %w", err)
	}
	cmd := exec.Command("pam-auth-update", "--package", "--enable", "bor-session-access")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pam-auth-update --enable: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DisablePamTimeDebian removes the profile from the account stack and deletes
// it. Idempotent — a missing profile is not an error.
func DisablePamTimeDebian() error {
	if _, err := os.Stat(pamConfigProfilePath); err == nil {
		cmd := exec.Command("pam-auth-update", "--package", "--remove", "bor-session-access")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pam-auth-update --remove: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if err := os.Remove(pamConfigProfilePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove pam-config profile: %w", err)
		}
	}
	return nil
}

// WriteTimeConf merges lines into the managed block of /etc/security/time.conf
// and writes it atomically, preserving admin content and the file mode. An
// empty lines slice strips the managed block. As a final guard rail it rejects
// any managed line that fails the self-check (e.g. a "root" or "*" target).
func WriteTimeConf(lines []string) error {
	return writeTimeConfAt(TimeConfPath, lines)
}

func writeTimeConfAt(path string, lines []string) error {
	for _, l := range lines {
		if !timeConfLineRE.MatchString(l) || strings.Contains(l, ";root;") {
			return fmt.Errorf("refusing to write unsafe time.conf line: %q", l)
		}
	}

	existing, err := os.ReadFile(path) //nolint:gosec // G304: fixed system path
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	merged, err := MergeTimeConf(string(existing), lines)
	if err != nil {
		return err
	}

	// Nothing to do if the file already matches (avoids needless watcher churn).
	if string(existing) == merged {
		return nil
	}

	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".time.conf-bor-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(merged); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}
