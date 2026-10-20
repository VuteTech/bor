// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// DnfOfflineDir holds dnf5's stored offline transaction.
const DnfOfflineDir = "/usr/lib/sysimage/libdnf5/offline"

// OfflineStatus is dnf5's stored offline transaction, when any.
type OfflineStatus struct {
	Staged bool
	// Command is what stored it ("dnf5 upgrade --offline --security -y").
	Command string
	// Packages counts the rpms of the transaction when the transaction
	// file could be read.
	Packages int
}

// DnfOfflineStatus reads "dnf5 offline status" and the stored transaction.
func DnfOfflineStatus(ctx context.Context, run Runner, p *Platform) OfflineStatus {
	var st OfflineStatus
	if !p.DNF5 || p.DnfBin == "" {
		return st
	}
	out, _ := run(ctx, p.DnfBin, "-q", "offline", "status")
	text := string(out)
	if strings.Contains(text, "No offline transaction") || !strings.Contains(text, "offline transaction") {
		return st
	}
	st.Staged = true
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "dnf5 ") || strings.HasPrefix(line, "dnf ") {
			st.Command = line
			break
		}
	}
	st.Packages = countOfflinePackages(p.Path(DnfOfflineDir + "/transaction.json"))
	return st
}

// countOfflinePackages counts the rpms in a stored transaction file.
func countOfflinePackages(path string) int {
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed dnf path
	if err != nil {
		return 0
	}
	var doc struct {
		RPMs []json.RawMessage `json:"rpms"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0
	}
	return len(doc.RPMs)
}

// DnfOfflineStage stores the upgrade of the mode as an offline
// transaction, to be run at the next boot.
func DnfOfflineStage(ctx context.Context, run Runner, p *Platform, mode Mode) ([]byte, error) {
	args := []string{"upgrade", "--offline", "-y"}
	if mode == ModeSecurity {
		args = append(args, "--security")
	}
	return run(ctx, p.DnfBin, args...)
}

// DnfOfflineClean removes a stored offline transaction.
func DnfOfflineClean(ctx context.Context, run Runner, p *Platform) error {
	_, err := run(ctx, p.DnfBin, "-y", "offline", "clean")
	return err
}

// DnfOfflineReboot asks dnf5 to reboot into the offline transaction.
func DnfOfflineReboot(ctx context.Context, run Runner, p *Platform) error {
	_, err := run(ctx, p.DnfBin, "-y", "offline", "reboot")
	return err
}

// DnfHasUpgrades reports whether dnf has upgrades to install for the
// mode, from the cache (exit 100 means there are).
func DnfHasUpgrades(ctx context.Context, run Runner, p *Platform, mode Mode) bool {
	args := []string{"-q", "-C", "check-upgrade"}
	if mode == ModeSecurity {
		args = append(args, "--security")
	}
	_, err := run(ctx, p.DnfBin, args...)
	return ExitCode(err) == 100
}
