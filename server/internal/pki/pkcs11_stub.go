// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

//go:build !pkcs11

// This file provides a stub for EnsureCAWithHSM when the server is built
// without PKCS#11 support (the default). If PKCS#11 HSM configuration is
// present at runtime, the server will log a fatal error directing the
// operator to rebuild with -tags pkcs11.

package pki

import (
	"crypto"
	"crypto/x509"
	"errors"
)

// ErrPKCS11NotCompiled is returned by EnsureCAWithHSM in builds without the
// pkcs11 build tag.
var ErrPKCS11NotCompiled = errors.New(
	"PKCS#11 HSM support is not compiled in: " +
		"rebuild the server with 'make server-pkcs11' (build tag pkcs11, CGO enabled)",
)

// EnsureCAWithHSM is not available in this build.
// Rebuild with -tags pkcs11 to enable PKCS#11 HSM support.
func EnsureCAWithHSM(_, _, _, _, _ string) (*x509.Certificate, crypto.Signer, error) {
	return nil, nil, ErrPKCS11NotCompiled
}
