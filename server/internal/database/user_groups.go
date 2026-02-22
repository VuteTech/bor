// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
)

// UserGroupRepository handles user_groups database operations (identity domain)
type UserGroupRepository struct {
	db *DB
}

// NewUserGroupRepository creates a new UserGroupRepository
func NewUserGroupRepository(db *DB) *UserGroupRepository {
	return &UserGroupRepository{db: db}
}

// Create inserts a new user group
func (r *UserGroupRepository) Create(ctx context.Context, ug *models.UserGroup) error {
	query := `INSERT INTO user_groups (name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4) RETURNING id`

	now := time.Now()
	ug.CreatedAt = now
	ug.UpdatedAt = now

	err := r.db.QueryRowContext(ctx, query, ug.Name, ug.Description, ug.CreatedAt, ug.UpdatedAt).Scan(&ug.ID)
	if err != nil {
		return fmt.Errorf("failed to create user group: %w", err)
	}
	return nil
}

// GetByID retrieves a user group by ID
func (r *UserGroupRepository) GetByID(ctx context.Context, id string) (*models.UserGroup, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM user_groups WHERE id = $1`
	ug := &models.UserGroup{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(&ug.ID, &ug.Name, &ug.Description, &ug.CreatedAt, &ug.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user group: %w", err)
	}
	return ug, nil
}

// ListAll returns all user groups
func (r *UserGroupRepository) ListAll(ctx context.Context) ([]*models.UserGroup, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM user_groups ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list user groups: %w", err)
	}
	defer rows.Close()

	var groups []*models.UserGroup
	for rows.Next() {
		ug := &models.UserGroup{}
		if err := rows.Scan(&ug.ID, &ug.Name, &ug.Description, &ug.CreatedAt, &ug.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan user group: %w", err)
		}
		groups = append(groups, ug)
	}
	return groups, rows.Err()
}

// Update updates a user group
func (r *UserGroupRepository) Update(ctx context.Context, id string, req *models.UpdateUserGroupRequest) error {
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *req.Description)
		argIdx++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, id)
	query := fmt.Sprintf("UPDATE user_groups SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update user group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user group not found")
	}
	return nil
}

// Delete removes a user group by ID
func (r *UserGroupRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM user_groups WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete user group: %w", err)
	}
	return nil
}
