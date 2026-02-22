// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/pki"
)

func newTestCA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
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
	svc := NewEnrollmentService(caCert, caKey, nil, nil)

	token, err := svc.CreateToken("test-group-id")
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
}

func TestEnrollmentService_CreateToken_EmptyGroupID(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, nil, nil)

	_, err := svc.CreateToken("")
	if err == nil {
		t.Error("CreateToken() should return error for empty group ID")
	}
}

func TestEnrollmentService_ConsumeToken(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, nil, nil)

	token, _ := svc.CreateToken("group-1")

	groupID, err := svc.ConsumeToken(token.Token)
	if err != nil {
		t.Fatalf("ConsumeToken() error = %v", err)
	}
	if groupID != "group-1" {
		t.Errorf("groupID = %q, want %q", groupID, "group-1")
	}

	// Second consume should fail (single-use)
	_, err = svc.ConsumeToken(token.Token)
	if err == nil {
		t.Error("ConsumeToken() should fail on second use")
	}
}

func TestEnrollmentService_ConsumeToken_Invalid(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, nil, nil)

	_, err := svc.ConsumeToken("nonexistent-token")
	if err == nil {
		t.Error("ConsumeToken() should return error for invalid token")
	}
}

func TestEnrollmentService_SignCSR(t *testing.T) {
	caCert, caKey := newTestCA(t)
	svc := NewEnrollmentService(caCert, caKey, nil, nil)

	agentKey, err := rsa.GenerateKey(rand.Reader, 2048)
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

	certPEM, err := svc.SignCSR(csrPEM)
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
	svc := NewEnrollmentService(caCert, caKey, nil, nil)

	caPEM := svc.GetCACertPEM()
	if len(caPEM) == 0 {
		t.Error("GetCACertPEM() returned empty")
	}

	block, _ := pem.Decode(caPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Error("GetCACertPEM() returned invalid PEM")
	}
}
