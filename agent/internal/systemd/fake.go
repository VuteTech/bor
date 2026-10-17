// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Manager for tests. Units are added with AddUnit;
// every call is recorded in Calls ("Enable bluetooth.service",
// "Start fstrim.timer", "Reload").
type Fake struct {
	mu sync.Mutex

	// Unavailable makes Available return false.
	Unavailable bool
	// Units by name. A unit absent from the map reports LoadState
	// "not-found".
	Units map[string]*UnitStatus
	// Calls records every mutating call in order.
	Calls []string
	// Fail maps a call string (as recorded in Calls) to an error.
	Fail map[string]error
	// JobResults maps a call string ("Start x.service") to the result
	// returned instead of JobDone.
	JobResults map[string]JobResult
	// Reloads counts Reload calls.
	Reloads int
	// StatusErrors maps a unit name to an error Status returns.
	StatusErrors map[string]error
	// NextLoadState maps a unit name to the LoadState the unit takes
	// after the next Reload (bad-setting rollback tests).
	NextLoadState map[string]string

	// pendingLoad holds load states that take effect on the next Reload
	// (a mask or unmask); unmasked holds the state a masked unit returns
	// to when unmasked.
	pendingLoad map[string]string
	unmasked    map[string]string
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{
		Units:         map[string]*UnitStatus{},
		Fail:          map[string]error{},
		JobResults:    map[string]JobResult{},
		StatusErrors:  map[string]error{},
		NextLoadState: map[string]string{},
		pendingLoad:   map[string]string{},
		unmasked:      map[string]string{},
	}
}

// AddUnit registers a unit. The zero LoadState becomes "loaded".
func (f *Fake) AddUnit(u UnitStatus) *UnitStatus { //nolint:gocritic // hugeParam: tests build the value inline; the fake stores a copy anyway
	f.mu.Lock()
	defer f.mu.Unlock()
	if u.LoadState == "" {
		u.LoadState = "loaded"
	}
	if u.ActiveState == "" {
		u.ActiveState = "inactive"
	}
	if u.SubState == "" {
		if u.ActiveState == "active" {
			u.SubState = "running"
		} else {
			u.SubState = "dead"
		}
	}
	if u.Result == "" {
		u.Result = "success"
	}
	c := u
	f.Units[u.Name] = &c
	return f.Units[u.Name]
}

// Unit returns the current state of a unit (nil when unknown).
func (f *Fake) Unit(name string) *UnitStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Units[name]
}

func (f *Fake) record(call string) error {
	f.Calls = append(f.Calls, call)
	if err, ok := f.Fail[call]; ok {
		return err
	}
	return nil
}

// Available implements Manager.
func (f *Fake) Available() bool { return !f.Unavailable }

// Version implements Manager.
func (f *Fake) Version(context.Context) (string, error) { return "252", nil }

// Status implements Manager.
func (f *Fake) Status(_ context.Context, name string) (*UnitStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.StatusErrors[name]; ok {
		return nil, err
	}
	u, ok := f.Units[name]
	if !ok {
		return &UnitStatus{Name: name, LoadState: "not-found", ActiveState: "inactive", SubState: "dead"}, nil
	}
	c := *u
	return &c, nil
}

