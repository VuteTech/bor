// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package edition decides which optional capabilities a Bor server runs
// with. The community build runs the Community edition, which enables none
// of them. A distribution that ships its own entry point passes a different
// Edition to app.Run; Enabled is consulted at request time, so an edition
// backed by an expiring license can switch a feature off without a restart.
package edition

// Feature identifies an optional capability.
type Feature string

const (
	// NodeGroupScopedRBAC lets role bindings be scoped to a single node
	// group (delegated administration). When it is off, only global role
	// bindings can be created and existing node-group-scoped bindings grant
	// nothing.
	NodeGroupScopedRBAC Feature = "node_group_scoped_rbac"
)

// AllFeatures lists every known Feature, in a stable order.
var AllFeatures = []Feature{NodeGroupScopedRBAC}

// Edition reports the running edition and its enabled features.
type Edition interface {
	// Name is a short, human-readable edition name such as "community".
	Name() string
	// Enabled reports whether the feature is available right now.
	Enabled(f Feature) bool
}

// Community is the default edition: no optional features.
type Community struct{}

// Name returns "community".
func (Community) Name() string { return "community" }

// Enabled always returns false.
func (Community) Enabled(Feature) bool { return false }

// EnabledFeatures returns the features e currently enables, in AllFeatures
// order.
func EnabledFeatures(e Edition) []Feature {
	var out []Feature
	for _, f := range AllFeatures {
		if e.Enabled(f) {
			out = append(out, f)
		}
	}
	return out
}
