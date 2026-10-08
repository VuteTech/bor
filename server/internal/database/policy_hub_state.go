// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"
)

// PolicyHubStateRepository persists the PolicyHub revision high-water mark so
// revision numbers stay monotonic across server restarts. It implements
// grpc.RevisionStore.
type PolicyHubStateRepository struct {
	db *DB
}

// NewPolicyHubStateRepository creates a new PolicyHubStateRepository.
func NewPolicyHubStateRepository(db *DB) *PolicyHubStateRepository {
	return &PolicyHubStateRepository{db: db}
}

// LoadRevision returns the persisted revision high-water mark, or 0 when no
// state has been recorded yet.
func (r *PolicyHubStateRepository) LoadRevision(ctx context.Context) (int64, error) {
	var revision int64
	err := r.db.QueryRowContext(ctx,
		`SELECT revision FROM policy_hub_state WHERE id = 1`).Scan(&revision)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to load policy hub revision: %w", err)
	}
	return revision, nil
}

// SaveRevision records a new revision high-water mark. GREATEST keeps the
// stored value monotonic even when concurrent publishers persist out of order.
func (r *PolicyHubStateRepository) SaveRevision(ctx context.Context, revision int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO policy_hub_state (id, revision) VALUES (1, $1)
		 ON CONFLICT (id) DO UPDATE SET revision = GREATEST(policy_hub_state.revision, EXCLUDED.revision)`,
		revision)
	if err != nil {
		return fmt.Errorf("failed to save policy hub revision: %w", err)
	}
	return nil
}
