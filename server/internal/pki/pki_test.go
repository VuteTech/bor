// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureServerCert_SelfSigned(t *testing.T) {
	dir := t.TempDir()

	// Without CA: self-signed fallback
	certPath, keyPath, err := EnsureServerCert(dir, nil, nil)
	if err != nil {
		t.Fatalf("EnsureServerCert() error = %v", err)
	}
	if certPath != filepath.Join(dir, "ui.crt") {
		t.Errorf("certPath = %q, want %q", certPath, filepath.Join(dir, "ui.crt"))
	}
	if keyPath != filepath.Join(dir, "ui.key") {
		t.Errorf("keyPath = %q, want %q", keyPath, filepath.Join(dir, "ui.key"))
	}

	// Verify the certificate can be loaded
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("Failed to load generated cert/key: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	// Check SANs include localhost
	found := false
	for _, name := range parsed.DNSNames {
		if name == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Certificate missing localhost SAN")
	}

	// Check key usage
	if parsed.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("Certificate missing DigitalSignature key usage")
	}

	// Check extended key usage
	hasServerAuth := false
	for _, usage := range parsed.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
			break
		}
	}
	if !hasServerAuth {
		t.Error("Certificate missing ServerAuth extended key usage")
	}

	// Calling again should reuse existing cert (idempotent)
	certPath2, keyPath2, err := EnsureServerCert(dir, nil, nil)
	if err != nil {
		t.Fatalf("EnsureServerCert() second call error = %v", err)
	}
	if certPath2 != certPath || keyPath2 != keyPath {
		t.Error("Second call returned different paths")
	}
}

func TestEnsureServerCert_SignedByCA(t *testing.T) {
	caDir := t.TempDir()
	caCertPath, caKeyPath, err := EnsureCA(caDir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	caCert, caKey, err := LoadCA(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}

	dir := t.TempDir()
	certPath, keyPath, err := EnsureServerCert(dir, caCert, caKey)
	if err != nil {
		t.Fatalf("EnsureServerCert() error = %v", err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("Failed to load generated cert/key: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	// Verify the server cert is signed by the CA
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	_, err = parsed.Verify(x509.VerifyOptions{
		Roots: pool,
	})
	if err != nil {
		t.Errorf("Server cert should verify against CA: %v", err)
	}

	// Verify SANs
	found := false
	for _, name := range parsed.DNSNames {
		if name == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Certificate missing localhost SAN")
	}
}

// TestEnsureServerCert_RegeneratesWhenCAChanges verifies that if an
// existing server cert was self-signed (or signed by a different CA),
// EnsureServerCert regenerates it so it matches the current CA.
func TestEnsureServerCert_RegeneratesWhenCAChanges(t *testing.T) {
	dir := t.TempDir()

	// Step 1: Create a self-signed server cert (no CA)
	certPath, keyPath, err := EnsureServerCert(dir, nil, nil)
	if err != nil {
		t.Fatalf("EnsureServerCert(nil CA) error = %v", err)
	}
	oldCertPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("Failed to read old cert: %v", err)
	}

	// Step 2: Create a CA
	caDir := t.TempDir()
	caCertPath, caKeyPath, err := EnsureCA(caDir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	caCert, caKey, err := LoadCA(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}

	// Step 3: Call EnsureServerCert WITH the CA — should detect mismatch
	// and regenerate the cert.
	certPath2, keyPath2, err := EnsureServerCert(dir, caCert, caKey)
	if err != nil {
		t.Fatalf("EnsureServerCert(with CA) error = %v", err)
	}

	// Paths should be the same (same dir)
	if certPath2 != certPath || keyPath2 != keyPath {
		t.Error("Expected same paths after regeneration")
	}

	// The cert content should be DIFFERENT (regenerated)
	newCertPEM, err := os.ReadFile(certPath2)
	if err != nil {
		t.Fatalf("Failed to read regenerated cert: %v", err)
	}
	if string(newCertPEM) == string(oldCertPEM) {
		t.Error("Cert was NOT regenerated — should have been replaced when CA changed")
	}

	// The new cert should verify against the CA
	cert, err := tls.LoadX509KeyPair(certPath2, keyPath2)
	if err != nil {
		t.Fatalf("Failed to load regenerated cert: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse regenerated cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	_, err = parsed.Verify(x509.VerifyOptions{Roots: pool})
	if err != nil {
		t.Errorf("Regenerated cert should verify against CA: %v", err)
	}
}

func TestEnsureCA(t *testing.T) {
	dir := t.TempDir()

	certPath, keyPath, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	if certPath != filepath.Join(dir, "ca.crt") {
		t.Errorf("certPath = %q, want %q", certPath, filepath.Join(dir, "ca.crt"))
	}

	// Load and verify CA cert
	caCert, caKey, err := LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}
	if !caCert.IsCA {
		t.Error("CA certificate IsCA = false")
	}
	if caCert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA missing CertSign key usage")
	}
	_ = caKey

	// Idempotent
	certPath2, keyPath2, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() second call error = %v", err)
	}
	if certPath2 != certPath || keyPath2 != keyPath {
		t.Error("Second call returned different paths")
	}
}

func TestSignCSR(t *testing.T) {
	dir := t.TempDir()

	caCertPath, caKeyPath, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}

	caCert, caKey, err := LoadCA(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}

	// Generate a CSR
	agentKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate agent key: %v", err)
	}
	csrTmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "test-agent",
			Organization: []string{"Bor Agent"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTmpl, agentKey)
	if err != nil {
		t.Fatalf("Failed to create CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Sign the CSR
	certPEM, err := SignCSR(csrPEM, caCert, caKey)
	if err != nil {
		t.Fatalf("SignCSR() error = %v", err)
	}

	// Parse and verify the signed certificate
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

	hasClientAuth := false
	for _, usage := range signedCert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
			break
		}
	}
	if !hasClientAuth {
		t.Error("Signed cert missing ClientAuth extended key usage")
	}

	// Verify against CA
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	_, err = signedCert.Verify(x509.VerifyOptions{
		Roots:    pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		t.Errorf("Signed cert failed CA verification: %v", err)
	}
}

func TestSignCSR_InvalidPEM(t *testing.T) {
	dir := t.TempDir()
	caCertPath, caKeyPath, _ := EnsureCA(dir)
	caCert, caKey, _ := LoadCA(caCertPath, caKeyPath)

	_, err := SignCSR([]byte("not a PEM"), caCert, caKey)
	if err == nil {
		t.Error("SignCSR() should return error for invalid PEM")
	}
}

func TestLoadCACertPool(t *testing.T) {
	dir := t.TempDir()
	certPath, _, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}

	pool, err := LoadCACertPool(certPath)
	if err != nil {
		t.Fatalf("LoadCACertPool() error = %v", err)
	}
	if pool == nil {
		t.Error("LoadCACertPool() returned nil pool")
	}
}

func TestLoadCACertPool_MissingFile(t *testing.T) {
	_, err := LoadCACertPool("/nonexistent/ca.crt")
	if err == nil {
		t.Error("LoadCACertPool() should return error for missing file")
	}
}

func TestEnsureServerCert_FilePermissions(t *testing.T) {
	dir := t.TempDir()

	_, keyPath, err := EnsureServerCert(dir, nil, nil)
	if err != nil {
		t.Fatalf("EnsureServerCert() error = %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		t.Errorf("Key file has overly permissive mode: %o", perm)
	}
}
