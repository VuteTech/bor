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
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// testCA is a CA certificate with its key, used to build chains in tests.
type testCA struct {
	cert *x509.Certificate
	key  crypto.Signer
}

// newTestCA issues a CA certificate named cn from parent (self-signed when
// parent is nil). mutate may adjust the template before signing.
func newTestCA(t *testing.T, cn string, parent *testCA, mutate func(*x509.Certificate)) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return newTestCAWithKey(t, cn, parent, key, mutate)
}

func newTestCAWithKey(t *testing.T, cn string, parent *testCA, key crypto.Signer, mutate func(*x509.Certificate)) *testCA {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if mutate != nil {
		mutate(tmpl)
	}
	issuer, signer := tmpl, key
	if parent != nil {
		issuer, signer = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, issuer, key.Public(), signer)
	if err != nil {
		t.Fatalf("CreateCertificate(%s): %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate(%s): %v", cn, err)
	}
	return &testCA{cert: cert, key: key}
}

// issueLeaf issues a client (agent) certificate from ca.
func issueLeaf(t *testing.T, ca *testCA, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	if err != nil {
		t.Fatalf("CreateCertificate(%s): %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate(%s): %v", cn, err)
	}
	return cert
}

func bundle(cas ...*testCA) []byte {
	var out []byte
	for _, ca := range cas {
		out = append(out, EncodeCertPEM(ca.cert)...)
	}
	return out
}

func TestParseCAChain(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	mid := newTestCA(t, "Corp Issuing", root, nil)
	signing := newTestCA(t, "Bor CA", mid, nil)
	other := newTestCA(t, "Other Root", nil, nil)

	keyBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")})

	tests := []struct {
		name        string
		pem         []byte
		wantIssuers int
		wantErr     string
	}{
		{name: "self-signed only", pem: bundle(root)},
		{name: "single subordinate certificate", pem: bundle(signing)},
		{name: "full chain", pem: bundle(signing, mid, root), wantIssuers: 2},
		{name: "two levels", pem: bundle(mid, root), wantIssuers: 1},
		{name: "out of order", pem: bundle(signing, root, mid), wantErr: "out of order or broken"},
		{name: "root first", pem: bundle(root, mid, signing), wantErr: "out of order or broken"},
		{name: "unrelated root", pem: bundle(signing, mid, other), wantErr: "out of order or broken"},
		{name: "missing root", pem: bundle(signing, mid), wantErr: "incomplete"},
		{name: "private key in bundle", pem: append(bundle(signing), keyBlock...), wantErr: "unexpected PEM block"},
		{name: "empty", pem: []byte("not pem"), wantErr: "no PEM certificate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, err := ParseCAChain(tt.pem)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseCAChain() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCAChain() error = %v", err)
			}
			if len(chain.Issuers) != tt.wantIssuers {
				t.Errorf("len(Issuers) = %d, want %d", len(chain.Issuers), tt.wantIssuers)
			}
		})
	}
}

