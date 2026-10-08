// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EnrollmentTokenRepository persists pending enrollment tokens so they
// survive a server restart. Only SHA-256 hashes of tokens are stored; the
// plaintext token never reaches the database.
type EnrollmentTokenRepository struct {
	db *DB
}

// NewEnrollmentTokenRepository creates a new EnrollmentTokenRepository.
func NewEnrollmentTokenRepository(db *DB) *EnrollmentTokenRepository {
	return &EnrollmentTokenRepository{db: db}
}

// Create stores a new pending enrollment token hash. Expired rows are pruned
// opportunistically here: tokens are rare and short-lived, so the table stays
// tiny without a background sweeper.
func (r *EnrollmentTokenRepository) Create(ctx context.Context, tokenHash, nodeGroupID string, expiresAt time.Time) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM enrollment_tokens WHERE expires_at < $1`, time.Now().UTC()); err != nil {
		return fmt.Errorf("failed to prune expired enrollment tokens: %w", err)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (token_hash, node_group_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, nodeGroupID, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("failed to store enrollment token: %w", err)
	}
	return nil
}

// Consume atomically deletes the token row and returns its node group ID and
// expiry. found is false when no such token exists (unknown or already
// consumed). The caller checks the expiry; an expired row is dead either way.
func (r *EnrollmentTokenRepository) Consume(ctx context.Context, tokenHash string) (nodeGroupID string, expiresAt time.Time, found bool, err error) {
	err = r.db.QueryRowContext(ctx,
		`DELETE FROM enrollment_tokens WHERE token_hash = $1 RETURNING node_group_id, expires_at`,
		tokenHash).Scan(&nodeGroupID, &expiresAt)
	if err == sql.ErrNoRows {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("failed to consume enrollment token: %w", err)
	}
	return nodeGroupID, expiresAt, true, nil
}
