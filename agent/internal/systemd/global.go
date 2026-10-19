// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/VuteTech/Bor/server/pkg/systemdunits"
)

// GlobalEtcDir is where "systemctl --global" keeps the administrator's
// user units, drop-ins, enablement symlinks and masks.
const GlobalEtcDir = "/etc/systemd/user"

// globalLibDirs are the other directories user managers load units from,
// in search order after GlobalEtcDir.
var globalLibDirs = []string{
	"/run/systemd/user",
	"/usr/local/lib/systemd/user",
	"/usr/lib/systemd/user",
	"/lib/systemd/user",
}

// Unit-file states of the global scope, as "systemctl --global
// is-enabled" prints them.
const (
	GlobalStateEnabled  = "enabled"
	GlobalStateDisabled = "disabled"
	GlobalStateMasked   = "masked"
	GlobalStateStatic   = "static"
	GlobalStateNotFound = "not-found"
)

// GlobalUnits performs the enablement operations of the global scope on
// the file system, as "systemctl --global" does: there is no manager to
// ask, because the global scope is every user manager's configuration.
// Enabling creates the symlinks the unit's [Install] section names under
// EtcDir, masking links the unit to /dev/null there.
type GlobalUnits struct {
	// EtcDir is the writable unit directory (default GlobalEtcDir).
	EtcDir string
	// LibDirs are the read-only unit directories (default: the
	// distribution's user unit directories).
	LibDirs []string
}

func (g GlobalUnits) etcDir() string {
	if g.EtcDir == "" {
		return GlobalEtcDir
	}
	return g.EtcDir
}

func (g GlobalUnits) libDirs() []string {
	if g.LibDirs == nil {
		return globalLibDirs
	}
	return g.LibDirs
}

// Lookup finds the unit file: the path in EtcDir or the first LibDir that
// has it, whether it is masked there (a symlink to /dev/null in EtcDir),
// and false when no directory has it.
func (g GlobalUnits) Lookup(unit string) (path string, masked, found bool) {
	if systemdunits.ValidateName(unit) != nil {
		return "", false, false
	}
	p := filepath.Join(g.etcDir(), unit)
	if target, err := os.Readlink(p); err == nil && target == os.DevNull {
		return p, true, true
	}
	for _, dir := range append([]string{g.etcDir()}, g.libDirs()...) {
		p := filepath.Join(dir, unit)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, false, true
		}
	}
	return "", false, false
}

// State returns the unit-file state of the global scope: masked,
// enabled, disabled, static or not-found.
func (g GlobalUnits) State(unit string) string {
	path, masked, found := g.Lookup(unit)
	switch {
	case !found:
		return GlobalStateNotFound
	case masked:
		return GlobalStateMasked
	}
	info := ScanUnitFile(path)
	if !info.HasInstall || (len(info.WantedBy) == 0 && len(info.RequiredBy) == 0 && len(info.Alias) == 0) {
		return GlobalStateStatic
	}
	for _, link := range g.installLinks(unit, &info) {
		if _, err := os.Lstat(link); err == nil {
			return GlobalStateEnabled
		}
	}
	return GlobalStateDisabled
}

// installLinks lists the symlinks the [Install] section asks for.
func (g GlobalUnits) installLinks(unit string, info *UnitFileInfo) []string {
	var links []string
	for _, t := range info.WantedBy {
		if systemdunits.ValidateName(t) == nil {
			links = append(links, filepath.Join(g.etcDir(), t+".wants", unit))
		}
	}
	for _, t := range info.RequiredBy {
		if systemdunits.ValidateName(t) == nil {
			links = append(links, filepath.Join(g.etcDir(), t+".requires", unit))
		}
	}
	for _, a := range info.Alias {
		if systemdunits.ValidateName(a) == nil && a != unit {
			links = append(links, filepath.Join(g.etcDir(), a))
		}
	}
	return links
}

// Enable creates the enablement symlinks of the unit.
func (g GlobalUnits) Enable(unit string) error {
	path, masked, found := g.Lookup(unit)
	switch {
	case !found:
		return fmt.Errorf("unit %s not found", unit)
	case masked:
		return fmt.Errorf("unit %s is masked", unit)
	}
	info := ScanUnitFile(path)
	links := g.installLinks(unit, &info)
	if len(links) == 0 {
		return ErrNoInstall
	}
	for _, link := range links {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil { //nolint:gosec // G301: unit directories are world-readable
			return err
		}
		if target, err := os.Readlink(link); err == nil && target == path {
			continue
		}
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Symlink(path, link); err != nil {
			return err
		}
	}
	return nil
}

// Disable removes the enablement symlinks of the unit.
func (g GlobalUnits) Disable(unit string) error {
	path, masked, found := g.Lookup(unit)
	if !found || masked {
		return nil
	}
	info := ScanUnitFile(path)
	for _, link := range g.installLinks(unit, &info) {
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_ = os.Remove(filepath.Dir(link)) // a .wants directory left empty
	}
	return nil
}

// Mask links the unit to /dev/null in EtcDir, after removing its
// enablement symlinks. A unit whose own file lives in EtcDir cannot be
// masked, because the mask would replace it.
func (g GlobalUnits) Mask(unit string) error {
	if systemdunits.ValidateName(unit) != nil {
		return fmt.Errorf("invalid unit name %q", unit)
	}
	p := filepath.Join(g.etcDir(), unit)
	if st, err := os.Lstat(p); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(p); err == nil && target == os.DevNull {
				return nil
			}
		}
		return fmt.Errorf("unit file %s already exists; masking would replace it", p)
	}
	if err := g.Disable(unit); err != nil {
		return err
	}
	if err := os.MkdirAll(g.etcDir(), 0o755); err != nil { //nolint:gosec // G301: unit directories are world-readable
		return err
	}
	return os.Symlink(os.DevNull, p)
}

// Unmask removes the /dev/null link of a masked unit.
func (g GlobalUnits) Unmask(unit string) error {
	if systemdunits.ValidateName(unit) != nil {
		return fmt.Errorf("invalid unit name %q", unit)
	}
	p := filepath.Join(g.etcDir(), unit)
	target, err := os.Readlink(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if target != os.DevNull {
		return fmt.Errorf("%s is not a mask", p)
	}
	return os.Remove(p)
}

// IsEnabledState reports whether a global state counts as enabled.
func IsEnabledState(state string) bool {
	return state == GlobalStateEnabled || strings.HasPrefix(state, "enabled")
}
