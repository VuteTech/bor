// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	enrollpb "github.com/VuteTech/Bor/server/pkg/grpc/enrollment"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// EnrollmentPaths holds the paths to persisted enrollment artifacts.
type EnrollmentPaths struct {
	CertFile string // agent client certificate (signed by server CA)
	KeyFile  string // agent private key
	CACert   string // server CA certificate
}

// DefaultPaths returns the standard paths inside the given data directory.
func DefaultPaths(dataDir string) EnrollmentPaths {
	return EnrollmentPaths{
		CertFile: filepath.Join(dataDir, "agent.crt"),
		KeyFile:  filepath.Join(dataDir, "agent.key"),
		CACert:   filepath.Join(dataDir, "ca.crt"),
	}
}

// IsEnrolled returns true if the agent cert and key exist on disk.
func IsEnrolled(paths EnrollmentPaths) bool {
	_, errC := os.Stat(paths.CertFile)
	_, errK := os.Stat(paths.KeyFile)
	_, errCA := os.Stat(paths.CACert)
	return errC == nil && errK == nil && errCA == nil
}

// Enroll performs the one-time enrollment flow:
//  1. Generate an RSA 2048 key pair.
//  2. Create a CSR with the agent's node name.
//  3. Connect to the server (TLS, optionally skip-verify for self-signed).
//  4. Call EnrollmentService.Enroll with the token + CSR.
//  5. Persist the signed cert, private key, and CA cert to disk.
func Enroll(serverAddr, token, nodeName string, insecureSkipVerify bool, paths EnrollmentPaths) error {
	if err := os.MkdirAll(filepath.Dir(paths.CertFile), 0o700); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	// 1. Generate RSA key pair
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// 2. Create CSR
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   nodeName,
			Organization: []string{"Bor Agent"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// 3. Connect to server with TLS (no client cert yet — this is the bootstrap call)
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify,
	}
	if insecureSkipVerify {
		log.Println("WARNING: server certificate verification disabled for enrollment")
	}

	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	)
	if err != nil {
		return fmt.Errorf("failed to connect for enrollment: %w", err)
	}
	defer conn.Close()

	// 4. Call Enroll RPC
	client := enrollpb.NewEnrollmentServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Enroll(ctx, &enrollpb.EnrollRequest{
		EnrollmentToken: token,
		CsrPem:          csrPEM,
		NodeName:        nodeName,
	})
	if err != nil {
		return fmt.Errorf("enrollment RPC failed: %w", err)
	}

	log.Printf("Enrolled successfully: node_group=%s", resp.GetAssignedNodeGroup())

	// 5. Persist artifacts
	if err := writeFile(paths.KeyFile, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600); err != nil {
		return fmt.Errorf("failed to save agent key: %w", err)
	}

	if err := writeFile(paths.CertFile, resp.GetSignedCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save agent cert: %w", err)
	}

	if err := writeFile(paths.CACert, resp.GetCaCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save CA cert: %w", err)
	}

	return nil
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}
