// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	authSvc          *services.AuthService
	mfaSvc           *services.MFAService
	webauthnSvc      *services.WebAuthnService
	privacyPolicyURL string
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(authSvc *services.AuthService, mfaSvc *services.MFAService, webauthnSvc *services.WebAuthnService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, mfaSvc: mfaSvc, webauthnSvc: webauthnSvc}
}

// WithPrivacyPolicyURL sets the privacy policy URL returned by PublicConfig.
func (h *AuthHandler) WithPrivacyPolicyURL(url string) *AuthHandler {
	h.privacyPolicyURL = url
	return h
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

	SetSessionCookie(w, resp.Token, h.authSvc.TokenLifetime())
	SetCSRFCookie(w)
	if refreshToken, err := h.authSvc.GenerateRefreshToken(&resp.User); err == nil {
		SetRefreshCookie(w, refreshToken, h.authSvc.RefreshLifetimeSeconds())
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
	if err := json.NewEncoder(w).Encode(resp); err != nil { //nolint:gosec // intentionally marshaling auth tokens in auth endpoint response
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

	// When the final JWT is issued, set it as an httpOnly cookie.
	if resp.Token != "" && resp.User != nil {
		SetSessionCookie(w, resp.Token, h.authSvc.TokenLifetime())
		SetCSRFCookie(w)
		if refreshToken, rtErr := h.authSvc.GenerateRefreshToken(resp.User); rtErr == nil {
			SetRefreshCookie(w, refreshToken, h.authSvc.RefreshLifetimeSeconds())
		}
	} else if resp.Token != "" {
		SetSessionCookie(w, resp.Token, h.authSvc.TokenLifetime())
		SetCSRFCookie(w)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil { //nolint:gosec // intentionally marshaling auth tokens in auth endpoint response
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
	if err := json.NewEncoder(w).Encode(resp); err != nil { //nolint:gosec // intentionally marshaling auth tokens in auth endpoint response
		log.Printf("Failed to encode MFA setup begin response: %v", err)
	}
}

// MFASetupQR handles GET /api/v1/users/me/mfa/setup/qr
// Returns the QR code as a PNG image, generated server-side so the TOTP
// secret is never sent to an external service.
func (h *AuthHandler) MFASetupQR(w http.ResponseWriter, r *http.Request) {
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

	user, err := h.authSvc.GetUser(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	png, err := h.mfaSvc.GenerateSetupQR(r.Context(), claims.UserID, user.Username)
	if err != nil {
		log.Printf("MFA QR generation failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to generate QR code"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
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

// webAuthnNotImplemented returns 501 when WebAuthn is not configured.
func (h *AuthHandler) webAuthnNotImplemented(w http.ResponseWriter) {
	http.Error(w, `{"error":"WebAuthn not configured"}`, http.StatusNotImplemented)
}

// WebAuthnRegisterBegin handles POST /api/v1/users/me/webauthn/register/begin
func (h *AuthHandler) WebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
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
	optionsJSON, err := h.webauthnSvc.BeginRegistration(r.Context(), claims.UserID, user.Username)
	if err != nil {
		log.Printf("WebAuthn register begin failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to begin WebAuthn registration"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(optionsJSON)
}

// WebAuthnRegisterFinish handles POST /api/v1/users/me/webauthn/register/finish
func (h *AuthHandler) WebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
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
	var body struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	err = json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	cred, err := h.webauthnSvc.FinishRegistration(r.Context(), claims.UserID, user.Username, body.Name, body.Credential)
	if err != nil {
		log.Printf("WebAuthn register finish failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"WebAuthn registration failed"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cred); err != nil {
		log.Printf("Failed to encode WebAuthn credential: %v", err)
	}
}

// WebAuthnListCredentials handles GET /api/v1/users/me/webauthn/credentials
func (h *AuthHandler) WebAuthnListCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	creds, err := h.webauthnSvc.ListCredentials(r.Context(), claims.UserID)
	if err != nil {
		log.Printf("WebAuthn list credentials failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to list credentials"}`, http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []*models.WebAuthnCredential{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(creds); err != nil {
		log.Printf("Failed to encode credentials: %v", err)
	}
}

// WebAuthnRenameCredential handles PUT /api/v1/users/me/webauthn/credentials/{id}
func (h *AuthHandler) WebAuthnRenameCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	// Extract credential ID from URL path: /api/v1/users/me/webauthn/credentials/{id}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	credID := parts[len(parts)-1]
	if credID == "" {
		http.Error(w, `{"error":"missing credential id"}`, http.StatusBadRequest)
		return
	}
	var req models.RenameWebAuthnCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.webauthnSvc.RenameCredential(r.Context(), credID, claims.UserID, req.Name); err != nil {
		log.Printf("WebAuthn rename credential failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to rename credential"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// WebAuthnDeleteCredential handles DELETE /api/v1/users/me/webauthn/credentials/{id}
func (h *AuthHandler) WebAuthnDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	credID := parts[len(parts)-1]
	if credID == "" {
		http.Error(w, `{"error":"missing credential id"}`, http.StatusBadRequest)
		return
	}
	if err := h.webauthnSvc.DeleteCredential(r.Context(), credID, claims.UserID); err != nil {
		log.Printf("WebAuthn delete credential failed for user %s: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to delete credential"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// WebAuthnCredentialHandler routes GET/PUT/DELETE for /api/v1/users/me/webauthn/credentials and /.../{id}
func (h *AuthHandler) WebAuthnCredentialHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.WebAuthnListCredentials(w, r)
	case http.MethodPut:
		h.WebAuthnRenameCredential(w, r)
	case http.MethodDelete:
		h.WebAuthnDeleteCredential(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// WebAuthnAuthBegin handles POST /api/v1/auth/webauthn/begin (public)
// Body: {"session_token": "..."}
func (h *AuthHandler) WebAuthnAuthBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	var body struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	sessionClaims, err := h.authSvc.ValidateSessionToken(body.SessionToken)
	if err != nil {
		http.Error(w, `{"error":"invalid session token"}`, http.StatusUnauthorized)
		return
	}
	optionsJSON, err := h.webauthnSvc.BeginAuthentication(r.Context(), sessionClaims.UserID)
	if err != nil {
		log.Printf("WebAuthn auth begin failed for user %s: %v", sessionClaims.UserID, err)
		http.Error(w, `{"error":"failed to begin WebAuthn authentication"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(optionsJSON); err != nil {
		log.Printf("Failed to write WebAuthn auth begin response: %v", err)
	}
}

// WebAuthnAuthFinish handles POST /api/v1/auth/webauthn/finish (public)
// Body: {"session_token": "...", "credential": <WebAuthn JSON>}
func (h *AuthHandler) WebAuthnAuthFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	var body struct {
		SessionToken string          `json:"session_token"`
		Credential   json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	sessionClaims, err := h.authSvc.ValidateSessionToken(body.SessionToken)
	if err != nil {
		http.Error(w, `{"error":"invalid session token"}`, http.StatusUnauthorized)
		return
	}
	err = h.webauthnSvc.FinishAuthentication(r.Context(), sessionClaims.UserID, body.Credential)
	if err != nil {
		log.Printf("WebAuthn auth finish failed for user %s: %v", sessionClaims.UserID, err)
		http.Error(w, `{"error":"WebAuthn authentication failed"}`, http.StatusUnauthorized)
		return
	}
	// WebAuthn fully authenticates the user — issue the final JWT directly, no password needed.
	loginResp, err := h.authSvc.IssueTokenByUserID(r.Context(), sessionClaims.UserID)
	if err != nil {
		log.Printf("Failed to issue token after WebAuthn for user %s: %v", sessionClaims.UserID, err)
		http.Error(w, `{"error":"failed to issue token"}`, http.StatusInternalServerError)
		return
	}
	resp := models.AuthStepResponse{
		Token: loginResp.Token,
		User:  &loginResp.User,
	}
	SetSessionCookie(w, loginResp.Token, h.authSvc.TokenLifetime())
	SetCSRFCookie(w)
	if refreshToken, rtErr := h.authSvc.GenerateRefreshToken(&loginResp.User); rtErr == nil {
		SetRefreshCookie(w, refreshToken, h.authSvc.RefreshLifetimeSeconds())
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil { //nolint:gosec // intentionally marshaling auth tokens in auth endpoint response
		log.Printf("Failed to encode WebAuthn auth finish response: %v", err)
	}
}

// Logout handles POST /api/v1/auth/logout — clears the session cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	ClearSessionCookie(w)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// Refresh handles POST /api/v1/auth/refresh — issues a new access token
// using the bor_refresh httpOnly cookie. No auth middleware is needed since
// the refresh cookie itself is the credential.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie(RefreshCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error":"missing refresh token"}`, http.StatusUnauthorized)
		return
	}

	claims, err := h.authSvc.ValidateRefreshToken(cookie.Value)
	if err != nil {
		log.Printf("Refresh token validation failed: %v", err)
		http.Error(w, `{"error":"invalid or expired refresh token"}`, http.StatusUnauthorized)
		return
	}

	user, err := h.authSvc.GetUser(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusUnauthorized)
		return
	}
	if !user.Enabled {
		http.Error(w, `{"error":"user account is disabled"}`, http.StatusUnauthorized)
		return
	}

	loginResp, err := h.authSvc.IssueTokenByUserID(r.Context(), claims.UserID)
	if err != nil {
		log.Printf("Failed to issue token during refresh for user %q: %v", claims.UserID, err)
		http.Error(w, `{"error":"failed to issue token"}`, http.StatusInternalServerError)
		return
	}

	SetSessionCookie(w, loginResp.Token, h.authSvc.TokenLifetime())
	SetCSRFCookie(w)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// DataExport handles GET /api/v1/users/me/export — GDPR data subject access request.
// Returns a JSON document with all personal data stored for the requesting user.
func (h *AuthHandler) DataExport(w http.ResponseWriter, r *http.Request) {
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

	export := map[string]any{
		"user":        user,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	}

	// MFA status (enabled/disabled, NOT the secret)
	if h.mfaSvc != nil {
		status, mfaErr := h.mfaSvc.GetStatus(r.Context(), claims.UserID)
		if mfaErr == nil && status != nil {
			export["mfa"] = map[string]any{
				"enabled":   status.Enabled,
				"algorithm": status.Algorithm,
			}
		}
	}

	// WebAuthn credential metadata (NOT keys)
	if h.webauthnSvc != nil {
		creds, credErr := h.webauthnSvc.ListCredentials(r.Context(), claims.UserID)
		if credErr == nil && len(creds) > 0 {
			export["webauthn_credentials"] = creds
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="bor-data-export.json"`)
	if err := json.NewEncoder(w).Encode(export); err != nil {
		log.Printf("Failed to encode data export for user %s: %v", claims.UserID, err)
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

// PublicConfig handles GET /api/v1/config — returns non-sensitive server
// configuration needed by the frontend before the user is authenticated.
func (h *AuthHandler) PublicConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"privacy_policy_url": h.privacyPolicyURL,
	}); err != nil {
		log.Printf("PublicConfig: encode error: %v", err)
	}
}
