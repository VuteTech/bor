// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// PolicyBindingHandler handles policy binding API endpoints
type PolicyBindingHandler struct {
	bindingSvc *services.PolicyBindingService
	// OnBindingChange is called after any binding mutation (create,
	// update, delete) so that streaming agents can be notified.
	OnBindingChange func()
}

// NewPolicyBindingHandler creates a new PolicyBindingHandler
func NewPolicyBindingHandler(bindingSvc *services.PolicyBindingService) *PolicyBindingHandler {
	return &PolicyBindingHandler{bindingSvc: bindingSvc}
}

// List handles GET /api/v1/policy-bindings
func (h *PolicyBindingHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	bindings, err := h.bindingSvc.ListBindings(r.Context())
	if err != nil {
		log.Printf("Failed to list policy bindings: %v", err)
		http.Error(w, `{"error":"failed to list policy bindings"}`, http.StatusInternalServerError)
		return
	}

	if bindings == nil {
		bindings = []*models.PolicyBindingWithDetails{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(bindings); err != nil {
		log.Printf("Failed to encode policy bindings response: %v", err)
	}
}

// Create handles POST /api/v1/policy-bindings
func (h *PolicyBindingHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.CreatePolicyBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	binding, err := h.bindingSvc.CreateBinding(r.Context(), &req)
	if err != nil {
		log.Printf("Failed to create policy binding: %v", err)
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
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Printf("Failed to encode policy binding response: %v", err)
	}

	if h.OnBindingChange != nil {
		h.OnBindingChange()
	}
}

// ServeHTTP routes /api/v1/policy-bindings and /api/v1/policy-bindings/{id}
func (h *PolicyBindingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := extractBindingIDFromPath(r.URL.Path)

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
		h.Get(w, r, id)
	case http.MethodPut:
		h.Update(w, r, id)
	case http.MethodDelete:
		h.Delete(w, r, id)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// Get handles GET /api/v1/policy-bindings/{id}
func (h *PolicyBindingHandler) Get(w http.ResponseWriter, r *http.Request, id string) {
	binding, err := h.bindingSvc.GetBinding(r.Context(), id)
	if err != nil || binding == nil {
		http.Error(w, `{"error":"policy binding not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Printf("Failed to encode policy binding response: %v", err)
	}
}

// Update handles PUT /api/v1/policy-bindings/{id}
func (h *PolicyBindingHandler) Update(w http.ResponseWriter, r *http.Request, id string) {
	var req models.UpdatePolicyBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	binding, err := h.bindingSvc.UpdateBinding(r.Context(), id, &req)
	if err != nil {
		log.Printf("Failed to update policy binding: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Printf("Failed to encode policy binding response: %v", err)
	}

	if h.OnBindingChange != nil {
		h.OnBindingChange()
	}
}

// Delete handles DELETE /api/v1/policy-bindings/{id}
func (h *PolicyBindingHandler) Delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.bindingSvc.DeleteBinding(r.Context(), id); err != nil {
		log.Printf("Failed to delete policy binding: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)

	if h.OnBindingChange != nil {
		h.OnBindingChange()
	}
}

// extractBindingIDFromPath extracts the ID from URL path like /api/v1/policy-bindings/{id}
func extractBindingIDFromPath(path string) string {
	const prefix = "/api/v1/policy-bindings/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	id = strings.TrimSuffix(id, "/")
	return id
}
