// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"errors"
	"strings"
	"testing"
)

func TestEnsureFlatpakOverride(t *testing.T) {
	var calls [][]string
	state := ""
	run := func(args ...string) (string, error) {
		calls = append(calls, args)
		if len(args) == 3 && args[2] == "--show" {
			return state, nil
		}
		for _, a := range args {
			switch {
			case strings.HasPrefix(a, "--filesystem="):
				state += "[Context]\nfilesystems=" + strings.TrimPrefix(a, "--filesystem=") + ";\n"
			case strings.HasPrefix(a, "--env="):
				state += "[Environment]\n" + strings.TrimPrefix(a, "--env=") + "\n"
			case a == "--unset-env=XDG_CONFIG_DIRS":
				state = strings.ReplaceAll(state, "[Environment]\nXDG_CONFIG_DIRS=/etc/bor/xdg:/app/etc/xdg:/etc/xdg\n", "")
			case strings.HasPrefix(a, "--nofilesystem="):
				// Flatpak keeps a negative entry after a revocation.
				state = strings.ReplaceAll(state, "filesystems=/etc/bor/xdg:ro;", "filesystems=!/etc/bor/xdg;")
			}
		}
		return "", nil
	}

	changed, err := EnsureFlatpakOverride(run, "/etc/bor/xdg", true)
	if err != nil || !changed {
		t.Fatalf("first enable: changed=%v err=%v", changed, err)
	}
	if last := strings.Join(calls[len(calls)-1], " "); last != "override --system --filesystem=/etc/bor/xdg:ro --env=XDG_CONFIG_DIRS=/etc/bor/xdg:/app/etc/xdg:/etc/xdg" {
		t.Errorf("unexpected enable command: %s", last)
	}

	changed, err = EnsureFlatpakOverride(run, "/etc/bor/xdg", true)
	if err != nil || changed {
		t.Errorf("second enable must be a no-op: changed=%v err=%v", changed, err)
	}

	changed, err = EnsureFlatpakOverride(run, "/etc/bor/xdg", false)
	if err != nil || !changed {
		t.Fatalf("disable: changed=%v err=%v", changed, err)
	}
	if last := strings.Join(calls[len(calls)-1], " "); !strings.Contains(last, "--nofilesystem=/etc/bor/xdg") || !strings.Contains(last, "--unset-env=XDG_CONFIG_DIRS") {
		t.Errorf("unexpected disable command: %s", last)
	}

	// The negative filesystems entry left behind must not count as granted.
	changed, err = EnsureFlatpakOverride(run, "/etc/bor/xdg", false)
	if err != nil || changed {
		t.Errorf("disable with only a negative entry must be a no-op: changed=%v err=%v", changed, err)
	}
	changed, err = EnsureFlatpakOverride(run, "/etc/bor/xdg", true)
	if err != nil || !changed {
		t.Errorf("re-enable after a revocation must grant again: changed=%v err=%v", changed, err)
	}

	failing := func(_ ...string) (string, error) {
		return "error: no system installation", errors.New("exit status 1")
	}
	if _, err := EnsureFlatpakOverride(failing, "/etc/bor/xdg", true); err == nil {
		t.Error("a failing flatpak command must surface as an error")
	}
	if changed, err := EnsureFlatpakOverride(nil, "/etc/bor/xdg", true); err != nil || changed {
		t.Error("nil runner must do nothing")
	}
}
