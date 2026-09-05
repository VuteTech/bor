// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// FlatpakCatalogHandler serves the read-only catalog endpoints used by the
// policy editor under /api/v1/flatpak-catalog/ and the .flatpakrepo probe at
// /api/v1/flatpak-remote-info. All routes are gated by policy:view.
type FlatpakCatalogHandler struct {
	svc *services.FlatpakRepoService
}

// NewFlatpakCatalogHandler creates a new FlatpakCatalogHandler.
func NewFlatpakCatalogHandler(svc *services.FlatpakRepoService) *FlatpakCatalogHandler {
	return &FlatpakCatalogHandler{svc: svc}
}

// ServeHTTP routes /api/v1/flatpak-catalog/{repos|apps|icon}.
func (h *FlatpakCatalogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		flatpakError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/flatpak-catalog"), "/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "repos":
		h.repos(w, r)
	case rest == "apps":
		h.search(w, r)
	case len(parts) == 3 && parts[0] == "apps":
		h.app(w, r, parts[1], parts[2])
	case len(parts) == 3 && parts[0] == "icon":
		h.icon(w, r, parts[1], parts[2])
	default:
		flatpakError(w, http.StatusNotFound, "not found")
	}
}

func (h *FlatpakCatalogHandler) repos(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.CatalogRepos(r.Context())
	if err != nil {
		flatpakServiceError(w, err, "list flatpak catalog repositories")
		return
	}
	flatpakJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (h *FlatpakCatalogHandler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := &models.FlatpakCatalogSearchRequest{
		Search:  strings.TrimSpace(q.Get("search")),
		Repo:    strings.TrimSpace(q.Get("repo")),
		Arch:    strings.TrimSpace(q.Get("arch")),
		Page:    atoiDefault(q.Get("page"), 1),
		PerPage: atoiDefault(q.Get("per_page"), 25),
	}
	if kinds := strings.TrimSpace(q.Get("kind")); kinds != "" {
		for _, k := range strings.Split(kinds, ",") {
			if k = strings.TrimSpace(k); k != "" {
				req.Kinds = append(req.Kinds, k)
			}
		}
	}
	if v := q.Get("verified"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			flatpakError(w, http.StatusBadRequest, "verified must be true or false")
			return
		}
		req.Verified = &b
	}
	resp, err := h.svc.SearchApps(r.Context(), req)
	if err != nil {
		flatpakServiceError(w, err, "search flatpak catalog")
		return
	}
	flatpakJSON(w, http.StatusOK, resp)
}

func (h *FlatpakCatalogHandler) app(w http.ResponseWriter, r *http.Request, repoName, appID string) {
	detail, err := h.svc.GetApp(r.Context(), repoName, appID)
	if err != nil {
		flatpakServiceError(w, err, "get flatpak catalog app")
		return
	}
	flatpakJSON(w, http.StatusOK, detail)
}

func (h *FlatpakCatalogHandler) icon(w http.ResponseWriter, r *http.Request, repoName, appID string) {
	mime, data, err := h.svc.Icon(r.Context(), repoName, appID)
	if err != nil {
		flatpakServiceError(w, err, "get flatpak catalog icon")
		return
	}
	// The payload was fetched from the repository host, sniffed as PNG by
	// magic bytes (SVG is refused) and is served with nosniff plus a CSP that
	// forbids any active content, so it cannot execute in the UI origin.
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) //nolint:gosec // G705: binary PNG validated by magic bytes, nosniff + default-src 'none' CSP, never HTML
}

// FlatpakRemoteInfoHandler implements POST /api/v1/flatpak-remote-info: it
// fetches a .flatpakrepo file server-side (no browser CORS, allowlisted
// redirects, 64 KiB cap) and returns its parsed fields.
type FlatpakRemoteInfoHandler struct {
	svc *services.FlatpakRepoService
}

// NewFlatpakRemoteInfoHandler creates a new FlatpakRemoteInfoHandler.
func NewFlatpakRemoteInfoHandler(svc *services.FlatpakRepoService) *FlatpakRemoteInfoHandler {
	return &FlatpakRemoteInfoHandler{svc: svc}
}

func (h *FlatpakRemoteInfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		flatpakError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		FlatpakrepoURL string `json:"flatpakrepo_url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil || strings.TrimSpace(body.FlatpakrepoURL) == "" {
		flatpakError(w, http.StatusBadRequest, "flatpakrepo_url is required")
		return
	}
	info, err := h.svc.Probe(r.Context(), body.FlatpakrepoURL)
	if err != nil {
		flatpakServiceError(w, err, "fetch .flatpakrepo file")
		return
	}
	flatpakJSON(w, http.StatusOK, info)
}
