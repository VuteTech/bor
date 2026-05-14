// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/godbus/dbus/v5"
)

// PackageOptions controls how package installation is performed.
type PackageOptions struct {
	AllowDowngrade bool
}

// MergePackageEntries deduplicates PackageEntry slices by name.
// Entries are expected sorted ascending by priority; later (higher-priority)
// entries overwrite earlier ones for the same name.
func MergePackageEntries(policies []*pb.PackagePolicy) []*pb.PackageEntry {
	seen := make(map[string]*pb.PackageEntry)
	var order []string
	for _, pol := range policies {
		if pol == nil {
			continue
		}
		for _, pkg := range pol.GetPackages() {
			if _, exists := seen[pkg.GetName()]; !exists {
				order = append(order, pkg.GetName())
			}
			seen[pkg.GetName()] = pkg
		}
	}
	result := make([]*pb.PackageEntry, 0, len(order))
	for _, name := range order {
		result = append(result, seen[name])
	}
	return result
}

// ApplyPackages enforces the desired state for each package entry via
// PackageKit D-Bus. For each entry it:
//  1. Checks current installation state (non-destructive Resolve call).
//  2. If a delta exists (install/remove/upgrade), performs the operation.
//  3. Re-checks to confirm and records the compliance result.
func ApplyPackages(
	ctx context.Context,
	conn *dbus.Conn,
	entries []*pb.PackageEntry,
	opts PackageOptions,
) []ComplianceItem {
	items := make([]ComplianceItem, 0, len(entries))
	for _, entry := range entries {
		item := applyPackageEntry(ctx, conn, entry, opts)
		items = append(items, item)
	}
	return items
}

// CheckPackageCompliance checks the current installation state of each
// entry without making any changes. Used for periodic re-checks.
func CheckPackageCompliance(
	ctx context.Context,
	conn *dbus.Conn,
	entries []*pb.PackageEntry,
) []ComplianceItem {
	items := make([]ComplianceItem, 0, len(entries))
	for _, entry := range entries {
		item := checkPackageEntry(ctx, conn, entry)
		items = append(items, item)
	}
	return items
}

// applyPackageEntry enforces the desired state of a single package entry.
func applyPackageEntry(ctx context.Context, conn *dbus.Conn, entry *pb.PackageEntry, opts PackageOptions) ComplianceItem {
	name := entry.GetName()
	key := "pkg:" + name

	switch entry.GetState() {
	case pb.PackageState_PACKAGE_STATE_PRESENT:
		return applyPresent(ctx, conn, key, name, entry.GetVersion(), entry.GetOptional(), opts)

	case pb.PackageState_PACKAGE_STATE_ABSENT:
		return applyAbsent(ctx, conn, key, name)

	case pb.PackageState_PACKAGE_STATE_LATEST:
		return applyLatest(ctx, conn, key, name, entry.GetOptional())

	default:
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: fmt.Sprintf("unknown package state %d", entry.GetState()),
		}
	}
}

// applyPresent ensures a package is installed, optionally at a pinned version.
func applyPresent(ctx context.Context, conn *dbus.Conn, key, name, version string, optional bool, opts PackageOptions) ComplianceItem {
	installedID, found, err := ResolvePackage(ctx, conn, name, PkFilterInstalled)
	if err != nil {
		return errItem(key, err)
	}

	if found {
		installedVer := ExtractVersionFromPackageID(installedID)
		if version == "" || installedVer == version {
			return ComplianceItem{
				Key:     key,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
				Message: "installed: " + installedVer,
			}
		}
		// Version mismatch.
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
			Message: fmt.Sprintf("version mismatch: expected %s, installed %s", version, installedVer),
		}
	}

	// Package not installed — try to install it.
	availID, avFound, err := ResolvePackage(ctx, conn, name, PkFilterNone)
	if err != nil {
		return errItem(key, err)
	}
	if !avFound {
		return notFoundItem(key, name, optional)
	}

	if installErr := InstallPackage(ctx, conn, availID, opts.AllowDowngrade); installErr != nil {
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: installErr.Error(),
		}
	}

	// Confirm installation.
	newID, confirmed, _ := ResolvePackage(ctx, conn, name, PkFilterInstalled)
	if !confirmed {
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: "installation appeared to succeed but package not found afterwards",
		}
	}
	return ComplianceItem{
		Key:     key,
		Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		Message: "installed: " + ExtractVersionFromPackageID(newID),
	}
}

// applyAbsent ensures a package is not installed.
func applyAbsent(ctx context.Context, conn *dbus.Conn, key, name string) ComplianceItem {
	installedID, found, err := ResolvePackage(ctx, conn, name, PkFilterInstalled)
	if err != nil {
		return errItem(key, err)
	}
	if !found {
		return ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT}
	}

	if removeErr := RemovePackage(ctx, conn, installedID); removeErr != nil {
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: removeErr.Error(),
		}
	}

	// Confirm removal.
	_, stillInstalled, _ := ResolvePackage(ctx, conn, name, PkFilterInstalled)
	if stillInstalled {
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: "removal requested but package still detected as installed",
		}
	}
	return ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT}
}

