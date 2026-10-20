// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

// MacActionResult is what an agent reported for one on-demand mandatory
// access control action (a policy proposal built from recent denials).
type MacActionResult = NodeActionResult

// MacActionBroker routes mandatory access control proposals from the
// agent's RPC to the request waiting for them.
type MacActionBroker = NodeActionBroker

// NewMacActionBroker creates an empty broker.
func NewMacActionBroker() *MacActionBroker {
	return NewNodeActionBroker()
}

// UpdatesActionResult is what an agent reported for one Software updates
// action (check, install, reboot).
type UpdatesActionResult = NodeActionResult

// UpdatesActionBroker routes Software updates action results from the
// agent's RPC to the request waiting for them.
type UpdatesActionBroker = NodeActionBroker

// NewUpdatesActionBroker creates an empty broker.
func NewUpdatesActionBroker() *UpdatesActionBroker {
	return NewNodeActionBroker()
}
