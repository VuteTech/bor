// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"fmt"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// flatpakUserScopeMessage is reported for USER-scope entries until Phase 2.
const flatpakUserScopeMessage = "user-scope enforcement not available in this agent version"

// listFlatpakApps returns the installed apps of the selected installation.
func listFlatpakApps(ctx context.Context, opts *FlatpakOptions) ([]flatpakInstalledApp, error) {
	inst, err := opts.instFlag()
	if err != nil {
		return nil, err
	}
	out, err := opts.run(ctx, flatpakListTimeout, "list", inst, "--app", "--columns="+flatpakListColumns)
	if err != nil {
		kind, msg := classifyFlatpakFailure(err, out)
		if kind == flatpakFailureUnknownInstallation {
			return nil, &FlatpakInapplicableError{Reason: "flatpak installation is not defined on this node: " + msg}
		}
		return nil, fmt.Errorf("flatpak list: %s", msg)
	}
	return parseFlatpakList(out), nil
}

// findInstalled returns the installed row matching app id (and branch when
// given).
func findInstalled(installed []flatpakInstalledApp, appID, branch string) (flatpakInstalledApp, bool) {
	for _, a := range installed {
		if a.App != appID {
			continue
		}
		if branch != "" && a.Branch != branch {
			continue
		}
		return a, true
	}
	return flatpakInstalledApp{}, false
}

// flatpakRefArg renders the ref argument for install/uninstall.
func flatpakRefArg(appID, branch string) string {
	if branch == "" {
		return appID
	}
	return appID + "//" + branch
}

// describeInstalled formats an installed row for compliance messages.
func describeInstalled(a *flatpakInstalledApp) string {
	msg := "installed"
	if a.Version != "" {
		msg += " " + a.Version
	}
	if a.Origin != "" {
		msg += " from " + a.Origin
	}
	if a.Branch != "" {
		msg += " (" + a.Branch + ")"
	}
	return msg
}

// SyncFlatpakApps enforces the desired state of every app entry (system scope)
// and returns one ComplianceItem per entry. It reads the installed set first,
// only runs install/uninstall for entries whose observed state differs, then
// re-reads to confirm. A returned error is fatal (listing failed).
func SyncFlatpakApps(ctx context.Context, opts *FlatpakOptions, desired *FlatpakDesired) ([]ComplianceItem, error) {
	inst, err := opts.instFlag()
	if err != nil {
		return nil, &FlatpakInapplicableError{Reason: err.Error()}
	}
	installed, err := listFlatpakApps(ctx, opts)
	if err != nil {
		return nil, err
	}

	type pending struct {
		entry *pb.FlatpakAppEntry
		key   string
		msg   string
	}
	items := make([]ComplianceItem, 0, len(desired.Apps))
	var recheck []pending
	changed := false

	for _, a := range desired.Apps {
		key := flatpakItemApp + a.GetAppId()
		if a.GetScope() == pb.FlatpakScope_FLATPAK_SCOPE_USER {
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, Message: flatpakUserScopeMessage})
			continue
		}
		if verr := ValidateFlatpakAppID(a.GetAppId()); verr != nil {
			items = append(items, errItem(key, verr))
			continue
		}
		if verr := ValidateFlatpakBranch(a.GetBranch()); verr != nil {
			items = append(items, errItem(key, verr))
			continue
		}
		if a.GetRemote() != "" {
			if verr := ValidateFlatpakRemoteName(a.GetRemote()); verr != nil {
				items = append(items, errItem(key, verr))
				continue
			}
		}

		cur, isInstalled := findInstalled(installed, a.GetAppId(), a.GetBranch())
		ref := flatpakRefArg(a.GetAppId(), a.GetBranch())

		switch a.GetState() {
		case pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT:
			if isInstalled {
				items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: describeInstalled(&cur)})
				continue
			}
			args := []string{"install", inst, "--noninteractive", "--assumeyes"}
			if a.GetRemote() != "" {
				args = append(args, a.GetRemote())
			}
			args = append(args, ref)
			if item, ok := runFlatpakAppOp(ctx, opts, key, a.GetOptional(), args...); !ok {
				items = append(items, item)
				continue
			}
			changed = true
			recheck = append(recheck, pending{entry: a, key: key, msg: "installed"})

		case pb.FlatpakAppState_FLATPAK_APP_STATE_LATEST:
			args := []string{"install", inst, "--noninteractive", "--assumeyes", "--or-update"}
			if a.GetRemote() != "" {
				args = append(args, a.GetRemote())
			}
			args = append(args, ref)
			if item, ok := runFlatpakAppOp(ctx, opts, key, a.GetOptional(), args...); !ok {
				items = append(items, item)
				continue
			}
			changed = true
			recheck = append(recheck, pending{entry: a, key: key, msg: "up to date"})

		case pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT:
			if !isInstalled {
				items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: "not installed"})
				continue
			}
			args := []string{"uninstall", inst, "--noninteractive", "--assumeyes"}
			if a.GetDeleteData() {
				args = append(args, "--delete-data")
			}
			args = append(args, ref)
			if item, ok := runFlatpakAppOp(ctx, opts, key, true, args...); !ok {
				items = append(items, item)
				continue
			}
			changed = true
			recheck = append(recheck, pending{entry: a, key: key, msg: "removed"})

		default:
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "unknown desired state"})
		}
	}

	if desired.UninstallUnused && changed {
		if out, uerr := opts.run(ctx, opts.opTimeout(), "uninstall", inst, "--unused", "--noninteractive", "--assumeyes"); uerr != nil {
			if kind, msg := classifyFlatpakFailure(uerr, out); kind != flatpakFailureNotFound {
				items = append(items, ComplianceItem{Key: flatpakItemApp + "unused", Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "uninstall --unused failed: " + msg})
			}
		}
	}

	if len(recheck) > 0 {
		after, lerr := listFlatpakApps(ctx, opts)
		if lerr != nil {
			for _, p := range recheck {
				items = append(items, errItem(p.key, fmt.Errorf("verify after change: %w", lerr)))
			}
			return items, nil
		}
		for _, p := range recheck {
			cur, isInstalled := findInstalled(after, p.entry.GetAppId(), p.entry.GetBranch())
			wantInstalled := p.entry.GetState() != pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT
			switch {
			case wantInstalled && isInstalled:
				items = append(items, ComplianceItem{Key: p.key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: describeInstalled(&cur)})
			case !wantInstalled && !isInstalled:
				items = append(items, ComplianceItem{Key: p.key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: p.msg})
			default:
				items = append(items, ComplianceItem{Key: p.key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "state did not change after flatpak operation"})
			}
		}
	}
	return items, nil
}

