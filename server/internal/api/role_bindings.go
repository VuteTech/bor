// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UserRoleBindingHandler handles user role binding endpoints
type UserRoleBindingHandler struct {
	bindingRepo *database.UserRoleBindingRepository
	roleRepo    *database.RoleRepository
	auditSvc    *services.AuditService
}

// NewUserRoleBindingHandler creates a new UserRoleBindingHandler
func NewUserRoleBindingHandler(bindingRepo *database.UserRoleBindingRepository, roleRepo *database.RoleRepository) *UserRoleBindingHandler {
	return &UserRoleBindingHandler{bindingRepo: bindingRepo, roleRepo: roleRepo}
}

// WithAuditService attaches an AuditService so privilege revocations are
// recorded with the affected user and role (the generic HTTP audit
// middleware only sees the binding UUID on a DELETE).
func (h *UserRoleBindingHandler) WithAuditService(auditSvc *services.AuditService) *UserRoleBindingHandler {
	h.auditSvc = auditSvc
	return h
}

// validateGlobalScope rejects scoped role bindings. RBAC is global-only:
// scoped bindings were never enforced, so accepting them would silently
// create grants that do nothing (or, worse, grants that spring to life if
// scoped enforcement ships later). An empty scope type defaults to global.
func validateGlobalScope(scopeType string, scopeID *string) (string, error) {
	if scopeType == "" {
		scopeType = models.ScopeGlobal
	}
	if scopeType != models.ScopeGlobal || scopeID != nil {
		return "", errors.New("scoped role bindings are not supported; scope_type must be 'global' with no scope_id")
	}
	return scopeType, nil
}

