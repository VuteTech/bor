// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// StepUpHeader carries the single-use step-up token on privileged requests.
const StepUpHeader = "X-Bor-Step-Up"

// DiskEncryptionHandler serves /api/v1/disk-encryption: the fleet directory
// (the "recovery-key directory"), per-node detail, reveal and rotate.
type DiskEncryptionHandler struct {
	svc          *services.DiskEncryptionService
	stepUp       *services.StepUpService
	anonymizeIPs bool
}

// NewDiskEncryptionHandler creates a DiskEncryptionHandler.
func NewDiskEncryptionHandler(svc *services.DiskEncryptionService, stepUp *services.StepUpService, anonymizeIPs bool) *DiskEncryptionHandler {
	return &DiskEncryptionHandler{svc: svc, stepUp: stepUp, anonymizeIPs: anonymizeIPs}
}

func diskEncJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("disk_encryption: encode response: %v", err)
	}
}

func diskEncHTTPError(w http.ResponseWriter, status int, msg string) {
	diskEncJSON(w, status, map[string]string{"error": msg})
}

// diskEncServiceError maps service errors to HTTP responses.
func diskEncServiceError(w http.ResponseWriter, err error, context string) {
	var verr *services.DiskEncryptionValidationError
	switch {
	case errors.As(err, &verr):
		diskEncHTTPError(w, http.StatusBadRequest, verr.Msg)
	case errors.Is(err, services.ErrLuksVolumeNotFound), errors.Is(err, services.ErrRecoveryKeyNotFound):
		diskEncHTTPError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrEscrowNotConfigured):
		diskEncHTTPError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("disk_encryption: %s: %v", context, err)
		diskEncHTTPError(w, http.StatusInternalServerError, "failed to "+context)
	}
}

// Summary handles GET /api/v1/disk-encryption/summary.
func (h *DiskEncryptionHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sum, err := h.svc.Summary(r.Context())
	if err != nil {
		diskEncServiceError(w, err, "load disk encryption summary")
		return
	}
	diskEncJSON(w, http.StatusOK, sum)
}

// Volumes routes /api/v1/disk-encryption/volumes and
// /api/v1/disk-encryption/volumes/{id}[/reveal|/rotate].
// Permission gating happens in the route wiring (view for reads, reveal and
// rotate for the respective actions).
func (h *DiskEncryptionHandler) Volumes(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/disk-encryption/volumes"), "/")
	if rest == "" {
		if r.Method != http.MethodGet {
			diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.list(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		h.get(w, r, id)
	case len(parts) == 2 && parts[1] == "reveal" && r.Method == http.MethodPost:
		h.reveal(w, r, id)
	case len(parts) == 2 && parts[1] == "rotate" && r.Method == http.MethodPost:
		h.rotate(w, r, id)
	default:
		diskEncHTTPError(w, http.StatusNotFound, "not found")
	}
}

// IsRevealPath reports whether the request targets the reveal action
// (used by the route wiring to apply the reveal permission and skip the
// generic audit middleware - the handler emits explicit events).
func IsRevealPath(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reveal")
}

// IsRotatePath reports whether the request targets the rotate action.
func IsRotatePath(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rotate")
}

func (h *DiskEncryptionHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := h.svc.ListVolumes(r.Context(), q.Get("search"), q.Get("node_id"),
		atoiDefault(q.Get("page"), 1), atoiDefault(q.Get("per_page"), 25))
	if err != nil {
		diskEncServiceError(w, err, "list volumes")
		return
	}
	diskEncJSON(w, http.StatusOK, resp)
}

func (h *DiskEncryptionHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	detail, err := h.svc.GetVolumeDetail(r.Context(), id)
	if err != nil {
		diskEncServiceError(w, err, "get volume")
		return
	}
	diskEncJSON(w, http.StatusOK, detail)
}

// NodeDetail handles GET /api/v1/disk-encryption/nodes/{id}: platform facts
// and volumes for the node drawer.
func (h *DiskEncryptionHandler) NodeDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/disk-encryption/nodes"), "/")
	if id == "" || strings.Contains(id, "/") {
		diskEncHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	detail, err := h.svc.NodeDetail(r.Context(), id)
	if err != nil {
		diskEncServiceError(w, err, "get node disk encryption")
		return
	}
	diskEncJSON(w, http.StatusOK, detail)
}

// reveal handles POST /api/v1/disk-encryption/volumes/{id}/reveal.
// It requires a valid single-use step-up token (X-Bor-Step-Up) on top of the
// disk_encryption:reveal permission, and emits the explicit
// recoverykey.reveal / recoverykey.reveal_denied audit events itself - this
// route deliberately bypasses the generic audit middleware so no duplicate
// row is written and no key material can end up in a request log.
func (h *DiskEncryptionHandler) reveal(w http.ResponseWriter, r *http.Request, volumeID string) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		diskEncHTTPError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	srcIP := extractAuditIP(r, h.anonymizeIPs)

	var req models.RevealRecoveryKeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		diskEncHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.stepUp.Consume(r.Header.Get(StepUpHeader), claims.UserID, services.StepUpPurposeRevealRecoveryKey); err != nil {
		h.svc.EmitRevealAudit(r.Context(), claims.Username, claims.UserID, volumeID, srcIP, false, map[string]interface{}{
			"error":  "step-up validation failed",
			"reason": req.Reason,
		})
		diskEncHTTPError(w, http.StatusForbidden, err.Error())
		return
	}

	resp, err := h.svc.Reveal(r.Context(), claims.Username, volumeID, &req)
	if err != nil {
		h.svc.EmitRevealAudit(r.Context(), claims.Username, claims.UserID, volumeID, srcIP, false, map[string]interface{}{
			"error":  err.Error(),
			"reason": req.Reason,
		})
		diskEncServiceError(w, err, "reveal recovery key")
		return
	}
	h.svc.EmitRevealAudit(r.Context(), claims.Username, claims.UserID, volumeID, srcIP, true, map[string]interface{}{
		"escrow_id":        resp.EscrowID,
		"reason":           req.Reason,
		"rotation_pending": resp.RotationPending,
	})

	// The key is displayed once and never cached anywhere.
	w.Header().Set("Cache-Control", "no-store")
	diskEncJSON(w, http.StatusOK, resp)
}

// rotate handles POST /api/v1/disk-encryption/volumes/{id}/rotate.
func (h *DiskEncryptionHandler) rotate(w http.ResponseWriter, r *http.Request, volumeID string) {
	claims := GetUserFromContext(r.Context())
	actor := ""
	if claims != nil {
		actor = claims.Username
	}
	if err := h.svc.RequestRotation(r.Context(), actor, volumeID); err != nil {
		diskEncServiceError(w, err, "request rotation")
		return
	}
	diskEncJSON(w, http.StatusAccepted, map[string]string{"status": "rotation requested"})
}