// runFlatpakAppOp runs one install/uninstall invocation and classifies the
// outcome. It returns ok=true on success; otherwise the ComplianceItem to
// record. notFoundInapplicable maps a missing ref to INAPPLICABLE (optional
// apps and uninstalls) instead of NON_COMPLIANT.
func runFlatpakAppOp(ctx context.Context, opts *FlatpakOptions, key string, notFoundInapplicable bool, args ...string) (ComplianceItem, bool) {
	out, err := opts.run(ctx, opts.opTimeout(), args...)
	if err == nil {
		return ComplianceItem{}, true
	}
	kind, msg := classifyFlatpakFailure(err, out)
	switch kind {
	case flatpakFailureNotFound:
		status := pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
		if notFoundInapplicable {
			status = pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
		}
		return ComplianceItem{Key: key, Status: status, Message: "not found in remote: " + msg}, false
	case flatpakFailureTimeout:
		return ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "flatpak " + args[0] + " timed out after " + opts.opTimeout().String()}, false
	case flatpakFailureUnknownInstallation:
		return ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, Message: msg}, false
	default:
		return ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "flatpak " + args[0] + " failed: " + msg}, false
	}
}

// CheckFlatpakApps verifies the current installed state against desired
// without making changes (used after an auto-update tick).
func CheckFlatpakApps(ctx context.Context, opts *FlatpakOptions, desired *FlatpakDesired) ([]ComplianceItem, error) {
	installed, err := listFlatpakApps(ctx, opts)
	if err != nil {
		return nil, err
	}
	items := make([]ComplianceItem, 0, len(desired.Apps))
	for _, a := range desired.Apps {
		key := flatpakItemApp + a.GetAppId()
		if a.GetScope() == pb.FlatpakScope_FLATPAK_SCOPE_USER {
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, Message: flatpakUserScopeMessage})
			continue
		}
		cur, isInstalled := findInstalled(installed, a.GetAppId(), a.GetBranch())
		wantInstalled := a.GetState() != pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT
		switch {
		case wantInstalled && isInstalled:
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: describeInstalled(&cur)})
		case !wantInstalled && !isInstalled:
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, Message: "not installed"})
		case wantInstalled && a.GetOptional():
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, Message: "not installed (optional)"})
		case wantInstalled:
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "not installed"})
		default:
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: describeInstalled(&cur) + " but should be absent"})
		}
	}
	return items, nil
}

// FlatpakUpdateAll runs `flatpak update` for the selected installation and,
// when requested, removes unused runtimes. It is used by the auto-update
// ticker.
func FlatpakUpdateAll(ctx context.Context, opts *FlatpakOptions, uninstallUnused bool) error {
	inst, err := opts.instFlag()
	if err != nil {
		return err
	}
	if out, uerr := opts.run(ctx, 2*opts.opTimeout(), "update", inst, "--noninteractive", "--assumeyes"); uerr != nil {
		_, msg := classifyFlatpakFailure(uerr, out)
		return fmt.Errorf("flatpak update: %s", msg)
	}
	if uninstallUnused {
		if out, uerr := opts.run(ctx, opts.opTimeout(), "uninstall", inst, "--unused", "--noninteractive", "--assumeyes"); uerr != nil {
			if kind, msg := classifyFlatpakFailure(uerr, out); kind != flatpakFailureNotFound {
				return fmt.Errorf("flatpak uninstall --unused: %s", msg)
			}
		}
	}
	return nil
}
