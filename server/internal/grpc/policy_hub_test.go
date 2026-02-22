// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"testing"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func makeTestPolicy(id, name string) *pb.Policy {
	return &pb.Policy{
		Id:        id,
		Name:      name,
		Type:      "Firefox",
		Content:   `{"DisableAppUpdate": true}`,
		Version:   1,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Enabled:   true,
	}
}

func TestPolicyHub_RevisionStartsAtZero(t *testing.T) {
	hub := NewPolicyHub()
	if got := hub.Revision(); got != 0 {
		t.Errorf("initial revision = %d, want 0", got)
	}
}

func TestPolicyHub_PublishIncrementsRevision(t *testing.T) {
	hub := NewPolicyHub()

	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))
	if got := hub.Revision(); got != 1 {
		t.Errorf("revision after 1 publish = %d, want 1", got)
	}

	hub.Publish(pb.PolicyUpdate_UPDATED, makeTestPolicy("p1", "Policy 1"))
	if got := hub.Revision(); got != 2 {
		t.Errorf("revision after 2 publishes = %d, want 2", got)
	}
}

func TestPolicyHub_EventsSince_AllEvents(t *testing.T) {
	hub := NewPolicyHub()

	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))
	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p2", "Policy 2"))
	hub.Publish(pb.PolicyUpdate_UPDATED, makeTestPolicy("p1", "Policy 1 v2"))

	events := hub.EventsSince(0)
	if len(events) != 3 {
		t.Fatalf("EventsSince(0) returned %d events, want 3", len(events))
	}

	if events[0].Revision != 1 || events[1].Revision != 2 || events[2].Revision != 3 {
		t.Errorf("unexpected revisions: %d, %d, %d", events[0].Revision, events[1].Revision, events[2].Revision)
	}
}

func TestPolicyHub_EventsSince_Partial(t *testing.T) {
	hub := NewPolicyHub()

	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))
	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p2", "Policy 2"))
	hub.Publish(pb.PolicyUpdate_UPDATED, makeTestPolicy("p1", "Policy 1 v2"))

	events := hub.EventsSince(1)
	if len(events) != 2 {
		t.Fatalf("EventsSince(1) returned %d events, want 2", len(events))
	}

	if events[0].Revision != 2 {
		t.Errorf("first delta event revision = %d, want 2", events[0].Revision)
	}
}

func TestPolicyHub_EventsSince_UpToDate(t *testing.T) {
	hub := NewPolicyHub()

	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))

	events := hub.EventsSince(1)
	if len(events) != 0 {
		t.Fatalf("EventsSince(current) returned %d events, want 0", len(events))
	}
}

func TestPolicyHub_EventsSince_EmptyHub(t *testing.T) {
	hub := NewPolicyHub()

	// Up-to-date on empty hub.
	events := hub.EventsSince(0)
	if len(events) != 0 {
		t.Fatalf("EventsSince(0) on empty hub returned %d events, want 0", len(events))
	}
}

func TestPolicyHub_EventsSince_Compacted(t *testing.T) {
	hub := NewPolicyHub()
	// Use a small log size to force compaction.
	hub.maxLogSize = 5

	for i := 0; i < 10; i++ {
		hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p", "P"))
	}

	// Asking for revision 0 should return nil because early events
	// were compacted away.
	events := hub.EventsSince(0)
	if events != nil {
		t.Fatalf("EventsSince(0) after compaction should return nil, got %d events", len(events))
	}

	// Asking for a recent revision should still work.
	events = hub.EventsSince(hub.Revision() - 1)
	if events == nil {
		t.Fatal("EventsSince(recent) after compaction should not return nil")
	}
}

