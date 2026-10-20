// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/lib/pq"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// UpdatesRepository stores the Software updates inventory of nodes.
type UpdatesRepository struct {
	db *DB
}

// NewUpdatesRepository creates an UpdatesRepository.
func NewUpdatesRepository(db *DB) *UpdatesRepository {
	return &UpdatesRepository{db: db}
}

var updatesMarshal = protojson.MarshalOptions{UseProtoNames: false}

const updatesInsertBatch = 200

// UpdatesNodeStatus is the status row of one node: the inventory without
// its pending list, as protojson, plus the columns the lists filter on.
type UpdatesNodeStatus struct {
	NodeID          string
	Engine          string
	EffectiveMode   string
	ManagedBy       string
	PendingTotal    int
	PendingSecurity int
	RebootRequired  bool
	RebootSince     *time.Time
	InfoJSON        json.RawMessage
	ReportedAt      time.Time
}

// UpdatesPendingRow is one pending update of a node.
type UpdatesPendingRow struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Advisory string   `json:"advisory"`
	Severity string   `json:"severity"`
	Security bool     `json:"security"`
	CVEs     []string `json:"cves"`
}

// ReplaceNodeInventory replaces the status row and the pending rows of a
// node in one transaction.
func (r *UpdatesRepository) ReplaceNodeInventory(ctx context.Context, nodeID string, inv *pb.UpdatesInventory) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("updates: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()

	// The status row keeps the inventory without the pending list, which
	// has its own table.
	stripped := proto.Clone(inv).(*pb.UpdatesInventory) //nolint:errcheck // Clone returns the same type
	stripped.Pending = nil
	info, err := updatesMarshal.Marshal(stripped)
	if err != nil {
		return fmt.Errorf("updates: marshal inventory: %w", err)
	}
	var since sql.NullTime
	if rb := inv.GetReboot(); rb != nil && rb.GetSince() != nil {
		since = sql.NullTime{Time: rb.GetSince().AsTime(), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO node_updates_status (node_id, engine, effective_mode, managed_by, pending_total, pending_security, reboot_required, reboot_since, info_json, reported_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (node_id) DO UPDATE SET engine = EXCLUDED.engine, effective_mode = EXCLUDED.effective_mode,
		  managed_by = EXCLUDED.managed_by, pending_total = EXCLUDED.pending_total, pending_security = EXCLUDED.pending_security,
		  reboot_required = EXCLUDED.reboot_required, reboot_since = EXCLUDED.reboot_since, info_json = EXCLUDED.info_json,
		  reported_at = EXCLUDED.reported_at`,
		nodeID, inv.GetEngine(), inv.GetEffectiveMode(), inv.GetEngineManagedBy(), inv.GetPendingTotal(), inv.GetPendingSecurity(),
		inv.GetReboot().GetRequired(), since, string(info), now); err != nil {
		return fmt.Errorf("updates: upsert status: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_updates_pending WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("updates: delete pending: %w", err)
	}
	pending := inv.GetPending()
	for start := 0; start < len(pending); start += updatesInsertBatch {
		end := min(start+updatesInsertBatch, len(pending))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO node_updates_pending (node_id, kind, name, version, advisory, severity, security, cves_json, reported_at) VALUES `)
		args := make([]any, 0, (end-start)*9)
		for i, p := range pending[start:end] {
			if i > 0 {
				sb.WriteString(", ")
			}
			cves := p.GetCves()
			if cves == nil {
				cves = []string{}
			}
			cvesJSON, _ := json.Marshal(cves)
			n := len(args)
			fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)", n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9)
			args = append(args, nodeID, p.GetKind(), p.GetName(), p.GetVersion(), p.GetAdvisory(), p.GetSeverity(), p.GetSecurity(), string(cvesJSON), now)
		}
		sb.WriteString(` ON CONFLICT (node_id, kind, name) DO UPDATE SET version = EXCLUDED.version, advisory = EXCLUDED.advisory,
		  severity = EXCLUDED.severity, security = EXCLUDED.security, cves_json = EXCLUDED.cves_json, reported_at = EXCLUDED.reported_at`)
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("updates: insert pending: %w", err)
		}
	}
	return tx.Commit()
}

