// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"errors"
	"log"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The disk-encryption RPCs run on the mTLS policy port. The node identity is
// always the verified certificate CN (requireClientIdentity), and every
// volume lookup is scoped to that node - nothing identifies a node by
// request fields.

// WithDiskEncryptionService wires the disk-encryption service into the
// PolicyServer. Without it the five RPCs return Unimplemented.
func (s *PolicyServer) WithDiskEncryptionService(svc *services.DiskEncryptionService) *PolicyServer {
	s.diskEncSvc = svc
	return s
}

// resolveNode authenticates the caller and loads its node row.
func (s *PolicyServer) resolveNode(ctx context.Context, claimedID string) (*models.Node, error) {
	clientID, err := requireClientIdentity(ctx, claimedID)
	if err != nil {
		return nil, err
	}
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node")
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}
	return node, nil
}

// diskEncError maps service errors to gRPC status codes without leaking
// internals.
func diskEncError(op string, err error) error {
	var verr *services.DiskEncryptionValidationError
	switch {
	case errors.As(err, &verr):
		return status.Errorf(codes.InvalidArgument, "%s", verr.Msg)
	case errors.Is(err, services.ErrLuksVolumeNotFound), errors.Is(err, services.ErrRecoveryKeyNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, services.ErrEscrowNotConfigured):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	case errors.Is(err, services.ErrNoRotationPending), errors.Is(err, services.ErrServerAssistedRotationOff):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	case errors.Is(err, services.ErrReleaseRateLimited), errors.Is(err, services.ErrEscrowRateLimited):
		return status.Errorf(codes.ResourceExhausted, "%v", err)
	default:
		log.Printf("WARNING: disk_encryption rpc: %s: %v", op, err)
		return status.Errorf(codes.Internal, "failed to %s", op)
	}
}

// ReportDiskEncryptionState ingests an agent inventory report and returns
// the node's pending tasks.
func (s *PolicyServer) ReportDiskEncryptionState(ctx context.Context, req *pb.ReportDiskEncryptionStateRequest) (*pb.ReportDiskEncryptionStateResponse, error) {
	if s.diskEncSvc == nil {
		return nil, status.Errorf(codes.Unimplemented, "disk encryption is not enabled on this server")
	}
	node, err := s.resolveNode(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	tasks, err := s.diskEncSvc.ProcessStateReport(ctx, node, req)
	if err != nil {
		return nil, diskEncError("process disk encryption state", err)
	}
	return &pb.ReportDiskEncryptionStateResponse{Tasks: tasks}, nil
}

// GetDiskEncryptionTasks returns the node's pending tasks.
func (s *PolicyServer) GetDiskEncryptionTasks(ctx context.Context, req *pb.GetDiskEncryptionTasksRequest) (*pb.GetDiskEncryptionTasksResponse, error) {
	if s.diskEncSvc == nil {
		return nil, status.Errorf(codes.Unimplemented, "disk encryption is not enabled on this server")
	}
	node, err := s.resolveNode(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	tasks, err := s.diskEncSvc.GetTasks(ctx, node)
	if err != nil {
		return nil, diskEncError("get disk encryption tasks", err)
	}
	return &pb.GetDiskEncryptionTasksResponse{Tasks: tasks}, nil
}

// EscrowRecoveryKey stores a new recovery key before its keyslot exists.
func (s *PolicyServer) EscrowRecoveryKey(ctx context.Context, req *pb.EscrowRecoveryKeyRequest) (*pb.EscrowRecoveryKeyResponse, error) {
	if s.diskEncSvc == nil {
		return nil, status.Errorf(codes.Unimplemented, "disk encryption is not enabled on this server")
	}
	node, err := s.resolveNode(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if err := s.diskEncSvc.EscrowKey(ctx, node, req); err != nil {
		return nil, diskEncError("escrow recovery key", err)
	}
	return &pb.EscrowRecoveryKeyResponse{Accepted: true}, nil
}

// ConfirmRecoveryKey promotes a pending key to active.
func (s *PolicyServer) ConfirmRecoveryKey(ctx context.Context, req *pb.ConfirmRecoveryKeyRequest) (*pb.ConfirmRecoveryKeyResponse, error) {
	if s.diskEncSvc == nil {
		return nil, status.Errorf(codes.Unimplemented, "disk encryption is not enabled on this server")
	}
	node, err := s.resolveNode(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if err := s.diskEncSvc.ConfirmKey(ctx, node, req); err != nil {
		return nil, diskEncError("confirm recovery key", err)
	}
	return &pb.ConfirmRecoveryKeyResponse{Accepted: true}, nil
}

// BeginRecoveryKeyRotation releases the active key as the rotation
// credential (only while a rotation task exists; audited; rate limited).
func (s *PolicyServer) BeginRecoveryKeyRotation(ctx context.Context, req *pb.BeginRecoveryKeyRotationRequest) (*pb.BeginRecoveryKeyRotationResponse, error) {
	if s.diskEncSvc == nil {
		return nil, status.Errorf(codes.Unimplemented, "disk encryption is not enabled on this server")
	}
	node, err := s.resolveNode(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	rotationID, key, completeBy, err := s.diskEncSvc.BeginRotation(ctx, node, req.GetLuksUuid())
	if err != nil {
		return nil, diskEncError("begin recovery key rotation", err)
	}
	return &pb.BeginRecoveryKeyRotationResponse{
		RotationId:         rotationID,
		CurrentRecoveryKey: key,
		CompleteBy:         timestamppb.New(completeBy),
	}, nil
}
