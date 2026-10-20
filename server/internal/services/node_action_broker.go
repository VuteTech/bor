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

// NodeActionResult is what an agent reported for one on-demand node
// action (a mandatory access control proposal, a Software updates
// check, install or reboot).
type NodeActionResult struct {
	Success bool
	Message string
	// Text is the action's output: a proposed policy source, the tail of
	// an engine run.
	Text string
}

// NodeActionBroker hands the result of an on-demand action from the
// agent's RPC to the REST request waiting for it. Like the systemd action
// broker it is in-memory on purpose: an action runs now for the person
// looking at the node, or not at all.
type NodeActionBroker struct {
	mu      sync.Mutex
	pending map[string]*pendingNodeAction
}

type pendingNodeAction struct {
	node   string
	result chan NodeActionResult
}

// NewNodeActionBroker creates an empty broker.
func NewNodeActionBroker() *NodeActionBroker {
	return &NodeActionBroker{pending: map[string]*pendingNodeAction{}}
}

// NewAction registers a pending action for the named node and returns its
// id, the channel its result arrives on, and a function that forgets the
// action.
func (b *NodeActionBroker) NewAction(node string) (id string, result <-chan NodeActionResult, forget func(), err error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, nil, fmt.Errorf("generate action id: %w", err)
	}
	id = hex.EncodeToString(raw[:])
	p := &pendingNodeAction{node: node, result: make(chan NodeActionResult, 1)}
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
func (b *NodeActionBroker) Deliver(id, node string, r NodeActionResult) bool {
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
func (b *NodeActionBroker) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
