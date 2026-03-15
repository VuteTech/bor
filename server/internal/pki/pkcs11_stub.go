// SPDX-License-Identifier: LGPL-3.0-or-later
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
	"fmt"
)

// EnsureCAWithHSM is not available in this build.
// Rebuild with -tags pkcs11 to enable PKCS#11 HSM support.
func EnsureCAWithHSM(_, _, _, _, _ string) (*x509.Certificate, crypto.Signer, error) {
	return nil, nil, fmt.Errorf(
		"PKCS#11 HSM support is not compiled in: " +
			"rebuild the server with '-tags pkcs11' and add the dependency: " +
			"cd server && go get github.com/ThalesIgnite/crypto11",
	)
}