// ListByUser handles GET /api/v1/user-role-bindings?user_id={id}
func (h *UserRoleBindingHandler) ListByUser(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, `{"error":"user_id query parameter required"}`, http.StatusBadRequest)
		return
	}

	bindings, err := h.bindingRepo.ListByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("Failed to list user role bindings: %v", err)
		http.Error(w, `{"error":"failed to list bindings"}`, http.StatusInternalServerError)
		return
	}

	if bindings == nil {
		bindings = []*models.UserRoleBinding{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(bindings); err != nil {
		log.Printf("Failed to encode bindings response: %v", err)
	}
}

// Create handles POST /api/v1/user-role-bindings
func (h *UserRoleBindingHandler) Create(w http.ResponseWriter, r *http.Request) {
	var binding models.UserRoleBinding
	if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if binding.UserID == "" || binding.RoleID == "" {
		http.Error(w, `{"error":"user_id and role_id are required"}`, http.StatusBadRequest)
		return
	}

	scopeType, err := validateGlobalScope(binding.ScopeType, binding.ScopeID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	binding.ScopeType = scopeType

	// Privilege-escalation guard: the caller may only assign a role whose
	// permissions are a subset of the permissions the caller already holds.
	// This blocks a delegated user administrator from binding "Super Admin"
	// (or any higher-privileged role) to anyone, including themselves.
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	callerPerms, err := callerEffectivePermissions(r.Context(), h.roleRepo, h.bindingRepo, claims.UserID)
	if err != nil {
		log.Printf("role binding: failed to load caller permissions: %v", err)
		http.Error(w, `{"error":"failed to verify caller permissions"}`, http.StatusInternalServerError)
		return
	}
	subset, missing, err := rolePermissionsSubsetOf(r.Context(), h.roleRepo, binding.RoleID, callerPerms)
	if err != nil {
		log.Printf("role binding: failed to check role permissions: %v", err)
		http.Error(w, `{"error":"failed to verify role permissions"}`, http.StatusInternalServerError)
		return
	}
	if !subset {
		log.Printf("role binding denied: user %s tried to assign role %s granting unheld permission %q",
			claims.UserID, binding.RoleID, missing)
		http.Error(w, `{"error":"cannot assign a role that grants permissions you do not hold"}`, http.StatusForbidden)
		auditDenial(r, "user_role_binding", "create", binding.UserID, "would grant "+missing+", which the caller does not hold")
		return
	}

	if err := h.bindingRepo.Create(r.Context(), &binding); err != nil {
		log.Printf("Failed to create user role binding: %v", err)
		http.Error(w, `{"error":"failed to create binding"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Printf("Failed to encode binding response: %v", err)
	}
}

// Delete handles DELETE /api/v1/user-role-bindings/{id}
func (h *UserRoleBindingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := extractIDFromPath(r.URL.Path, "/api/v1/user-role-bindings/")
	if id == "" {
		http.Error(w, `{"error":"binding id required"}`, http.StatusBadRequest)
		return
	}

	// Fetch the binding first: the last-Super-Admin guard needs it, and the
	// audit event must record which user and role were affected (after the
	// delete only the UUID would remain).
	binding, err := h.bindingRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Printf("Failed to load role binding before delete: %v", err)
		http.Error(w, `{"error":"failed to delete binding"}`, http.StatusInternalServerError)
		return
	}

	// Refuse to unassign the Super Admin role from the last Super Admin.
	if err := h.guardLastSuperAdminBinding(r.Context(), binding); err != nil {
		if errors.Is(err, services.ErrLastSuperAdmin) {
			http.Error(w, `{"error":"Cannot remove the Super Admin role from the last Super Admin user."}`, http.StatusConflict)
			return
		}
		log.Printf("Failed to check role binding before delete: %v", err)
		http.Error(w, `{"error":"failed to delete binding"}`, http.StatusInternalServerError)
		return
	}

	if err := h.bindingRepo.Delete(r.Context(), id); err != nil {
		log.Printf("Failed to delete user role binding: %v", err)
		http.Error(w, `{"error":"failed to delete binding"}`, http.StatusInternalServerError)
		return
	}

	if binding != nil {
		h.auditRevoke(r, binding)
	}

	w.WriteHeader(http.StatusNoContent)
}

// auditRevoke records a privilege-revocation event naming the affected user
// and role. This complements the generic HTTP audit middleware entry, whose
// DELETE record only carries the binding UUID.
func (h *UserRoleBindingHandler) auditRevoke(r *http.Request, binding *models.UserRoleBinding) {
	if h.auditSvc == nil {
		return
	}

	roleName := ""
	if role, err := h.roleRepo.GetByID(r.Context(), binding.RoleID); err == nil && role != nil {
		roleName = role.Name
	}

	details, err := json.Marshal(map[string]string{
		"binding_id": binding.ID,
		"user_id":    binding.UserID,
		"role_id":    binding.RoleID,
		"role_name":  roleName,
	})
	if err != nil {
		return
	}

	actor := &auditpb.Actor{}
	if claims := GetUserFromContext(r.Context()); claims != nil {
		actor.UserId = claims.UserID
		actor.Username = claims.Username
	}

	h.auditSvc.Emit(r.Context(), &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      actor,
		Action:     "revoke_role",
		Resource:   &auditpb.Resource{Type: "user_role_binding", Id: binding.ID},
		Outcome:    auditpb.Outcome_OUTCOME_SUCCESS,
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

// guardLastSuperAdminBinding returns services.ErrLastSuperAdmin when removing
// the given binding would unassign the Super Admin role from the only
// remaining Super Admin. A nil binding (already gone) passes the guard.
func (h *UserRoleBindingHandler) guardLastSuperAdminBinding(ctx context.Context, binding *models.UserRoleBinding) error {
	if binding == nil {
		return nil // already gone; let Delete handle it
	}
	role, err := h.roleRepo.GetByName(ctx, models.RoleSuperAdmin)
	if err != nil || role == nil || binding.RoleID != role.ID {
		return nil // not a Super Admin binding
	}
	count, err := h.bindingRepo.CountUsersWithRole(ctx, role.ID)
	if err != nil {
		return err
	}
	if count <= 1 {
		return services.ErrLastSuperAdmin
	}
	return nil
}

// ServeHTTP routes requests
func (h *UserRoleBindingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := extractIDFromPath(r.URL.Path, "/api/v1/user-role-bindings/")

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			h.ListByUser(w, r)
		case http.MethodPost:
			h.Create(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.Delete(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}
