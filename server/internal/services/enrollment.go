// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/pki"
)

const enrollmentTokenTTL = 5 * time.Minute

// EnrollmentService manages enrollment tokens and agent certificate signing.
type EnrollmentService struct {
	mu     sync.Mutex
	tokens map[string]*models.EnrollmentToken

	caCert *x509.Certificate
	caKey  *rsa.PrivateKey

	nodeGroupSvc *NodeGroupService
	nodeSvc      *NodeService
}

// NewEnrollmentService creates a new EnrollmentService.
func NewEnrollmentService(caCert *x509.Certificate, caKey *rsa.PrivateKey, nodeGroupSvc *NodeGroupService, nodeSvc *NodeService) *EnrollmentService {
	return &EnrollmentService{
		tokens:       make(map[string]*models.EnrollmentToken),
		caCert:       caCert,
		caKey:        caKey,
		nodeGroupSvc: nodeGroupSvc,
		nodeSvc:      nodeSvc,
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
func (s *EnrollmentService) SignCSR(csrPEM []byte) ([]byte, error) {
	return pki.SignCSR(csrPEM, s.caCert, s.caKey)
}

// CreateNodeOnEnroll creates a Node record in the database for a newly
// enrolled agent.
func (s *EnrollmentService) CreateNodeOnEnroll(ctx context.Context, nodeName, nodeGroupID string) (string, error) {
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
