// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/macrules"
	"google.golang.org/protobuf/encoding/protojson"
)

// Limits for an AppArmor policy.
const (
	maxApparmorProfiles  = 500
	maxApparmorOverrides = 64
	maxApparmorHomeDirs  = 16
)

// ValidateApparmorContent validates AppArmor policy JSON content: profile
// matches and modes, the protected list, custom profile and override
// text, home directories, duplicates and limits.
func ValidateApparmorContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("AppArmor policy content is empty")
	}
	var pol pb.ApparmorPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid AppArmor policy: %w", err)
	}
	if !apparmorPolicyHasSettings(&pol) {
		return fmt.Errorf("AppArmor policy does not change anything: set a requirement, a restriction, a profile mode, a custom profile, a local override or a home directory")
	}
	for _, sw := range []pb.ApparmorSwitch{pol.GetRestrictUnprivilegedUserns(), pol.GetRestrictUnprivilegedUnconfined()} {
		if _, ok := pb.ApparmorSwitch_name[int32(sw)]; !ok {
			return fmt.Errorf("unknown switch value %d", sw)
		}
	}

	if n := len(pol.GetProfiles()); n > maxApparmorProfiles {
		return fmt.Errorf("AppArmor policy has %d profile entries; the limit is %d", n, maxApparmorProfiles)
	}
	seen := make(map[string]struct{})
	for i, p := range pol.GetProfiles() {
		if err := macrules.ValidateProfileMatch(p.GetMatch()); err != nil {
			return fmt.Errorf("profile entry %d: %w", i+1, err)
		}
		switch p.GetMode() {
		case pb.ApparmorMode_APPARMOR_MODE_ENFORCE, pb.ApparmorMode_APPARMOR_MODE_COMPLAIN:
		case pb.ApparmorMode_APPARMOR_MODE_DISABLED:
			if macrules.IsGlob(p.GetMatch()) {
				return fmt.Errorf("profile pattern %q: profiles are disabled one by one, not by pattern", p.GetMatch())
			}
			if macrules.IsProtectedProfile(p.GetMatch()) {
				return fmt.Errorf("profile %q cannot be disabled: the user namespace restriction depends on it", p.GetMatch())
			}
			if p.GetEditVendorFlags() {
				return fmt.Errorf("profile %q: editing the vendor flags applies to enforce and complain only", p.GetMatch())
			}
		case pb.ApparmorMode_APPARMOR_MODE_UNSPECIFIED:
			return fmt.Errorf("profile entry %q has no mode", p.GetMatch())
		default:
			return fmt.Errorf("profile entry %q: unknown mode %d", p.GetMatch(), p.GetMode())
		}
		if strings.HasPrefix(p.GetMatch(), macrules.BorPrefix) {
			return fmt.Errorf("profile %q is a Bor profile; its mode is part of its text", p.GetMatch())
		}
		if _, dup := seen[p.GetMatch()]; dup {
			return fmt.Errorf("profile entry %q is listed twice", p.GetMatch())
		}
		seen[p.GetMatch()] = struct{}{}
	}

	if n := len(pol.GetCustomProfiles()); n > macrules.MaxCustomItems {
		return fmt.Errorf("AppArmor policy has %d custom profiles; the limit is %d", n, macrules.MaxCustomItems)
	}
	seen = make(map[string]struct{})
	for i, cp := range pol.GetCustomProfiles() {
		if err := macrules.ValidateCustomName(cp.GetName()); err != nil {
			return fmt.Errorf("custom profile %d: %w", i+1, err)
		}
		if _, dup := seen[cp.GetName()]; dup {
			return fmt.Errorf("custom profile %q is listed twice", cp.GetName())
		}
		seen[cp.GetName()] = struct{}{}
		if err := macrules.CheckProfileText(cp.GetName(), cp.GetText()); err != nil {
			return fmt.Errorf("custom profile %q: %w", cp.GetName(), err)
		}
		if len(cp.GetDescription()) > macrules.MaxDescriptionLength {
			return fmt.Errorf("custom profile %q: description is longer than %d characters", cp.GetName(), macrules.MaxDescriptionLength)
		}
	}

	if n := len(pol.GetLocalOverrides()); n > maxApparmorOverrides {
		return fmt.Errorf("AppArmor policy has %d local overrides; the limit is %d", n, maxApparmorOverrides)
	}
	seen = make(map[string]struct{})
	for i, o := range pol.GetLocalOverrides() {
		if err := macrules.ValidateProfileFile(o.GetProfileFile()); err != nil {
			return fmt.Errorf("local override %d: %w", i+1, err)
		}
		if strings.HasPrefix(o.GetProfileFile(), macrules.BorPrefix) {
			return fmt.Errorf("local override %q: a Bor profile is edited as a custom profile", o.GetProfileFile())
		}
		if _, dup := seen[o.GetProfileFile()]; dup {
			return fmt.Errorf("local override %q is listed twice", o.GetProfileFile())
		}
		seen[o.GetProfileFile()] = struct{}{}
		if err := macrules.CheckLocalOverrideText(o.GetText()); err != nil {
			return fmt.Errorf("local override %q: %w", o.GetProfileFile(), err)
		}
	}

	if n := len(pol.GetExtraHomeDirs()); n > maxApparmorHomeDirs {
		return fmt.Errorf("AppArmor policy has %d home directories; the limit is %d", n, maxApparmorHomeDirs)
	}
	seen = make(map[string]struct{})
	for i, d := range pol.GetExtraHomeDirs() {
		if err := macrules.ValidateHomeDir(d); err != nil {
			return fmt.Errorf("home directory %d: %w", i+1, err)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("home directory %q is listed twice", d)
		}
		seen[d] = struct{}{}
	}
	return nil
}

