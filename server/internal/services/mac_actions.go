// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// MacActionResult is what an agent reported for one on-demand mandatory
// access control action (a policy proposal built from recent denials).
type MacActionResult struct {
	Success bool
	Message string
	// Text is the proposed policy source.
	Text string
}

// MacActionBroker hands the result of an on-demand action from the
// agent's RPC to the REST request waiting for it. Like the systemd action
// broker it is in-memory on purpose: a proposal is computed now for the
// person looking at the node, or not at all.
type MacActionBroker struct {
	mu      sync.Mutex
	pending map[string]*pendingMacAction
}

type pendingMacAction struct {
	node   string
	result chan MacActionResult
}

// NewMacActionBroker creates an empty broker.
func NewMacActionBroker() *MacActionBroker {
	return &MacActionBroker{pending: map[string]*pendingMacAction{}}
}

// NewAction registers a pending action for the named node and returns its
// id, the channel its result arrives on, and a function that forgets the
// action.
func (b *MacActionBroker) NewAction(node string) (id string, result <-chan MacActionResult, forget func(), err error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, nil, fmt.Errorf("generate action id: %w", err)
	}
	id = hex.EncodeToString(raw[:])
	p := &pendingMacAction{node: node, result: make(chan MacActionResult, 1)}
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
// reports false when no request waits for the id or the result comes
// from another node.
func (b *MacActionBroker) Deliver(id, node string, r MacActionResult) bool {
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

// Pending returns how many actions wait for a result.
func (b *MacActionBroker) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
