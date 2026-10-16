// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// BackupSuffix is appended to the original file path to create a backup.
const BackupSuffix = ".bor-backup"

// ManagedFileHeader is prepended to every file written by SyncKConfigFiles.
const ManagedFileHeader = "# This file is managed by Bor. Do not edit manually.\n# Changes will be overwritten by policy enforcement.\n\n"

// KDE configuration files and groups the typed policy mapping writes to.
// The three restriction groups are the only ones KAuthorized reads in KDE
// Frameworks 5 and 6; it reads them from the application's own config
// merged with kdeglobals, which is why everything lands in kdeglobals.
const (
	KConfigFileGlobals        = "kdeglobals"
	KConfigFileKWin           = "kwinrc"
	KConfigFileScreenLocker   = "kscreenlockerrc"
	KConfigFileDesktopApplets = "plasma-org.kde.plasma.desktop-appletsrc"

	KConfigGroupActionRestrictions = "KDE Action Restrictions"
	KConfigGroupKCMRestrictions    = "KDE Control Module Restrictions"
	KConfigGroupURLRestrictions    = "KDE URL Restrictions"
)

// kconfigGroup holds entries for a single INI [Group] within a file.
type kconfigGroup struct {
	name    string
	entries []*pb.KConfigEntry
}

// enforcedSet builds a fast-lookup set from the KConfigPolicy.EnforcedFields list.
func enforcedSet(pol *pb.KConfigPolicy) map[string]bool {
	s := make(map[string]bool, len(pol.EnforcedFields))
	for _, f := range pol.EnforcedFields {
		s[f] = true
	}
	return s
}

// boolVal converts an optional bool proto pointer to an INI "true"/"false" string.
func boolVal(v *bool) string {
	if v != nil && *v {
		return "true"
	}
	return "false"
}

// KConfigPolicyToEntries converts a typed KConfigPolicy to the flat
// []*KConfigEntry slice consumed by MergeKConfigPolicies / MergeKConfigEntries.
// Absent optional fields (nil pointers, empty repeated) are skipped.
//
// Key names follow the KDE Kiosk documentation and the KConfigXT schemas
// of the components involved; KConfig keys are case-sensitive, so the
// spelling matters (Autolock, not AutoLock).
func KConfigPolicyToEntries(pol *pb.KConfigPolicy) []*pb.KConfigEntry {
	if pol == nil {
		return nil
	}

	enforced := enforcedSet(pol)

	var entries []*pb.KConfigEntry
	add := func(e *pb.KConfigEntry) {
		if e != nil {
			entries = append(entries, e)
		}
	}

	boolE := func(file, group, key, jsonKey string, val *bool) *pb.KConfigEntry {
		if val == nil {
			return nil
		}
		return &pb.KConfigEntry{File: file, Group: group, Key: key, Value: boolVal(val), Type: "bool", Enforced: enforced[jsonKey]}
	}

	strE := func(file, group, key, jsonKey string, val *string) *pb.KConfigEntry {
		if val == nil {
			return nil
		}
		return &pb.KConfigEntry{File: file, Group: group, Key: key, Value: *val, Type: "string", Enforced: enforced[jsonKey]}
	}

	intE := func(file, group, key, jsonKey string, val *int32) *pb.KConfigEntry {
		if val == nil {
			return nil
		}
		return &pb.KConfigEntry{File: file, Group: group, Key: key, Value: strconv.Itoa(int(*val)), Type: "int", Enforced: enforced[jsonKey]}
	}

	// Kiosk action restrictions (kdeglobals, [KDE Action Restrictions]).
	// false restricts, true allows.
	action := func(key, jsonKey string, val *bool) {
		add(boolE(KConfigFileGlobals, KConfigGroupActionRestrictions, key, jsonKey, val))
	}
	action("shell_access", "shellAccess", pol.ShellAccess)
	// run_command disables KRunner's command execution; action/run_command
	// additionally hides the entry from the desktop menu (KDE Kiosk keys).
	action("run_command", "runCommand", pol.RunCommand)
	action("action/run_command", "runCommand", pol.RunCommand)
	// Plasma checks the generic "logout" restriction as well as the
	// "action/logout" action for the leave, shutdown and restart entries.
	action("logout", "actionLogout", pol.ActionLogout)
	action("action/logout", "actionLogout", pol.ActionLogout)
	action("action/file_new", "actionFileNew", pol.ActionFileNew)
	action("action/file_open", "actionFileOpen", pol.ActionFileOpen)
	action("action/file_save", "actionFileSave", pol.ActionFileSave)

	// Plasma desktop lockdown. These are KAuthorized actions, not plasmarc
	// keys: plasmashell checks plasma/plasmashell/unlockedDesktop before it
	// lets the user edit panels, widgets or launcher favourites.
	action("plasma/plasmashell/unlockedDesktop", "plasmoidUnlockedDesktop", pol.PlasmoidUnlockedDesktop)
	action("plasma/allow_configure_when_locked", "allowConfigureWhenLocked", pol.AllowConfigureWhenLocked)

	// Resource restrictions (restrict_wallpaper .. restrict_cursors) are not
	// emitted: [KDE Resource Restrictions] was a KDE 4 KStandardDirs feature
	// and no KF5/KF6 component reads it. The fields are kept in the proto so
	// that old content still parses; the editor only offers to remove them.

	// Window Manager (kwinrc, [Windows])
	add(boolE(KConfigFileKWin, "Windows", "BorderlessMaximizedWindows", "borderlessMaximizedWindows", pol.BorderlessMaximizedWindows))

	// Screen lock (kscreenlockerrc, [Daemon]) - key names from
	// kscreenlockersettings.kcfg. Timeout is in minutes, LockGrace in seconds.
	add(boolE(KConfigFileScreenLocker, "Daemon", "Autolock", "autoLock", pol.AutoLock))
	add(boolE(KConfigFileScreenLocker, "Daemon", "LockOnResume", "lockOnResume", pol.LockOnResume))
	add(boolE(KConfigFileScreenLocker, "Daemon", "LockOnStart", "lockOnStart", pol.LockOnStart))
	add(intE(KConfigFileScreenLocker, "Daemon", "Timeout", "lockTimeout", pol.LockTimeout))
	add(intE(KConfigFileScreenLocker, "Daemon", "LockGrace", "lockGrace", pol.LockGrace))

	// Lock-screen wallpaper (kscreenlockerrc, [Greeter]). Unlike the desktop
	// wallpaper this cascades reliably from system configuration.
	if pol.LockScreenImage != nil {
		plugin := "org.kde.image"
		add(strE(KConfigFileScreenLocker, "Greeter", "WallpaperPlugin", "lockScreenImage", &plugin))
		add(strE(KConfigFileScreenLocker, "Greeter][Wallpaper][org.kde.image][General", "Image", "lockScreenImage", pol.LockScreenImage))
	}

	// Appearance
	add(strE(KConfigFileGlobals, "Icons", "Theme", "iconTheme", pol.IconTheme))
	add(strE(KConfigFileDesktopApplets, "Containments][1", "wallpaperplugin", "wallpaperPlugin", pol.WallpaperPlugin))
	add(strE(KConfigFileDesktopApplets, "Containments][1][Wallpaper][org.kde.image][General", "Image", "wallpaperImage", pol.WallpaperImage))
	add(strE(KConfigFileDesktopApplets, "Containments][1][Wallpaper][org.kde.image][General", "FillMode", "wallpaperFillMode", pol.WallpaperFillMode))
	add(strE(KConfigFileDesktopApplets, "Containments][1][Wallpaper][org.kde.image][General", "Color", "wallpaperColor", pol.WallpaperColor))

	// URL Restrictions (kdeglobals, [KDE URL Restrictions]) - always enforced
	for i, r := range pol.UrlRestrictions {
		val := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%v",
			r.GetAction(), r.GetReferrerProtocol(), r.GetReferrerHost(), r.GetReferrerPath(),
			r.GetProtocol(), r.GetHost(), r.GetPath(), r.GetEnabled())
		entries = append(entries, &pb.KConfigEntry{
			File:     KConfigFileGlobals,
			Group:    KConfigGroupURLRestrictions,
			Key:      fmt.Sprintf("rule_%d", i+1),
			Value:    val,
			Type:     "string",
			Enforced: true,
		})
	}
	if len(pol.UrlRestrictions) > 0 {
		entries = append(entries, &pb.KConfigEntry{
			File:     KConfigFileGlobals,
			Group:    KConfigGroupURLRestrictions,
			Key:      "rule_count",
			Value:    strconv.Itoa(len(pol.UrlRestrictions)),
			Type:     "string",
			Enforced: true,
		})
	}

	// KCM Restrictions (kdeglobals, [KDE Control Module Restrictions]) - always
	// enforced. In show-only mode the list is an allow list that needs the
	// node's installed modules; MergeKConfigPolicies expands it.
	if pol.GetKcmMode() != pb.KcmRestrictionMode_KCM_SHOW_ONLY_LISTED {
		for _, mod := range pol.KcmRestrictions {
			entries = append(entries, &pb.KConfigEntry{
				File:     KConfigFileGlobals,
				Group:    KConfigGroupKCMRestrictions,
				Key:      mod,
				Value:    "false",
				Type:     "bool",
				Enforced: true,
			})
		}
	}

	// Generic settings: any file, nested groups, all markers.
	entries = append(entries, settingsToEntries(pol.Settings)...)

	return entries
}

