// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	"github.com/VuteTech/Bor/server/pkg/report"
)

// reportService is the subset of services.ReportService the handler uses.
type reportService interface {
	Generate(ctx context.Context, req *services.GenerateReportRequest, actor services.ReportActor) (*services.ReportSummary, error)
	List(ctx context.Context, p *services.ListReportsParams) ([]*services.ReportSummary, int, error)
	Record(ctx context.Context, id string) (*database.ReportRecord, error)
	Get(ctx context.Context, rec *database.ReportRecord, actor services.ReportActor) (*services.ReportSummary, error)
	Nodes(ctx context.Context, reportID string, p *database.ReportNodeParams) ([]database.ReportNodeRow, int, error)
	Results(ctx context.Context, reportID string, p *database.ReportResultParams) ([]database.ReportResultRow, int, error)
	ResultItems(ctx context.Context, rec *database.ReportRecord, rows []database.ReportResultRow) (map[[2]string][]report.Item, error)
	Policies(ctx context.Context, reportID string) ([]database.ReportPolicyRow, error)
	Download(ctx context.Context, rec *database.ReportRecord, format, part string, actor services.ReportActor) (*services.ReportDownload, error)
	Delete(ctx context.Context, rec *database.ReportRecord, actor services.ReportActor) error
}

// ReportsHandler serves compliance reports.
type ReportsHandler struct {
	svc          reportService
	anonymizeIPs bool
	timeout      time.Duration
}

// NewReportsHandler creates a ReportsHandler. timeout bounds one
// generation.
func NewReportsHandler(svc reportService, anonymizeIPs bool, timeout time.Duration) *ReportsHandler {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &ReportsHandler{svc: svc, anonymizeIPs: anonymizeIPs, timeout: timeout}
}

func (h *ReportsHandler) actor(r *http.Request) services.ReportActor {
	a := services.ReportActor{IP: extractAuditIP(r, h.anonymizeIPs)}
	if claims := GetUserFromContext(r.Context()); claims != nil {
		a.UserID, a.Username = claims.UserID, claims.Username
	}
	return a
}

// createReportRequest is the body of POST /api/v1/compliance/reports.
type createReportRequest struct {
	Scope struct {
		AllNodes     bool     `json:"all_nodes"`
		NodeGroupIDs []string `json:"node_group_ids"`
		PolicyTypes  []string `json:"policy_types"`
	} `json:"scope"`
	Label        string `json:"label"`
	Notes        string `json:"notes"`
	IncludeItems *bool  `json:"include_items"`
}

