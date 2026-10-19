// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/services"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
)

// systemdCatalogueStore is the subset of database.SystemdRepository the
// REST handler needs.
type systemdCatalogueStore interface {
	ListUnits(ctx context.Context, q, typ string) ([]database.SystemdCatalogueUnit, error)
	ListUnitsByNode(ctx context.Context, nodeID, q, typ string) ([]database.SystemdCatalogueUnit, error)
}

// SystemdHandler serves the systemd unit catalogue to the web UI.
type SystemdHandler struct {
	store systemdCatalogueStore
}

// NewSystemdHandler creates a new SystemdHandler.
func NewSystemdHandler(store systemdCatalogueStore) *SystemdHandler {
	return &SystemdHandler{store: store}
}

// systemdUnitResponse is one catalogue unit as the editor consumes it.
type systemdUnitResponse struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Type          string   `json:"type"`
	UnitFileState string   `json:"unitFileState"`
	Preset        string   `json:"preset"`
	ActiveState   string   `json:"activeState"`
	SubState      string   `json:"subState"`
	HasInstall    bool     `json:"hasInstall"`
	OnCalendar    []string `json:"onCalendar"`
	Triggers      string   `json:"triggers"`
	FragmentPath  string   `json:"fragmentPath"`
	ServiceType   string   `json:"serviceType"`
	// Services: the exposure level and predicate of systemd-analyze
	// security ("9.6", "UNSAFE"); empty when the node did not report them.
	SecurityExposure  string    `json:"securityExposure"`
	SecurityPredicate string    `json:"securityPredicate"`
	Risky             bool      `json:"risky"`
	NodeCount         int       `json:"nodeCount"`
	ReportedAt        time.Time `json:"reportedAt"`
}

// systemdUnitsResponse converts catalogue rows to the JSON the UI consumes.
func systemdUnitsResponse(units []database.SystemdCatalogueUnit) []systemdUnitResponse {
	out := make([]systemdUnitResponse, 0, len(units))
	for _, u := range units {
		info := u.Info
		onCalendar := info.GetOnCalendar()
		if onCalendar == nil {
			onCalendar = []string{}
		}
		out = append(out, systemdUnitResponse{
			Name:              info.GetName(),
			Description:       info.GetDescription(),
			Type:              info.GetType(),
			UnitFileState:     info.GetUnitFileState(),
			Preset:            info.GetPreset(),
			ActiveState:       info.GetActiveState(),
			SubState:          info.GetSubState(),
			HasInstall:        info.GetHasInstall(),
			OnCalendar:        onCalendar,
			Triggers:          info.GetTriggers(),
			FragmentPath:      info.GetFragmentPath(),
			ServiceType:       info.GetServiceType(),
			SecurityExposure:  info.GetSecurityExposure(),
			SecurityPredicate: info.GetSecurityPredicate(),
			Risky:             systemdunits.IsRisky(info.GetName()),
			NodeCount:         u.NodeCount,
			ReportedAt:        u.ReportedAt,
		})
	}
	return out
}

const maxSystemdQueryLength = 128

// ListUnits handles GET /api/v1/systemd/units.
// Query params: q (substring of the name or description), type (service |
// timer | socket | target | path), node_id (one node's units instead of the
// union over every node).
func (h *SystemdHandler) ListUnits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(q) > maxSystemdQueryLength {
		q = q[:maxSystemdQueryLength]
	}
	typ := query.Get("type")
	if typ != "" && systemdunits.Type("x."+typ) == "" {
		http.Error(w, `{"error":"type must be one of service, timer, socket, target, path"}`, http.StatusBadRequest)
		return
	}
	nodeID := query.Get("node_id")

	var (
		units []database.SystemdCatalogueUnit
		err   error
	)
	if nodeID != "" {
		units, err = h.store.ListUnitsByNode(ctx, nodeID, q, typ)
	} else {
		units, err = h.store.ListUnits(ctx, q, typ)
	}
	if err != nil {
		log.Printf("Failed to list systemd unit catalogue: %v", err)
		http.Error(w, `{"error":"failed to list units"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(systemdUnitsResponse(units)); err != nil {
		log.Printf("Failed to encode systemd unit catalogue response: %v", err)
	}
}

// calendarPreviewRequest is the body of POST /api/v1/systemd/calendar-preview.
type calendarPreviewRequest struct {
	Expression string `json:"expression"`
	// Iterations is how many elapse times to list (default 5, at most 10).
	Iterations int `json:"iterations"`
}

// CalendarPreview handles POST /api/v1/systemd/calendar-preview: it
// evaluates a calendar expression with the server's systemd-analyze and
// answers the normalized form and the next elapse times, or
// {"available": false} on a server without systemd-analyze. A malformed
// expression answers 400 with the parser's message.
func (h *SystemdHandler) CalendarPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req calendarPreviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	iterations := req.Iterations
	if iterations <= 0 {
		iterations = 5
	}
	res, err := services.PreviewSystemdCalendar(strings.TrimSpace(req.Expression), iterations)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.Printf("Failed to encode calendar preview response: %v", err)
	}
}
