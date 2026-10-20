// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/services"
	"github.com/VuteTech/Bor/server/pkg/report"
)

type fakeReportService struct {
	records   map[string]*database.ReportRecord
	generated *services.GenerateReportRequest
	deleted   []string
	downloads []string
}

func newFakeReportService() *fakeReportService {
	return &fakeReportService{records: map[string]*database.ReportRecord{
		"11111111-1111-1111-1111-111111111111": {ID: "11111111-1111-1111-1111-111111111111", Label: "fleet", ScopeAll: true, GeneratedAt: time.Now()},
		"22222222-2222-2222-2222-222222222222": {ID: "22222222-2222-2222-2222-222222222222", Label: "lab", ScopeGroupIDs: []string{"g1"}, GeneratedAt: time.Now()},
		"33333333-3333-3333-3333-333333333333": {ID: "33333333-3333-3333-3333-333333333333", Label: "held", ScopeGroupIDs: []string{"g1", "g2"}, Pinned: true, GeneratedAt: time.Now()},
	}}
}

func (f *fakeReportService) Generate(_ context.Context, req *services.GenerateReportRequest, actor services.ReportActor) (*services.ReportSummary, error) {
	f.generated = req
	return &services.ReportSummary{ID: "new", Label: req.Label, GeneratedBy: actor.Username, Scope: report.Scope{AllNodes: req.AllNodes}}, nil
}

func (f *fakeReportService) List(_ context.Context, p *services.ListReportsParams) ([]*services.ReportSummary, int, error) {
	var out []*services.ReportSummary
	for _, rec := range f.records {
		if services.ScopeAllows(p.Scope, rec) {
			out = append(out, &services.ReportSummary{ID: rec.ID, Label: rec.Label})
		}
	}
	return out, len(out), nil
}

func (f *fakeReportService) Record(_ context.Context, id string) (*database.ReportRecord, error) {
	rec := f.records[id]
	if rec == nil {
		return nil, services.ErrReportNotFound
	}
	return rec, nil
}

func (f *fakeReportService) Get(_ context.Context, rec *database.ReportRecord, _ services.ReportActor) (*services.ReportSummary, error) {
	return &services.ReportSummary{ID: rec.ID, Label: rec.Label, Integrity: &services.ReportIntegrity{Hash: "ok", Signature: "unsigned"}}, nil
}

func (f *fakeReportService) Nodes(context.Context, string, *database.ReportNodeParams) ([]database.ReportNodeRow, int, error) {
	return []database.ReportNodeRow{{NodeID: "n1", NodeName: "alpha"}}, 1, nil
}

func (f *fakeReportService) Results(context.Context, string, *database.ReportResultParams) ([]database.ReportResultRow, int, error) {
	return []database.ReportResultRow{{NodeID: "n1", PolicyID: "p1", Status: "compliant"}}, 1, nil
}

func (f *fakeReportService) ResultItems(context.Context, *database.ReportRecord, []database.ReportResultRow) (map[[2]string][]report.Item, error) {
	return map[[2]string][]report.Item{{"n1", "p1"}: {{SchemaID: "selinux:mode", Key: "enforcing", Status: "compliant"}}}, nil
}

func (f *fakeReportService) Policies(context.Context, string) ([]database.ReportPolicyRow, error) {
	return []database.ReportPolicyRow{{PolicyID: "p1", PolicyName: "P", Nodes: 1}}, nil
}

func (f *fakeReportService) Download(_ context.Context, _ *database.ReportRecord, format, part string, _ services.ReportActor) (*services.ReportDownload, error) {
	f.downloads = append(f.downloads, format+"/"+part)
	switch format {
	case "json":
		return &services.ReportDownload{Filename: "r.json", ContentType: "application/json", Data: []byte(`{}`)}, nil
	case "jws":
		return nil, services.ErrReportNoSignature
	default:
		return nil, services.ErrReportNoFormat
	}
}

func (f *fakeReportService) Delete(_ context.Context, rec *database.ReportRecord, _ services.ReportActor) error {
	if rec.Pinned {
		return services.ErrReportInvalid
	}
	f.deleted = append(f.deleted, rec.ID)
	return nil
}

func reportRequest(method, path, body string, grants map[string]authz.Scope) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, http.NoBody)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if i := strings.Index(path, "/reports/"); i >= 0 {
		rest := path[i+len("/reports/"):]
		if j := strings.IndexAny(rest, "/?"); j >= 0 {
			rest = rest[:j]
		}
		r.SetPathValue("id", rest)
	}
	st := &authzState{userID: "u1", grants: authz.NewGrants(grants)}
	return r.WithContext(context.WithValue(r.Context(), authzContextKey{}, st))
}