// settingsToEntries converts generic settings into INI entries. Group paths
// are joined with "][" so that nested groups render as [A][B].
func settingsToEntries(settings []*pb.KConfigSetting) []*pb.KConfigEntry {
	out := make([]*pb.KConfigEntry, 0, len(settings))
	for _, s := range settings {
		out = append(out, &pb.KConfigEntry{
			File:     s.GetFile(),
			Group:    strings.Join(s.GetGroupPath(), "]["),
			Key:      s.GetKey(),
			Value:    s.GetValue(),
			Type:     "string",
			Enforced: s.GetImmutable(),
			Expand:   s.GetExpand(),
			Delete:   s.GetDelete(),
			Locale:   s.GetLocale(),
		})
	}
	return out
}

// KConfigPolicySource is one bound policy together with the binding
// priority the server delivered for it (Policy.priority).
type KConfigPolicySource struct {
	ID       string
	Name     string
	Priority int32
	Policy   *pb.KConfigPolicy
	// Groups limits the policy to members of these Unix groups; empty
	// means every user on the node.
	Groups []string
}

// kconfigWinner records which policy's entry owns a key during a merge.
type kconfigWinner struct {
	entry *pb.KConfigEntry
	src   KConfigPolicySource
}

// KConfigMergeResult is the outcome of MergeKConfigPolicies.
type KConfigMergeResult struct {
	// Files maps a config file basename to its rendered INI content.
	Files map[string][]byte
	// Entries are the winning entries in merge order. They are what Files
	// was rendered from and feed the optional /etc/kde5rc mirror.
	Entries []*pb.KConfigEntry
	// Items holds one compliance item per entry of every source policy,
	// keyed by policy ID. Entries that lost against a higher-priority
	// policy are reported as INAPPLICABLE with the applied value.
	Items map[string][]*pb.ComplianceItemResult
	// Layout is the desktop layout to deploy (from the highest-priority
	// policy that has one), with the policy it came from. Nil when no
	// bound policy configures a layout.
	Layout           *pb.KConfigLayout
	LayoutPolicyID   string
	LayoutPolicyName string
	// GroupFiles holds the rendered files of group-scoped policies, keyed
	// by group slug and then file basename. Groups lists the scopes,
	// highest priority first, as the login hook orders them.
	GroupFiles map[string]map[string][]byte
	Groups     []KConfigGroupScope
	// Apps are the node-wide application files (Konsole profile, hidden
	// desktop entries); GroupApps the same per group slug.
	Apps      KConfigAppFiles
	GroupApps map[string]KConfigAppFiles
}