// A client certificate issued by another CA under the same external root
// must not be accepted, whatever the CA bundle contains.
func TestCAChain_ClientCAPoolTrustsSigningCertificateOnly(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	signing := newTestCA(t, "Bor CA", root, nil)
	sibling := newTestCA(t, "Corp VPN CA", root, nil)

	chain, err := ParseCAChain(bundle(signing, root))
	if err != nil {
		t.Fatalf("ParseCAChain() error = %v", err)
	}
	pool := chain.ClientCAPool()

	// A peer may send any intermediates it likes; they must not help.
	offered := x509.NewCertPool()
	offered.AddCert(sibling.cert)
	offered.AddCert(root.cert)
	opts := x509.VerifyOptions{
		Roots:         pool,
		Intermediates: offered,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	if _, err := issueLeaf(t, signing, "node-1").Verify(opts); err != nil {
		t.Errorf("certificate issued by the signing CA rejected: %v", err)
	}
	if _, err := issueLeaf(t, sibling, "node-1").Verify(opts); err == nil {
		t.Error("certificate issued by a sibling CA under the same root was accepted")
	}
	if _, err := issueLeaf(t, root, "node-2").Verify(opts); err == nil {
		t.Error("certificate issued directly by the external root was accepted")
	}
}

func TestCAChain_Validate(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	weakKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	serverOnly := func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }
	bothUsages := func(c *x509.Certificate) {
		c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	}
	noSubCA := func(c *x509.Certificate) { c.MaxPathLen, c.MaxPathLenZero = 0, true }

	restrictedMid := newTestCA(t, "Server-only Issuing", root, serverOnly)
	leafOnlyMid := newTestCA(t, "No Sub-CA Issuing", root, noSubCA)

	tests := []struct {
		name    string
		signing *testCA
		above   []*testCA
		key     crypto.Signer // overrides the signing key when set
		wantErr string
	}{
		{name: "self-signed", signing: root},
		{name: "subordinate with chain", signing: newTestCA(t, "Bor CA", root, nil), above: []*testCA{root}},
		{name: "subordinate without chain", signing: newTestCA(t, "Bor CA", root, nil)},
		{name: "both usages allowed", signing: newTestCA(t, "Bor CA", root, bothUsages), above: []*testCA{root}},
		{name: "key mismatch", signing: newTestCA(t, "Bor CA", root, nil), key: otherKey, wantErr: "does not match"},
		{
			name:    "not a CA",
			signing: newTestCA(t, "Bor CA", root, func(c *x509.Certificate) { c.IsCA = false }),
			wantErr: "is not a CA certificate",
		},
		{
			name:    "no certificate signing usage",
			signing: newTestCA(t, "Bor CA", root, func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageDigitalSignature }),
			wantErr: "lacks the certificate signing key usage",
		},
		{
			name:    "extended key usage excludes client auth",
			signing: newTestCA(t, "Bor CA", root, serverOnly),
			wantErr: "restricts extended key usage",
		},
		{
			name:    "issuer extended key usage excludes client auth",
			signing: newTestCA(t, "Bor CA", restrictedMid, nil),
			above:   []*testCA{restrictedMid, root},
			wantErr: "Server-only Issuing",
		},
		{
			name:    "weak RSA key",
			signing: newTestCAWithKey(t, "Bor CA", root, weakKey, nil),
			wantErr: "below the baseline",
		},
		{
			name:    "issuer path length forbids a subordinate CA",
			signing: newTestCA(t, "Bor CA", leafOnlyMid, nil),
			above:   []*testCA{leafOnlyMid, root},
			wantErr: "cannot issue agent certificates",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, err := ParseCAChain(bundle(append([]*testCA{tt.signing}, tt.above...)...))
			if err != nil {
				t.Fatalf("ParseCAChain() error = %v", err)
			}
			key := tt.signing.key
			if tt.key != nil {
				key = tt.key
			}
			err = chain.Validate(key)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestCAChain_CheckServerNames(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	constrained := newTestCA(t, "Bor CA", root, func(c *x509.Certificate) {
		c.PermittedDNSDomainsCritical = true
		c.PermittedDNSDomains = []string{"example.com"}
	})
	free := newTestCA(t, "Bor CA", root, nil)

	tests := []struct {
		name    string
		signing *testCA
		dns     []string
		ips     []net.IP
		wantErr bool
	}{
		{name: "unconstrained", signing: free, dns: []string{"localhost", "bor.example.com"}, ips: []net.IP{net.ParseIP("127.0.0.1")}},
		{name: "inside constraint", signing: constrained, dns: []string{"bor.example.com"}},
		{name: "outside constraint", signing: constrained, dns: []string{"bor.example.com", "localhost"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, err := ParseCAChain(bundle(tt.signing, root))
			if err != nil {
				t.Fatalf("ParseCAChain() error = %v", err)
			}
			err = chain.CheckServerNames(tt.signing.key, tt.dns, tt.ips)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckServerNames() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCAChain_ServedIssuers(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	mid := newTestCA(t, "Corp Issuing", root, nil)
	signing := newTestCA(t, "Bor CA", mid, nil)

	single, _ := ParseCAChain(bundle(signing))
	if got := single.ServedIssuers(); len(got) != 0 {
		t.Errorf("ServedIssuers() without a chain = %d certificates, want 0", len(got))
	}

	full, err := ParseCAChain(bundle(signing, mid, root))
	if err != nil {
		t.Fatalf("ParseCAChain() error = %v", err)
	}
	got := full.ServedIssuers()
	if len(got) != 2 || !got[0].Equal(signing.cert) || !got[1].Equal(mid.cert) {
		t.Errorf("ServedIssuers() = %d certificates, want the signing certificate and the intermediate, without the root", len(got))
	}
}

// The server certificate file carries the chain, so a client that trusts
// only the external root can verify it.
func TestEnsureServerCertWithChain(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	mid := newTestCA(t, "Corp Issuing", root, nil)
	signing := newTestCA(t, "Bor CA", mid, nil)
	chain, err := ParseCAChain(bundle(signing, mid, root))
	if err != nil {
		t.Fatalf("ParseCAChain() error = %v", err)
	}

	dir := t.TempDir()
	certPath, keyPath, err := EnsureServerCertWithChain(dir, signing.cert, signing.key, []string{"bor.example.com"}, chain.ServedIssuers())
	if err != nil {
		t.Fatalf("EnsureServerCertWithChain() error = %v", err)
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair() error = %v", err)
	}
	if len(pair.Certificate) != 3 {
		t.Fatalf("certificate file holds %d certificates, want 3 (leaf, signing, intermediate)", len(pair.Certificate))
	}

	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root.cert)
	presented := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		c, parseErr := x509.ParseCertificate(der)
		if parseErr != nil {
			t.Fatalf("ParseCertificate() error = %v", parseErr)
		}
		presented.AddCert(c)
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: presented, DNSName: "bor.example.com"}); err != nil {
		t.Errorf("server certificate does not verify against the external root: %v", err)
	}

	// Unchanged input keeps the certificate.
	before, _ := os.ReadFile(certPath)
	if _, _, err = EnsureServerCertWithChain(dir, signing.cert, signing.key, []string{"bor.example.com"}, chain.ServedIssuers()); err != nil {
		t.Fatalf("EnsureServerCertWithChain() second call error = %v", err)
	}
	after, _ := os.ReadFile(certPath)
	if !bytes.Equal(before, after) {
		t.Error("server certificate was regenerated although nothing changed")
	}

	// A different set of served issuers regenerates it.
	if _, _, err = EnsureServerCertWithChain(dir, signing.cert, signing.key, []string{"bor.example.com"}, nil); err != nil {
		t.Fatalf("EnsureServerCertWithChain() without issuers error = %v", err)
	}
	pair, err = tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair() error = %v", err)
	}
	if len(pair.Certificate) != 1 {
		t.Errorf("certificate file holds %d certificates after dropping the chain, want 1", len(pair.Certificate))
	}
}
