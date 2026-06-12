// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/pki"
)

const enrollmentTokenTTL = 5 * time.Minute

// EnrollmentService manages enrollment tokens and agent certificate signing.
type EnrollmentService struct {
	mu     sync.Mutex
	tokens map[string]*models.EnrollmentToken

	caCert *x509.Certificate
	caKey  crypto.Signer

	nodeGroupSvc *NodeGroupService
	nodeSvc      *NodeService
	revokeRepo   *database.RevocationRepository
}

// NewEnrollmentService creates a new EnrollmentService.
func NewEnrollmentService(caCert *x509.Certificate, caKey crypto.Signer, nodeGroupSvc *NodeGroupService, nodeSvc *NodeService, revokeRepo *database.RevocationRepository) *EnrollmentService {
	return &EnrollmentService{
		tokens:       make(map[string]*models.EnrollmentToken),
		caCert:       caCert,
		caKey:        caKey,
		nodeGroupSvc: nodeGroupSvc,
		nodeSvc:      nodeSvc,
		revokeRepo:   revokeRepo,
	}
}

// CreateToken generates a short-lived, single-use enrollment token for a node group.
func (s *EnrollmentService) CreateToken(nodeGroupID string) (*models.EnrollmentToken, error) {
	if nodeGroupID == "" {
		return nil, fmt.Errorf("node_group_id is required")
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	token := &models.EnrollmentToken{
		Token:       hex.EncodeToString(b),
		NodeGroupID: nodeGroupID,
		ExpiresAt:   time.Now().Add(enrollmentTokenTTL),
		Used:        false,
	}

	s.mu.Lock()
	s.tokens[token.Token] = token
	s.mu.Unlock()

	return token, nil
}

// ConsumeToken validates and consumes an enrollment token. Returns the
// associated node group ID on success.
func (s *EnrollmentService) ConsumeToken(tokenStr string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[tokenStr]
	if !ok {
		return "", fmt.Errorf("invalid enrollment token")
	}
	if token.Used {
		return "", fmt.Errorf("enrollment token already used")
	}
	if time.Now().After(token.ExpiresAt) {
		delete(s.tokens, tokenStr)
		return "", fmt.Errorf("enrollment token expired")
	}

	token.Used = true
	delete(s.tokens, tokenStr)

	return token.NodeGroupID, nil
}

// SignCSR signs a PEM-encoded certificate signing request with the internal CA.
// The issued certificate's Common Name is forced to subjectCN (the
// server-assigned node name) so an enrolling agent cannot request an arbitrary
// identity. Returns the signed cert PEM, serial hex, and notAfter time.
func (s *EnrollmentService) SignCSR(csrPEM []byte, subjectCN string) (certPEM []byte, serial string, notAfter time.Time, err error) {
	return pki.SignCSR(csrPEM, s.caCert, s.caKey, subjectCN)
}

// SetNodeCertificate persists the cert serial and notAfter for an enrolled node.
func (s *EnrollmentService) SetNodeCertificate(ctx context.Context, nodeID, serial string, notAfter time.Time) error {
	return s.nodeSvc.UpdateNodeCertificate(ctx, nodeID, serial, notAfter)
}

// RenewCertificate signs a new CSR for an existing node (cert renewal).
// It replaces the node's cert record and clears any prior revocation for that node.
// nodeName is the authoritative identity (CN) carried over from the node's
// existing certificate; it is forced onto the renewed certificate.
func (s *EnrollmentService) RenewCertificate(ctx context.Context, nodeID, nodeName string, csrPEM []byte) ([]byte, error) {
	certPEM, serial, notAfter, err := pki.SignCSR(csrPEM, s.caCert, s.caKey, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to sign renewal CSR: %w", err)
	}
	if err := s.nodeSvc.UpdateNodeCertificate(ctx, nodeID, serial, notAfter); err != nil {
		return nil, fmt.Errorf("failed to update node certificate: %w", err)
	}
	// Clear any stale revocation so the new cert is accepted.
	if err := s.revokeRepo.DeleteByNodeID(ctx, nodeID); err != nil {
		return nil, fmt.Errorf("failed to clear revocations: %w", err)
	}
	return certPEM, nil
}

// RevokeCertificate revokes the certificate of a node by serial number.
func (s *EnrollmentService) RevokeCertificate(ctx context.Context, nodeID, serial, reason string) error {
	return s.revokeRepo.Revoke(ctx, nodeID, serial, reason)
}

// CreateNodeOnEnroll creates a Node record in the database for a newly
// enrolled agent. It rejects a name that already belongs to another node,
// because the node name is the authoritative mTLS identity (CN) and must be
// unique — otherwise a new enrollment could shadow an existing node's identity.
func (s *EnrollmentService) CreateNodeOnEnroll(ctx context.Context, nodeName, nodeGroupID string) (string, error) {
	existing, err := s.nodeSvc.GetNodeByName(ctx, nodeName)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing node: %w", err)
	}
	if existing != nil {
		return "", fmt.Errorf("a node named %q already exists; delete it before re-enrolling", nodeName)
	}

	node := &models.Node{
		Name: nodeName,
	}
	if err := s.nodeSvc.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("failed to create node: %w", err)
	}
	if nodeGroupID != "" {
		if err := s.nodeSvc.AddNodeToGroup(ctx, node.ID, nodeGroupID); err != nil {
			return "", fmt.Errorf("failed to assign node to group: %w", err)
		}
	}
	return node.ID, nil
}

// GetCACertPEM returns the CA certificate in PEM format.
func (s *EnrollmentService) GetCACertPEM() []byte {
	return pki.EncodeCertPEM(s.caCert)
}
