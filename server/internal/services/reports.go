// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/reports"
	"github.com/VuteTech/Bor/server/pkg/ext"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	"github.com/VuteTech/Bor/server/pkg/report"
)

// Errors of the report service.
var (
	ErrReportNotFound     = errors.New("report not found")
	ErrReportEmptyScope   = errors.New("the report scope selects no node group")
	ErrReportUnknownGroup = errors.New("unknown node group in the report scope")
	ErrReportTooLarge     = errors.New("the report would exceed the configured maximum number of results; narrow its scope")
	ErrReportInvalid      = errors.New("invalid report request")
	ErrReportNoSignature  = errors.New("the report is not signed")
	ErrReportNoFormat     = errors.New("unknown report format")
)

// ReportConfig is the service's configuration (config.ReportsConfig).
type ReportConfig struct {
	RetentionDays int
	StaleAfter    time.Duration
	MaxResults    int
}

// ReportActor is who performs a report operation, for the audit log.
type ReportActor struct {
	UserID   string
	Username string
	IP       string
}

// GenerateReportRequest describes the report to generate.
type GenerateReportRequest struct {
	AllNodes     bool
	GroupIDs     []string
	PolicyTypes  []string
	Label        string
	Notes        string
	IncludeItems bool
}

// ReportIntegrity is the result of checking a stored report.
type ReportIntegrity struct {
	SHA256 string `json:"sha256"`
	// Hash is "ok" when the stored bundle still hashes to SHA256, "mismatch"
	// when it does not, "missing" when the bundle is gone.
	Hash string `json:"hash"`
	// Signature is "unsigned", "valid", "invalid" or "unverified" (signed,
	// but no signer is available to check it).
	Signature       string `json:"signature"`
	SignatureFormat string `json:"signature_format,omitempty"`
	SignatureKeyID  string `json:"signature_key_id,omitempty"`
}

// ReportSummary is a report as the API lists it.
type ReportSummary struct {
	ID                string              `json:"id"`
	GeneratedAt       time.Time           `json:"generated_at"`
	Trigger           string              `json:"trigger"`
	Kind              string              `json:"kind"`
	GeneratedBy       string              `json:"generated_by"`
	GeneratedByUserID *string             `json:"generated_by_user_id,omitempty"`
	Label             string              `json:"label"`
	Notes             string              `json:"notes"`
	Scope             report.Scope        `json:"scope"`
	Nodes             int                 `json:"nodes"`
	NodesOnline       int                 `json:"nodes_online"`
	NodesUnreachable  int                 `json:"nodes_unreachable"`
	Results           int                 `json:"results"`
	ResultsStale      int                 `json:"results_stale"`
	ByStatus          report.StatusCounts `json:"by_status"`
	ScorePct          *float64            `json:"score_pct"`
	Pinned            bool                `json:"pinned"`
	BundleBytes       int                 `json:"bundle_bytes"`
	SHA256            string              `json:"sha256,omitempty"`
	Signed            bool                `json:"signed"`
	// Integrity is set when the report was opened (not in lists).
	Integrity *ReportIntegrity `json:"integrity,omitempty"`
	// Formats lists the download formats available for this report.
	Formats []string `json:"formats,omitempty"`
}

// ReportDownload is a rendered report ready to send.
type ReportDownload struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ReportService generates, stores and serves compliance reports.
type ReportService struct {
	repo        *database.ReportRepository
	groups      *database.NodeGroupRepository
	registry    *reports.Registry
	audit       *AuditService
	version     string
	cfg         ReportConfig
	controlsFor func(schemaID string) []reports.Control
	now         func() time.Time

	janitorCancel context.CancelFunc
	janitorWG     sync.WaitGroup
}

