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

// PolicyBindingRepository handles policy_bindings database operations
type PolicyBindingRepository struct {
	db *DB
}

// NewPolicyBindingRepository creates a new PolicyBindingRepository
func NewPolicyBindingRepository(db *DB) *PolicyBindingRepository {
	return &PolicyBindingRepository{db: db}
}

// Create inserts a new policy binding
func (r *PolicyBindingRepository) Create(ctx context.Context, b *models.PolicyBinding) error {
	query := `INSERT INTO policy_bindings (policy_id, group_id, state, priority, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`

	now := time.Now()
	b.CreatedAt = now
	b.UpdatedAt = now

	if b.State == "" {
		b.State = models.BindingStateDisabled
	}

	err := r.db.QueryRowContext(ctx, query, b.PolicyID, b.GroupID, b.State, b.Priority, b.CreatedAt, b.UpdatedAt).Scan(&b.ID)
	if err != nil {
		return fmt.Errorf("failed to create policy binding: %w", err)
	}
	return nil
}

// GetByID retrieves a policy binding by ID
func (r *PolicyBindingRepository) GetByID(ctx context.Context, id string) (*models.PolicyBinding, error) {
	query := `SELECT id, policy_id, group_id, state, priority, created_at, updated_at
		FROM policy_bindings WHERE id = $1`
	b := &models.PolicyBinding{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(&b.ID, &b.PolicyID, &b.GroupID, &b.State, &b.Priority, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy binding: %w", err)
	}
	return b, nil
}

// ListAll returns all policy bindings with related policy and group details
func (r *PolicyBindingRepository) ListAll(ctx context.Context) ([]*models.PolicyBindingWithDetails, error) {
	query := `SELECT pb.id, pb.policy_id, pb.group_id, pb.state, pb.priority, pb.created_at, pb.updated_at,
			p.name AS policy_name, p.status AS policy_state,
			ng.name AS group_name,
			(SELECT COUNT(*) FROM node_group_members ngm WHERE ngm.node_group_id = ng.id) AS node_count
		FROM policy_bindings pb
		JOIN policies p ON p.id = pb.policy_id
		JOIN node_groups ng ON ng.id = pb.group_id
		ORDER BY pb.priority DESC, p.name, pb.id`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list policy bindings: %w", err)
	}
	defer rows.Close()

	var bindings []*models.PolicyBindingWithDetails
	for rows.Next() {
		b := &models.PolicyBindingWithDetails{}
		if err := rows.Scan(&b.ID, &b.PolicyID, &b.GroupID, &b.State, &b.Priority,
			&b.CreatedAt, &b.UpdatedAt, &b.PolicyName, &b.PolicyState, &b.GroupName, &b.NodeCount); err != nil {
			return nil, fmt.Errorf("failed to scan policy binding: %w", err)
		}
		bindings = append(bindings, b)
	}
	return bindings, rows.Err()
}

// Update updates a policy binding
func (r *PolicyBindingRepository) Update(ctx context.Context, id string, req *models.UpdatePolicyBindingRequest) error {
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.State != nil {
		setClauses = append(setClauses, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *req.State)
		argIdx++
	}
	if req.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, id)
	query := fmt.Sprintf("UPDATE policy_bindings SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update policy binding: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("policy binding not found")
	}
	return nil
}

// Delete removes a policy binding by ID
func (r *PolicyBindingRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM policy_bindings WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete policy binding: %w", err)
	}
	return nil
}

// CountEnabledByPolicyID returns the count of enabled bindings for a given policy
func (r *PolicyBindingRepository) CountEnabledByPolicyID(ctx context.Context, policyID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM policy_bindings WHERE policy_id = $1 AND state = 'enabled'", policyID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count enabled bindings: %w", err)
	}
	return count, nil
}

// ListPoliciesByGroupID returns released policies with enabled bindings for a given node group
func (r *PolicyBindingRepository) ListPoliciesByGroupID(ctx context.Context, groupID string) ([]*models.Policy, error) {
	query := `SELECT p.id, p.name, p.description, p.type, p.content, p.version, p.status,
			p.deprecated_at, p.deprecation_message, p.replacement_policy_id,
			p.created_by, p.created_at, p.updated_at
		FROM policies p
		JOIN policy_bindings pb ON pb.policy_id = p.id
		WHERE pb.group_id = $1
		  AND pb.state = 'enabled'
		  AND p.status = 'released'
		ORDER BY pb.priority DESC, p.name, pb.id`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies by group: %w", err)
	}
	defer rows.Close()

	var policies []*models.Policy
	for rows.Next() {
		p := &models.Policy{}
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Type, &p.Content, &p.Version, &p.State,
			&p.DeprecatedAt, &p.DeprecationMessage, &p.ReplacementPolicyID,
			&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan policy: %w", err)
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

// ListPoliciesByGroupIDs returns released policies with enabled bindings for any of the given node groups.
// Policies are deduplicated; priority is the max across all bindings.
func (r *PolicyBindingRepository) ListPoliciesByGroupIDs(ctx context.Context, groupIDs []string) ([]*models.Policy, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(groupIDs))
	args := make([]interface{}, len(groupIDs))
	for i, id := range groupIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(`SELECT DISTINCT ON (p.id) p.id, p.name, p.description, p.type, p.content, p.version, p.status,
			p.deprecated_at, p.deprecation_message, p.replacement_policy_id,
			p.created_by, p.created_at, p.updated_at
		FROM policies p
		JOIN policy_bindings pb ON pb.policy_id = p.id
		WHERE pb.group_id IN (%s)
		  AND pb.state = 'enabled'
		  AND p.status = 'released'
		ORDER BY p.id, pb.priority DESC, p.name`, strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies by groups: %w", err)
	}
	defer rows.Close()
	var policies []*models.Policy
	for rows.Next() {
		p := &models.Policy{}
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Type, &p.Content, &p.Version, &p.State,
			&p.DeprecatedAt, &p.DeprecationMessage, &p.ReplacementPolicyID,
			&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan policy: %w", err)
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

// DeleteByPolicyID deletes all bindings for a given policy
func (r *PolicyBindingRepository) DeleteByPolicyID(ctx context.Context, policyID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM policy_bindings WHERE policy_id = $1", policyID)
	if err != nil {
		return fmt.Errorf("failed to delete bindings for policy: %w", err)
	}
	return nil
}
