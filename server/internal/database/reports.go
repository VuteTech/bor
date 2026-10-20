// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/reports"
	"github.com/VuteTech/Bor/server/pkg/report"
)

// ReportRepository stores compliance reports: the canonical bundle plus
// relational copies of its rows for paging and aggregation.
type ReportRepository struct {
	db *DB
}

// NewReportRepository creates a ReportRepository.
func NewReportRepository(db *DB) *ReportRepository {
	return &ReportRepository{db: db}
}

// ReportCounts counts results by compliance status.
type ReportCounts struct {
	Compliant    int `json:"compliant"`
	NonCompliant int `json:"non_compliant"`
	Error        int `json:"error"`
	Inapplicable int `json:"inapplicable"`
	Unknown      int `json:"unknown"`
}

// ReportRecord is a row of compliance_reports.
type ReportRecord struct {
	ID                string
	GeneratedAt       time.Time
	Trigger           string
	Kind              string
	GeneratedByUserID *string
	GeneratedBy       string
	ScheduleID        *string
	Label             string
	Notes             string
	Scope             report.Scope
	ScopeAll          bool
	ScopeGroupIDs     []string
	NodeCount         int
	NodesOnline       int
	NodesUnreachable  int
	ResultCount       int
	ResultsStale      int
	Counts            ReportCounts
	ScorePct          *float64
	BundleSHA256      []byte
	BundleBytes       int
	SignatureFormat   string
	SignatureValue    string
	SignatureKeyID    string
	Pinned            bool
	PinnedBy          *string
	PinnedAt          *time.Time
	CreatedAt         time.Time
}

// ReportNodeRow is a row of compliance_report_nodes.
type ReportNodeRow struct {
	NodeID          string       `json:"node_id"`
	NodeName        string       `json:"node_name"`
	OSName          string       `json:"os_name,omitempty"`
	OSVersion       string       `json:"os_version,omitempty"`
	AgentVersion    string       `json:"agent_version,omitempty"`
	Status          string       `json:"status"`
	LastSeen        *time.Time   `json:"last_seen,omitempty"`
	Unreachable     bool         `json:"unreachable"`
	Groups          []string     `json:"groups"`
	LSM             string       `json:"lsm,omitempty"`
	LSMMode         string       `json:"lsm_mode,omitempty"`
	Encrypted       *bool        `json:"encrypted,omitempty"`
	Escrowed        *bool        `json:"escrowed,omitempty"`
	PendingSecurity *int         `json:"pending_security,omitempty"`
	RebootRequired  *bool        `json:"reboot_required,omitempty"`
	Counts          ReportCounts `json:"by_status"`
}

// ReportResultRow is a row of compliance_report_results.
type ReportResultRow struct {
	NodeID        string     `json:"node_id"`
	NodeName      string     `json:"node_name"`
	PolicyID      string     `json:"policy_id"`
	PolicyName    string     `json:"policy_name"`
	PolicyType    string     `json:"policy_type"`
	PolicyVersion int        `json:"policy_version"`
	Status        string     `json:"status"`
	Message       string     `json:"message,omitempty"`
	ReportedAt    *time.Time `json:"reported_at,omitempty"`
	Stale         bool       `json:"stale"`
	ItemTotal     int        `json:"items"`
	ItemFailed    int        `json:"items_failed"`
}

// ReportPolicyRow aggregates a report's results per policy.
type ReportPolicyRow struct {
	PolicyID      string       `json:"policy_id"`
	PolicyName    string       `json:"policy_name"`
	PolicyType    string       `json:"policy_type"`
	PolicyVersion int          `json:"policy_version"`
	Nodes         int          `json:"nodes"`
	Counts        ReportCounts `json:"by_status"`
}

// ReportControlRow is a row of compliance_report_controls.
type ReportControlRow struct {
	Framework    string `json:"framework"`
	ControlID    string `json:"id"`
	Title        string `json:"title"`
	Pass         int    `json:"pass"`
	Fail         int    `json:"fail"`
	Error        int    `json:"error"`
	Inapplicable int    `json:"inapplicable"`
	NodesFailing int    `json:"nodes_failing"`
}

