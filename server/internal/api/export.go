// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/export"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// ExportHandler serves policy export/import in the bor.dev/v1 envelope
// format (docs/policy-export-import-plan.md).
type ExportHandler struct {
	policySvc  *services.PolicyService
	bindingSvc *services.PolicyBindingService
	groupSvc   *services.NodeGroupService
	auditSvc   *services.AuditService
}

// NewExportHandler wires the export/import endpoints to their services.
func NewExportHandler(policySvc *services.PolicyService, bindingSvc *services.PolicyBindingService, groupSvc *services.NodeGroupService, auditSvc *services.AuditService) *ExportHandler {
	return &ExportHandler{policySvc: policySvc, bindingSvc: bindingSvc, groupSvc: groupSvc, auditSvc: auditSvc}
}

// Export handles GET /api/v1/policies/export?ids=a,b&include_bindings=true&format=yaml|json
func (h *ExportHandler) Export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	wantIDs := map[string]bool{}
	if ids := strings.TrimSpace(q.Get("ids")); ids != "" {
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				wantIDs[id] = true
			}
		}
	}
	includeBindings := q.Get("include_bindings") == "true"
	format := q.Get("format")
	if format == "" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" {
		http.Error(w, `{"error":"format must be yaml or json"}`, http.StatusBadRequest)
		return
	}

	all, err := h.policySvc.ListAllPolicies(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list policies"}`, http.StatusInternalServerError)
		return
	}
	var selected []*models.Policy
	for _, p := range all {
		if len(wantIDs) == 0 || wantIDs[p.ID] {
			selected = append(selected, p)
		}
	}
	if len(selected) == 0 {
		http.Error(w, `{"error":"no matching policies"}`, http.StatusNotFound)
		return
	}

	// Stable slugs, unique within the bundle.
	slugByID := make(map[string]string, len(selected))
	used := map[string]int{}
	for _, p := range selected {
		s := export.Slugify(p.Name)
		used[s]++
		if used[s] > 1 {
			s = fmt.Sprintf("%s-%d", s, used[s])
		}
		slugByID[p.ID] = s
	}

	var docs [][]byte
	for _, p := range selected {
		doc, derr := export.BuildPolicyDoc(p, slugByID[p.ID])
		if derr != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, derr.Error()), http.StatusInternalServerError)
			return
		}
		docs = append(docs, doc)
	}
	if includeBindings {
		bindings, berr := h.bindingSvc.ListBindings(r.Context())
		if berr != nil {
			http.Error(w, `{"error":"failed to list bindings"}`, http.StatusInternalServerError)
			return
		}
		for _, b := range bindings {
			slug, ok := slugByID[b.PolicyID]
			if !ok {
				continue
			}
			doc, derr := export.BuildBindingDoc(slug, b.PolicyName, b.GroupName, b.Priority)
			if derr != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, derr.Error()), http.StatusInternalServerError)
				return
			}
			docs = append(docs, doc)
		}
	}

	var out []byte
	var contentType, filename string
	if format == "json" {
		out, err = export.MarshalBundleJSON(docs)
		contentType, filename = "application/json", "bor-policies.json"
	} else {
		out, err = export.MarshalBundle(docs)
		contentType, filename = "application/yaml", "bor-policies.yaml"
	}
	if err != nil {
		http.Error(w, `{"error":"failed to serialize bundle"}`, http.StatusInternalServerError)
		return
	}
	if len(selected) == 1 {
		filename = slugByID[selected[0].ID] + "." + format
	}

	h.audit(r, "policy.export", fmt.Sprintf(`{"policies":%d,"bindings_included":%t}`, len(selected), includeBindings))

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if _, err := w.Write(out); err != nil {
		log.Printf("Failed to write export response: %v", err)
	}
}

// Import handles POST /api/v1/policies/import?dry_run=true&on_conflict=error|skip|new-version
func (h *ExportHandler) Import(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, export.MaxBundleBytes))
	if err != nil {
		http.Error(w, `{"error":"request body too large or unreadable"}`, http.StatusRequestEntityTooLarge)
		return
	}

	claims := GetUserFromContext(r.Context())
	actor := ""
	if claims != nil {
		actor = claims.Username
	}
	opts := export.Options{
		DryRun:     r.URL.Query().Get("dry_run") == "true",
		OnConflict: r.URL.Query().Get("on_conflict"),
		Actor:      actor,
	}

	report, err := export.Import(r.Context(), h.policySvc, h.bindingSvc, h.groupSvc, services.ValidatePolicyContentByType, body, opts)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if !opts.DryRun {
		h.audit(r, "policy.import", fmt.Sprintf(`{"created":%d,"updated":%d,"skipped":%d,"errors":%d}`,
			report.Created, report.Updated, report.Skipped, report.Errors))
	}

	w.Header().Set("Content-Type", "application/json")
	if !report.Ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	if err := json.NewEncoder(w).Encode(report); err != nil {
		log.Printf("Failed to encode import report: %v", err)
	}
}

func (h *ExportHandler) audit(r *http.Request, action, details string) {
	if h.auditSvc == nil {
		return
	}
	claims := GetUserFromContext(r.Context())
	username := ""
	if claims != nil {
		username = claims.Username
	}
	h.auditSvc.LogEvent(r.Context(), &models.AuditLog{
		Username:     username,
		Action:       action,
		ResourceType: "policy",
		Details:      details,
	})
}
