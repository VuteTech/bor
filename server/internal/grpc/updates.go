// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// updatesRepository is the subset of database.UpdatesRepository used by
// PolicyServer.
type updatesRepository interface {
	ReplaceNodeInventory(ctx context.Context, nodeID string, inv *pb.UpdatesInventory) error
}

// WithUpdatesRepository enables the Software updates inventory RPC.
func (s *PolicyServer) WithUpdatesRepository(repo updatesRepository) *PolicyServer {
	s.updatesRepo = repo
	return s
}

// updatesActionSink receives the results of on-demand Software updates
// actions (services.UpdatesActionBroker).
type updatesActionSink interface {
	Deliver(id, node string, r services.UpdatesActionResult) bool
}

// WithUpdatesActions routes the results of on-demand actions to the
// requests waiting for them.
func (s *PolicyServer) WithUpdatesActions(sink updatesActionSink) *PolicyServer {
	s.updatesActions = sink
	return s
}

// Limits for the node-reported inventory; agent data is untrusted input.
const (
	maxReportedUpdatesPending = 200
	maxReportedUpdatesCVEs    = 20
	maxReportedUpdatesReasons = 8
	maxUpdatesName            = 255
	maxUpdatesAdvisory        = 128
	maxUpdatesMessage         = 1024
)

var (
	updatesEngines   = map[string]bool{"none": true, "dnf-automatic": true, "unattended-upgrades": true, "os-update": true, "transactional-update": true, "rpm-ostree": true}
	updatesModes     = map[string]bool{"": true, "off": true, "check": true, "security": true, "all": true}
	updatesManagedBy = map[string]bool{"": true, "bor": true, "vendor": true, "foreign": true}
	updatesSeverity  = map[string]bool{"": true, "critical": true, "important": true, "moderate": true, "low": true}
	updatesCVEre     = regexp.MustCompile(`^CVE-\d{4}-\d{4,}$`)
	printableRE      = regexp.MustCompile(`^[^\x00-\x1f\x7f]*$`)
)

func cleanString(s string, limit int) string {
	s = strings.TrimSpace(s)
	if !printableRE.MatchString(s) {
		s = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, s)
	}
	if len(s) > limit {
		s = s[:limit]
	}
	return s
}

func cleanTime(ts *timestamppb.Timestamp) *timestamppb.Timestamp {
	if ts == nil || !ts.IsValid() {
		return nil
	}
	return ts
}

