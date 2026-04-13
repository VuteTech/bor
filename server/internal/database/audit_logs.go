// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
)

// AuditLogRepository handles audit log database operations
type AuditLogRepository struct {
	db *DB
}

// NewAuditLogRepository creates a new AuditLogRepository
func NewAuditLogRepository(db *DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

// Create inserts a new audit log entry
func (r *AuditLogRepository) Create(ctx context.Context, entry *models.AuditLog) error {
	query := `INSERT INTO audit_logs (user_id, username, action, resource_type, resource_id, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`

	entry.CreatedAt = time.Now()

	err := r.db.QueryRowContext(ctx, query,
		entry.UserID, entry.Username, entry.Action, entry.ResourceType,
		entry.ResourceID, entry.Details, entry.IPAddress, entry.CreatedAt,
	).Scan(&entry.ID)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}

	return nil
}

// DeleteOlderThan removes audit log entries with a timestamp before the given cutoff.
// It returns the number of deleted rows.
func (r *AuditLogRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, "DELETE FROM audit_logs WHERE created_at < $1", cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old audit logs: %w", err)
	}
	return result.RowsAffected()
}

// List retrieves audit logs with pagination and optional filters
func (r *AuditLogRepository) List(ctx context.Context, req *models.AuditLogListRequest) ([]*models.AuditLog, error) {
	where, args := buildAuditLogFilter(req)

	query := fmt.Sprintf(`SELECT id, user_id, username, action, resource_type, resource_id, details, ip_address, created_at
		FROM audit_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, len(args)+1, len(args)+2)

	limit := req.PerPage
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var logs []*models.AuditLog
	for rows.Next() {
		entry := &models.AuditLog{}
		if err := rows.Scan(
			&entry.ID, &entry.UserID, &entry.Username, &entry.Action,
			&entry.ResourceType, &entry.ResourceID, &entry.Details,
			&entry.IPAddress, &entry.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, entry)
	}

	return logs, rows.Err()
}

// Count returns the total number of audit logs matching filters
func (r *AuditLogRepository) Count(ctx context.Context, req *models.AuditLogListRequest) (int, error) {
	where, args := buildAuditLogFilter(req)

	query := fmt.Sprintf(`SELECT COUNT(*) FROM audit_logs %s`, where)

	var count int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	return count, nil
}

// CountByAction returns the total number of audit log entries grouped by action.
func (r *AuditLogRepository) CountByAction(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT action, COUNT(*) FROM audit_logs GROUP BY action`)
	if err != nil {
		return nil, fmt.Errorf("failed to count audit logs by action: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, fmt.Errorf("failed to scan audit log count: %w", err)
		}
		counts[action] = count
	}
	return counts, rows.Err()
}

func buildAuditLogFilter(req *models.AuditLogListRequest) (clause string, args []interface{}) {
	var conditions []string
	argIdx := 1

	if len(req.ResourceTypes) > 0 {
		placeholders := make([]string, len(req.ResourceTypes))
		for i, v := range req.ResourceTypes {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, v)
			argIdx++
		}
		conditions = append(conditions, "resource_type IN ("+strings.Join(placeholders, ", ")+")")
	}
	if len(req.Actions) > 0 {
		placeholders := make([]string, len(req.Actions))
		for i, v := range req.Actions {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, v)
			argIdx++
		}
		conditions = append(conditions, "action IN ("+strings.Join(placeholders, ", ")+")")
	}
	if req.Username != "" {
		conditions = append(conditions, fmt.Sprintf("username ILIKE $%d", argIdx))
		args = append(args, "%"+req.Username+"%")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	return where, args
}
