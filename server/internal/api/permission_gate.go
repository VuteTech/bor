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

// PermissionGate builds the permission middleware for the REST API.
//
// There are two kinds of gate, and the split is what keeps node-group-scoped
// (delegated) administration fail-closed:
//
//   - Require / RequireMethod admit only callers holding the permission
//     globally. This is the default for every route.
//   - RequireScoped / RequireScopedMethod also admit callers holding the
//     permission for some node groups only. They must be used solely on
//     routes whose handlers enforce node-group scope per object (via
//     requestScope); a handler that forgets to is therefore never reachable
//     by a scoped caller in the first place.
//
// Both kinds store the caller's grants in the request context so handlers
// can resolve object-level scope without another database round trip.
// Refused state-changing requests, and refusals by the privilege-escalation
// guards behind a gate, are recorded as access_denied audit events.
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

// Require admits callers holding resource:action globally.
func (g *PermissionGate) Require(resource, action string) func(http.Handler) http.Handler {
	return g.gate(fixedPermission(resource, action), false)
}

// RequireScoped admits callers holding resource:action globally or for at
// least one node group. The handler must enforce object-level scope.
func (g *PermissionGate) RequireScoped(resource, action string) func(http.Handler) http.Handler {
	return g.gate(fixedPermission(resource, action), true)
}

// RequireMethod admits callers holding, globally, the permission mapped to
// the request's HTTP method. Unmapped methods get 405.
func (g *PermissionGate) RequireMethod(perms []MethodPermission) func(http.Handler) http.Handler {
	return g.gate(methodPermission(perms), false)
}

// RequireScopedMethod is RequireMethod admitting node-group-scoped grants.
// The handler must enforce object-level scope.
func (g *PermissionGate) RequireScopedMethod(perms []MethodPermission) func(http.Handler) http.Handler {
	return g.gate(methodPermission(perms), true)
}

type permissionResolver func(r *http.Request) (resource, action string, ok bool)

func fixedPermission(resource, action string) permissionResolver {
	return func(*http.Request) (string, string, bool) { return resource, action, true }
}

func methodPermission(perms []MethodPermission) permissionResolver {
	return func(r *http.Request) (string, string, bool) {
		for _, p := range perms {
			if p.Method == r.Method {
				return p.Resource, p.Action, true
			}
		}
		return "", "", false
	}
}

func (g *PermissionGate) gate(resolve permissionResolver, scoped bool) func(http.Handler) http.Handler {
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

			grants, err := g.az.Grants(r.Context(), claims.UserID)
			if err != nil {
				log.Printf("authorization check failed for user %s: %v", claims.UserID, err)
				http.Error(w, `{"error":"authorization check failed"}`, http.StatusInternalServerError)
				return
			}

			held := grants.Scope(resource, action)
			state := &authzState{userID: claims.UserID, grants: grants, gate: g}
			r = r.WithContext(context.WithValue(r.Context(), authzContextKey{}, state))

			switch {
			case held.IsGlobal(), scoped && !held.IsEmpty():
				next.ServeHTTP(w, r)
			case held.IsEmpty():
				denyRequest(w, r, resource, action, "", "permission not held")
			default:
				// Held for some node groups only, on a route that needs the
				// permission everywhere.
				writeJSONError(w, http.StatusForbidden, "this operation requires a global role binding")
				auditDenial(r, resource, action, "", "permission held only for node groups; route requires a global grant")
			}
		})
	}
}

// authzState is what a permission gate leaves in the request context.
type authzState struct {
	userID string
	grants authz.Grants
	gate   *PermissionGate
}

type authzContextKey struct{}

func authzFromRequest(r *http.Request) *authzState {
	st, _ := r.Context().Value(authzContextKey{}).(*authzState)
	return st
}

// requestScope returns where the caller holds resource:action, as resolved
// by the permission gate in front of this handler. A request that did not
// pass a permission gate gets an empty scope, so a handler wired without one
// denies instead of granting.
func requestScope(r *http.Request, resource, action string) authz.Scope {
	st := authzFromRequest(r)
	if st == nil {
		return authz.Scope{}
	}
	return st.grants.Scope(resource, action)
}

// requestGrants returns the caller's grants from the permission gate, or
// empty grants when the request did not pass one.
func requestGrants(r *http.Request) authz.Grants {
	st := authzFromRequest(r)
	if st == nil {
		return authz.Grants{}
	}
	return st.grants
}

// auditSrcIP returns the client IP for an audit event, anonymized when the
// deployment asks for it (GDPR data minimization). A request that did not
// pass a permission gate falls back to the plain IP.
func auditSrcIP(r *http.Request) string {
	if st := authzFromRequest(r); st != nil && st.gate != nil {
		return extractAuditIP(r, st.gate.anonymizeIPs)
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

// denyRequest answers 403 and audits the denial when the request changes
// state. objectID may be empty.
func denyRequest(w http.ResponseWriter, r *http.Request, resource, action, objectID, reason string) {
	writeJSONError(w, http.StatusForbidden, "insufficient permissions")
	auditDenial(r, resource, action, objectID, reason)
}

// denyOutOfScope answers 403 for an object the caller can see but may not
// change from their node-group scope, and audits it.
func denyOutOfScope(w http.ResponseWriter, r *http.Request, resource, action, objectID string) {
	writeJSONError(w, http.StatusForbidden, "this object is outside your node-group scope")
	auditDenial(r, resource, action, objectID, "object outside the caller's node-group scope")
}

// auditDenial records a refused state-changing request as an access_denied
// audit event naming the actor, the permission or guard involved, and the
// reason. objectID may be empty. Reads are not audited: the UI hides what a
// user cannot open, so denied reads are rare and would mostly add noise.
func auditDenial(r *http.Request, resource, action, objectID, reason string) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return
	}
	st := authzFromRequest(r)
	if st == nil || st.gate == nil || st.gate.auditSvc == nil {
		return
	}

	details, err := json.Marshal(map[string]string{
		"permission": authz.PermKey(resource, action),
		"reason":     reason,
	})
	if err != nil {
		return
	}

	actor := &auditpb.Actor{UserId: st.userID}
	if claims := GetUserFromContext(r.Context()); claims != nil {
		actor.Username = claims.Username
	}

	st.gate.auditSvc.Emit(r.Context(), &auditpb.AuditEvent{
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
