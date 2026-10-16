// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// MFA enrollment while signing in. When MFA is required and a user who has
// none proves their first factor (password or identity provider), the
// sign-in answers next "mfa_setup" with an enrollment token. These public
// endpoints accept only that token: the user sets up TOTP or a security
// key, and only then is the session started. The token grants nothing
// else and is refused once a factor exists.

type enrollmentRequest struct {
	SessionToken string          `json:"session_token"`
	Code         string          `json:"code,omitempty"`
	Name         string          `json:"name,omitempty"`
	Credential   json.RawMessage `json:"credential,omitempty"`
}

// enrollmentRequestUser decodes the request and resolves its token.
func (h *AuthHandler) enrollmentRequestUser(w http.ResponseWriter, r *http.Request) (*enrollmentRequest, *models.User, *services.AuthSessionClaims, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return nil, nil, nil, false
	}
	var req enrollmentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return nil, nil, nil, false
	}
	user, claims, err := h.authSvc.EnrollmentUser(r.Context(), req.SessionToken)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return nil, nil, nil, false
	}
	return &req, user, claims, true
}

// completeEnrollment starts the session after a factor was set up.
func (h *AuthHandler) completeEnrollment(w http.ResponseWriter, r *http.Request, user *models.User, claims *services.AuthSessionClaims, factor string, backupCodes []string) {
	h.emitAuthAudit(r, "auth.mfa.enroll", user.Username, true, `{"factor":"`+factor+`","via":"sign-in"}`)
	loginResp, err := h.authSvc.IssueTokenByUserID(r.Context(), user.ID)
	if err != nil {
		log.Printf("Failed to issue token after MFA enrollment for user %q: %v", user.ID, err) //nolint:gosec // G706: the ID is quoted with %q
		http.Error(w, `{"error":"failed to issue token"}`, http.StatusInternalServerError)
		return
	}
	h.emitAuthAudit(r, "auth.login", user.Username, true, services.EnrollmentAuditDetail(claims, factor))
	SetSessionCookie(w, loginResp.Token, h.authSvc.TokenLifetime())
	SetCSRFCookie(w)
	if refresh, rErr := h.authSvc.GenerateRefreshToken(&loginResp.User); rErr == nil {
		SetRefreshCookie(w, refresh, h.authSvc.RefreshLifetimeSeconds())
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:gosec // intentionally returning the auth token and one-time backup codes
		"token": loginResp.Token, "user": &loginResp.User, "backup_codes": backupCodes,
	})
}

// EnrollTOTPBegin handles POST /api/v1/auth/enroll/totp/begin.
func (h *AuthHandler) EnrollTOTPBegin(w http.ResponseWriter, r *http.Request) {
	if h.mfaSvc == nil {
		http.Error(w, `{"error":"MFA not configured"}`, http.StatusServiceUnavailable)
		return
	}
	_, user, _, ok := h.enrollmentRequestUser(w, r)
	if !ok {
		return
	}
	setup, err := h.mfaSvc.BeginSetup(r.Context(), user.ID, user.Username)
	if err != nil {
		log.Printf("MFA enrollment begin failed for user %s: %v", user.ID, err)
		http.Error(w, `{"error":"failed to begin MFA setup"}`, http.StatusInternalServerError)
		return
	}
	qr, err := h.mfaSvc.GenerateSetupQR(r.Context(), user.ID, user.Username)
	if err != nil {
		log.Printf("MFA enrollment QR failed for user %s: %v", user.ID, err)
		http.Error(w, `{"error":"failed to begin MFA setup"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{ //nolint:gosec // the TOTP secret is shown to its owner during setup
		"secret": setup.Secret, "algorithm": setup.Algorithm,
		"qr_image": "data:" + http.DetectContentType(qr) + ";base64," + base64.StdEncoding.EncodeToString(qr),
	})
}

// EnrollTOTPFinish handles POST /api/v1/auth/enroll/totp/finish.
func (h *AuthHandler) EnrollTOTPFinish(w http.ResponseWriter, r *http.Request) {
	if h.mfaSvc == nil {
		http.Error(w, `{"error":"MFA not configured"}`, http.StatusServiceUnavailable)
		return
	}
	req, user, claims, ok := h.enrollmentRequestUser(w, r)
	if !ok {
		return
	}
	resp, err := h.mfaSvc.FinishSetup(r.Context(), user.ID, req.Code)
	if err != nil {
		h.emitAuthAudit(r, "auth.mfa.enroll", user.Username, false, `{"factor":"totp","via":"sign-in","result":"failed"}`)
		http.Error(w, `{"error":"invalid TOTP code"}`, http.StatusBadRequest)
		return
	}
	h.completeEnrollment(w, r, user, claims, "totp", resp.BackupCodes)
}

// EnrollWebAuthnBegin handles POST /api/v1/auth/enroll/webauthn/begin.
func (h *AuthHandler) EnrollWebAuthnBegin(w http.ResponseWriter, r *http.Request) {
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	_, user, _, ok := h.enrollmentRequestUser(w, r)
	if !ok {
		return
	}
	options, err := h.webauthnSvc.BeginRegistration(r.Context(), user.ID, user.Username)
	if err != nil {
		log.Printf("WebAuthn enrollment begin failed for user %s: %v", user.ID, err)
		http.Error(w, `{"error":"failed to begin WebAuthn registration"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(options)
}

// EnrollWebAuthnFinish handles POST /api/v1/auth/enroll/webauthn/finish.
func (h *AuthHandler) EnrollWebAuthnFinish(w http.ResponseWriter, r *http.Request) {
	if h.webauthnSvc == nil {
		h.webAuthnNotImplemented(w)
		return
	}
	req, user, claims, ok := h.enrollmentRequestUser(w, r)
	if !ok {
		return
	}
	name := req.Name
	if name == "" {
		name = "Security key"
	}
	if _, err := h.webauthnSvc.FinishRegistration(r.Context(), user.ID, user.Username, name, req.Credential); err != nil {
		log.Printf("WebAuthn enrollment finish failed for user %s: %v", user.ID, err)
		h.emitAuthAudit(r, "auth.mfa.enroll", user.Username, false, `{"factor":"webauthn","via":"sign-in","result":"failed"}`)
		http.Error(w, `{"error":"WebAuthn registration failed"}`, http.StatusBadRequest)
		return
	}
	h.completeEnrollment(w, r, user, claims, "webauthn", nil)
}
