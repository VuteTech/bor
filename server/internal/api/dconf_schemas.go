// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// DConfHandler handles dconf-related REST endpoints.
type DConfHandler struct {
	dconfRepo *database.DConfRepository
}

// NewDConfHandler creates a new DConfHandler.
func NewDConfHandler(dconfRepo *database.DConfRepository) *DConfHandler {
	return &DConfHandler{dconfRepo: dconfRepo}
}

// dconfSchemaResponse is the JSON representation of a GSettings schema.
type dconfSchemaResponse struct {
	SchemaID    string             `json:"schema_id"`
	Path        string             `json:"path"`
	Relocatable bool               `json:"relocatable"`
	Source      string             `json:"source"`
	Keys        []dconfKeyResponse `json:"keys"`
}

// dconfKeyResponse is the JSON representation of a single GSettings key.
type dconfKeyResponse struct {
	Name         string                   `json:"name"`
	Type         string                   `json:"type"`
	Summary      string                   `json:"summary,omitempty"`
	Description  string                   `json:"description,omitempty"`
	DefaultValue string                   `json:"default_value,omitempty"`
	EnumValues   []dconfEnumValueResponse `json:"enum_values,omitempty"`
	RangeMin     string                   `json:"range_min,omitempty"`
	RangeMax     string                   `json:"range_max,omitempty"`
	Choices      []string                 `json:"choices,omitempty"`
}

// dconfEnumValueResponse is the JSON representation of one enum nick/value pair.
type dconfEnumValueResponse struct {
	Nick  string `json:"nick"`
	Value int32  `json:"value"`
}

// protoToSchemaResponse converts a protobuf GSettingsSchema to the REST response shape.
// The source field is not stored in the proto type so it defaults to empty string here;
// callers that have the DB source available should set it separately.
func protoToSchemaResponse(s *pb.GSettingsSchema) dconfSchemaResponse {
	keys := make([]dconfKeyResponse, 0, len(s.GetKeys()))
	for _, k := range s.GetKeys() {
		var enumVals []dconfEnumValueResponse
		for _, ev := range k.GetEnumValues() {
			enumVals = append(enumVals, dconfEnumValueResponse{
				Nick:  ev.GetNick(),
				Value: ev.GetValue(),
			})
		}
		keys = append(keys, dconfKeyResponse{
			Name:         k.GetName(),
			Type:         k.GetType(),
			Summary:      k.GetSummary(),
			Description:  k.GetDescription(),
			DefaultValue: k.GetDefaultValue(),
			EnumValues:   enumVals,
			RangeMin:     k.GetRangeMin(),
			RangeMax:     k.GetRangeMax(),
			Choices:      k.GetChoices(),
		})
	}
	return dconfSchemaResponse{
		SchemaID:    s.GetSchemaId(),
		Path:        s.GetPath(),
		Relocatable: s.GetRelocatable(),
		Keys:        keys,
	}
}

// ListSchemas handles GET /api/v1/dconf/schemas
// Optional query param: node_id=<uuid> to filter by schemas available on a node.
func (h *DConfHandler) ListSchemas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	nodeID := r.URL.Query().Get("node_id")

	var (
		schemas []*pb.GSettingsSchema
		err     error
	)
	if nodeID != "" {
		schemas, err = h.dconfRepo.ListSchemasByNode(ctx, nodeID)
	} else {
		schemas, err = h.dconfRepo.ListSchemas(ctx)
	}
	if err != nil {
		log.Printf("Failed to list dconf schemas: %v", err)
		http.Error(w, `{"error":"failed to list schemas"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]dconfSchemaResponse, 0, len(schemas))
	for _, s := range schemas {
		resp = append(resp, protoToSchemaResponse(s))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode dconf schemas response: %v", err)
	}
}

// ServeHTTP routes /api/v1/dconf/...
func (h *DConfHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/dconf/schemas") || strings.Contains(path, "/dconf/schemas?") {
		h.ListSchemas(w, r)
		return
	}
	http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
}

// ComplianceHandler handles compliance-related REST endpoints.
type ComplianceHandler struct {
	dconfRepo *database.DConfRepository
}

// NewComplianceHandler creates a new ComplianceHandler.
func NewComplianceHandler(dconfRepo *database.DConfRepository) *ComplianceHandler {
	return &ComplianceHandler{dconfRepo: dconfRepo}
}

// complianceListResponse is the paginated compliance list envelope.
type complianceListResponse struct {
	Items        []*database.ComplianceRow `json:"items"`
	Total        int                       `json:"total"`
	Page         int                       `json:"page"`
	PerPage      int                       `json:"per_page"`
	TotalPages   int                       `json:"total_pages"`
	StatusCounts map[string]int            `json:"status_counts"`
}

// List handles GET /api/v1/compliance
func (h *ComplianceHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	req := &database.ComplianceListParams{
		Search:    q.Get("search"),
		Status:    q.Get("status"),
		SortField: q.Get("sort_field"),
		SortOrder: q.Get("sort_order"),
		Page:      atoiDefault(q.Get("page"), 1),
		PerPage:   atoiDefault(q.Get("per_page"), 25),
	}

	results, err := h.dconfRepo.ListComplianceResultsPaged(r.Context(), req)
	if err != nil {
		log.Printf("Failed to list compliance results: %v", err)
		http.Error(w, `{"error":"failed to list compliance results"}`, http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []*database.ComplianceRow{}
	}

	total, err := h.dconfRepo.CountComplianceFiltered(r.Context(), req)
	if err != nil {
		log.Printf("Failed to count compliance results: %v", err)
		http.Error(w, `{"error":"failed to count compliance results"}`, http.StatusInternalServerError)
		return
	}

	statusCounts, err := h.dconfRepo.CountComplianceByStatusFiltered(r.Context(), req.Search)
	if err != nil {
		log.Printf("Failed to count compliance by status: %v", err)
		http.Error(w, `{"error":"failed to count compliance results"}`, http.StatusInternalServerError)
		return
	}

	page, perPage := models.ClampPagination(req.Page, req.PerPage)
	totalPages := 0
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(complianceListResponse{
		Items:        results,
		Total:        total,
		Page:         page,
		PerPage:      perPage,
		TotalPages:   totalPages,
		StatusCounts: statusCounts,
	}); err != nil {
		log.Printf("Failed to encode compliance response: %v", err)
	}
}
