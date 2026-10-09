// SPDX-License-Identifier: LGPL-3.0-or-later
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

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/services"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

// scopeAuthorizer returns fixed grants for every user.
type scopeAuthorizer struct {
	grants map[string]authz.Scope
}

func (a *scopeAuthorizer) Grants(context.Context, string) (authz.Grants, error) {
	return authz.NewGrants(a.grants), nil
}

func (a *scopeAuthorizer) HasPermission(ctx context.Context, userID, resource, action string) (bool, error) {
	g, err := a.Grants(ctx, userID)
	return !g.Scope(resource, action).IsEmpty(), err
}

// captureSink records emitted audit events.
type captureSink struct {
	events []*auditpb.AuditEvent
}

func (c *captureSink) Emit(_ context.Context, e *auditpb.AuditEvent) {
	c.events = append(c.events, e)
}

func newAuditingGate(grants map[string]authz.Scope) (*PermissionGate, *captureSink) {
	sink := &captureSink{}
	auditSvc := services.NewAuditService(nil)
	auditSvc.AddSink(sink)
	return NewPermissionGate(&scopeAuthorizer{grants: grants}, auditSvc, false), sink
}

func serve(mw func(http.Handler) http.Handler, h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, req)
	return rec
}

var berlinNodeView = map[string]authz.Scope{"node:view": authz.GroupScope("berlin")}

func TestGate_GlobalRouteRejectsScopedGrant(t *testing.T) {
	gate, sink := newAuditingGate(map[string]authz.Scope{"node:edit": authz.GroupScope("berlin")})

	rec := serve(gate.Require("node", "edit"), func(http.ResponseWriter, *http.Request) {
		t.Fatal("a global route must not admit a node-group-scoped grant")
	}, reqWithUser(http.MethodPut, "/api/v1/whatever"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "global role binding") {
		t.Errorf("body = %q, want a hint that a global binding is needed", rec.Body.String())
	}
	if len(sink.events) != 1 || sink.events[0].GetAction() != "access_denied" {
		t.Fatalf("audit events = %v, want one access_denied", sink.events)
	}
}

func TestGate_ScopedRouteAdmitsScopedGrantAndExposesScope(t *testing.T) {
	gate, _ := newAuditingGate(berlinNodeView)

	called := false
	rec := serve(gate.RequireScoped("node", "view"), func(w http.ResponseWriter, r *http.Request) {
		called = true
		scope := requestScope(r, "node", "view")
		if scope.IsGlobal() || !scope.Allows("berlin") || scope.Allows("paris") {
			t.Errorf("handler scope = %+v, want only berlin", scope)
		}
		if !requestScope(r, "node", "delete").IsEmpty() {
			t.Error("unheld permission should resolve to an empty scope")
		}
		w.WriteHeader(http.StatusOK)
	}, reqWithUser(http.MethodGet, "/api/v1/nodes"))

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("called = %v, status = %d; want handler reached with 200", called, rec.Code)
	}
}

func TestGate_ScopedRouteStillRejectsNoGrant(t *testing.T) {
	gate, sink := newAuditingGate(berlinNodeView)

	rec := serve(gate.RequireScopedMethod([]MethodPermission{
		{Method: http.MethodDelete, Resource: "node", Action: "delete"},
	}), func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be reached without any grant")
	}, reqWithUser(http.MethodDelete, "/api/v1/nodes/n1"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(sink.events) != 1 {
		t.Fatalf("got %d audit events, want 1", len(sink.events))
	}
	var details map[string]string
	if err := json.Unmarshal([]byte(sink.events[0].GetHttpChange().GetBodyJson()), &details); err != nil {
		t.Fatalf("audit details not JSON: %v", err)
	}
	if details["permission"] != "node:delete" {
		t.Errorf("audited permission = %q, want node:delete", details["permission"])
	}
	if sink.events[0].GetOutcome() != auditpb.Outcome_OUTCOME_FAILURE {
		t.Errorf("outcome = %v, want FAILURE", sink.events[0].GetOutcome())
	}
}

func TestGate_DeniedReadsAreNotAudited(t *testing.T) {
	gate, sink := newAuditingGate(nil)

	rec := serve(gate.Require("audit_log", "view"), func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be reached")
	}, reqWithUser(http.MethodGet, "/api/v1/audit-logs"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(sink.events) != 0 {
		t.Errorf("denied GET produced %d audit events, want 0", len(sink.events))
	}
}

func TestRequestScope_WithoutGateFailsClosed(t *testing.T) {
	r := reqWithUser(http.MethodGet, "/api/v1/nodes")
	if !requestScope(r, "node", "view").IsEmpty() {
		t.Error("a request that bypassed the permission gate must resolve to an empty scope")
	}
}

func TestDenyOutOfScope_AuditsWithObjectID(t *testing.T) {
	gate, sink := newAuditingGate(map[string]authz.Scope{"node:delete": authz.GroupScope("berlin")})

	rec := serve(gate.RequireScoped("node", "delete"), func(w http.ResponseWriter, r *http.Request) {
		denyOutOfScope(w, r, "node", "delete", "n42")
	}, reqWithUser(http.MethodDelete, "/api/v1/nodes/n42"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(sink.events) != 1 || sink.events[0].GetResource().GetId() != "n42" {
		t.Fatalf("audit events = %v, want one naming n42", sink.events)
	}
}

func TestPermissionGate_DeniedWriteIsAudited(t *testing.T) {
	gate, sink := newAuditingGate(nil)
	mw := gate.RequireMethod([]MethodPermission{{Method: http.MethodPost, Resource: "user_group", Action: "create"}})

	rec := serve(mw, func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be reached")
	}, reqWithUser(http.MethodPost, "/api/v1/user-groups"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(sink.events) != 1 {
		t.Fatalf("got %d audit events, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if ev.GetAction() != "access_denied" || ev.GetOutcome() != auditpb.Outcome_OUTCOME_FAILURE {
		t.Errorf("event = %s/%v, want access_denied/FAILURE", ev.GetAction(), ev.GetOutcome())
	}
	if ev.GetActor().GetUsername() != "tester" || ev.GetActor().GetUserId() != "test-user" {
		t.Errorf("actor = %+v, want tester/test-user", ev.GetActor())
	}
}

func TestPermissionGate_HandlerDenialHonorsIPAnonymization(t *testing.T) {
	sink := &captureSink{}
	auditSvc := services.NewAuditService(nil)
	auditSvc.AddSink(sink)
	gate := NewPermissionGate(&scopeAuthorizer{grants: map[string]authz.Scope{
		"user_group:create": authz.GlobalScope(),
	}}, auditSvc, true)

	req := reqWithUser(http.MethodPost, "/api/v1/user-groups/g1/members")
	req.RemoteAddr = "203.0.113.77:51000"
	serve(gate.Require("user_group", "create"), func(w http.ResponseWriter, r *http.Request) {
		auditDenial(r, "user_group", "add_member", "g1", "guard refused")
		w.WriteHeader(http.StatusForbidden)
	}, req)

	if len(sink.events) != 1 {
		t.Fatalf("got %d audit events, want 1", len(sink.events))
	}
	if got := sink.events[0].GetSrcIp(); got != "203.0.113.0" {
		t.Errorf("src ip = %q, want anonymized 203.0.113.0", got)
	}
}

func TestAuditDenial_WithoutGateIsANoOp(_ *testing.T) {
	// Must not panic or emit anything when no gate is in the context.
	auditDenial(reqWithUser(http.MethodPost, "/x"), "user_group", "create", "", "test")
}
