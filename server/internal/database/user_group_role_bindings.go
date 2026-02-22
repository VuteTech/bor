// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
)

// UserGroupRoleBindingRepository handles user_group_role_bindings database operations
type UserGroupRoleBindingRepository struct {
	db *DB
}

// NewUserGroupRoleBindingRepository creates a new UserGroupRoleBindingRepository
func NewUserGroupRoleBindingRepository(db *DB) *UserGroupRoleBindingRepository {
	return &UserGroupRoleBindingRepository{db: db}
}

// Create inserts a new group role binding
func (r *UserGroupRoleBindingRepository) Create(ctx context.Context, b *models.UserGroupRoleBinding) error {
	query := `
		INSERT INTO user_group_role_bindings (group_id, role_id, scope_type, scope_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	b.CreatedAt = time.Now()

	err := r.db.QueryRowContext(ctx, query,
		b.GroupID, b.RoleID, b.ScopeType, b.ScopeID, b.CreatedAt,
	).Scan(&b.ID)
	if err != nil {
		return fmt.Errorf("failed to create group role binding: %w", err)
	}

	return nil
}

// ListByGroupID returns all role bindings for a group
func (r *UserGroupRoleBindingRepository) ListByGroupID(ctx context.Context, groupID string) ([]*models.UserGroupRoleBinding, error) {
	query := `
		SELECT id, group_id, role_id, scope_type, scope_id, created_at
		FROM user_group_role_bindings WHERE group_id = $1
		ORDER BY created_at`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to list group role bindings: %w", err)
	}
	defer rows.Close()

	var bindings []*models.UserGroupRoleBinding
	for rows.Next() {
		b := &models.UserGroupRoleBinding{}
		if err := rows.Scan(&b.ID, &b.GroupID, &b.RoleID, &b.ScopeType, &b.ScopeID, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group role binding: %w", err)
		}
		bindings = append(bindings, b)
	}

	return bindings, rows.Err()
}

// Delete removes a group role binding
func (r *UserGroupRoleBindingRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM user_group_role_bindings WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete group role binding: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("group role binding not found")
	}

	return nil
}
