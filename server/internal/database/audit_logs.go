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
	defer rows.Close()

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

// buildAuditLogFilter builds WHERE clause and args for audit log queries
func buildAuditLogFilter(req *models.AuditLogListRequest) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	if req.ResourceType != "" {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argIdx))
		args = append(args, req.ResourceType)
		argIdx++
	}
	if req.Action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, req.Action)
		argIdx++
	}
	if req.Username != "" {
		conditions = append(conditions, fmt.Sprintf("username ILIKE $%d", argIdx))
		args = append(args, "%"+req.Username+"%")
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	return where, args
}
