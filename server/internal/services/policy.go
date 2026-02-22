// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// timeNow is a variable for testing
var timeNow = time.Now

// PolicyService handles policy business logic
type PolicyService struct {
	policyRepo  *database.PolicyRepository
	bindingRepo *database.PolicyBindingRepository
}

// NewPolicyService creates a new PolicyService
func NewPolicyService(policyRepo *database.PolicyRepository, bindingRepo *database.PolicyBindingRepository) *PolicyService {
	return &PolicyService{policyRepo: policyRepo, bindingRepo: bindingRepo}
}

// ListEnabledPolicies returns all released policies (for agent consumption)
func (s *PolicyService) ListEnabledPolicies(ctx context.Context) ([]*models.Policy, error) {
	return s.policyRepo.ListEnabled(ctx)
}

// ListPoliciesForNodeGroup returns released policies with enabled bindings for a node group
func (s *PolicyService) ListPoliciesForNodeGroup(ctx context.Context, groupID string) ([]*models.Policy, error) {
	if s.bindingRepo == nil {
		return nil, fmt.Errorf("binding repository not configured")
	}
	return s.bindingRepo.ListPoliciesByGroupID(ctx, groupID)
}

// ListPoliciesForNodeGroups returns released policies with enabled bindings for any of the given groups.
func (s *PolicyService) ListPoliciesForNodeGroups(ctx context.Context, groupIDs []string) ([]*models.Policy, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	if s.bindingRepo == nil {
		return nil, fmt.Errorf("binding repository not configured")
	}
	return s.bindingRepo.ListPoliciesByGroupIDs(ctx, groupIDs)
}

// ListAllPolicies returns all policies
func (s *PolicyService) ListAllPolicies(ctx context.Context) ([]*models.Policy, error) {
	return s.policyRepo.ListAll(ctx)
}

// GetPolicy retrieves a policy by ID
func (s *PolicyService) GetPolicy(ctx context.Context, id string) (*models.Policy, error) {
	return s.policyRepo.GetByID(ctx, id)
}

// isValidState checks if the given state is a valid policy state
func isValidState(state string) bool {
	switch state {
	case models.PolicyStateDraft, models.PolicyStateReleased, models.PolicyStateArchived:
		return true
	default:
		return false
	}
}

// CreatePolicy creates a new policy (always starts in DRAFT state)
func (s *PolicyService) CreatePolicy(ctx context.Context, req *models.CreatePolicyRequest, createdBy string) (*models.Policy, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("policy name is required")
	}
	if req.Type == "" {
		return nil, fmt.Errorf("policy type is required")
	}

	policy := &models.Policy{
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Content:     req.Content,
		Version:     1,
		State:       models.PolicyStateDraft,
		CreatedBy:   createdBy,
	}

	if err := s.policyRepo.Create(ctx, policy); err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	return policy, nil
}

// UpdatePolicy updates an existing policy (only allowed if state == DRAFT)
func (s *PolicyService) UpdatePolicy(ctx context.Context, id string, req *models.UpdatePolicyRequest) (*models.Policy, error) {
	policy, err := s.policyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}
	if policy == nil {
		return nil, fmt.Errorf("policy not found")
	}

	if policy.State != models.PolicyStateDraft {
		return nil, fmt.Errorf("policy can only be edited in draft state (current state: %s)", policy.State)
	}

	if req.Name != nil {
		policy.Name = *req.Name
	}
	if req.Description != nil {
		policy.Description = *req.Description
	}
	if req.Type != nil {
		policy.Type = *req.Type
	}
	if req.Content != nil {
		policy.Content = *req.Content
	}

	if err := s.policyRepo.Update(ctx, policy); err != nil {
		return nil, fmt.Errorf("failed to update policy: %w", err)
	}

	return policy, nil
}

// SetPolicyState changes the state of a policy with validation
func (s *PolicyService) SetPolicyState(ctx context.Context, id string, newState string) (*models.Policy, error) {
	if !isValidState(newState) {
		return nil, fmt.Errorf("invalid policy state: %s (valid states: draft, released, archived)", newState)
	}

	policy, err := s.policyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}
	if policy == nil {
		return nil, fmt.Errorf("policy not found")
	}

	// Validate state transitions
	switch newState {
	case models.PolicyStateDraft:
		// Unpublish: RELEASED → DRAFT (only if no enabled bindings)
		if policy.State != models.PolicyStateReleased {
			return nil, fmt.Errorf("only released policies can be unpublished (current state: %s)", policy.State)
		}
		if s.bindingRepo != nil {
			count, err := s.bindingRepo.CountEnabledByPolicyID(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("failed to check bindings: %w", err)
			}
			if count > 0 {
				return nil, fmt.Errorf("cannot unpublish policy: %d enabled binding(s) exist; disable all bindings first", count)
			}
		}

	case models.PolicyStateReleased:
		if policy.State != models.PolicyStateDraft {
			return nil, fmt.Errorf("only draft policies can be released (current state: %s)", policy.State)
		}
		// Validate policy content for release
		if policy.Name == "" {
			return nil, fmt.Errorf("policy name is required for release")
		}
		if policy.Type == "" {
			return nil, fmt.Errorf("policy type is required for release")
		}
		if policy.Content == "" {
			return nil, fmt.Errorf("policy content is required for release")
		}

	case models.PolicyStateArchived:
		if policy.State != models.PolicyStateReleased {
			return nil, fmt.Errorf("only released policies can be archived (current state: %s)", policy.State)
		}
		// Check for enabled bindings
		if s.bindingRepo != nil {
			count, err := s.bindingRepo.CountEnabledByPolicyID(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("failed to check bindings: %w", err)
			}
			if count > 0 {
				return nil, fmt.Errorf("cannot archive policy: %d enabled binding(s) exist; disable all bindings first", count)
			}
		}
	}

	if err := s.policyRepo.SetState(ctx, id, newState); err != nil {
		return nil, fmt.Errorf("failed to set policy state: %w", err)
	}

	return s.policyRepo.GetByID(ctx, id)
}

// DeletePolicy deletes a policy and its associated bindings
func (s *PolicyService) DeletePolicy(ctx context.Context, id string) error {
	policy, err := s.policyRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get policy: %w", err)
	}
	if policy == nil {
		return fmt.Errorf("policy not found")
	}

	// Check for enabled bindings before allowing delete
	if s.bindingRepo != nil {
		count, err := s.bindingRepo.CountEnabledByPolicyID(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to check bindings: %w", err)
		}
		if count > 0 {
			return fmt.Errorf("cannot delete policy: %d enabled binding(s) exist; disable all bindings first", count)
		}

		// Delete all bindings for this policy
		if err := s.bindingRepo.DeleteByPolicyID(ctx, id); err != nil {
			return fmt.Errorf("failed to delete policy bindings: %w", err)
		}
	}

	if err := s.policyRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}

	return nil
}

// DeprecatePolicy sets deprecation metadata on a policy
func (s *PolicyService) DeprecatePolicy(ctx context.Context, id string, req *models.DeprecatePolicyRequest) (*models.Policy, error) {
	policy, err := s.policyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}
	if policy == nil {
		return nil, fmt.Errorf("policy not found")
	}

	now := timeNow()
	if err := s.policyRepo.SetDeprecation(ctx, id, &now, req.Message, req.ReplacementPolicyID); err != nil {
		return nil, fmt.Errorf("failed to deprecate policy: %w", err)
	}

	return s.policyRepo.GetByID(ctx, id)
}
