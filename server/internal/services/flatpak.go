// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// Identifier rules shared with the agent (agent/internal/policy/flatpak.go).
// Every value that ends up on a flatpak command line or in a file name must
// satisfy one of these.
var (
	flatpakRemoteNameRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	flatpakAppIDRE       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{5,254}$`)
	flatpakBranchRE      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	flatpakSubsetRE      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	flatpakFilterRefRE   = regexp.MustCompile(`^[A-Za-z0-9._*/-]{1,256}$`)
	flatpakCollectionRE  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	flatpakCommitRE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	flatpakLanguageRE    = regexp.MustCompile(`^[a-z]{2,3}(_[A-Z]{2})?$`)
	flatpakKnownSubsets  = map[string]struct{}{"verified": {}, "floss": {}, "verified_floss": {}}
	flatpakMaxGPGKeyLen  = 64 * 1024
	flatpakMaxDescLen    = 512
	flatpakMinIntervalH  = int32(1)
	flatpakMaxIntervalH  = int32(168)
	flatpakMinTimeoutMin = int32(5)
	flatpakMaxTimeoutMin = int32(240)
)

// ValidateFlatpakContent validates a FlatpakPolicy JSON string for release.
func ValidateFlatpakContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("flatpak policy content is empty")
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var pol pb.FlatpakPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid flatpak policy: %w", err)
	}

	if len(pol.GetRemotes()) == 0 && len(pol.GetApps()) == 0 {
		return fmt.Errorf("policy must contain at least one remote or one application")
	}

	if inst := pol.GetInstallation(); inst != "" && !flatpakRemoteNameRE.MatchString(inst) {
		return fmt.Errorf("invalid installation name %q: must match [A-Za-z0-9][A-Za-z0-9._-]* and be at most 64 characters", inst)
	}
	if h := pol.GetAutoUpdateIntervalHours(); h != 0 && (h < flatpakMinIntervalH || h > flatpakMaxIntervalH) {
		return fmt.Errorf("auto_update_interval_hours must be between %d and %d (got %d)", flatpakMinIntervalH, flatpakMaxIntervalH, h)
	}
	if m := pol.GetOperationTimeoutMinutes(); m != 0 && (m < flatpakMinTimeoutMin || m > flatpakMaxTimeoutMin) {
		return fmt.Errorf("operation_timeout_minutes must be between %d and %d (got %d)", flatpakMinTimeoutMin, flatpakMaxTimeoutMin, m)
	}
	for _, l := range append(append([]string{}, pol.GetLanguages()...), pol.GetExtraLanguages()...) {
		if !flatpakLanguageRE.MatchString(l) {
			return fmt.Errorf("invalid language code %q (expected e.g. \"en\" or \"pt_BR\")", l)
		}
	}

	seenRemotes := make(map[string]bool)
	for i, r := range pol.GetRemotes() {
		prefix := fmt.Sprintf("remote[%d]", i)
		if err := validateFlatpakRemote(r); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		if seenRemotes[r.GetName()] {
			return fmt.Errorf("%s: duplicate remote name %q", prefix, r.GetName())
		}
		seenRemotes[r.GetName()] = true
	}

	seenApps := make(map[string]bool)
	for i, a := range pol.GetApps() {
		prefix := fmt.Sprintf("app[%d]", i)
		if err := validateFlatpakApp(a); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		key := a.GetAppId() + "|" + a.GetScope().String()
		if seenApps[key] {
			return fmt.Errorf("%s: duplicate application %q for scope %s", prefix, a.GetAppId(), a.GetScope().String())
		}
		seenApps[key] = true
	}
	return nil
}

func validateFlatpakRemote(r *pb.FlatpakRemoteEntry) error {
	name := r.GetName()
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if !flatpakRemoteNameRE.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid remote name %q: must match [A-Za-z0-9][A-Za-z0-9._-]* and be at most 64 characters", name)
	}
	if err := validateFlatpakRemoteURL(r.GetUrl()); err != nil {
		return err
	}
	if len(r.GetGpgKeyData()) > flatpakMaxGPGKeyLen {
		return fmt.Errorf("gpg_key_data exceeds %d bytes", flatpakMaxGPGKeyLen)
	}
	if r.GetGpgVerify() && len(r.GetGpgKeyData()) == 0 {
		return fmt.Errorf("gpg_verify is enabled but no gpg_key_data was provided for remote %q", name)
	}
	if id := r.GetGpgKeyId(); id != "" && !gpgFingerprintRe.MatchString(id) {
		return fmt.Errorf("gpg_key_id must be a 40-character hex fingerprint (got %d chars)", len(id))
	}
	if s := r.GetSubset(); s != "" {
		if _, known := flatpakKnownSubsets[s]; !known && !flatpakSubsetRE.MatchString(s) {
			return fmt.Errorf("invalid subset %q", s)
		}
	}
	if b := r.GetDefaultBranch(); b != "" && !flatpakBranchRE.MatchString(b) {
		return fmt.Errorf("invalid default_branch %q", b)
	}
	if p := r.GetPriority(); p < 0 || p > 1000 {
		return fmt.Errorf("priority must be between 0 and 1000 (got %d)", p)
	}
	if c := r.GetCollectionId(); c != "" && !flatpakCollectionRE.MatchString(c) {
		return fmt.Errorf("invalid collection_id %q", c)
	}
	if len(r.GetTitle()) > flatpakMaxDescLen || len(r.GetComment()) > flatpakMaxDescLen || len(r.GetHomepage()) > 2048 {
		return fmt.Errorf("title, comment or homepage is too long")
	}
	switch r.GetFilterMode() {
	case pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_NONE:
		if len(r.GetFilterRefs()) > 0 {
			return fmt.Errorf("filter_refs given but filter_mode is NONE")
		}
	case pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_ALLOWLIST, pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_DENYLIST:
		for _, ref := range r.GetFilterRefs() {
			if !flatpakFilterRefRE.MatchString(ref) || strings.Contains(ref, "..") {
				return fmt.Errorf("invalid filter ref %q", ref)
			}
		}
	default:
		return fmt.Errorf("unknown filter_mode %d", int32(r.GetFilterMode()))
	}
	return nil
}

func validateFlatpakRemoteURL(u string) error {
	if u == "" {
		return fmt.Errorf("url is required")
	}
	if len(u) > 2048 {
		return fmt.Errorf("url is too long")
	}
	lower := strings.ToLower(u)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "oci+https://") {
		return fmt.Errorf("invalid url %q: only https:// and oci+https:// remotes are allowed", u)
	}
	if strings.ContainsAny(u, " \t\r\n\"'") {
		return fmt.Errorf("invalid url %q: contains whitespace or quotes", u)
	}
	return nil
}

func validateFlatpakApp(a *pb.FlatpakAppEntry) error {
	id := a.GetAppId()
	if id == "" {
		return fmt.Errorf("app_id is required")
	}
	if !flatpakAppIDRE.MatchString(id) || strings.Contains(id, "..") || !strings.Contains(id, ".") {
		return fmt.Errorf("invalid app_id %q: expected a reverse-DNS identifier such as org.mozilla.firefox", id)
	}
	if r := a.GetRemote(); r != "" && (!flatpakRemoteNameRE.MatchString(r) || strings.Contains(r, "..")) {
		return fmt.Errorf("invalid remote name %q", r)
	}
	if b := a.GetBranch(); b != "" && !flatpakBranchRE.MatchString(b) {
		return fmt.Errorf("invalid branch %q", b)
	}
	switch a.GetState() {
	case pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT,
		pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT,
		pb.FlatpakAppState_FLATPAK_APP_STATE_LATEST:
	default:
		return fmt.Errorf("state must be PRESENT, ABSENT or LATEST for %q", id)
	}
	switch a.GetScope() {
	case pb.FlatpakScope_FLATPAK_SCOPE_UNSPECIFIED, pb.FlatpakScope_FLATPAK_SCOPE_SYSTEM, pb.FlatpakScope_FLATPAK_SCOPE_USER:
	default:
		return fmt.Errorf("unknown scope %d for %q", int32(a.GetScope()), id)
	}
	if c := a.GetCommit(); c != "" && !flatpakCommitRE.MatchString(c) {
		return fmt.Errorf("commit must be a 64-character lowercase hex OSTree checksum for %q", id)
	}
	if len(a.GetDisplayName()) > flatpakMaxDescLen {
		return fmt.Errorf("display_name is too long for %q", id)
	}
	return nil
}
