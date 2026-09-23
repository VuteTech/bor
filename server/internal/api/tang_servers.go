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

// TangServersHandler serves /api/v1/tang-servers (Settings -> Tang servers):
// CRUD, probe and check.
type TangServersHandler struct {
	svc *services.TangServerService
}

// NewTangServersHandler creates a TangServersHandler.
func NewTangServersHandler(svc *services.TangServerService) *TangServersHandler {
	return &TangServersHandler{svc: svc}
}

// tangServiceError maps service errors to HTTP responses.
func tangServiceError(w http.ResponseWriter, err error, context string) {
	var verr *services.DiskEncryptionValidationError
	switch {
	case errors.As(err, &verr):
		diskEncHTTPError(w, http.StatusBadRequest, verr.Msg)
	case errors.Is(err, services.ErrTangServerNotFound):
		diskEncHTTPError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrTangServerConflict), errors.Is(err, services.ErrTangServerInUse):
		diskEncHTTPError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("tang: %s: %v", context, err)
		diskEncHTTPError(w, http.StatusInternalServerError, "failed to "+context)
	}
}

// ServeHTTP routes /api/v1/tang-servers, /api/v1/tang-servers/probe and
// /api/v1/tang-servers/{id}[/check].
func (h *TangServersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/tang-servers"), "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if rest == "probe" && r.Method == http.MethodPost {
		h.probe(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.get(w, r, id)
		case http.MethodPut:
			h.update(w, r, id)
		case http.MethodDelete:
			h.delete(w, r, id)
		default:
			diskEncHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "check" && r.Method == http.MethodPost {
		h.check(w, r, id)
		return
	}
	diskEncHTTPError(w, http.StatusNotFound, "not found")
}

func (h *TangServersHandler) list(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.List(r.Context())
	if err != nil {
		tangServiceError(w, err, "list tang servers")
		return
	}
	diskEncJSON(w, http.StatusOK, resp)
}

func (h *TangServersHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	srv, err := h.svc.Get(r.Context(), id)
	if err != nil {
		tangServiceError(w, err, "get tang server")
		return
	}
	diskEncJSON(w, http.StatusOK, srv)
}

func (h *TangServersHandler) create(w http.ResponseWriter, r *http.Request) {
	var req models.TangServerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		diskEncHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	createdBy := ""
	if claims := GetUserFromContext(r.Context()); claims != nil {
		createdBy = claims.Username
	}
	srv, err := h.svc.Create(r.Context(), &req, createdBy)
	if err != nil {
		tangServiceError(w, err, "create tang server")
		return
	}
	diskEncJSON(w, http.StatusCreated, srv)
}

func (h *TangServersHandler) update(w http.ResponseWriter, r *http.Request, id string) {
	var req models.TangServerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		diskEncHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	srv, err := h.svc.Update(r.Context(), id, &req)
	if err != nil {
		tangServiceError(w, err, "update tang server")
		return
	}
	diskEncJSON(w, http.StatusOK, srv)
}

func (h *TangServersHandler) delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Delete(r.Context(), id); err != nil {
		tangServiceError(w, err, "delete tang server")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// probe fetches an advertisement so the admin can confirm the thumbprints
// against `tang-show-keys` on the Tang host before trusting them.
func (h *TangServersHandler) probe(w http.ResponseWriter, r *http.Request) {
	var req models.TangProbeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		diskEncHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.svc.Probe(r.Context(), req.URL)
	if err != nil {
		var verr *services.DiskEncryptionValidationError
		if errors.As(err, &verr) {
			diskEncHTTPError(w, http.StatusBadRequest, verr.Msg)
			return
		}
		diskEncHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	diskEncJSON(w, http.StatusOK, resp)
}

func (h *TangServersHandler) check(w http.ResponseWriter, r *http.Request, id string) {
	srv, err := h.svc.Check(r.Context(), id)
	if err != nil {
		tangServiceError(w, err, "check tang server")
		return
	}
	diskEncJSON(w, http.StatusOK, srv)
}
