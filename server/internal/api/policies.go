// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// PolicyHandler handles policy endpoints
type PolicyHandler struct {
	policySvc *services.PolicyService
	// bindings resolves which node groups a policy is bound to, for
	// node-group-scoped callers. Without it, scoped callers see no policies.
	bindings policyGroupSource
	// OnPolicyChange is called after a mutation that may affect agents:
	// Update, SetState, Deprecate. It receives the policy ID so the
	// caller can scope notifications to the affected node groups.
	// Not called for Create (draft) or Delete (blocked when enabled bindings exist).
	OnPolicyChange func(policyID string)
}

// NewPolicyHandler creates a new PolicyHandler
func NewPolicyHandler(policySvc *services.PolicyService) *PolicyHandler {
	return &PolicyHandler{policySvc: policySvc}
}

// WithBindings attaches the policy-to-node-group lookup that delegated
// (node-group-scoped) administration needs.
func (h *PolicyHandler) WithBindings(bindings policyGroupSource) *PolicyHandler {
	h.bindings = bindings
	return h
}

// scopePolicies filters a policy list down to what the caller may view.
func (h *PolicyHandler) scopePolicies(r *http.Request, policies []*models.Policy) ([]*models.Policy, error) {
	scope := requestScope(r, "policy", "view")
	if scope.IsGlobal() {
		return policies, nil
	}
	idx, err := loadPolicyGroupIndex(r.Context(), h.bindings)
	if err != nil {
		return nil, err
	}
	return filterVisiblePolicies(policies, idx, scope, callerID(r.Context())), nil
}

// loadPolicy fetches a policy the caller may view and, when writeAction is
// set, also change with policy:writeAction. It answers 404 (missing or not
// visible, indistinguishably) or 403 (visible but outside the write scope)
// itself.
func (h *PolicyHandler) loadPolicy(w http.ResponseWriter, r *http.Request, id, writeAction string) (*models.Policy, bool) {
	policy, err := h.policySvc.GetPolicy(r.Context(), id)
	if err != nil || policy == nil {
		http.Error(w, `{"error":"policy not found"}`, http.StatusNotFound)
		return nil, false
	}

	viewScope := requestScope(r, "policy", "view")
	writeScope := authz.GlobalScope()
	if writeAction != "" {
		writeScope = requestScope(r, "policy", writeAction)
	}
	if viewScope.IsGlobal() && writeScope.IsGlobal() {
		return policy, true
	}

	var groupIDs []string
	if h.bindings != nil {
		groupIDs, err = h.bindings.GetGroupIDsForPolicy(r.Context(), id)
		if err != nil {
			log.Printf("Failed to resolve groups of policy %s: %v", id, err) //nolint:gosec // id comes from URL path parameter
			http.Error(w, `{"error":"failed to check policy scope"}`, http.StatusInternalServerError)
			return nil, false
		}
	} else if !viewScope.IsGlobal() {
		http.Error(w, `{"error":"policy not found"}`, http.StatusNotFound)
		return nil, false
	}

	uid := callerID(r.Context())
	if !viewScope.IsGlobal() && !canViewPolicy(viewScope, policy, groupIDs, uid) {
		http.Error(w, `{"error":"policy not found"}`, http.StatusNotFound)
		return nil, false
	}
	if writeAction != "" && !canWritePolicy(writeScope, policy, groupIDs, uid) {
		denyOutOfScope(w, r, "policy", writeAction, id)
		return nil, false
	}
	return policy, true
}

