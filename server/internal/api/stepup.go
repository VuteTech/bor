// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// StepUpHandler serves POST /api/v1/auth/step-up: re-authentication that
// yields a single-use, short-lived token for one privileged action
// . Generic so future privileged
// actions can reuse it.
type StepUpHandler struct {
	svc *services.StepUpService

	// Federated accounts re-authenticate at their identity provider.
	users           func(ctx context.Context, id string) (*models.User, error)
	resolveExternal func(federationProvider, username, purpose string) (string, bool)
}

// WithExternalStepUp lets server extensions re-authenticate the federated
// accounts they own: resolve returns the URL that starts it.
func (h *StepUpHandler) WithExternalStepUp(users func(ctx context.Context, id string) (*models.User, error), resolve func(federationProvider, username, purpose string) (string, bool)) *StepUpHandler {
	h.users = users
	h.resolveExternal = resolve
	return h
}

// Method handles GET /api/v1/auth/step-up/method?purpose=...: it tells the
// web UI how the signed-in user re-authenticates: "password" (with a TOTP
// code when asked), or "redirect" to url in a popup followed by Collect.
func (h *StepUpHandler) Method(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		diskEncHTTPError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp := models.StepUpMethodResponse{Method: "password"}
	if h.users != nil {
		user, err := h.users(r.Context(), claims.UserID)
		if err != nil || user == nil {
			diskEncHTTPError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if user.Source == models.SourceFederated {
			provider := ""
			if user.FederationProvider != nil {
				provider = *user.FederationProvider
			}
			url, ok := "", false
			if h.resolveExternal != nil {
				url, ok = h.resolveExternal(provider, user.Username, r.URL.Query().Get("purpose"))
			}
			if !ok {
				diskEncHTTPError(w, http.StatusServiceUnavailable, "re-authentication through your identity provider is not available")
				return
			}
			resp = models.StepUpMethodResponse{Method: "redirect", URL: url}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	diskEncJSON(w, http.StatusOK, &resp)
}

// Collect handles POST /api/v1/auth/step-up/collect: after a federated
// re-authentication it returns the step-up token, asking for a TOTP code
// first when one is still needed (409 "totp_required").
func (h *StepUpHandler) Collect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		diskEncHTTPError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		TOTPCode string `json:"totp_code"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			diskEncHTTPError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	c, err := r.Cookie(PendingStepUpCookieName)
	if err != nil || c.Value == "" {
		diskEncHTTPError(w, http.StatusUnauthorized, "re-authenticate first")
		return
	}
	token, _, err := h.svc.CollectFederated(r.Context(), c.Value, claims.UserID, req.TOTPCode)
	switch {
	case errors.Is(err, services.ErrStepUpTOTPNeeded):
		diskEncHTTPError(w, http.StatusConflict, "totp_required")
		return
	case errors.Is(err, services.ErrStepUpMFARequired):
		clearPendingStepUpCookie(w)
		diskEncHTTPError(w, http.StatusForbidden, err.Error())
		return
	case errors.Is(err, services.ErrStepUpTokenInvalid):
		clearPendingStepUpCookie(w)
		diskEncHTTPError(w, http.StatusUnauthorized, "re-authenticate first")
		return
	case errors.Is(err, services.ErrStepUpInvalid):
		diskEncHTTPError(w, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		log.Printf("step-up: collect: %v", err)
		diskEncHTTPError(w, http.StatusInternalServerError, "failed to issue step-up token")
		return
	}
	clearPendingStepUpCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	diskEncJSON(w, http.StatusOK, &models.StepUpResponse{Token: token, ExpiresIn: h.svc.Lifetime()})
}

// NewStepUpHandler creates a StepUpHandler.
func NewStepUpHandler(svc *services.StepUpService) *StepUpHandler {
	return &StepUpHandler{svc: svc}
}

// ServeHTTP handles POST /api/v1/auth/step-up. The caller must already hold
// a valid session (AuthMiddleware); the request body carries the fresh
// credentials.
func (h *StepUpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		diskEncHTTPError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req models.StepUpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		diskEncHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, err := h.svc.Issue(r.Context(), claims.UserID, claims.Username, req.Purpose, req.Password, req.TOTPCode)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrStepUpMFARequired):
			diskEncHTTPError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, services.ErrStepUpInvalid):
			diskEncHTTPError(w, http.StatusUnauthorized, err.Error())
		default:
			log.Printf("step-up: issue: %v", err)
			diskEncHTTPError(w, http.StatusInternalServerError, "failed to issue step-up token")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	diskEncJSON(w, http.StatusOK, &models.StepUpResponse{Token: token, ExpiresIn: h.svc.Lifetime()})
}
