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

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// MacRepository stores the mandatory access control inventory nodes
// report: the status of SELinux or AppArmor, the denial summary, and the
// booleans, modules or profiles the policy editors pick from.
type MacRepository struct {
	db *DB
}

// NewMacRepository creates a new MacRepository.
func NewMacRepository(db *DB) *MacRepository {
	return &MacRepository{db: db}
}

// Item kinds of node_mac_items.
const (
	MacKindBoolean = "boolean"
	MacKindModule  = "module"
	MacKindProfile = "profile"
)

var macMarshal = protojson.MarshalOptions{UseProtoNames: false}

// macInsertBatch bounds the parameters of one multi-row INSERT.
const macInsertBatch = 500

// MacNodeStatus is the status row of one node.
type MacNodeStatus struct {
	NodeID       string
	LSM          string
	Mode         string
	Selinux      *pb.SelinuxStatus
	Apparmor     *pb.ApparmorStatus
	Denials      []*pb.MacDenialGroup
	DenialsSince *time.Time
	ReportedAt   time.Time
}

// MacInventory is what one report carries, already sanitised.
type MacInventory struct {
	LSM          string
	Mode         string
	Selinux      *pb.SelinuxStatus
	Apparmor     *pb.ApparmorStatus
	Booleans     []*pb.SelinuxBooleanInfo
	Modules      []*pb.SelinuxModuleInfo
	Profiles     []*pb.ApparmorProfileInfo
	Denials      []*pb.MacDenialGroup
	DenialsSince *time.Time
}

// MacCatalogueItem is one inventory item: the kind, the protojson of the
// item, how many nodes reported it (union listing) and when.
type MacCatalogueItem struct {
	Kind       string
	Name       string
	InfoJSON   json.RawMessage
	NodeCount  int
	ReportedAt time.Time
}

// ReplaceNodeInventory replaces the status row and every item of a node
// in one transaction.
func (r *MacRepository) ReplaceNodeInventory(ctx context.Context, nodeID string, inv *MacInventory) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mac: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()

	var info []byte
	switch {
	case inv.Selinux != nil:
		info, err = macMarshal.Marshal(inv.Selinux)
	case inv.Apparmor != nil:
		info, err = macMarshal.Marshal(inv.Apparmor)
	default:
		info = []byte("{}")
	}
	if err != nil {
		return fmt.Errorf("mac: marshal status: %w", err)
	}
	denials := make([]json.RawMessage, 0, len(inv.Denials))
	for _, d := range inv.Denials {
		data, merr := macMarshal.Marshal(d)
		if merr != nil {
			return fmt.Errorf("mac: marshal denial: %w", merr)
		}
		denials = append(denials, data)
	}
	denialsJSON, err := json.Marshal(denials)
	if err != nil {
		return fmt.Errorf("mac: marshal denials: %w", err)
	}
	var since sql.NullTime
	if inv.DenialsSince != nil {
		since = sql.NullTime{Time: *inv.DenialsSince, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO node_mac_status (node_id, lsm, mode, info_json, denials_json, denials_since, reported_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (node_id) DO UPDATE SET lsm = EXCLUDED.lsm, mode = EXCLUDED.mode,
		  info_json = EXCLUDED.info_json, denials_json = EXCLUDED.denials_json,
		  denials_since = EXCLUDED.denials_since, reported_at = EXCLUDED.reported_at`,
		nodeID, inv.LSM, inv.Mode, string(info), string(denialsJSON), since, now); err != nil {
		return fmt.Errorf("mac: upsert status: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_mac_items WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("mac: delete node items: %w", err)
	}
	type row struct {
		lsm, kind, name string
		data            []byte
	}
	var rows []row
	for _, b := range inv.Booleans {
		data, err := macMarshal.Marshal(b)
		if err != nil {
			return fmt.Errorf("mac: marshal boolean %s: %w", b.GetName(), err)
		}
		rows = append(rows, row{"selinux", MacKindBoolean, b.GetName(), data})
	}
	for _, m := range inv.Modules {
		data, err := macMarshal.Marshal(m)
		if err != nil {
			return fmt.Errorf("mac: marshal module %s: %w", m.GetName(), err)
		}
		rows = append(rows, row{"selinux", MacKindModule, m.GetName(), data})
	}
	for _, p := range inv.Profiles {
		data, err := macMarshal.Marshal(p)
		if err != nil {
			return fmt.Errorf("mac: marshal profile %s: %w", p.GetName(), err)
		}
		rows = append(rows, row{"apparmor", MacKindProfile, p.GetName(), data})
	}
	for start := 0; start < len(rows); start += macInsertBatch {
		end := min(start+macInsertBatch, len(rows))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO node_mac_items (node_id, lsm, kind, name, info_json, reported_at) VALUES `)
		args := make([]any, 0, (end-start)*6)
		for i, it := range rows[start:end] {
			if i > 0 {
				sb.WriteString(", ")
			}
			n := len(args)
			fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d)", n+1, n+2, n+3, n+4, n+5, n+6)
			args = append(args, nodeID, it.lsm, it.kind, it.name, string(it.data), now)
		}
		sb.WriteString(` ON CONFLICT (node_id, lsm, kind, name) DO UPDATE SET info_json = EXCLUDED.info_json, reported_at = EXCLUDED.reported_at`)
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("mac: insert node items: %w", err)
		}
	}
	return tx.Commit()
}

