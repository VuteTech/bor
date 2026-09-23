// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// fsRoot prefixes every absolute sysfs/procfs/etc path so tests can point the
// package at a synthetic tree.
var fsRoot = "/"

func rooted(path string) string {
	return filepath.Join(fsRoot, path)
}

// efiSecureBootVar is the efivars SecureBoot variable: 4 attribute
// bytes + 1 value byte.
const efiSecureBootVar = "sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
const efiSetupModeVar = "sys/firmware/efi/efivars/SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c"

// Platform holds the per-node facts of DiskEncryptionPlatform.
type Platform struct {
	TPM2Present        bool
	SecureBoot         pb.SecureBootState
	InitramfsGenerator string // dracut | initramfs-tools | mkinitcpio | rpm-ostree | unknown
	SystemdVersion     string
	CryptsetupVersion  string
	ClevisVersion      string // "" = not installed
}

// CollectPlatform reads the platform facts (sysfs, efivars, tool probes).
func CollectPlatform(ctx context.Context, cfg *Config) *Platform {
	p := &Platform{
		TPM2Present:        TPM2Present(),
		SecureBoot:         SecureBootState(),
		InitramfsGenerator: DetectInitramfsGenerator(),
	}
	p.SystemdVersion = systemdVersion(ctx)
	p.CryptsetupVersion = cryptsetupVersion(ctx, cfg.CryptsetupBinary())
	p.ClevisVersion = clevisVersion(ctx, cfg.ClevisBinary())
	return p
}

// TPM2Present reports whether a TPM 2.0 with a resource manager exists
// .
func TPM2Present() bool {
	data, err := os.ReadFile(rooted("sys/class/tpm/tpm0/tpm_version_major"))
	if err != nil || strings.TrimSpace(string(data)) != "2" {
		return false
	}
	if _, err := os.Stat(rooted("dev/tpmrm0")); err != nil {
		return false
	}
	return true
}

// SecureBootState reads the efivars SecureBoot/SetupMode state.
func SecureBootState() pb.SecureBootState {
	if _, err := os.Stat(rooted("sys/firmware/efi")); err != nil {
		return pb.SecureBootState_SECURE_BOOT_STATE_LEGACY_BIOS
	}
	sb, err := os.ReadFile(rooted(efiSecureBootVar))
	if err != nil || len(sb) < 5 {
		return pb.SecureBootState_SECURE_BOOT_STATE_UNSPECIFIED
	}
	if setup, err := os.ReadFile(rooted(efiSetupModeVar)); err == nil && len(setup) >= 5 && setup[4] == 1 {
		return pb.SecureBootState_SECURE_BOOT_STATE_SETUP_MODE
	}
	if sb[4] == 1 {
		return pb.SecureBootState_SECURE_BOOT_STATE_ENABLED
	}
	return pb.SecureBootState_SECURE_BOOT_STATE_DISABLED
}

// DetectInitramfsGenerator identifies the initramfs tooling. The generator
// decides which automatic protectors can work at boot.
func DetectInitramfsGenerator() string {
	// rpm-ostree systems (Fedora Atomic) manage the initramfs in the image.
	if _, err := os.Stat(rooted("run/ostree-booted")); err == nil {
		return "rpm-ostree"
	}
	if lookPath("dracut") == nil {
		return "dracut"
	}
	if lookPath("update-initramfs") == nil {
		return "initramfs-tools"
	}
	if lookPath("mkinitcpio") == nil {
		return "mkinitcpio"
	}
	return "unknown"
}

// ExternallyManagedBy detects vendor FDE stacks that own the TPM enrollment
// on this node. Bor must not fight them; the TPM protector is
// reported INAPPLICABLE with the manager's name.
func ExternallyManagedBy() string {
	// Ubuntu TPM-backed FDE: snapd/secboot owns sealing, recovery keys live
	// in Landscape.
	if lookPath("snap-tpmctl") == nil {
		return "snapd"
	}
	// openSUSE sdbootutil FDE: systemd-cryptenroll + pcrlock orchestrated by
	// sdbootutil, re-enrolled on updates.
	if lookPath("sdbootutil") == nil && pcrlockPresent() {
		return "sdbootutil"
	}
	// systemd-pcrlock policies set up by other tooling: cryptenroll would
	// pick pcrlock.json up implicitly, so the TPM is not Bor's.
	if pcrlockPresent() {
		return "pcrlock"
	}
	// Signed PCR policies (UKI): tpm2-pcr-public-key.pem is used
	// automatically when present.
	for _, dir := range []string{"etc/systemd", "run/systemd", "usr/lib/systemd"} {
		if _, err := os.Stat(rooted(filepath.Join(dir, "tpm2-pcr-public-key.pem"))); err == nil {
			return "signed-pcr"
		}
	}
	return ""
}

// pcrlockPresent reports whether a systemd-pcrlock policy exists.
func pcrlockPresent() bool {
	for _, p := range []string{"run/systemd/pcrlock.json", "var/lib/systemd/pcrlock.json"} {
		if _, err := os.Stat(rooted(p)); err == nil {
			return true
		}
	}
	return false
}

var versionNumberRE = regexp.MustCompile(`\d+(\.\d+)*`)

// systemdVersion returns the systemd version number ("257").
func systemdVersion(ctx context.Context) string {
	return probeVersion(ctx, "systemctl", []string{"--version"})
}

// cryptsetupVersion returns the cryptsetup version number ("2.8.7").
func cryptsetupVersion(ctx context.Context, binary string) string {
	return probeVersion(ctx, binary, []string{"--version"})
}

// clevisVersion returns the clevis version, or "" when not installed.
// `clevis --help` prints "Usage: clevis COMMAND [OPTIONS]" without a version
// on some builds, so the luks subcommand list presence is the install signal
// and the version is best-effort.
func clevisVersion(ctx context.Context, binary string) string {
	if lookPath(binary) != nil {
		return ""
	}
	if v := probeVersion(ctx, binary, []string{"--version"}); v != "" {
		return v
	}
	return "installed"
}

// probeVersion runs a binary and extracts the first version-looking number
// from its first output line. Returns "" when the binary is missing.
func probeVersion(ctx context.Context, binary string, args []string) string {
	if binary == "" || lookPath(binary) != nil {
		return ""
	}
	res, err := runCommand(ctx, &runRequest{Name: binary, Args: args})
	out := ""
	if res != nil {
		out = res.Stdout + "\n" + res.Stderr
	}
	if err != nil && out == "" {
		return ""
	}
	firstLine := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	return versionNumberRE.FindString(firstLine)
}

// ToProto converts the platform facts to the wire representation.
func (p *Platform) ToProto() *pb.DiskEncryptionPlatform {
	return &pb.DiskEncryptionPlatform{
		Tpm2Present:        p.TPM2Present,
		SecureBoot:         p.SecureBoot,
		InitramfsGenerator: p.InitramfsGenerator,
		SystemdVersion:     p.SystemdVersion,
		CryptsetupVersion:  p.CryptsetupVersion,
		ClevisVersion:      p.ClevisVersion,
	}
}
