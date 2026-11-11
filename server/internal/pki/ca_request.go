// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CAStore is where the CA certificate and key live on disk.
type CAStore struct {
	// CertPath is the PEM file of the signing certificate, optionally
	// followed by its issuers.
	CertPath string
	// KeyPath is the PEM file of the private key. Empty when the key lives
	// in a PKCS#11 token.
	KeyPath string
}

// RequestPath is where the pending certificate signing request is kept: next
// to the certificate, with the extension .csr.
func (s CAStore) RequestPath() string {
	return strings.TrimSuffix(s.CertPath, filepath.Ext(s.CertPath)) + ".csr"
}

// PreviousPath is where the certificate file replaced by the last install is
// kept.
func (s CAStore) PreviousPath() string { return s.CertPath + ".previous" }

// RequestPending reports whether a signing request was created for a CA
// that has no certificate yet. The server must not start in this state: it
// would otherwise generate a self-signed CA in place of the requested one.
func (s CAStore) RequestPending() bool {
	return fileExists(s.RequestPath()) && !fileExists(s.CertPath)
}

// LoadOrCreateCAKey loads the CA private key at keyPath, or generates an
// ECDSA P-384 key there (mode 0600) when the file does not exist.
func LoadOrCreateCAKey(keyPath string) (key crypto.Signer, created bool, err error) {
	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // key path is admin-configured
	switch {
	case err == nil:
		block, _ := pem.Decode(keyPEM)
		if block == nil {
			return nil, false, fmt.Errorf("failed to decode CA key PEM in %s", keyPath)
		}
		key, err = parsePrivateKey(block)
		if err != nil {
			return nil, false, fmt.Errorf("failed to parse CA key %s: %w", keyPath, err)
		}
		return key, false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, false, fmt.Errorf("failed to read CA key: %w", err)
	}

	if err = os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, false, fmt.Errorf("failed to create CA directory: %w", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, false, fmt.Errorf("failed to generate ECDSA P-384 CA key: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal CA key: %w", err)
	}
	if writeErr := writePEM(keyPath, "PRIVATE KEY", keyDER); writeErr != nil {
		return nil, false, writeErr
	}
	return ecKey, true, nil
}

// CAManager has the CA certified by an external authority: it creates
// certificate signing requests for the CA key and installs the certificate
// the authority issued. The key never leaves the server (or the token).
type CAManager struct {
	Store CAStore
	Key   crypto.Signer
}

// Current returns the certificate chain on disk, or nil when the CA has no
// certificate yet.
func (m *CAManager) Current() (*CAChain, error) {
	if !fileExists(m.Store.CertPath) {
		return nil, nil //nolint:nilnil // "no certificate yet" is not an error
	}
	return LoadCAChain(m.Store.CertPath)
}

var (
	oidExtBasicConstraints = asn1.ObjectIdentifier{2, 5, 29, 19}
	oidExtKeyUsage         = asn1.ObjectIdentifier{2, 5, 29, 15}
)