// NewReportService creates a ReportService. registry may be nil (no
// signer, no extra formats); audit may be nil (no audit events).
func NewReportService(repo *database.ReportRepository, groups *database.NodeGroupRepository, registry *reports.Registry, audit *AuditService, version string, cfg ReportConfig) *ReportService {
	if registry == nil {
		registry = reports.NewRegistry()
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 100000
	}
	return &ReportService{
		repo:     repo,
		groups:   groups,
		registry: registry,
		audit:    audit,
		version:  version,
		cfg:      cfg,
		controlsFor: func(schemaID string) []reports.Control {
			controls := ComplianceControlsFor(schemaID)
			out := make([]reports.Control, 0, len(controls))
			for _, c := range controls {
				out = append(out, reports.Control{Framework: c.Framework, ID: c.ID, Title: c.Title})
			}
			return out
		},
		now: time.Now,
	}
}

const (
	maxReportLabel = 255
	maxReportNotes = 10000
)

var policyTypeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,49}$`)

// Generate builds and stores a report for req on behalf of actor.
func (s *ReportService) Generate(ctx context.Context, req *GenerateReportRequest, actor ReportActor) (*ReportSummary, error) {
	label := strings.TrimSpace(req.Label)
	notes := strings.TrimSpace(req.Notes)
	if len(label) > maxReportLabel || len(notes) > maxReportNotes {
		return nil, fmt.Errorf("%w: label or notes too long", ErrReportInvalid)
	}
	types := uniqueSorted(req.PolicyTypes)
	for _, t := range types {
		if !policyTypeName.MatchString(t) {
			return nil, fmt.Errorf("%w: policy type %q", ErrReportInvalid, t)
		}
	}
	groupIDs := uniqueSorted(req.GroupIDs)
	if !req.AllNodes && len(groupIDs) == 0 {
		return nil, ErrReportEmptyScope
	}

	scope := report.Scope{AllNodes: req.AllNodes, PolicyTypes: types}
	if !req.AllNodes {
		names, err := s.groupNames(ctx, groupIDs)
		if err != nil {
			return nil, err
		}
		for _, id := range groupIDs {
			scope.NodeGroups = append(scope.NodeGroups, report.Ref{ID: id, Name: names[id]})
		}
	}

	src, err := s.repo.LoadSource(ctx, database.ReportSourceFilter{AllNodes: req.AllNodes, GroupIDs: groupIDs, PolicyTypes: types})
	if err != nil {
		return nil, err
	}
	if len(src.Results) > s.cfg.MaxResults {
		return nil, ErrReportTooLarge
	}

	id := uuid.New().String()
	generatedAt := s.now().UTC()
	bundle := reports.Build(src, &reports.Options{
		ID:            id,
		GeneratedAt:   generatedAt,
		Trigger:       report.TriggerManual,
		Actor:         report.Actor{UserID: actor.UserID, Username: actor.Username},
		ServerVersion: s.version,
		Label:         label,
		Notes:         notes,
		Scope:         scope,
		StaleAfter:    s.cfg.StaleAfter,
		IncludeItems:  req.IncludeItems,
		ControlsFor:   s.controlsFor,
	})
	canonical, err := reports.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	sum := reports.Hash(canonical)
	gz, err := reports.Gzip(canonical)
	if err != nil {
		return nil, err
	}

	rec := &database.ReportRecord{
		ID:               id,
		GeneratedAt:      generatedAt,
		Trigger:          bundle.Trigger,
		Kind:             "full",
		GeneratedBy:      actor.Username,
		Label:            label,
		Notes:            notes,
		Scope:            bundle.Scope,
		ScopeAll:         req.AllNodes,
		ScopeGroupIDs:    groupIDs,
		NodeCount:        bundle.Summary.Nodes,
		NodesOnline:      bundle.Summary.NodesOnline,
		NodesUnreachable: bundle.Summary.NodesUnreachable,
		ResultCount:      bundle.Summary.Results,
		ResultsStale:     bundle.Summary.ResultsStale,
		Counts:           toCounts(bundle.Summary.ByStatus),
		ScorePct:         bundle.Summary.ScorePct,
		BundleSHA256:     sum,
		BundleBytes:      len(canonical),
	}
	if actor.UserID != "" {
		uid := actor.UserID
		rec.GeneratedByUserID = &uid
	}
	if signer := s.registry.Signer(); signer != nil {
		sig, err := signer.Sign(ctx, canonical)
		if err != nil {
			s.recordEvent(ctx, actor, "report.generate", id, false, map[string]interface{}{"error": "signing failed", "sha256": hex.EncodeToString(sum)})
			return nil, fmt.Errorf("sign report: %w", err)
		}
		if sig != nil {
			rec.SignatureFormat, rec.SignatureValue, rec.SignatureKeyID = sig.Format, sig.Value, sig.KeyID
		}
	}
	if err := s.repo.Insert(ctx, rec, bundle, gz); err != nil {
		s.recordEvent(ctx, actor, "report.generate", id, false, map[string]interface{}{"error": "storing failed", "sha256": hex.EncodeToString(sum)})
		return nil, err
	}
	s.recordEvent(ctx, actor, "report.generate", id, true, map[string]interface{}{
		"trigger": rec.Trigger, "label": label, "scope": bundle.Scope,
		"nodes": rec.NodeCount, "results": rec.ResultCount, "by_status": bundle.Summary.ByStatus,
		"score_pct": rec.ScorePct, "sha256": hex.EncodeToString(sum), "signature_key_id": rec.SignatureKeyID,
	})
	summary := s.summary(rec)
	summary.Integrity = &ReportIntegrity{SHA256: summary.SHA256, Hash: "ok", Signature: s.signatureState(rec, canonical)}
	summary.Integrity.SignatureFormat, summary.Integrity.SignatureKeyID = rec.SignatureFormat, rec.SignatureKeyID
	return summary, nil
}

func (s *ReportService) groupNames(ctx context.Context, ids []string) (map[string]string, error) {
	if s.groups == nil {
		return nil, ErrReportUnknownGroup
	}
	all, err := s.groups.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list node groups: %w", err)
	}
	names := make(map[string]string, len(all))
	for _, g := range all {
		names[g.ID] = g.Name
	}
	for _, id := range ids {
		if _, ok := names[id]; !ok {
			return nil, ErrReportUnknownGroup
		}
	}
	return names, nil
}

func uniqueSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func toCounts(c report.StatusCounts) database.ReportCounts {
	return database.ReportCounts{Compliant: c.Compliant, NonCompliant: c.NonCompliant, Error: c.Error, Inapplicable: c.Inapplicable, Unknown: c.Unknown}
}

func fromCounts(c database.ReportCounts) report.StatusCounts {
	return report.StatusCounts{Compliant: c.Compliant, NonCompliant: c.NonCompliant, Error: c.Error, Inapplicable: c.Inapplicable, Unknown: c.Unknown}
}

func (s *ReportService) summary(rec *database.ReportRecord) *ReportSummary {
	out := &ReportSummary{
		ID:                rec.ID,
		GeneratedAt:       rec.GeneratedAt,
		Trigger:           rec.Trigger,
		Kind:              rec.Kind,
		GeneratedBy:       rec.GeneratedBy,
		GeneratedByUserID: rec.GeneratedByUserID,
		Label:             rec.Label,
		Notes:             rec.Notes,
		Scope:             rec.Scope,
		Nodes:             rec.NodeCount,
		NodesOnline:       rec.NodesOnline,
		NodesUnreachable:  rec.NodesUnreachable,
		Results:           rec.ResultCount,
		ResultsStale:      rec.ResultsStale,
		ByStatus:          fromCounts(rec.Counts),
		ScorePct:          rec.ScorePct,
		Pinned:            rec.Pinned,
		BundleBytes:       rec.BundleBytes,
		Signed:            rec.SignatureValue != "",
		Formats:           s.formats(rec),
	}
	if out.Scope.NodeGroups == nil {
		out.Scope.NodeGroups = []report.Ref{}
	}
	if out.Scope.PolicyTypes == nil {
		out.Scope.PolicyTypes = []string{}
	}
	if out.Scope.Policies == nil {
		out.Scope.Policies = []report.Ref{}
	}
	if len(rec.BundleSHA256) > 0 {
		out.SHA256 = hex.EncodeToString(rec.BundleSHA256)
	}
	return out
}

func (s *ReportService) formats(rec *database.ReportRecord) []string {
	formats := []string{reports.FormatJSON, reports.FormatCSV}
	if rec.SignatureValue != "" {
		formats = append(formats, reports.FormatJWS)
	}
	return append(formats, s.registry.Formats()...)
}

// ListReportsParams page and filter the report list.
type ListReportsParams = database.ReportListParams

// List returns one page of reports and the total.
func (s *ReportService) List(ctx context.Context, p *ListReportsParams) ([]*ReportSummary, int, error) {
	recs, total, err := s.repo.List(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*ReportSummary, 0, len(recs))
	for _, rec := range recs {
		out = append(out, s.summary(rec))
	}
	return out, total, nil
}

// Record returns the stored report row, or ErrReportNotFound.
func (s *ReportService) Record(ctx context.Context, id string) (*database.ReportRecord, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrReportNotFound
	}
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, ErrReportNotFound
	}
	return rec, nil
}

// Get returns a report with its integrity checked: the stored bundle is
// re-hashed and, when a signer is available, the signature verified. An
// integrity failure is audited.
func (s *ReportService) Get(ctx context.Context, rec *database.ReportRecord, actor ReportActor) (*ReportSummary, error) {
	summary := s.summary(rec)
	integrity := &ReportIntegrity{SHA256: summary.SHA256, Hash: "ok", Signature: "unsigned", SignatureFormat: rec.SignatureFormat, SignatureKeyID: rec.SignatureKeyID}
	canonical, err := s.canonical(ctx, rec)
	switch {
	case err != nil:
		return nil, err
	case canonical == nil:
		integrity.Hash = "missing"
		integrity.Signature = s.signatureState(rec, nil)
	default:
		if !bytes.Equal(reports.Hash(canonical), rec.BundleSHA256) {
			integrity.Hash = "mismatch"
		}
		integrity.Signature = s.signatureState(rec, canonical)
	}
	if integrity.Hash != "ok" || integrity.Signature == "invalid" {
		s.recordEvent(ctx, actor, "report.integrity_failure", rec.ID, false, map[string]interface{}{
			"hash": integrity.Hash, "signature": integrity.Signature, "sha256": summary.SHA256,
		})
	}
	summary.Integrity = integrity
	return summary, nil
}

// signatureState says what the stored signature is worth for canonical:
// unsigned, valid, invalid, or unverified when no signer can check it.
func (s *ReportService) signatureState(rec *database.ReportRecord, canonical []byte) string {
	if rec.SignatureValue == "" {
		return "unsigned"
	}
	signer := s.registry.Signer()
	if signer == nil || canonical == nil {
		return "unverified"
	}
	sig := &ext.ReportSignature{Format: rec.SignatureFormat, Value: rec.SignatureValue, KeyID: rec.SignatureKeyID}
	if err := signer.Verify(context.Background(), canonical, sig); err != nil {
		return "invalid"
	}
	return "valid"
}

func (s *ReportService) canonical(ctx context.Context, rec *database.ReportRecord) ([]byte, error) {
	gz, err := s.repo.GetBundle(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	if gz == nil {
		return nil, nil
	}
	return reports.Gunzip(gz)
}

// Bundle returns the parsed bundle of a report.
func (s *ReportService) Bundle(ctx context.Context, rec *database.ReportRecord) (*report.Bundle, error) {
	canonical, err := s.canonical(ctx, rec)
	if err != nil {
		return nil, err
	}
	if canonical == nil {
		return nil, ErrReportNotFound
	}
	return report.Parse(canonical)
}

// Download renders a report in format (json, csv with a part, jws, or a
// registered renderer's format) and audits the download.
func (s *ReportService) Download(ctx context.Context, rec *database.ReportRecord, format, part string, actor ReportActor) (*ReportDownload, error) {
	canonical, err := s.canonical(ctx, rec)
	if err != nil {
		return nil, err
	}
	if canonical == nil {
		return nil, ErrReportNotFound
	}
	base := reportFilename(rec)
	var dl *ReportDownload
	switch format {
	case reports.FormatJSON:
		dl = &ReportDownload{Filename: base + ".json", ContentType: "application/json", Data: canonical}
	case reports.FormatJWS:
		if rec.SignatureValue == "" {
			return nil, ErrReportNoSignature
		}
		dl = &ReportDownload{Filename: base + ".jws", ContentType: "application/jose", Data: []byte(rec.SignatureValue)}
	case reports.FormatCSV:
		if part == "" {
			part = reports.CSVResults
		}
		b, err := report.Parse(canonical)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := reports.WriteCSV(&buf, b, part); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReportNoFormat, err)
		}
		dl = &ReportDownload{Filename: base + "-" + part + ".csv", ContentType: "text/csv; charset=utf-8", Data: buf.Bytes()}
	default:
		renderer := s.registry.Renderer(format)
		if renderer == nil {
			return nil, ErrReportNoFormat
		}
		var sig *ext.ReportSignature
		if rec.SignatureValue != "" {
			sig = &ext.ReportSignature{Format: rec.SignatureFormat, Value: rec.SignatureValue, KeyID: rec.SignatureKeyID}
		}
		data, err := renderer.Render(ctx, canonical, sig)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", format, err)
		}
		dl = &ReportDownload{Filename: base + "." + renderer.FileExtension(), ContentType: renderer.ContentType(), Data: data}
	}
	s.recordEvent(ctx, actor, "report.download", rec.ID, true, map[string]interface{}{"format": format, "part": part, "filename": dl.Filename})
	return dl, nil
}

var filenameUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func reportFilename(rec *database.ReportRecord) string {
	slug := strings.Trim(filenameUnsafe.ReplaceAllString(strings.ToLower(rec.Label), "-"), "-")
	if slug == "" {
		slug = strings.SplitN(rec.ID, "-", 2)[0]
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return "compliance-report-" + slug + "-" + rec.GeneratedAt.UTC().Format("20060102-1504")
}

// Delete removes a report. Pinned reports are refused.
func (s *ReportService) Delete(ctx context.Context, rec *database.ReportRecord, actor ReportActor) error {
	if rec.Pinned {
		return fmt.Errorf("%w: the report is pinned", ErrReportInvalid)
	}
	deleted, err := s.repo.Delete(ctx, rec.ID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrReportNotFound
	}
	s.recordEvent(ctx, actor, "report.delete", rec.ID, true, map[string]interface{}{
		"label": rec.Label, "generated_at": rec.GeneratedAt, "sha256": hex.EncodeToString(rec.BundleSHA256),
	})
	return nil
}

// Nodes, Results, Policies and Controls page the stored copies of a report.

// Nodes returns one page of a report's nodes.
func (s *ReportService) Nodes(ctx context.Context, reportID string, p *database.ReportNodeParams) ([]database.ReportNodeRow, int, error) {
	return s.repo.ListNodes(ctx, reportID, p)
}

// Results returns one page of a report's results.
func (s *ReportService) Results(ctx context.Context, reportID string, p *database.ReportResultParams) ([]database.ReportResultRow, int, error) {
	return s.repo.ListResults(ctx, reportID, p)
}

// Policies aggregates a report's results per policy.
func (s *ReportService) Policies(ctx context.Context, reportID string) ([]database.ReportPolicyRow, error) {
	return s.repo.ListPolicies(ctx, reportID)
}

// Controls returns a report's control aggregates.
func (s *ReportService) Controls(ctx context.Context, reportID string) ([]database.ReportControlRow, error) {
	return s.repo.ListControls(ctx, reportID)
}

// ResultItems returns the items of the results on one page, keyed by
// node id and policy id, read from the bundle.
func (s *ReportService) ResultItems(ctx context.Context, rec *database.ReportRecord, rows []database.ReportResultRow) (map[[2]string][]report.Item, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	b, err := s.Bundle(ctx, rec)
	if err != nil {
		return nil, err
	}
	wanted := make(map[[2]string]bool, len(rows))
	for i := range rows {
		wanted[[2]string{rows[i].NodeID, rows[i].PolicyID}] = true
	}
	out := make(map[[2]string][]report.Item, len(rows))
	for i := range b.Results {
		key := [2]string{b.Results[i].NodeID, b.Results[i].PolicyID}
		if wanted[key] {
			out[key] = b.Results[i].Items
		}
	}
	return out, nil
}

// ─── Retention ───────────────────────────────────────────────────────────

// StartJanitor purges reports older than the retention period, hourly,
// when retention is configured.
func (s *ReportService) StartJanitor(ctx context.Context) {
	if s.cfg.RetentionDays <= 0 {
		return
	}
	jctx, cancel := context.WithCancel(ctx)
	s.janitorCancel = cancel
	s.janitorWG.Add(1)
	go func() {
		defer s.janitorWG.Done()
		timer := time.NewTimer(3 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-jctx.Done():
				return
			case <-timer.C:
			}
			s.Purge(jctx)
			timer.Reset(time.Hour)
		}
	}()
	log.Printf("Compliance report retention enabled: %d days", s.cfg.RetentionDays)
}

// StopJanitor stops the purge goroutine and waits for it.
func (s *ReportService) StopJanitor() {
	if s.janitorCancel != nil {
		s.janitorCancel()
	}
	s.janitorWG.Wait()
}

// Purge runs one retention pass. Exported for tests.
func (s *ReportService) Purge(ctx context.Context) {
	if s.cfg.RetentionDays <= 0 {
		return
	}
	cutoff := s.now().AddDate(0, 0, -s.cfg.RetentionDays)
	n, err := s.repo.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		log.Printf("WARNING: compliance report retention purge failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("Compliance report retention: purged %d reports older than %d days", n, s.cfg.RetentionDays)
		s.recordEvent(ctx, ReportActor{Username: "system"}, "report.purge", "", true, map[string]interface{}{
			"count": n, "cutoff": cutoff.UTC().Format(time.RFC3339), "retention_days": s.cfg.RetentionDays,
		})
	}
}

// ─── Audit ───────────────────────────────────────────────────────────────

func (s *ReportService) recordEvent(ctx context.Context, actor ReportActor, action, reportID string, success bool, details map[string]interface{}) {
	if s.audit == nil {
		return
	}
	body, _ := json.Marshal(details)
	outcome := auditpb.Outcome_OUTCOME_SUCCESS
	if !success {
		outcome = auditpb.Outcome_OUTCOME_FAILURE
	}
	s.audit.Emit(ctx, &auditpb.AuditEvent{
		OccurredAt: timestamppb.New(s.now()),
		Actor:      &auditpb.Actor{UserId: actor.UserID, Username: actor.Username},
		Action:     action,
		Resource:   &auditpb.Resource{Type: "compliance_report", Id: reportID},
		Outcome:    outcome,
		SrcIp:      actor.IP,
		Payload:    &auditpb.AuditEvent_HttpChange{HttpChange: &auditpb.HttpPayload{BodyJson: string(body)}},
	})
}

// ScopeAllows reports whether a report lies inside scope: a report over
// every node, or over groups outside the scope, is not visible to a
// delegated administrator.
func ScopeAllows(scope *models.GroupScopeFilter, rec *database.ReportRecord) bool {
	if scope == nil {
		return true
	}
	if rec.ScopeAll || len(rec.ScopeGroupIDs) == 0 {
		return false
	}
	allowed := make(map[string]bool, len(scope.GroupIDs))
	for _, id := range scope.GroupIDs {
		allowed[id] = true
	}
	for _, id := range rec.ScopeGroupIDs {
		if !allowed[id] {
			return false
		}
	}
	return true
}
