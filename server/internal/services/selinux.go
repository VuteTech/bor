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

// Limits for an SELinux policy. Every value reaches the agent, where it
// becomes a command argument or a line of policy text.
const (
	maxSelinuxBooleans     = 200
	maxSelinuxPermissive   = 64
	maxSelinuxModules      = 200
	maxSelinuxFileContexts = 200
	maxSelinuxLogins       = 200
)

// ValidateSelinuxContent validates SELinux policy JSON content: names,
// the protected list, CIL module text, file context expressions, login
// mappings, duplicates and limits.
func ValidateSelinuxContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("SELinux policy content is empty")
	}
	var pol pb.SelinuxPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid SELinux policy: %w", err)
	}
	if !selinuxPolicyHasSettings(&pol) {
		return fmt.Errorf("SELinux policy does not change anything: set a mode, a requirement, a boolean, an exception, a module, a file context or a login mapping")
	}
	switch pol.GetMode() {
	case pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED, pb.SelinuxMode_SELINUX_MODE_ENFORCING, pb.SelinuxMode_SELINUX_MODE_PERMISSIVE:
	default:
		return fmt.Errorf("unknown SELinux mode %d", pol.GetMode())
	}
	if pol.GetPrepareDisabledNodes() && pol.GetMode() == pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED {
		return fmt.Errorf("preparing disabled nodes needs a mode (enforcing or permissive) the node is brought to")
	}

	if n := len(pol.GetBooleans()); n > maxSelinuxBooleans {
		return fmt.Errorf("SELinux policy has %d booleans; the limit is %d", n, maxSelinuxBooleans)
	}
	seen := make(map[string]struct{})
	for i, b := range pol.GetBooleans() {
		if err := macrules.ValidateBooleanName(b.GetName()); err != nil {
			return fmt.Errorf("boolean %d: %w", i+1, err)
		}
		if macrules.IsProtectedBoolean(b.GetName()) {
			return fmt.Errorf("boolean %q cannot be set by a policy: it would lock the policy against every later change, including Bor's", b.GetName())
		}
		if _, dup := seen[b.GetName()]; dup {
			return fmt.Errorf("boolean %q is listed twice", b.GetName())
		}
		seen[b.GetName()] = struct{}{}
		if len(b.GetDescription()) > macrules.MaxDescriptionLength {
			return fmt.Errorf("boolean %q: description is longer than %d characters", b.GetName(), macrules.MaxDescriptionLength)
		}
	}

	if n := len(pol.GetPermissiveDomains()); n > maxSelinuxPermissive {
		return fmt.Errorf("SELinux policy has %d permissive domains; the limit is %d", n, maxSelinuxPermissive)
	}
	seen = make(map[string]struct{})
	for i, d := range pol.GetPermissiveDomains() {
		if err := macrules.ValidateDomain(d); err != nil {
			return fmt.Errorf("permissive domain %d: %w", i+1, err)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("permissive domain %q is listed twice", d)
		}
		seen[d] = struct{}{}
	}

	if n := len(pol.GetModules()); n > maxSelinuxModules {
		return fmt.Errorf("SELinux policy has %d modules; the limit is %d", n, maxSelinuxModules)
	}
	seen = make(map[string]struct{})
	for i, m := range pol.GetModules() {
		if err := macrules.ValidateModuleName(m.GetName()); err != nil {
			return fmt.Errorf("module %d: %w", i+1, err)
		}
		if strings.HasPrefix(m.GetName(), macrules.BorPrefix) {
			return fmt.Errorf("module %q is a Bor module; manage it through the custom modules", m.GetName())
		}
		if _, dup := seen[m.GetName()]; dup {
			return fmt.Errorf("module %q is listed twice", m.GetName())
		}
		seen[m.GetName()] = struct{}{}
	}

	if n := len(pol.GetCustomModules()); n > macrules.MaxCustomItems {
		return fmt.Errorf("SELinux policy has %d custom modules; the limit is %d", n, macrules.MaxCustomItems)
	}
	seen = make(map[string]struct{})
	for i, m := range pol.GetCustomModules() {
		if err := macrules.ValidateCustomName(m.GetName()); err != nil {
			return fmt.Errorf("custom module %d: %w", i+1, err)
		}
		if _, dup := seen[m.GetName()]; dup {
			return fmt.Errorf("custom module %q is listed twice", m.GetName())
		}
		seen[m.GetName()] = struct{}{}
		if err := macrules.CheckCIL(m.GetCil()); err != nil {
			return fmt.Errorf("custom module %q: %w", m.GetName(), err)
		}
		if len(m.GetDescription()) > macrules.MaxDescriptionLength {
			return fmt.Errorf("custom module %q: description is longer than %d characters", m.GetName(), macrules.MaxDescriptionLength)
		}
	}

	if n := len(pol.GetFileContexts()); n > maxSelinuxFileContexts {
		return fmt.Errorf("SELinux policy has %d file contexts; the limit is %d", n, maxSelinuxFileContexts)
	}
	seen = make(map[string]struct{})
	for i, fc := range pol.GetFileContexts() {
		if _, err := macrules.ValidateFileContextRegex(fc.GetPathRegex()); err != nil {
			return fmt.Errorf("file context %d: %w", i+1, err)
		}
		if err := macrules.ValidateDomain(fc.GetType()); err != nil {
			return fmt.Errorf("file context %q: %w", fc.GetPathRegex(), err)
		}
		if _, ok := pb.SelinuxFileKind_name[int32(fc.GetKind())]; !ok {
			return fmt.Errorf("file context %q: unknown file kind %d", fc.GetPathRegex(), fc.GetKind())
		}
		key := fmt.Sprintf("%s\x00%d", fc.GetPathRegex(), fc.GetKind())
		if _, dup := seen[key]; dup {
			return fmt.Errorf("file context %q is listed twice", fc.GetPathRegex())
		}
		seen[key] = struct{}{}
	}

	if n := len(pol.GetLogins()); n > maxSelinuxLogins {
		return fmt.Errorf("SELinux policy has %d login mappings; the limit is %d", n, maxSelinuxLogins)
	}
	seen = make(map[string]struct{})
	for i, l := range pol.GetLogins() {
		if err := macrules.ValidateLogin(l.GetLogin()); err != nil {
			return fmt.Errorf("login mapping %d: %w", i+1, err)
		}
		if err := macrules.ValidateSeuser(l.GetSeuser()); err != nil {
			return fmt.Errorf("login mapping %q: %w", l.GetLogin(), err)
		}
		if err := macrules.ValidateRange(l.GetRange()); err != nil {
			return fmt.Errorf("login mapping %q: %w", l.GetLogin(), err)
		}
		if _, dup := seen[l.GetLogin()]; dup {
			return fmt.Errorf("login %q is mapped twice", l.GetLogin())
		}
		seen[l.GetLogin()] = struct{}{}
	}
	return nil
}

