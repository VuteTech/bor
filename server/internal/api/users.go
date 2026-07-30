// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// UserHandler handles user management endpoints
type UserHandler struct {
	authSvc *services.AuthService
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(authSvc *services.AuthService) *UserHandler {
	return &UserHandler{authSvc: authSvc}
}

// List handles GET /api/v1/users
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	users, err := h.authSvc.ListUsers(r.Context(), limit, offset)
	if err != nil {
		log.Printf("Failed to list users: %v", err)
		http.Error(w, `{"error":"failed to list users"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		log.Printf("Failed to encode users response: %v", err)
	}
}

// Create handles POST /api/v1/users
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Privilege-escalation guard: the caller cannot assign a role granting
	// permissions beyond their own.
	if claims := GetUserFromContext(r.Context()); claims != nil {
		if err := h.authSvc.EnsureCallerCanAssignRole(r.Context(), claims.UserID, req.RoleName); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusForbidden)
			return
		}
	}

	user, err := h.authSvc.CreateUser(r.Context(), &req)
	if err != nil {
		log.Printf("Failed to create user: %v", err)
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
	if err := json.NewEncoder(w).Encode(user); err != nil {
		log.Printf("Failed to encode user response: %v", err)
	}
}

// Get handles GET /api/v1/users/{id}
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractIDFromPath(r.URL.Path, "/api/v1/users/")
	if id == "" {
		http.Error(w, `{"error":"user id required"}`, http.StatusBadRequest)
		return
	}

	user, err := h.authSvc.GetUser(r.Context(), id)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(user); err != nil {
		log.Printf("Failed to encode user response: %v", err)
	}
}

// Update handles PUT /api/v1/users/{id}
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractIDFromPath(r.URL.Path, "/api/v1/users/")
	if id == "" {
		http.Error(w, `{"error":"user id required"}`, http.StatusBadRequest)
		return
	}

	var req models.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Email == nil && req.FullName == nil && req.Enabled == nil {
		http.Error(w, `{"error":"at least one field must be provided"}`, http.StatusBadRequest)
		return
	}

	if err := h.authSvc.UpdateUser(r.Context(), id, &req); err != nil {
		if errors.Is(err, services.ErrLastSuperAdmin) {
			http.Error(w, `{"error":"Cannot disable the last Super Admin user."}`, http.StatusConflict)
			return
		}
		log.Printf("Failed to update user: %v", err)
		http.Error(w, `{"error":"failed to update user"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete handles DELETE /api/v1/users/{id}
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractIDFromPath(r.URL.Path, "/api/v1/users/")
	if id == "" {
		http.Error(w, `{"error":"user id required"}`, http.StatusBadRequest)
		return
	}

	if err := h.authSvc.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, services.ErrLastSuperAdmin) {
			http.Error(w, `{"error":"Cannot delete the last Super Admin user."}`, http.StatusConflict)
			return
		}
		log.Printf("Failed to delete user: %v", err)
		http.Error(w, `{"error":"failed to delete user"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SetPassword handles PUT /api/v1/users/{id}/password — admin sets a user's password.
// Only works for local accounts; no knowledge of the current password is required.
func (h *UserHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractIDFromPath(strings.TrimSuffix(r.URL.Path, "/password"), "/api/v1/users/")
	if id == "" {
		http.Error(w, `{"error":"user id required"}`, http.StatusBadRequest)
		return
	}

	var req models.AdminSetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.NewPassword == "" {
		http.Error(w, `{"error":"new_password is required"}`, http.StatusBadRequest)
		return
	}

	if err := h.authSvc.AdminSetPassword(r.Context(), id, req.NewPassword); err != nil {
		log.Printf("AdminSetPassword failed for user %q: %v", id, err) //nolint:gosec // id is a UUID from a URL path; %q prevents injection
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ServeHTTP routes requests to the appropriate handler method
func (h *UserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Sub-resource: /api/v1/users/{id}/password
	if strings.HasSuffix(r.URL.Path, "/password") {
		h.SetPassword(w, r)
		return
	}

	id := extractIDFromPath(r.URL.Path, "/api/v1/users/")

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
		h.Get(w, r)
	case http.MethodPut:
		h.Update(w, r)
	case http.MethodDelete:
		h.Delete(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// extractIDFromPath extracts a resource ID from a URL path
func extractIDFromPath(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	id = strings.TrimSuffix(id, "/")
	return id
}