// ReportSourceFilter selects what a report covers.
type ReportSourceFilter struct {
	// AllNodes covers every node; otherwise the members of GroupIDs.
	AllNodes bool
	GroupIDs []string
	// PolicyTypes limits results to these types; empty means all.
	PolicyTypes []string
}

// LoadSource reads the nodes in scope with their posture and their live
// compliance results in one repeatable-read transaction, so the report
// describes one instant.
func (r *ReportRepository) LoadSource(ctx context.Context, f ReportSourceFilter) (*reports.Source, error) {
	if !f.AllNodes && len(f.GroupIDs) == 0 {
		return &reports.Source{}, nil
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("reports: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nodes, err := loadSourceNodes(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return &reports.Source{}, nil
	}
	nodeIDs := make([]string, 0, len(nodes))
	for i := range nodes {
		nodeIDs = append(nodeIDs, nodes[i].ID)
	}
	results, err := loadSourceResults(ctx, tx, nodeIDs, f.PolicyTypes)
	if err != nil {
		return nil, err
	}
	return &reports.Source{Nodes: nodes, Results: results}, nil
}

func loadSourceNodes(ctx context.Context, tx *sql.Tx, f ReportSourceFilter) ([]reports.SourceNode, error) {
	where := ""
	var args []interface{}
	if !f.AllNodes {
		where = "WHERE EXISTS (SELECT 1 FROM node_group_members m WHERE m.node_id = n.id AND m.node_group_id = ANY($1::uuid[]))"
		args = append(args, pq.Array(f.GroupIDs))
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT n.id, n.name, n.fqdn, n.os_name, n.os_version, n.desktop_env, n.agent_version, n.status_cached, n.last_seen,
		       n.lsm, n.lsm_mode, n.cert_not_after,
		       COALESCE(ARRAY(SELECT ng.id::text FROM node_group_members m JOIN node_groups ng ON ng.id = m.node_group_id WHERE m.node_id = n.id ORDER BY ng.name), '{}'),
		       COALESCE(ARRAY(SELECT ng.name FROM node_group_members m JOIN node_groups ng ON ng.id = m.node_group_id WHERE m.node_id = n.id ORDER BY ng.name), '{}'),
		       us.engine, us.effective_mode, us.pending_total, us.pending_security, us.reboot_required, us.reboot_since, us.reported_at,
		       de.tpm2_present, de.secure_boot, jsonb_array_length(de.unencrypted_system_mounts), de.reported_at,
		       sv.cipher, (sv.id IS NOT NULL), sv.has_key
		FROM nodes n
		LEFT JOIN node_updates_status us ON us.node_id = n.id
		LEFT JOIN node_disk_encryption de ON de.node_id = n.id
		LEFT JOIN LATERAL (
			SELECT v.id, v.cipher,
			       EXISTS (SELECT 1 FROM luks_recovery_keys k WHERE k.volume_id = v.id AND k.status = 'active') AS has_key
			FROM luks_volumes v
			WHERE v.node_id = n.id AND v.is_system
			ORDER BY v.created_at
			LIMIT 1) sv ON TRUE
		`+where+`
		ORDER BY n.name, n.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("reports: load nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []reports.SourceNode
	for rows.Next() {
		var (
			n                                reports.SourceNode
			fqdn, osName, osVersion, desktop sql.NullString
			agentVersion, lsm, lsmMode       sql.NullString
			lastSeen, certNotAfter           sql.NullTime
			groupIDs, groupNames             []string
			engine, mode                     sql.NullString
			pendingTotal, pendingSecurity    sql.NullInt64
			rebootRequired                   sql.NullBool
			rebootSince, updatesAt           sql.NullTime
			tpm2                             sql.NullBool
			secureBoot                       sql.NullString
			unencryptedMounts                sql.NullInt64
			diskAt                           sql.NullTime
			cipher                           sql.NullString
			hasSystemVolume, hasKey          sql.NullBool
		)
		if err := rows.Scan(&n.ID, &n.Name, &fqdn, &osName, &osVersion, &desktop, &agentVersion, &n.Status, &lastSeen,
			&lsm, &lsmMode, &certNotAfter, pq.Array(&groupIDs), pq.Array(&groupNames),
			&engine, &mode, &pendingTotal, &pendingSecurity, &rebootRequired, &rebootSince, &updatesAt,
			&tpm2, &secureBoot, &unencryptedMounts, &diskAt,
			&cipher, &hasSystemVolume, &hasKey); err != nil {
			return nil, fmt.Errorf("reports: scan node: %w", err)
		}
		n.FQDN, n.OSName, n.OSVersion, n.DesktopEnv = fqdn.String, osName.String, osVersion.String, desktop.String
		n.AgentVersion, n.LSM, n.LSMMode = agentVersion.String, lsm.String, lsmMode.String
		n.LastSeen = nullTimePtr(lastSeen)
		n.CertNotAfter = nullTimePtr(certNotAfter)
		n.GroupIDs, n.GroupNames = groupIDs, groupNames
		if engine.Valid {
			n.Updates = &reports.SourceUpdates{
				Engine:          engine.String,
				EffectiveMode:   mode.String,
				PendingTotal:    int(pendingTotal.Int64),
				PendingSecurity: int(pendingSecurity.Int64),
				RebootRequired:  rebootRequired.Bool,
				RebootSince:     nullTimePtr(rebootSince),
				ReportedAt:      nullTimePtr(updatesAt),
			}
		}
		if diskAt.Valid || hasSystemVolume.Bool {
			d := &reports.SourceDiskEncryption{
				SystemVolumeEncrypted:   hasSystemVolume.Bool,
				Cipher:                  cipher.String,
				Escrowed:                hasKey.Valid && hasKey.Bool,
				SecureBoot:              secureBoot.String,
				UnencryptedSystemMounts: int(unencryptedMounts.Int64),
				ReportedAt:              nullTimePtr(diskAt),
			}
			if tpm2.Valid {
				v := tpm2.Bool
				d.TPM2Present = &v
			}
			n.DiskEncryption = d
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func loadSourceResults(ctx context.Context, tx *sql.Tx, nodeIDs, policyTypes []string) ([]reports.SourceResult, error) {
	query := `
		SELECT cr.node_id, cr.policy_id, p.name, p.type, p.version, cr.status, COALESCE(cr.message, ''), cr.items_json, cr.reported_at
		FROM compliance_results cr
		JOIN policies p ON p.id = cr.policy_id
		WHERE cr.node_id = ANY($1::uuid[])
		  AND EXISTS (
			SELECT 1
			FROM policy_bindings pb
			JOIN node_group_members ngm ON ngm.node_group_id = pb.group_id
			WHERE pb.policy_id = cr.policy_id
			  AND ngm.node_id  = cr.node_id
			  AND pb.state     = 'enabled')`
	args := []interface{}{pq.Array(nodeIDs)}
	if len(policyTypes) > 0 {
		query += " AND p.type = ANY($2::text[])"
		args = append(args, pq.Array(policyTypes))
	}
	rows, err := tx.QueryContext(ctx, query+" ORDER BY cr.node_id, cr.policy_id", args...)
	if err != nil {
		return nil, fmt.Errorf("reports: load results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []reports.SourceResult
	for rows.Next() {
		var (
			res   reports.SourceResult
			items []byte
		)
		if err := rows.Scan(&res.NodeID, &res.PolicyID, &res.PolicyName, &res.PolicyType, &res.PolicyVersion, &res.Status, &res.Message, &items, &res.ReportedAt); err != nil {
			return nil, fmt.Errorf("reports: scan result: %w", err)
		}
		if len(items) > 0 {
			res.ItemsJSON = append([]byte(nil), items...)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

// Insert stores a report with its bundle and the relational copies of its
// rows in one transaction. rec.ID, the counts and the hash must be set.
func (r *ReportRepository) Insert(ctx context.Context, rec *ReportRecord, b *report.Bundle, gzipBundle []byte) error {
	scopeJSON, err := json.Marshal(rec.Scope)
	if err != nil {
		return fmt.Errorf("reports: encode scope: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reports: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var sigFormat, sigValue, sigKey sql.NullString
	if rec.SignatureValue != "" {
		sigFormat = sql.NullString{String: rec.SignatureFormat, Valid: true}
		sigValue = sql.NullString{String: rec.SignatureValue, Valid: true}
		sigKey = sql.NullString{String: rec.SignatureKeyID, Valid: true}
	}
	var score sql.NullFloat64
	if rec.ScorePct != nil {
		score = sql.NullFloat64{Float64: *rec.ScorePct, Valid: true}
	}
	groupIDs := rec.ScopeGroupIDs
	if groupIDs == nil {
		groupIDs = []string{}
	}
	if _, execErr := tx.ExecContext(ctx, `
		INSERT INTO compliance_reports (
			id, generated_at, trigger, kind, generated_by_user_id, generated_by, schedule_id, label, notes,
			scope_json, scope_all, scope_group_ids,
			node_count, nodes_online, nodes_unreachable, result_count, results_stale,
			count_compliant, count_non_compliant, count_error, count_inapplicable, count_unknown, score_pct,
			bundle_sha256, bundle_bytes, signature_format, signature_value, signature_key_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::uuid[], $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28)`,
		rec.ID, rec.GeneratedAt, rec.Trigger, rec.Kind, rec.GeneratedByUserID, rec.GeneratedBy, rec.ScheduleID, rec.Label, rec.Notes,
		scopeJSON, rec.ScopeAll, pq.Array(groupIDs),
		rec.NodeCount, rec.NodesOnline, rec.NodesUnreachable, rec.ResultCount, rec.ResultsStale,
		rec.Counts.Compliant, rec.Counts.NonCompliant, rec.Counts.Error, rec.Counts.Inapplicable, rec.Counts.Unknown, score,
		rec.BundleSHA256, rec.BundleBytes, sigFormat, sigValue, sigKey); execErr != nil {
		return fmt.Errorf("reports: insert report: %w", execErr)
	}
	if _, execErr := tx.ExecContext(ctx, `INSERT INTO compliance_report_bundles (report_id, gzip_json) VALUES ($1, $2)`, rec.ID, gzipBundle); execErr != nil {
		return fmt.Errorf("reports: insert bundle: %w", execErr)
	}

	nodeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO compliance_report_nodes (
			report_id, node_id, node_name, os_name, os_version, agent_version, status, last_seen, unreachable, groups,
			lsm, lsm_mode, encrypted, escrowed, pending_security, reboot_required,
			count_compliant, count_non_compliant, count_error, count_inapplicable, count_unknown)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`)
	if err != nil {
		return fmt.Errorf("reports: prepare nodes: %w", err)
	}
	defer func() { _ = nodeStmt.Close() }()
	for i := range b.Nodes {
		n := &b.Nodes[i]
		var encrypted, escrowed, reboot sql.NullBool
		var pending sql.NullInt64
		if d := n.Posture.DiskEncryption; d != nil {
			encrypted = sql.NullBool{Bool: d.SystemVolumeEncrypted, Valid: true}
			escrowed = sql.NullBool{Bool: d.RecoveryKeyEscrowed, Valid: true}
		}
		if u := n.Posture.Updates; u != nil {
			pending = sql.NullInt64{Int64: int64(u.PendingSecurity), Valid: true}
			reboot = sql.NullBool{Bool: u.RebootRequired, Valid: true}
		}
		groups := n.Groups
		if groups == nil {
			groups = []string{}
		}
		if _, execErr := nodeStmt.ExecContext(ctx, rec.ID, n.ID, n.Name, nullIfEmpty(n.OSName), nullIfEmpty(n.OSVersion), nullIfEmpty(n.AgentVersion),
			n.Status, parseBundleTime(n.LastSeen), n.Unreachable, pq.Array(groups),
			nullIfEmpty(n.Posture.LSM), nullIfEmpty(n.Posture.LSMMode), encrypted, escrowed, pending, reboot,
			n.ByStatus.Compliant, n.ByStatus.NonCompliant, n.ByStatus.Error, n.ByStatus.Inapplicable, n.ByStatus.Unknown); execErr != nil {
			return fmt.Errorf("reports: insert node row: %w", execErr)
		}
	}

	policies := make(map[string]report.PolicySummary, len(b.Policies))
	for _, p := range b.Policies {
		policies[p.ID] = p
	}
	resultStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO compliance_report_results (
			report_id, node_id, node_name, policy_id, policy_name, policy_type, policy_version, status, message, reported_at, stale, item_total, item_failed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`)
	if err != nil {
		return fmt.Errorf("reports: prepare results: %w", err)
	}
	defer func() { _ = resultStmt.Close() }()
	for i := range b.Results {
		res := &b.Results[i]
		p := policies[res.PolicyID]
		failed := 0
		for _, it := range res.Items {
			if it.Status == report.StatusNonCompliant || it.Status == report.StatusError {
				failed++
			}
		}
		if _, execErr := resultStmt.ExecContext(ctx, rec.ID, res.NodeID, res.Node, res.PolicyID, p.Name, p.Type, p.Version,
			res.Status, nullIfEmpty(res.Message), parseBundleTime(res.ReportedAt), res.Stale, len(res.Items), failed); execErr != nil {
			return fmt.Errorf("reports: insert result row: %w", execErr)
		}
	}

	for i := range b.Controls {
		c := &b.Controls[i]
		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO compliance_report_controls (report_id, framework, control_id, title, pass, fail, error, inapplicable, nodes_failing)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			rec.ID, c.Framework, c.ID, c.Title, c.Pass, c.Fail, c.Error, c.Inapplicable, c.NodesFailing); execErr != nil {
			return fmt.Errorf("reports: insert control row: %w", execErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reports: commit: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func parseBundleTime(s string) sql.NullTime {
	if s == "" {
		return sql.NullTime{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

const reportColumns = `
	id, generated_at, trigger, kind, generated_by_user_id, generated_by, schedule_id, label, notes,
	scope_json, scope_all, scope_group_ids,
	node_count, nodes_online, nodes_unreachable, result_count, results_stale,
	count_compliant, count_non_compliant, count_error, count_inapplicable, count_unknown, score_pct,
	bundle_sha256, bundle_bytes, signature_format, signature_value, signature_key_id,
	pinned, pinned_by, pinned_at, created_at`

type reportScanner interface {
	Scan(dest ...interface{}) error
}

func scanReport(s reportScanner) (*ReportRecord, error) {
	var (
		rec                         ReportRecord
		scopeJSON                   []byte
		groupIDs                    []string
		score                       sql.NullFloat64
		bundleBytes                 sql.NullInt64
		sigFormat, sigValue, sigKey sql.NullString
		pinnedBy                    sql.NullString
		pinnedAt                    sql.NullTime
	)
	if err := s.Scan(&rec.ID, &rec.GeneratedAt, &rec.Trigger, &rec.Kind, &rec.GeneratedByUserID, &rec.GeneratedBy, &rec.ScheduleID, &rec.Label, &rec.Notes,
		&scopeJSON, &rec.ScopeAll, pq.Array(&groupIDs),
		&rec.NodeCount, &rec.NodesOnline, &rec.NodesUnreachable, &rec.ResultCount, &rec.ResultsStale,
		&rec.Counts.Compliant, &rec.Counts.NonCompliant, &rec.Counts.Error, &rec.Counts.Inapplicable, &rec.Counts.Unknown, &score,
		&rec.BundleSHA256, &bundleBytes, &sigFormat, &sigValue, &sigKey,
		&rec.Pinned, &pinnedBy, &pinnedAt, &rec.CreatedAt); err != nil {
		return nil, err
	}
	if len(scopeJSON) > 0 {
		_ = json.Unmarshal(scopeJSON, &rec.Scope)
	}
	rec.ScopeGroupIDs = groupIDs
	if score.Valid {
		v := score.Float64
		rec.ScorePct = &v
	}
	rec.BundleBytes = int(bundleBytes.Int64)
	rec.SignatureFormat, rec.SignatureValue, rec.SignatureKeyID = sigFormat.String, sigValue.String, sigKey.String
	if pinnedBy.Valid {
		v := pinnedBy.String
		rec.PinnedBy = &v
	}
	rec.PinnedAt = nullTimePtr(pinnedAt)
	rec.GeneratedAt = rec.GeneratedAt.UTC()
	rec.CreatedAt = rec.CreatedAt.UTC()
	return &rec, nil
}

// ReportListParams filter and page the report list.
type ReportListParams struct {
	// Scope limits the list to reports whose scope lies inside these groups
	// (a delegated administrator's view). nil means every report.
	Scope   *models.GroupScopeFilter
	Trigger string
	Kind    string
	// GroupID keeps reports that cover the group (or every node).
	GroupID string
	// Search matches the label.
	Search  string
	From    *time.Time
	To      *time.Time
	Page    int
	PerPage int
}

func buildReportListFilter(p *ReportListParams) (where string, args []interface{}) {
	var sb strings.Builder
	sb.WriteString(" WHERE TRUE")
	add := func(cond string, v interface{}) {
		args = append(args, v)
		sb.WriteString(" AND " + strings.ReplaceAll(cond, "$N", fmt.Sprintf("$%d", len(args))))
	}
	if p.Scope != nil {
		if len(p.Scope.GroupIDs) == 0 {
			sb.WriteString(" AND FALSE")
		} else {
			add("(NOT scope_all AND cardinality(scope_group_ids) > 0 AND scope_group_ids <@ $N::uuid[])", pq.Array(p.Scope.GroupIDs))
		}
	}
	if p.Trigger != "" {
		add("trigger = $N", p.Trigger)
	}
	if p.Kind != "" {
		add("kind = $N", p.Kind)
	}
	if p.GroupID != "" {
		add("(scope_all OR $N::uuid = ANY(scope_group_ids))", p.GroupID)
	}
	if s := strings.TrimSpace(p.Search); s != "" {
		add("(label ILIKE $N OR generated_by ILIKE $N)", "%"+s+"%")
	}
	if p.From != nil {
		add("generated_at >= $N", *p.From)
	}
	if p.To != nil {
		add("generated_at < $N", *p.To)
	}
	return sb.String(), args
}

// List returns one page of reports, newest first, and the total count.
func (r *ReportRepository) List(ctx context.Context, p *ReportListParams) ([]*ReportRecord, int, error) {
	where, args := buildReportListFilter(p)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM compliance_reports`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("reports: count: %w", err)
	}
	page, perPage := models.ClampPagination(p.Page, p.PerPage)
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.QueryContext(ctx, `SELECT `+reportColumns+` FROM compliance_reports`+where+
		fmt.Sprintf(" ORDER BY generated_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("reports: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*ReportRecord
	for rows.Next() {
		rec, err := scanReport(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("reports: scan: %w", err)
		}
		out = append(out, rec)
	}
	return out, total, rows.Err()
}

// Get returns a report, or nil when it does not exist.
func (r *ReportRepository) Get(ctx context.Context, id string) (*ReportRecord, error) {
	rec, err := scanReport(r.db.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM compliance_reports WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reports: get: %w", err)
	}
	return rec, nil
}

// GetBundle returns the stored gzip-compressed bundle, or nil when absent.
func (r *ReportRepository) GetBundle(ctx context.Context, id string) ([]byte, error) {
	var gz []byte
	err := r.db.QueryRowContext(ctx, `SELECT gzip_json FROM compliance_report_bundles WHERE report_id = $1`, id).Scan(&gz)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reports: get bundle: %w", err)
	}
	return gz, nil
}

// ReportNodeParams filter and page a report's nodes.
type ReportNodeParams struct {
	Status      string
	Unreachable *bool
	Search      string
	Page        int
	PerPage     int
}

// ListNodes returns one page of a report's node rows, by name.
func (r *ReportRepository) ListNodes(ctx context.Context, reportID string, p *ReportNodeParams) ([]ReportNodeRow, int, error) {
	where := " WHERE report_id = $1"
	args := []interface{}{reportID}
	if p.Status != "" {
		args = append(args, p.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if p.Unreachable != nil {
		args = append(args, *p.Unreachable)
		where += fmt.Sprintf(" AND unreachable = $%d", len(args))
	}
	if s := strings.TrimSpace(p.Search); s != "" {
		args = append(args, "%"+s+"%")
		where += fmt.Sprintf(" AND node_name ILIKE $%d", len(args))
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM compliance_report_nodes`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("reports: count nodes: %w", err)
	}
	page, perPage := models.ClampPagination(p.Page, p.PerPage)
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.QueryContext(ctx, `
		SELECT node_id, node_name, os_name, os_version, agent_version, status, last_seen, unreachable, groups,
		       lsm, lsm_mode, encrypted, escrowed, pending_security, reboot_required,
		       count_compliant, count_non_compliant, count_error, count_inapplicable, count_unknown
		FROM compliance_report_nodes`+where+fmt.Sprintf(" ORDER BY node_name, node_id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("reports: list nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ReportNodeRow{}
	for rows.Next() {
		var (
			n                        ReportNodeRow
			osName, osVersion, agent sql.NullString
			lastSeen                 sql.NullTime
			groups                   []string
			lsm, lsmMode             sql.NullString
			encrypted, escrowed      sql.NullBool
			pending                  sql.NullInt64
			reboot                   sql.NullBool
		)
		if err := rows.Scan(&n.NodeID, &n.NodeName, &osName, &osVersion, &agent, &n.Status, &lastSeen, &n.Unreachable, pq.Array(&groups),
			&lsm, &lsmMode, &encrypted, &escrowed, &pending, &reboot,
			&n.Counts.Compliant, &n.Counts.NonCompliant, &n.Counts.Error, &n.Counts.Inapplicable, &n.Counts.Unknown); err != nil {
			return nil, 0, fmt.Errorf("reports: scan node: %w", err)
		}
		n.OSName, n.OSVersion, n.AgentVersion = osName.String, osVersion.String, agent.String
		n.LastSeen = nullTimePtr(lastSeen)
		n.Groups = groups
		if n.Groups == nil {
			n.Groups = []string{}
		}
		n.LSM, n.LSMMode = lsm.String, lsmMode.String
		if encrypted.Valid {
			v := encrypted.Bool
			n.Encrypted = &v
		}
		if escrowed.Valid {
			v := escrowed.Bool
			n.Escrowed = &v
		}
		if pending.Valid {
			v := int(pending.Int64)
			n.PendingSecurity = &v
		}
		if reboot.Valid {
			v := reboot.Bool
			n.RebootRequired = &v
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

// ReportResultParams filter and page a report's results.
type ReportResultParams struct {
	Status     string
	PolicyID   string
	PolicyType string
	NodeID     string
	Stale      *bool
	// Search matches the node or policy name.
	Search  string
	Page    int
	PerPage int
}

// ListResults returns one page of a report's result rows, by node then policy.
func (r *ReportRepository) ListResults(ctx context.Context, reportID string, p *ReportResultParams) ([]ReportResultRow, int, error) {
	where := " WHERE report_id = $1"
	args := []interface{}{reportID}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if p.Status != "" {
		add("status = $%d", p.Status)
	}
	if p.PolicyID != "" {
		add("policy_id = $%d", p.PolicyID)
	}
	if p.PolicyType != "" {
		add("policy_type = $%d", p.PolicyType)
	}
	if p.NodeID != "" {
		add("node_id = $%d", p.NodeID)
	}
	if p.Stale != nil {
		add("stale = $%d", *p.Stale)
	}
	if s := strings.TrimSpace(p.Search); s != "" {
		args = append(args, "%"+s+"%")
		where += fmt.Sprintf(" AND (node_name ILIKE $%d OR policy_name ILIKE $%d)", len(args), len(args))
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM compliance_report_results`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("reports: count results: %w", err)
	}
	page, perPage := models.ClampPagination(p.Page, p.PerPage)
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.QueryContext(ctx, `
		SELECT node_id, node_name, policy_id, policy_name, policy_type, policy_version, status, COALESCE(message, ''), reported_at, stale, item_total, item_failed
		FROM compliance_report_results`+where+fmt.Sprintf(" ORDER BY node_name, node_id, policy_name, policy_id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("reports: list results: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ReportResultRow{}
	for rows.Next() {
		var (
			row        ReportResultRow
			reportedAt sql.NullTime
		)
		if err := rows.Scan(&row.NodeID, &row.NodeName, &row.PolicyID, &row.PolicyName, &row.PolicyType, &row.PolicyVersion, &row.Status, &row.Message, &reportedAt, &row.Stale, &row.ItemTotal, &row.ItemFailed); err != nil {
			return nil, 0, fmt.Errorf("reports: scan result: %w", err)
		}
		row.ReportedAt = nullTimePtr(reportedAt)
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// ListPolicies aggregates a report's results per policy, by policy name.
func (r *ReportRepository) ListPolicies(ctx context.Context, reportID string) ([]ReportPolicyRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT policy_id, policy_name, policy_type, policy_version, COUNT(*),
		       COUNT(*) FILTER (WHERE status = 'compliant'),
		       COUNT(*) FILTER (WHERE status = 'non_compliant'),
		       COUNT(*) FILTER (WHERE status = 'error'),
		       COUNT(*) FILTER (WHERE status = 'inapplicable'),
		       COUNT(*) FILTER (WHERE status NOT IN ('compliant', 'non_compliant', 'error', 'inapplicable'))
		FROM compliance_report_results
		WHERE report_id = $1
		GROUP BY policy_id, policy_name, policy_type, policy_version
		ORDER BY policy_name, policy_id`, reportID)
	if err != nil {
		return nil, fmt.Errorf("reports: list policies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ReportPolicyRow{}
	for rows.Next() {
		var p ReportPolicyRow
		if err := rows.Scan(&p.PolicyID, &p.PolicyName, &p.PolicyType, &p.PolicyVersion, &p.Nodes,
			&p.Counts.Compliant, &p.Counts.NonCompliant, &p.Counts.Error, &p.Counts.Inapplicable, &p.Counts.Unknown); err != nil {
			return nil, fmt.Errorf("reports: scan policy: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListControls returns a report's control aggregates.
func (r *ReportRepository) ListControls(ctx context.Context, reportID string) ([]ReportControlRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT framework, control_id, title, pass, fail, error, inapplicable, nodes_failing
		FROM compliance_report_controls WHERE report_id = $1 ORDER BY framework, control_id`, reportID)
	if err != nil {
		return nil, fmt.Errorf("reports: list controls: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ReportControlRow{}
	for rows.Next() {
		var c ReportControlRow
		if err := rows.Scan(&c.Framework, &c.ControlID, &c.Title, &c.Pass, &c.Fail, &c.Error, &c.Inapplicable, &c.NodesFailing); err != nil {
			return nil, fmt.Errorf("reports: scan control: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete removes a report and everything stored with it. It reports
// whether a row was deleted.
func (r *ReportRepository) Delete(ctx context.Context, id string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM compliance_reports WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("reports: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PurgeOlderThan deletes unpinned reports generated before cutoff.
func (r *ReportRepository) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM compliance_reports WHERE generated_at < $1 AND NOT pinned`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("reports: purge: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
