// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

//go:build pkcs11

// Package pki provides PKI helpers with optional PKCS#11 HSM support.
//
// This file is compiled only when the 'pkcs11' build tag is set:
//
//	make server-pkcs11
//
// Before building with HSM support, add the dependency:
//
//	cd server && go get github.com/ThalesIgnite/crypto11
//
// Runtime requirements:
//   - A PKCS#11 shared library (.so) for your HSM or software token
//     (e.g. SoftHSMv2: /usr/lib/softhsm/libsofthsm2.so)
//   - The HSM token must be initialised and the CA key must exist (or be
//     generated automatically on first run).
//
// Recommended configuration (env vars, not YAML, to protect the PIN):
//
//	BOR_CA_PKCS11_LIB=/usr/lib/softhsm/libsofthsm2.so
//	BOR_CA_PKCS11_TOKEN_LABEL=bor-ca
//	BOR_CA_PKCS11_KEY_LABEL=bor-ca-key
//	BOR_CA_PKCS11_PIN=<token-pin>
//	BOR_CA_CERT_FILE=/var/lib/bor/pki/ca/ca.crt   # cert on disk, key in HSM

package pki

import (
	"crypto"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/ThalesIgnite/crypto11"
)

// EnsureCAWithHSM loads (or creates) the CA certificate at certPath whose
// private key lives in the PKCS#11 HSM identified by lib/tokenLabel/keyLabel/pin.
//
// Behaviour:
//   - If a key with keyLabel already exists on the token and certPath exists,
//     both are loaded and returned.
//   - If the key exists but certPath is missing, a new self-signed CA
//     certificate is generated (ECDSA P-384, 10 years) and written to certPath.
//   - If no key with keyLabel is found on the token, a new ECDSA P-384 key is
//     generated on the HSM, then a CA certificate is created and written to certPath.
//
// The returned crypto.Signer wraps the HSM key; signing operations happen
// inside the HSM. The PKCS#11 context is intentionally kept open for the
// process lifetime so the signer remains valid.
func EnsureCAWithHSM(certPath, lib, tokenLabel, keyLabel, pin string) (*x509.Certificate, crypto.Signer, error) {
	ctx, err := crypto11.Configure(&crypto11.Config{
		Path:       lib,
		TokenLabel: tokenLabel,
		Pin:        pin,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("PKCS#11 configure (lib=%s token=%s): %w", lib, tokenLabel, err)
	}
	// ctx is not deferred-closed: the returned signer needs it alive.

	signer, err := ctx.FindKeyPair(nil, []byte(keyLabel))
	if err != nil {
		return nil, nil, fmt.Errorf("PKCS#11 FindKeyPair (label=%s): %w", keyLabel, err)
	}

	if signer == nil {
		// Key not present on token — generate a new P-384 key.
		log.Printf("pki: key %q not found on HSM token %q — generating new ECDSA P-384 CA key", keyLabel, tokenLabel)
		signer, err = ctx.GenerateECDSAKeyPairWithLabel(nil, []byte(keyLabel), elliptic.P384())
		if err != nil {
			return nil, nil, fmt.Errorf("PKCS#11 GenerateECDSAKeyPairWithLabel (label=%s): %w", keyLabel, err)
		}
	}

	// Ensure the CA certificate exists on disk.
	if !fileExists(certPath) {
		if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
			return nil, nil, fmt.Errorf("failed to create CA cert directory: %w", err)
		}
		log.Printf("pki: generating CA certificate for HSM key %q → %s", keyLabel, certPath)
		if err := generateCACert(certPath, signer); err != nil {
			return nil, nil, err
		}
	}

	caCert, err := loadCACert(certPath)
	if err != nil {
		return nil, nil, err
	}

	return caCert, signer, nil
}

// generateCACert creates a self-signed CA certificate using the given signer
// and writes it to certPath (PEM, 0o644).
func generateCACert(certPath string, signer crypto.Signer) error {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate CA serial number: %w", err)
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

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, signer.Public(), signer)
	if err != nil {
		return fmt.Errorf("failed to create HSM CA certificate: %w", err)
	}

	f, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write CA cert %s: %w", certPath, err)
	}
	defer f.Close()

	return pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}