func apparmorPolicyHasSettings(pol *pb.ApparmorPolicy) bool {
	return pol.GetRequireEnabled() || pol.GetEnsureService() ||
		pol.GetRestrictUnprivilegedUserns() != pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED ||
		pol.GetRestrictUnprivilegedUnconfined() != pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED ||
		len(pol.GetProfiles()) > 0 || len(pol.GetCustomProfiles()) > 0 ||
		len(pol.GetLocalOverrides()) > 0 || len(pol.GetExtraHomeDirs()) > 0
}

// ApparmorContentHasCustom reports whether an AppArmor policy carries
// administrator-authored policy text: custom profiles or local overrides.
// Such saves and releases are audited.
func ApparmorContentHasCustom(content string) bool {
	var pol pb.ApparmorPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return false
	}
	return len(pol.GetCustomProfiles()) > 0 || len(pol.GetLocalOverrides()) > 0
}

// ApparmorCustomSummary describes the custom content of an AppArmor policy
// for the audit log: profile and override names with a SHA-256 of their
// text.
func ApparmorCustomSummary(content string) string {
	var pol pb.ApparmorPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return "{}"
	}
	type item struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	}
	summary := struct {
		Profiles  []item `json:"profiles"`
		Overrides []item `json:"local_overrides"`
	}{Profiles: []item{}, Overrides: []item{}}
	for _, p := range pol.GetCustomProfiles() {
		sum := sha256.Sum256([]byte(p.GetText()))
		summary.Profiles = append(summary.Profiles, item{Name: macrules.ProfileFileName(p.GetName()), SHA256: hex.EncodeToString(sum[:])})
	}
	for _, o := range pol.GetLocalOverrides() {
		sum := sha256.Sum256([]byte(o.GetText()))
		summary.Overrides = append(summary.Overrides, item{Name: o.GetProfileFile(), SHA256: hex.EncodeToString(sum[:])})
	}
	out, err := json.Marshal(summary)
	if err != nil {
		return "{}"
	}
	return string(out)
}

// MacContentHasCustom reports whether a policy of either mandatory access
// control type carries administrator-authored policy text.
func MacContentHasCustom(policyType, content string) bool {
	switch policyType {
	case "Selinux":
		return SelinuxContentHasCustom(content)
	case "Apparmor":
		return ApparmorContentHasCustom(content)
	}
	return false
}

// MacCustomSummary is the audit detail for MacContentHasCustom.
func MacCustomSummary(policyType, content string) string {
	switch policyType {
	case "Selinux":
		return SelinuxCustomSummary(content)
	case "Apparmor":
		return ApparmorCustomSummary(content)
	}
	return "{}"
}
