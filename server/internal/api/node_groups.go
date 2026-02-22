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

// NodeGroupHandler handles node group API endpoints
type NodeGroupHandler struct {
	nodeGroupSvc *services.NodeGroupService
	enrollSvc    *services.EnrollmentService
}

// NewNodeGroupHandler creates a new NodeGroupHandler
func NewNodeGroupHandler(nodeGroupSvc *services.NodeGroupService, enrollSvc *services.EnrollmentService) *NodeGroupHandler {
	return &NodeGroupHandler{
		nodeGroupSvc: nodeGroupSvc,
		enrollSvc:    enrollSvc,
	}
}

// List handles GET /api/v1/node-groups
func (h *NodeGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	groups, err := h.nodeGroupSvc.ListNodeGroups(r.Context())
	if err != nil {
		log.Printf("Failed to list node groups: %v", err)
		http.Error(w, `{"error":"failed to list node groups"}`, http.StatusInternalServerError)
		return
	}

	if groups == nil {
		groups = []*models.NodeGroup{}
	}

	// Build response with node counts
	type nodeGroupWithCount struct {
		models.NodeGroup
		NodeCount int `json:"node_count"`
	}

	result := make([]nodeGroupWithCount, 0, len(groups))
	for _, g := range groups {
		count, err := h.nodeGroupSvc.CountNodesByGroupID(r.Context(), g.ID)
		if err != nil {
			log.Printf("Failed to count nodes for group %s: %v", g.ID, err)
			count = 0
		}
		result = append(result, nodeGroupWithCount{
			NodeGroup: *g,
			NodeCount: count,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Failed to encode node groups response: %v", err)
	}
}

// Create handles POST /api/v1/node-groups
func (h *NodeGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req models.CreateNodeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	group, err := h.nodeGroupSvc.CreateNodeGroup(r.Context(), &req)
	if err != nil {
		log.Printf("Failed to create node group: %v", err)
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
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("Failed to encode node group response: %v", err)
	}
}

// ServeHTTP routes /api/v1/node-groups/{id} and sub-paths
func (h *NodeGroupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, subpath := extractNodeGroupIDAndSubpath(r.URL.Path)

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

	// Handle sub-paths like /api/v1/node-groups/{id}/tokens
	if subpath == "tokens" {
		h.GenerateToken(w, r, id)
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

// Get handles GET /api/v1/node-groups/{id}
func (h *NodeGroupHandler) Get(w http.ResponseWriter, r *http.Request, id string) {
	group, err := h.nodeGroupSvc.GetNodeGroup(r.Context(), id)
	if err != nil || group == nil {
		http.Error(w, `{"error":"node group not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("Failed to encode node group response: %v", err)
	}
}

// Update handles PUT /api/v1/node-groups/{id}
func (h *NodeGroupHandler) Update(w http.ResponseWriter, r *http.Request, id string) {
	var req models.UpdateNodeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	group, err := h.nodeGroupSvc.UpdateNodeGroup(r.Context(), id, &req)
	if err != nil {
		log.Printf("Failed to update node group: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("Failed to encode node group response: %v", err)
	}
}

// Delete handles DELETE /api/v1/node-groups/{id}
func (h *NodeGroupHandler) Delete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.nodeGroupSvc.DeleteNodeGroup(r.Context(), id); err != nil {
		log.Printf("Failed to delete node group: %v", err)
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
}

// GenerateToken handles POST /api/v1/node-groups/{id}/tokens
func (h *NodeGroupHandler) GenerateToken(w http.ResponseWriter, r *http.Request, groupID string) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Verify the group exists
	group, err := h.nodeGroupSvc.GetNodeGroup(r.Context(), groupID)
	if err != nil || group == nil {
		http.Error(w, `{"error":"node group not found"}`, http.StatusNotFound)
		return
	}

	token, err := h.enrollSvc.CreateToken(groupID)
	if err != nil {
		log.Printf("Failed to create enrollment token: %v", err)
		http.Error(w, `{"error":"failed to create enrollment token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(token); err != nil {
		log.Printf("Failed to encode token response: %v", err)
	}
}

// extractNodeGroupIDAndSubpath extracts the ID and optional sub-path from
// URL paths like /api/v1/node-groups/{id} or /api/v1/node-groups/{id}/tokens
func extractNodeGroupIDAndSubpath(path string) (string, string) {
	const prefix = "/api/v1/node-groups/"
	if !strings.HasPrefix(path, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/")

	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		return "", ""
	}

	subpath := ""
	if len(parts) > 1 {
		subpath = parts[1]
	}

	return id, subpath
}
