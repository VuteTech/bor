// SPDX-License-Identifier: LGPL-3.0-or-later
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

// UserRoleBindingRepository handles user_role_bindings database operations
type UserRoleBindingRepository struct {
	db *DB
}

// NewUserRoleBindingRepository creates a new UserRoleBindingRepository
func NewUserRoleBindingRepository(db *DB) *UserRoleBindingRepository {
	return &UserRoleBindingRepository{db: db}
}

// Create inserts a new user role binding
func (r *UserRoleBindingRepository) Create(ctx context.Context, binding *models.UserRoleBinding) error {
	query := `
		INSERT INTO user_role_bindings (user_id, role_id, scope_type, scope_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	binding.CreatedAt = time.Now()

	err := r.db.QueryRowContext(ctx, query,
		binding.UserID, binding.RoleID, binding.ScopeType, binding.ScopeID, binding.CreatedAt,
	).Scan(&binding.ID)
	if err != nil {
		return fmt.Errorf("failed to create user role binding: %w", err)
	}

	return nil
}

// ListByUserID returns all role bindings for a user
func (r *UserRoleBindingRepository) ListByUserID(ctx context.Context, userID string) ([]*models.UserRoleBinding, error) {
	query := `
		SELECT id, user_id, role_id, scope_type, scope_id, created_at
		FROM user_role_bindings WHERE user_id = $1`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list user role bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bindings []*models.UserRoleBinding
	for rows.Next() {
		b := &models.UserRoleBinding{}
		if err := rows.Scan(&b.ID, &b.UserID, &b.RoleID, &b.ScopeType, &b.ScopeID, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan user role binding: %w", err)
		}
		bindings = append(bindings, b)
	}

	return bindings, rows.Err()
}

// Delete removes a user role binding
func (r *UserRoleBindingRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM user_role_bindings WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user role binding: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user role binding not found")
	}

	return nil
}

// DeleteByUserID removes all role bindings for a user
func (r *UserRoleBindingRepository) DeleteByUserID(ctx context.Context, userID string) error {
	query := `DELETE FROM user_role_bindings WHERE user_id = $1`

	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user role bindings: %w", err)
	}

	return nil
}

// GetByID returns a single role binding by id, or nil if it does not exist.
func (r *UserRoleBindingRepository) GetByID(ctx context.Context, id string) (*models.UserRoleBinding, error) {
	b := &models.UserRoleBinding{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, role_id, scope_type, scope_id, created_at
		 FROM user_role_bindings WHERE id = $1`, id).
		Scan(&b.ID, &b.UserID, &b.RoleID, &b.ScopeType, &b.ScopeID, &b.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get role binding: %w", err)
	}
	return b, nil
}

// CountUsersWithRole returns the number of distinct users bound to the given role.
func (r *UserRoleBindingRepository) CountUsersWithRole(ctx context.Context, roleID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM user_role_bindings WHERE role_id = $1`, roleID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users with role: %w", err)
	}
	return count, nil
}
