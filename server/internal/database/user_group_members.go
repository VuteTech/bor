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

// UserGroupMemberRepository handles user_group_members database operations
type UserGroupMemberRepository struct {
	db *DB
}

// NewUserGroupMemberRepository creates a new UserGroupMemberRepository
func NewUserGroupMemberRepository(db *DB) *UserGroupMemberRepository {
	return &UserGroupMemberRepository{db: db}
}

// Create adds a user to a group
func (r *UserGroupMemberRepository) Create(ctx context.Context, m *models.UserGroupMember) error {
	query := `
		INSERT INTO user_group_members (group_id, user_id, created_at)
		VALUES ($1, $2, $3)
		RETURNING id`

	m.CreatedAt = time.Now()

	err := r.db.QueryRowContext(ctx, query, m.GroupID, m.UserID, m.CreatedAt).Scan(&m.ID)
	if err != nil {
		return fmt.Errorf("failed to add group member: %w", err)
	}

	return nil
}

// ListByGroupID returns all members for a group
func (r *UserGroupMemberRepository) ListByGroupID(ctx context.Context, groupID string) ([]*models.UserGroupMember, error) {
	query := `
		SELECT id, group_id, user_id, created_at
		FROM user_group_members WHERE group_id = $1
		ORDER BY created_at`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
	}
	defer rows.Close()

	var members []*models.UserGroupMember
	for rows.Next() {
		m := &models.UserGroupMember{}
		if err := rows.Scan(&m.ID, &m.GroupID, &m.UserID, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group member: %w", err)
		}
		members = append(members, m)
	}

	return members, rows.Err()
}

// Delete removes a member from a group
func (r *UserGroupMemberRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM user_group_members WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to remove group member: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("group member not found")
	}

	return nil
}
