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

	"github.com/VuteTech/Bor/server/internal/flatpakcatalog"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// flatpakUploadMaxBytes caps a manually uploaded appstream.xml.gz.
const flatpakUploadMaxBytes = 64 << 20

// FlatpakReposHandler serves /api/v1/flatpak-repos (Settings → Flatpak
// repositories): CRUD, manual refresh and catalog upload.
type FlatpakReposHandler struct {
	svc *services.FlatpakRepoService
}

// NewFlatpakReposHandler creates a new FlatpakReposHandler.
func NewFlatpakReposHandler(svc *services.FlatpakRepoService) *FlatpakReposHandler {
	return &FlatpakReposHandler{svc: svc}
}

func flatpakJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("flatpak: encode response: %v", err)
	}
}

func flatpakError(w http.ResponseWriter, status int, msg string) {
	flatpakJSON(w, status, map[string]string{"error": msg})
}

// flatpakServiceError maps service errors to HTTP responses.
func flatpakServiceError(w http.ResponseWriter, err error, context string) {
	var verr *services.FlatpakRepoValidationError
	switch {
	case errors.As(err, &verr):
		flatpakError(w, http.StatusBadRequest, verr.Msg)
	case errors.Is(err, services.ErrFlatpakRepoNotFound), errors.Is(err, services.ErrFlatpakAppNotFound), errors.Is(err, services.ErrFlatpakIconNotFound):
		flatpakError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrFlatpakRepoConflict), errors.Is(err, services.ErrFlatpakRepoBuiltin),
		errors.Is(err, flatpakcatalog.ErrRefreshDisabled), errors.Is(err, flatpakcatalog.ErrCatalogDisabled):
		flatpakError(w, http.StatusConflict, err.Error())
	case errors.Is(err, flatpakcatalog.ErrNoAppstreamURL), errors.Is(err, flatpakcatalog.ErrNotFlatpakrepo),
		errors.Is(err, flatpakcatalog.ErrTooLarge), errors.Is(err, flatpakcatalog.ErrNotFound):
		flatpakError(w, http.StatusBadGateway, err.Error())
	default:
		log.Printf("flatpak: %s: %v", context, err)
		flatpakError(w, http.StatusInternalServerError, "failed to "+context)
	}
}

// ServeHTTP routes /api/v1/flatpak-repos and /api/v1/flatpak-repos/{id}[/refresh|/catalog-upload].
func (h *FlatpakReposHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/flatpak-repos"), "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			flatpakError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
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
			flatpakError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "refresh":
			h.refresh(w, r, id)
			return
		case "catalog-upload":
			h.upload(w, r, id)
			return
		}
	}
	flatpakError(w, http.StatusNotFound, "not found")
}

func (h *FlatpakReposHandler) list(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.List(r.Context())
	if err != nil {
		flatpakServiceError(w, err, "list flatpak repositories")
		return
	}
	flatpakJSON(w, http.StatusOK, resp)
}

func (h *FlatpakReposHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	repo, err := h.svc.Get(r.Context(), id)
	if err != nil {
		flatpakServiceError(w, err, "get flatpak repository")
		return
	}
	flatpakJSON(w, http.StatusOK, repo)
}

func (h *FlatpakReposHandler) create(w http.ResponseWriter, r *http.Request) {
	var req models.FlatpakRepositoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		flatpakError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	createdBy := ""
	if claims := GetUserFromContext(r.Context()); claims != nil {
		createdBy = claims.Username
	}
	repo, err := h.svc.Create(r.Context(), &req, createdBy)
	if err != nil {
		flatpakServiceError(w, err, "create flatpak repository")
		return
	}
	flatpakJSON(w, http.StatusCreated, repo)
}

func (h *FlatpakReposHandler) update(w http.ResponseWriter, r *http.Request, id string) {
	var req models.FlatpakRepositoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		flatpakError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	repo, err := h.svc.Update(r.Context(), id, &req)
	if err != nil {
		flatpakServiceError(w, err, "update flatpak repository")
		return
	}
	flatpakJSON(w, http.StatusOK, repo)
}

func (h *FlatpakReposHandler) delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Delete(r.Context(), id); err != nil {
		flatpakServiceError(w, err, "delete flatpak repository")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FlatpakReposHandler) refresh(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Refresh(r.Context(), id); err != nil {
		flatpakServiceError(w, err, "refresh flatpak catalog")
		return
	}
	flatpakJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (h *FlatpakReposHandler) upload(w http.ResponseWriter, r *http.Request, id string) {
	r.Body = http.MaxBytesReader(w, r.Body, flatpakUploadMaxBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		flatpakError(w, http.StatusBadRequest, "invalid multipart form or file too large")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		flatpakError(w, http.StatusBadRequest, "missing 'file' field in form data")
		return
	}
	defer func() { _ = f.Close() }()
	n, err := h.svc.Upload(r.Context(), id, r.FormValue("arch"), f)
	if err != nil {
		flatpakServiceError(w, err, "import flatpak catalog")
		return
	}
	flatpakJSON(w, http.StatusOK, map[string]int{"app_count": n})
}