// kconfigScope collects the winning entries of one overlay: the node-wide
// directory (prefix "") or a per-group directory ("groups/<slug>/").
type kconfigScope struct {
	prefix        string
	winners       map[string]kconfigWinner
	entries       []*pb.KConfigEntry
	fileImmutable map[string]bool
	// Application files of the scope, and who provided them.
	apps       KConfigAppFiles
	konsoleSrc KConfigPolicySource
	hiddenBy   map[string]string // desktop file id -> first policy hiding it
}

func newKConfigScope(prefix string) *kconfigScope {
	return &kconfigScope{prefix: prefix, winners: make(map[string]kconfigWinner), fileImmutable: make(map[string]bool), hiddenBy: make(map[string]string)}
}

// item builds the compliance item for an entry of this scope.
func (sc *kconfigScope) item(e *pb.KConfigEntry, status pb.ComplianceStatus, msg string) *pb.ComplianceItemResult {
	key := fmt.Sprintf("[%s] %s", e.Group, e.Key)
	if e.Locale != "" {
		key += "[" + e.Locale + "]"
	}
	return &pb.ComplianceItemResult{SchemaId: sc.prefix + e.File, Key: key, Status: status, Message: msg}
}

// claim records e as the winner for its key in this scope and returns
// false when another policy already owns it.
func (sc *kconfigScope) claim(e *pb.KConfigEntry, src KConfigPolicySource) (kconfigWinner, bool) {
	id := e.File + "\x00" + e.Group + "\x00" + e.Key + "\x00" + e.Locale
	if w, seen := sc.winners[id]; seen {
		return w, false
	}
	sc.winners[id] = kconfigWinner{entry: e, src: src}
	sc.entries = append(sc.entries, e)
	return kconfigWinner{}, true
}

// render turns the scope's entries into files.
func (sc *kconfigScope) render() (map[string][]byte, error) {
	files, err := MergeKConfigEntries(sc.entries)
	if err != nil {
		return nil, err
	}
	for name := range sc.fileImmutable {
		if files == nil {
			files = make(map[string][]byte)
		}
		// A bare [$i] line before the first group makes the whole file, and
		// therefore every user's copy of it, immutable.
		files[name] = append([]byte("[$i]\n\n"), files[name]...)
	}
	return files, nil
}

func sameKConfigEffect(a, b *pb.KConfigEntry) bool {
	return a.Value == b.Value && a.Enforced == b.Enforced && a.Expand == b.Expand && a.Delete == b.Delete
}

// MergeKConfigPolicies combines all bound KDE Plasma policies into one set
// of config files per scope. Within a scope each (file, group, key, locale)
// is written once: the policy with the highest binding priority wins, ties
// are broken by policy ID so the result never depends on event order. URL
// restriction rules are additive across policies and renumbered. Entries
// that cannot be written safely (line breaks, odd key characters) are
// skipped and reported.
//
// Policies without Groups fill the node-wide scope; a policy with Groups
// fills one scope per group (GroupFiles), which the login hook adds to
// XDG_CONFIG_DIRS for members only. installedKCMs is the list of System
// Settings modules present on the node (InstalledKCMs); it is only needed
// by policies in show-only module mode.
func MergeKConfigPolicies(sources []KConfigPolicySource, installedKCMs []string) (*KConfigMergeResult, error) {
	ordered := make([]KConfigPolicySource, len(sources))
	copy(ordered, sources)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority > ordered[j].Priority
		}
		return ordered[i].ID < ordered[j].ID
	})

	res := &KConfigMergeResult{Items: make(map[string][]*pb.ComplianceItemResult, len(ordered))}
	nodeScope := newKConfigScope("")
	groupScopes := make(map[string]*kconfigScope)
	groupMeta := make(map[string]*KConfigGroupScope)

	for _, src := range ordered {
		items := []*pb.ComplianceItemResult{}

		scopes := []*kconfigScope{nodeScope}
		groups := uniqueGroupNames(src.Groups)
		if len(groups) > 0 {
			scopes = scopes[:0]
			for _, g := range groups {
				slug := GroupSlug(g)
				sc, ok := groupScopes[slug]
				if !ok {
					sc = newKConfigScope(GroupOverlayDir + "/" + slug + "/")
					groupScopes[slug] = sc
					groupMeta[slug] = &KConfigGroupScope{Name: g, Slug: slug, Priority: src.Priority}
				}
				meta := groupMeta[slug]
				if src.Priority > meta.Priority {
					meta.Priority = src.Priority
				}
				meta.PolicyIDs = append(meta.PolicyIDs, src.ID)
				scopes = append(scopes, sc)
			}
		}

		for _, sc := range scopes {
			items = append(items, mergeSourceIntoScope(sc, src, installedKCMs)...)
		}

		// Desktop layout: one look-and-feel package per node, from the
		// highest-priority policy that configures one. The package is
		// selected through kdeglobals [KDE] LookAndFeelPackage, which a
		// catalogue setting of a higher-priority policy may still override.
		// A group-scoped policy cannot carry it: the package and its update
		// script apply to every user.
		if LayoutHasContent(src.Policy.GetLayout()) {
			switch {
			case len(groups) > 0:
				items = append(items, &pb.ComplianceItemResult{
					SchemaId: "layout",
					Key:      "look-and-feel",
					Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
					Message:  "The desktop layout applies to every user on a node; move it to a policy without a group scope",
				})
			case res.Layout == nil:
				res.Layout = src.Policy.GetLayout()
				res.LayoutPolicyID = src.ID
				res.LayoutPolicyName = src.Name
				pkg := LayoutPackageID(src.ID)
				e := &pb.KConfigEntry{File: KConfigFileGlobals, Group: "KDE", Key: "LookAndFeelPackage", Value: pkg, Type: "string", Enforced: res.Layout.GetLockPackage()}
				if w, ok := nodeScope.claim(e, src); ok {
					items = append(items, nodeScope.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed look-and-feel package "+pkg))
				} else {
					items = append(items, nodeScope.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
						fmt.Sprintf("Global theme is set to %q by policy %q; the layout package %s is generated but not selected", w.entry.Value, w.src.Name, pkg)))
				}
			default:
				items = append(items, &pb.ComplianceItemResult{
					SchemaId: "layout",
					Key:      "look-and-feel",
					Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
					Message:  fmt.Sprintf("Overridden by higher-priority policy %q, which provides the desktop layout", res.LayoutPolicyName),
				})
			}
		}
		res.Items[src.ID] = items
	}

	files, err := nodeScope.render()
	if err != nil {
		return nil, err
	}
	res.Files = files
	res.Entries = nodeScope.entries
	res.Apps = nodeScope.apps

	if len(groupScopes) > 0 {
		res.GroupFiles = make(map[string]map[string][]byte, len(groupScopes))
		res.GroupApps = make(map[string]KConfigAppFiles, len(groupScopes))
		for slug, sc := range groupScopes {
			gf, err := sc.render()
			if err != nil {
				return nil, err
			}
			if gf == nil {
				gf = map[string][]byte{}
			}
			res.GroupFiles[slug] = gf
			res.GroupApps[slug] = sc.apps
			res.Groups = append(res.Groups, *groupMeta[slug])
		}
		sort.SliceStable(res.Groups, func(i, j int) bool {
			if res.Groups[i].Priority != res.Groups[j].Priority {
				return res.Groups[i].Priority > res.Groups[j].Priority
			}
			return res.Groups[i].Slug < res.Groups[j].Slug
		})
	}
	return res, nil
}

