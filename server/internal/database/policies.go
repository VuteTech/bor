// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
)

// PolicyRepository handles policy database operations
type PolicyRepository struct {
	db *DB
}

// NewPolicyRepository creates a new PolicyRepository
func NewPolicyRepository(db *DB) *PolicyRepository {
	return &PolicyRepository{db: db}
}

// Create inserts a new policy into the database
func (r *PolicyRepository) Create(ctx context.Context, policy *models.Policy) error {
	query := `
		INSERT INTO policies (name, description, type, content, version, status, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`

	now := time.Now()
	policy.CreatedAt = now
	policy.UpdatedAt = now

	if policy.State == "" {
		policy.State = models.PolicyStateDraft
	}

	err := r.db.QueryRowContext(ctx, query,
		policy.Name, policy.Description, policy.Type, policy.Content,
		policy.Version, policy.State, policy.CreatedBy,
		policy.CreatedAt, policy.UpdatedAt,
	).Scan(&policy.ID)
	if err != nil {
		return fmt.Errorf("failed to create policy: %w", err)
	}

	return nil
}

// GetByName retrieves a policy by name
func (r *PolicyRepository) GetByName(ctx context.Context, name string) (*models.Policy, error) {
	query := `
		SELECT id, name, description, type, content, version, status, deprecated_at, deprecation_message, replacement_policy_id, created_by, created_at, updated_at
		FROM policies WHERE name = $1`

	policy := &models.Policy{}
	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&policy.ID, &policy.Name, &policy.Description, &policy.Type,
		&policy.Content, &policy.Version, &policy.State,
		&policy.DeprecatedAt, &policy.DeprecationMessage, &policy.ReplacementPolicyID,
		&policy.CreatedBy, &policy.CreatedAt, &policy.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy by name: %w", err)
	}

	return policy, nil
}

// GetByID retrieves a policy by ID
func (r *PolicyRepository) GetByID(ctx context.Context, id string) (*models.Policy, error) {
	query := `
		SELECT id, name, description, type, content, version, status, deprecated_at, deprecation_message, replacement_policy_id, created_by, created_at, updated_at
		FROM policies WHERE id = $1`

	policy := &models.Policy{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&policy.ID, &policy.Name, &policy.Description, &policy.Type,
		&policy.Content, &policy.Version, &policy.State,
		&policy.DeprecatedAt, &policy.DeprecationMessage, &policy.ReplacementPolicyID,
		&policy.CreatedBy, &policy.CreatedAt, &policy.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy by id: %w", err)
	}

	return policy, nil
}

// ListEnabled returns all released policies (for agent consumption)
func (r *PolicyRepository) ListEnabled(ctx context.Context) ([]*models.Policy, error) {
	query := `
		SELECT id, name, description, type, content, version, status, deprecated_at, deprecation_message, replacement_policy_id, created_by, created_at, updated_at
		FROM policies WHERE status = 'released' ORDER BY name`

	return r.scanPolicies(ctx, query)
}

// ListAll returns all policies regardless of state
func (r *PolicyRepository) ListAll(ctx context.Context) ([]*models.Policy, error) {
	query := `
		SELECT id, name, description, type, content, version, status, deprecated_at, deprecation_message, replacement_policy_id, created_by, created_at, updated_at
		FROM policies ORDER BY updated_at DESC`

	return r.scanPolicies(ctx, query)
}

// scanPolicies is a helper to scan multiple policies from a query
func (r *PolicyRepository) scanPolicies(ctx context.Context, query string, args ...interface{}) ([]*models.Policy, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}
	defer rows.Close()

	var policies []*models.Policy
	for rows.Next() {
		policy := &models.Policy{}
		err := rows.Scan(
			&policy.ID, &policy.Name, &policy.Description, &policy.Type,
			&policy.Content, &policy.Version, &policy.State,
			&policy.DeprecatedAt, &policy.DeprecationMessage, &policy.ReplacementPolicyID,
			&policy.CreatedBy, &policy.CreatedAt, &policy.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan policy: %w", err)
		}
		policies = append(policies, policy)
	}

	return policies, rows.Err()
}

// Update updates an existing policy (only allowed in DRAFT state - enforced by service layer)
func (r *PolicyRepository) Update(ctx context.Context, policy *models.Policy) error {
	query := `
		UPDATE policies
		SET name = $1, description = $2, type = $3, content = $4,
		    version = version + 1, updated_at = $5
		WHERE id = $6
		RETURNING version`

	policy.UpdatedAt = time.Now()

	err := r.db.QueryRowContext(ctx, query,
		policy.Name, policy.Description, policy.Type, policy.Content,
		policy.UpdatedAt, policy.ID,
	).Scan(&policy.Version)
	if err != nil {
		return fmt.Errorf("failed to update policy: %w", err)
	}

	return nil
}

// SetState updates the state of a policy
func (r *PolicyRepository) SetState(ctx context.Context, id string, state string) error {
	query := `UPDATE policies SET status = $1, updated_at = $2 WHERE id = $3`
	result, err := r.db.ExecContext(ctx, query, state, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to set policy state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("policy not found")
	}
	return nil
}

// SetDeprecation sets or clears deprecation metadata on a policy
func (r *PolicyRepository) SetDeprecation(ctx context.Context, id string, deprecatedAt *time.Time, message *string, replacementID *string) error {
	query := `UPDATE policies SET deprecated_at = $1, deprecation_message = $2, replacement_policy_id = $3, updated_at = $4 WHERE id = $5`
	result, err := r.db.ExecContext(ctx, query, deprecatedAt, message, replacementID, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to set deprecation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("policy not found")
	}
	return nil
}

// Delete removes a policy from the database
func (r *PolicyRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM policies WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("policy not found")
	}
	return nil
}
