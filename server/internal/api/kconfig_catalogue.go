// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// kconfigCatalogueStore is the subset of database.KConfigRepository the
// REST handler needs.
type kconfigCatalogueStore interface {
	ListEntries(ctx context.Context) ([]*pb.KConfigCatalogueEntry, error)
	ListEntriesByNode(ctx context.Context, nodeID string) ([]*pb.KConfigCatalogueEntry, error)
	ListKCMs(ctx context.Context) ([]*pb.KcmModule, error)
	ListKCMsByNode(ctx context.Context, nodeID string) ([]*pb.KcmModule, error)
	ListLookAndFeel(ctx context.Context) ([]*pb.LookAndFeelPackage, error)
	ListLookAndFeelByNode(ctx context.Context, nodeID string) ([]*pb.LookAndFeelPackage, error)
}

// KConfigHandler serves the KDE settings catalogue to the web UI.
type KConfigHandler struct {
	store kconfigCatalogueStore
}

// NewKConfigHandler creates a new KConfigHandler.
func NewKConfigHandler(store kconfigCatalogueStore) *KConfigHandler {
	return &KConfigHandler{store: store}
}

var kconfigEntryJSON = protojson.MarshalOptions{UseProtoNames: false}

// ListCatalogue handles GET /api/v1/kconfig/catalogue.
// Optional query param: node_id=<uuid> returns the entries that node reported.
// Entries are protojson (camelCase), the same shape as the built-in catalogue.
func (h *KConfigHandler) ListCatalogue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	nodeID := r.URL.Query().Get("node_id")

	var (
		entries []*pb.KConfigCatalogueEntry
		err     error
	)
	if nodeID != "" {
		entries, err = h.store.ListEntriesByNode(ctx, nodeID)
	} else {
		entries, err = h.store.ListEntries(ctx)
	}
	if err != nil {
		log.Printf("Failed to list KDE settings catalogue: %v", err)
		http.Error(w, `{"error":"failed to list catalogue"}`, http.StatusInternalServerError)
		return
	}

	out := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		data, err := kconfigEntryJSON.Marshal(e)
		if err != nil {
			log.Printf("Failed to marshal a catalogue entry: %v", err)
			continue
		}
		out = append(out, data)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("Failed to encode KDE settings catalogue response: %v", err)
	}
}

type kcmModuleResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListKCMs handles GET /api/v1/kconfig/kcms.
// Optional query param: node_id=<uuid> returns the modules that node reported;
// without it, every module any node reported.
func (h *KConfigHandler) ListKCMs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	nodeID := r.URL.Query().Get("node_id")

	var (
		mods []*pb.KcmModule
		err  error
	)
	if nodeID != "" {
		mods, err = h.store.ListKCMsByNode(ctx, nodeID)
	} else {
		mods, err = h.store.ListKCMs(ctx)
	}
	if err != nil {
		log.Printf("Failed to list System Settings modules: %v", err)
		http.Error(w, `{"error":"failed to list modules"}`, http.StatusInternalServerError)
		return
	}

	out := make([]kcmModuleResponse, 0, len(mods))
	for _, m := range mods {
		out = append(out, kcmModuleResponse{ID: m.GetId(), Name: m.GetName()})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("Failed to encode System Settings modules response: %v", err)
	}
}
