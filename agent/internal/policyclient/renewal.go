// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// CertExpiringSoon returns true if the certificate at certPath expires
// within the given threshold duration.
func CertExpiringSoon(certPath string, threshold time.Duration) (bool, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // G304: path comes from trusted config
	if err != nil {
		return false, fmt.Errorf("failed to read cert %s: %w", certPath, err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false, fmt.Errorf("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse cert: %w", err)
	}
	return time.Until(cert.NotAfter) < threshold, nil
}

// RenewCertificate performs in-place certificate renewal:
//  1. Generates a new ECDSA P-256 key pair (FIPS 140-3 / BSI TR-02102-1 approved).
//  2. Creates a CSR with the same CN as the existing cert.
//  3. Calls the RenewCertificate RPC (authenticated with the current cert).
//  4. Atomically replaces key + cert on disk.
func RenewCertificate(serverAddr, caCertPath, certPath, keyPath string) error {
	// Load existing cert to extract CN.
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // G304: path comes from trusted config
	if err != nil {
		return fmt.Errorf("failed to read existing cert: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to decode existing cert")
	}
	existing, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse existing cert: %w", err)
	}
	nodeName := existing.Subject.CommonName

	log.Printf("Renewing certificate for node %s (expires %s)", nodeName, existing.NotAfter.Format("2006-01-02"))

	// Generate new ECDSA P-256 key pair.
	newKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate new ECDSA P-256 key: %w", err)
	}

	// Create CSR with same CN.
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   nodeName,
			Organization: []string{"Bor Agent"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, newKey)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Connect with existing cert (mTLS).
	caPEM, err := os.ReadFile(caCertPath) //nolint:gosec // G304: path comes from trusted config
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("failed to parse CA cert")
	}
	clientCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to load existing client cert: %w", err)
	}
	// TLS 1.3 minimum: renewal connects to the agent-only mTLS port (8444).
	tlsCfg := &tls.Config{
		RootCAs:          caPool,
		Certificates:     []tls.Certificate{clientCert},
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
	}
	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	)
	if err != nil {
		return fmt.Errorf("failed to connect for renewal: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := pb.NewPolicyServiceClient(conn).RenewCertificate(ctx, &pb.RenewCertificateRequest{
		CsrPem: csrPEM,
	})
	if err != nil {
		return fmt.Errorf("RenewCertificate RPC failed: %w", err)
	}

	// Write new key + cert to disk (key first, then cert).
	newKeyDER, err := x509.MarshalPKCS8PrivateKey(newKey)
	if err != nil {
		return fmt.Errorf("failed to marshal new agent key: %w", err)
	}
	newKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: newKeyDER,
	})
	if err := writeFile(keyPath, newKeyPEM, 0o600); err != nil {
		return fmt.Errorf("failed to save new agent key: %w", err)
	}
	if err := writeFile(certPath, resp.GetSignedCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save renewed cert: %w", err)
	}

	log.Printf("Certificate renewed successfully for node %s", nodeName)
	return nil
}