// GetNodeStatus returns the status row of a node, or nil when the node
// has not reported.
func (r *MacRepository) GetNodeStatus(ctx context.Context, nodeID string) (*MacNodeStatus, error) {
	var (
		st          MacNodeStatus
		info        []byte
		denialsJSON []byte
		since       sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT node_id, lsm, mode, info_json, denials_json, denials_since, reported_at
		FROM node_mac_status WHERE node_id = $1`, nodeID).
		Scan(&st.NodeID, &st.LSM, &st.Mode, &info, &denialsJSON, &since, &st.ReportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mac: get status: %w", err)
	}
	if since.Valid {
		t := since.Time
		st.DenialsSince = &t
	}
	switch st.LSM {
	case "selinux":
		st.Selinux = &pb.SelinuxStatus{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(info, st.Selinux); err != nil {
			return nil, fmt.Errorf("mac: decode selinux status: %w", err)
		}
	case "apparmor":
		st.Apparmor = &pb.ApparmorStatus{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(info, st.Apparmor); err != nil {
			return nil, fmt.Errorf("mac: decode apparmor status: %w", err)
		}
	}
	var raws []json.RawMessage
	if len(denialsJSON) > 0 {
		if err := json.Unmarshal(denialsJSON, &raws); err != nil {
			return nil, fmt.Errorf("mac: decode denials: %w", err)
		}
	}
	for _, raw := range raws {
		d := &pb.MacDenialGroup{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, d); err != nil {
			continue
		}
		st.Denials = append(st.Denials, d)
	}
	return &st, nil
}

// ListItems returns the union of the items every node reported for an
// LSM and kind, one row per name with the most recently reported data. q
// filters by a case-insensitive substring of the name or description.
func (r *MacRepository) ListItems(ctx context.Context, lsm, kind, q string, limit int) ([]MacCatalogueItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ON (i.name) i.kind, i.name, i.info_json, i.reported_at,
		       (SELECT COUNT(*) FROM node_mac_items c WHERE c.lsm = i.lsm AND c.kind = i.kind AND c.name = i.name) AS node_count
		FROM node_mac_items i
		WHERE i.lsm = $1 AND i.kind = $2
		  AND ($3 = '' OR i.name ILIKE '%' || $3 || '%' OR i.info_json->>'description' ILIKE '%' || $3 || '%' OR i.info_json->>'file' ILIKE '%' || $3 || '%')
		ORDER BY i.name, i.reported_at DESC
		LIMIT $4`, lsm, kind, q, limit)
	if err != nil {
		return nil, fmt.Errorf("mac: list items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMacItems(rows)
}

// ListItemsByNode returns the items one node reported for an LSM and kind.
func (r *MacRepository) ListItemsByNode(ctx context.Context, nodeID, lsm, kind, q string, limit int) ([]MacCatalogueItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT i.kind, i.name, i.info_json, i.reported_at, 1 AS node_count
		FROM node_mac_items i
		WHERE i.node_id = $1 AND i.lsm = $2 AND i.kind = $3
		  AND ($4 = '' OR i.name ILIKE '%' || $4 || '%' OR i.info_json->>'description' ILIKE '%' || $4 || '%' OR i.info_json->>'file' ILIKE '%' || $4 || '%')
		ORDER BY i.name
		LIMIT $5`, nodeID, lsm, kind, q, limit)
	if err != nil {
		return nil, fmt.Errorf("mac: list node items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMacItems(rows)
}

func scanMacItems(rows *sql.Rows) ([]MacCatalogueItem, error) {
	var out []MacCatalogueItem
	for rows.Next() {
		var it MacCatalogueItem
		var data []byte
		if err := rows.Scan(&it.Kind, &it.Name, &data, &it.ReportedAt, &it.NodeCount); err != nil {
			return nil, fmt.Errorf("mac: scan item: %w", err)
		}
		it.InfoJSON = json.RawMessage(data)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mac: iterate items: %w", err)
	}
	return out, nil
}