// applyLatest ensures a package is installed at the latest available version.
func applyLatest(ctx context.Context, conn *dbus.Conn, key, name string, optional bool) ComplianceItem {
	installedID, installed, err := ResolvePackage(ctx, conn, name, PkFilterInstalled)
	if err != nil {
		return errItem(key, err)
	}
	availID, avFound, err := ResolvePackage(ctx, conn, name, PkFilterAvailable)
	if err != nil {
		return errItem(key, err)
	}

	if !installed && !avFound {
		return notFoundItem(key, name, optional)
	}

	if !installed {
		// Not installed at all — install latest.
		if installErr := InstallPackage(ctx, conn, availID, false); installErr != nil {
			return ComplianceItem{
				Key:     key,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message: installErr.Error(),
			}
		}
		newID, _, _ := ResolvePackage(ctx, conn, name, PkFilterInstalled)
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: "installed: " + ExtractVersionFromPackageID(newID),
		}
	}

	if !avFound {
		// Installed and no update available — compliant.
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: "installed: " + ExtractVersionFromPackageID(installedID) + " (no update available)",
		}
	}

	installedVer := ExtractVersionFromPackageID(installedID)
	latestVer := ExtractVersionFromPackageID(availID)

	if installedVer == latestVer {
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: "installed: " + installedVer,
		}
	}

	// Upgrade available.
	if updateErr := UpdatePackage(ctx, conn, installedID); updateErr != nil {
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
			Message: updateErr.Error(),
		}
	}
	newID, _, _ := ResolvePackage(ctx, conn, name, PkFilterInstalled)
	return ComplianceItem{
		Key:     key,
		Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		Message: "updated to: " + ExtractVersionFromPackageID(newID),
	}
}

// checkPackageEntry checks the current state of a single package without
// making any changes. Used for compliance-only re-checks.
func checkPackageEntry(ctx context.Context, conn *dbus.Conn, entry *pb.PackageEntry) ComplianceItem {
	name := entry.GetName()
	key := "pkg:" + name

	switch entry.GetState() {
	case pb.PackageState_PACKAGE_STATE_PRESENT:
		installedID, found, err := ResolvePackage(ctx, conn, name, PkFilterInstalled)
		if err != nil {
			return errItem(key, err)
		}
		if !found {
			return notFoundItem(key, name, entry.GetOptional())
		}
		installedVer := ExtractVersionFromPackageID(installedID)
		if entry.GetVersion() != "" && installedVer != entry.GetVersion() {
			return ComplianceItem{
				Key:    key,
				Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: fmt.Sprintf("version mismatch: expected %s, installed %s",
					entry.GetVersion(), installedVer),
			}
		}
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: "installed: " + installedVer,
		}

	case pb.PackageState_PACKAGE_STATE_ABSENT:
		_, found, err := ResolvePackage(ctx, conn, name, PkFilterInstalled)
		if err != nil {
			return errItem(key, err)
		}
		if found {
			return ComplianceItem{
				Key:     key,
				Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: "package is installed but state=absent",
			}
		}
		return ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT}

	case pb.PackageState_PACKAGE_STATE_LATEST:
		installedID, installed, err := ResolvePackage(ctx, conn, name, PkFilterInstalled)
		if err != nil {
			return errItem(key, err)
		}
		if !installed {
			return notFoundItem(key, name, entry.GetOptional())
		}
		availID, avFound, _ := ResolvePackage(ctx, conn, name, PkFilterAvailable)
		if avFound && ExtractVersionFromPackageID(installedID) != ExtractVersionFromPackageID(availID) {
			return ComplianceItem{
				Key:    key,
				Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message: fmt.Sprintf("update available: installed %s, latest %s",
					ExtractVersionFromPackageID(installedID), ExtractVersionFromPackageID(availID)),
			}
		}
		return ComplianceItem{
			Key:     key,
			Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			Message: "installed: " + ExtractVersionFromPackageID(installedID),
		}
	}

	return ComplianceItem{
		Key:     key,
		Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
		Message: fmt.Sprintf("unknown package state %d", entry.GetState()),
	}
}

// RollupPackageCompliance derives an overall compliance status from a slice
// of per-item results, following the same logic as DConf and Polkit.
func RollupPackageCompliance(items []ComplianceItem) (status pb.ComplianceStatus, message string) {
	if len(items) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_UNKNOWN, ""
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
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "all package entries are inapplicable on this node"
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, ""
	}
}

func errItem(key string, err error) ComplianceItem {
	return ComplianceItem{
		Key:     key,
		Status:  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
		Message: err.Error(),
	}
}

func notFoundItem(key, name string, optional bool) ComplianceItem {
	msg := fmt.Sprintf("package %q not found in any enabled repository", name)
	status := pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
	if optional {
		status = pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
	}
	return ComplianceItem{Key: key, Status: status, Message: msg}
}
