// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package policy — PackageKit D-Bus client.
//
// All package operations go through the PackageKit system daemon
// (org.freedesktop.PackageKit) rather than calling apt-get/dnf/zypper
// directly. PackageKit abstracts the underlying package manager and handles
// privilege escalation via PolicyKit.
//
// Transaction lifecycle:
//  1. Call CreateTransaction() on the manager object → get transaction path.
//  2. Subscribe to signals scoped to that path.
//  3. Call the operation method on the transaction object.
//  4. Drain Package / ErrorCode signals until Finished arrives.
//  5. Return collected results.
package policy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	pkService   = "org.freedesktop.PackageKit"
	pkObjPath   = "/org/freedesktop/PackageKit"
	pkIface     = "org.freedesktop.PackageKit"
	pkTxIface   = "org.freedesktop.PackageKit.Transaction"
	pkCallFlags = 0

	// PkFilterNone matches any package (installed or available).
	PkFilterNone uint64 = 0
	// PkFilterInstalled limits Resolve to installed packages only.
	PkFilterInstalled uint64 = 1 << 2 // 4
	// PkFilterAvailable limits Resolve to available (not installed) packages.
	PkFilterAvailable uint64 = 1 << 3 // 8

	// transaction flags (uint64)
	pkTxFlagNone           uint64 = 0
	pkTxFlagOnlyTrusted    uint64 = 1 << 1 // 2
	pkTxFlagAllowDowngrade uint64 = 1 << 6 // 64

	// exit codes (uint32)
	pkExitSuccess uint32 = 1
	pkExitFailed  uint32 = 2

	// error codes (uint32) — subset relevant to compliance mapping.
	pkErrPackageNotInstalled uint32 = 7
	pkErrPackageNotFound     uint32 = 8
)

// pkPackage is one item received from a Package D-Bus signal.
type pkPackage struct {
	Info      uint32
	PackageID string // "name;version;arch;data"
	Summary   string
}

// pkError is the payload from an ErrorCode D-Bus signal.
type pkError struct {
	Code    uint32
	Details string
}

// IsPackageKitAvailable returns true when the PackageKit daemon is either
// currently running or D-Bus activatable (i.e. will be auto-started on the
// first method call). PackageKit is idle-stopped after inactivity, so
// checking ListNames alone produces a false negative between uses; we
// additionally check ListActivatableNames so the guard only fails on nodes
// where PackageKit is genuinely absent.
func IsPackageKitAvailable(conn *dbus.Conn) bool {
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")

	var running []string
	if err := obj.Call("org.freedesktop.DBus.ListNames", 0).Store(&running); err == nil &&
		slices.Contains(running, pkService) {
		return true
	}

	var activatable []string
	if err := obj.Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false
	}
	return slices.Contains(activatable, pkService)
}

// ResolvePackage resolves a package name to a PackageKit package_id string.
// filter selects which packages to consider: PkFilterInstalled, PkFilterAvailable,
// or PkFilterNone (both). Returns ("", false, nil) when no match is found.
func ResolvePackage(ctx context.Context, conn *dbus.Conn, name string, filter uint64) (packageID string, found bool, err error) {
	pkgs, pkErr, exit, err := runTransaction(ctx, conn, func(txn dbus.BusObject) error {
		return txn.CallWithContext(ctx, pkTxIface+".Resolve", pkCallFlags, filter, []string{name}).Err
	})
	if err != nil {
		return "", false, fmt.Errorf("PackageKit Resolve(%q): %w", name, err)
	}
	if exit == pkExitFailed && pkErr != nil {
		if pkErr.Code == pkErrPackageNotFound || pkErr.Code == pkErrPackageNotInstalled {
			return "", false, nil
		}
		return "", false, fmt.Errorf("PackageKit Resolve(%q): error %d: %s", name, pkErr.Code, pkErr.Details)
	}
	for _, p := range pkgs {
		return p.PackageID, true, nil
	}
	return "", false, nil
}

// InstallPackage installs a package identified by its package_id.
// allowDowngrade sets pkTxFlagAllowDowngrade in the transaction flags.
func InstallPackage(ctx context.Context, conn *dbus.Conn, packageID string, allowDowngrade bool) error {
	flags := pkTxFlagOnlyTrusted
	if allowDowngrade {
		flags |= pkTxFlagAllowDowngrade
	}
	_, pkErr, exit, err := runTransaction(ctx, conn, func(txn dbus.BusObject) error {
		return txn.CallWithContext(ctx, pkTxIface+".InstallPackages", pkCallFlags, flags, []string{packageID}).Err
	})
	if err != nil {
		return fmt.Errorf("PackageKit InstallPackages: %w", err)
	}
	if exit != pkExitSuccess {
		if pkErr != nil {
			return fmt.Errorf("PackageKit InstallPackages: error %d: %s", pkErr.Code, pkErr.Details)
		}
		return fmt.Errorf("PackageKit InstallPackages: exit %d", exit)
	}
	return nil
}