// GetNodeStatus returns the status row of a node, or nil when the node
// has not reported.
func (r *UpdatesRepository) GetNodeStatus(ctx context.Context, nodeID string) (*UpdatesNodeStatus, error) {
	var (
		st    UpdatesNodeStatus
		since sql.NullTime
		info  string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT node_id, engine, effective_mode, managed_by, pending_total, pending_security, reboot_required, reboot_since, info_json, reported_at
		FROM node_updates_status WHERE node_id = $1`, nodeID).Scan(
		&st.NodeID, &st.Engine, &st.EffectiveMode, &st.ManagedBy, &st.PendingTotal, &st.PendingSecurity, &st.RebootRequired, &since, &info, &st.ReportedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("updates: get status: %w", err)
	}
	if since.Valid {
		t := since.Time
		st.RebootSince = &t
	}
	st.InfoJSON = json.RawMessage(info)
	return &st, nil
}

// ListPending lists the pending updates of a node, security first, with
// an optional name filter.
func (r *UpdatesRepository) ListPending(ctx context.Context, nodeID, q string, securityOnly bool, limit int) ([]UpdatesPendingRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `SELECT kind, name, version, advisory, severity, security, cves_json FROM node_updates_pending WHERE node_id = $1`
	args := []any{nodeID}
	if q != "" {
		query += ` AND (name ILIKE $2 OR advisory ILIKE $2 OR cves_json::text ILIKE $2)`
		args = append(args, "%"+q+"%")
	}
	if securityOnly {
		query += ` AND security`
	}
	query += fmt.Sprintf(` ORDER BY security DESC, CASE severity WHEN 'critical' THEN 4 WHEN 'important' THEN 3 WHEN 'moderate' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC, name LIMIT %d`, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("updates: list pending: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []UpdatesPendingRow{}
	for rows.Next() {
		var row UpdatesPendingRow
		var cves string
		if err := rows.Scan(&row.Kind, &row.Name, &row.Version, &row.Advisory, &row.Severity, &row.Security, &cves); err != nil {
			return nil, fmt.Errorf("updates: scan pending: %w", err)
		}
		_ = json.Unmarshal([]byte(cves), &row.CVEs)
		if row.CVEs == nil {
			row.CVEs = []string{}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SummariesByNodeIDs returns the list summary of every given node that
// has reported, keyed by node id.
func (r *UpdatesRepository) SummariesByNodeIDs(ctx context.Context, nodeIDs []string) (map[string]*models.NodeUpdatesSummary, error) {
	out := map[string]*models.NodeUpdatesSummary{}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT node_id, engine, COALESCE((info_json->>'engineInstalled')::boolean, FALSE), pending_total, pending_security, reboot_required, reboot_since, reported_at
		FROM node_updates_status WHERE node_id = ANY($1::uuid[])`, pq.Array(nodeIDs))
	if err != nil {
		return nil, fmt.Errorf("updates: summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id    string
			sm    models.NodeUpdatesSummary
			since sql.NullTime
		)
		if err := rows.Scan(&id, &sm.Engine, &sm.EngineInstalled, &sm.PendingTotal, &sm.PendingSecurity, &sm.RebootRequired, &since, &sm.ReportedAt); err != nil {
			return nil, fmt.Errorf("updates: scan summary: %w", err)
		}
		if since.Valid {
			t := since.Time
			sm.RebootSince = &t
		}
		s := sm
		out[id] = &s
	}
	return out, rows.Err()
}

// FleetSummary counts the nodes by their update state. A non-nil scope
// limits the count to nodes in at least one of the scope's groups.
func (r *UpdatesRepository) FleetSummary(ctx context.Context, scope *models.GroupScopeFilter) (*models.UpdatesFleetSummary, error) {
	scopeCond, args := "TRUE", []any(nil)
	if scope != nil {
		if len(scope.GroupIDs) == 0 {
			return &models.UpdatesFleetSummary{}, nil
		}
		scopeCond = "EXISTS (SELECT 1 FROM node_group_members sgm WHERE sgm.node_id = n.id AND sgm.node_group_id = ANY($1::uuid[]))"
		args = append(args, pq.Array(scope.GroupIDs))
	}
	query := fmt.Sprintf(`
		SELECT COUNT(*),
		       COUNT(us.node_id),
		       COUNT(*) FILTER (WHERE us.pending_security > 0),
		       COUNT(*) FILTER (WHERE us.reboot_required),
		       COUNT(*) FILTER (WHERE us.node_id IS NOT NULL AND (us.engine = 'none' OR NOT COALESCE((us.info_json->>'engineInstalled')::boolean, FALSE)))
		FROM nodes n LEFT JOIN node_updates_status us ON us.node_id = n.id
		WHERE %s`, scopeCond)
	var sm models.UpdatesFleetSummary
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&sm.Nodes, &sm.Reported, &sm.SecurityPending, &sm.RebootPending, &sm.EngineMissing); err != nil {
		return nil, fmt.Errorf("updates: fleet summary: %w", err)
	}
	return &sm, nil
}
