// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UserGroupHandler handles user group API endpoints (identity domain)
type UserGroupHandler struct {
	userGroupSvc *services.UserGroupService
	memberRepo   *database.UserGroupMemberRepository
	bindingRepo  *database.UserGroupRoleBindingRepository
	roleRepo     *database.RoleRepository
	auditSvc     *services.AuditService
	// userBindingRepo resolves the caller's own permissions for the
	// privilege-escalation guards. Without it, adding group roles or members
	// is refused.
	userBindingRepo *database.UserRoleBindingRepository
}

// NewUserGroupHandler creates a new UserGroupHandler
func NewUserGroupHandler(
	userGroupSvc *services.UserGroupService,
	memberRepo *database.UserGroupMemberRepository,
	bindingRepo *database.UserGroupRoleBindingRepository,
) *UserGroupHandler {
	return &UserGroupHandler{
		userGroupSvc: userGroupSvc,
		memberRepo:   memberRepo,
		bindingRepo:  bindingRepo,
	}
}

// WithAuditService attaches an AuditService (and a role repository for
// resolving role names) so group privilege revocations are recorded with the
// affected group and role (the generic HTTP audit middleware only sees the
// binding UUID on a DELETE).
func (h *UserGroupHandler) WithAuditService(auditSvc *services.AuditService, roleRepo *database.RoleRepository) *UserGroupHandler {
	h.auditSvc = auditSvc
	h.roleRepo = roleRepo
	return h
}

// WithRoleGuard enables the privilege-escalation guards on group roles and
// group membership. Roles bound to a user group are granted to its members,
// so both adding a role to a group and adding a member to a group hand out
// permissions; the caller must already hold every one of them.
func (h *UserGroupHandler) WithRoleGuard(userBindingRepo *database.UserRoleBindingRepository) *UserGroupHandler {
	h.userBindingRepo = userBindingRepo
	return h
}

// guardRoleGrant answers 403 (and audits it) unless the caller holds every
// permission of roleIDs. It answers 500 when the guard is not wired or the
// check fails, so a misconfigured handler refuses rather than allows.
func (h *UserGroupHandler) guardRoleGrant(w http.ResponseWriter, r *http.Request, groupID, action string, roleIDs []string) bool {
	if h.roleRepo == nil || h.userBindingRepo == nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to verify role permissions")
		return false
	}
	ok, missing, err := callerCanGrantRoles(r.Context(), h.roleRepo, h.userBindingRepo, callerID(r.Context()), roleIDs)
	if err != nil {
		log.Printf("user group %s: failed to check role permissions: %v", action, err)
		writeJSONError(w, http.StatusInternalServerError, "failed to verify role permissions")
		return false
	}
	if !ok {
		writeJSONError(w, http.StatusForbidden, "cannot grant a role with permissions you do not hold")
		auditDenial(r, "user_group", action, groupID, "would grant "+missing+", which the caller does not hold")
		return false
	}
	return true
}