// sanitizeUpdatesInventory bounds and cleans what a node reported.
func sanitizeUpdatesInventory(in *pb.UpdatesInventory) *pb.UpdatesInventory {
	out := &pb.UpdatesInventory{}
	if in == nil {
		out.Engine = "none"
		return out
	}
	out.Engine = in.GetEngine()
	if !updatesEngines[out.Engine] {
		out.Engine = "none"
	}
	out.EngineInstalled = in.GetEngineInstalled()
	out.EngineTimerEnabled = in.GetEngineTimerEnabled()
	if updatesManagedBy[in.GetEngineManagedBy()] {
		out.EngineManagedBy = in.GetEngineManagedBy()
	}
	if updatesModes[in.GetEffectiveMode()] {
		out.EffectiveMode = in.GetEffectiveMode()
	}
	out.LastCheck = cleanTime(in.GetLastCheck())
	if r := in.GetLastRun(); r != nil {
		out.LastRun = &pb.UpdatesRun{Time: cleanTime(r.GetTime()), Success: r.GetSuccess(), Packages: max(r.GetPackages(), 0), Message: cleanString(r.GetMessage(), maxUpdatesMessage)}
	}
	out.PendingTotal = max(in.GetPendingTotal(), 0)
	out.PendingSecurity = max(in.GetPendingSecurity(), 0)
	for i, p := range in.GetPending() {
		if i >= maxReportedUpdatesPending || p == nil {
			break
		}
		name := cleanString(p.GetName(), maxUpdatesName)
		if name == "" {
			continue
		}
		item := &pb.UpdatesPending{Name: name, Version: cleanString(p.GetVersion(), maxUpdatesName), Advisory: cleanString(p.GetAdvisory(), maxUpdatesAdvisory), Security: p.GetSecurity(), Kind: "package"}
		if updatesSeverity[p.GetSeverity()] {
			item.Severity = p.GetSeverity()
		}
		if p.GetKind() == "firmware" {
			item.Kind = "firmware"
		}
		for _, c := range p.GetCves() {
			if len(item.Cves) >= maxReportedUpdatesCVEs {
				break
			}
			if updatesCVEre.MatchString(c) {
				item.Cves = append(item.Cves, c)
			}
		}
		out.Pending = append(out.Pending, item)
	}
	if r := in.GetReboot(); r != nil {
		out.Reboot = &pb.UpdatesRebootState{Required: r.GetRequired(), Since: cleanTime(r.GetSince()), Staged: r.GetStaged()}
		for _, reason := range r.GetReasons() {
			if len(out.Reboot.Reasons) >= maxReportedUpdatesReasons {
				break
			}
			if cleaned := cleanString(reason, maxUpdatesName); cleaned != "" {
				out.Reboot.Reasons = append(out.Reboot.Reasons, cleaned)
			}
		}
	}
	out.BootTime = cleanTime(in.GetBootTime())
	out.RunningKernel = cleanString(in.GetRunningKernel(), maxUpdatesName)
	out.NewestKernel = cleanString(in.GetNewestKernel(), maxUpdatesName)
	for _, f := range in.GetForeignMechanisms() {
		if len(out.ForeignMechanisms) >= maxReportedUpdatesReasons {
			break
		}
		if cleaned := cleanString(f, maxUpdatesName); cleaned != "" {
			out.ForeignMechanisms = append(out.ForeignMechanisms, cleaned)
		}
	}
	out.LastPolicyReboot = cleanTime(in.GetLastPolicyReboot())
	out.OldestSecuritySince = cleanTime(in.GetOldestSecuritySince())
	out.PendingFirmware = max(in.GetPendingFirmware(), 0)
	return out
}

// ReportUpdatesInventory accepts the update state of an agent's node and
// replaces the node's rows.
func (s *PolicyServer) ReportUpdatesInventory(ctx context.Context, req *pb.ReportUpdatesInventoryRequest) (*pb.ReportUpdatesInventoryResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if s.updatesRepo == nil {
		return &pb.ReportUpdatesInventoryResponse{}, nil
	}
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}
	inv := sanitizeUpdatesInventory(req.GetInventory())
	if err := s.updatesRepo.ReplaceNodeInventory(ctx, node.ID, inv); err != nil {
		log.Printf("WARNING: updates inventory: failed to store inventory for node %s: %v", clientID, err)
		return nil, status.Errorf(codes.Internal, "failed to store inventory")
	}
	log.Printf("updates inventory: node %s reported %s (%s): %d pending, %d security, reboot required=%v",
		clientID, inv.Engine, inv.EffectiveMode, inv.PendingTotal, inv.PendingSecurity, inv.GetReboot().GetRequired())
	return &pb.ReportUpdatesInventoryResponse{}, nil
}

// ReportUpdatesActionResult hands the outcome of a check, install or
// reboot action to the request waiting for it.
func (s *PolicyServer) ReportUpdatesActionResult(ctx context.Context, req *pb.ReportUpdatesActionResultRequest) (*pb.ReportUpdatesActionResultResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if !macActionIDPattern.MatchString(req.GetActionId()) {
		return nil, status.Errorf(codes.InvalidArgument, "action_id is malformed")
	}
	result := services.UpdatesActionResult{
		Success: req.GetSuccess(),
		Message: truncateString(req.GetMessage(), maxMacActionMessage),
		Text:    truncateString(req.GetText(), maxMacActionText),
	}
	delivered := s.updatesActions != nil && s.updatesActions.Deliver(req.GetActionId(), clientID, result)
	log.Printf("updates action %s: node %s reported success=%v (waiting request: %v)", req.GetActionId(), clientID, req.GetSuccess(), delivered)
	return &pb.ReportUpdatesActionResultResponse{}, nil
}