// mergeSourceIntoScope adds one policy's entries to a scope and returns the
// compliance items for them.
func mergeSourceIntoScope(sc *kconfigScope, src KConfigPolicySource, installedKCMs []string) []*pb.ComplianceItemResult {
	items := []*pb.ComplianceItemResult{}
	for _, e := range KConfigPolicyToEntries(src.Policy) {
		if err := validateINIEntry(e); err != nil {
			items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()))
			continue
		}

		if e.Group == KConfigGroupURLRestrictions {
			// Rules from all policies are kept; rule_count is regenerated
			// by renumberURLRestrictions after the merge.
			if e.Key == "rule_count" {
				continue
			}
			sc.entries = append(sc.entries, e)
			items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"))
			continue
		}

		if w, ok := sc.claim(e, src); !ok {
			if sameKConfigEffect(w.entry, e) {
				items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
					fmt.Sprintf("Same value as higher-priority policy %q", w.src.Name)))
			} else {
				items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
					fmt.Sprintf("Overridden by higher-priority policy %q (priority %d); applied value: %s", w.src.Name, w.src.Priority, w.entry.Value)))
			}
			continue
		}
		items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"))
	}

	// Show-only module mode: the listed modules stay allowed (written as
	// true so that they win over a lower-priority hide), every other
	// installed module is hidden.
	if src.Policy.GetKcmMode() == pb.KcmRestrictionMode_KCM_SHOW_ONLY_LISTED && len(src.Policy.GetKcmRestrictions()) > 0 {
		items = append(items, mergeShowOnlyKCMs(sc, src, installedKCMs))
	}

	// Application files: one Konsole profile per scope (highest priority
	// wins), hidden desktop entries combined from every policy.
	if p := src.Policy.GetKonsoleProfile(); KonsoleProfileHasContent(p) {
		items = append(items, mergeKonsoleProfile(sc, src, p)...)
	}
	items = append(items, mergeHiddenApplications(sc, src)...)

	if src.Policy.GetFileImmutableKdeglobals() {
		sc.fileImmutable[KConfigFileGlobals] = true
		items = append(items, &pb.ComplianceItemResult{
			SchemaId: sc.prefix + KConfigFileGlobals,
			Key:      "[$i]",
			Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message:  "Whole file marked immutable",
		})
	}
	return items
}

// mergeKonsoleProfile records the scope's Konsole profile (first policy in
// priority order) and claims the entries that select it.
func mergeKonsoleProfile(sc *kconfigScope, src KConfigPolicySource, p *pb.KonsoleProfile) []*pb.ComplianceItemResult {
	file := konsoleProfileDir + "/" + KonsoleProfileFile(p)
	if sc.apps.KonsoleProfile != nil {
		return []*pb.ComplianceItemResult{{
			SchemaId: sc.prefix + file,
			Key:      "profile",
			Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
			Message:  fmt.Sprintf("Overridden by higher-priority policy %q, which provides the Konsole profile %s", sc.konsoleSrc.Name, KonsoleProfileFile(sc.apps.KonsoleProfile)),
		}}
	}
	sc.apps.KonsoleProfile = p
	sc.apps.KonsoleProfilePolicy = src.Name
	sc.konsoleSrc = src

	msg := "Deployed Konsole profile " + KonsoleProfileName(p) + " (" + historyModeLabel(p.GetHistoryMode()) + ")"
	items := []*pb.ComplianceItemResult{{SchemaId: sc.prefix + file, Key: "profile", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: msg}}
	for _, e := range KonsoleProfileEntries(p) {
		if w, ok := sc.claim(e, src); !ok {
			if sameKConfigEffect(w.entry, e) {
				items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, fmt.Sprintf("Same value as higher-priority policy %q", w.src.Name)))
			} else {
				items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
					fmt.Sprintf("Overridden by higher-priority policy %q (priority %d); applied value: %s", w.src.Name, w.src.Priority, w.entry.Value)))
			}
			continue
		}
		items = append(items, sc.item(e, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"))
	}
	return items
}

