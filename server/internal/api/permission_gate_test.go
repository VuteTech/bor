// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

// captureSink records emitted audit events.
type captureSink struct {
	events []*auditpb.AuditEvent
}

func (c *captureSink) Emit(_ context.Context, e *auditpb.AuditEvent) {
	c.events = append(c.events, e)
}

func newAuditingGate(allowed map[string]bool, anonymize bool) (*PermissionGate, *captureSink) {
	sink := &captureSink{}
	auditSvc := services.NewAuditService(nil)
	auditSvc.AddSink(sink)
	return NewPermissionGate(&permCheckingAuthorizer{allowed: allowed}, auditSvc, anonymize), sink
}

func TestPermissionGate_DeniedWriteIsAudited(t *testing.T) {
	gate, sink := newAuditingGate(nil, false)
	mw := gate.RequireMethod([]MethodPermission{{Method: http.MethodPost, Resource: "user_group", Action: "create"}})

	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be reached")
	})).ServeHTTP(rec, reqWithUser(http.MethodPost, "/api/v1/user-groups"))

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
	var details map[string]string
	if err := json.Unmarshal([]byte(ev.GetHttpChange().GetBodyJson()), &details); err != nil {
		t.Fatalf("details not JSON: %v", err)
	}
	if details["permission"] != "user_group:create" {
		t.Errorf("permission = %q, want user_group:create", details["permission"])
	}
}

func TestPermissionGate_DeniedReadIsNotAudited(t *testing.T) {
	gate, sink := newAuditingGate(nil, false)

	rec := httptest.NewRecorder()
	gate.Require("audit_log", "view")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be reached")
	})).ServeHTTP(rec, reqWithUser(http.MethodGet, "/api/v1/audit-logs"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(sink.events) != 0 {
		t.Errorf("denied GET produced %d audit events, want 0", len(sink.events))
	}
}

func TestPermissionGate_AllowedRequestReachesHandlerWithAuditContext(t *testing.T) {
	gate, sink := newAuditingGate(map[string]bool{"user_group:create": true}, true)

	req := reqWithUser(http.MethodPost, "/api/v1/user-groups/g1/members")
	req.RemoteAddr = "203.0.113.77:51000"
	rec := httptest.NewRecorder()
	gate.Require("user_group", "create")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A handler-level guard refusal is audited through the same gate,
		// honoring IP anonymization.
		auditDenial(r, "user_group", "add_member", "g1", "guard refused")
		w.WriteHeader(http.StatusForbidden)
	})).ServeHTTP(rec, req)

	if len(sink.events) != 1 {
		t.Fatalf("got %d audit events, want 1", len(sink.events))
	}
	if got := sink.events[0].GetSrcIp(); got != "203.0.113.0" {
		t.Errorf("src ip = %q, want anonymized 203.0.113.0", got)
	}
	if got := sink.events[0].GetResource().GetId(); got != "g1" {
		t.Errorf("resource id = %q, want g1", got)
	}
}

func TestAuditDenial_WithoutGateIsANoOp(_ *testing.T) {
	// Must not panic or emit anything when no gate is in the context.
	auditDenial(reqWithUser(http.MethodPost, "/x"), "user_group", "create", "", "test")
}

func TestFirstUnheldPermission(t *testing.T) {
	held := map[string]struct{}{"policy:view": {}, "policy:edit": {}}
	perm := func(r, a string) *models.Permission { return &models.Permission{Resource: r, Action: a} }

	tests := []struct {
		name  string
		perms []*models.Permission
		want  string
	}{
		{"role with no permissions", nil, ""},
		{"all held", []*models.Permission{perm("policy", "view"), perm("policy", "edit")}, ""},
		{"one missing", []*models.Permission{perm("policy", "view"), perm("user", "manage")}, "user:manage"},
		{"first missing reported", []*models.Permission{perm("role", "edit"), perm("user", "manage")}, "role:edit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstUnheldPermission(held, tt.perms); got != tt.want {
				t.Errorf("firstUnheldPermission = %q, want %q", got, tt.want)
			}
		})
	}
}
