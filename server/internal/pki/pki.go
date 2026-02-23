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
// certificate is generated (RSA 2048, 365 days) and signed by the
// given CA.
//
// The certificate always includes SANs for localhost, 127.0.0.1, ::1,
// and the system hostname. Additional DNS names or IP addresses can be
// provided via extraHostnames — each entry is classified as an IP
// address if net.ParseIP succeeds, otherwise it is treated as a DNS name.
//
// When caCert/caKey are nil the certificate is self-signed (fallback
// for when no CA is available).
// Returns the paths to the cert and key files.
func EnsureServerCert(dir string, caCert *x509.Certificate, caKey *rsa.PrivateKey, extraHostnames []string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dir, "ui.crt")
	keyPath = filepath.Join(dir, "ui.key")

	if fileExists(certPath) && fileExists(keyPath) {
		if caCert != nil && !isSignedByCA(certPath, caCert) {
			// Existing cert is NOT signed by the current CA
			// (e.g. self-signed from before CA-signing was implemented).
			// Remove the old cert/key and regenerate below.
			if err := os.Remove(certPath); err != nil {
				return "", "", fmt.Errorf("failed to remove old server cert %s: %w", certPath, err)
			}
			if err := os.Remove(keyPath); err != nil {
				return "", "", fmt.Errorf("failed to remove old server key %s: %w", keyPath, err)
			}
		} else {
			return certPath, keyPath, nil
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("failed to create TLS autogen dir %s: %w", dir, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
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
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	// Sign with CA if available, otherwise self-sign.
	issuer := tmpl
	signingKey := key
	if caCert != nil && caKey != nil {
		issuer = caCert
		signingKey = caKey
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, issuer, &key.PublicKey, signingKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create server certificate: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", certDER); err != nil {
		return "", "", err
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	if err := writePEM(keyPath, "RSA PRIVATE KEY", keyDER); err != nil {
		return "", "", err
	}

	return certPath, keyPath, nil
}

// EnsureCA checks for an existing CA cert/key at dir/ca.crt and dir/ca.key.
// If they do not exist it generates a new CA (RSA 2048, 10 years).
// Returns the paths to the CA cert and key files.
func EnsureCA(dir string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dir, "ca.crt")
	keyPath = filepath.Join(dir, "ca.key")

	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("failed to create CA autogen dir %s: %w", dir, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate CA RSA key: %w", err)
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

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("failed to create CA certificate: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", certDER); err != nil {
		return "", "", err
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	if err := writePEM(keyPath, "RSA PRIVATE KEY", keyDER); err != nil {
		return "", "", err
	}

	return certPath, keyPath, nil
}

// LoadCA loads a CA certificate and private key from PEM files and returns
// the parsed certificate, private key, and a tls.Certificate.
func LoadCA(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
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

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read CA key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	return caCert, caKey, nil
}

// SignCSR signs a PEM-encoded CSR with the given CA and returns the signed
// certificate PEM. The issued certificate is valid for 365 days with
// client-auth extended key usage.
func SignCSR(csrPEM []byte, caCert *x509.Certificate, caKey *rsa.PrivateKey) ([]byte, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature verification failed: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               csr.Subject,
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), nil
}

// LoadCACertPool loads a CA certificate from the given path and returns
// a CertPool containing it.
func LoadCACertPool(certPath string) (*x509.CertPool, error) {
	certPEM, err := os.ReadFile(certPath)
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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer f.Close()

	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: data})
}

// isSignedByCA loads a PEM certificate from path and verifies that it
// was issued by the given CA. Returns false on any error (file missing,
// parse failure, verification failure).
func isSignedByCA(certPath string, caCert *x509.Certificate) bool {
	certPEM, err := os.ReadFile(certPath)
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
