// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
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
