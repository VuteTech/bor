// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// UserMFARow is the raw DB record for user_mfa.
type UserMFARow struct {
	UserID        string
	TOTPSecret    string // AES-256-GCM encrypted, base64 encoded
	TOTPAlgorithm string
	TOTPEnabled   bool
	BackupCodes   []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// MFARepository manages the user_mfa table.
type MFARepository struct {
	db *DB
}

// NewMFARepository creates a new MFARepository.
func NewMFARepository(db *DB) *MFARepository {
	return &MFARepository{db: db}
}

// GetByUserID returns the MFA row for the given user, or nil if not found.
func (r *MFARepository) GetByUserID(ctx context.Context, userID string) (*UserMFARow, error) {
	row := &UserMFARow{}
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id, totp_secret, totp_algorithm, totp_enabled, backup_codes, created_at, updated_at
		 FROM user_mfa WHERE user_id = $1`, userID,
	).Scan(&row.UserID, &row.TOTPSecret, &row.TOTPAlgorithm, &row.TOTPEnabled,
		pq.Array(&row.BackupCodes), &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user mfa: %w", err)
	}
	return row, nil
}

// Upsert inserts or replaces the MFA row for a user.
func (r *MFARepository) Upsert(ctx context.Context, row *UserMFARow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_mfa (user_id, totp_secret, totp_algorithm, totp_enabled, backup_codes, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
		   totp_secret    = EXCLUDED.totp_secret,
		   totp_algorithm = EXCLUDED.totp_algorithm,
		   totp_enabled   = EXCLUDED.totp_enabled,
		   backup_codes   = EXCLUDED.backup_codes,
		   updated_at     = NOW()`,
		row.UserID, row.TOTPSecret, row.TOTPAlgorithm, row.TOTPEnabled, pq.Array(row.BackupCodes),
	)
	return err
}

// Delete removes the MFA row for a user (disables MFA).
func (r *MFARepository) Delete(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_mfa WHERE user_id = $1`, userID)
	return err
}

// SetEnabled sets the totp_enabled flag and clears/sets backup codes.
func (r *MFARepository) SetEnabled(ctx context.Context, userID string, enabled bool, backupCodes []string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_mfa SET totp_enabled = $2, backup_codes = $3, updated_at = NOW()
		 WHERE user_id = $1`,
		userID, enabled, pq.Array(backupCodes),
	)
	return err
}
