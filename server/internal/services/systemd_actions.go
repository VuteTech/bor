// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// SystemdUnitActionResult is what an agent reported for one on-demand
// unit action.
type SystemdUnitActionResult struct {
	Success bool
	Message string
	// Unit is the unit's state after the action, when the agent sent it.
	Unit *pb.SystemdUnitInfo
}

// SystemdActionBroker hands the result of an on-demand unit action from
// the agent's RPC to the REST request that is waiting for it. Actions are
// in-memory on purpose: an operator's "restart now" must run now or not
// at all, so nothing is queued for a node that is offline, and a server
// restart turns a pending action into a timeout for the waiting request.
type SystemdActionBroker struct {
	mu      sync.Mutex
	pending map[string]*pendingSystemdAction
}

type pendingSystemdAction struct {
	node   string
	result chan SystemdUnitActionResult
}

// NewSystemdActionBroker creates an empty broker.
func NewSystemdActionBroker() *SystemdActionBroker {
	return &SystemdActionBroker{pending: map[string]*pendingSystemdAction{}}
}

// NewAction registers a pending action for the named node and returns its
// id, the channel its result arrives on, and a function that forgets the
// action (to be called when the caller stops waiting).
func (b *SystemdActionBroker) NewAction(node string) (id string, result <-chan SystemdUnitActionResult, forget func(), err error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, nil, fmt.Errorf("generate action id: %w", err)
	}
	id = hex.EncodeToString(raw[:])
	p := &pendingSystemdAction{node: node, result: make(chan SystemdUnitActionResult, 1)}
	b.mu.Lock()
	b.pending[id] = p
	b.mu.Unlock()
	forget = func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}
	return id, p.result, forget, nil
}

// Deliver hands a result to the request waiting for the action. It
// reports false when no request waits for the id, or when the result
// comes from a node other than the one the action was sent to.
func (b *SystemdActionBroker) Deliver(id, node string, r SystemdUnitActionResult) bool {
	b.mu.Lock()
	p, ok := b.pending[id]
	if ok && p.node == node {
		delete(b.pending, id)
	}
	b.mu.Unlock()
	if !ok || p.node != node {
		return false
	}
	p.result <- r
	return true
}

// Pending returns how many actions wait for a result (for tests and
// diagnostics).
func (b *SystemdActionBroker) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
