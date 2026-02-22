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

// RoleRepository handles role database operations
type RoleRepository struct {
	db *DB
}

// NewRoleRepository creates a new RoleRepository
func NewRoleRepository(db *DB) *RoleRepository {
	return &RoleRepository{db: db}
}

// GetByID retrieves a role by ID
func (r *RoleRepository) GetByID(ctx context.Context, id string) (*models.Role, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM roles WHERE id = $1`

	role := &models.Role{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&role.ID, &role.Name, &role.Description, &role.CreatedAt, &role.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get role by id: %w", err)
	}

	return role, nil
}

// GetByName retrieves a role by name
func (r *RoleRepository) GetByName(ctx context.Context, name string) (*models.Role, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM roles WHERE name = $1`

	role := &models.Role{}
	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&role.ID, &role.Name, &role.Description, &role.CreatedAt, &role.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get role by name: %w", err)
	}

	return role, nil
}

// List returns all roles
func (r *RoleRepository) List(ctx context.Context) ([]*models.Role, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM roles ORDER BY name`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	defer rows.Close()

	var roles []*models.Role
	for rows.Next() {
		role := &models.Role{}
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan role: %w", err)
		}
		roles = append(roles, role)
	}

	return roles, rows.Err()
}

// Create inserts a new role
func (r *RoleRepository) Create(ctx context.Context, role *models.Role) error {
	query := `INSERT INTO roles (name, description, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id`

	now := time.Now()
	role.CreatedAt = now
	role.UpdatedAt = now

	err := r.db.QueryRowContext(ctx, query, role.Name, role.Description, role.CreatedAt, role.UpdatedAt).Scan(&role.ID)
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}

	return nil
}

// GetPermissionsByRoleID returns all permissions for a role
func (r *RoleRepository) GetPermissionsByRoleID(ctx context.Context, roleID string) ([]*models.Permission, error) {
	query := `
		SELECT p.id, p.resource, p.action
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		WHERE rp.role_id = $1`

	rows, err := r.db.QueryContext(ctx, query, roleID)
	if err != nil {
		return nil, fmt.Errorf("failed to get permissions for role: %w", err)
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

// Update updates a role's name and/or description
func (r *RoleRepository) Update(ctx context.Context, id string, req *models.UpdateRoleRequest) error {
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

	query := fmt.Sprintf("UPDATE roles SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update role: %w", err)
	}
	return nil
}

// Delete removes a role by ID
func (r *RoleRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM roles WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("role not found")
	}

	return nil
}

// SetPermissions replaces all permissions for a role
func (r *RoleRepository) SetPermissions(ctx context.Context, roleID string, permissionIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing permissions
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return fmt.Errorf("failed to clear role permissions: %w", err)
	}

	// Insert new permissions
	for _, permID := range permissionIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)`,
			roleID, permID); err != nil {
			return fmt.Errorf("failed to add permission %s: %w", permID, err)
		}
	}

	return tx.Commit()
}
