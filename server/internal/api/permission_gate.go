// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/services"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PermissionGate builds the permission middleware for the REST API. Besides
// admitting or refusing a request, it records refused state-changing requests
// in the audit log, and it leaves itself in the request context so handlers
// can audit their own refusals (privilege-escalation guards) the same way.
type PermissionGate struct {
	az           authz.Authorizer
	auditSvc     *services.AuditService
	anonymizeIPs bool
}

// NewPermissionGate creates a PermissionGate. auditSvc may be nil (no denial
// auditing).
func NewPermissionGate(az authz.Authorizer, auditSvc *services.AuditService, anonymizeIPs bool) *PermissionGate {
	return &PermissionGate{az: az, auditSvc: auditSvc, anonymizeIPs: anonymizeIPs}
}

// Require admits callers holding resource:action.
func (g *PermissionGate) Require(resource, action string) func(http.Handler) http.Handler {
	return g.gate(func(*http.Request) (string, string, bool) { return resource, action, true })
}

// RequireMethod admits callers holding the permission mapped to the request's
// HTTP method. Unmapped methods get 405.
func (g *PermissionGate) RequireMethod(perms []MethodPermission) func(http.Handler) http.Handler {
	return g.gate(func(r *http.Request) (string, string, bool) {
		for _, p := range perms {
			if p.Method == r.Method {
				return p.Resource, p.Action, true
			}
		}
		return "", "", false
	})
}

func (g *PermissionGate) gate(resolve func(*http.Request) (resource, action string, ok bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			resource, action, ok := resolve(r)
			if !ok {
				http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
				return
			}

			r = r.WithContext(context.WithValue(r.Context(), permissionGateKey{}, g))

			allowed, err := g.az.HasPermission(r.Context(), claims.UserID, resource, action)
			if err != nil {
				log.Printf("authorization check failed for user %s: %v", claims.UserID, err)
				http.Error(w, `{"error":"authorization check failed"}`, http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				auditDenial(r, resource, action, "", "permission not held")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type permissionGateKey struct{}

func gateFromRequest(r *http.Request) *PermissionGate {
	g, _ := r.Context().Value(permissionGateKey{}).(*PermissionGate)
	return g
}

// auditSrcIP returns the client IP for an audit event, anonymized when the
// deployment asks for it (GDPR data minimization). A request that did not
// pass a permission gate falls back to the plain IP.
func auditSrcIP(r *http.Request) string {
	if g := gateFromRequest(r); g != nil {
		return extractAuditIP(r, g.anonymizeIPs)
	}
	return extractIP(r)
}

// writeJSONError writes {"error": msg} with the given status.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("Failed to encode error response: %v", err)
	}
}

// auditDenial records a refused state-changing request as an access_denied
// audit event naming the actor, the permission or guard involved, and the
// reason. objectID may be empty. Reads are not audited: the UI hides what a
// user cannot open, so denied reads are rare and would mostly add noise.
func auditDenial(r *http.Request, resource, action, objectID, reason string) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return
	}
	g := gateFromRequest(r)
	if g == nil || g.auditSvc == nil {
		return
	}

	details, err := json.Marshal(map[string]string{
		"permission": permKey(resource, action),
		"reason":     reason,
	})
	if err != nil {
		return
	}

	actor := &auditpb.Actor{}
	if claims := GetUserFromContext(r.Context()); claims != nil {
		actor.UserId = claims.UserID
		actor.Username = claims.Username
	}

	g.auditSvc.Emit(r.Context(), &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      actor,
		Action:     "access_denied",
		Resource:   &auditpb.Resource{Type: resource, Id: objectID},
		Outcome:    auditpb.Outcome_OUTCOME_FAILURE,
		SrcIp:      auditSrcIP(r),
		Payload: &auditpb.AuditEvent_HttpChange{
			HttpChange: &auditpb.HttpPayload{
				Method:   r.Method,
				Path:     r.URL.Path,
				BodyJson: string(details),
			},
		},
	})
}
