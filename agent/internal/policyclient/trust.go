// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// loadTrustPool reads the CA certificates the agent verifies the server
// against.
func loadTrustPool(caCertPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caCertPath) //nolint:gosec // G304: path comes from trusted config
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate %s: %w", caCertPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", caCertPath)
	}
	return pool, nil
}

// verifyWithTrustFile returns a tls.Config.VerifyConnection callback that
// verifies the server's certificate chain against the CA certificates
// currently in caCertPath, and that the certificate is valid for serverName
// (a host name or an IP address). It performs the verification
// crypto/tls does for a fixed RootCAs pool, with the pool read from the file
// for every handshake.
func verifyWithTrustFile(caCertPath, serverName string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("server presented no certificate")
		}
		if serverName == "" {
			return errors.New("no server name to verify the certificate against")
		}
		roots, err := loadTrustPool(caCertPath)
		if err != nil {
			return err
		}
		intermediates := x509.NewCertPool()
		for _, cert := range cs.PeerCertificates[1:] {
			intermediates.AddCert(cert)
		}
		_, err = cs.PeerCertificates[0].Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			DNSName:       serverName,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		return err
	}
}

// TrustBundleSHA256 returns the hex SHA-256 of the CA certificate file, the
// fingerprint the agent reports to the server. It is empty when the file
// cannot be read.
func TrustBundleSHA256(caCertPath string) string {
	data, err := os.ReadFile(caCertPath) //nolint:gosec // G304: path comes from trusted config
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// WriteTrustBundle replaces the CA certificate file with bundlePEM, the CA
// certificates the server says the agent must trust. The bundle is accepted
// only when it consists of parseable CA certificates; the file is replaced
// atomically, so a crash leaves either the old or the new content.
//
// The bundle comes from a server the agent has authenticated against its
// current CA certificates, over the mutually authenticated policy channel.
func WriteTrustBundle(caCertPath string, bundlePEM []byte) error {
	count := 0
	for rest := bundlePEM; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("trust bundle holds a %q block, only certificates are allowed", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("trust bundle certificate %d cannot be parsed: %w", count+1, err)
		}
		if !cert.IsCA {
			return fmt.Errorf("trust bundle certificate %d (%s) is not a CA certificate", count+1, cert.Subject)
		}
		count++
	}
	if count == 0 {
		return errors.New("trust bundle holds no certificate")
	}

	tmp, err := os.CreateTemp(filepath.Dir(caCertPath), ".ca-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary CA file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after the rename
	if _, err = tmp.Write(bundlePEM); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmpName, 0o644) //nolint:gosec // G302: CA certificates are public
	}
	if err != nil {
		return fmt.Errorf("failed to write CA certificates: %w", err)
	}
	if err = os.Rename(tmpName, caCertPath); err != nil {
		return fmt.Errorf("failed to replace %s: %w", caCertPath, err)
	}
	return nil
}
