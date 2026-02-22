// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"

	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/enrollment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnrollmentServer implements the gRPC EnrollmentService.
type EnrollmentServer struct {
	pb.UnimplementedEnrollmentServiceServer
	enrollSvc *services.EnrollmentService
	adminToken string
}

// NewEnrollmentServer creates a new EnrollmentServer.
func NewEnrollmentServer(enrollSvc *services.EnrollmentService, adminToken string) *EnrollmentServer {
	return &EnrollmentServer{
		enrollSvc:  enrollSvc,
		adminToken: adminToken,
	}
}

// CreateEnrollmentToken generates a short-lived enrollment token for a node group.
func (s *EnrollmentServer) CreateEnrollmentToken(ctx context.Context, req *pb.CreateEnrollmentTokenRequest) (*pb.CreateEnrollmentTokenResponse, error) {
	if err := s.checkAdminAuth(ctx); err != nil {
		return nil, err
	}

	if req.GetNodeGroupId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "node_group_id is required")
	}

	token, err := s.enrollSvc.CreateToken(req.GetNodeGroupId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create enrollment token: %v", err)
	}

	log.Printf("Enrollment token created for node group %s (expires %s)", req.GetNodeGroupId(), token.ExpiresAt)

	return &pb.CreateEnrollmentTokenResponse{
		Token:     token.Token,
		ExpiresAt: timestamppb.New(token.ExpiresAt),
	}, nil
}

// Enroll registers a new agent using an enrollment token and CSR.
func (s *EnrollmentServer) Enroll(ctx context.Context, req *pb.EnrollRequest) (*pb.EnrollResponse, error) {
	if req.GetEnrollmentToken() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "enrollment_token is required")
	}
	if len(req.GetCsrPem()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "csr_pem is required")
	}

	nodeGroupID, err := s.enrollSvc.ConsumeToken(req.GetEnrollmentToken())
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "enrollment failed: %v", err)
	}

	signedCert, err := s.enrollSvc.SignCSR(req.GetCsrPem())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to sign CSR: %v", err)
	}

	caCertPEM := s.enrollSvc.GetCACertPEM()

	nodeName := req.GetNodeName()
	if nodeName == "" {
		nodeName = "unnamed-agent"
	}

	// Create node record in database
	nodeID, err := s.enrollSvc.CreateNodeOnEnroll(ctx, nodeName, nodeGroupID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "enrolled but failed to create node record: %v", err)
	}

	log.Printf("Agent enrolled: name=%s group=%s node_id=%s", nodeName, nodeGroupID, nodeID)

	return &pb.EnrollResponse{
		NodeId:            nodeID,
		SignedCertPem:     signedCert,
		CaCertPem:         caCertPEM,
		AssignedNodeGroup: nodeGroupID,
	}, nil
}

// checkAdminAuth validates the x-admin-token metadata header.
func (s *EnrollmentServer) checkAdminAuth(ctx context.Context) error {
	if s.adminToken == "" {
		return status.Errorf(codes.FailedPrecondition, "BOR_ADMIN_TOKEN not configured on server")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing metadata")
	}

	tokens := md.Get("x-admin-token")
	if len(tokens) == 0 {
		return status.Errorf(codes.Unauthenticated, "x-admin-token header required")
	}

	if tokens[0] != s.adminToken {
		return status.Errorf(codes.PermissionDenied, "invalid admin token")
	}

	return nil
}