func TestReportsHandler_CreateGlobal(t *testing.T) {
	svc := newFakeReportService()
	h := NewReportsHandler(svc, false, time.Second)
	rr := httptest.NewRecorder()
	h.Create(rr, reportRequest(http.MethodPost, "/api/v1/compliance/reports", `{"scope":{"all_nodes":true,"policy_types":["Selinux"]},"label":"Q4"}`,
		map[string]authz.Scope{"report:create": authz.GlobalScope()}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if svc.generated == nil || !svc.generated.AllNodes || svc.generated.Label != "Q4" || !svc.generated.IncludeItems || len(svc.generated.PolicyTypes) != 1 {
		t.Fatalf("request %+v", svc.generated)
	}
}

func TestReportsHandler_CreateScopedIsConfined(t *testing.T) {
	svc := newFakeReportService()
	h := NewReportsHandler(svc, false, time.Second)
	scope := map[string]authz.Scope{"report:create": authz.GroupScope("g1", "g2")}

	// "All nodes" becomes the caller's groups.
	rr := httptest.NewRecorder()
	h.Create(rr, reportRequest(http.MethodPost, "/api/v1/compliance/reports", `{"scope":{"all_nodes":true}}`, scope))
	if rr.Code != http.StatusCreated || svc.generated.AllNodes || len(svc.generated.GroupIDs) != 2 {
		t.Fatalf("status %d request %+v", rr.Code, svc.generated)
	}

	// A group outside the scope is refused.
	rr = httptest.NewRecorder()
	h.Create(rr, reportRequest(http.MethodPost, "/api/v1/compliance/reports", `{"scope":{"node_group_ids":["g1","g9"]}}`, scope))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}

	// Groups inside the scope pass through.
	rr = httptest.NewRecorder()
	h.Create(rr, reportRequest(http.MethodPost, "/api/v1/compliance/reports", `{"scope":{"node_group_ids":["g2"]},"include_items":false}`, scope))
	if rr.Code != http.StatusCreated || svc.generated.GroupIDs[0] != "g2" || svc.generated.IncludeItems {
		t.Fatalf("status %d request %+v", rr.Code, svc.generated)
	}
}

func TestReportsHandler_CreateBadBody(t *testing.T) {
	h := NewReportsHandler(newFakeReportService(), false, time.Second)
	rr := httptest.NewRecorder()
	h.Create(rr, reportRequest(http.MethodPost, "/api/v1/compliance/reports", `{`, map[string]authz.Scope{"report:create": authz.GlobalScope()}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestReportsHandler_ListAndGetHonourScope(t *testing.T) {
	svc := newFakeReportService()
	h := NewReportsHandler(svc, false, time.Second)
	scoped := map[string]authz.Scope{"report:view": authz.GroupScope("g1")}

	rr := httptest.NewRecorder()
	h.List(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports", "", scoped))
	var list listReportsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || list.Total != 1 || list.Items[0].Label != "lab" {
		t.Fatalf("list %s (%v)", rr.Body.String(), err)
	}

	// The fleet report is invisible to a scoped viewer.
	rr = httptest.NewRecorder()
	h.Get(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111", "", scoped))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("fleet report status %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.Get(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/22222222-2222-2222-2222-222222222222", "", scoped))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"integrity"`) {
		t.Fatalf("lab report status %d: %s", rr.Code, rr.Body.String())
	}
	// A report over g1 and g2 is outside a g1-only scope.
	rr = httptest.NewRecorder()
	h.Get(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/33333333-3333-3333-3333-333333333333", "", scoped))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("mixed report status %d", rr.Code)
	}
	// Globally it opens.
	rr = httptest.NewRecorder()
	h.Get(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111", "", map[string]authz.Scope{"report:view": authz.GlobalScope()}))
	if rr.Code != http.StatusOK {
		t.Fatalf("global status %d", rr.Code)
	}
	// Unknown id.
	rr = httptest.NewRecorder()
	h.Get(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/nope", "", map[string]authz.Scope{"report:view": authz.GlobalScope()}))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown status %d", rr.Code)
	}
}

func TestReportsHandler_ResultsWithItems(t *testing.T) {
	h := NewReportsHandler(newFakeReportService(), false, time.Second)
	rr := httptest.NewRecorder()
	h.Results(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111/results?include_items=1&stale=1", "", map[string]authz.Scope{"report:view": authz.GlobalScope()}))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"item_list":[{"schema_id":"selinux:mode"`) {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.Nodes(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111/nodes", "", map[string]authz.Scope{"report:view": authz.GlobalScope()}))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"node_name":"alpha"`) {
		t.Fatalf("nodes %d: %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.Policies(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111/policies", "", map[string]authz.Scope{"report:view": authz.GlobalScope()}))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"policy_name":"P"`) {
		t.Fatalf("policies %d: %s", rr.Code, rr.Body.String())
	}
}

func TestReportsHandler_Download(t *testing.T) {
	svc := newFakeReportService()
	h := NewReportsHandler(svc, false, time.Second)
	global := map[string]authz.Scope{"report:view": authz.GlobalScope()}

	rr := httptest.NewRecorder()
	h.Download(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111/download", "", global))
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Disposition") != `attachment; filename="r.json"` || rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("json download %d %v", rr.Code, rr.Header())
	}
	rr = httptest.NewRecorder()
	h.Download(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111/download?format=JWS", "", global))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("jws of an unsigned report %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.Download(rr, reportRequest(http.MethodGet, "/api/v1/compliance/reports/11111111-1111-1111-1111-111111111111/download?format=pdf", "", global))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown format %d", rr.Code)
	}
	if strings.Join(svc.downloads, ",") != "json/,jws/,pdf/" {
		t.Fatalf("downloads %v", svc.downloads)
	}
}

func TestReportsHandler_Delete(t *testing.T) {
	svc := newFakeReportService()
	h := NewReportsHandler(svc, false, time.Second)
	manage := map[string]authz.Scope{"report:manage": authz.GlobalScope()}

	rr := httptest.NewRecorder()
	h.Delete(rr, reportRequest(http.MethodDelete, "/api/v1/compliance/reports/22222222-2222-2222-2222-222222222222", "", manage))
	if rr.Code != http.StatusNoContent || len(svc.deleted) != 1 {
		t.Fatalf("delete %d %v", rr.Code, svc.deleted)
	}
	rr = httptest.NewRecorder()
	h.Delete(rr, reportRequest(http.MethodDelete, "/api/v1/compliance/reports/33333333-3333-3333-3333-333333333333", "", manage))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("pinned delete %d", rr.Code)
	}
}
