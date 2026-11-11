// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// CAChain is the Bor signing CA certificate together with the certificates
// of the authorities above it.
//
// A self-generated CA, or an operator-supplied CA file holding a single
// certificate, has no issuers. When the CA file holds a bundle, the first
// certificate is the signing certificate and the rest lead up to a
// self-signed root.
type CAChain struct {
	// Signing is the certificate Bor issues agent and server certificates
	// from.
	Signing *x509.Certificate
	// Issuers are the certificates above Signing, ordered from its direct
	// issuer up to the self-signed root. Empty when the CA file holds only
	// the signing certificate.
	Issuers []*x509.Certificate
}

// LoadCAChain reads the PEM bundle at certPath. See ParseCAChain.
func LoadCAChain(certPath string) (*CAChain, error) {
	data, err := os.ReadFile(certPath) //nolint:gosec // cert path is admin-configured
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	chain, err := ParseCAChain(data)
	if err != nil {
		return nil, fmt.Errorf("CA certificate file %s: %w", certPath, err)
	}
	return chain, nil
}

// ParseCAChain splits a PEM bundle into the signing certificate (first) and
// its issuers. A bundle with more than one certificate must be ordered from
// the signing certificate upwards, every certificate must be signed by the
// next one, and the last one must be a self-signed root.
func ParseCAChain(pemBytes []byte) (*CAChain, error) {
	var certs []*x509.Certificate
	for rest := pemBytes; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("unexpected PEM block %q, only certificates are allowed", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("certificate %d cannot be parsed: %w", len(certs)+1, err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no PEM certificate found")
	}

	chain := &CAChain{Signing: certs[0], Issuers: certs[1:]}
	if len(chain.Issuers) == 0 {
		return chain, nil
	}

	for i := 0; i < len(certs)-1; i++ {
		if err := certs[i].CheckSignatureFrom(certs[i+1]); err != nil {
			return nil, fmt.Errorf(
				"chain is out of order or broken: certificate %d (%s) is not issued by certificate %d (%s): %w; "+
					"list the signing certificate first, then each issuer up to the root",
				i+1, certs[i].Subject, i+2, certs[i+1].Subject, err)
		}
	}
	root := certs[len(certs)-1]
	if !bytes.Equal(root.RawSubject, root.RawIssuer) || root.CheckSignatureFrom(root) != nil {
		return nil, fmt.Errorf(
			"chain is incomplete: the last certificate (%s) is not a self-signed root; "+
				"add the missing certificates up to the root (%s is the next issuer)",
			root.Subject, root.Issuer)
	}
	return chain, nil
}

// ClientCAPool returns the pool that client (agent) certificates are verified
// against. It holds the signing certificate only: the authorities above it
// must never be trusted for client authentication, otherwise a certificate
// issued by any other CA under the same root would be accepted as an agent.
func (c *CAChain) ClientCAPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.Signing)
	return pool
}

// ServedIssuers returns the certificates a server presents after its own
// leaf certificate so that a client trusting only the external root can
// build the path: the signing certificate and its issuers without the root.
// It is empty when the CA has no known issuers.
func (c *CAChain) ServedIssuers() []*x509.Certificate {
	if len(c.Issuers) == 0 {
		return nil
	}
	served := []*x509.Certificate{c.Signing}
	return append(served, c.Issuers[:len(c.Issuers)-1]...)
}

