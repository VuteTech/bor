// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"encoding/json"
	"log"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PolicyServer implements the gRPC PolicyService.
type PolicyServer struct {
	pb.UnimplementedPolicyServiceServer
	policySvc   *services.PolicyService
	nodeSvc     *services.NodeService
	settingsSvc *services.SettingsService
	hub         *PolicyHub
}

// NewPolicyServer creates a new PolicyServer.
func NewPolicyServer(policySvc *services.PolicyService, nodeSvc *services.NodeService, settingsSvc *services.SettingsService, hub *PolicyHub) *PolicyServer {
	return &PolicyServer{policySvc: policySvc, nodeSvc: nodeSvc, settingsSvc: settingsSvc, hub: hub}
}

// GetPolicy returns a single policy by ID.
func (s *PolicyServer) GetPolicy(ctx context.Context, req *pb.GetPolicyRequest) (*pb.GetPolicyResponse, error) {
	if req.GetPolicyId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "policy_id is required")
	}

	policy, err := s.policySvc.GetPolicy(ctx, req.GetPolicyId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get policy: %v", err)
	}
	if policy == nil {
		return nil, status.Errorf(codes.NotFound, "policy not found: %s", req.GetPolicyId())
	}

	return &pb.GetPolicyResponse{
		Policy: modelToProto(policy),
	}, nil
}

// ListPolicies returns policies bound to the calling node's group with enabled bindings.
func (s *PolicyServer) ListPolicies(ctx context.Context, req *pb.ListPoliciesRequest) (*pb.ListPoliciesResponse, error) {
	clientID := req.GetClientId()
	if clientID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "client_id is required")
	}

	// Look up the node to determine its node group
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}
	if len(node.NodeGroupIDs) == 0 {
		log.Printf("Node %s (%s) has no node group assigned; no policies will be delivered.", node.ID, node.Name)
		return &pb.ListPoliciesResponse{
			Policies:   []*pb.Policy{},
			TotalCount: 0,
		}, nil
	}

	// Fetch only policies with enabled bindings for this node's groups
	policies, err := s.policySvc.ListPoliciesForNodeGroups(ctx, node.NodeGroupIDs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list policies for node group: %v", err)
	}

	typeFilter := req.GetTypeFilter()
	var result []*pb.Policy
	for _, p := range policies {
		if typeFilter != "" && p.Type != typeFilter {
			continue
		}
		result = append(result, modelToProto(p))
	}

	return &pb.ListPoliciesResponse{
		Policies:   result,
		TotalCount: int32(len(result)),
	}, nil
}

// SubscribePolicyUpdates is a server-streaming RPC for policy change notifications.
// It implements initial sync (full snapshot or delta) followed by a live watch.
func (s *PolicyServer) SubscribePolicyUpdates(req *pb.SubscribePolicyUpdatesRequest, stream pb.PolicyService_SubscribePolicyUpdatesServer) error {
	clientID := req.GetClientId()
	if clientID == "" {
		return status.Errorf(codes.InvalidArgument, "client_id is required")
	}

	ctx := stream.Context()

	// Resolve node and its group to scope the snapshot.
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	lastKnown := req.GetLastKnownRevision()
	currentRev := s.hub.Revision()

	// ── Initial sync ──────────────────────────────────────────────────
	if lastKnown == 0 || lastKnown > currentRev {
		// First connect or invalid revision → full snapshot.
		if err := s.sendSnapshot(ctx, stream, node); err != nil {
			return err
		}
	} else if lastKnown < currentRev {
		// Client is behind. Try delta first.
		events := s.hub.EventsSince(lastKnown)
		if events == nil {
			// Delta unavailable (compacted). Fall back to snapshot.
			log.Printf("Delta unavailable for client %s (rev %d → %d), sending full snapshot", clientID, lastKnown, currentRev)
			if err := s.sendSnapshot(ctx, stream, node); err != nil {
				return err
			}
		} else {
			// Send delta events.
			for _, ev := range events {
				if err := stream.Send(ev); err != nil {
					return err
				}
			}
		}
	}
	// else lastKnown == currentRev → client is up-to-date, enter watch mode.

	// Mark node online now that the initial sync is complete.
	if err := s.nodeSvc.UpdateNodeStatus(ctx, node.ID, "online"); err != nil {
		log.Printf("Failed to set node %s online: %v", clientID, err)
	}
	defer func() {
		if err := s.nodeSvc.UpdateNodeStatus(context.Background(), node.ID, "offline"); err != nil {
			log.Printf("Failed to set node %s offline: %v", clientID, err)
		}
	}()

	log.Printf("Client %s entering watch mode at revision %d", clientID, s.hub.Revision())

	// ── Watch mode ────────────────────────────────────────────────────
	updates, cancel := s.hub.Subscribe(ctx, clientID)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Client %s stream ended: %v", clientID, ctx.Err())
			return nil
		case ev := <-updates:
			if IsResyncSignal(ev) {
				// A binding or policy changed — send a fresh
				// full snapshot so the client has the correct
				// set of policies for its node group.
				if err := s.sendSnapshot(ctx, stream, node); err != nil {
					return err
				}
			} else {
				if err := stream.Send(ev); err != nil {
					return err
				}
			}
		}
	}
}