// List handles GET /api/v1/policies
func (h *PolicyHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	policies, err := h.policySvc.ListEnabledPolicies(r.Context())
	if err == nil {
		policies, err = h.scopePolicies(r, policies)
	}
	if err != nil {
		log.Printf("Failed to list policies: %v", err)
		http.Error(w, `{"error":"failed to list policies"}`, http.StatusInternalServerError)
		return
	}

	if policies == nil {
		policies = []*models.Policy{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policies); err != nil {
		log.Printf("Failed to encode policies response: %v", err)
	}
}

// ListAll handles GET /api/v1/policies/all
func (h *PolicyHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	policies, err := h.policySvc.ListAllPolicies(r.Context())
	if err == nil {
		policies, err = h.scopePolicies(r, policies)
	}
	if err != nil {
		log.Printf("Failed to list all policies: %v", err)
		http.Error(w, `{"error":"failed to list policies"}`, http.StatusInternalServerError)
		return
	}

	if policies == nil {
		policies = []*models.Policy{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policies); err != nil {
		log.Printf("Failed to encode policies response: %v", err)
	}
}

// Create handles POST /api/v1/policies/all
func (h *PolicyHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	claims := GetUserFromContext(r.Context())
	createdBy := ""
	if claims != nil {
		createdBy = claims.Username
		// The creator owns the draft: a node-group-scoped author keeps access
		// to it until it is bound to one of their groups.
		req.CreatedByUserID = claims.UserID
	}

	policy, err := h.policySvc.CreatePolicy(r.Context(), &req, createdBy)
	if err != nil {
		log.Printf("Failed to create policy: %v", err)
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
	if err := json.NewEncoder(w).Encode(policy); err != nil {
		log.Printf("Failed to encode policy response: %v", err)
	}

	// New policies start as DRAFT — no agents need to know yet.
}

// Get handles GET /api/v1/policies/{id}
func (h *PolicyHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractPolicyIDFromPath(r.URL.Path)
	if id == "" {
		http.Error(w, `{"error":"policy id required"}`, http.StatusBadRequest)
		return
	}

	policy, ok := h.loadPolicy(w, r, id, "")
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policy); err != nil {
		log.Printf("Failed to encode policy response: %v", err)
	}
}

// Update handles PUT /api/v1/policies/{id}
func (h *PolicyHandler) Update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := extractPolicyIDFromPath(r.URL.Path)
	if id == "" {
		http.Error(w, `{"error":"policy id required"}`, http.StatusBadRequest)
		return
	}

	if _, ok := h.loadPolicy(w, r, id, "edit"); !ok {
		return
	}

	var req models.UpdatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	policy, err := h.policySvc.UpdatePolicy(r.Context(), id, &req)
	if err != nil {
		log.Printf("Failed to update policy: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policy); err != nil {
		log.Printf("Failed to encode policy response: %v", err)
	}

	if h.OnPolicyChange != nil {
		h.OnPolicyChange(policy.ID)
	}
}

// ServeHTTP routes /api/v1/policies/all and /api/v1/policies/all/{id}
func (h *PolicyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, subpath := extractPolicyIDAndSubpath(r.URL.Path)

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			h.ListAll(w, r)
		case http.MethodPost:
			h.Create(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	// Handle sub-paths like /api/v1/policies/all/{id}/state
	if subpath == "state" {
		h.SetState(w, r, id)
		return
	}
	if subpath == "deprecate" {
		h.Deprecate(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.Get(w, r)
	case http.MethodPut:
		h.Update(w, r)
	case http.MethodDelete:
		h.Delete(w, r, id)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// SetState handles PUT /api/v1/policies/all/{id}/state
func (h *PolicyHandler) SetState(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if _, ok := h.loadPolicy(w, r, id, "edit"); !ok {
		return
	}

	var req models.SetPolicyStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	policy, err := h.policySvc.SetPolicyState(r.Context(), id, req.State)
	if err != nil {
		log.Printf("Failed to set policy state: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policy); err != nil {
		log.Printf("Failed to encode policy response: %v", err)
	}

	if h.OnPolicyChange != nil {
		h.OnPolicyChange(policy.ID)
	}
}

// Deprecate handles POST /api/v1/policies/all/{id}/deprecate
func (h *PolicyHandler) Deprecate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if _, ok := h.loadPolicy(w, r, id, "edit"); !ok {
		return
	}

	var req models.DeprecatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	policy, err := h.policySvc.DeprecatePolicy(r.Context(), id, &req)
	if err != nil {
		log.Printf("Failed to deprecate policy: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policy); err != nil {
		log.Printf("Failed to encode policy response: %v", err)
	}

	if h.OnPolicyChange != nil {
		h.OnPolicyChange(policy.ID)
	}
}

// Delete handles DELETE /api/v1/policies/all/{id}
func (h *PolicyHandler) Delete(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if _, ok := h.loadPolicy(w, r, id, "delete"); !ok {
		return
	}

	err := h.policySvc.DeletePolicy(r.Context(), id)
	if err != nil {
		log.Printf("Failed to delete policy: %v", err)
		w.Header().Set("Content-Type", "application/json")
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(errMsg, "enabled binding") {
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		errResp := map[string]string{"error": errMsg}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
	// Deletion is blocked when enabled bindings exist, so no active agents
	// hold this policy and no notification is needed.
}

// extractPolicyIDAndSubpath extracts a policy ID and optional sub-path from URL
func extractPolicyIDAndSubpath(path string) (id, subpath string) {
	const prefix = "/api/v1/policies/all/"
	if !strings.HasPrefix(path, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return "", ""
	}

	parts := strings.SplitN(rest, "/", 2)
	id = parts[0]
	if len(parts) > 1 {
		subpath = parts[1]
	}
	return id, subpath
}

// extractPolicyIDFromPath extracts a policy ID from URL path like /api/v1/policies/all/{id}
func extractPolicyIDFromPath(path string) string {
	id, _ := extractPolicyIDAndSubpath(path)
	return id
}