// mergeHiddenApplications adds the policy's hidden desktop entries to the
// scope. Ids are combined across policies; an invalid id is reported.
func mergeHiddenApplications(sc *kconfigScope, src KConfigPolicySource) []*pb.ComplianceItemResult {
	var items []*pb.ComplianceItemResult
	seen := make(map[string]bool)
	for _, raw := range src.Policy.GetHiddenApplications() {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		item := &pb.ComplianceItemResult{SchemaId: sc.prefix + applicationsDir + "/" + id, Key: "NoDisplay"}
		switch {
		case !ValidDesktopFileID(id):
			item.Status = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
			item.Message = "not a desktop file id (expected something like org.kde.konsole.desktop)"
		case sc.hiddenBy[id] != "":
			item.Status = pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
			item.Message = fmt.Sprintf("Hidden from menus and launchers (also by policy %q)", sc.hiddenBy[id])
		default:
			sc.hiddenBy[id] = src.Name
			sc.apps.HiddenApplications = append(sc.apps.HiddenApplications, id)
			item.Status = pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
			item.Message = "Hidden from menus and launchers"
		}
		items = append(items, item)
	}
	return items
}

// mergeShowOnlyKCMs expands a show-only module list against the installed
// modules and returns the summary compliance item for it.
func mergeShowOnlyKCMs(sc *kconfigScope, src KConfigPolicySource, installedKCMs []string) *pb.ComplianceItemResult {
	summary := &pb.ComplianceItemResult{
		SchemaId: sc.prefix + KConfigFileGlobals,
		Key:      "[" + KConfigGroupKCMRestrictions + "] show only listed modules",
	}
	if len(installedKCMs) == 0 {
		summary.Status = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
		summary.Message = "the list of installed System Settings modules is not available on this node; show-only mode was not applied"
		return summary
	}

	allowed := make(map[string]bool, len(src.Policy.GetKcmRestrictions()))
	for _, mod := range src.Policy.GetKcmRestrictions() {
		allowed[mod] = true
	}
	hidden := 0
	for _, mod := range installedKCMs {
		value := "false"
		if allowed[mod] {
			value = "true"
		}
		e := &pb.KConfigEntry{File: KConfigFileGlobals, Group: KConfigGroupKCMRestrictions, Key: mod, Value: value, Type: "bool", Enforced: true}
		if validateINIEntry(e) != nil {
			continue
		}
		if _, ok := sc.claim(e, src); ok && value == "false" {
			hidden++
		}
	}
	summary.Status = pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
	summary.Message = fmt.Sprintf("Hiding %d of %d installed modules; %d listed modules stay visible", hidden, len(installedKCMs), len(allowed))
	return summary
}

// localePattern matches KConfig locale suffixes such as de, de_DE or sr@latin.
var localePattern = regexp.MustCompile(`^[a-z]{2,3}(_[A-Z]{2})?(@[a-z]+)?$`)

// validateINIEntry rejects entries that would corrupt the rendered INI file
// or escape the managed directory. Server-side validation performs the same
// checks; the agent treats server data as untrusted.
func validateINIEntry(e *pb.KConfigEntry) error {
	if e.File == "" || strings.HasPrefix(e.File, ".") || strings.ContainsAny(e.File, "/\\\x00") {
		return fmt.Errorf("invalid config file name %q", e.File)
	}
	if e.Group == "" || strings.ContainsAny(e.Group, "\r\n") {
		return fmt.Errorf("invalid group %q for key %q", e.Group, e.Key)
	}
	if e.Key == "" || strings.ContainsAny(e.Key, "=[]\r\n") {
		return fmt.Errorf("key %q contains characters that are not allowed in a KConfig key", e.Key)
	}
	if strings.ContainsAny(e.Value, "\r\n") {
		return fmt.Errorf("value for %q contains a line break and was not written", e.Key)
	}
	if e.Locale != "" && !localePattern.MatchString(e.Locale) {
		return fmt.Errorf("invalid locale %q for key %q", e.Locale, e.Key)
	}
	return nil
}

// Where System Settings modules announce themselves. Every module ships a
// kcm_*.desktop entry and/or a plugin under plasma/kcms/<category>/.
var (
	kcmDesktopDirs = []string{"/usr/share/applications", "/usr/local/share/applications"}
	kcmPluginGlobs = []string{
		"/usr/lib*/qt6/plugins/plasma/kcms/*/*.so",
		"/usr/lib/*/qt6/plugins/plasma/kcms/*/*.so",
		"/usr/lib*/qt/plugins/plasma/kcms/*/*.so",
		"/usr/lib*/qt5/plugins/plasma/kcms/*/*.so",
		"/usr/lib/*/qt5/plugins/plasma/kcms/*/*.so",
	}
)

// InstalledKCMs lists the System Settings module ids installed on this
// node (kcm_access, kcm_bluetooth, ...), sorted. Used to expand show-only
// module lists.
func InstalledKCMs() []string {
	return InstalledKCMsFrom(kcmDesktopDirs, kcmPluginGlobs)
}

