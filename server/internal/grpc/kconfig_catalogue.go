// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"
	"regexp"

	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// kconfigRepository is the subset of database.KConfigRepository used by PolicyServer.
type kconfigRepository interface {
	UpsertEntry(ctx context.Context, e *pb.KConfigCatalogueEntry, source string) error
	ReplaceNodeEntries(ctx context.Context, nodeID string, entryIDs []string) error
	ReplaceNodeKCMs(ctx context.Context, nodeID string, modules []*pb.KcmModule) error
	ReplaceNodeLookAndFeel(ctx context.Context, nodeID string, themes []*pb.LookAndFeelPackage) error
}

// WithKConfigRepository enables the KDE settings catalogue RPC.
func (s *PolicyServer) WithKConfigRepository(repo kconfigRepository) *PolicyServer {
	s.kconfigRepo = repo
	return s
}

// Limits for node-reported catalogues; agent data is untrusted input.
const (
	maxReportedKConfigEntries = 5000
	maxReportedKCMs           = 1000
	maxReportedLookAndFeel    = 500
)

var (
	kconfigCatalogueIDPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,512}$`)
	kcmModuleIDPattern        = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
)

// ReportKConfigCatalogue accepts the KConfigXT schema entries and System
// Settings modules installed on an agent's node. Entries are stored with
// source "kcfg" next to the built-in catalogue and linked to the node.
func (s *PolicyServer) ReportKConfigCatalogue(ctx context.Context, req *pb.ReportKConfigCatalogueRequest) (*pb.ReportKConfigCatalogueResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}

	if s.kconfigRepo == nil {
		// No database configured: accept silently.
		return &pb.ReportKConfigCatalogueResponse{}, nil
	}

	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	entries := req.GetEntries()
	if len(entries) > maxReportedKConfigEntries {
		entries = entries[:maxReportedKConfigEntries]
	}
	entryIDs := make([]string, 0, len(entries))
	skipped := 0
	for _, e := range entries {
		if !kconfigCatalogueIDPattern.MatchString(e.GetId()) || e.GetLabel() == "" || e.GetType() == "" {
			skipped++
			continue
		}
		if err := services.ValidateKConfigTarget(e.GetFile(), e.GetGroupPath(), e.GetKey()); err != nil {
			skipped++
			continue
		}
		if err := s.kconfigRepo.UpsertEntry(ctx, e, "kcfg"); err != nil {
			log.Printf("WARNING: kconfig catalogue: failed to upsert entry %s for node %s: %v", e.GetId(), clientID, err)
			continue
		}
		entryIDs = append(entryIDs, e.GetId())
	}
	if err := s.kconfigRepo.ReplaceNodeEntries(ctx, node.ID, entryIDs); err != nil {
		log.Printf("WARNING: kconfig catalogue: failed to replace node entries for %s: %v", clientID, err)
	}

	kcms := req.GetKcms()
	if len(kcms) > maxReportedKCMs {
		kcms = kcms[:maxReportedKCMs]
	}
	valid := make([]*pb.KcmModule, 0, len(kcms))
	for _, m := range kcms {
		if !kcmModuleIDPattern.MatchString(m.GetId()) {
			continue
		}
		name := m.GetName()
		if len(name) > 255 {
			name = name[:255]
		}
		valid = append(valid, &pb.KcmModule{Id: m.GetId(), Name: name})
	}
	if err := s.kconfigRepo.ReplaceNodeKCMs(ctx, node.ID, valid); err != nil {
		log.Printf("WARNING: kconfig catalogue: failed to replace node modules for %s: %v", clientID, err)
	}

	// Global themes: same id rules as modules (package ids look like
	// org.kde.breezedark.desktop).
	reported := req.GetLookAndFeel()
	if len(reported) > maxReportedLookAndFeel {
		reported = reported[:maxReportedLookAndFeel]
	}
	themes := make([]*pb.LookAndFeelPackage, 0, len(reported))
	for _, t := range reported {
		if !kcmModuleIDPattern.MatchString(t.GetId()) {
			continue
		}
		name := t.GetName()
		if len(name) > 255 {
			name = name[:255]
		}
		themes = append(themes, &pb.LookAndFeelPackage{Id: t.GetId(), Name: name})
	}
	if err := s.kconfigRepo.ReplaceNodeLookAndFeel(ctx, node.ID, themes); err != nil {
		log.Printf("WARNING: kconfig catalogue: failed to replace node themes for %s: %v", clientID, err)
	}

	if v := req.GetPlasmaVersion(); v != "" {
		log.Printf("kconfig catalogue: node %s reported %d settings (%d skipped), %d modules and %d global themes (Plasma %s)", clientID, len(entryIDs), skipped, len(valid), len(themes), v)
	} else {
		log.Printf("kconfig catalogue: node %s reported %d settings (%d skipped), %d modules and %d global themes", clientID, len(entryIDs), skipped, len(valid), len(themes))
	}
	return &pb.ReportKConfigCatalogueResponse{}, nil
}
