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
	// policySvc lets a node-group-scoped caller's new binding be checked
	// against policy visibility. Without it, scoped callers cannot create
	// bindings.
	policySvc *services.PolicyService
	// OnBindingChange is called after a binding mutation that may affect
	// agents: Update and Delete. It receives the affected binding so the
	// caller can scope notifications to the right node group.
	// Not called for Create (new bindings start disabled).
	OnBindingChange func(b *models.PolicyBinding)
}

// NewPolicyBindingHandler creates a new PolicyBindingHandler
func NewPolicyBindingHandler(bindingSvc *services.PolicyBindingService) *PolicyBindingHandler {
	return &PolicyBindingHandler{bindingSvc: bindingSvc}
}

// WithPolicies attaches the policy lookup used to check that a scoped caller
// can see the policy they are binding.
func (h *PolicyBindingHandler) WithPolicies(policySvc *services.PolicyService) *PolicyBindingHandler {
	h.policySvc = policySvc
	return h
}

// loadBinding fetches a binding whose group is visible to the caller (404
// otherwise) and, when action is set, whose group is in scope for
// binding:action (403 otherwise).
func (h *PolicyBindingHandler) loadBinding(w http.ResponseWriter, r *http.Request, id, action string) (*models.PolicyBinding, bool) {
	binding, err := h.bindingSvc.GetBinding(r.Context(), id)
	if err != nil || binding == nil || !requestScope(r, "binding", "view").Allows(binding.GroupID) {
		http.Error(w, `{"error":"policy binding not found"}`, http.StatusNotFound)
		return nil, false
	}
	if action != "" && !requestScope(r, "binding", action).Allows(binding.GroupID) {
		denyOutOfScope(w, r, "binding", action, id)
		return nil, false
	}
	return binding, true
}

// callerCanBindPolicy reports whether the caller may see policyID, which a
// node-group-scoped caller needs before binding it to one of their groups.
func (h *PolicyBindingHandler) callerCanBindPolicy(r *http.Request, policyID string) (bool, error) {
	scope := requestScope(r, "policy", "view")
	if scope.IsGlobal() {
		return true, nil
	}
	if h.policySvc == nil {
		return false, nil
	}
	policy, err := h.policySvc.GetPolicy(r.Context(), policyID)
	if err != nil || policy == nil {
		return false, err
	}
	groupIDs, err := h.bindingSvc.GetGroupIDsForPolicy(r.Context(), policyID)
	if err != nil {
		return false, err
	}
	return canViewPolicy(scope, policy, groupIDs, callerID(r.Context())), nil
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

	viewScope := requestScope(r, "binding", "view")
	visible := make([]*models.PolicyBindingWithDetails, 0, len(bindings))
	for _, b := range bindings {
		if viewScope.Allows(b.GroupID) {
			visible = append(visible, b)
		}
	}
	bindings = visible

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

	if !requestScope(r, "binding", "create").Allows(req.GroupID) {
		denyOutOfScope(w, r, "binding", "create", req.GroupID)
		return
	}
	canBind, err := h.callerCanBindPolicy(r, req.PolicyID)
	if err != nil {
		log.Printf("Failed to check policy visibility for binding: %v", err)
		http.Error(w, `{"error":"failed to check policy scope"}`, http.StatusInternalServerError)
		return
	}
	if !canBind {
		http.Error(w, `{"error":"policy not found"}`, http.StatusNotFound)
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
	// New bindings start as DISABLED — no agents need to know yet.
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
	binding, ok := h.loadBinding(w, r, id, "")
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Printf("Failed to encode policy binding response: %v", err)
	}
}

// Update handles PUT /api/v1/policy-bindings/{id}
func (h *PolicyBindingHandler) Update(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := h.loadBinding(w, r, id, "toggle"); !ok {
		return
	}

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

	// Notify agents when state or priority changes — both affect which policy
	// values are applied on the node.
	if h.OnBindingChange != nil && (req.State != nil || req.Priority != nil) {
		h.OnBindingChange(binding)
	}
}

// Delete handles DELETE /api/v1/policy-bindings/{id}
func (h *PolicyBindingHandler) Delete(w http.ResponseWriter, r *http.Request, id string) {
	// Read before deletion so we can notify if the binding was enabled.
	binding, ok := h.loadBinding(w, r, id, "toggle")
	if !ok {
		return
	}

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

	if h.OnBindingChange != nil && binding != nil && binding.State == models.BindingStateEnabled {
		h.OnBindingChange(binding)
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