// InstalledKCMsFrom is InstalledKCMs with injectable locations for tests.
func InstalledKCMsFrom(desktopDirs, pluginGlobs []string) []string {
	seen := make(map[string]bool)
	for _, dir := range desktopDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "kcm") && strings.HasSuffix(name, ".desktop") {
				seen[strings.TrimSuffix(name, ".desktop")] = true
			}
		}
	}
	for _, g := range pluginGlobs {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		for _, m := range matches {
			base := strings.TrimSuffix(filepath.Base(m), ".so")
			if strings.HasPrefix(base, "kcm") {
				seen[base] = true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// MergeKConfigEntries takes already-parsed proto entries (flattened from
// all policies), groups them by target file and INI group, renders INI
// content with [$i] enforcement suffixes, and returns a map of file->INI bytes.
func MergeKConfigEntries(entries []*pb.KConfigEntry) (map[string][]byte, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Group entries by file, then by group name within each file.
	// Use ordered maps to produce deterministic output.
	type fileData struct {
		groups map[string]*kconfigGroup
		order  []string // insertion order of group names
	}
	files := make(map[string]*fileData)
	var fileOrder []string

	for _, e := range entries {
		fd, ok := files[e.File]
		if !ok {
			fd = &fileData{groups: make(map[string]*kconfigGroup)}
			files[e.File] = fd
			fileOrder = append(fileOrder, e.File)
		}
		g, ok := fd.groups[e.Group]
		if !ok {
			g = &kconfigGroup{name: e.Group}
			fd.groups[e.Group] = g
			fd.order = append(fd.order, e.Group)
		}
		g.entries = append(g.entries, e)
	}

	sort.Strings(fileOrder)

	// Renumber URL restriction rules when multiple policies contribute
	// rule_N entries to the same [KDE URL Restrictions] group.
	for _, fd := range files {
		for _, g := range fd.groups {
			if g.name == KConfigGroupURLRestrictions {
				renumberURLRestrictions(g)
			}
		}
	}

	result := make(map[string][]byte, len(files))
	for _, fileName := range fileOrder {
		fd := files[fileName]
		var buf strings.Builder

		sortedGroups := make([]string, len(fd.order))
		copy(sortedGroups, fd.order)
		sort.Strings(sortedGroups)

		for i, groupName := range sortedGroups {
			g := fd.groups[groupName]
			if i > 0 {
				buf.WriteString("\n")
			}
			renderINIGroup(&buf, g)
		}

		result[fileName] = []byte(buf.String())
	}

	return result, nil
}

// renderINIGroup writes a single INI group to the builder. Immutability is
// always marked on the keys, never on the group: KConfig applies a
// group-level [$i] to every file parsed afterwards, so a locked group in
// the node-wide file would make KDE ignore the keys that group-scoped
// overlays add to the same group (a student policy's shell_access next to
// the baseline's run_command, for example).
func renderINIGroup(buf *strings.Builder, g *kconfigGroup) {
	fmt.Fprintf(buf, "[%s]\n", g.name)

	// Sort entries by key (then locale) for deterministic output.
	sorted := make([]*pb.KConfigEntry, len(g.entries))
	copy(sorted, g.entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Key != sorted[j].Key {
			return sorted[i].Key < sorted[j].Key
		}
		return sorted[i].Locale < sorted[j].Locale
	})

	for _, e := range sorted {
		buf.WriteString(renderINIKey(e, false))
		if e.Delete {
			// KConfig writes deleted entries as a bare key[$d] line.
			buf.WriteString("\n")
		} else {
			fmt.Fprintf(buf, "=%s\n", e.Value)
		}
	}
}

// renderINIKey renders "key", "key[de]" and the marker suffix: [$i] for
// immutable entries (unless the whole group is), [$e] for variable
// expansion and [$d] for deleted entries, combined as in key[$ie].
func renderINIKey(e *pb.KConfigEntry, groupImmutable bool) string {
	var sb strings.Builder
	sb.WriteString(e.Key)
	if e.Locale != "" {
		sb.WriteString("[" + e.Locale + "]")
	}
	flags := ""
	if e.Enforced && !groupImmutable {
		flags += "i"
	}
	if e.Expand {
		flags += "e"
	}
	if e.Delete {
		flags += "d"
	}
	if flags != "" {
		sb.WriteString("[$" + flags + "]")
	}
	return sb.String()
}

// BackupOriginal creates a backup of the original file before policy
// enforcement overwrites it. If a backup already exists it is never
// overwritten (idempotent). When no original file exists an empty
// sentinel backup is created so that cleanup knows to delete the
// managed file rather than restore content.
func BackupOriginal(targetPath string) error {
	backupPath := targetPath + BackupSuffix
	if _, err := os.Stat(backupPath); err == nil {
		return nil // backup already exists - never overwrite
	}

	data, err := os.ReadFile(targetPath) //nolint:gosec // G304: path comes from server-managed policy config
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read original file %s: %w", targetPath, err)
		}
		// No original file - write empty sentinel so cleanup can delete.
		data = nil
	}

	if err := WriteFileAtomically(backupPath, data); err != nil {
		return fmt.Errorf("failed to write backup %s: %w", backupPath, err)
	}
	return nil
}

