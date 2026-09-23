// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// tangPinConfig builds the Clevis pin name and JSON config for the desired
// Tang servers, always with encoding/json (never string concatenation) and
// always with a pinned thumbprint - `-y` would disable the thumbprint check
// entirely and is never passed.
func tangPinConfig(servers []*pb.TangServer, threshold uint32) (pin, cfg string, err error) {
	if len(servers) == 0 {
		return "", "", fmt.Errorf("luks: tang binding needs at least one server")
	}
	type tangCfg struct {
		URL string `json:"url"`
		Thp string `json:"thp"`
	}
	if len(servers) == 1 {
		b, marshalErr := json.Marshal(tangCfg{URL: servers[0].GetUrl(), Thp: servers[0].GetThumbprint()})
		if marshalErr != nil {
			return "", "", marshalErr
		}
		return "tang", string(b), nil
	}
	if threshold == 0 {
		threshold = 1
	}
	pins := make([]tangCfg, 0, len(servers))
	for _, s := range servers {
		pins = append(pins, tangCfg{URL: s.GetUrl(), Thp: s.GetThumbprint()})
	}
	b, err := json.Marshal(map[string]interface{}{
		"t":    threshold,
		"pins": map[string]interface{}{"tang": pins},
	})
	if err != nil {
		return "", "", err
	}
	return "sss", string(b), nil
}

// ClevisBind binds a new Clevis slot for the desired Tang servers. The
// existing credential goes in on stdin (`-k -`); with `thp` set there is no
// prompt, and any unexpected prompt reads /dev/tty, which the daemon lacks -
// it fails safely. TMPDIR points at the root-only tmpfs run
// directory because Clevis writes a full LUKS header backup there during a
// bind. Returns the new slot index.
func ClevisBind(ctx context.Context, cfg *Config, device string, credential []byte, servers []*pb.TangServer, threshold uint32) (int, error) {
	pin, pinCfg, err := tangPinConfig(servers, threshold)
	if err != nil {
		Zero(credential)
		return -1, err
	}
	if dirErr := ensureRunDir(cfg); dirErr != nil {
		Zero(credential)
		return -1, dirErr
	}

	before, err := DumpHeader(ctx, cfg.CryptsetupBinary(), device, "")
	if err != nil {
		Zero(credential)
		return -1, err
	}
	occupied := map[int]bool{}
	for _, ks := range before.Keyslots {
		occupied[ks.Index] = true
	}

	_, err = runCommand(ctx, &runRequest{
		Name:  cfg.ClevisBinary(),
		Args:  []string{"luks", "bind", "-k", "-", "-d", device, pin, pinCfg},
		Stdin: credential,
		Env:   []string{"TMPDIR=" + cfg.RunDirPath()},
	})
	cleanRunDir(cfg)
	if err != nil {
		return -1, fmt.Errorf("luks: clevis bind on %s: %w", device, err)
	}

	after, err := DumpHeader(ctx, cfg.CryptsetupBinary(), device, "")
	if err != nil {
		return -1, err
	}
	for _, ks := range after.Keyslots {
		if !occupied[ks.Index] && ks.Kind == pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS {
			return ks.Index, nil
		}
	}
	return -1, fmt.Errorf("luks: clevis bind on %s succeeded but no new clevis slot found", device)
}

// ClevisUnbind removes a Clevis binding (luksKillSlot + token remove).
func ClevisUnbind(ctx context.Context, cfg *Config, device string, slot int) error {
	_, err := runCommand(ctx, &runRequest{
		Name: cfg.ClevisBinary(),
		Args: []string{"luks", "unbind", "-d", device, "-s", strconv.Itoa(slot), "-f"},
	})
	if err != nil {
		return fmt.Errorf("luks: clevis unbind slot %d on %s: %w", slot, device, err)
	}
	return nil
}

// ClevisPass decrypts a Clevis slot's passphrase through its pin (release
// v16) - a usable unlock credential while Tang is reachable, and the
// verification step after every bind. The caller must Zero the result.
func ClevisPass(ctx context.Context, cfg *Config, device string, slot int) ([]byte, error) {
	res, err := runCommand(ctx, &runRequest{
		Name: cfg.ClevisBinary(),
		Args: []string{"luks", "pass", "-d", device, "-s", strconv.Itoa(slot)},
		Env:  []string{"TMPDIR=" + cfg.RunDirPath()},
	})
	if err != nil {
		return nil, fmt.Errorf("luks: clevis pass for slot %d on %s: %w", slot, device, err)
	}
	// The passphrase is printed verbatim; strip at most one trailing newline.
	pass := strings.TrimSuffix(res.Stdout, "\n")
	if pass == "" {
		return nil, fmt.Errorf("luks: clevis pass for slot %d on %s returned nothing", slot, device)
	}
	return []byte(pass), nil
}

// ClevisAvailable reports whether the clevis luks tooling is installed.
// Unlike application-management types, its absence under a Tang policy is
// NON_COMPLIANT, not INAPPLICABLE: a required security control is missing.
func ClevisAvailable(cfg *Config) bool {
	return lookPath(cfg.ClevisBinary()) == nil
}

// TangBindingCurrent reports whether an existing Clevis binding matches the
// desired server set: same URLs, and every preferred signing thumbprint
// present in the advertisement embedded at bind time. A binding made before
// a Tang key rotation is stale and gets re-bound (bind new -> verify ->
// unbind stale; never `clevis luks regen`, which trusts blindly).
func TangBindingCurrent(slot *Keyslot, servers []*pb.TangServer, threshold uint32) bool {
	if slot.Kind != pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS {
		return false
	}
	wantPin := "tang"
	wantThreshold := uint32(0)
	if len(servers) > 1 {
		wantPin = "sss"
		wantThreshold = threshold
		if wantThreshold == 0 {
			wantThreshold = 1
		}
	}
	if slot.ClevisPin != wantPin {
		return false
	}
	if wantPin == "sss" && slot.ClevisThreshold != wantThreshold {
		return false
	}
	wantURLs := make([]string, 0, len(servers))
	for _, s := range servers {
		wantURLs = append(wantURLs, s.GetUrl())
	}
	slices.Sort(wantURLs)
	if !slices.Equal(wantURLs, slot.TangURLs) {
		return false
	}
	for _, s := range servers {
		if !slices.Contains(slot.TangSigningThumbprints, s.GetThumbprint()) {
			return false
		}
	}
	return true
}

// ensureRunDir creates the root-only tmpfs work directory.
func ensureRunDir(cfg *Config) error {
	if err := os.MkdirAll(cfg.RunDirPath(), 0o700); err != nil {
		return fmt.Errorf("luks: create run dir: %w", err)
	}
	return nil
}

// cleanRunDir removes everything inside the run directory (Clevis header
// backups must not outlive the run).
func cleanRunDir(cfg *Config) {
	entries, err := os.ReadDir(cfg.RunDirPath())
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(cfg.RunDirPath() + "/" + e.Name())
	}
}
