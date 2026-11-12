// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/pki"
)

const enrollmentTokenTTL = 5 * time.Minute

// EnrollmentTokenStore persists pending enrollment tokens so they survive a
// server restart. Implemented by database.EnrollmentTokenRepository.
type EnrollmentTokenStore interface {
	Create(ctx context.Context, tokenHash, nodeGroupID string, expiresAt time.Time) error
	Consume(ctx context.Context, tokenHash string) (nodeGroupID string, expiresAt time.Time, found bool, err error)
}

// EnrollmentService manages enrollment tokens and agent certificate signing.
type EnrollmentService struct {
	tokenStore EnrollmentTokenStore

	caCert *x509.Certificate
	caKey  crypto.Signer
	// agentTrust overrides the CA certificate handed to agents; see
	// SetAgentTrustSource.
	agentTrust func() []byte

	nodeGroupSvc *NodeGroupService
	nodeSvc      *NodeService
	revokeRepo   *database.RevocationRepository
}

// NewEnrollmentService creates a new EnrollmentService.
func NewEnrollmentService(caCert *x509.Certificate, caKey crypto.Signer, tokenStore EnrollmentTokenStore, nodeGroupSvc *NodeGroupService, nodeSvc *NodeService, revokeRepo *database.RevocationRepository) *EnrollmentService {
	return &EnrollmentService{
		tokenStore:   tokenStore,
		caCert:       caCert,
		caKey:        caKey,
		nodeGroupSvc: nodeGroupSvc,
		nodeSvc:      nodeSvc,
		revokeRepo:   revokeRepo,
	}
}

// hashToken returns the hex SHA-256 of a token. Only the hash is persisted,
// so a database compromise does not yield usable enrollment tokens.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateToken generates a short-lived, single-use enrollment token for a node group.
func (s *EnrollmentService) CreateToken(ctx context.Context, nodeGroupID string) (*models.EnrollmentToken, error) {
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

	if err := s.tokenStore.Create(ctx, hashToken(token.Token), nodeGroupID, token.ExpiresAt); err != nil {
		return nil, fmt.Errorf("failed to store enrollment token: %w", err)
	}

	return token, nil
}

// ConsumeToken validates and consumes an enrollment token. Returns the
// associated node group ID on success. Consumption is atomic (the store
// deletes the token in the same operation that returns it), so a token can
// never be used twice even under concurrent enrollment attempts.
func (s *EnrollmentService) ConsumeToken(ctx context.Context, tokenStr string) (string, error) {
	nodeGroupID, expiresAt, found, err := s.tokenStore.Consume(ctx, hashToken(tokenStr))
	if err != nil {
		return "", fmt.Errorf("failed to consume enrollment token: %w", err)
	}
	if !found {
		return "", fmt.Errorf("invalid enrollment token")
	}
	if time.Now().After(expiresAt) {
		return "", fmt.Errorf("enrollment token expired")
	}

	return nodeGroupID, nil
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

// ErrStaleCertificate is returned by RenewCertificate when the node
// authenticated with a certificate it no longer holds.
var ErrStaleCertificate = database.ErrStaleCertificate

// RenewCertificate signs a new CSR for an existing node (cert renewal).
// nodeName is the authoritative identity (CN) carried over from the node's
// existing certificate; it is forced onto the renewed certificate.
// presentedSerial is the certificate the node authenticated with. That
// certificate stays valid until the node first connects with the new one
// (ConfirmCertificateInUse), so a lost response can be retried; any
// certificate it supersedes is revoked. Earlier revocations are kept.
func (s *EnrollmentService) RenewCertificate(ctx context.Context, nodeID, nodeName, presentedSerial string, csrPEM []byte) ([]byte, error) {
	certPEM, serial, notAfter, err := pki.SignCSR(csrPEM, s.caCert, s.caKey, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to sign renewal CSR: %w", err)
	}
	revoked, err := s.revokeRepo.RotateCertificate(ctx, nodeID, presentedSerial, serial, notAfter)
	if err != nil {
		return nil, fmt.Errorf("failed to record renewed certificate: %w", err)
	}
	for _, r := range revoked {
		log.Printf("Revoked superseded certificate %s of node %s", r, nodeName)
	}
	return certPEM, nil
}

// ConfirmCertificateInUse is called when a node connects with the
// certificate presentedSerial. Once the renewed certificate is in use, the
// one it replaced is revoked.
func (s *EnrollmentService) ConfirmCertificateInUse(ctx context.Context, nodeID, presentedSerial string) error {
	revoked, err := s.revokeRepo.ConfirmCertificateInUse(ctx, nodeID, presentedSerial)
	if err != nil {
		return err
	}
	if revoked != "" {
		log.Printf("Node %s connected with its renewed certificate; revoked the previous certificate %s", nodeID, revoked)
	}
	return nil
}

// IsRevoked reports whether the certificate serial has been revoked.
func (s *EnrollmentService) IsRevoked(ctx context.Context, serial string) (bool, error) {
	return s.revokeRepo.IsRevoked(ctx, serial)
}

// RevokeNode revokes every certificate the node holds: the current one and,
// shortly after a renewal, the one it replaced. It returns the serials.
func (s *EnrollmentService) RevokeNode(ctx context.Context, nodeID, reason string) ([]string, error) {
	return s.revokeRepo.RevokeNode(ctx, nodeID, reason)
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

// SetAgentTrustSource sets where the CA certificates agents receive at
// enrollment and verify the server against come from. It is asked on every
// enrollment, so it can follow a CA replacement. Without it agents get the
// signing certificate. Call it before the service handles requests.
func (s *EnrollmentService) SetAgentTrustSource(source func() []byte) {
	s.agentTrust = source
}

// GetCACertPEM returns the CA certificates agents trust, in PEM format: the
// signing certificate, or the bundle of SetAgentTrustSource.
func (s *EnrollmentService) GetCACertPEM() []byte {
	if s.agentTrust != nil {
		if trust := s.agentTrust(); len(trust) > 0 {
			return trust
		}
	}
	return pki.EncodeCertPEM(s.caCert)
}
