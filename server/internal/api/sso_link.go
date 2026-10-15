// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// ssoLinkApprovalLifetime is how long an approval to link an administrator
// account by email stays valid.
const ssoLinkApprovalLifetime = 24 * time.Hour

// SSOLinkApprovalHandler serves /api/v1/sso-link-approvals/{userID}: an
// administrator allows (POST), withdraws (DELETE) or checks (GET) linking
// an administrator account to a single sign-on identity by email at its
// next sign-in. Accounts without administrative permissions need no
// approval when a login connection links by email.
type SSOLinkApprovalHandler struct {
	authSvc *services.AuthService
	users   *database.UserRepository
}

// NewSSOLinkApprovalHandler creates the handler.
func NewSSOLinkApprovalHandler(authSvc *services.AuthService, users *database.UserRepository) *SSOLinkApprovalHandler {
	return &SSOLinkApprovalHandler{authSvc: authSvc, users: users}
}

func (h *SSOLinkApprovalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller := GetUserFromContext(ctx)
	if caller == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/sso-link-approvals/"), "/")
	target, err := h.users.GetByID(ctx, id)
	if err != nil || target == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}
	admin, err := h.authSvc.IsAdministrator(ctx, target.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to check the account")
		return
	}
	switch r.Method {
	case http.MethodGet:
		until, readErr := h.users.SSOLinkAllowedUntil(ctx, target.ID)
		if readErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to read the approval")
			return
		}
		if until != nil && time.Now().After(*until) {
			until = nil
		}
		writeJSON(w, http.StatusOK, map[string]any{"administrator": admin, "allowed_until": until})
		return
	case http.MethodPost, http.MethodDelete:
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if target.Source != models.SourceLocal && target.Source != models.SourceLDAP {
		writeJSONError(w, http.StatusConflict, "only local and LDAP accounts can be linked")
		return
	}
	// Approving the takeover of an administrator account by an identity
	// provider is itself an administrator-level decision.
	grants, err := h.authSvc.GetUserGrants(ctx, caller.UserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to verify permissions")
		return
	}
	if admin && (!grants.Scope("user", "manage").IsGlobal() || !grants.Scope("settings", "manage").IsGlobal()) {
		writeJSONError(w, http.StatusForbidden, "approving an administrator account needs global user:manage and settings:manage")
		auditDenial(r, "user", "sso_link_approval", target.ID, "caller lacks global user:manage and settings:manage")
		return
	}
	var until *time.Time
	if r.Method == http.MethodPost {
		t := time.Now().Add(ssoLinkApprovalLifetime)
		until = &t
	}
	if err := h.users.SetSSOLinkAllowedUntil(ctx, target.ID, until); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save the approval")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"administrator": admin, "allowed_until": until})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