// RemovePackage removes a package identified by its package_id.
func RemovePackage(ctx context.Context, conn *dbus.Conn, packageID string) error {
	_, pkErr, exit, err := runTransaction(ctx, conn, func(txn dbus.BusObject) error {
		// allow_deps=false, autoremove=false — conservative defaults.
		return txn.CallWithContext(ctx, pkTxIface+".RemovePackages", pkCallFlags,
			pkTxFlagNone, []string{packageID}, false, false).Err
	})
	if err != nil {
		return fmt.Errorf("PackageKit RemovePackages: %w", err)
	}
	if exit != pkExitSuccess {
		if pkErr != nil {
			return fmt.Errorf("PackageKit RemovePackages: error %d: %s", pkErr.Code, pkErr.Details)
		}
		return fmt.Errorf("PackageKit RemovePackages: exit %d", exit)
	}
	return nil
}

// UpdatePackage updates a package to the latest available version.
// packageID is the currently-installed package_id from ResolvePackage.
func UpdatePackage(ctx context.Context, conn *dbus.Conn, packageID string) error {
	_, pkErr, exit, err := runTransaction(ctx, conn, func(txn dbus.BusObject) error {
		return txn.CallWithContext(ctx, pkTxIface+".UpdatePackages", pkCallFlags,
			pkTxFlagOnlyTrusted, []string{packageID}).Err
	})
	if err != nil {
		return fmt.Errorf("PackageKit UpdatePackages: %w", err)
	}
	if exit != pkExitSuccess {
		if pkErr != nil {
			return fmt.Errorf("PackageKit UpdatePackages: error %d: %s", pkErr.Code, pkErr.Details)
		}
		return fmt.Errorf("PackageKit UpdatePackages: exit %d", exit)
	}
	return nil
}

// RefreshCache tells PackageKit to refresh its package metadata cache.
// Equivalent to apt-get update / dnf makecache / zypper refresh.
func RefreshCache(ctx context.Context, conn *dbus.Conn) error {
	_, pkErr, exit, err := runTransaction(ctx, conn, func(txn dbus.BusObject) error {
		return txn.CallWithContext(ctx, pkTxIface+".RefreshCache", pkCallFlags, false).Err
	})
	if err != nil {
		return fmt.Errorf("PackageKit RefreshCache: %w", err)
	}
	if exit != pkExitSuccess {
		if pkErr != nil {
			return fmt.Errorf("PackageKit RefreshCache: error %d: %s", pkErr.Code, pkErr.Details)
		}
		return fmt.Errorf("PackageKit RefreshCache: exit %d", exit)
	}
	return nil
}

// ExtractVersionFromPackageID returns the version segment from a PackageKit
// package_id string of the form "name;version;arch;data".
func ExtractVersionFromPackageID(packageID string) string {
	parts := strings.SplitN(packageID, ";", 4)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// runTransaction is the internal helper that manages the full PackageKit
// transaction lifecycle: create → subscribe signals → call operation →
// drain signals until Finished.
func runTransaction(
	ctx context.Context,
	conn *dbus.Conn,
	setup func(txn dbus.BusObject) error,
) (packages []pkPackage, errCode *pkError, exit uint32, err error) {
	// Create a new transaction.
	manager := conn.Object(pkService, pkObjPath)
	var txnPath dbus.ObjectPath
	createCtx, createCancel := context.WithTimeout(ctx, 15*time.Second)
	defer createCancel()
	if err = manager.CallWithContext(createCtx, pkIface+".CreateTransaction", pkCallFlags).Store(&txnPath); err != nil {
		return nil, nil, 0, fmt.Errorf("CreateTransaction: %w", err)
	}

	// Subscribe to signals from this specific transaction object.
	matchRules := []dbus.MatchOption{
		dbus.WithMatchObjectPath(txnPath),
		dbus.WithMatchInterface(pkTxIface),
	}
	if addErr := conn.AddMatchSignal(matchRules...); addErr != nil {
		return nil, nil, 0, fmt.Errorf("AddMatchSignal: %w", addErr)
	}
	defer func() { _ = conn.RemoveMatchSignal(matchRules...) }()

	sigCh := make(chan *dbus.Signal, 64)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	// Invoke the operation method.
	txn := conn.Object(pkService, txnPath)
	if err = setup(txn); err != nil {
		return nil, nil, 0, fmt.Errorf("transaction setup: %w", err)
	}

	// Drain signals until Finished.
	for {
		select {
		case <-ctx.Done():
			// Best-effort cancel — ignore error, we're already returning.
			_ = txn.CallWithContext(context.Background(), pkTxIface+".Cancel", pkCallFlags).Err
			return nil, nil, 0, ctx.Err()

		case sig, ok := <-sigCh:
			if !ok {
				return nil, nil, 0, fmt.Errorf("D-Bus signal channel closed unexpectedly")
			}
			// Only process signals from our transaction.
			if sig.Path != txnPath {
				continue
			}
			switch sig.Name {
			case pkTxIface + ".Package":
				if len(sig.Body) >= 3 {
					info, _ := sig.Body[0].(uint32)
					pkgID, _ := sig.Body[1].(string)
					summary, _ := sig.Body[2].(string)
					packages = append(packages, pkPackage{Info: info, PackageID: pkgID, Summary: summary})
				}
			case pkTxIface + ".ErrorCode":
				if len(sig.Body) >= 2 {
					code, _ := sig.Body[0].(uint32)
					details, _ := sig.Body[1].(string)
					errCode = &pkError{Code: code, Details: details}
				}
			case pkTxIface + ".Finished":
				if len(sig.Body) >= 1 {
					exit, _ = sig.Body[0].(uint32)
				}
				return packages, errCode, exit, nil
			}
		}
	}
}
