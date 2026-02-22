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

// MetadataRequestSender can push a metadata refresh request to a named agent.
type MetadataRequestSender interface {
	SendMetadataRefreshRequest(clientID string) bool
}

// NodeHandler handles node API endpoints
type NodeHandler struct {
	nodeSvc    *services.NodeService
	metaSender MetadataRequestSender // may be nil if hub not available
}

// NewNodeHandler creates a new NodeHandler
func NewNodeHandler(nodeSvc *services.NodeService, hub MetadataRequestSender) *NodeHandler {
	return &NodeHandler{nodeSvc: nodeSvc, metaSender: hub}
}

// List handles GET /api/v1/nodes
func (h *NodeHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var nodes []*models.Node
	var err error

	// Check for search query
	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")

	if len(search) > 500 {
		http.Error(w, `{"error":"search term too long"}`, http.StatusBadRequest)
		return
	}

	if search != "" {
		nodes, err = h.nodeSvc.SearchNodes(r.Context(), search)
	} else if status != "" {
		nodes, err = h.nodeSvc.ListNodesByStatus(r.Context(), status)
	} else {
		nodes, err = h.nodeSvc.ListAllNodes(r.Context())
	}

	if err != nil {
		log.Printf("Failed to list nodes: %v", err)
		http.Error(w, `{"error":"failed to list nodes"}`, http.StatusInternalServerError)
		return
	}

	if nodes == nil {
		nodes = []*models.Node{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		log.Printf("Failed to encode nodes response: %v", err)
	}
}

// Get handles GET /api/v1/nodes/{id}
func (h *NodeHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id, _, _ := parseNodePath(r.URL.Path)
	if id == "" {
		http.Error(w, `{"error":"node id required"}`, http.StatusBadRequest)
		return
	}

	node, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(node); err != nil {
		log.Printf("Failed to encode node response: %v", err)
	}
}

// Update handles PUT /api/v1/nodes/{id}
func (h *NodeHandler) Update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id, _, _ := parseNodePath(r.URL.Path)
	if id == "" {
		http.Error(w, `{"error":"node id required"}`, http.StatusBadRequest)
		return
	}

	var req models.UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	node, err := h.nodeSvc.UpdateNode(r.Context(), id, &req)
	if err != nil {
		log.Printf("Failed to update node: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := map[string]string{"error": err.Error()}
		if encErr := json.NewEncoder(w).Encode(errResp); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(node); err != nil {
		log.Printf("Failed to encode node response: %v", err)
	}
}

// CountByStatus handles GET /api/v1/nodes/status-counts
func (h *NodeHandler) CountByStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	counts, err := h.nodeSvc.CountByStatus(r.Context())
	if err != nil {
		log.Printf("Failed to count nodes by status: %v", err)
		http.Error(w, `{"error":"failed to count nodes"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(counts); err != nil {
		log.Printf("Failed to encode counts response: %v", err)
	}
}

// Delete handles DELETE /api/v1/nodes/{id}.
// Deleting a node removes it from the database. Its mTLS certificate is no longer
// trusted at the application level — reconnection will be rejected until the agent
// re-enrolls with a new token.
func (h *NodeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, _, _ := parseNodePath(r.URL.Path)
	if id == "" {
		http.Error(w, `{"error":"node id required"}`, http.StatusBadRequest)
		return
	}

	node, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}

	if err := h.nodeSvc.DeleteNode(r.Context(), id); err != nil {
		log.Printf("Failed to delete node %s: %v", id, err)
		http.Error(w, `{"error":"failed to delete node"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RefreshMetadata handles POST /api/v1/nodes/{id}/refresh-metadata.
// It sends a METADATA_REQUEST event to the named agent's stream, asking it
// to collect fresh system info and report back via the Heartbeat RPC.
func (h *NodeHandler) RefreshMetadata(w http.ResponseWriter, r *http.Request, id string) {
	node, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}

	if h.metaSender == nil {
		http.Error(w, `{"error":"metadata refresh not available"}`, http.StatusServiceUnavailable)
		return
	}

	if !h.metaSender.SendMetadataRefreshRequest(node.Name) {
		http.Error(w, `{"error":"agent not connected"}`, http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// AddToGroup handles POST /api/v1/nodes/{id}/groups — adds node to a group.
func (h *NodeHandler) AddToGroup(w http.ResponseWriter, r *http.Request) {
	id, _, _ := parseNodePath(r.URL.Path)
	if id == "" {
		http.Error(w, `{"error":"node id required"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GroupID == "" {
		http.Error(w, `{"error":"group_id is required"}`, http.StatusBadRequest)
		return
	}
	node, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}
	if err := h.nodeSvc.AddNodeToGroup(r.Context(), id, req.GroupID); err != nil {
		log.Printf("Failed to add node %s to group %s: %v", id, req.GroupID, err)
		http.Error(w, `{"error":"failed to add node to group"}`, http.StatusInternalServerError)
		return
	}
	// Return updated node
	updated, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil || updated == nil {
		http.Error(w, `{"error":"failed to reload node"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// RemoveFromGroup handles DELETE /api/v1/nodes/{id}/groups/{groupId}.
func (h *NodeHandler) RemoveFromGroup(w http.ResponseWriter, r *http.Request) {
	id, _, groupID := parseNodePath(r.URL.Path)
	if id == "" || groupID == "" {
		http.Error(w, `{"error":"node id and group id required"}`, http.StatusBadRequest)
		return
	}
	node, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}
	if err := h.nodeSvc.RemoveNodeFromGroup(r.Context(), id, groupID); err != nil {
		log.Printf("Failed to remove node %s from group %s: %v", id, groupID, err)
		http.Error(w, `{"error":"failed to remove node from group"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeHTTP routes /api/v1/nodes and /api/v1/nodes/{id}[/action]
func (h *NodeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, action, _ := parseNodePath(r.URL.Path)

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			h.List(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	if action == "refresh-metadata" {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		h.RefreshMetadata(w, r, id)
		return
	}

	if action == "groups" {
		switch r.Method {
		case http.MethodPost:
			h.AddToGroup(w, r)
		case http.MethodDelete:
			h.RemoveFromGroup(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.Get(w, r)
	case http.MethodPut:
		h.Update(w, r)
	case http.MethodDelete:
		h.Delete(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// parseNodePath parses a node API URL path, returning the node ID,
// optional action sub-path, and optional sub-action. Examples:
//
//	/api/v1/nodes/abc123                       → ("abc123", "", "")
//	/api/v1/nodes/abc123/refresh-metadata      → ("abc123", "refresh-metadata", "")
//	/api/v1/nodes/abc123/groups                → ("abc123", "groups", "")
//	/api/v1/nodes/abc123/groups/{groupId}      → ("abc123", "groups", groupId)
func parseNodePath(path string) (id, action, subAction string) {
	const prefix = "/api/v1/nodes/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/")
	if rest == "status-counts" {
		return "", "", ""
	}
	parts := strings.SplitN(rest, "/", 3)
	id = parts[0]
	if len(parts) >= 2 {
		action = parts[1]
	}
	if len(parts) >= 3 {
		subAction = parts[2]
	}
	return id, action, subAction
}
