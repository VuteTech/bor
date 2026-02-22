// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// AuditLogHandler handles audit log API endpoints
type AuditLogHandler struct {
	auditSvc *services.AuditService
}

// NewAuditLogHandler creates a new AuditLogHandler
func NewAuditLogHandler(auditSvc *services.AuditService) *AuditLogHandler {
	return &AuditLogHandler{auditSvc: auditSvc}
}

// List handles GET /api/v1/audit-logs
func (h *AuditLogHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	req := &models.AuditLogListRequest{
		Page:         1,
		PerPage:      25,
		ResourceType: r.URL.Query().Get("resource_type"),
		Action:       r.URL.Query().Get("action"),
		Username:     r.URL.Query().Get("username"),
	}

	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			req.Page = v
		}
	}
	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if v, err := strconv.Atoi(pp); err == nil && v > 0 {
			req.PerPage = v
		}
	}

	resp, err := h.auditSvc.List(r.Context(), req)
	if err != nil {
		log.Printf("Failed to list audit logs: %v", err)
		http.Error(w, `{"error":"failed to list audit logs"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode audit logs response: %v", err)
	}
}

// Export handles GET /api/v1/audit-logs/export?format=csv|json
func (h *AuditLogHandler) Export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}

	req := &models.AuditLogListRequest{
		ResourceType: r.URL.Query().Get("resource_type"),
		Action:       r.URL.Query().Get("action"),
		Username:     r.URL.Query().Get("username"),
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=audit_logs.csv")
		if err := h.auditSvc.ExportCSV(r.Context(), req, w); err != nil {
			log.Printf("Failed to export audit logs as CSV: %v", err)
			// Headers already sent, can't change status code
		}
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=audit_logs.json")
		if err := h.auditSvc.ExportJSON(r.Context(), req, w); err != nil {
			log.Printf("Failed to export audit logs as JSON: %v", err)
		}
	default:
		http.Error(w, `{"error":"invalid format, use csv or json"}`, http.StatusBadRequest)
	}
}
