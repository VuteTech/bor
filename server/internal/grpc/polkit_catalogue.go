// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReportPolkitCatalogue accepts a polkit action catalogue from an agent.
// The agent calls this at startup after running pkaction --verbose.
func (s *PolicyServer) ReportPolkitCatalogue(ctx context.Context, req *pb.ReportPolkitCatalogueRequest) (*pb.ReportPolkitCatalogueResponse, error) {
	clientID := req.GetClientId()
	if clientID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "client_id is required")
	}

	if s.polkitRepo == nil {
		// No database configured — accept silently.
		return &pb.ReportPolkitCatalogueResponse{Success: true}, nil
	}

	// Look up the node to get its UUID (needed for node_polkit_actions).
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	actions := req.GetActions()
	actionIDs := make([]string, 0, len(actions))

	for _, action := range actions {
		if action.GetActionId() == "" {
			continue
		}
		if err := s.polkitRepo.UpsertAction(ctx, action, "agent"); err != nil {
			log.Printf("WARNING: polkit catalogue: failed to upsert action %s for node %s: %v",
				action.GetActionId(), clientID, err)
			// Continue — a single bad action should not abort the whole report.
			continue
		}
		actionIDs = append(actionIDs, action.GetActionId())
	}

	if err := s.polkitRepo.ReplaceNodeActions(ctx, node.ID, actionIDs); err != nil {
		log.Printf("WARNING: polkit catalogue: failed to replace node actions for %s: %v", clientID, err)
	}

	log.Printf("polkit catalogue: node %s reported %d actions", clientID, len(actionIDs))

	return &pb.ReportPolkitCatalogueResponse{Success: true}, nil
}
