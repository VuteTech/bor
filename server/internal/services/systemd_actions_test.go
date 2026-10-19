// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"testing"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestSystemdActionBroker(t *testing.T) {
	b := NewSystemdActionBroker()
	id, results, forget, err := b.NewAction("lab-01")
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	if len(id) != 32 || b.Pending() != 1 {
		t.Fatalf("id %q, pending %d", id, b.Pending())
	}
	if b.Deliver(id, "other-node", SystemdUnitActionResult{Success: true}) {
		t.Fatal("a result from another node must not be delivered")
	}
	if b.Deliver("0123456789abcdef0123456789abcdef", "lab-01", SystemdUnitActionResult{}) {
		t.Fatal("an unknown id must not be delivered")
	}
	want := SystemdUnitActionResult{Success: true, Message: "restart: done", Unit: &pb.SystemdUnitInfo{Name: "cups.service"}}
	if !b.Deliver(id, "lab-01", want) {
		t.Fatal("Deliver must find the pending action")
	}
	select {
	case got := <-results:
		if got.Message != want.Message || got.Unit.GetName() != "cups.service" {
			t.Errorf("result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no result")
	}
	if b.Pending() != 0 {
		t.Errorf("pending after delivery = %d", b.Pending())
	}
	forget()

	id2, _, forget2, _ := b.NewAction("lab-01")
	forget2()
	if b.Deliver(id2, "lab-01", SystemdUnitActionResult{}) {
		t.Fatal("a forgotten action must not be delivered")
	}
}
