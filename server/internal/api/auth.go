// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	authSvc *services.AuthService
	mfaSvc  *services.MFAService
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(authSvc *services.AuthService, mfaSvc *services.MFAService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, mfaSvc: mfaSvc}
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authSvc.Login(r.Context(), &req)
	if err != nil {
		log.Printf("Login failed for user %s: %v", req.Username, err)
		http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode login response: %v", err)
	}
}

// Begin handles POST /api/v1/auth/begin — starts the multi-step auth flow.
func (h *AuthHandler) Begin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.AuthBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authSvc.AuthBegin(r.Context(), &req)
	if err != nil {
		log.Printf("AuthBegin failed for user %s: %v", req.Username, err)
		http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode auth begin response: %v", err)
	}
}

// Step handles POST /api/v1/auth/step — advances the multi-step auth flow.
func (h *AuthHandler) Step(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.AuthStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authSvc.AuthStep(r.Context(), &req)
	if err != nil {
		log.Printf("AuthStep failed: %v", err)
		http.Error(w, `{"error":"authentication failed"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode auth step response: %v", err)
	}
}

// Me handles GET /api/v1/auth/me - returns current user info with permissions
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	user, err := h.authSvc.GetUser(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	permissions, err := h.authSvc.GetUserPermissions(r.Context(), claims.UserID)
	if err != nil {
		log.Printf("Failed to get permissions for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to load permissions"}`, http.StatusInternalServerError)
		return
	}
	if permissions == nil {
		permissions = []string{}
	}

	resp := models.MeResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		FullName:    user.FullName,
		Permissions: permissions,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode me response: %v", err)
	}
}

// MFAStatus handles GET /api/v1/users/me/mfa — returns current user's MFA status.
func (h *AuthHandler) MFAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if h.mfaSvc == nil {
		http.Error(w, `{"error":"MFA not configured"}`, http.StatusServiceUnavailable)
		return
	}

	status, err := h.mfaSvc.GetStatus(r.Context(), claims.UserID)
	if err != nil {
		log.Printf("Failed to get MFA status for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to get MFA status"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Printf("Failed to encode MFA status: %v", err)
	}
}

// MFASetupBegin handles POST /api/v1/users/me/mfa/setup/begin
func (h *AuthHandler) MFASetupBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if h.mfaSvc == nil {
		http.Error(w, `{"error":"MFA not configured"}`, http.StatusServiceUnavailable)
		return
	}

	user, err := h.authSvc.GetUser(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	resp, err := h.mfaSvc.BeginSetup(r.Context(), claims.UserID, user.Username)
	if err != nil {
		log.Printf("MFA setup begin failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to begin MFA setup"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode MFA setup begin response: %v", err)
	}
}

// MFASetupFinish handles POST /api/v1/users/me/mfa/setup/finish
func (h *AuthHandler) MFASetupFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if h.mfaSvc == nil {
		http.Error(w, `{"error":"MFA not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req models.MFASetupFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.mfaSvc.FinishSetup(r.Context(), claims.UserID, req.Code)
	if err != nil {
		log.Printf("MFA setup finish failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"invalid TOTP code"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode MFA setup finish response: %v", err)
	}
}

// MFADisable handles DELETE /api/v1/users/me/mfa (also accepts POST from the route /users/me/mfa/disable)
func (h *AuthHandler) MFADisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if h.mfaSvc == nil {
		http.Error(w, `{"error":"MFA not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req models.MFADisableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Require the current password for local users.
	user, err := h.authSvc.GetUser(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	if user.Source == models.SourceLocal {
		if req.Password == "" {
			http.Error(w, `{"error":"password is required to disable MFA"}`, http.StatusBadRequest)
			return
		}
		if _, err := h.authSvc.Login(r.Context(), &models.LoginRequest{
			Username: user.Username,
			Password: req.Password,
		}); err != nil {
			http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
			return
		}
	}

	if err := h.mfaSvc.Disable(r.Context(), claims.UserID); err != nil {
		log.Printf("MFA disable failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to disable MFA"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
