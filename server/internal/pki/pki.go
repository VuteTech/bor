// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package pki provides internal certificate authority and TLS certificate management for Bor.
package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureServerCert checks for an existing cert/key pair at dir/ui.crt
// and dir/ui.key. If they exist AND are signed by the provided CA,
// they are reused. If they do not exist, or were signed by a different
// CA (e.g. self-signed from a previous run), a new TLS server
// certificate is generated (ECDSA P-256, 365 days) and signed by the
// given CA.
//
// ECDSA P-256 satisfies FIPS 140-3, BSI TR-02102-1 (2024), ANSSI RGS,
// ENISA 2023, and ETSI TS 119 312 recommendations.
//
// The certificate always includes SANs for localhost, 127.0.0.1, ::1,
// and the system hostname. Additional DNS names or IP addresses can be
// provided via extraHostnames.
//
// When caCert/caKey are nil the certificate is self-signed (fallback
// for when no CA is available).
// Returns the paths to the cert and key files.
func EnsureServerCert(dir string, caCert *x509.Certificate, caKey crypto.Signer, extraHostnames []string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dir, "ui.crt")
	keyPath = filepath.Join(dir, "ui.key")

	if fileExists(certPath) && fileExists(keyPath) {
		if caCert != nil && !isSignedByCA(certPath, caCert) {
			// Existing cert is NOT signed by the current CA — regenerate.
			if err = os.Remove(certPath); err != nil {
				return "", "", fmt.Errorf("failed to remove old server cert %s: %w", certPath, err)
			}
			if err = os.Remove(keyPath); err != nil {
				return "", "", fmt.Errorf("failed to remove old server key %s: %w", keyPath, err)
			}
		} else {
			return certPath, keyPath, nil
		}
	}

	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("failed to create TLS autogen dir %s: %w", dir, err)
	}

	// ECDSA P-256: 128-bit security, FIPS 140-3 + BSI TR-02102-1 + ANSSI + ETSI approved.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ECDSA P-256 key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	hostname, _ := os.Hostname()

	dnsNames := []string{"localhost"}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	if hostname != "" {
		dnsNames = append(dnsNames, hostname)
	}

	for _, h := range extraHostnames {
		if ip := net.ParseIP(h); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		} else {
			dnsNames = append(dnsNames, h)
		}
	}

	tmpl := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Bor"},
			CommonName:   "Bor UI",
		},
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		// KeyUsageDigitalSignature only — KeyUsageKeyEncipherment is RSA-specific
		// and must not be set for ECDSA certs (ETSI EN 319 412, RFC 5480).
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	// Sign with CA if available, otherwise self-sign.
	issuer := tmpl
	var signingKey crypto.Signer = key
	if caCert != nil && caKey != nil {
		issuer = caCert
		signingKey = caKey
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, issuer, key.Public(), signingKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create server certificate: %w", err)
	}

	if err = writePEM(certPath, "CERTIFICATE", certDER); err != nil { //nolint:gocritic // sloppyReassign: named return err updated intentionally
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal server key: %w", err)
	}
	if err = writePEM(keyPath, "PRIVATE KEY", keyDER); err != nil { //nolint:gocritic // sloppyReassign: named return err updated intentionally
		return "", "", err
	}

	return certPath, keyPath, nil
}

// EnsureCA checks for an existing CA cert/key at dir/ca.crt and dir/ca.key.
// If they do not exist it generates a new CA (ECDSA P-384, 10 years).
//
// ECDSA P-384 provides 192-bit security — appropriate for a CA with a
// 10-year lifetime. Satisfies FIPS 140-3, BSI TR-02102-1, ANSSI RGS,
// and ETSI TS 119 312.
//
// Returns the paths to the CA cert and key files.
func EnsureCA(dir string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dir, "ca.crt")
	keyPath = filepath.Join(dir, "ca.key")

	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, nil
	}

	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("failed to create CA autogen dir %s: %w", dir, err)
	}

	// ECDSA P-384: 192-bit security, appropriate for a long-lived CA key.
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ECDSA P-384 CA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate CA serial number: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Bor"},
			CommonName:   "Bor Internal CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return "", "", fmt.Errorf("failed to create CA certificate: %w", err)
	}

	if err = writePEM(certPath, "CERTIFICATE", certDER); err != nil { //nolint:gocritic // sloppyReassign: named return err updated intentionally
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal CA key: %w", err)
	}
	if err = writePEM(keyPath, "PRIVATE KEY", keyDER); err != nil { //nolint:gocritic // sloppyReassign: named return err updated intentionally
		return "", "", err
	}

	return certPath, keyPath, nil
}

