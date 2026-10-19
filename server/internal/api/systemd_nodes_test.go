// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

type fakeNodeLookup struct{ node *models.Node }

func (f fakeNodeLookup) GetNode(context.Context, string) (*models.Node, error) { return f.node, nil }

type fakeUnitStore struct {
	units []database.SystemdCatalogueUnit
}

func (f fakeUnitStore) ListUnits(context.Context, string, string) ([]database.SystemdCatalogueUnit, error) {
	return f.units, nil
}

func (f fakeUnitStore) ListUnitsByNode(context.Context, string, string, string) ([]database.SystemdCatalogueUnit, error) {
	return f.units, nil
}

type fakeActionSender struct {
	connected bool
	sent      *pb.SystemdUnitAction
	onSend    func(*pb.SystemdUnitAction)
}

func (f *fakeActionSender) SendSystemdUnitAction(_ string, a *pb.SystemdUnitAction) bool {
	f.sent = a
	if f.onSend != nil {
		f.onSend(a)
	}
	return f.connected
}

func systemdNodeRequest(method, path, body string, grants map[string]authz.Scope) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, http.NoBody)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.SetPathValue("id", "n1")
	st := &authzState{userID: "u1", grants: authz.NewGrants(grants)}
	return r.WithContext(context.WithValue(r.Context(), authzContextKey{}, st))
}

func TestSystemdNodeHandler_ListUnits(t *testing.T) {
	node := &models.Node{ID: "n1", Name: "lab-01", NodeGroupIDs: []string{"g1"}}
	store := fakeUnitStore{units: []database.SystemdCatalogueUnit{{Info: &pb.SystemdUnitInfo{Name: "cups.service", Type: "service", ActiveState: "active", SecurityExposure: "9.6", SecurityPredicate: "UNSAFE"}, NodeCount: 1}}}
	h := NewSystemdNodeHandler(fakeNodeLookup{node}, store, nil, nil, nil, false)

	rr := httptest.NewRecorder()
	h.ListUnits(rr, systemdNodeRequest(http.MethodGet, "/api/v1/nodes/n1/systemd/units", "", map[string]authz.Scope{"node:view": authz.GroupScope("g1")}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var out []systemdUnitResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || len(out) != 1 || out[0].SecurityPredicate != "UNSAFE" {
		t.Fatalf("body %s (%v)", rr.Body.String(), err)
	}

	rr = httptest.NewRecorder()
	h.ListUnits(rr, systemdNodeRequest(http.MethodGet, "/api/v1/nodes/n1/systemd/units", "", map[string]authz.Scope{"node:view": authz.GroupScope("other")}))
	if rr.Code != http.StatusNotFound {
		t.Errorf("out-of-scope node: status %d, want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ListUnits(rr, systemdNodeRequest(http.MethodGet, "/api/v1/nodes/n1/systemd/units?type=mount", "", map[string]authz.Scope{"node:view": authz.GlobalScope()}))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad type: status %d, want 400", rr.Code)
	}
}

func TestSystemdNodeHandler_Action(t *testing.T) {
	node := &models.Node{ID: "n1", Name: "lab-01", NodeGroupIDs: []string{"g1"}}
	allowed := map[string]authz.Scope{"node:view": authz.GlobalScope(), "systemd:operate": authz.GlobalScope()}
	broker := services.NewSystemdActionBroker()
	sender := &fakeActionSender{connected: true}
	h := NewSystemdNodeHandler(fakeNodeLookup{node}, fakeUnitStore{}, sender, broker, nil, false)
	h.timeout = 2 * time.Second

	// The agent answers: the request returns its result and the unit state.
	sender.onSend = func(a *pb.SystemdUnitAction) {
		go broker.Deliver(a.GetId(), "lab-01", services.SystemdUnitActionResult{Success: true, Message: "restart: done", Unit: &pb.SystemdUnitInfo{Name: a.GetUnit(), Type: "service", ActiveState: "active", SubState: "running"}})
	}
	rr := httptest.NewRecorder()
	h.Action(rr, systemdNodeRequest(http.MethodPost, "/api/v1/nodes/n1/systemd/actions", `{"unit":"cups.service","action":"Restart"}`, allowed))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var res systemdActionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil || !res.Success || res.Action != "restart" || res.UnitState == nil || res.UnitState.ActiveState != "active" {
		t.Fatalf("body %s (%v)", rr.Body.String(), err)
	}
	if sender.sent.GetAction() != pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_RESTART || sender.sent.GetUnit() != "cups.service" || len(sender.sent.GetId()) != 32 {
		t.Errorf("sent %+v", sender.sent)
	}
	if broker.Pending() != 0 {
		t.Errorf("pending actions after completion: %d", broker.Pending())
	}

	// Validation.
	for _, body := range []string{`{"unit":"dbus.service","action":"stop"}`, `{"unit":"cups.service","action":"enable"}`, `{"unit":"multi-user.target","action":"start"}`, `not json`} {
		rr = httptest.NewRecorder()
		h.Action(rr, systemdNodeRequest(http.MethodPost, "/api/v1/nodes/n1/systemd/actions", body, allowed))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, rr.Code)
		}
	}

	// Scope: the caller holds systemd:operate for another group only.
	rr = httptest.NewRecorder()
	h.Action(rr, systemdNodeRequest(http.MethodPost, "/api/v1/nodes/n1/systemd/actions", `{"unit":"cups.service","action":"stop"}`, map[string]authz.Scope{"node:view": authz.GlobalScope(), "systemd:operate": authz.GroupScope("other")}))
	if rr.Code != http.StatusForbidden {
		t.Errorf("out-of-scope action: status %d, want 403", rr.Code)
	}

	// The node is not connected.
	sender.connected = false
	sender.onSend = nil
	rr = httptest.NewRecorder()
	h.Action(rr, systemdNodeRequest(http.MethodPost, "/api/v1/nodes/n1/systemd/actions", `{"unit":"cups.service","action":"stop"}`, allowed))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("offline node: status %d, want 503", rr.Code)
	}
	if broker.Pending() != 0 {
		t.Errorf("pending actions after a failed send: %d", broker.Pending())
	}

	// The agent never answers.
	sender.connected = true
	h.timeout = 50 * time.Millisecond
	rr = httptest.NewRecorder()
	h.Action(rr, systemdNodeRequest(http.MethodPost, "/api/v1/nodes/n1/systemd/actions", `{"unit":"cups.service","action":"stop"}`, allowed))
	if rr.Code != http.StatusGatewayTimeout {
		t.Errorf("silent agent: status %d, want 504", rr.Code)
	}
	if broker.Pending() != 0 {
		t.Errorf("pending actions after a timeout: %d", broker.Pending())
	}
}
