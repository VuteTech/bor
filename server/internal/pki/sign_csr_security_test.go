// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
)

func makeCSR(t *testing.T, key any, cn string) []byte {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, key)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// TestSignCSR_RejectsWeakRSAKey verifies the FIPS/BSI key-strength baseline:
// an RSA-2048 key must be rejected (minimum is 3072).
func TestSignCSR_RejectsWeakRSAKey(t *testing.T) {
	dir := t.TempDir()
	caCertPath, caKeyPath, _ := EnsureCA(dir)
	caCert, caKey, _ := LoadCA(caCertPath, caKeyPath)

	weak, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	csr := makeCSR(t, weak, "weak-agent")

	if _, _, _, err := SignCSR(csr, caCert, caKey, "weak-agent"); err == nil {
		t.Fatal("SignCSR accepted an RSA-2048 key; want rejection")
	}
}

// TestSignCSR_OverridesCommonName verifies that the issued certificate's CN is
// forced to the server-assigned value, ignoring the CN embedded in the CSR.
// This prevents identity takeover via an attacker-chosen CSR subject.
func TestSignCSR_OverridesCommonName(t *testing.T) {
	dir := t.TempDir()
	caCertPath, caKeyPath, _ := EnsureCA(dir)
	caCert, caKey, _ := LoadCA(caCertPath, caKeyPath)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	// CSR claims to be "victim-node"; the server assigns "attacker-node".
	csr := makeCSR(t, key, "victim-node")

	certPEM, _, _, err := SignCSR(csr, caCert, caKey, "attacker-node")
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if cert.Subject.CommonName != "attacker-node" {
		t.Errorf("issued CN = %q, want the server-assigned %q (CSR CN must be ignored)",
			cert.Subject.CommonName, "attacker-node")
	}
}

// TestSignCSR_AcceptsP256 confirms a compliant ECDSA P-256 key is accepted.
func TestSignCSR_AcceptsP256(t *testing.T) {
	dir := t.TempDir()
	caCertPath, caKeyPath, _ := EnsureCA(dir)
	caCert, caKey, _ := LoadCA(caCertPath, caKeyPath)

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	csr := makeCSR(t, key, "good-agent")
	if _, _, _, err := SignCSR(csr, caCert, caKey, "good-agent"); err != nil {
		t.Fatalf("SignCSR rejected a compliant P-256 key: %v", err)
	}
}
