// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/pki"
)

// fakeTokenStore is an in-memory EnrollmentTokenStore for tests. It mirrors
// the atomic delete-on-consume semantics of the database implementation.
type fakeTokenStore struct {
	tokens map[string]fakeTokenRow
}

type fakeTokenRow struct {
	nodeGroupID string
	expiresAt   time.Time
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: make(map[string]fakeTokenRow)}
}

func (f *fakeTokenStore) Create(_ context.Context, tokenHash, nodeGroupID string, expiresAt time.Time) error {
	f.tokens[tokenHash] = fakeTokenRow{nodeGroupID: nodeGroupID, expiresAt: expiresAt}
	return nil
}

func (f *fakeTokenStore) Consume(_ context.Context, tokenHash string) (nodeGroupID string, expiresAt time.Time, found bool, err error) {
	row, ok := f.tokens[tokenHash]
	if !ok {
		return "", time.Time{}, false, nil
	}
	delete(f.tokens, tokenHash)
	return row.nodeGroupID, row.expiresAt, true, nil
}

func newTestCA(t *testing.T) (*x509.Certificate, crypto.Signer) {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath, err := pki.EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	cert, key, err := pki.LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}
	return cert, key
}

func TestEnrollmentService_CreateToken(t *testing.T) {
	caCert, caKey := newTestCA(t)
	store := newFakeTokenStore()
	svc := NewEnrollmentService(caCert, caKey, store, nil, nil, nil)

	token, err := svc.CreateToken(context.Background(), "test-group-id")
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if token.Token == "" {
		t.Error("CreateToken() returned empty token")
	}
	if token.NodeGroupID != "test-group-id" {
		t.Errorf("NodeGroupID = %q, want %q", token.NodeGroupID, "test-group-id")
	}
	if token.ExpiresAt.Before(time.Now()) {
		t.Error("Token already expired")
	}

	// Only the hash reaches the store, never the plaintext token.
	if _, ok := store.tokens[token.Token]; ok {
		t.Error("plaintext token was persisted to the store")
	}
	if _, ok := store.tokens[hashToken(token.Token)]; !ok {
		t.Error("token hash not found in the store")
	}
}

func TestEnrollmentService_CreateToken_EmptyGroupID(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, newFakeTokenStore(), nil, nil, nil)

	_, err := svc.CreateToken(context.Background(), "")
	if err == nil {
		t.Error("CreateToken() should return error for empty group ID")
	}
}

func TestEnrollmentService_ConsumeToken(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, newFakeTokenStore(), nil, nil, nil)

	token, _ := svc.CreateToken(context.Background(), "group-1")

	groupID, err := svc.ConsumeToken(context.Background(), token.Token)
	if err != nil {
		t.Fatalf("ConsumeToken() error = %v", err)
	}
	if groupID != "group-1" {
		t.Errorf("groupID = %q, want %q", groupID, "group-1")
	}

	// Second consume should fail (single-use)
	_, err = svc.ConsumeToken(context.Background(), token.Token)
	if err == nil {
		t.Error("ConsumeToken() should fail on second use")
	}
}

func TestEnrollmentService_ConsumeToken_Invalid(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, newFakeTokenStore(), nil, nil, nil)

	_, err := svc.ConsumeToken(context.Background(), "nonexistent-token")
	if err == nil {
		t.Error("ConsumeToken() should return error for invalid token")
	}
}

func TestEnrollmentService_ConsumeToken_Expired(t *testing.T) {
	caCert, caKey := newTestCA(t)
	store := newFakeTokenStore()
	svc := NewEnrollmentService(caCert, caKey, store, nil, nil, nil)

	token, err := svc.CreateToken(context.Background(), "group-1")
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	// Backdate the stored expiry past the TTL.
	hash := hashToken(token.Token)
	row := store.tokens[hash]
	row.expiresAt = time.Now().Add(-time.Minute)
	store.tokens[hash] = row

	_, err = svc.ConsumeToken(context.Background(), token.Token)
	if err == nil {
		t.Fatal("ConsumeToken() should fail for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %v, want mention of expiry", err)
	}
}

func TestEnrollmentService_SignCSR(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, newFakeTokenStore(), nil, nil, nil)

	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate agent key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "test-agent"},
	}, agentKey)
	if err != nil {
		t.Fatalf("Failed to create CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	certPEM, _, _, err := svc.SignCSR(csrPEM, "test-agent")
	if err != nil {
		t.Fatalf("SignCSR() error = %v", err)
	}
	if len(certPEM) == 0 {
		t.Error("SignCSR() returned empty cert")
	}

	// Verify the signed cert
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("Failed to decode signed cert PEM")
		return // unreachable, but satisfies staticcheck SA5011
	}
	signedCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse signed cert: %v", err)
	}
	if signedCert.Subject.CommonName != "test-agent" {
		t.Errorf("Subject CN = %q, want %q", signedCert.Subject.CommonName, "test-agent")
	}
}

func TestEnrollmentService_GetCACertPEM(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, newFakeTokenStore(), nil, nil, nil)

	caPEM := svc.GetCACertPEM()
	if len(caPEM) == 0 {
		t.Error("GetCACertPEM() returned empty")
	}

	block, _ := pem.Decode(caPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Error("GetCACertPEM() returned invalid PEM")
	}
}
