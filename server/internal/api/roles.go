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
)

// RoleHandler handles role management endpoints
type RoleHandler struct {
	roleRepo    *database.RoleRepository
	permRepo    *database.PermissionRepository
	bindingRepo *database.UserRoleBindingRepository
}

// NewRoleHandler creates a new RoleHandler
func NewRoleHandler(roleRepo *database.RoleRepository, permRepo *database.PermissionRepository, bindingRepo *database.UserRoleBindingRepository) *RoleHandler {
	return &RoleHandler{roleRepo: roleRepo, permRepo: permRepo, bindingRepo: bindingRepo}
}

// ServeHTTP routes role requests
func (h *RoleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is a permissions sub-resource request: /api/v1/roles/{id}/permissions
	path := r.URL.Path
	if idx := strings.Index(path, "/permissions"); idx > 0 {
		roleID := extractRoleID(path)
		if roleID == "" {
			http.Error(w, `{"error":"role id required"}`, http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.GetRolePermissions(w, r, roleID)
		case http.MethodPut:
			h.SetRolePermissions(w, r, roleID)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	id := extractIDFromPath(r.URL.Path, "/api/v1/roles/")

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

// extractRoleID extracts the role ID from paths like /api/v1/roles/{id}/permissions
func extractRoleID(path string) string {
	const prefix = "/api/v1/roles/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return parts[0]
}

// List handles GET /api/v1/roles
func (h *RoleHandler) List(w http.ResponseWriter, r *http.Request) {
	roles, err := h.roleRepo.List(r.Context())
	if err != nil {
		log.Printf("Failed to list roles: %v", err)
		http.Error(w, `{"error":"failed to list roles"}`, http.StatusInternalServerError)
		return
	}

	// Build response with permission counts
	type roleWithCount struct {
		models.Role
		PermissionCount int `json:"permission_count"`
	}

	result := make([]roleWithCount, 0, len(roles))
	for _, role := range roles {
		perms, err := h.roleRepo.GetPermissionsByRoleID(r.Context(), role.ID)
		if err != nil {
			log.Printf("Failed to get permissions for role %s: %v", role.ID, err)
			result = append(result, roleWithCount{Role: *role, PermissionCount: 0})
			continue
		}
		result = append(result, roleWithCount{Role: *role, PermissionCount: len(perms)})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Failed to encode roles response: %v", err)
	}
}

// Get handles GET /api/v1/roles/{id}
func (h *RoleHandler) Get(w http.ResponseWriter, r *http.Request, id string) {
	role, err := h.roleRepo.GetByID(r.Context(), id)
	if err != nil || role == nil {
		http.Error(w, `{"error":"role not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(role); err != nil {
		log.Printf("Failed to encode role response: %v", err)
	}
}

// Create handles POST /api/v1/roles
func (h *RoleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	role := &models.Role{
		Name:        req.Name,
		Description: req.Description,
	}

	if err := h.roleRepo.Create(r.Context(), role); err != nil {
		log.Printf("Failed to create role: %v", err)
		http.Error(w, `{"error":"failed to create role"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(role); err != nil {
		log.Printf("Failed to encode role response: %v", err)
	}
}

// Update handles PUT /api/v1/roles/{id}
func (h *RoleHandler) Update(w http.ResponseWriter, r *http.Request, id string) {
	role, err := h.roleRepo.GetByID(r.Context(), id)
	if err != nil || role == nil {
		http.Error(w, `{"error":"role not found"}`, http.StatusNotFound)
		return
	}

	var req models.UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := h.roleRepo.Update(r.Context(), id, &req); err != nil {
		log.Printf("Failed to update role: %v", err)
		http.Error(w, `{"error":"failed to update role"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete handles DELETE /api/v1/roles/{id}
func (h *RoleHandler) Delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.roleRepo.Delete(r.Context(), id); err != nil {
		log.Printf("Failed to delete role: %v", err)
		http.Error(w, `{"error":"failed to delete role"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetRolePermissions handles GET /api/v1/roles/{id}/permissions
func (h *RoleHandler) GetRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	perms, err := h.roleRepo.GetPermissionsByRoleID(r.Context(), roleID)
	if err != nil {
		log.Printf("Failed to get role permissions: %v", err)
		http.Error(w, `{"error":"failed to get role permissions"}`, http.StatusInternalServerError)
		return
	}

	if perms == nil {
		perms = []*models.Permission{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(perms); err != nil {
		log.Printf("Failed to encode permissions response: %v", err)
	}
}

// SetRolePermissions handles PUT /api/v1/roles/{id}/permissions
func (h *RoleHandler) SetRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	var req models.SetRolePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := h.roleRepo.SetPermissions(r.Context(), roleID, req.PermissionIDs); err != nil {
		log.Printf("Failed to set role permissions: %v", err)
		http.Error(w, `{"error":"failed to set role permissions"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListAllPermissions handles GET /api/v1/permissions
func (h *RoleHandler) ListAllPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	perms, err := h.permRepo.List(r.Context())
	if err != nil {
		log.Printf("Failed to list permissions: %v", err)
		http.Error(w, `{"error":"failed to list permissions"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(perms); err != nil {
		log.Printf("Failed to encode permissions response: %v", err)
	}
}