func selinuxPolicyHasSettings(pol *pb.SelinuxPolicy) bool {
	return pol.GetMode() != pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED ||
		pol.GetRequireActive() || pol.GetPrepareDisabledNodes() ||
		len(pol.GetBooleans()) > 0 || len(pol.GetPermissiveDomains()) > 0 ||
		len(pol.GetModules()) > 0 || len(pol.GetCustomModules()) > 0 ||
		len(pol.GetFileContexts()) > 0 || len(pol.GetLogins()) > 0
}

// SelinuxContentHasCustom reports whether an SELinux policy carries
// administrator-authored policy text or mappings that change what the
// system allows beyond booleans and modes: custom modules, file contexts
// or login mappings. Such saves and releases are audited.
func SelinuxContentHasCustom(content string) bool {
	var pol pb.SelinuxPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return false
	}
	return len(pol.GetCustomModules()) > 0 || len(pol.GetFileContexts()) > 0 || len(pol.GetLogins()) > 0 || len(pol.GetPermissiveDomains()) > 0
}

// SelinuxCustomSummary describes the custom content of an SELinux policy
// for the audit log: the module names with a SHA-256 of their source, the
// permissive domains, and the counts of file contexts and login mappings.
func SelinuxCustomSummary(content string) string {
	var pol pb.SelinuxPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return "{}"
	}
	type module struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	}
	summary := struct {
		Modules      []module `json:"modules"`
		Permissive   []string `json:"permissive_domains"`
		FileContexts int      `json:"file_contexts"`
		Logins       int      `json:"logins"`
	}{Modules: []module{}, Permissive: []string{}}
	for _, m := range pol.GetCustomModules() {
		sum := sha256.Sum256([]byte(m.GetCil()))
		summary.Modules = append(summary.Modules, module{Name: macrules.ModuleName(m.GetName()), SHA256: hex.EncodeToString(sum[:])})
	}
	summary.Permissive = append(summary.Permissive, pol.GetPermissiveDomains()...)
	summary.FileContexts = len(pol.GetFileContexts())
	summary.Logins = len(pol.GetLogins())
	out, err := json.Marshal(summary)
	if err != nil {
		return "{}"
	}
	return string(out)
}