// LoadCA loads a CA certificate and private key from PEM files and returns
// the parsed certificate and a crypto.Signer for the private key.
// Accepts PKCS#8 ECDSA and RSA keys (the latter for operational flexibility
// when an externally-provided RSA CA is in use).
func LoadCA(certPath, keyPath string) (*x509.Certificate, crypto.Signer, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // cert path is admin-configured
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA cert: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // cert path is admin-configured
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read CA key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := parsePrivateKey(keyBlock)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	return caCert, caKey, nil
}

// SignCSR signs a PEM-encoded CSR with the given CA and returns the signed
// certificate PEM, the certificate serial number as a hex string, and the
// NotAfter time. The issued certificate is valid for 90 days with
// client-auth extended key usage.
func SignCSR(csrPEM []byte, caCert *x509.Certificate, caKey crypto.Signer) (certPEM []byte, serial string, notAfter time.Time, err error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, "", time.Time{}, fmt.Errorf("failed to decode CSR PEM")
	}

	csr, parseErr := x509.ParseCertificateRequest(block.Bytes)
	if parseErr != nil {
		return nil, "", time.Time{}, fmt.Errorf("failed to parse CSR: %w", parseErr)
	}
	if sigErr := csr.CheckSignature(); sigErr != nil {
		return nil, "", time.Time{}, fmt.Errorf("CSR signature verification failed: %w", sigErr)
	}

	serialNumber, randErr := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if randErr != nil {
		return nil, "", time.Time{}, fmt.Errorf("failed to generate serial number: %w", randErr)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		// KeyUsageDigitalSignature only — KeyUsageKeyEncipherment is RSA-specific
		// and must not appear in ECDSA client certs (RFC 5480, ETSI EN 319 412).
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, certErr := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
	if certErr != nil {
		return nil, "", time.Time{}, fmt.Errorf("failed to sign CSR: %w", certErr)
	}

	parsedCert, parseErr2 := x509.ParseCertificate(certDER)
	if parseErr2 != nil {
		return nil, "", time.Time{}, fmt.Errorf("failed to parse signed cert: %w", parseErr2)
	}
	serialHex := parsedCert.SerialNumber.Text(16)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), serialHex, tmpl.NotAfter, nil
}

// LoadCACertPool loads a CA certificate from the given path and returns
// a CertPool containing it.
func LoadCACertPool(certPath string) (*x509.CertPool, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // cert path is admin-configured
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		return nil, fmt.Errorf("failed to append CA cert to pool")
	}
	return pool, nil
}

// LoadTLSCert loads a TLS certificate from cert and key PEM files.
func LoadTLSCert(certPath, keyPath string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// EncodeCertPEM encodes a parsed certificate back to PEM format.
func EncodeCertPEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func writePEM(path, pemType string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path is admin-configured
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: data})
}

// parsePrivateKey parses a PKCS#8 private key from a PEM block.
// Supports ECDSA (generated by Bor) and RSA (for externally-provided CA keys).
func parsePrivateKey(block *pem.Block) (crypto.Signer, error) {
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("expected PKCS#8 PEM block type \"PRIVATE KEY\", got %q", block.Type)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		return k, nil
	case *rsa.PrivateKey:
		return k, nil
	default:
		return nil, fmt.Errorf("unsupported key type %T", key)
	}
}

// isSignedByCA loads a PEM certificate from path and verifies that it
// was issued by the given CA. Returns false on any error.
func isSignedByCA(certPath string, caCert *x509.Certificate) bool {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // cert path is admin-configured
	if err != nil {
		return false
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	_, err = cert.Verify(x509.VerifyOptions{Roots: pool})
	return err == nil
}

// loadCACert loads only the CA certificate from a PEM file.
// Used by pkcs11.go where the key comes from an HSM rather than a file.
//
//nolint:unused // used by pkcs11.go under the pkcs11 build tag
func loadCACert(certPath string) (*x509.Certificate, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // cert path is admin-configured
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert %s: %w", certPath, err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA cert PEM from %s", certPath)
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA cert: %w", err)
	}
	return caCert, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
