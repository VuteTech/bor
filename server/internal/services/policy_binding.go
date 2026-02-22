// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// PolicyBindingService handles policy binding business logic
type PolicyBindingService struct {
	repo          *database.PolicyBindingRepository
	policyRepo    *database.PolicyRepository
	nodeGroupRepo *database.NodeGroupRepository
}

// NewPolicyBindingService creates a new PolicyBindingService
func NewPolicyBindingService(repo *database.PolicyBindingRepository, policyRepo *database.PolicyRepository, nodeGroupRepo *database.NodeGroupRepository) *PolicyBindingService {
	return &PolicyBindingService{
		repo:          repo,
		policyRepo:    policyRepo,
		nodeGroupRepo: nodeGroupRepo,
	}
}

// CreateBinding creates a new policy binding (default state: DISABLED)
func (s *PolicyBindingService) CreateBinding(ctx context.Context, req *models.CreatePolicyBindingRequest) (*models.PolicyBinding, error) {
	if req.PolicyID == "" {
		return nil, fmt.Errorf("policy_id is required")
	}
	if req.GroupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	// Verify policy exists
	policy, err := s.policyRepo.GetByID(ctx, req.PolicyID)
	if err != nil {
		return nil, fmt.Errorf("failed to verify policy: %w", err)
	}
	if policy == nil {
		return nil, fmt.Errorf("policy not found")
	}

	// Verify group exists
	group, err := s.nodeGroupRepo.GetByID(ctx, req.GroupID)
	if err != nil {
		return nil, fmt.Errorf("failed to verify group: %w", err)
	}
	if group == nil {
		return nil, fmt.Errorf("node group not found")
	}

	b := &models.PolicyBinding{
		PolicyID: req.PolicyID,
		GroupID:  req.GroupID,
		State:    models.BindingStateDisabled,
		Priority: req.Priority,
	}
	if err := s.repo.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("failed to create binding: %w", err)
	}
	return b, nil
}

// GetBinding retrieves a policy binding by ID
func (s *PolicyBindingService) GetBinding(ctx context.Context, id string) (*models.PolicyBinding, error) {
	return s.repo.GetByID(ctx, id)
}

// ListBindings returns all policy bindings with details
func (s *PolicyBindingService) ListBindings(ctx context.Context) ([]*models.PolicyBindingWithDetails, error) {
	return s.repo.ListAll(ctx)
}

// UpdateBinding updates a policy binding with enforcement rules
func (s *PolicyBindingService) UpdateBinding(ctx context.Context, id string, req *models.UpdatePolicyBindingRequest) (*models.PolicyBinding, error) {
	// If trying to enable, verify the policy is RELEASED
	if req.State != nil && *req.State == models.BindingStateEnabled {
		binding, err := s.repo.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get binding: %w", err)
		}
		if binding == nil {
			return nil, fmt.Errorf("binding not found")
		}

		policy, err := s.policyRepo.GetByID(ctx, binding.PolicyID)
		if err != nil {
			return nil, fmt.Errorf("failed to verify policy: %w", err)
		}
		if policy == nil {
			return nil, fmt.Errorf("policy not found")
		}
		if policy.State != models.PolicyStateReleased {
			return nil, fmt.Errorf("binding can only be enabled when policy is released (current policy state: %s)", policy.State)
		}
	}

	// Validate state value if provided
	if req.State != nil {
		if *req.State != models.BindingStateEnabled && *req.State != models.BindingStateDisabled {
			return nil, fmt.Errorf("invalid binding state: %s (valid states: enabled, disabled)", *req.State)
		}
	}

	if err := s.repo.Update(ctx, id, req); err != nil {
		return nil, fmt.Errorf("failed to update binding: %w", err)
	}
	return s.repo.GetByID(ctx, id)
}

// DeleteBinding deletes a policy binding
func (s *PolicyBindingService) DeleteBinding(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// HasEnabledBinding returns true if the policy has at least one enabled binding
func (s *PolicyBindingService) HasEnabledBinding(ctx context.Context, policyID string) (bool, error) {
	count, err := s.repo.CountEnabledByPolicyID(ctx, policyID)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
