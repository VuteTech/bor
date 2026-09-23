// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The initramfs is the dangerous part: Bor never touches the
// bootloader, never removes crypttab lines, and never rebuilds the
// initramfs unless the policy says MANAGE. VERIFY_ONLY (the default) only
// checks and reports, with the exact remedy in the message.

// InitramfsCheck is the verification result of one volume's boot support.
type InitramfsCheck struct {
	// Status: "ok" | "missing:tpm2-tss" | "missing:clevis" | "unknown".
	Status string
	// Missing lists the human-readable remedies.
	Missing []string
}

// rebuild rate limit: a tamper loop must not become a rebuild storm.
var (
	rebuildMu       sync.Mutex
	lastRebuild     time.Time
	rebuildMinDelay = 10 * time.Minute
)

// VerifyInitramfs checks whether the current initramfs can unlock with the
// desired protectors at boot (dracut systems: lsinitrd contents, crypttab
// options, kernel cmdline).
func VerifyInitramfs(ctx context.Context, wantTPM2, wantTang, tangOnRoot bool, mappingNames []string) *InitramfsCheck {
	check := &InitramfsCheck{Status: "ok"}
	if !wantTPM2 && !wantTang {
		return check
	}
	if lookPath("lsinitrd") != nil {
		check.Status = "unknown"
		check.Missing = append(check.Missing, "lsinitrd not available; cannot verify the initramfs contents")
		return check
	}
	res, err := runCommand(ctx, &runRequest{
		Name:    "lsinitrd",
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		check.Status = "unknown"
		check.Missing = append(check.Missing, "lsinitrd failed: "+LastLine(res.Stderr))
		return check
	}

	if wantTPM2 && !strings.Contains(res.Stdout, "libcryptsetup-token-systemd-tpm2.so") {
		check.Status = "missing:tpm2-tss"
		check.Missing = append(check.Missing,
			`the initramfs lacks the systemd TPM2 token module; add add_dracutmodules+=" tpm2-tss " (requires tpm2-tools) and rebuild, or set initramfs_mode MANAGE`)
	}
	if wantTang && !strings.Contains(res.Stdout, "clevis") {
		if check.Status == "ok" {
			check.Status = "missing:clevis"
		}
		check.Missing = append(check.Missing,
			"the initramfs lacks the Clevis module; install clevis-dracut and rebuild")
	}

	// crypttab option tokens for the managed volumes.
	if wantTPM2 {
		for _, name := range mappingNames {
			if !crypttabHasOption(rooted(strings.TrimPrefix(CrypttabPath, "/")), name, "tpm2-device=auto") {
				check.Missing = append(check.Missing,
					fmt.Sprintf("crypttab entry %q lacks tpm2-device=auto", name))
				if check.Status == "ok" {
					check.Status = "missing:tpm2-tss"
				}
			}
		}
	}
	if wantTang && tangOnRoot && !cmdlineHasNeednet() {
		check.Missing = append(check.Missing,
			"Tang protects the root volume but the kernel cmdline lacks rd.neednet=1 (dracut --hostonly-cmdline adds it on rebuild)")
		if check.Status == "ok" {
			check.Status = "missing:clevis"
		}
	}
	return check
}

// cmdlineHasNeednet checks the running kernel cmdline for rd.neednet=1.
func cmdlineHasNeednet() bool {
	data, err := os.ReadFile(rooted("proc/cmdline"))
	if err != nil {
		return false
	}
	for _, f := range strings.Fields(string(data)) {
		if f == "rd.neednet=1" {
			return true
		}
	}
	return false
}

// DracutDropInContent renders the Bor-managed dracut drop-in (MANAGE mode).
func DracutDropInContent(wantTPM2, tangOnRoot bool) string {
	var b strings.Builder
	b.WriteString("# Managed by Bor (DiskEncryption policy) - do not edit.\n")
	b.WriteString("# Enables boot-time unlocking for the protectors the policy configures.\n")
	if wantTPM2 {
		b.WriteString("add_dracutmodules+=\" tpm2-tss \"\n")
	}
	if tangOnRoot {
		// Detects Tang bindings and adds rd.neednet=1 to the cmdline.
		b.WriteString("hostonly_cmdline=\"yes\"\n")
	}
	return b.String()
}

// WriteDracutDropIn writes (or removes, when content is empty) the drop-in.
// Returns whether the file changed.
func WriteDracutDropIn(content string) (bool, error) {
	path := rooted(strings.TrimPrefix(DracutDropInPath, "/"))
	if content == "" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false, nil
		}
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("luks: remove dracut drop-in: %w", err)
		}
		return true, nil
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content { //nolint:gosec // fixed path
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: /etc/dracut.conf.d is world-readable by convention
		return false, fmt.Errorf("luks: create dracut.conf.d: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // world-readable config, no secrets
		return false, fmt.Errorf("luks: write dracut drop-in: %w", err)
	}
	return true, nil
}

// EnsureCrypttabOptions appends missing option tokens to the crypttab line
// of one mapping - a minimal in-place edit that preserves every other byte
// of the file and never rewrites other lines. Returns whether the
// file changed.
func EnsureCrypttabOptions(path, mappingName string, options []string) (bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // fixed system path (test-rooted)
	if err != nil {
		return false, fmt.Errorf("luks: read crypttab: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || fields[0] != mappingName {
			continue
		}
		// crypttab: name device [keyfile [options]]
		for len(fields) < 3 {
			fields = append(fields, "none")
		}
		var opts []string
		if len(fields) >= 4 && fields[3] != "" {
			opts = strings.Split(fields[3], ",")
		}
		lineChanged := false
		for _, want := range options {
			if !hasCrypttabOption(opts, want) {
				opts = append(opts, want)
				lineChanged = true
			}
		}
		if !lineChanged {
			continue
		}
		if len(fields) >= 4 {
			fields[3] = strings.Join(opts, ",")
			fields = fields[:4]
		} else {
			fields = append(fields[:3], strings.Join(opts, ","))
		}
		lines[i] = strings.Join(fields, " ")
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil { //nolint:gosec // crypttab is world-readable by convention
		return false, fmt.Errorf("luks: write crypttab: %w", err)
	}
	return true, nil
}

// crypttabHasOption checks one mapping's options for a token.
func crypttabHasOption(path, mappingName, option string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // fixed system path (test-rooted)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 || fields[0] != mappingName {
			continue
		}
		return hasCrypttabOption(strings.Split(fields[3], ","), option)
	}
	return false
}

func hasCrypttabOption(opts []string, want string) bool {
	for _, o := range opts {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}

// RebuildInitramfs runs `dracut -f --regenerate-all`, rate-limited so a
// tamper loop cannot become a rebuild storm.
func RebuildInitramfs(ctx context.Context) error {
	rebuildMu.Lock()
	if time.Since(lastRebuild) < rebuildMinDelay {
		rebuildMu.Unlock()
		return fmt.Errorf("luks: initramfs rebuild rate-limited (last run %s ago)", time.Since(lastRebuild).Round(time.Second))
	}
	lastRebuild = time.Now()
	rebuildMu.Unlock()

	_, err := runCommand(ctx, &runRequest{
		Name:    "dracut",
		Args:    []string{"-f", "--regenerate-all"},
		Timeout: dracutTimeout,
	})
	if err != nil {
		return fmt.Errorf("luks: rebuild initramfs: %w", err)
	}
	return nil
}
