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

var (
	// kconfigFileNamePattern: a config file basename inside the managed
	// directory ("kdeglobals", "plasma-org.kde.plasma.desktop-appletsrc").
	kconfigFileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// kconfigLocalePattern: KConfig locale suffixes such as de, de_DE, sr@latin.
	kconfigLocalePattern = regexp.MustCompile(`^[a-z]{2,3}(_[A-Z]{2})?(@[a-z]+)?$`)
	// kconfigCatalogueIDPattern: catalogue ids such as kiosk.shell_access.
	kconfigCatalogueIDPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]*$`)
)

const (
	// maxKConfigStringLen bounds free-text values written into INI files.
	maxKConfigStringLen = 4096
	// maxKConfigSettings bounds the generic settings list of one policy.
	maxKConfigSettings = 500
	// maxKConfigNameLen bounds file names, group names and keys.
	maxKConfigNameLen = 256
)

// ValidateKConfigTarget checks that file, group path and key can be written
// as an INI entry inside the managed directory. It is shared by the policy
// validator and the catalogue test.
func ValidateKConfigTarget(file string, groupPath []string, key string) error {
	if !kconfigFileNamePattern.MatchString(file) || len(file) > maxKConfigNameLen {
		return fmt.Errorf("invalid config file name %q (letters, digits, '.', '_' and '-' only; no paths)", file)
	}
	if len(groupPath) == 0 {
		return fmt.Errorf("%s/%s: group path must not be empty", file, key)
	}
	for _, g := range groupPath {
		if g == "" || len(g) > maxKConfigNameLen || strings.ContainsAny(g, "[]\r\n") {
			return fmt.Errorf("%s/%s: invalid group name %q", file, key, g)
		}
	}
	if key == "" || len(key) > maxKConfigNameLen || strings.ContainsAny(key, "=[]\r\n") {
		return fmt.Errorf("%s: invalid key %q (no '=', '[', ']' or line breaks)", file, key)
	}
	return nil
}

// joinGroupPath renders a group path the way the agent keys entries.
func joinGroupPath(path []string) string {
	return strings.Join(path, "][")
}

// validateKConfigSettings checks the generic settings list of a policy.
func validateKConfigSettings(settings []*pb.KConfigSetting) error {
	if len(settings) > maxKConfigSettings {
		return fmt.Errorf("too many settings (%d, max %d)", len(settings), maxKConfigSettings)
	}
	seen := make(map[string]int, len(settings))
	for i, s := range settings {
		if err := ValidateKConfigTarget(s.GetFile(), s.GetGroupPath(), s.GetKey()); err != nil {
			return fmt.Errorf("settings[%d]: %w", i, err)
		}
		if strings.ContainsAny(s.GetValue(), "\r\n") {
			return fmt.Errorf("settings[%d] (%s): value must not contain line breaks", i, s.GetKey())
		}
		if len(s.GetValue()) > maxKConfigStringLen {
			return fmt.Errorf("settings[%d] (%s): value is too long (max %d characters)", i, s.GetKey(), maxKConfigStringLen)
		}
		if l := s.GetLocale(); l != "" && !kconfigLocalePattern.MatchString(l) {
			return fmt.Errorf("settings[%d] (%s): invalid locale %q", i, s.GetKey(), l)
		}
		if id := s.GetCatalogueId(); len(id) > 128 || !kconfigCatalogueIDPattern.MatchString(id) {
			return fmt.Errorf("settings[%d] (%s): invalid catalogue id %q", i, s.GetKey(), id)
		}
		id := s.GetFile() + "\x00" + joinGroupPath(s.GetGroupPath()) + "\x00" + s.GetKey() + "\x00" + s.GetLocale()
		if j, dup := seen[id]; dup {
			return fmt.Errorf("settings[%d] and settings[%d] both write %s [%s] %s", j, i, s.GetFile(), joinGroupPath(s.GetGroupPath()), s.GetKey())
		}
		seen[id] = i
	}
	return nil
}

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
	if kcp.GetKcmMode() == pb.KcmRestrictionMode_KCM_SHOW_ONLY_LISTED && len(kcp.KcmRestrictions) == 0 {
		return fmt.Errorf("kcm_mode show-only needs at least one System Settings module to keep visible")
	}

	if err := validateKConfigSettings(kcp.Settings); err != nil {
		return err
	}

	if err := validateKConfigLayout(kcp.GetLayout()); err != nil {
		return err
	}

	if err := validateKConfigUnixGroups(kcp.GetUnixGroups()); err != nil {
		return err
	}

	if err := validateKonsoleProfile(kcp.GetKonsoleProfile()); err != nil {
		return err
	}

	if err := validateHiddenApplications(kcp.GetHiddenApplications()); err != nil {
		return err
	}

	return nil
}

// Limits for the application packs.
const (
	maxKonsoleProfileNameLen = 64
	maxKonsoleProfileExtra   = 8 * 1024
	maxKonsoleHistorySize    = 1_000_000
	maxHiddenApplications    = 200
)

var (
	konsoleProfileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)
	desktopFileIDPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.desktop$`)
)

