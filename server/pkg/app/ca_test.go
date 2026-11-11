// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package app

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/config"
	"github.com/VuteTech/Bor/server/internal/pki"
)

func countCerts(pemBytes []byte) int {
	n := 0
	for {
		var block *pem.Block
		block, pemBytes = pem.Decode(pemBytes)
		if block == nil {
			return n
		}
		n++
	}
}

// externalRoot is a self-signed authority that signs CA requests.
type externalRoot struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newExternalRoot(t *testing.T) *externalRoot {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Corp Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &externalRoot{cert: cert, key: key}
}

// sign issues a CA certificate for the request and returns it followed by
// the root, in PEM.
func (r *externalRoot) sign(t *testing.T, csrPEM []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatalf("no PEM request in %q", csrPEM)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		RawSubject:            csr.RawSubject,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(12 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, r.cert, csr.PublicKey, r.key)
	if err != nil {
		t.Fatal(err)
	}
	return append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pki.EncodeCertPEM(r.cert)...)
}

func TestCAStore(t *testing.T) {
	tests := []struct {
		name string
		ca   config.CAConfig
		want pki.CAStore
	}{
		{
			name: "auto-generated",
			ca:   config.CAConfig{AutogenDir: "/var/lib/bor/pki/ca"},
			want: pki.CAStore{CertPath: "/var/lib/bor/pki/ca/ca.crt", KeyPath: "/var/lib/bor/pki/ca/ca.key"},
		},
		{
			name: "operator files",
			ca:   config.CAConfig{AutogenDir: "/var/lib/bor/pki/ca", CertFile: "/etc/bor/pki/bor-ca.crt", KeyFile: "/etc/bor/pki/bor-ca.key"},
			want: pki.CAStore{CertPath: "/etc/bor/pki/bor-ca.crt", KeyPath: "/etc/bor/pki/bor-ca.key"},
		},
		{
			name: "PKCS#11 with default certificate path",
			ca: config.CAConfig{AutogenDir: "/var/lib/bor/pki/ca",
				PKCS11: config.PKCS11Config{Lib: "lib.so", TokenLabel: "t", KeyLabel: "k"}},
			want: pki.CAStore{CertPath: "/var/lib/bor/pki/ca/ca.crt"},
		},
		{
			name: "PKCS#11 with certificate file",
			ca: config.CAConfig{AutogenDir: "/var/lib/bor/pki/ca", CertFile: "/etc/bor/pki/hsm-ca.crt",
				PKCS11: config.PKCS11Config{Lib: "lib.so", TokenLabel: "t", KeyLabel: "k"}},
			want: pki.CAStore{CertPath: "/etc/bor/pki/hsm-ca.crt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := caStore(&config.Config{CA: tt.ca}); got != tt.want {
				t.Errorf("caStore() = %+v, want %+v", got, tt.want)
			}
		})
	}
	if got := (pki.CAStore{CertPath: "/etc/bor/pki/bor-ca.crt"}).RequestPath(); got != "/etc/bor/pki/bor-ca.csr" {
		t.Errorf("RequestPath() = %q", got)
	}
}

// The whole command line flow: request, issue, import, with the agent trust
// material that results from each setting.
func TestCACommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BOR_CONFIG", filepath.Join(dir, "absent.yaml"))
	t.Setenv("BOR_CA_AUTOGEN_DIR", filepath.Join(dir, "ca"))
	root := newExternalRoot(t)
	certPath := filepath.Join(dir, "ca", "ca.crt")

	var out bytes.Buffer
	if err := CACommand([]string{"status"}, &out); err != nil {
		t.Fatalf("ca status error = %v", err)
	}
	if !strings.Contains(out.String(), "No CA certificate") {
		t.Errorf("ca status = %q", out.String())
	}

	csrFile := filepath.Join(dir, "bor-ca.csr")
	out.Reset()
	if err := CACommand([]string{"csr", "-cn", "Example Bor CA", "-out", csrFile}, &out); err != nil {
		t.Fatalf("ca csr error = %v", err)
	}
	if !strings.Contains(out.String(), "Generated a new ECDSA P-384 CA key") {
		t.Errorf("ca csr did not report the new key: %q", out.String())
	}
	csrPEM, err := os.ReadFile(csrFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !caStore(cfg).RequestPending() {
		t.Error("no pending request after ca csr")
	}

	if err = CACommand([]string{"import"}, &out); err == nil {
		t.Error("ca import without -in succeeded")
	}
	if err = CACommand([]string{"import", "-in", csrFile}, &out); err == nil {
		t.Error("ca import accepted a signing request as a certificate")
	}

	signedFile := filepath.Join(dir, "signed.pem")
	if err = os.WriteFile(signedFile, root.sign(t, csrPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = CACommand([]string{"import", "-in", signedFile}, &out); err != nil {
		t.Fatalf("ca import error = %v", err)
	}
	if !strings.Contains(out.String(), "CN=Corp Root") || !strings.Contains(out.String(), "Restart the server") {
		t.Errorf("ca import = %q", out.String())
	}
	if caStore(cfg).RequestPending() {
		t.Error("request still pending after ca import")
	}

	chain, err := pki.LoadCAChain(certPath)
	if err != nil {
		t.Fatalf("LoadCAChain() error = %v", err)
	}
	if chain.Signing.Subject.CommonName != "Example Bor CA" || len(chain.Issuers) != 1 {
		t.Errorf("installed chain: subject %q, %d issuers", chain.Signing.Subject.CommonName, len(chain.Issuers))
	}
	store := caStore(cfg)
	trustOf := func(c *pki.CAChain, mode string) []byte {
		state, stateErr := pki.NewTrustState(c, store, agentTrustIncludesIssuers(c, mode))
		if stateErr != nil {
			t.Fatalf("NewTrustState() error = %v", stateErr)
		}
		return state.AgentTrustPEM()
	}
	if n := countCerts(trustOf(chain, config.AgentTrustSigning)); n != 1 {
		t.Errorf("agent trust signing holds %d certificates, want 1", n)
	}
	if n := countCerts(trustOf(chain, config.AgentTrustChain)); n != 2 {
		t.Errorf("agent trust chain holds %d certificates, want 2", n)
	}
	if agentTrustIncludesIssuers(&pki.CAChain{Signing: chain.Signing}, config.AgentTrustChain) {
		t.Error("agent trust chain without issuers claims to include issuers")
	}

	// Renewal: no -cn keeps the subject, and the import says agents are fine.
	out.Reset()
	if err = CACommand([]string{"csr"}, &out); err != nil {
		t.Fatalf("ca csr (renewal) error = %v", err)
	}
	if err = os.WriteFile(signedFile, root.sign(t, out.Bytes()), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = CACommand([]string{"import", "-in", signedFile}, &out); err != nil {
		t.Fatalf("ca import (renewal) error = %v", err)
	}
	if !strings.Contains(out.String(), "same key and subject") {
		t.Errorf("ca import (renewal) = %q", out.String())
	}

	out.Reset()
	if err = CACommand([]string{"status"}, &out); err != nil {
		t.Fatalf("ca status error = %v", err)
	}
	for _, want := range []string{"subordinate CA", "CN=Example Bor CA", "CN=Corp Root", "ECDSA P-384"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("ca status lacks %q: %q", want, out.String())
		}
	}

	if err = CACommand([]string{"bogus"}, &out); err == nil {
		t.Error("unknown ca command succeeded")
	}
}
