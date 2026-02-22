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

// NodeGroupRepository handles node_groups database operations
type NodeGroupRepository struct {
	db *DB
}

// NewNodeGroupRepository creates a new NodeGroupRepository
func NewNodeGroupRepository(db *DB) *NodeGroupRepository {
	return &NodeGroupRepository{db: db}
}

// Create inserts a new node group
func (r *NodeGroupRepository) Create(ctx context.Context, ng *models.NodeGroup) error {
	query := `INSERT INTO node_groups (name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4) RETURNING id`

	now := time.Now()
	ng.CreatedAt = now
	ng.UpdatedAt = now

	err := r.db.QueryRowContext(ctx, query, ng.Name, ng.Description, ng.CreatedAt, ng.UpdatedAt).Scan(&ng.ID)
	if err != nil {
		return fmt.Errorf("failed to create node group: %w", err)
	}
	return nil
}

// GetByID retrieves a node group by ID
func (r *NodeGroupRepository) GetByID(ctx context.Context, id string) (*models.NodeGroup, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM node_groups WHERE id = $1`
	ng := &models.NodeGroup{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(&ng.ID, &ng.Name, &ng.Description, &ng.CreatedAt, &ng.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node group: %w", err)
	}
	return ng, nil
}

// ListAll returns all node groups
func (r *NodeGroupRepository) ListAll(ctx context.Context) ([]*models.NodeGroup, error) {
	query := `SELECT id, name, description, created_at, updated_at FROM node_groups ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list node groups: %w", err)
	}
	defer rows.Close()

	var groups []*models.NodeGroup
	for rows.Next() {
		ng := &models.NodeGroup{}
		if err := rows.Scan(&ng.ID, &ng.Name, &ng.Description, &ng.CreatedAt, &ng.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan node group: %w", err)
		}
		groups = append(groups, ng)
	}
	return groups, rows.Err()
}

// Update updates a node group
func (r *NodeGroupRepository) Update(ctx context.Context, id string, req *models.UpdateNodeGroupRequest) error {
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
	query := fmt.Sprintf("UPDATE node_groups SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update node group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("node group not found")
	}
	return nil
}

// Delete removes a node group by ID
func (r *NodeGroupRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM node_groups WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete node group: %w", err)
	}
	return nil
}

// CountNodesByGroupID returns how many nodes belong to a given node group
func (r *NodeGroupRepository) CountNodesByGroupID(ctx context.Context, groupID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM node_group_members WHERE node_group_id = $1", groupID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count nodes by group: %w", err)
	}
	return count, nil
}