// konsoleProfileHasContent mirrors the agent's KonsoleProfileHasContent.
func konsoleProfileHasContent(p *pb.KonsoleProfile) bool {
	if p == nil {
		return false
	}
	return strings.TrimSpace(p.GetCommand()) != "" || strings.TrimSpace(p.GetHistoryMode()) != "" ||
		strings.TrimSpace(p.GetColorScheme()) != "" || strings.TrimSpace(p.GetFont()) != "" ||
		strings.TrimSpace(p.GetExtraIni()) != "" || p.GetSetDefault() || p.GetLock()
}

// validateKonsoleProfile checks the Konsole profile: a file-name safe
// profile name, single-line values, a known history mode and INI text made
// of groups, keys and comments only.
func validateKonsoleProfile(p *pb.KonsoleProfile) error {
	if p == nil {
		return nil
	}
	if name := strings.TrimSpace(p.GetName()); name != "" {
		if len(name) > maxKonsoleProfileNameLen || !konsoleProfileNamePattern.MatchString(name) {
			return fmt.Errorf("konsoleProfile.name must be letters, digits, spaces, dots, underscores or dashes (max %d characters)", maxKonsoleProfileNameLen)
		}
	}
	for field, v := range map[string]string{"command": p.GetCommand(), "colorScheme": p.GetColorScheme(), "font": p.GetFont()} {
		if strings.ContainsAny(v, "\r\n") || len(v) > maxKConfigStringLen {
			return fmt.Errorf("konsoleProfile.%s must be a single line (max %d characters)", field, maxKConfigStringLen)
		}
	}
	switch strings.TrimSpace(p.GetHistoryMode()) {
	case "", "0", "1", "2":
	default:
		return fmt.Errorf("konsoleProfile.historyMode must be 0 (none), 1 (fixed) or 2 (unlimited)")
	}
	if p.GetHistorySize() < 0 || p.GetHistorySize() > maxKonsoleHistorySize {
		return fmt.Errorf("konsoleProfile.historySize must be between 0 and %d lines", maxKonsoleHistorySize)
	}
	if len(p.GetExtraIni()) > maxKonsoleProfileExtra {
		return fmt.Errorf("konsoleProfile.extraIni is too long (max %d bytes)", maxKonsoleProfileExtra)
	}
	for i, raw := range strings.Split(strings.ReplaceAll(p.GetExtraIni(), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "", strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) > 2:
		default:
			k, _, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(k) == "" || strings.ContainsAny(k, "[]") {
				return fmt.Errorf("konsoleProfile.extraIni line %d: expected [Group], key=value or a # comment", i+1)
			}
		}
	}
	return nil
}

