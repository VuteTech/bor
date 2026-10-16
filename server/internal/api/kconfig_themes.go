// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

type lookAndFeelResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListThemes handles GET /api/v1/kconfig/themes: the global themes (Plasma
// look-and-feel packages) nodes reported, so the editor can offer them by
// name. Optional query param: node_id=<uuid> limits the list to one node;
// without it, every theme any node reported.
func (h *KConfigHandler) ListThemes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	nodeID := r.URL.Query().Get("node_id")

	var (
		themes []*pb.LookAndFeelPackage
		err    error
	)
	if nodeID != "" {
		themes, err = h.store.ListLookAndFeelByNode(ctx, nodeID)
	} else {
		themes, err = h.store.ListLookAndFeel(ctx)
	}
	if err != nil {
		log.Printf("Failed to list global themes: %v", err)
		http.Error(w, `{"error":"failed to list themes"}`, http.StatusInternalServerError)
		return
	}

	out := make([]lookAndFeelResponse, 0, len(themes))
	for _, t := range themes {
		out = append(out, lookAndFeelResponse{ID: t.GetId(), Name: t.GetName()})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("Failed to encode global themes response: %v", err)
	}
}
