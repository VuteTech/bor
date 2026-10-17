// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// SystemdRepository stores the systemd units nodes report for the policy
// editor's unit picker.
type SystemdRepository struct {
	db *DB
}

// NewSystemdRepository creates a new SystemdRepository.
func NewSystemdRepository(db *DB) *SystemdRepository {
	return &SystemdRepository{db: db}
}

// SystemdCatalogueUnit is one catalogue row: the reported unit, how many
// nodes reported a unit of that name (union listing) and when.
type SystemdCatalogueUnit struct {
	Info       *pb.SystemdUnitInfo
	NodeCount  int
	ReportedAt time.Time
}

var systemdUnitMarshal = protojson.MarshalOptions{UseProtoNames: false}

// systemdInsertBatch bounds the parameters of one multi-row INSERT.
const systemdInsertBatch = 500

// ReplaceNodeUnits replaces the units one node reported.
func (r *SystemdRepository) ReplaceNodeUnits(ctx context.Context, nodeID string, units []*pb.SystemdUnitInfo) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("systemd: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_systemd_units WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("systemd: delete node units: %w", err)
	}
	now := time.Now()
	for start := 0; start < len(units); start += systemdInsertBatch {
		end := min(start+systemdInsertBatch, len(units))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO node_systemd_units (node_id, name, unit_type, info_json, reported_at) VALUES `)
		args := make([]any, 0, (end-start)*5)
		for i, u := range units[start:end] {
			data, err := systemdUnitMarshal.Marshal(u)
			if err != nil {
				return fmt.Errorf("systemd: marshal unit %s: %w", u.GetName(), err)
			}
			if i > 0 {
				sb.WriteString(", ")
			}
			n := len(args)
			fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d)", n+1, n+2, n+3, n+4, n+5)
			args = append(args, nodeID, u.GetName(), u.GetType(), string(data), now)
		}
		sb.WriteString(` ON CONFLICT (node_id, name) DO UPDATE SET unit_type = EXCLUDED.unit_type, info_json = EXCLUDED.info_json, reported_at = EXCLUDED.reported_at`)
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("systemd: insert node units: %w", err)
		}
	}
	return tx.Commit()
}

// ListUnits returns the union of the units every node reported, one row
// per name with the most recently reported state. q filters by a
// case-insensitive substring of the name or description, typ by unit
// type; both may be empty.
func (r *SystemdRepository) ListUnits(ctx context.Context, q, typ string) ([]SystemdCatalogueUnit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ON (u.name) u.info_json, u.reported_at,
		       (SELECT COUNT(*) FROM node_systemd_units c WHERE c.name = u.name) AS node_count
		FROM node_systemd_units u
		WHERE ($1 = '' OR u.name ILIKE '%' || $1 || '%' OR u.info_json->>'description' ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR u.unit_type = $2)
		ORDER BY u.name, u.reported_at DESC`, q, typ)
	if err != nil {
		return nil, fmt.Errorf("systemd: list units: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSystemdUnits(rows)
}

// ListUnitsByNode returns the units one node reported.
func (r *SystemdRepository) ListUnitsByNode(ctx context.Context, nodeID, q, typ string) ([]SystemdCatalogueUnit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.info_json, u.reported_at, 1 AS node_count
		FROM node_systemd_units u
		WHERE u.node_id = $1
		  AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.info_json->>'description' ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR u.unit_type = $3)
		ORDER BY u.name`, nodeID, q, typ)
	if err != nil {
		return nil, fmt.Errorf("systemd: list units by node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSystemdUnits(rows)
}

func scanSystemdUnits(rows rowScanner) ([]SystemdCatalogueUnit, error) {
	var out []SystemdCatalogueUnit
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	for rows.Next() {
		var (
			raw        string
			reportedAt time.Time
			nodeCount  int
		)
		if err := rows.Scan(&raw, &reportedAt, &nodeCount); err != nil {
			return nil, fmt.Errorf("systemd: scan unit: %w", err)
		}
		var info pb.SystemdUnitInfo
		if err := opts.Unmarshal([]byte(raw), &info); err != nil {
			return nil, fmt.Errorf("systemd: unmarshal unit: %w", err)
		}
		out = append(out, SystemdCatalogueUnit{Info: &info, NodeCount: nodeCount, ReportedAt: reportedAt})
	}
	return out, rows.Err()
}