// validateHiddenApplications checks the desktop file ids of the hidden
// applications list.
func validateHiddenApplications(ids []string) error {
	if len(ids) > maxHiddenApplications {
		return fmt.Errorf("too many hiddenApplications (%d, max %d)", len(ids), maxHiddenApplications)
	}
	seen := make(map[string]bool, len(ids))
	for i, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || len(id) > 255 || strings.Contains(id, "..") || !desktopFileIDPattern.MatchString(id) {
			return fmt.Errorf("hiddenApplications[%d]: %q is not a desktop file id (expected something like org.kde.konsole.desktop)", i, raw)
		}
		if seen[id] {
			return fmt.Errorf("hiddenApplications[%d]: duplicate %q", i, id)
		}
		seen[id] = true
	}
	return nil
}

// Limits for the group scope of a policy.
const (
	maxKConfigUnixGroups   = 32
	maxKConfigGroupNameLen = 128
)

// validateKConfigUnixGroups checks the Unix group names a policy is limited
// to. Names are resolved on the nodes (local, LDAP, SSSD, winbind), so
// spaces and domain separators are allowed; path and control characters
// are not.
func validateKConfigUnixGroups(groups []string) error {
	if len(groups) > maxKConfigUnixGroups {
		return fmt.Errorf("too many unix_groups (%d, max %d)", len(groups), maxKConfigUnixGroups)
	}
	seen := make(map[string]bool, len(groups))
	for i, g := range groups {
		name := strings.TrimSpace(g)
		if name == "" {
			return fmt.Errorf("unix_groups[%d]: empty group name", i)
		}
		if len(name) > maxKConfigGroupNameLen || strings.ContainsAny(name, "/\x00\r\n\t:") {
			return fmt.Errorf("unix_groups[%d]: invalid group name %q", i, name)
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			return fmt.Errorf("unix_groups[%d]: duplicate group %q", i, name)
		}
		seen[lower] = true
	}
	return nil
}

// maxKConfigLayoutScript bounds the Plasma layout script.
const maxKConfigLayoutScript = 64 * 1024

// kconfigLayoutHasContent mirrors the agent's LayoutHasContent.
func kconfigLayoutHasContent(l *pb.KConfigLayout) bool {
	return l != nil && (strings.TrimSpace(l.GetWallpaperImage()) != "" || strings.TrimSpace(l.GetLayoutScript()) != "")
}

// validateKConfigLayout checks the desktop layout block.
func validateKConfigLayout(l *pb.KConfigLayout) error {
	if l == nil {
		return nil
	}
	if img := strings.TrimSpace(l.GetWallpaperImage()); img != "" {
		if strings.ContainsAny(img, "\r\n\"\\") || len(img) > maxKConfigStringLen {
			return fmt.Errorf("layout.wallpaperImage contains characters that are not allowed or is too long")
		}
		if !strings.HasPrefix(img, "/") && !strings.HasPrefix(img, "file:///") {
			return fmt.Errorf("layout.wallpaperImage must be an absolute path or a file:// URL readable by every user")
		}
	}
	switch strings.TrimSpace(l.GetWallpaperFillMode()) {
	case "", "0", "1", "2", "3", "6":
	default:
		return fmt.Errorf("layout.wallpaperFillMode must be one of 0, 1, 2, 3 or 6")
	}
	if len(l.GetLayoutScript()) > maxKConfigLayoutScript {
		return fmt.Errorf("layout.layoutScript is too long (max %d bytes)", maxKConfigLayoutScript)
	}
	if !kconfigLayoutHasContent(l) && (l.GetApplyOnce() || l.GetApplyLive() || l.GetLockPackage()) {
		return fmt.Errorf("layout needs a wallpaper image or a layout script")
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
		len(kcp.KcmRestrictions) > 0 ||
		len(kcp.Settings) > 0 ||
		kcp.GetFileImmutableKdeglobals() ||
		kconfigLayoutHasContent(kcp.GetLayout()) ||
		konsoleProfileHasContent(kcp.GetKonsoleProfile()) ||
		len(kcp.HiddenApplications) > 0
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
