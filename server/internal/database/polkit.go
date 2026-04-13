// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"fmt"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// PolkitRepository handles polkit action catalogue database operations.
type PolkitRepository struct {
	db *DB
}

// NewPolkitRepository creates a new PolkitRepository.
func NewPolkitRepository(db *DB) *PolkitRepository {
	return &PolkitRepository{db: db}
}

// UpsertAction inserts or updates a single polkit action description.
// source should be "builtin" or "agent".
func (r *PolkitRepository) UpsertAction(ctx context.Context, action *pb.PolkitActionDescription, source string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO polkit_actions
		  (action_id, description, message, vendor,
		   default_any, default_inactive, default_active, source, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (action_id) DO UPDATE
		  SET description      = EXCLUDED.description,
		      message          = EXCLUDED.message,
		      vendor           = EXCLUDED.vendor,
		      default_any      = EXCLUDED.default_any,
		      default_inactive = EXCLUDED.default_inactive,
		      default_active   = EXCLUDED.default_active,
		      source           = CASE WHEN polkit_actions.source = 'builtin'
		                              THEN 'builtin'
		                              ELSE EXCLUDED.source END,
		      updated_at       = EXCLUDED.updated_at`,
		action.GetActionId(),
		nullableString(action.GetDescription()),
		nullableString(action.GetMessage()),
		nullableString(action.GetVendor()),
		nullableString(action.GetDefaultAny()),
		nullableString(action.GetDefaultInactive()),
		nullableString(action.GetDefaultActive()),
		source,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("polkit: upsert action %s: %w", action.GetActionId(), err)
	}
	return nil
}

// ReplaceNodeActions replaces the full set of polkit action IDs for a node.
func (r *PolkitRepository) ReplaceNodeActions(ctx context.Context, nodeID string, actionIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("polkit: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_polkit_actions WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("polkit: delete node actions: %w", err)
	}

	for _, id := range actionIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO node_polkit_actions (node_id, action_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			nodeID, id,
		); err != nil {
			return fmt.Errorf("polkit: insert node action %s: %w", id, err)
		}
	}

	return tx.Commit()
}

// ListActions returns all known polkit actions (for the UI action picker).
func (r *PolkitRepository) ListActions(ctx context.Context) ([]*pb.PolkitActionDescription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT action_id, description, message, vendor,
		       default_any, default_inactive, default_active
		FROM polkit_actions
		ORDER BY action_id`)
	if err != nil {
		return nil, fmt.Errorf("polkit: list actions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanActions(rows)
}

// ListActionsByNode returns polkit action descriptions available on a specific node.
func (r *PolkitRepository) ListActionsByNode(ctx context.Context, nodeID string) ([]*pb.PolkitActionDescription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pa.action_id, pa.description, pa.message, pa.vendor,
		       pa.default_any, pa.default_inactive, pa.default_active
		FROM polkit_actions pa
		JOIN node_polkit_actions npa ON npa.action_id = pa.action_id
		WHERE npa.node_id = $1
		ORDER BY pa.action_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("polkit: list actions by node: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanActions(rows)
}

func scanActions(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*pb.PolkitActionDescription, error) {
	var actions []*pb.PolkitActionDescription
	for rows.Next() {
		var a pb.PolkitActionDescription
		var description, message, vendor, defaultAny, defaultInactive, defaultActive *string
		if err := rows.Scan(
			&a.ActionId,
			&description, &message, &vendor,
			&defaultAny, &defaultInactive, &defaultActive,
		); err != nil {
			return nil, fmt.Errorf("polkit: scan action: %w", err)
		}
		if description != nil {
			a.Description = *description
		}
		if message != nil {
			a.Message = *message
		}
		if vendor != nil {
			a.Vendor = *vendor
		}
		if defaultAny != nil {
			a.DefaultAny = *defaultAny
		}
		if defaultInactive != nil {
			a.DefaultInactive = *defaultInactive
		}
		if defaultActive != nil {
			a.DefaultActive = *defaultActive
		}
		actions = append(actions, &a)
	}
	return actions, rows.Err()
}
