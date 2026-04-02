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

// ReportSchemaCatalogue accepts a GSettings schema catalogue from an agent.
// The agent calls this at startup after scanning /usr/share/glib-2.0/schemas/.
func (s *PolicyServer) ReportSchemaCatalogue(ctx context.Context, req *pb.ReportSchemaCatalogueRequest) (*pb.ReportSchemaCatalogueResponse, error) {
	clientID := req.GetClientId()
	if clientID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "client_id is required")
	}

	if s.dconfRepo == nil {
		// No database configured — accept silently.
		return &pb.ReportSchemaCatalogueResponse{}, nil
	}

	// Look up the node to get its UUID (needed for node_dconf_schemas).
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	schemas := req.GetSchemas()
	schemaIDs := make([]string, 0, len(schemas))

	for _, schema := range schemas {
		if schema.GetSchemaId() == "" {
			continue
		}
		if err := s.dconfRepo.UpsertSchema(ctx, schema, "agent"); err != nil {
			log.Printf("WARNING: dconf catalogue: failed to upsert schema %s for node %s: %v",
				schema.GetSchemaId(), clientID, err)
			// Continue — a single bad schema should not abort the whole report.
			continue
		}
		schemaIDs = append(schemaIDs, schema.GetSchemaId())
	}

	if err := s.dconfRepo.ReplaceNodeSchemas(ctx, node.ID, schemaIDs); err != nil {
		log.Printf("WARNING: dconf catalogue: failed to replace node schemas for %s: %v", clientID, err)
	}

	gnomeVer := req.GetGnomeVersion()
	if gnomeVer != "" {
		log.Printf("dconf catalogue: node %s reported %d schemas (GNOME %s)", clientID, len(schemaIDs), gnomeVer)
	} else {
		log.Printf("dconf catalogue: node %s reported %d schemas", clientID, len(schemaIDs))
	}

	return &pb.ReportSchemaCatalogueResponse{}, nil
}
