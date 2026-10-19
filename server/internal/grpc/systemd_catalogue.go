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
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// systemdRepository is the subset of database.SystemdRepository used by PolicyServer.
type systemdRepository interface {
	ReplaceNodeUnits(ctx context.Context, nodeID string, units []*pb.SystemdUnitInfo) error
	UpsertNodeUnit(ctx context.Context, nodeID string, unit *pb.SystemdUnitInfo) error
}

// systemdActionSink receives the results of on-demand unit actions
// (services.SystemdActionBroker).
type systemdActionSink interface {
	Deliver(id, node string, r services.SystemdUnitActionResult) bool
}

// WithSystemdRepository enables the systemd unit catalogue RPC.
func (s *PolicyServer) WithSystemdRepository(repo systemdRepository) *PolicyServer {
	s.systemdRepo = repo
	return s
}

// WithSystemdActions routes the results of on-demand unit actions to the
// requests waiting for them.
func (s *PolicyServer) WithSystemdActions(sink systemdActionSink) *PolicyServer {
	s.systemdActions = sink
	return s
}

// Limits for the node-reported unit catalogue; agent data is untrusted input.
const (
	maxReportedSystemdUnits    = 4000
	maxSystemdCalendarPerUnit  = 16
	maxSystemdInfoStringLength = 255
	maxSystemdPathLength       = 512
)

var (
	systemdStateWordPattern = regexp.MustCompile(`^[a-z-]{0,32}$`)
	// systemdExposurePattern matches the exposure level systemd-analyze
	// security prints ("9.6", "0.5", "10.0").
	systemdExposurePattern  = regexp.MustCompile(`^(\d{1,2}(\.\d)?)?$`)
	systemdPredicatePattern = regexp.MustCompile(`^[A-Z]{0,12}$`)
	systemdActionIDPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// maxSystemdActionMessage bounds the message an agent reports for an
// action.
const maxSystemdActionMessage = 2048

// ReportSystemdCatalogue accepts the systemd units installed on an agent's
// node and replaces the node's rows in the catalogue.
func (s *PolicyServer) ReportSystemdCatalogue(ctx context.Context, req *pb.ReportSystemdCatalogueRequest) (*pb.ReportSystemdCatalogueResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if s.systemdRepo == nil {
		return &pb.ReportSystemdCatalogueResponse{}, nil
	}

	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	reported := req.GetUnits()
	if len(reported) > maxReportedSystemdUnits {
		reported = reported[:maxReportedSystemdUnits]
	}
	units := make([]*pb.SystemdUnitInfo, 0, len(reported))
	seen := make(map[string]struct{}, len(reported))
	skipped := 0
	for _, u := range reported {
		clean, ok := sanitizeSystemdUnitInfo(u)
		if !ok {
			skipped++
			continue
		}
		if _, dup := seen[clean.GetName()]; dup {
			continue
		}
		seen[clean.GetName()] = struct{}{}
		units = append(units, clean)
	}
	if err := s.systemdRepo.ReplaceNodeUnits(ctx, node.ID, units); err != nil {
		log.Printf("WARNING: systemd catalogue: failed to replace units for node %s: %v", clientID, err)
		return nil, status.Errorf(codes.Internal, "failed to store unit catalogue")
	}
	if v := req.GetSystemdVersion(); v != "" {
		log.Printf("systemd catalogue: node %s reported %d units (%d skipped), systemd %s", clientID, len(units), skipped, truncateString(v, 32))
	} else {
		log.Printf("systemd catalogue: node %s reported %d units (%d skipped)", clientID, len(units), skipped)
	}
	return &pb.ReportSystemdCatalogueResponse{}, nil
}

// sanitizeSystemdUnitInfo validates the name and bounds every string of a
// reported unit. It returns false when the unit must be dropped.
func sanitizeSystemdUnitInfo(u *pb.SystemdUnitInfo) (*pb.SystemdUnitInfo, bool) {
	if u == nil || systemdunits.ValidateName(u.GetName()) != nil {
		return nil, false
	}
	typ := systemdunits.Type(u.GetName())
	if u.GetType() != "" && u.GetType() != typ {
		return nil, false
	}
	for _, w := range []string{u.GetUnitFileState(), u.GetPreset(), u.GetActiveState(), u.GetSubState()} {
		if !systemdStateWordPattern.MatchString(w) {
			return nil, false
		}
	}
	calendars := u.GetOnCalendar()
	if len(calendars) > maxSystemdCalendarPerUnit {
		calendars = calendars[:maxSystemdCalendarPerUnit]
	}
	cleanCalendars := make([]string, 0, len(calendars))
	for _, c := range calendars {
		if systemdunits.ValidateCalendar(c) == nil {
			cleanCalendars = append(cleanCalendars, c)
		}
	}
	triggers := u.GetTriggers()
	if triggers != "" && systemdunits.ValidateName(triggers) != nil {
		triggers = ""
	}
	serviceType := u.GetServiceType()
	if !systemdStateWordPattern.MatchString(serviceType) {
		serviceType = ""
	}
	exposure, predicate := u.GetSecurityExposure(), u.GetSecurityPredicate()
	if typ != "service" || !systemdExposurePattern.MatchString(exposure) || !systemdPredicatePattern.MatchString(predicate) {
		exposure, predicate = "", ""
	}
	return &pb.SystemdUnitInfo{
		Name:              u.GetName(),
		Description:       truncateString(u.GetDescription(), maxSystemdInfoStringLength),
		Type:              typ,
		UnitFileState:     u.GetUnitFileState(),
		Preset:            u.GetPreset(),
		ActiveState:       u.GetActiveState(),
		SubState:          u.GetSubState(),
		HasInstall:        u.GetHasInstall(),
		OnCalendar:        cleanCalendars,
		Triggers:          triggers,
		FragmentPath:      truncateString(u.GetFragmentPath(), maxSystemdPathLength),
		ServiceType:       serviceType,
		SecurityExposure:  exposure,
		SecurityPredicate: predicate,
	}, true
}

// ReportSystemdUnitActionResult accepts the outcome of an on-demand unit
// action: it refreshes the unit's catalogue row from the reported state
// and hands the result to the request that waits for it.
func (s *PolicyServer) ReportSystemdUnitActionResult(ctx context.Context, req *pb.ReportSystemdUnitActionResultRequest) (*pb.ReportSystemdUnitActionResultResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if !systemdActionIDPattern.MatchString(req.GetActionId()) {
		return nil, status.Errorf(codes.InvalidArgument, "action_id is malformed")
	}
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}
	var unit *pb.SystemdUnitInfo
	if u := req.GetUnit(); u != nil {
		if clean, ok := sanitizeSystemdUnitInfo(u); ok {
			unit = clean
			if s.systemdRepo != nil {
				if err := s.systemdRepo.UpsertNodeUnit(ctx, node.ID, clean); err != nil {
					log.Printf("WARNING: systemd catalogue: failed to update %s for node %s: %v", clean.GetName(), clientID, err)
				}
			}
		}
	}
	result := services.SystemdUnitActionResult{
		Success: req.GetSuccess(),
		Message: truncateString(req.GetMessage(), maxSystemdActionMessage),
		Unit:    unit,
	}
	delivered := s.systemdActions != nil && s.systemdActions.Deliver(req.GetActionId(), clientID, result)
	log.Printf("systemd action %s: node %s reported success=%v (waiting request: %v)", req.GetActionId(), clientID, req.GetSuccess(), delivered)
	return &pb.ReportSystemdUnitActionResultResponse{}, nil
}

func truncateString(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