// RestoreOriginal restores the original file from its backup. If the
// backup is an empty sentinel the managed file is deleted (no original
// existed). If no backup exists the call is a no-op.
func RestoreOriginal(targetPath string) error {
	backupPath := targetPath + BackupSuffix

	data, err := os.ReadFile(backupPath) //nolint:gosec // G304: backup path derived from managed config path
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to restore
		}
		return fmt.Errorf("failed to read backup %s: %w", backupPath, err)
	}

	if len(data) == 0 {
		// Empty sentinel - no original existed; remove the managed file.
		_ = os.Remove(targetPath)
	} else {
		if err := WriteFileAtomically(targetPath, data); err != nil {
			return fmt.Errorf("failed to restore %s: %w", targetPath, err)
		}
	}

	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("failed to remove backup %s: %w", backupPath, err)
	}
	return nil
}

// ManagedFiles scans basePath for .bor-backup files and returns the
// base filenames (without the suffix) that are currently managed.
func ManagedFiles(basePath string) ([]string, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", basePath, err)
	}

	var managed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, BackupSuffix) {
			managed = append(managed, strings.TrimSuffix(name, BackupSuffix))
		}
	}
	return managed, nil
}

// SyncKConfigFiles synchronises the managed KConfig files under basePath
// with the desired state expressed by files. Files present in the desired
// state are backed up (first write only) and written with a managed
// header. Files that were previously managed but are no longer in the
// desired state are restored from their backups.
//
// Passing a nil or empty files map causes all previously managed files
// to be restored (full cleanup).
func SyncKConfigFiles(basePath string, files map[string][]byte) error {
	managed, err := ManagedFiles(basePath)
	if err != nil {
		return err
	}

	// Build set of previously managed filenames.
	prev := make(map[string]bool, len(managed))
	for _, name := range managed {
		prev[name] = true
	}

	// Write desired files.
	for name, data := range files {
		target := filepath.Join(basePath, name)
		if err := BackupOriginal(target); err != nil {
			return fmt.Errorf("failed to backup %s: %w", name, err)
		}

		withHeader := append([]byte(ManagedFileHeader), data...)
		if err := WriteFileAtomically(target, withHeader); err != nil {
			return fmt.Errorf("failed to write KConfig file %s: %w", name, err)
		}

		delete(prev, name) // still active - don't restore
	}

	// Restore files that are no longer in the desired state.
	for name := range prev {
		target := filepath.Join(basePath, name)
		if err := RestoreOriginal(target); err != nil {
			return fmt.Errorf("failed to restore %s: %w", name, err)
		}
	}

	return nil
}

// parseRuleNum extracts the numeric index from a "rule_N" key.
// Returns -1 if the key does not match the pattern.
func parseRuleNum(key string) int {
	if !strings.HasPrefix(key, "rule_") {
		return -1
	}
	n, err := strconv.Atoi(key[len("rule_"):])
	if err != nil {
		return -1
	}
	return n
}

// renumberURLRestrictions collects all rule_N entries in a
// [KDE URL Restrictions] group, renumbers them sequentially starting
// from rule_1, and sets a single rule_count entry with the total.
// Non-rule entries (other than rule_count) are preserved.
func renumberURLRestrictions(g *kconfigGroup) {
	type indexedRule struct {
		origNum int
		entry   *pb.KConfigEntry
	}

	var rules []indexedRule
	var other []*pb.KConfigEntry

	for _, e := range g.entries {
		if e.Key == "rule_count" {
			continue // drop old rule_count - we'll regenerate it
		}
		n := parseRuleNum(e.Key)
		if n > 0 {
			rules = append(rules, indexedRule{origNum: n, entry: e})
		} else {
			other = append(other, e)
		}
	}

	// Stable sort by original index so that rules from different
	// policies with the same index maintain insertion order.
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].origNum < rules[j].origNum
	})

	// Renumber sequentially.
	result := make([]*pb.KConfigEntry, 0, len(other)+len(rules)+1)
	result = append(result, other...)
	for i, r := range rules {
		r.entry.Key = fmt.Sprintf("rule_%d", i+1)
		result = append(result, r.entry)
	}

	// Add rule_count if there are any rules.
	if len(rules) > 0 {
		result = append(result, &pb.KConfigEntry{
			File:     g.entries[0].File,
			Group:    g.name,
			Key:      "rule_count",
			Value:    strconv.Itoa(len(rules)),
			Enforced: g.entries[0].Enforced,
		})
	}

	g.entries = result
}

// kde5rcPath is the global rc file that KConfig (KF5 and KF6) parses before
// kdeglobals. Writing the control-module restrictions there is optional: the
// copy in kdeglobals is what KAuthorized reads on every supported Plasma.
var kde5rcPath = "/etc/kde5rc"

// legacyKde6rcPath was written by agent versions before 0.11. KDE never
// reads it; the agent restores it (when a Bor backup exists) on every sync.
var legacyKde6rcPath = "/etc/kde6rc"

// KCMRestrictionsForKde5rc returns copies of the control-module restriction
// entries among entries, re-targeted at kde5rc, so that they can be rendered
// into /etc/kde5rc with MergeKConfigEntries.
func KCMRestrictionsForKde5rc(entries []*pb.KConfigEntry) []*pb.KConfigEntry {
	var out []*pb.KConfigEntry
	for _, e := range entries {
		if e.File != KConfigFileGlobals || e.Group != KConfigGroupKCMRestrictions {
			continue
		}
		out = append(out, &pb.KConfigEntry{
			File:     "kde5rc",
			Group:    e.Group,
			Key:      e.Key,
			Value:    e.Value,
			Type:     e.Type,
			Enforced: e.Enforced,
		})
	}
	return out
}