func TestPolicyHub_Subscribe_ReceivesPublishedEvents(t *testing.T) {
	hub := NewPolicyHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub := hub.Subscribe(ctx, "")
	defer unsub()

	p := makeTestPolicy("p1", "Policy 1")
	hub.Publish(pb.PolicyUpdate_CREATED, p)

	select {
	case ev := <-ch:
		if ev.Revision != 1 {
			t.Errorf("received event revision = %d, want 1", ev.Revision)
		}
		if ev.Type != pb.PolicyUpdate_CREATED {
			t.Errorf("received event type = %v, want CREATED", ev.Type)
		}
		if ev.Policy.GetId() != "p1" {
			t.Errorf("received policy id = %q, want p1", ev.Policy.GetId())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPolicyHub_Subscribe_UnsubStopsDelivery(t *testing.T) {
	hub := NewPolicyHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub := hub.Subscribe(ctx, "")

	// Unsubscribe immediately.
	unsub()

	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))

	select {
	case <-ch:
		t.Fatal("received event after unsubscribe")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestPolicyHub_MultipleSubscribers(t *testing.T) {
	hub := NewPolicyHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1, unsub1 := hub.Subscribe(ctx, "")
	defer unsub1()
	ch2, unsub2 := hub.Subscribe(ctx, "")
	defer unsub2()

	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))

	for _, ch := range []<-chan *pb.PolicyUpdate{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Revision != 1 {
				t.Errorf("subscriber got revision %d, want 1", ev.Revision)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber timed out")
		}
	}
}

func TestPolicyHub_EventTypes(t *testing.T) {
	hub := NewPolicyHub()

	tests := []struct {
		updateType pb.PolicyUpdate_UpdateType
	}{
		{pb.PolicyUpdate_CREATED},
		{pb.PolicyUpdate_UPDATED},
		{pb.PolicyUpdate_DELETED},
		{pb.PolicyUpdate_SNAPSHOT},
	}

	for _, tt := range tests {
		hub.Publish(tt.updateType, makeTestPolicy("p", "P"))
	}

	events := hub.EventsSince(0)
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}

	for i, tt := range tests {
		if events[i].Type != tt.updateType {
			t.Errorf("event %d type = %v, want %v", i, events[i].Type, tt.updateType)
		}
	}
}

func TestPolicyHub_ConcurrentPublishAndSubscribe(t *testing.T) {
	hub := NewPolicyHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub := hub.Subscribe(ctx, "")
	defer unsub()

	const numEvents = 50
	done := make(chan struct{})

	// Receive in a goroutine to keep up with the publisher.
	received := make(chan int, 1)
	go func() {
		count := 0
		for {
			select {
			case <-ch:
				count++
				if count == numEvents {
					received <- count
					return
				}
			case <-time.After(2 * time.Second):
				received <- count
				return
			}
		}
	}()

	for i := 0; i < numEvents; i++ {
		hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p", "P"))
	}
	close(done)

	count := <-received
	if count != numEvents {
		t.Errorf("received %d events, want %d", count, numEvents)
	}
}

func TestPolicyHub_PublishResync_IncrementsRevision(t *testing.T) {
	hub := NewPolicyHub()

	hub.PublishResync()
	if got := hub.Revision(); got != 1 {
		t.Errorf("revision after PublishResync = %d, want 1", got)
	}
}

func TestPolicyHub_PublishResync_IsResyncSignal(t *testing.T) {
	hub := NewPolicyHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub := hub.Subscribe(ctx, "")
	defer unsub()

	hub.PublishResync()

	select {
	case ev := <-ch:
		if !IsResyncSignal(ev) {
			t.Error("PublishResync event should be identified as resync signal")
		}
		if ev.Policy != nil {
			t.Error("resync signal should have nil policy")
		}
		if ev.Type != pb.PolicyUpdate_SNAPSHOT {
			t.Errorf("resync signal type = %v, want SNAPSHOT", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resync event")
	}
}

func TestIsResyncSignal_RegularEventsAreNotResync(t *testing.T) {
	tests := []struct {
		name string
		ev   *pb.PolicyUpdate
		want bool
	}{
		{
			name: "SNAPSHOT with policy is not resync",
			ev: &pb.PolicyUpdate{
				Type:   pb.PolicyUpdate_SNAPSHOT,
				Policy: makeTestPolicy("p1", "Policy 1"),
			},
			want: false,
		},
		{
			name: "CREATED is not resync",
			ev: &pb.PolicyUpdate{
				Type:   pb.PolicyUpdate_CREATED,
				Policy: makeTestPolicy("p1", "Policy 1"),
			},
			want: false,
		},
		{
			name: "UPDATED is not resync",
			ev: &pb.PolicyUpdate{
				Type: pb.PolicyUpdate_UPDATED,
			},
			want: false,
		},
		{
			name: "SNAPSHOT with nil policy is resync",
			ev: &pb.PolicyUpdate{
				Type:   pb.PolicyUpdate_SNAPSHOT,
				Policy: nil,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsResyncSignal(tt.ev); got != tt.want {
				t.Errorf("IsResyncSignal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPolicyHub_PublishResync_SubscriberReceivesSignal(t *testing.T) {
	hub := NewPolicyHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Publish some regular events first.
	hub.Publish(pb.PolicyUpdate_CREATED, makeTestPolicy("p1", "Policy 1"))
	hub.Publish(pb.PolicyUpdate_UPDATED, makeTestPolicy("p1", "Policy 1 v2"))

	ch, unsub := hub.Subscribe(ctx, "")
	defer unsub()

	// Then publish a resync.
	hub.PublishResync()

	select {
	case ev := <-ch:
		if !IsResyncSignal(ev) {
			t.Error("expected resync signal, got regular event")
		}
		if ev.Revision != 3 {
			t.Errorf("resync revision = %d, want 3", ev.Revision)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resync event")
	}
}
