// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Default locations for Flatpak enforcement state.
const (
	// FlatpakStateDir holds the Bor-rendered .flatpakrepo, .filter and .gpg
	// files that back every managed remote. Files here are tamper-protected.
	FlatpakStateDir = "/etc/bor/flatpak"
	// FlatpakStateFile records which remotes Bor created or adopted so that
	// policy removal can undo exactly what Bor did.
	FlatpakStateFile = "/var/lib/bor/agent/flatpak-state.json"

	flatpakDefaultOperationTimeout = 30 * time.Minute
	flatpakDefaultUpdateInterval   = 24 * time.Hour
	flatpakListTimeout             = 60 * time.Second
)

// Identifier rules shared with the server (server/internal/services/flatpak.go).
// Every value that ends up on a flatpak command line or in a file name must
// satisfy one of these before use.
var (
	flatpakRemoteNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	flatpakAppIDRE      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{5,254}$`)
	flatpakBranchRE     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	flatpakFilterRefRE  = regexp.MustCompile(`^[A-Za-z0-9._*/-]{1,256}$`)
)

// ValidateFlatpakRemoteName reports whether a server-supplied remote name is
// safe to use as a single path component and as a flatpak argument.
func ValidateFlatpakRemoteName(name string) error {
	if err := validatePathIdentifier("remote name", name); err != nil {
		return err
	}
	if !flatpakRemoteNameRE.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid remote name %q: must match [A-Za-z0-9][A-Za-z0-9._-]* and be at most 64 characters", name)
	}
	return nil
}

// ValidateFlatpakAppID reports whether a server-supplied application ID is a
// well-formed reverse-DNS identifier safe to place on a command line.
func ValidateFlatpakAppID(id string) error {
	if id == "" {
		return fmt.Errorf("app id must not be empty")
	}
	if !flatpakAppIDRE.MatchString(id) || strings.Contains(id, "..") || !strings.Contains(id, ".") {
		return fmt.Errorf("invalid app id %q: expected a reverse-DNS identifier such as org.mozilla.firefox", id)
	}
	return nil
}

// ValidateFlatpakBranch reports whether a branch name is safe to use.
func ValidateFlatpakBranch(branch string) error {
	if branch == "" {
		return nil
	}
	if !flatpakBranchRE.MatchString(branch) || strings.Contains(branch, "..") {
		return fmt.Errorf("invalid branch %q", branch)
	}
	return nil
}

// ValidateFlatpakFilterRef reports whether a filter ref glob is safe to write
// into a filter file.
func ValidateFlatpakFilterRef(ref string) error {
	if !flatpakFilterRefRE.MatchString(ref) || strings.Contains(ref, "..") {
		return fmt.Errorf("invalid filter ref %q", ref)
	}
	return nil
}

// FlatpakEntry pairs a FlatpakPolicy with its binding priority so merged
// scalars can be resolved highest-priority-wins.
type FlatpakEntry struct {
	Priority int32
	Policy   *pb.FlatpakPolicy
}

// FlatpakDesired is the merged desired state across every bound policy.
type FlatpakDesired struct {
	Remotes            []*pb.FlatpakRemoteEntry
	Apps               []*pb.FlatpakAppEntry
	Installation       string
	AutoUpdate         bool
	AutoUpdateInterval time.Duration
	UninstallUnused    bool
	OperationTimeout   time.Duration
}

// Empty reports whether nothing is desired (no remotes and no apps).
func (d *FlatpakDesired) Empty() bool {
	return len(d.Remotes) == 0 && len(d.Apps) == 0
}

// MergeFlatpakEntries merges bound policies into one desired state. Entries
// are sorted ascending by priority so later (higher-priority) entries win:
// remotes are deduplicated by name, apps by (app_id, scope); order is
// preserved first-seen. auto_update and uninstall_unused are OR-ed;
// installation, the update interval and the operation timeout come from the
// highest-priority policy that sets them.
func MergeFlatpakEntries(entries []FlatpakEntry) FlatpakDesired {
	sorted := make([]FlatpakEntry, 0, len(entries))
	for _, e := range entries {
		if e.Policy != nil {
			sorted = append(sorted, e)
		}
	}
	slices.SortStableFunc(sorted, func(a, b FlatpakEntry) int {
		return cmp.Compare(a.Priority, b.Priority)
	})

	d := FlatpakDesired{
		AutoUpdateInterval: flatpakDefaultUpdateInterval,
		OperationTimeout:   flatpakDefaultOperationTimeout,
	}
	remotes := make(map[string]*pb.FlatpakRemoteEntry)
	var remoteOrder []string
	apps := make(map[string]*pb.FlatpakAppEntry)
	var appOrder []string

	for _, e := range sorted {
		pol := e.Policy
		for _, r := range pol.GetRemotes() {
			if r == nil {
				continue
			}
			if _, seen := remotes[r.GetName()]; !seen {
				remoteOrder = append(remoteOrder, r.GetName())
			}
			remotes[r.GetName()] = r
		}
		for _, a := range pol.GetApps() {
			if a == nil {
				continue
			}
			key := a.GetAppId() + "|" + flatpakScopeKey(a.GetScope())
			if _, seen := apps[key]; !seen {
				appOrder = append(appOrder, key)
			}
			apps[key] = a
		}
		if pol.GetAutoUpdate() {
			d.AutoUpdate = true
		}
		if pol.GetUninstallUnused() {
			d.UninstallUnused = true
		}
		if inst := pol.GetInstallation(); inst != "" {
			d.Installation = inst
		}
		if h := pol.GetAutoUpdateIntervalHours(); h > 0 {
			d.AutoUpdateInterval = time.Duration(h) * time.Hour
		}
		if m := pol.GetOperationTimeoutMinutes(); m > 0 {
			d.OperationTimeout = time.Duration(m) * time.Minute
		}
	}

	for _, name := range remoteOrder {
		d.Remotes = append(d.Remotes, remotes[name])
	}
	for _, key := range appOrder {
		d.Apps = append(d.Apps, apps[key])
	}
	return d
}

func flatpakScopeKey(s pb.FlatpakScope) string {
	if s == pb.FlatpakScope_FLATPAK_SCOPE_USER {
		return "user"
	}
	return "system"
}

// FlatpakInapplicableError signals that Flatpak policies cannot be applied on
// this node (flatpak missing, installation unknown). Callers map it to
// COMPLIANCE_STATUS_INAPPLICABLE.
type FlatpakInapplicableError struct {
	Reason string
}

func (e *FlatpakInapplicableError) Error() string { return e.Reason }

// Compliance item key prefixes. main.go splits the key on the first ':' to
// derive the ComplianceItemResult schema_id ("flatpak:<kind>") and key.
const (
	flatpakItemRemote = "remote:"
	flatpakItemApp    = "app:"
)

// RollupFlatpakCompliance derives the overall status from per-item results
// using the same rules as RollupPackageCompliance: all INAPPLICABLE →
// INAPPLICABLE, any ERROR → ERROR, any NON_COMPLIANT → NON_COMPLIANT,
// otherwise COMPLIANT.
func RollupFlatpakCompliance(items []ComplianceItem) (status pb.ComplianceStatus, message string) {
	if len(items) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"
	}

	allInapplicable := true
	var hasError, hasNonCompliant bool
	var msgs []string

	for _, it := range items {
		switch it.Status {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
			// stays inapplicable
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			allInapplicable = false
			hasError = true
			msgs = append(msgs, it.Key+": "+it.Message)
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			allInapplicable = false
			hasNonCompliant = true
			msgs = append(msgs, it.Key+": "+it.Message)
		default:
			allInapplicable = false
		}
	}

	switch {
	case allInapplicable:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "all flatpak entries are inapplicable on this node"
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"
	}
}

// SplitFlatpakItemKey splits a ComplianceItem key such as "app:org.x.App"
// into the schema id ("flatpak:app") and the bare key ("org.x.App").
func SplitFlatpakItemKey(key string) (schemaID, bare string) {
	kind, rest, ok := strings.Cut(key, ":")
	if !ok {
		return "flatpak", key
	}
	return "flatpak:" + kind, rest
}
