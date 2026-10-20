// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"os"
	"strings"
)

// TransactionalRebootFile is written by transactional-update when a new
// snapshot waits for a reboot and the reboot method is none or notify.
const TransactionalRebootFile = "/run/reboot-needed"

// TransactionalMethod is the transactional-update command of a mode:
// "patch" installs the patches (the security mode of a snapshot system),
// "dup" or "up" every update, the way the distribution updates itself.
func TransactionalMethod(mode Mode, osID, variant string) string {
	if mode == ModeSecurity {
		return "patch"
	}
	rolling := strings.Contains(osID, "tumbleweed") || strings.Contains(osID, "microos") || strings.Contains(osID, "aeon") || strings.Contains(osID, "kalpa") ||
		strings.Contains(variant, "microos") || strings.Contains(variant, "aeon") || strings.Contains(variant, "kalpa")
	if rolling {
		return "dup"
	}
	return "up"
}

// RenderTransactionalUpdateConf renders /etc/transactional-update.conf:
// the update method of the mode and no reboot of its own.
func RenderTransactionalUpdateConf(mode Mode, osID, variant string) []byte {
	return []byte("# " + Header + "\n" +
		"UPDATE_METHOD=" + TransactionalMethod(mode, osID, variant) + "\n" +
		"REBOOT_METHOD=none\n")
}

// TransactionalUpdateMode reads UPDATE_METHOD of a transactional-update.conf.
func TransactionalUpdateMode(content []byte) Mode {
	for _, l := range strings.Split(string(content), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "UPDATE_METHOD=") {
			if strings.Trim(strings.TrimPrefix(l, "UPDATE_METHOD="), `"'`) == "patch" {
				return ModeSecurity
			}
			return ModeAll
		}
	}
	return ModeAll
}

// TransactionalRebootNeeded reports transactional-update's reboot marker.
func TransactionalRebootNeeded(root string) bool {
	_, err := os.Stat(root + TransactionalRebootFile)
	return err == nil
}
