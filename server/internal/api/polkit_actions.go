// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/database"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// PolkitHandler handles polkit-related REST endpoints.
type PolkitHandler struct {
	polkitRepo *database.PolkitRepository
}

// NewPolkitHandler creates a new PolkitHandler.
func NewPolkitHandler(polkitRepo *database.PolkitRepository) *PolkitHandler {
	return &PolkitHandler{polkitRepo: polkitRepo}
}

// polkitActionResponse is the JSON representation of a polkit action.
type polkitActionResponse struct {
	ActionID        string `json:"action_id"`
	Description     string `json:"description,omitempty"`
	Message         string `json:"message,omitempty"`
	Vendor          string `json:"vendor,omitempty"`
	DefaultAny      string `json:"default_any,omitempty"`
	DefaultInactive string `json:"default_inactive,omitempty"`
	DefaultActive   string `json:"default_active,omitempty"`
}

func protoToPolkitActionResponse(a *pb.PolkitActionDescription) polkitActionResponse {
	return polkitActionResponse{
		ActionID:        a.GetActionId(),
		Description:     a.GetDescription(),
		Message:         a.GetMessage(),
		Vendor:          a.GetVendor(),
		DefaultAny:      a.GetDefaultAny(),
		DefaultInactive: a.GetDefaultInactive(),
		DefaultActive:   a.GetDefaultActive(),
	}
}

// ListActions handles GET /api/v1/polkit/actions
// Optional query param: node_id=<uuid> to filter to actions available on a specific node.
func (h *PolkitHandler) ListActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	nodeID := r.URL.Query().Get("node_id")

	var (
		actions []*pb.PolkitActionDescription
		err     error
	)
	if nodeID != "" {
		actions, err = h.polkitRepo.ListActionsByNode(ctx, nodeID)
	} else {
		actions, err = h.polkitRepo.ListActions(ctx)
	}
	if err != nil {
		log.Printf("Failed to list polkit actions: %v", err)
		http.Error(w, `{"error":"failed to list polkit actions"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]polkitActionResponse, 0, len(actions))
	for _, a := range actions {
		resp = append(resp, protoToPolkitActionResponse(a))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode polkit actions response: %v", err)
	}
}

// ServeHTTP routes /api/v1/polkit/actions
func (h *PolkitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ListActions(w, r)
}
