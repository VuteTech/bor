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

// PermissionRepository handles permission database operations
type PermissionRepository struct {
	db *DB
}

// NewPermissionRepository creates a new PermissionRepository
func NewPermissionRepository(db *DB) *PermissionRepository {
	return &PermissionRepository{db: db}
}

// GetByResourceAction retrieves a permission by resource and action
func (r *PermissionRepository) GetByResourceAction(ctx context.Context, resource, action string) (*models.Permission, error) {
	query := `SELECT id, resource, action FROM permissions WHERE resource = $1 AND action = $2`

	perm := &models.Permission{}
	err := r.db.QueryRowContext(ctx, query, resource, action).Scan(&perm.ID, &perm.Resource, &perm.Action)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get permission: %w", err)
	}

	return perm, nil
}

// List returns all permissions
func (r *PermissionRepository) List(ctx context.Context) ([]*models.Permission, error) {
	query := `SELECT id, resource, action FROM permissions ORDER BY resource, action`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}
	defer rows.Close()

	var perms []*models.Permission
	for rows.Next() {
		p := &models.Permission{}
		if err := rows.Scan(&p.ID, &p.Resource, &p.Action); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		perms = append(perms, p)
	}

	return perms, rows.Err()
}
