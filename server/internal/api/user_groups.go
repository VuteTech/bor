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
)

// UserGroupHandler handles user group API endpoints (identity domain)
type UserGroupHandler struct {
	userGroupSvc *services.UserGroupService
	memberRepo   *database.UserGroupMemberRepository
	bindingRepo  *database.UserGroupRoleBindingRepository
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
func (h *UserGroupHandler) handleMembers(w http.ResponseWriter, r *http.Request, groupID string, memberID string) {
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
func (h *UserGroupHandler) handleRoleBindings(w http.ResponseWriter, r *http.Request, groupID string, bindingID string) {
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

	if req.RoleID == "" || req.ScopeType == "" {
		http.Error(w, `{"error":"role_id and scope_type are required"}`, http.StatusBadRequest)
		return
	}

	binding := &models.UserGroupRoleBinding{
		GroupID:   groupID,
		RoleID:    req.RoleID,
		ScopeType: req.ScopeType,
		ScopeID:   req.ScopeID,
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
	if err := h.bindingRepo.Delete(r.Context(), bindingID); err != nil {
		log.Printf("Failed to delete group role binding: %v", err)
		http.Error(w, `{"error":"failed to delete role binding"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseUserGroupPath extracts group ID, sub-resource name, and sub-resource ID from paths like:
//   /api/v1/user-groups/{id}
//   /api/v1/user-groups/{id}/members
//   /api/v1/user-groups/{id}/members/{member_id}
//   /api/v1/user-groups/{id}/role-bindings
//   /api/v1/user-groups/{id}/role-bindings/{binding_id}
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
