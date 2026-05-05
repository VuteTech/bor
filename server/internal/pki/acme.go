// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"fmt"
	"os"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Well-known ACME directory URLs for common certificate authorities.
// Pass any of these (or any other RFC 8555-compliant directory URL) as
// ACMEManagerConfig.DirectoryURL.
const (
	// LetsEncryptDirectory is the Let's Encrypt production ACME v2 directory.
	LetsEncryptDirectory = "https://acme-v02.api.letsencrypt.org/directory"

	// LetsEncryptStagingDirectory is the Let's Encrypt staging directory.
	// Use it during development to avoid hitting production rate limits.
	// Certificates issued by staging are NOT trusted by browsers.
	LetsEncryptStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// ZeroSSLDirectory is the ZeroSSL ACME directory (free DV certificates).
	ZeroSSLDirectory = "https://acme.zerossl.com/v2/DV90"

	// BuypassDirectory is the Buypass Go SSL ACME directory (free DV certificates).
	BuypassDirectory = "https://api.buypass.com/acme/directory" //nolint:gosec // CA directory URL, not a credential

	// GoogleTrustServicesDirectory is the Google Trust Services ACME directory.
	GoogleTrustServicesDirectory = "https://dv.acme-v02.api.pki.goog/directory"

	// ALPNProto is the TLS-ALPN-01 ACME challenge ALPN protocol identifier
	// (RFC 8737). Include it in tls.Config.NextProtos alongside "h2" and
	// "http/1.1" to enable TLS-ALPN-01 challenge support.
	ALPNProto = acme.ALPNProto
)

// ACMEManagerConfig holds the parameters for NewACMEManager.
type ACMEManagerConfig struct {
	// DirectoryURL is the ACME directory URL of the certificate authority.
	// When empty, Let's Encrypt production (LetsEncryptDirectory) is used.
	// Any RFC 8555-compliant CA is supported by setting its directory URL.
	DirectoryURL string

	// Email is the contact email address registered with the ACME CA.
	// Required by Let's Encrypt; consult each CA's documentation.
	Email string

	// Domains is the whitelist of domain names the manager may request
	// certificates for. Certificate requests for unlisted domains are rejected.
	Domains []string

	// CacheDir is the directory where ACME account keys and issued certificates
	// are persisted across server restarts. Created with mode 0700 if absent.
	// Defaults to /var/lib/bor/acme when empty.
	CacheDir string
}

// NewACMEManager creates an autocert.Manager configured for the given ACME CA.
//
// The manager automatically handles certificate issuance and renewal before
// expiry. Two challenge methods are wired up by the caller:
//
//   - HTTP-01: serve acmeMgr.HTTPHandler(nil) on the configured HTTP port
//     (default 80). The ACME CA must be able to reach that port over plain
//     HTTP on each domain being validated.
//
//   - TLS-ALPN-01: include pki.ALPNProto in tls.Config.NextProtos and set
//     tls.Config.GetCertificate = acmeMgr.GetCertificate. The CA validates
//     ownership via a special TLS handshake; requires the TLS port to be
//     reachable as port 443 from the CA's perspective.
func NewACMEManager(cfg ACMEManagerConfig) (*autocert.Manager, error) {
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = "/var/lib/bor/acme"
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create ACME cache dir %q: %w", cacheDir, err)
	}

	directoryURL := cfg.DirectoryURL
	if directoryURL == "" {
		directoryURL = LetsEncryptDirectory
	}

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.Domains...),
		Cache:      autocert.DirCache(cacheDir),
		Email:      cfg.Email,
		Client: &acme.Client{
			DirectoryURL: directoryURL,
		},
	}

	return m, nil
}
