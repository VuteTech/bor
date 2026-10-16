// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"regexp"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// kcmModuleIDPattern matches KDE control module identifiers such as
// kcm_autostart or kcmspellchecking. The id becomes an INI key.
var kcmModuleIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// maxKConfigStringLen bounds free-text values written into INI files.
const maxKConfigStringLen = 4096

// ValidateKConfigPolicy validates a KConfig policy content JSON string.
func ValidateKConfigPolicy(content string) error {
	if content == "" || content == "{}" {
		return fmt.Errorf("KConfig policy content is empty")
	}

	var kcp pb.KConfigPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &kcp); err != nil {
		return fmt.Errorf("invalid KConfig policy JSON: %w", err)
	}

	// At least one effective setting must be configured. The legacy
	// resource-restriction fields parse but have no effect on Plasma 5/6.
	if !kconfigPolicyHasSettings(&kcp) {
		if legacy := legacyKConfigFields(&kcp); len(legacy) > 0 {
			return fmt.Errorf("KConfig policy only configures %s: KDE Plasma 5 and 6 ignore these legacy resource restrictions, so the policy would have no effect; remove them and configure another setting",
				strings.Join(legacy, ", "))
		}
		return fmt.Errorf("KConfig policy must configure at least one setting")
	}

	if v := kcp.LockTimeout; v != nil && *v < 0 {
		return fmt.Errorf("lockTimeout (minutes) must not be negative")
	}
	if v := kcp.LockGrace; v != nil && *v < 0 {
		return fmt.Errorf("lockGrace (seconds) must not be negative")
	}

	// String values become INI "key=value" lines: no line breaks.
	for _, f := range []struct {
		name string
		val  *string
	}{
		{"iconTheme", kcp.IconTheme},
		{"wallpaperPlugin", kcp.WallpaperPlugin},
		{"wallpaperImage", kcp.WallpaperImage},
		{"wallpaperFillMode", kcp.WallpaperFillMode},
		{"wallpaperColor", kcp.WallpaperColor},
		{"lockScreenImage", kcp.LockScreenImage},
	} {
		if err := validateKConfigString(f.name, f.val); err != nil {
			return err
		}
	}

	// URL restriction rules are rendered as eight comma-separated fields.
	for i, r := range kcp.UrlRestrictions {
		switch r.GetAction() {
		case "open", "list", "redirect":
			// valid
		default:
			return fmt.Errorf("url_restrictions[%d]: invalid action %q (must be open, list, or redirect)", i, r.GetAction())
		}
		for _, f := range []struct {
			name string
			val  string
		}{
			{"referrerProtocol", r.GetReferrerProtocol()},
			{"referrerHost", r.GetReferrerHost()},
			{"referrerPath", r.GetReferrerPath()},
			{"protocol", r.GetProtocol()},
			{"host", r.GetHost()},
			{"path", r.GetPath()},
		} {
			if strings.ContainsAny(f.val, ",\r\n") {
				return fmt.Errorf("url_restrictions[%d]: %s must not contain commas or line breaks", i, f.name)
			}
			if len(f.val) > maxKConfigStringLen {
				return fmt.Errorf("url_restrictions[%d]: %s is too long (max %d characters)", i, f.name, maxKConfigStringLen)
			}
		}
	}

	// Control module ids become INI keys.
	for i, id := range kcp.KcmRestrictions {
		if !kcmModuleIDPattern.MatchString(id) {
			return fmt.Errorf("kcm_restrictions[%d]: invalid module id %q (letters, digits, '_', '.', '-' only)", i, id)
		}
	}

	return nil
}

// validateKConfigString checks a free-text INI value.
func validateKConfigString(name string, v *string) error {
	if v == nil {
		return nil
	}
	if strings.ContainsAny(*v, "\r\n") {
		return fmt.Errorf("%s must not contain line breaks", name)
	}
	if len(*v) > maxKConfigStringLen {
		return fmt.Errorf("%s is too long (max %d characters)", name, maxKConfigStringLen)
	}
	return nil
}

// kconfigPolicyHasSettings reports whether the policy has at least one
// effective setting configured (any non-nil optional field or non-empty
// repeated field). The deprecated resource-restriction fields do not count.
func kconfigPolicyHasSettings(kcp *pb.KConfigPolicy) bool {
	return kcp.ShellAccess != nil ||
		kcp.RunCommand != nil ||
		kcp.ActionLogout != nil ||
		kcp.ActionFileNew != nil ||
		kcp.ActionFileOpen != nil ||
		kcp.ActionFileSave != nil ||
		kcp.BorderlessMaximizedWindows != nil ||
		kcp.PlasmoidUnlockedDesktop != nil ||
		kcp.AllowConfigureWhenLocked != nil ||
		kcp.AutoLock != nil ||
		kcp.LockOnResume != nil ||
		kcp.LockTimeout != nil ||
		kcp.LockGrace != nil ||
		kcp.LockOnStart != nil ||
		kcp.LockScreenImage != nil ||
		kcp.IconTheme != nil ||
		kcp.WallpaperPlugin != nil ||
		kcp.WallpaperImage != nil ||
		kcp.WallpaperFillMode != nil ||
		kcp.WallpaperColor != nil ||
		len(kcp.UrlRestrictions) > 0 ||
		len(kcp.KcmRestrictions) > 0
}

// legacyKConfigFields lists the deprecated [KDE Resource Restrictions]
// fields that are set. They are kept so that old content still parses; the
// agent ignores them because no KF5/KF6 component reads that group.
//
//nolint:staticcheck // SA1019: the deprecated fields are inspected on purpose to warn about them.
func legacyKConfigFields(kcp *pb.KConfigPolicy) []string {
	var out []string
	if kcp.RestrictWallpaper != nil {
		out = append(out, "restrictWallpaper")
	}
	if kcp.RestrictIcons != nil {
		out = append(out, "restrictIcons")
	}
	if kcp.RestrictAutostart != nil {
		out = append(out, "restrictAutostart")
	}
	if kcp.RestrictColors != nil {
		out = append(out, "restrictColors")
	}
	if kcp.RestrictCursors != nil {
		out = append(out, "restrictCursors")
	}
	return out
}

// ParseKConfigPolicyContent parses and validates a KConfig policy content string.
func ParseKConfigPolicyContent(content string) (*pb.KConfigPolicy, error) {
	if err := ValidateKConfigPolicy(content); err != nil {
		return nil, err
	}

	var kcp pb.KConfigPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &kcp); err != nil {
		return nil, fmt.Errorf("invalid KConfig policy JSON: %w", err)
	}

	return &kcp, nil
}
