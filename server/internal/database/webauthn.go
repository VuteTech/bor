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

// WebAuthnCredentialRow is the raw DB record for user_webauthn_credentials.
type WebAuthnCredentialRow struct {
	ID             string
	UserID         string
	CredentialID   string
	PublicKey      []byte
	AAGUID         string
	SignCount      uint32
	Name           string
	Transports     []string
	BackupEligible bool
	BackupState    bool
	CreatedAt      time.Time
	LastUsedAt     *time.Time
}

// WebAuthnSessionRow is the raw DB record for webauthn_sessions.
type WebAuthnSessionRow struct {
	ID          string
	UserID      string
	SessionType string
	SessionData string
	ExpiresAt   time.Time
}

// WebAuthnRepository manages WebAuthn credential and session tables.
type WebAuthnRepository struct{ db *DB }

// NewWebAuthnRepository creates a new WebAuthnRepository.
func NewWebAuthnRepository(db *DB) *WebAuthnRepository { return &WebAuthnRepository{db: db} }

// ListByUserID returns all WebAuthn credentials for a user.
func (r *WebAuthnRepository) ListByUserID(ctx context.Context, userID string) ([]*WebAuthnCredentialRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, credential_id, public_key, aaguid, sign_count, name, transports,
		        backup_eligible, backup_state, created_at, last_used_at
		 FROM user_webauthn_credentials WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list webauthn credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*WebAuthnCredentialRow
	for rows.Next() {
		row := &WebAuthnCredentialRow{}
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.CredentialID, &row.PublicKey, &row.AAGUID,
			&row.SignCount, &row.Name, pq.Array(&row.Transports),
			&row.BackupEligible, &row.BackupState, &row.CreatedAt, &row.LastUsedAt,
		); err != nil {
			return nil, fmt.Errorf("scan webauthn credential: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// GetByCredentialID returns a WebAuthn credential by its credential ID.
func (r *WebAuthnRepository) GetByCredentialID(ctx context.Context, credentialID string) (*WebAuthnCredentialRow, error) {
	row := &WebAuthnCredentialRow{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, credential_id, public_key, aaguid, sign_count, name, transports,
		        backup_eligible, backup_state, created_at, last_used_at
		 FROM user_webauthn_credentials WHERE credential_id = $1`, credentialID,
	).Scan(&row.ID, &row.UserID, &row.CredentialID, &row.PublicKey, &row.AAGUID,
		&row.SignCount, &row.Name, pq.Array(&row.Transports),
		&row.BackupEligible, &row.BackupState, &row.CreatedAt, &row.LastUsedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get webauthn credential: %w", err)
	}
	return row, nil
}

// Create inserts a new WebAuthn credential.
func (r *WebAuthnRepository) Create(ctx context.Context, row *WebAuthnCredentialRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_webauthn_credentials
		        (user_id, credential_id, public_key, aaguid, sign_count, name, transports, backup_eligible, backup_state)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		row.UserID, row.CredentialID, row.PublicKey, row.AAGUID, row.SignCount, row.Name,
		pq.Array(row.Transports), row.BackupEligible, row.BackupState,
	)
	if err != nil {
		return fmt.Errorf("create webauthn credential: %w", err)
	}
	return nil
}

// UpdateAfterLogin updates the sign count, backup state, and last_used_at after a successful authentication.
func (r *WebAuthnRepository) UpdateAfterLogin(ctx context.Context, credentialID string, signCount uint32, backupState bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_webauthn_credentials
		 SET sign_count = $2, backup_state = $3, last_used_at = NOW()
		 WHERE credential_id = $1`,
		credentialID, signCount, backupState,
	)
	if err != nil {
		return fmt.Errorf("update webauthn credential after login: %w", err)
	}
	return nil
}

// Rename updates the name of a credential (scoped to userID for safety).
func (r *WebAuthnRepository) Rename(ctx context.Context, id, userID, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_webauthn_credentials SET name = $3 WHERE id = $1 AND user_id = $2`,
		id, userID, name,
	)
	if err != nil {
		return fmt.Errorf("rename webauthn credential: %w", err)
	}
	return nil
}

// Delete removes a credential (scoped to userID for safety).
func (r *WebAuthnRepository) Delete(ctx context.Context, id, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM user_webauthn_credentials WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete webauthn credential: %w", err)
	}
	return nil
}

// CreateSession inserts a new WebAuthn session.
func (r *WebAuthnRepository) CreateSession(ctx context.Context, row *WebAuthnSessionRow) error {
	// Delete any pre-existing session of the same type for this user first.
	_, _ = r.db.ExecContext(ctx,
		`DELETE FROM webauthn_sessions WHERE user_id = $1 AND session_type = $2`,
		row.UserID, row.SessionType,
	)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO webauthn_sessions (user_id, session_type, session_data, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		row.UserID, row.SessionType, row.SessionData, row.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create webauthn session: %w", err)
	}
	return nil
}

// GetSession returns the most recent non-expired session for a user and type.
func (r *WebAuthnRepository) GetSession(ctx context.Context, userID, sessionType string) (*WebAuthnSessionRow, error) {
	row := &WebAuthnSessionRow{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, session_type, session_data, expires_at
		 FROM webauthn_sessions
		 WHERE user_id = $1 AND session_type = $2 AND expires_at > NOW()
		 ORDER BY expires_at DESC LIMIT 1`,
		userID, sessionType,
	).Scan(&row.ID, &row.UserID, &row.SessionType, &row.SessionData, &row.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get webauthn session: %w", err)
	}
	return row, nil
}

// DeleteSession removes all sessions of the given type for a user.
func (r *WebAuthnRepository) DeleteSession(ctx context.Context, userID, sessionType string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM webauthn_sessions WHERE user_id = $1 AND session_type = $2`,
		userID, sessionType,
	)
	if err != nil {
		return fmt.Errorf("delete webauthn session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes all expired WebAuthn sessions.
func (r *WebAuthnRepository) DeleteExpiredSessions(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE expires_at <= NOW()`)
	if err != nil {
		return fmt.Errorf("delete expired webauthn sessions: %w", err)
	}
	return nil
}
