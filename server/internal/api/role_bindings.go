// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// UserRoleBindingHandler handles user role binding endpoints
type UserRoleBindingHandler struct {
	bindingRepo *database.UserRoleBindingRepository
	roleRepo    *database.RoleRepository
}

// NewUserRoleBindingHandler creates a new UserRoleBindingHandler
func NewUserRoleBindingHandler(bindingRepo *database.UserRoleBindingRepository, roleRepo *database.RoleRepository) *UserRoleBindingHandler {
	return &UserRoleBindingHandler{bindingRepo: bindingRepo, roleRepo: roleRepo}
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

	if binding.UserID == "" || binding.RoleID == "" || binding.ScopeType == "" {
		http.Error(w, `{"error":"user_id, role_id, and scope_type are required"}`, http.StatusBadRequest)
		return
	}

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

	if err := h.bindingRepo.Delete(r.Context(), id); err != nil {
		log.Printf("Failed to delete user role binding: %v", err)
		http.Error(w, `{"error":"failed to delete binding"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
