// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/models"
)

// RevocationRepository handles certificate revocation database operations.
type RevocationRepository struct {
	db *DB
}

// NewRevocationRepository creates a new RevocationRepository.
func NewRevocationRepository(db *DB) *RevocationRepository {
	return &RevocationRepository{db: db}
}

// Revoke records a new certificate revocation.
func (r *RevocationRepository) Revoke(ctx context.Context, nodeID, serial, reason string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO revoked_certificates (node_id, serial, reason) VALUES ($1, $2, $3)`,
		nodeID, serial, reason)
	if err != nil {
		return fmt.Errorf("failed to record revocation: %w", err)
	}
	return nil
}

// IsRevoked returns true if the given serial number has been revoked.
func (r *RevocationRepository) IsRevoked(ctx context.Context, serial string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM revoked_certificates WHERE serial = $1`, serial).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check revocation: %w", err)
	}
	return count > 0, nil
}

// GetByNodeID returns the most recent revocation for a node, if any.
func (r *RevocationRepository) GetByNodeID(ctx context.Context, nodeID string) (*models.RevokedCertificate, error) {
	var rev models.RevokedCertificate
	err := r.db.QueryRowContext(ctx,
		`SELECT id, node_id, serial, revoked_at, reason FROM revoked_certificates WHERE node_id = $1 ORDER BY revoked_at DESC LIMIT 1`,
		nodeID).Scan(&rev.ID, &rev.NodeID, &rev.Serial, &rev.RevokedAt, &rev.Reason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get revocation by node ID: %w", err)
	}
	return &rev, nil
}

// DeleteByNodeID removes all revocations for a node (used when re-enrolling).
func (r *RevocationRepository) DeleteByNodeID(ctx context.Context, nodeID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM revoked_certificates WHERE node_id = $1`, nodeID)
	if err != nil {
		return fmt.Errorf("failed to delete revocations for node: %w", err)
	}
	return nil
}

// ListAll returns all revoked certificates ordered by revoked_at descending.
func (r *RevocationRepository) ListAll(ctx context.Context) ([]*models.RevokedCertificate, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, node_id, serial, revoked_at, reason FROM revoked_certificates ORDER BY revoked_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list revocations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var revs []*models.RevokedCertificate
	for rows.Next() {
		rev := &models.RevokedCertificate{}
		if err := rows.Scan(&rev.ID, &rev.NodeID, &rev.Serial, &rev.RevokedAt, &rev.Reason); err != nil {
			return nil, fmt.Errorf("failed to scan revocation: %w", err)
		}
		revs = append(revs, rev)
	}
	return revs, rows.Err()
}