// Create handles POST /api/v1/compliance/reports: generates a report for
// the requested scope. A delegated administrator's scope is forced inside
// their node groups.
func (h *ReportsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body createReportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := &services.GenerateReportRequest{
		AllNodes:     body.Scope.AllNodes,
		GroupIDs:     body.Scope.NodeGroupIDs,
		PolicyTypes:  body.Scope.PolicyTypes,
		Label:        body.Label,
		Notes:        body.Notes,
		IncludeItems: body.IncludeItems == nil || *body.IncludeItems,
	}
	scope := requestScope(r, "report", "create")
	if !scope.IsGlobal() {
		if scope.IsEmpty() {
			denyRequest(w, r, "report", "create", "", "permission not held")
			return
		}
		filter := scope.Filter()
		if req.AllNodes || len(req.GroupIDs) == 0 {
			// A delegated administrator's "all nodes" is all of their groups.
			req.AllNodes = false
			req.GroupIDs = append([]string{}, filter.GroupIDs...)
		} else if !scope.AllowsAll(req.GroupIDs) {
			denyOutOfScope(w, r, "report", "create", "")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	summary, err := h.svc.Generate(ctx, req, h.actor(r))
	if err != nil {
		h.serviceError(w, err, "generate report")
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

// listReportsResponse is GET /api/v1/compliance/reports.
type listReportsResponse struct {
	Items      []*services.ReportSummary `json:"items"`
	Total      int                       `json:"total"`
	Page       int                       `json:"page"`
	PerPage    int                       `json:"per_page"`
	TotalPages int                       `json:"total_pages"`
}

// List handles GET /api/v1/compliance/reports.
func (h *ReportsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := &services.ListReportsParams{
		Scope:   requestScope(r, "report", "view").Filter(),
		Trigger: q.Get("trigger"),
		Kind:    q.Get("kind"),
		GroupID: q.Get("group"),
		Search:  q.Get("q"),
		Page:    atoiDefault(q.Get("page"), 1),
		PerPage: atoiDefault(q.Get("per_page"), 25),
	}
	switch p.Kind {
	case "":
		p.Kind = "full"
	case "all":
		p.Kind = ""
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid from")
			return
		}
		p.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid to")
			return
		}
		p.To = &t
	}
	items, total, err := h.svc.List(r.Context(), p)
	if err != nil {
		h.serviceError(w, err, "list reports")
		return
	}
	if items == nil {
		items = []*services.ReportSummary{}
	}
	page, perPage := models.ClampPagination(p.Page, p.PerPage)
	writeJSON(w, http.StatusOK, listReportsResponse{Items: items, Total: total, Page: page, PerPage: perPage, TotalPages: (total + perPage - 1) / perPage})
}

// load reads the report of the path and checks the caller's scope.
func (h *ReportsHandler) load(w http.ResponseWriter, r *http.Request, action string) (*database.ReportRecord, bool) {
	rec, err := h.svc.Record(r.Context(), r.PathValue("id"))
	if err != nil {
		h.serviceError(w, err, "load report")
		return nil, false
	}
	if !services.ScopeAllows(requestScope(r, "report", action).Filter(), rec) {
		writeJSONError(w, http.StatusNotFound, "report not found")
		return nil, false
	}
	return rec, true
}

// Get handles GET /api/v1/compliance/reports/{id}.
func (h *ReportsHandler) Get(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.load(w, r, "view")
	if !ok {
		return
	}
	summary, err := h.svc.Get(r.Context(), rec, h.actor(r))
	if err != nil {
		h.serviceError(w, err, "open report")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

type pagedResponse struct {
	Items      interface{} `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PerPage    int         `json:"per_page"`
	TotalPages int         `json:"total_pages"`
}

func paged(items interface{}, total, page, perPage int) pagedResponse {
	page, perPage = models.ClampPagination(page, perPage)
	return pagedResponse{Items: items, Total: total, Page: page, PerPage: perPage, TotalPages: (total + perPage - 1) / perPage}
}

// Nodes handles GET /api/v1/compliance/reports/{id}/nodes.
func (h *ReportsHandler) Nodes(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.load(w, r, "view")
	if !ok {
		return
	}
	q := r.URL.Query()
	p := &database.ReportNodeParams{Status: q.Get("status"), Search: q.Get("q"), Page: atoiDefault(q.Get("page"), 1), PerPage: atoiDefault(q.Get("per_page"), 25)}
	if v := q.Get("unreachable"); v != "" {
		b := v == "1" || v == "true"
		p.Unreachable = &b
	}
	rows, total, err := h.svc.Nodes(r.Context(), rec.ID, p)
	if err != nil {
		h.serviceError(w, err, "list report nodes")
		return
	}
	writeJSON(w, http.StatusOK, paged(rows, total, p.Page, p.PerPage))
}

// reportResultResponse is a result row with its items when requested.
type reportResultResponse struct {
	database.ReportResultRow
	ItemList []report.Item `json:"item_list,omitempty"`
}

// Results handles GET /api/v1/compliance/reports/{id}/results.
func (h *ReportsHandler) Results(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.load(w, r, "view")
	if !ok {
		return
	}
	q := r.URL.Query()
	p := &database.ReportResultParams{
		Status: q.Get("status"), PolicyID: q.Get("policy"), PolicyType: q.Get("type"), NodeID: q.Get("node"), Search: q.Get("q"),
		Page: atoiDefault(q.Get("page"), 1), PerPage: atoiDefault(q.Get("per_page"), 25),
	}
	if v := q.Get("stale"); v != "" {
		b := v == "1" || v == "true"
		p.Stale = &b
	}
	rows, total, err := h.svc.Results(r.Context(), rec.ID, p)
	if err != nil {
		h.serviceError(w, err, "list report results")
		return
	}
	out := make([]reportResultResponse, 0, len(rows))
	for i := range rows {
		out = append(out, reportResultResponse{ReportResultRow: rows[i]})
	}
	if v := q.Get("include_items"); v == "1" || v == "true" {
		items, err := h.svc.ResultItems(r.Context(), rec, rows)
		if err != nil {
			h.serviceError(w, err, "read report items")
			return
		}
		for i := range out {
			out[i].ItemList = items[[2]string{out[i].NodeID, out[i].PolicyID}]
		}
	}
	writeJSON(w, http.StatusOK, paged(out, total, p.Page, p.PerPage))
}

// Policies handles GET /api/v1/compliance/reports/{id}/policies.
func (h *ReportsHandler) Policies(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.load(w, r, "view")
	if !ok {
		return
	}
	rows, err := h.svc.Policies(r.Context(), rec.ID)
	if err != nil {
		h.serviceError(w, err, "list report policies")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// Download handles GET /api/v1/compliance/reports/{id}/download?format=json|csv|jws|...&part=.
func (h *ReportsHandler) Download(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.load(w, r, "view")
	if !ok {
		return
	}
	q := r.URL.Query()
	format := strings.ToLower(strings.TrimSpace(q.Get("format")))
	if format == "" {
		format = "json"
	}
	dl, err := h.svc.Download(r.Context(), rec, format, strings.ToLower(strings.TrimSpace(q.Get("part"))), h.actor(r))
	if err != nil {
		h.serviceError(w, err, "download report")
		return
	}
	w.Header().Set("Content-Type", dl.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", dl.Filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(dl.Data)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(dl.Data); err != nil { //nolint:gosec // G705: an attachment (Content-Disposition set above), never rendered inline
		log.Printf("Failed to write report download: %v", err)
	}
}

// Delete handles DELETE /api/v1/compliance/reports/{id}.
func (h *ReportsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.load(w, r, "manage")
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), rec, h.actor(r)); err != nil {
		h.serviceError(w, err, "delete report")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ReportsHandler) serviceError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, services.ErrReportNotFound):
		writeJSONError(w, http.StatusNotFound, "report not found")
	case errors.Is(err, services.ErrReportEmptyScope), errors.Is(err, services.ErrReportUnknownGroup), errors.Is(err, services.ErrReportInvalid), errors.Is(err, services.ErrReportNoFormat):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrReportNoSignature):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrReportTooLarge):
		writeJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeJSONError(w, http.StatusGatewayTimeout, "report generation timed out; narrow its scope")
	default:
		log.Printf("Failed to %s: %v", what, err)
		writeJSONError(w, http.StatusInternalServerError, "failed to "+what)
	}
}
