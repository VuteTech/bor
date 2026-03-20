// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"
	"sync"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// defaultEventLogSize is the maximum number of events kept in the
// ring buffer for delta computation. Events beyond this are compacted
// (dropped), forcing reconnecting clients to do a full snapshot.
const defaultEventLogSize = 1000

// hubEvent is an internal event that carries a pb.PolicyUpdate along with
// routing metadata used by the streaming handler to decide whether to
// forward the event to a specific connected agent.
type hubEvent struct {
	update           *pb.PolicyUpdate
	affectedGroupIDs []string // nil/empty = broadcast to all agents
}

// PolicyHub is an in-process publish/subscribe hub that tracks policy
// change events and fans them out to connected gRPC streaming clients.
//
// It maintains:
//   - a monotonically increasing revision counter,
//   - a bounded ring buffer of past events (for delta sync),
//   - a set of subscriber channels (one per streaming client), and
//   - a per-client channel map for targeted dispatch.
type PolicyHub struct {
	mu          sync.RWMutex
	revision    int64
	eventLog    []*pb.PolicyUpdate
	maxLogSize  int
	subscribers map[chan *hubEvent]struct{}
	clients     map[string]chan *hubEvent // clientID → channel
}

// NewPolicyHub creates a ready-to-use PolicyHub.
func NewPolicyHub() *PolicyHub {
	return &PolicyHub{
		maxLogSize:  defaultEventLogSize,
		subscribers: make(map[chan *hubEvent]struct{}),
		clients:     make(map[string]chan *hubEvent),
	}
}

// Revision returns the current revision (thread-safe).
func (h *PolicyHub) Revision() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.revision
}

// publish is the internal broadcast method. It bumps the revision,
// appends to the event log, and fans out to all subscribers.
// affectedGroupIDs is forwarded to subscribers for per-agent filtering;
// it is NOT stored in the event log (log is used for delta catch-up only).
func (h *PolicyHub) publish(updateType pb.PolicyUpdate_UpdateType, policy *pb.Policy, affectedGroupIDs []string) {
	h.mu.Lock()

	h.revision++
	protoUpdate := &pb.PolicyUpdate{
		Type:     updateType,
		Policy:   policy,
		Revision: h.revision,
	}

	// Append to event log, evicting oldest entries when full.
	h.eventLog = append(h.eventLog, protoUpdate)
	if len(h.eventLog) > h.maxLogSize {
		drop := len(h.eventLog) / 2
		copy(h.eventLog, h.eventLog[drop:])
		h.eventLog = h.eventLog[:len(h.eventLog)-drop]
	}

	// Snapshot subscriber list while holding the lock.
	subs := make([]chan *hubEvent, 0, len(h.subscribers))
	for ch := range h.subscribers {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	ev := &hubEvent{update: protoUpdate, affectedGroupIDs: affectedGroupIDs}

	// Fan-out without holding the lock. Non-blocking send so a slow
	// subscriber does not stall the publisher.
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			log.Printf("policy_hub: dropping event for slow subscriber")
		}
	}
}

// Publish records a policy change event and broadcasts it to all agents.
// Use this for CREATED/UPDATED/DELETED events that affect all connected clients.
func (h *PolicyHub) Publish(updateType pb.PolicyUpdate_UpdateType, policy *pb.Policy) {
	h.publish(updateType, policy, nil)
}

// EventsSince returns all events with revision > sinceRevision.
// Returns nil if the requested revision has been compacted (too old).
func (h *PolicyHub) EventsSince(sinceRevision int64) []*pb.PolicyUpdate {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.eventLog) == 0 {
		if sinceRevision >= h.revision {
			return []*pb.PolicyUpdate{}
		}
		return nil
	}

	oldestAvailable := h.eventLog[0].Revision
	if sinceRevision < oldestAvailable-1 {
		return nil
	}

	var result []*pb.PolicyUpdate
	for _, ev := range h.eventLog {
		if ev.Revision > sinceRevision {
			result = append(result, ev)
		}
	}
	return result
}

// PublishResync bumps the revision and broadcasts a resync signal.
// affectedGroupIDs scopes the signal: only agents whose node groups overlap
// with affectedGroupIDs will trigger a fresh snapshot. When affectedGroupIDs
// is empty (or nil), all connected agents are signalled.
func (h *PolicyHub) PublishResync(affectedGroupIDs ...string) {
	h.publish(pb.PolicyUpdate_SNAPSHOT, nil, affectedGroupIDs)
}

// IsResyncSignal returns true if the event is a resync signal
// (published by PublishResync).
func IsResyncSignal(ev *pb.PolicyUpdate) bool {
	return ev.Type == pb.PolicyUpdate_SNAPSHOT && ev.Policy == nil
}

// Subscribe returns a channel that will receive future events and a
// cancel function. The caller MUST call cancel when done (e.g. when
// the gRPC stream ends) to avoid resource leaks. clientID is used
// for targeted dispatch via SendMetadataRefreshRequest.
func (h *PolicyHub) Subscribe(_ context.Context, clientID string) (<-chan *hubEvent, func()) { //nolint:gocritic // named returns conflict with internal channel variables
	ch := make(chan *hubEvent, 64)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	if clientID != "" {
		h.clients[clientID] = ch
	}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		if clientID != "" {
			if h.clients[clientID] == ch {
				delete(h.clients, clientID)
			}
		}
		h.mu.Unlock()
	}

	return ch, cancel
}

// SendMetadataRefreshRequest sends a METADATA_REQUEST event directly to
// the named client's stream. Returns false if the client is not connected.
func (h *PolicyHub) SendMetadataRefreshRequest(clientID string) bool {
	h.mu.RLock()
	ch, ok := h.clients[clientID]
	rev := h.revision
	h.mu.RUnlock()

	if !ok {
		return false
	}

	ev := &hubEvent{
		update: &pb.PolicyUpdate{
			Type:     pb.PolicyUpdate_METADATA_REQUEST,
			Revision: rev,
		},
	}

	select {
	case ch <- ev:
		return true
	default:
		log.Printf("policy_hub: dropping METADATA_REQUEST for slow subscriber %s", clientID)
		return false
	}
}
