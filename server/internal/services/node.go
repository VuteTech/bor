// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// NodeService handles node business logic
type NodeService struct {
	nodeRepo *database.NodeRepository
}

// NewNodeService creates a new NodeService
func NewNodeService(nodeRepo *database.NodeRepository) *NodeService {
	return &NodeService{nodeRepo: nodeRepo}
}

// CreateNode creates a new node
func (s *NodeService) CreateNode(ctx context.Context, node *models.Node) error {
	return s.nodeRepo.Create(ctx, node)
}

// ListAllNodes returns all nodes
func (s *NodeService) ListAllNodes(ctx context.Context) ([]*models.Node, error) {
	return s.nodeRepo.ListAll(ctx)
}

// ListNodesByStatus returns nodes filtered by status
func (s *NodeService) ListNodesByStatus(ctx context.Context, status string) ([]*models.Node, error) {
	if !isValidNodeStatus(status) {
		return nil, fmt.Errorf("invalid node status: %s", status)
	}
	return s.nodeRepo.ListByStatus(ctx, status)
}

// GetNode retrieves a node by ID
func (s *NodeService) GetNode(ctx context.Context, id string) (*models.Node, error) {
	return s.nodeRepo.GetByID(ctx, id)
}

// SearchNodes searches nodes by a term
func (s *NodeService) SearchNodes(ctx context.Context, term string) ([]*models.Node, error) {
	if term == "" {
		return s.nodeRepo.ListAll(ctx)
	}
	return s.nodeRepo.Search(ctx, term)
}

// UpdateNode updates node fields
func (s *NodeService) UpdateNode(ctx context.Context, id string, req *models.UpdateNodeRequest) (*models.Node, error) {
	if err := s.nodeRepo.UpdateFields(ctx, id, req); err != nil {
		return nil, fmt.Errorf("failed to update node: %w", err)
	}
	return s.nodeRepo.GetByID(ctx, id)
}

// GetNodeByName retrieves a node by name
func (s *NodeService) GetNodeByName(ctx context.Context, name string) (*models.Node, error) {
	return s.nodeRepo.GetByName(ctx, name)
}

// CountByStatus returns node counts per status
func (s *NodeService) CountByStatus(ctx context.Context) (map[string]int, error) {
	return s.nodeRepo.CountByStatus(ctx)
}

// ProcessHeartbeat records a node heartbeat, updating metadata facts.
// It does not change node status — status is managed by stream connect/disconnect.
func (s *NodeService) ProcessHeartbeat(ctx context.Context, nodeID string, info *models.NodeHeartbeatInfo) error {
	facts := map[string]string{
		"fqdn":          info.FQDN,
		"ip_address":    info.IPAddress,
		"os_name":       info.OSName,
		"os_version":    info.OSVersion,
		"desktop_env":   strings.Join(info.DesktopEnvs, ", "),
		"agent_version": info.AgentVersion,
		"machine_id":    info.MachineID,
	}
	// Omit empty values so we don't overwrite existing data with blanks.
	for k, v := range facts {
		if v == "" {
			delete(facts, k)
		}
	}
	if err := s.nodeRepo.UpdateHeartbeat(ctx, nodeID, facts); err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}
	return nil
}

// UpdateNodeStatus sets the cached status of a node. Used by the gRPC
// stream handler to mark nodes online on connect and offline on disconnect.
func (s *NodeService) UpdateNodeStatus(ctx context.Context, nodeID, status string) error {
	if err := s.nodeRepo.UpdateStatus(ctx, nodeID, status, ""); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}
	return nil
}

// DeleteNode removes a node by ID.
func (s *NodeService) DeleteNode(ctx context.Context, id string) error {
	return s.nodeRepo.Delete(ctx, id)
}

// AddNodeToGroup adds a node to a node group.
func (s *NodeService) AddNodeToGroup(ctx context.Context, nodeID, groupID string) error {
	return s.nodeRepo.AddToGroup(ctx, nodeID, groupID)
}

// RemoveNodeFromGroup removes a node from a specific node group.
func (s *NodeService) RemoveNodeFromGroup(ctx context.Context, nodeID, groupID string) error {
	return s.nodeRepo.RemoveFromGroup(ctx, nodeID, groupID)
}

// ListNodeGroupIDs returns all group IDs a node belongs to.
func (s *NodeService) ListNodeGroupIDs(ctx context.Context, nodeID string) ([]string, error) {
	return s.nodeRepo.ListGroupIDs(ctx, nodeID)
}

// isValidNodeStatus checks if the given status is a valid node status
func isValidNodeStatus(status string) bool {
	switch status {
	case models.NodeStatusOnline, models.NodeStatusOffline, models.NodeStatusUnknown:
		return true
	default:
		return false
	}
}