// Validate checks that the signing certificate is usable as the Bor CA with
// the given private key: the key matches, the certificate is a CA that may
// sign certificates, the key meets the cryptographic baseline, and neither
// it nor its issuers restrict the extended key usages Bor issues (server
// and client authentication). Finally a throwaway client certificate is
// issued and verified against the chain, which also catches path length
// violations.
func (c *CAChain) Validate(key crypto.Signer) error {
	s := c.Signing

	pub, ok := s.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(key.Public()) {
		return fmt.Errorf("CA private key does not match the CA certificate (%s)", s.Subject)
	}
	if !s.BasicConstraintsValid || !s.IsCA {
		return fmt.Errorf("CA certificate (%s) is not a CA certificate (basic constraints CA flag is not set)", s.Subject)
	}
	if s.KeyUsage != 0 && s.KeyUsage&x509.KeyUsageCertSign == 0 {
		return fmt.Errorf("CA certificate (%s) lacks the certificate signing key usage", s.Subject)
	}
	if err := validateCSRPublicKey(s.PublicKey); err != nil {
		return fmt.Errorf("CA certificate (%s) key is below the baseline: %w", s.Subject, err)
	}
	for _, cert := range append([]*x509.Certificate{s}, c.Issuers...) {
		if err := allowsServerAndClientAuth(cert); err != nil {
			return err
		}
	}

	if err := c.probe(key, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil, nil); err != nil {
		return fmt.Errorf("CA chain cannot issue agent certificates: %w", err)
	}
	return nil
}

// CheckServerNames verifies that the chain may issue a server certificate
// for the given DNS names and IP addresses, i.e. that no name constraint of
// the signing certificate or its issuers excludes them.
func (c *CAChain) CheckServerNames(key crypto.Signer, dnsNames []string, ips []net.IP) error {
	if err := c.probe(key, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, dnsNames, ips); err != nil {
		return fmt.Errorf("CA chain cannot issue a server certificate for %v %v: %w", dnsNames, ips, err)
	}
	return nil
}

// allowsServerAndClientAuth rejects a CA certificate whose extended key
// usage extension would stop certificates below it from being used for
// server or client authentication.
func allowsServerAndClientAuth(cert *x509.Certificate) error {
	if len(cert.ExtKeyUsage) == 0 && len(cert.UnknownExtKeyUsage) == 0 {
		return nil
	}
	var server, client bool
	for _, eku := range cert.ExtKeyUsage {
		switch eku {
		case x509.ExtKeyUsageAny:
			return nil
		case x509.ExtKeyUsageServerAuth:
			server = true
		case x509.ExtKeyUsageClientAuth:
			client = true
		default:
		}
	}
	if !server || !client {
		return fmt.Errorf(
			"CA certificate (%s) restricts extended key usage and does not allow both server and client authentication",
			cert.Subject)
	}
	return nil
}

// probe issues a short-lived certificate from the signing certificate and
// verifies it against the chain the way a relying party would. The
// certificate is discarded.
func (c *CAChain) probe(key crypto.Signer, usage []x509.ExtKeyUsage, dnsNames []string, ips []net.IP) error {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate probe key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate probe serial number: %w", err)
	}

	// Verify at a moment every certificate of the chain has become valid, so
	// that the outcome does not depend on the wall clock: an expired CA is
	// reported by the regular expiry handling, not here.
	at := c.Signing.NotBefore
	for _, cert := range c.Issuers {
		if cert.NotBefore.After(at) {
			at = cert.NotBefore
		}
	}
	at = at.Add(time.Second)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "bor-ca-validation-probe"},
		NotBefore:    at.Add(-time.Second),
		NotAfter:     at.Add(time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usage,
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Signing, leafKey.Public(), key)
	if err != nil {
		return fmt.Errorf("failed to sign with the CA key: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("failed to parse probe certificate: %w", err)
	}

	roots := x509.NewCertPool()
	inter := x509.NewCertPool()
	if len(c.Issuers) == 0 {
		roots.AddCert(c.Signing)
	} else {
		roots.AddCert(c.Issuers[len(c.Issuers)-1])
		inter.AddCert(c.Signing)
		for _, cert := range c.Issuers[:len(c.Issuers)-1] {
			inter.AddCert(cert)
		}
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: inter,
		CurrentTime:   at,
		KeyUsages:     usage,
	})
	return err
}

// KeyAlgorithm names a public key for display, e.g. "ECDSA P-384".
func KeyAlgorithm(pub crypto.PublicKey) string {
	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		return "ECDSA " + key.Curve.Params().Name
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", key.N.BitLen())
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return fmt.Sprintf("%T", pub)
	}
}