// sendSnapshot sends a full policy snapshot to the stream.
func (s *PolicyServer) sendSnapshot(ctx context.Context, stream pb.PolicyService_SubscribePolicyUpdatesServer, node *models.Node) error {
	var policies []*models.Policy
	var err error

	if len(node.NodeGroupIDs) > 0 {
		policies, err = s.policySvc.ListPoliciesForNodeGroups(ctx, node.NodeGroupIDs)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "failed to list policies for snapshot: %v", err)
	}

	currentRev := s.hub.Revision()

	for i, p := range policies {
		isLast := i == len(policies)-1
		update := &pb.PolicyUpdate{
			Type:             pb.PolicyUpdate_SNAPSHOT,
			Policy:           modelToProto(p),
			Revision:         currentRev,
			SnapshotComplete: isLast,
		}
		if err := stream.Send(update); err != nil {
			return err
		}
	}

	// If there are no policies, still send a completion marker.
	if len(policies) == 0 {
		if err := stream.Send(&pb.PolicyUpdate{
			Type:             pb.PolicyUpdate_SNAPSHOT,
			Revision:         currentRev,
			SnapshotComplete: true,
		}); err != nil {
			return err
		}
	}

	return nil
}

// ReportCompliance accepts a compliance report from a client.
func (s *PolicyServer) ReportCompliance(ctx context.Context, req *pb.ReportComplianceRequest) (*pb.ReportComplianceResponse, error) {
	if req.GetClientId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "client_id is required")
	}
	if req.GetPolicyId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "policy_id is required")
	}

	log.Printf("Compliance report: client=%s policy=%s compliant=%v message=%q",
		req.GetClientId(), req.GetPolicyId(), req.GetCompliant(), req.GetMessage())

	return &pb.ReportComplianceResponse{Success: true}, nil
}

// GetAgentConfig returns the agent configuration (notification settings, etc.).
func (s *PolicyServer) GetAgentConfig(ctx context.Context, req *pb.GetAgentConfigRequest) (*pb.GetAgentConfigResponse, error) {
	settings, err := s.settingsSvc.GetAgentNotificationSettings(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get agent config: %v", err)
	}

	return &pb.GetAgentConfigResponse{
		Config: &pb.AgentConfig{
			NotifyUsers:           settings.NotifyUsers,
			NotifyCooldownSeconds: int32(settings.NotifyCooldown),
			NotifyMessage:         settings.NotifyMessage,
			NotifyMessageFirefox:  settings.NotifyMessageFirefox,
			NotifyMessageChrome:   settings.NotifyMessageChrome,
		},
	}, nil
}

// Heartbeat records a node heartbeat with updated metadata.
func (s *PolicyServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	clientID := req.GetClientId()
	if clientID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "client_id is required")
	}

	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	info := &models.NodeHeartbeatInfo{}
	if ni := req.GetInfo(); ni != nil {
		info.FQDN = ni.GetFqdn()
		info.IPAddress = ni.GetIpAddress()
		info.OSName = ni.GetOsName()
		info.OSVersion = ni.GetOsVersion()
		info.DesktopEnvs = ni.GetDesktopEnvs()
		info.AgentVersion = ni.GetAgentVersion()
		info.MachineID = ni.GetMachineId()
	}

	if err := s.nodeSvc.ProcessHeartbeat(ctx, node.ID, info); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to process heartbeat: %v", err)
	}

	log.Printf("Heartbeat from %s: OS=%s %s, DE=%v, agent=%s",
		clientID, info.OSName, info.OSVersion, info.DesktopEnvs, info.AgentVersion)

	return &pb.HeartbeatResponse{Accepted: true}, nil
}

// modelToProto converts an internal Policy model to its protobuf representation.
func modelToProto(p *models.Policy) *pb.Policy {
	pol := &pb.Policy{
		Id:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Type:        p.Type,
		Content:     p.Content,
		Version:     int32(p.Version),
		CreatedAt:   timestamppb.New(p.CreatedAt),
		UpdatedAt:   timestamppb.New(p.UpdatedAt),
		Enabled:     p.State == models.PolicyStateReleased,
	}

	// Populate typed_content based on policy type.
	switch p.Type {
	case "Firefox":
		var fxPol pb.FirefoxPolicy
		if err := json.Unmarshal([]byte(p.Content), &fxPol); err != nil {
			log.Printf("WARNING: failed to unmarshal Firefox typed_content for policy %s: %v", p.ID, err)
		} else {
			pol.TypedContent = &pb.Policy_FirefoxPolicy{FirefoxPolicy: &fxPol}
		}
	case "Kconfig":
		var kcPol pb.KConfigPolicy
		if err := protojson.Unmarshal([]byte(p.Content), &kcPol); err != nil {
			log.Printf("WARNING: failed to unmarshal KConfig typed_content for policy %s: %v", p.ID, err)
		} else {
			pol.TypedContent = &pb.Policy_KconfigPolicy{KconfigPolicy: &kcPol}
		}
	case "Chrome":
		var chrPol pb.ChromePolicy
		if err := protojson.Unmarshal([]byte(p.Content), &chrPol); err != nil {
			log.Printf("WARNING: failed to unmarshal Chrome typed_content for policy %s: %v", p.ID, err)
		} else {
			pol.TypedContent = &pb.Policy_ChromePolicy{ChromePolicy: &chrPol}
		}
	}

	return pol
}
