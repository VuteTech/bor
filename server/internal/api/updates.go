// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// updatesStore is the subset of database.UpdatesRepository the handler uses.
type updatesStore interface {
	GetNodeStatus(ctx context.Context, nodeID string) (*database.UpdatesNodeStatus, error)
	ListPending(ctx context.Context, nodeID, q string, securityOnly bool, limit int) ([]database.UpdatesPendingRow, error)
}

const maxUpdatesQueryLength = 128

// UpdatesNodeHandler serves the Software updates state of one node.
type UpdatesNodeHandler struct {
	nodes nodeLookup
	store updatesStore
}

// NewUpdatesNodeHandler creates an UpdatesNodeHandler.
func NewUpdatesNodeHandler(nodes nodeLookup, store updatesStore) *UpdatesNodeHandler {
	return &UpdatesNodeHandler{nodes: nodes, store: store}
}

func (h *UpdatesNodeHandler) loadNode(w http.ResponseWriter, r *http.Request, id string) (*models.Node, bool) {
	if id == "" || h.nodes == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return nil, false
	}
	node, err := h.nodes.GetNode(r.Context(), id)
	if err != nil || node == nil || !requestScope(r, "node", "view").AllowsAny(node.NodeGroupIDs) {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return nil, false
	}
	return node, true
}

// updatesNodeResponse is GET /api/v1/nodes/{id}/updates.
type updatesNodeResponse struct {
	ReportedAt *time.Time      `json:"reportedAt,omitempty"`
	Inventory  json.RawMessage `json:"inventory,omitempty"`
}

// Get handles GET /api/v1/nodes/{id}/updates.
func (h *UpdatesNodeHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	node, ok := h.loadNode(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := h.store.GetNodeStatus(r.Context(), node.ID)
	if err != nil {
		log.Printf("Failed to load updates status of node %s: %v", node.ID, err)
		http.Error(w, `{"error":"failed to load status"}`, http.StatusInternalServerError)
		return
	}
	resp := updatesNodeResponse{}
	if st != nil {
		t := st.ReportedAt
		resp.ReportedAt = &t
		resp.Inventory = st.InfoJSON
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode updates node response: %v", err)
	}
}

// ListPending handles GET /api/v1/nodes/{id}/updates/pending?q=&security=1&limit=.
func (h *UpdatesNodeHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	node, ok := h.loadNode(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(q) > maxUpdatesQueryLength {
		q = q[:maxUpdatesQueryLength]
	}
	securityOnly := query.Get("security") == "1" || query.Get("security") == "true"
	limit, _ := strconv.Atoi(query.Get("limit"))
	rows, err := h.store.ListPending(r.Context(), node.ID, q, securityOnly, limit)
	if err != nil {
		log.Printf("Failed to list pending updates of node %s: %v", node.ID, err)
		http.Error(w, `{"error":"failed to list pending updates"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		log.Printf("Failed to encode pending updates: %v", err)
	}
}
