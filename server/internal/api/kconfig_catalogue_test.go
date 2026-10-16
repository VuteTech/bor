// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

type stubKConfigStore struct {
	all    []*pb.KConfigCatalogueEntry
	byNode map[string][]*pb.KConfigCatalogueEntry
	kcms   map[string][]*pb.KcmModule
	themes map[string][]*pb.LookAndFeelPackage
}

func (s *stubKConfigStore) ListLookAndFeel(context.Context) ([]*pb.LookAndFeelPackage, error) {
	var out []*pb.LookAndFeelPackage
	for _, t := range s.themes {
		out = append(out, t...)
	}
	return out, nil
}

func (s *stubKConfigStore) ListLookAndFeelByNode(_ context.Context, nodeID string) ([]*pb.LookAndFeelPackage, error) {
	return s.themes[nodeID], nil
}

func TestKConfigHandler_ListThemes(t *testing.T) {
	h := NewKConfigHandler(&stubKConfigStore{themes: map[string][]*pb.LookAndFeelPackage{
		"n1": {{Id: "org.kde.breezedark.desktop", Name: "Breeze Dark"}},
		"n2": {{Id: "org.fedoraproject.fedora.desktop", Name: "Fedora"}},
	}})

	rec := httptest.NewRecorder()
	h.ListThemes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kconfig/themes", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var all []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected both nodes' themes, got %+v", all)
	}

	rec = httptest.NewRecorder()
	h.ListThemes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kconfig/themes?node_id=n2", http.NoBody))
	var one []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].ID != "org.fedoraproject.fedora.desktop" || one[0].Name != "Fedora" {
		t.Errorf("node filter wrong: %+v", one)
	}

	rec = httptest.NewRecorder()
	h.ListThemes(rec, httptest.NewRequest(http.MethodPost, "/api/v1/kconfig/themes", http.NoBody))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST should be rejected, got %d", rec.Code)
	}
}

func (s *stubKConfigStore) ListEntries(context.Context) ([]*pb.KConfigCatalogueEntry, error) {
	return s.all, nil
}

func (s *stubKConfigStore) ListEntriesByNode(_ context.Context, nodeID string) ([]*pb.KConfigCatalogueEntry, error) {
	return s.byNode[nodeID], nil
}

func (s *stubKConfigStore) ListKCMs(context.Context) ([]*pb.KcmModule, error) {
	var out []*pb.KcmModule
	for _, m := range s.kcms {
		out = append(out, m...)
	}
	return out, nil
}

func (s *stubKConfigStore) ListKCMsByNode(_ context.Context, nodeID string) ([]*pb.KcmModule, error) {
	return s.kcms[nodeID], nil
}

func TestKConfigHandler_ListCatalogue(t *testing.T) {
	builtin := &pb.KConfigCatalogueEntry{Id: "kiosk.shell_access", File: "kdeglobals", GroupPath: []string{"KDE Action Restrictions"}, Key: "shell_access", Type: "Bool", Label: "Restrict shell access", Category: "Session & Shell", Inverted: true, Source: "builtin"}
	nodeEntry := &pb.KConfigCatalogueEntry{Id: "kcfg/dolphinrc/General/ShowFullPath", File: "dolphinrc", GroupPath: []string{"General"}, Key: "ShowFullPath", Type: "Bool", Label: "Show full path", Category: "Node: dolphinrc", Source: "kcfg"}
	store := &stubKConfigStore{
		all:    []*pb.KConfigCatalogueEntry{builtin, nodeEntry},
		byNode: map[string][]*pb.KConfigCatalogueEntry{"n1": {nodeEntry}},
		kcms:   map[string][]*pb.KcmModule{"n1": {{Id: "kcm_access", Name: "Accessibility"}}},
	}
	h := NewKConfigHandler(store)

	// Whole catalogue, camelCase protojson.
	rec := httptest.NewRecorder()
	h.ListCatalogue(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kconfig/catalogue", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var all []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(all) != 2 || all[0]["id"] != "kiosk.shell_access" || all[0]["groupPath"] == nil || all[0]["inverted"] != true {
		t.Errorf("unexpected catalogue response: %v", all)
	}

	// Per node.
	rec = httptest.NewRecorder()
	h.ListCatalogue(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kconfig/catalogue?node_id=n1", http.NoBody))
	var node []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &node); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(node) != 1 || node[0]["source"] != "kcfg" {
		t.Errorf("unexpected node response: %v", node)
	}

	// Modules.
	rec = httptest.NewRecorder()
	h.ListKCMs(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kconfig/kcms?node_id=n1", http.NoBody))
	var mods []kcmModuleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &mods); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(mods) != 1 || mods[0].ID != "kcm_access" || mods[0].Name != "Accessibility" {
		t.Errorf("unexpected modules response: %+v", mods)
	}

	// Method guard.
	rec = httptest.NewRecorder()
	h.ListCatalogue(rec, httptest.NewRequest(http.MethodPost, "/api/v1/kconfig/catalogue", http.NoBody))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