// CreateRequest returns a PEM certificate signing request for the CA key
// and stores it at Store.RequestPath().
//
// The subject is commonName with organization "Bor". When commonName is
// empty and the CA already has a certificate, that certificate's subject is
// requested unchanged, so a certificate that keeps the key and the subject
// can replace it without re-enrolling agents.
//
// The request asks for a CA certificate that can sign only end-entity
// certificates (basic constraints CA with path length 0, key usage
// certificate and CRL signing). The authority decides what it grants.
func (m *CAManager) CreateRequest(commonName string) ([]byte, error) {
	if err := validateCSRPublicKey(m.Key.Public()); err != nil {
		return nil, fmt.Errorf("CA key is below the baseline: %w", err)
	}

	tmpl := &x509.CertificateRequest{}
	switch commonName = strings.TrimSpace(commonName); {
	case commonName != "":
		tmpl.Subject = pkix.Name{Organization: []string{"Bor"}, CommonName: commonName}
	default:
		current, err := m.Current()
		if err != nil {
			return nil, err
		}
		if current != nil {
			tmpl.RawSubject = current.Signing.RawSubject
		} else {
			tmpl.Subject = pkix.Name{Organization: []string{"Bor"}, CommonName: "Bor CA"}
		}
	}

	basicConstraints, err := asn1.Marshal(struct {
		IsCA       bool
		MaxPathLen int
	}{IsCA: true, MaxPathLen: 0})
	if err != nil {
		return nil, fmt.Errorf("failed to encode basic constraints: %w", err)
	}
	// keyCertSign (bit 5) and cRLSign (bit 6).
	keyUsage, err := asn1.Marshal(asn1.BitString{Bytes: []byte{0x06}, BitLength: 7})
	if err != nil {
		return nil, fmt.Errorf("failed to encode key usage: %w", err)
	}
	tmpl.ExtraExtensions = []pkix.Extension{
		{Id: oidExtBasicConstraints, Critical: true, Value: basicConstraints},
		{Id: oidExtKeyUsage, Critical: true, Value: keyUsage},
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, m.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to create the certificate signing request: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})

	if err = os.MkdirAll(filepath.Dir(m.Store.RequestPath()), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create CA directory: %w", err)
	}
	if err = os.WriteFile(m.Store.RequestPath(), csrPEM, 0o600); err != nil {
		return nil, fmt.Errorf("failed to store the signing request: %w", err)
	}
	return csrPEM, nil
}

// CAInstallResult describes an installed CA certificate.
type CAInstallResult struct {
	Chain *CAChain
	// Replaced is true when a certificate was in place before.
	Replaced bool
	// SameIdentity is true when the replaced certificate had the same key
	// and the same subject. Certificates issued before and after the
	// replacement then verify against either CA certificate, so agents
	// that pin the replaced one keep working while it is valid.
	SameIdentity bool
}

// Install validates the certificate an authority issued for the CA key and
// writes it to Store.CertPath. bundlePEM is the signing certificate,
// optionally followed by its issuers up to the root (see ParseCAChain). A
// certificate already in place is kept at Store.PreviousPath(). The pending
// signing request is removed.
//
// A running server keeps using the certificate it loaded at startup.
func (m *CAManager) Install(bundlePEM []byte) (*CAInstallResult, error) {
	chain, err := ParseCAChain(bundlePEM)
	if err != nil {
		return nil, err
	}
	if validateErr := chain.Validate(m.Key); validateErr != nil {
		return nil, validateErr
	}

	res := &CAInstallResult{Chain: chain}
	mode := os.FileMode(0o600)
	if old, readErr := os.ReadFile(m.Store.CertPath); readErr == nil {
		res.Replaced = true
		if info, statErr := os.Stat(m.Store.CertPath); statErr == nil {
			mode = info.Mode().Perm()
		}
		if previous, parseErr := ParseCAChain(old); parseErr == nil {
			prevPub, ok := previous.Signing.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
			res.SameIdentity = ok && prevPub.Equal(chain.Signing.PublicKey) &&
				bytes.Equal(previous.Signing.RawSubject, chain.Signing.RawSubject)
		}
		if err = os.WriteFile(m.Store.PreviousPath(), old, mode); err != nil { //nolint:gosec // path derives from the admin-configured CA certificate path
			return nil, fmt.Errorf("failed to keep the previous CA certificate: %w", err)
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read the current CA certificate: %w", readErr)
	}

	// Normalize: only the certificates, in order.
	out := EncodeCertPEM(chain.Signing)
	for _, issuer := range chain.Issuers {
		out = append(out, EncodeCertPEM(issuer)...)
	}
	if err = os.MkdirAll(filepath.Dir(m.Store.CertPath), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create CA directory: %w", err)
	}
	tmp := m.Store.CertPath + ".tmp"
	if err = os.WriteFile(tmp, out, mode); err != nil {
		return nil, fmt.Errorf("failed to write the CA certificate: %w", err)
	}
	if err = os.Rename(tmp, m.Store.CertPath); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("failed to install the CA certificate: %w", err)
	}
	if err = os.Remove(m.Store.RequestPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("CA certificate installed, but the signing request could not be removed: %w", err)
	}
	return res, nil
}
