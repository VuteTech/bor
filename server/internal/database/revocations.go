// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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

// ErrStaleCertificate is returned by RotateCertificate when the certificate
// presented for renewal is neither the node's current nor its previous one.
var ErrStaleCertificate = errors.New("certificate is not the node's current certificate")

// RotateCertificate records a renewed certificate for a node, in one
// transaction. presentedSerial is the certificate the node authenticated
// the renewal with:
//
//   - the current certificate: it becomes the previous one and stays valid
//     until the node connects with the new one (see ConfirmCertificateInUse);
//     an older previous certificate, if any, is revoked.
//   - the previous certificate: the node lost the answer to an earlier
//     renewal. The certificate issued then was never used and is revoked;
//     the previous certificate stays as it is.
//
// Any other certificate is refused with ErrStaleCertificate. It returns the
// serials it revoked.
func (r *RevocationRepository) RotateCertificate(ctx context.Context, nodeID, presentedSerial, newSerial string, notAfter time.Time) (revoked []string, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin certificate rotation: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var current sql.NullString
	var previous string
	err = tx.QueryRowContext(ctx,
		`SELECT cert_serial, previous_cert_serial FROM nodes WHERE id = $1 FOR UPDATE`,
		nodeID).Scan(&current, &previous)
	if err != nil {
		return nil, fmt.Errorf("load node certificate: %w", err)
	}

	newPrevious := previous
	switch {
	case current.Valid && current.String != "" && presentedSerial == current.String:
		if previous != "" {
			revoked = append(revoked, previous)
		}
		newPrevious = current.String
	case previous != "" && presentedSerial == previous:
		if current.Valid && current.String != "" {
			revoked = append(revoked, current.String)
		}
	default:
		return nil, ErrStaleCertificate
	}

	for _, serial := range revoked {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO revoked_certificates (node_id, serial, reason) VALUES ($1, $2, $3)`,
			nodeID, serial, "superseded by renewal"); err != nil {
			return nil, fmt.Errorf("revoke superseded certificate: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE nodes SET cert_serial = $1, cert_not_after = $2, previous_cert_serial = $3, updated_at = NOW() WHERE id = $4`,
		newSerial, notAfter, newPrevious, nodeID); err != nil {
		return nil, fmt.Errorf("update node certificate: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit certificate rotation: %w", err)
	}
	return revoked, nil
}

// ConfirmCertificateInUse is called when a node connects with
// presentedSerial. When that is the node's current certificate and a
// previous one is still valid, the previous one is revoked. It returns the
// revoked serial, or "" when nothing changed.
func (r *RevocationRepository) ConfirmCertificateInUse(ctx context.Context, nodeID, presentedSerial string) (revoked string, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin certificate confirmation: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var current sql.NullString
	var previous string
	err = tx.QueryRowContext(ctx,
		`SELECT cert_serial, previous_cert_serial FROM nodes WHERE id = $1 FOR UPDATE`,
		nodeID).Scan(&current, &previous)
	if err != nil {
		return "", fmt.Errorf("load node certificate: %w", err)
	}
	if previous == "" || !current.Valid || current.String != presentedSerial {
		err = tx.Rollback()
		return "", err
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO revoked_certificates (node_id, serial, reason) VALUES ($1, $2, $3)`,
		nodeID, previous, "superseded by renewal"); err != nil {
		return "", fmt.Errorf("revoke previous certificate: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE nodes SET previous_cert_serial = '', updated_at = NOW() WHERE id = $1`, nodeID); err != nil {
		return "", fmt.Errorf("clear previous certificate: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("commit certificate confirmation: %w", err)
	}
	return previous, nil
}

// RevokeNode revokes every certificate a node holds (current and previous)
// in one transaction and returns the revoked serials.
func (r *RevocationRepository) RevokeNode(ctx context.Context, nodeID, reason string) (revoked []string, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin node revocation: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var current sql.NullString
	var previous string
	err = tx.QueryRowContext(ctx,
		`SELECT cert_serial, previous_cert_serial FROM nodes WHERE id = $1 FOR UPDATE`,
		nodeID).Scan(&current, &previous)
	if err != nil {
		return nil, fmt.Errorf("load node certificate: %w", err)
	}
	if current.Valid && current.String != "" {
		revoked = append(revoked, current.String)
	}
	if previous != "" {
		revoked = append(revoked, previous)
	}
	for _, serial := range revoked {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO revoked_certificates (node_id, serial, reason) VALUES ($1, $2, $3)`,
			nodeID, serial, reason); err != nil {
			return nil, fmt.Errorf("record revocation: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE nodes SET previous_cert_serial = '', updated_at = NOW() WHERE id = $1`, nodeID); err != nil {
		return nil, fmt.Errorf("clear previous certificate: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit node revocation: %w", err)
	}
	return revoked, nil
}