// ListUnitFiles implements Manager.
func (f *Fake) ListUnitFiles(_ context.Context, patterns []string) ([]UnitFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []UnitFile
	for _, u := range f.Units {
		if !matchesAny(u.Name, patterns) {
			continue
		}
		p := u.FragmentPath
		if p == "" {
			p = "/usr/lib/systemd/system/" + u.Name
		}
		out = append(out, UnitFile{Path: p, State: u.UnitFileState})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ListUnits implements Manager.
func (f *Fake) ListUnits(_ context.Context, patterns []string) ([]UnitStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []UnitStatus
	for _, u := range f.Units {
		if matchesAny(u.Name, patterns) {
			out = append(out, *u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func matchesAny(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// Enable implements Manager.
func (f *Fake) Enable(_ context.Context, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		if err := f.record("Enable " + n); err != nil {
			return err
		}
		u, ok := f.Units[n]
		if !ok {
			return fmt.Errorf("unit %s not found", n)
		}
		switch u.UnitFileState {
		case "static", "generated", "transient", "":
			return ErrNoInstall
		case "masked":
			return fmt.Errorf("unit %s is masked", n)
		}
		u.UnitFileState = "enabled"
	}
	return nil
}

// Disable implements Manager.
func (f *Fake) Disable(_ context.Context, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		if err := f.record("Disable " + n); err != nil {
			return err
		}
		if u, ok := f.Units[n]; ok && u.UnitFileState != "masked" {
			u.UnitFileState = "disabled"
		}
	}
	return nil
}

// Mask implements Manager.
func (f *Fake) Mask(_ context.Context, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		if err := f.record("Mask " + n); err != nil {
			return err
		}
		u, ok := f.Units[n]
		if !ok {
			u = &UnitStatus{Name: n, LoadState: "not-found", ActiveState: "inactive", SubState: "dead"}
			f.Units[n] = u
		}
		if strings.HasPrefix(u.FragmentPath, "/etc/systemd/system/") {
			return fmt.Errorf("unit file %s already exists; masking would replace it", u.FragmentPath)
		}
		if u.UnitFileState != "masked" {
			f.unmasked[n] = u.UnitFileState
		}
		u.UnitFileState = "masked"
		f.pendingLoad[n] = "masked"
	}
	return nil
}

// Unmask implements Manager.
func (f *Fake) Unmask(_ context.Context, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		if err := f.record("Unmask " + n); err != nil {
			return err
		}
		if u, ok := f.Units[n]; ok && u.UnitFileState == "masked" {
			u.UnitFileState = f.unmasked[n]
			if u.UnitFileState == "" {
				u.UnitFileState = "disabled"
			}
			f.pendingLoad[n] = "loaded"
		}
	}
	return nil
}

// Reload implements Manager: masks and unmasks take effect on the load
// state, and NextLoadState overrides apply.
func (f *Fake) Reload(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Reloads++
	if err := f.record("Reload"); err != nil {
		return err
	}
	for name, u := range f.Units {
		if ls, ok := f.pendingLoad[name]; ok {
			u.LoadState = ls
			delete(f.pendingLoad, name)
		}
		if ls, ok := f.NextLoadState[name]; ok {
			u.LoadState = ls
			delete(f.NextLoadState, name)
		}
		u.NeedDaemonReload = false
	}
	return nil
}

func (f *Fake) job(verb, name string, apply func(u *UnitStatus)) (JobResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := verb + " " + name
	if err := f.record(call); err != nil {
		return JobFailed, err
	}
	u, ok := f.Units[name]
	if !ok {
		return JobFailed, fmt.Errorf("unit %s not found", name)
	}
	if r, ok := f.JobResults[call]; ok {
		if r == JobFailed {
			u.ActiveState = "failed"
			u.SubState = "failed"
			u.Result = "exit-code"
		}
		return r, nil
	}
	apply(u)
	return JobDone, nil
}

// Start implements Manager.
func (f *Fake) Start(_ context.Context, name string) (JobResult, error) {
	return f.job("Start", name, func(u *UnitStatus) {
		if u.IsOneshot() {
			u.ActiveState = "inactive"
			u.SubState = "dead"
			u.Result = "success"
			u.ExecMainStart = time.Now()
			return
		}
		u.ActiveState = "active"
		if u.Type() == "timer" {
			u.SubState = "waiting"
		} else {
			u.SubState = "running"
		}
	})
}

// Stop implements Manager.
func (f *Fake) Stop(_ context.Context, name string) (JobResult, error) {
	return f.job("Stop", name, func(u *UnitStatus) {
		u.ActiveState = "inactive"
		u.SubState = "dead"
	})
}

// Restart implements Manager.
func (f *Fake) Restart(_ context.Context, name string) (JobResult, error) {
	return f.job("Restart", name, func(u *UnitStatus) {
		u.ActiveState = "active"
		if u.Type() == "timer" {
			u.SubState = "waiting"
		} else {
			u.SubState = "running"
		}
		u.NRestarts++
	})
}

// SetUnmaskedState records the unit-file state a unit returns to when it
// is unmasked (default "disabled").
func (f *Fake) SetUnmaskedState(name, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmasked[name] = state
}

// HasCall reports whether a call was recorded.
func (f *Fake) HasCall(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		if c == call {
			return true
		}
	}
	return false
}
