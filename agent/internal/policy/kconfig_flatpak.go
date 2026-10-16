// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"strings"
)

// Flatpak applications run in a sandbox with their own /etc; the KDE
// applications among them (Okular, Kate, Dolphin from Flathub) therefore
// never see the managed directory unless a global override exposes it and
// puts it first in the sandbox's XDG_CONFIG_DIRS. Per-group overlays cannot
// be expressed this way (the override is static), so only node-wide
// settings reach Flatpak applications.

// FlatpakRunner runs `flatpak <args...>` and returns the combined output.
type FlatpakRunner func(args ...string) (string, error)

// flatpakSandboxConfigDirs is the sandbox default XDG_CONFIG_DIRS.
const flatpakSandboxConfigDirs = "/app/etc/xdg:/etc/xdg"

// EnsureFlatpakOverride makes the system-wide Flatpak override expose
// configPath read-only and prepend it to XDG_CONFIG_DIRS when enable is
// true, and removes both when it is false. It changes nothing when the
// override already matches. The caller only invokes it when flatpak is
// installed.
func EnsureFlatpakOverride(run FlatpakRunner, configPath string, enable bool) (changed bool, err error) {
	if run == nil || configPath == "" {
		return false, nil
	}
	fsEntry := configPath + ":ro"
	envEntry := "XDG_CONFIG_DIRS=" + configPath + ":" + flatpakSandboxConfigDirs

	current, err := run("override", "--system", "--show")
	if err != nil {
		return false, fmt.Errorf("flatpak override --show: %w: %s", err, strings.TrimSpace(current))
	}
	hasFS := strings.Contains(current, fsEntry)
	hasEnv := strings.Contains(current, envEntry)

	switch {
	case enable && hasFS && hasEnv:
		return false, nil
	case enable:
		if out, err := run("override", "--system", "--filesystem="+fsEntry, "--env="+envEntry); err != nil {
			return false, fmt.Errorf("flatpak override: %w: %s", err, strings.TrimSpace(out))
		}
		return true, nil
	case !hasFS && !hasEnv:
		return false, nil
	default:
		args := []string{"override", "--system"}
		if hasFS {
			args = append(args, "--nofilesystem="+configPath)
		}
		if hasEnv {
			args = append(args, "--unset-env=XDG_CONFIG_DIRS")
		}
		if out, err := run(args...); err != nil {
			return false, fmt.Errorf("flatpak override (remove): %w: %s", err, strings.TrimSpace(out))
		}
		return true, nil
	}
}
