// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"

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
	nodeGroups  nodeGroupGetter
	scopesOn    func() bool
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

// WithNodeGroupScopes enables node-group-scoped role bindings, validated
// against groups, while enabled reports true (checked per request). Without
// it (the community default) only global bindings can be created; see the
// edition package.
func (h *UserRoleBindingHandler) WithNodeGroupScopes(groups nodeGroupGetter, enabled func() bool) *UserRoleBindingHandler {
	h.nodeGroups = groups
	h.scopesOn = enabled
	return h
}

// scopeGroups returns the node group lookup when node-group scopes are
// enabled right now, nil otherwise (which validateBindingScope refuses).
func scopeGroups(groups nodeGroupGetter, enabled func() bool) nodeGroupGetter {
	if groups == nil || enabled == nil || !enabled() {
		return nil
	}
	return groups
}

// nodeGroupGetter looks up a node group by ID (nil, nil when absent).
type nodeGroupGetter interface {
	GetByID(ctx context.Context, id string) (*models.NodeGroup, error)
}

// errInvalidScope marks a client error in a role binding's scope fields.
var errInvalidScope = errors.New("invalid scope")

// validateBindingScope normalizes and checks a role binding's scope:
//
//   - "" or "global": no scope_id allowed; returns ("global", nil).
//   - "node_group": scope_id must name an existing node group.
//
// Errors wrapping errInvalidScope are client errors (400); anything else is
// a lookup failure (500).
func validateBindingScope(ctx context.Context, groups nodeGroupGetter, scopeType string, scopeID *string) (normType string, normID *string, err error) {
	id := ""
	if scopeID != nil {
		id = *scopeID
	}

	switch scopeType {
	case "", models.ScopeGlobal:
		if id != "" {
			return "", nil, fmt.Errorf("%w: a global binding takes no scope_id", errInvalidScope)
		}
		return models.ScopeGlobal, nil, nil
	case models.ScopeNodeGroup:
		if !uuidPattern.MatchString(id) {
			return "", nil, fmt.Errorf("%w: scope_id must be a node group ID", errInvalidScope)
		}
		if groups == nil {
			return "", nil, fmt.Errorf("%w: node-group-scoped role assignments are not enabled in this edition", errInvalidScope)
		}
		group, err := groups.GetByID(ctx, id)
		if err != nil {
			return "", nil, fmt.Errorf("failed to look up node group: %w", err)
		}
		if group == nil {
			return "", nil, fmt.Errorf("%w: node group not found", errInvalidScope)
		}
		return models.ScopeNodeGroup, &id, nil
	default:
		return "", nil, fmt.Errorf("%w: scope_type must be 'global' or 'node_group'", errInvalidScope)
	}
}

// uuidPattern matches a canonical UUID string.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// writeScopeError answers a validateBindingScope error with 400 or 500.
func writeScopeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidScope) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("role binding scope validation failed: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "failed to validate binding scope")
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

	scopeType, scopeID, err := validateBindingScope(r.Context(), scopeGroups(h.nodeGroups, h.scopesOn), binding.ScopeType, binding.ScopeID)
	if err != nil {
		writeScopeError(w, err)
		return
	}
	binding.ScopeType, binding.ScopeID = scopeType, scopeID

	// Privilege-escalation guard: the caller may only grant permissions they
	// hold themselves, with at least the reach of the new binding. This blocks
	// a user administrator from binding "Super Admin" (or any role granting
	// more than they hold) to anyone, including themselves, and a caller with
	// only node-group-scoped rights from granting them more widely.
	ok, missing, err := callerCanDelegateRole(r, h.roleRepo, binding.RoleID, bindingTargetOf(binding.ScopeType, binding.ScopeID))
	if err != nil {
		log.Printf("role binding: failed to check role permissions: %v", err)
		http.Error(w, `{"error":"failed to verify role permissions"}`, http.StatusInternalServerError)
		return
	}
	if !ok {
		writeJSONError(w, http.StatusForbidden, "cannot assign a role that grants permissions you do not hold at this scope")
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
		"scope_type": binding.ScopeType,
		"scope_id":   derefString(binding.ScopeID),
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
	if binding.ScopeType != models.ScopeGlobal {
		return nil // a node-group-scoped binding never makes anyone a Super Admin
	}
	role, err := h.roleRepo.GetByName(ctx, models.RoleSuperAdmin)
	if err != nil || role == nil || binding.RoleID != role.ID {
		return nil // not a Super Admin binding
	}
	count, err := h.bindingRepo.CountUsersWithGlobalRole(ctx, role.ID)
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