// ServeHTTP routes user-groups requests including sub-resources
func (h *UserGroupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, sub, subID := parseUserGroupPath(r.URL.Path)

	// Collection-level: /api/v1/user-groups
	if id == "" {
		switch r.Method {
		case http.MethodGet:
			h.List(w, r)
		case http.MethodPost:
			h.Create(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	// Sub-resource: /api/v1/user-groups/{id}/members or /api/v1/user-groups/{id}/role-bindings
	if sub != "" {
		switch sub {
		case "members":
			h.handleMembers(w, r, id, subID)
		case "role-bindings":
			h.handleRoleBindings(w, r, id, subID)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
		return
	}

	// Single resource: /api/v1/user-groups/{id}
	switch r.Method {
	case http.MethodGet:
		h.Get(w, r, id)
	case http.MethodPut:
		h.Update(w, r, id)
	case http.MethodDelete:
		h.Delete(w, r, id)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// List handles GET /api/v1/user-groups
func (h *UserGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	groups, err := h.userGroupSvc.ListUserGroups(r.Context())
	if err != nil {
		log.Printf("Failed to list user groups: %v", err)
		http.Error(w, `{"error":"failed to list user groups"}`, http.StatusInternalServerError)
		return
	}

	if groups == nil {
		groups = []*models.UserGroup{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(groups); err != nil {
		log.Printf("Failed to encode user groups response: %v", err)
	}
}

// Create handles POST /api/v1/user-groups
func (h *UserGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateUserGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	group, err := h.userGroupSvc.CreateUserGroup(r.Context(), &req)
	if err != nil {
		log.Printf("Failed to create user group: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("Failed to encode user group response: %v", err)
	}
}

// Get handles GET /api/v1/user-groups/{id}
func (h *UserGroupHandler) Get(w http.ResponseWriter, r *http.Request, id string) {
	group, err := h.userGroupSvc.GetUserGroup(r.Context(), id)
	if err != nil || group == nil {
		http.Error(w, `{"error":"user group not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("Failed to encode user group response: %v", err)
	}
}

// Update handles PUT /api/v1/user-groups/{id}
func (h *UserGroupHandler) Update(w http.ResponseWriter, r *http.Request, id string) {
	var req models.UpdateUserGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	group, err := h.userGroupSvc.UpdateUserGroup(r.Context(), id, &req)
	if err != nil {
		log.Printf("Failed to update user group: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("Failed to encode user group response: %v", err)
	}
}

// Delete handles DELETE /api/v1/user-groups/{id}
func (h *UserGroupHandler) Delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.userGroupSvc.DeleteUserGroup(r.Context(), id); err != nil {
		log.Printf("Failed to delete user group: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMembers routes member sub-resource requests
func (h *UserGroupHandler) handleMembers(w http.ResponseWriter, r *http.Request, groupID, memberID string) {
	if memberID == "" {
		switch r.Method {
		case http.MethodGet:
			h.ListMembers(w, r, groupID)
		case http.MethodPost:
			h.AddMember(w, r, groupID)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.RemoveMember(w, r, memberID)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// ListMembers handles GET /api/v1/user-groups/{id}/members
func (h *UserGroupHandler) ListMembers(w http.ResponseWriter, r *http.Request, groupID string) {
	members, err := h.memberRepo.ListByGroupID(r.Context(), groupID)
	if err != nil {
		log.Printf("Failed to list group members: %v", err)
		http.Error(w, `{"error":"failed to list members"}`, http.StatusInternalServerError)
		return
	}

	if members == nil {
		members = []*models.UserGroupMember{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(members); err != nil {
		log.Printf("Failed to encode members response: %v", err)
	}
}

// AddMember handles POST /api/v1/user-groups/{id}/members
func (h *UserGroupHandler) AddMember(w http.ResponseWriter, r *http.Request, groupID string) {
	var req models.AddGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		http.Error(w, `{"error":"user_id is required"}`, http.StatusBadRequest)
		return
	}

	// Membership confers every role bound to the group.
	groupBindings, err := h.bindingRepo.ListByGroupID(r.Context(), groupID)
	if err != nil {
		log.Printf("Failed to list group role bindings: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to verify role permissions")
		return
	}
	roleIDs := make([]string, 0, len(groupBindings))
	for _, b := range groupBindings {
		roleIDs = append(roleIDs, b.RoleID)
	}
	if !h.guardRoleGrant(w, r, groupID, "add_member", roleIDs) {
		return
	}

	member := &models.UserGroupMember{
		GroupID: groupID,
		UserID:  req.UserID,
	}
	if err := h.memberRepo.Create(r.Context(), member); err != nil {
		log.Printf("Failed to add group member: %v", err)
		http.Error(w, `{"error":"failed to add member"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(member); err != nil {
		log.Printf("Failed to encode member response: %v", err)
	}
}

// RemoveMember handles DELETE /api/v1/user-groups/{id}/members/{member_id}
func (h *UserGroupHandler) RemoveMember(w http.ResponseWriter, r *http.Request, memberID string) {
	if err := h.memberRepo.Delete(r.Context(), memberID); err != nil {
		log.Printf("Failed to remove group member: %v", err)
		http.Error(w, `{"error":"failed to remove member"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRoleBindings routes role binding sub-resource requests
func (h *UserGroupHandler) handleRoleBindings(w http.ResponseWriter, r *http.Request, groupID, bindingID string) {
	if bindingID == "" {
		switch r.Method {
		case http.MethodGet:
			h.ListGroupRoleBindings(w, r, groupID)
		case http.MethodPost:
			h.AddGroupRoleBinding(w, r, groupID)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.RemoveGroupRoleBinding(w, r, bindingID)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// ListGroupRoleBindings handles GET /api/v1/user-groups/{id}/role-bindings
func (h *UserGroupHandler) ListGroupRoleBindings(w http.ResponseWriter, r *http.Request, groupID string) {
	bindings, err := h.bindingRepo.ListByGroupID(r.Context(), groupID)
	if err != nil {
		log.Printf("Failed to list group role bindings: %v", err)
		http.Error(w, `{"error":"failed to list role bindings"}`, http.StatusInternalServerError)
		return
	}

	if bindings == nil {
		bindings = []*models.UserGroupRoleBinding{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(bindings); err != nil {
		log.Printf("Failed to encode role bindings response: %v", err)
	}
}

// AddGroupRoleBinding handles POST /api/v1/user-groups/{id}/role-bindings
func (h *UserGroupHandler) AddGroupRoleBinding(w http.ResponseWriter, r *http.Request, groupID string) {
	var req models.CreateGroupRoleBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.RoleID == "" {
		http.Error(w, `{"error":"role_id is required"}`, http.StatusBadRequest)
		return
	}

	scopeType, err := validateGlobalScope(req.ScopeType, req.ScopeID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Every member of the group receives the role.
	if !h.guardRoleGrant(w, r, groupID, "add_role", []string{req.RoleID}) {
		return
	}

	binding := &models.UserGroupRoleBinding{
		GroupID:   groupID,
		RoleID:    req.RoleID,
		ScopeType: scopeType,
	}
	if err := h.bindingRepo.Create(r.Context(), binding); err != nil {
		log.Printf("Failed to create group role binding: %v", err)
		http.Error(w, `{"error":"failed to create role binding"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Printf("Failed to encode role binding response: %v", err)
	}
}

// RemoveGroupRoleBinding handles DELETE /api/v1/user-groups/{id}/role-bindings/{binding_id}
func (h *UserGroupHandler) RemoveGroupRoleBinding(w http.ResponseWriter, r *http.Request, bindingID string) {
	// Fetch first so the audit event can name the affected group and role;
	// after the delete only the UUID would remain.
	binding, err := h.bindingRepo.GetByID(r.Context(), bindingID)
	if err != nil {
		log.Printf("Failed to load group role binding before delete: %v", err)
		http.Error(w, `{"error":"failed to delete role binding"}`, http.StatusInternalServerError)
		return
	}

	if err := h.bindingRepo.Delete(r.Context(), bindingID); err != nil {
		log.Printf("Failed to delete group role binding: %v", err)
		http.Error(w, `{"error":"failed to delete role binding"}`, http.StatusInternalServerError)
		return
	}

	if binding != nil {
		h.auditGroupRoleRevoke(r, binding)
	}

	w.WriteHeader(http.StatusNoContent)
}

// auditGroupRoleRevoke records a privilege-revocation event naming the
// affected user group and role. This complements the generic HTTP audit
// middleware entry, whose DELETE record only carries the binding UUID.
func (h *UserGroupHandler) auditGroupRoleRevoke(r *http.Request, binding *models.UserGroupRoleBinding) {
	if h.auditSvc == nil {
		return
	}

	roleName := ""
	if h.roleRepo != nil {
		if role, err := h.roleRepo.GetByID(r.Context(), binding.RoleID); err == nil && role != nil {
			roleName = role.Name
		}
	}

	details, err := json.Marshal(map[string]string{
		"binding_id": binding.ID,
		"group_id":   binding.GroupID,
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
		Resource:   &auditpb.Resource{Type: "user_group_role_binding", Id: binding.ID},
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

// parseUserGroupPath extracts group ID, sub-resource name, and sub-resource ID from paths like:
//
//	/api/v1/user-groups/{id}
//	/api/v1/user-groups/{id}/members
//	/api/v1/user-groups/{id}/members/{member_id}
//	/api/v1/user-groups/{id}/role-bindings
//	/api/v1/user-groups/{id}/role-bindings/{binding_id}
func parseUserGroupPath(path string) (id, sub, subID string) {
	const prefix = "/api/v1/user-groups/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return "", "", ""
	}

	parts := strings.SplitN(rest, "/", 3)
	id = parts[0]
	if len(parts) >= 2 {
		sub = parts[1]
	}
	if len(parts) >= 3 {
		subID = parts[2]
	}
	return id, sub, subID
}