// SyncKde5rc mirrors the rendered control-module restrictions into
// /etc/kde5rc when content is non-nil and restores the original file
// otherwise (no-op when Bor never wrote it). It always restores the legacy
// /etc/kde6rc that older agents created.
func SyncKde5rc(content []byte) error {
	if err := RestoreOriginal(legacyKde6rcPath); err != nil {
		return fmt.Errorf("failed to restore %s: %w", legacyKde6rcPath, err)
	}

	if content == nil {
		if err := RestoreOriginal(kde5rcPath); err != nil {
			return fmt.Errorf("failed to restore %s: %w", kde5rcPath, err)
		}
		return nil
	}

	if err := BackupOriginal(kde5rcPath); err != nil {
		return fmt.Errorf("failed to backup %s: %w", kde5rcPath, err)
	}
	withHeader := append([]byte(ManagedFileHeader), content...)
	if err := WriteFileAtomically(kde5rcPath, withHeader); err != nil {
		return fmt.Errorf("failed to write %s: %w", kde5rcPath, err)
	}
	return nil
}

// Kde5rcPath returns the optional /etc/kde5rc mirror path.
func Kde5rcPath() string { return kde5rcPath }

// LegacyKde6rcPath returns the /etc/kde6rc path older agents wrote.
func LegacyKde6rcPath() string { return legacyKde6rcPath }

// Login hook scripts that put the Bor directory first in XDG_CONFIG_DIRS.
//
// plasma-workspace/env/*.sh is Plasma's own startup hook: startplasma
// sources every such script found through the XDG config directories
// before the session starts and imports the result into the systemd user
// session, so it works regardless of the display manager. profile.d covers
// login shells and display managers whose session wrappers source
// /etc/profile (SDDM does, for X11 and Wayland).
var (
	profileScriptPath   = "/etc/profile.d/99-bor.sh"
	plasmaEnvScriptPath = "/etc/xdg/plasma-workspace/env/00-bor.sh"
)

// EnvScriptPaths returns the login hook scripts the agent maintains.
func EnvScriptPaths() []string {
	return []string{profileScriptPath, plasmaEnvScriptPath}
}

// envScriptContent returns the POSIX shell snippet that prepends configPath
// to XDG_CONFIG_DIRS and dataPath to XDG_DATA_DIRS exactly once, however
// many hooks run it. groups are the per-group overlay directories, highest
// priority first; each is added before the node-wide directory for members
// of the group (matched on numeric GIDs from `id -G`, so group names never
// reach the shell).
func envScriptContent(configPath, dataPath string, groups []GroupDir) string {
	var b strings.Builder
	b.WriteString(`# Managed by Bor. Do not edit manually.
# Puts the Bor policy directories first in XDG_CONFIG_DIRS and XDG_DATA_DIRS
# so that KDE and other XDG-aware applications read the managed
# configuration files, look-and-feel packages and Plasma scripts.
`)
	// Nodes without group-scoped policies get the plain snippet; the group
	// logic (and its helper variables) only appears when needed.
	configPrefix, configCleanup, dataPrefix, dataCleanup := "", "", "", ""
	if len(groups) > 0 {
		b.WriteString("# Group-scoped policy directories, highest priority first.\n")
		b.WriteString("bor_group_dirs=\"\"\n")
		b.WriteString("bor_group_data=\"\"\n")
		b.WriteString("bor_gids=\" $(id -G 2>/dev/null) \"\n")
		for _, g := range groups {
			fmt.Fprintf(&b, "case \"$bor_gids\" in *\" %d \"*) bor_group_dirs=\"${bor_group_dirs}%s:\"; bor_group_data=\"${bor_group_data}%s:\" ;; esac\n", g.GID, g.Dir, g.DataDir)
		}
		b.WriteString("unset bor_gids\n")
		configPrefix, configCleanup = "${bor_group_dirs}", "unset bor_group_dirs\n"
		dataPrefix, dataCleanup = "${bor_group_data}", "unset bor_group_data\n"
	}
	// The data directory goes first (it shadows desktop entries and holds
	// the look-and-feel package); its "late" subdirectory goes last, so the
	// Plasma update scripts in it are found without shadowing the shell
	// package (see kconfig_layout.go).
	fmt.Fprintf(&b, `case ":${XDG_CONFIG_DIRS:-/etc/xdg}:" in
  *":%[1]s:"*) ;;
  *) export XDG_CONFIG_DIRS="%[3]s%[1]s:${XDG_CONFIG_DIRS:-/etc/xdg}" ;;
esac
%[4]scase ":${XDG_DATA_DIRS:-/usr/local/share:/usr/share}:" in
  *":%[2]s:"*) ;;
  *) export XDG_DATA_DIRS="%[5]s%[2]s:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}:%[2]s/%[7]s" ;;
esac
%[6]s`, configPath, dataPath, configPrefix, configCleanup, dataPrefix, dataCleanup, LateDataDir)
	return b.String()
}

// EnsureEnvScripts creates or updates the login hook scripts so that
// configPath (and the group directories for members) is prepended to
// XDG_CONFIG_DIRS and dataPath to XDG_DATA_DIRS for every Plasma session
// and login shell.
func EnsureEnvScripts(configPath, dataPath string, groups []GroupDir) error {
	desired := []byte(envScriptContent(configPath, dataPath, groups))
	for _, path := range EnvScriptPaths() {
		if err := ensureScript(path, desired); err != nil {
			return err
		}
	}
	return nil
}

// ensureScript writes desired to path (mode 0755) unless it is already
// identical.
func ensureScript(path string, desired []byte) error {
	existing, err := os.ReadFile(path) //nolint:gosec // G304: fixed hook script paths
	if err == nil && bytes.Equal(existing, desired) {
		return nil // already up to date
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: hook directories must be world-readable
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	if err := WriteFileAtomically(path, desired); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	// Hook scripts are sourced, but keep them executable like the rest of
	// profile.d.
	if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // G302: hook script must be world-readable
		return fmt.Errorf("failed to chmod %s: %w", path, err)
	}

	return nil
}
