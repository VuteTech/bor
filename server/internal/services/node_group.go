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

// NodeGroupService handles node group business logic
type NodeGroupService struct {
	repo *database.NodeGroupRepository
}

// NewNodeGroupService creates a new NodeGroupService
func NewNodeGroupService(repo *database.NodeGroupRepository) *NodeGroupService {
	return &NodeGroupService{repo: repo}
}

// CreateNodeGroup creates a new node group
func (s *NodeGroupService) CreateNodeGroup(ctx context.Context, req *models.CreateNodeGroupRequest) (*models.NodeGroup, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	ng := &models.NodeGroup{
		Name:        req.Name,
		Description: req.Description,
	}
	if err := s.repo.Create(ctx, ng); err != nil {
		return nil, fmt.Errorf("failed to create node group: %w", err)
	}
	return ng, nil
}

// GetNodeGroup retrieves a node group by ID
func (s *NodeGroupService) GetNodeGroup(ctx context.Context, id string) (*models.NodeGroup, error) {
	return s.repo.GetByID(ctx, id)
}

// ListNodeGroups returns all node groups
func (s *NodeGroupService) ListNodeGroups(ctx context.Context) ([]*models.NodeGroup, error) {
	return s.repo.ListAll(ctx)
}

// UpdateNodeGroup updates a node group
func (s *NodeGroupService) UpdateNodeGroup(ctx context.Context, id string, req *models.UpdateNodeGroupRequest) (*models.NodeGroup, error) {
	if err := s.repo.Update(ctx, id, req); err != nil {
		return nil, fmt.Errorf("failed to update node group: %w", err)
	}
	return s.repo.GetByID(ctx, id)
}

// DeleteNodeGroup deletes a node group
func (s *NodeGroupService) DeleteNodeGroup(ctx context.Context, id string) error {
	count, err := s.repo.CountNodesByGroupID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to check nodes in group: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete node group: %d node(s) still assigned", count)
	}
	return s.repo.Delete(ctx, id)
}

// CountNodesByGroupID returns the number of nodes in a group
func (s *NodeGroupService) CountNodesByGroupID(ctx context.Context, groupID string) (int, error) {
	return s.repo.CountNodesByGroupID(ctx, groupID)
}
