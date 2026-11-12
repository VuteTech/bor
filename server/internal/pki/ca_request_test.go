// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// signCARequest plays the external authority: it issues a CA certificate for
// the request from parent, copying the subject as requested.
func signCARequest(t *testing.T, csrPEM []byte, parent *testCA) []byte {
	t.Helper()
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("not a certificate signing request: %q", csrPEM)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificateRequest: %v", err)
	}
	if err = csr.CheckSignature(); err != nil {
		t.Fatalf("request signature: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		RawSubject:            csr.RawSubject,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent.cert, csr.PublicKey, parent.key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func newFileCAManager(t *testing.T) *CAManager {
	t.Helper()
	dir := t.TempDir()
	store := CAStore{CertPath: filepath.Join(dir, "ca.crt"), KeyPath: filepath.Join(dir, "ca.key")}
	key, created, err := LoadOrCreateCAKey(store.KeyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateCAKey() error = %v", err)
	}
	if !created {
		t.Fatal("LoadOrCreateCAKey() did not create the key")
	}
	return &CAManager{Store: store, Key: key}
}

func TestLoadOrCreateCAKey(t *testing.T) {
	mgr := newFileCAManager(t)
	info, err := os.Stat(mgr.Store.KeyPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o, want 600", perm)
	}
	again, created, err := LoadOrCreateCAKey(mgr.Store.KeyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateCAKey() second call error = %v", err)
	}
	if created {
		t.Error("existing key was replaced")
	}
	if !again.Public().(interface{ Equal(crypto.PublicKey) bool }).Equal(mgr.Key.Public()) {
		t.Error("second call returned a different key")
	}
}

func TestCAManager_CreateRequest(t *testing.T) {
	mgr := newFileCAManager(t)

	csrPEM, err := mgr.CreateRequest("")
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	block, _ := pem.Decode(csrPEM)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificateRequest: %v", err)
	}
	if err = csr.CheckSignature(); err != nil {
		t.Errorf("request is not signed by the CA key: %v", err)
	}
	if csr.Subject.CommonName != "Bor CA" {
		t.Errorf("default common name = %q, want %q", csr.Subject.CommonName, "Bor CA")
	}
	var sawCA, sawKeyUsage bool
	for _, e := range csr.Extensions {
		switch {
		case e.Id.Equal(oidExtBasicConstraints):
			sawCA = e.Critical
		case e.Id.Equal(oidExtKeyUsage):
			sawKeyUsage = e.Critical
		}
	}
	if !sawCA || !sawKeyUsage {
		t.Errorf("request lacks critical CA extensions: basicConstraints=%v keyUsage=%v", sawCA, sawKeyUsage)
	}

	stored, err := os.ReadFile(mgr.Store.RequestPath())
	if err != nil || !bytes.Equal(stored, csrPEM) {
		t.Errorf("request was not stored at %s: %v", mgr.Store.RequestPath(), err)
	}
	if !mgr.Store.RequestPending() {
		t.Error("RequestPending() = false with a request and no certificate")
	}

	named, err := mgr.CreateRequest("  Example Bor CA ")
	if err != nil {
		t.Fatalf("CreateRequest(name) error = %v", err)
	}
	block, _ = pem.Decode(named)
	csr, _ = x509.ParseCertificateRequest(block.Bytes)
	if csr.Subject.CommonName != "Example Bor CA" {
		t.Errorf("common name = %q, want %q", csr.Subject.CommonName, "Example Bor CA")
	}
}

func TestCAManager_Install(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	mgr := newFileCAManager(t)
	csrPEM, err := mgr.CreateRequest("Example Bor CA")
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	issued := signCARequest(t, csrPEM, root)
	bundlePEM := append(append([]byte{}, issued...), EncodeCertPEM(root.cert)...)

	t.Run("rejects a certificate for another key", func(t *testing.T) {
		other := newTestCA(t, "Someone Else", root, nil)
		if _, installErr := mgr.Install(EncodeCertPEM(other.cert)); installErr == nil ||
			!strings.Contains(installErr.Error(), "does not match") {
			t.Fatalf("Install() error = %v, want a key mismatch", installErr)
		}
		if fileExists(mgr.Store.CertPath) {
			t.Error("a rejected certificate was written")
		}
	})

	t.Run("rejects a broken bundle", func(t *testing.T) {
		reversed := append(EncodeCertPEM(root.cert), issued...)
		if _, installErr := mgr.Install(reversed); installErr == nil {
			t.Fatal("Install() accepted a bundle that starts with the root")
		}
	})

	res, err := mgr.Install(bundlePEM)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if res.Replaced || res.SameIdentity {
		t.Errorf("first install: Replaced=%v SameIdentity=%v, want false", res.Replaced, res.SameIdentity)
	}
	if len(res.Chain.Issuers) != 1 {
		t.Errorf("len(Issuers) = %d, want 1", len(res.Chain.Issuers))
	}
	if mgr.Store.RequestPending() || fileExists(mgr.Store.RequestPath()) {
		t.Error("the signing request was not removed after the install")
	}
	loadedCert, loadedKey, err := LoadCA(mgr.Store.CertPath, mgr.Store.KeyPath)
	if err != nil {
		t.Fatalf("LoadCA() after install error = %v", err)
	}
	if loadedCert.Subject.CommonName != "Example Bor CA" || loadedKey == nil {
		t.Errorf("LoadCA() returned %q", loadedCert.Subject.CommonName)
	}

	// Re-certifying with the same key and subject is flagged as such.
	csrPEM, err = mgr.CreateRequest("")
	if err != nil {
		t.Fatalf("CreateRequest() for renewal error = %v", err)
	}
	renewed := signCARequest(t, csrPEM, root)
	res, err = mgr.Install(append(renewed, EncodeCertPEM(root.cert)...))
	if err != nil {
		t.Fatalf("Install() renewal error = %v", err)
	}
	if !res.Replaced || !res.SameIdentity {
		t.Errorf("renewal: Replaced=%v SameIdentity=%v, want true", res.Replaced, res.SameIdentity)
	}
	previous, err := os.ReadFile(mgr.Store.PreviousPath())
	if err != nil || !bytes.Equal(previous, bundlePEM) {
		t.Errorf("the replaced certificate was not kept at %s: %v", mgr.Store.PreviousPath(), err)
	}

	// A different subject is not the same identity.
	csrPEM, _ = mgr.CreateRequest("Renamed Bor CA")
	res, err = mgr.Install(signCARequest(t, csrPEM, root))
	if err != nil {
		t.Fatalf("Install() renamed error = %v", err)
	}
	if !res.Replaced || res.SameIdentity {
		t.Errorf("renamed: Replaced=%v SameIdentity=%v, want true/false", res.Replaced, res.SameIdentity)
	}
}

// handshake runs a mutual TLS handshake on a loopback connection and returns
// the client's and the server's error.
func handshake(t *testing.T, server, client *tls.Config) (clientErr, serverErr error) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", server)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer func() { _ = conn.Close() }()
		hsErr := conn.(*tls.Conn).Handshake()
		if hsErr == nil {
			// TLS 1.3 reports a rejected client certificate on first read.
			_, _ = conn.Write([]byte("ok"))
		}
		done <- hsErr
	}()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", ln.Addr().String(), client)
	if err == nil {
		buf := make([]byte, 2)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = conn.Read(buf)
		_ = conn.Close()
	}
	return err, <-done
}

// clientHandshake is handshake reduced to what the client saw.
func clientHandshake(t *testing.T, server, client *tls.Config) error {
	t.Helper()
	clientErr, _ := handshake(t, server, client)
	return clientErr
}

// tlsCert issues an end-entity certificate from ca and returns it with its
// key and the given certificates to present after it.
func tlsCert(t *testing.T, ca *testCA, cn string, usage x509.ExtKeyUsage, dns []string, present ...*x509.Certificate) tls.Certificate {
	t.Helper()
	key := newTestCA(t, "unused", nil, nil).key
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     dns,
	}
	tmpl.Subject.CommonName = cn
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	if err != nil {
		t.Fatalf("CreateCertificate(%s): %v", cn, err)
	}
	out := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	for _, c := range present {
		out.Certificate = append(out.Certificate, c.Raw)
	}
	return out
}

func poolOf(certs ...*x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool
}

// The policy port over real TLS: which agents and servers accept each other
// when the Bor CA is a subordinate of an external root.
func TestSubordinateCA_MutualTLS(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	signing := newTestCA(t, "Bor CA", root, nil)
	sibling := newTestCA(t, "Corp VPN CA", root, nil)

	chain, err := ParseCAChain(bundle(signing, root))
	if err != nil {
		t.Fatalf("ParseCAChain() error = %v", err)
	}
	serverConfig := func(ca *testCA, c *CAChain) *tls.Config {
		return &tls.Config{
			Certificates: []tls.Certificate{tlsCert(t, ca, "Bor UI", x509.ExtKeyUsageServerAuth, []string{"bor.test"}, c.ServedIssuers()...)},
			ClientCAs:    c.ClientCAPool(),
			ClientAuth:   tls.RequireAndVerifyClientCert,
			MinVersion:   tls.VersionTLS13,
		}
	}
	clientConfig := func(trust *x509.CertPool, cert tls.Certificate) *tls.Config {
		return &tls.Config{
			RootCAs:      trust,
			Certificates: []tls.Certificate{cert},
			ServerName:   "bor.test",
			MinVersion:   tls.VersionTLS13,
		}
	}
	signingOnly := poolOf(signing.cert)           // ca.agent_trust: signing
	wholeChain := poolOf(signing.cert, root.cert) // ca.agent_trust: chain
	agentCert := tlsCert(t, signing, "node-1", x509.ExtKeyUsageClientAuth, nil)

	t.Run("agent of the Bor CA is accepted", func(t *testing.T) {
		for name, trust := range map[string]*x509.CertPool{"signing": signingOnly, "chain": wholeChain} {
			if cErr, sErr := handshake(t, serverConfig(signing, chain), clientConfig(trust, agentCert)); cErr != nil || sErr != nil {
				t.Errorf("agent trust %s: client error = %v, server error = %v", name, cErr, sErr)
			}
		}
	})

	t.Run("client certificate of a sibling CA is rejected", func(t *testing.T) {
		// The peer even presents the sibling CA certificate to help path
		// building; it must not matter.
		rogue := tlsCert(t, sibling, "node-1", x509.ExtKeyUsageClientAuth, nil, sibling.cert)
		for name, trust := range map[string]*x509.CertPool{"signing": signingOnly, "chain": wholeChain} {
			if cErr, sErr := handshake(t, serverConfig(signing, chain), clientConfig(trust, rogue)); sErr == nil && cErr == nil {
				t.Errorf("agent trust %s: the server accepted a client certificate issued by a sibling CA", name)
			}
		}
	})

	t.Run("server of a sibling CA", func(t *testing.T) {
		siblingChain, parseErr := ParseCAChain(bundle(sibling, root))
		if parseErr != nil {
			t.Fatalf("ParseCAChain() error = %v", parseErr)
		}
		impostor := serverConfig(sibling, siblingChain)
		impostor.ClientAuth = tls.NoClientCert
		if cErr := clientHandshake(t, impostor, clientConfig(signingOnly, agentCert)); cErr == nil {
			t.Error("agent trust signing: the agent accepted a server certificate issued by a sibling CA")
		}
		// This is the documented cost of handing agents the whole chain.
		if cErr := clientHandshake(t, impostor, clientConfig(wholeChain, agentCert)); cErr != nil {
			t.Errorf("agent trust chain: expected the agent to accept any CA under the root, got %v", cErr)
		}
	})

	t.Run("signing certificate re-issued for the same key", func(t *testing.T) {
		// An external authority chooses its own subject key identifier,
		// so the renewed certificate differs in that too.
		renewed := newTestCAWithKey(t, "Bor CA", root, signing.key, func(c *x509.Certificate) {
			c.SubjectKeyId = []byte{0xde, 0xad, 0xbe, 0xef}
		})
		renewedChain, parseErr := ParseCAChain(bundle(renewed, root))
		if parseErr != nil {
			t.Fatalf("ParseCAChain() error = %v", parseErr)
		}
		// Agents enrolled before the renewal, with either trust setting,
		// and their certificates issued by the previous signing certificate.
		for name, trust := range map[string]*x509.CertPool{"signing": signingOnly, "chain": wholeChain} {
			if cErr, sErr := handshake(t, serverConfig(renewed, renewedChain), clientConfig(trust, agentCert)); cErr != nil || sErr != nil {
				t.Errorf("agent trust %s: client error = %v, server error = %v", name, cErr, sErr)
			}
		}
	})

	t.Run("signing certificate replaced with a new key", func(t *testing.T) {
		rekeyed := newTestCA(t, "Bor CA", root, nil)
		rekeyedChain, parseErr := ParseCAChain(bundle(rekeyed, root))
		if parseErr != nil {
			t.Fatalf("ParseCAChain() error = %v", parseErr)
		}
		server := serverConfig(rekeyed, rekeyedChain)
		server.ClientAuth = tls.NoClientCert // look at server authentication only
		if cErr := clientHandshake(t, server, clientConfig(signingOnly, agentCert)); cErr == nil {
			t.Error("agent trust signing: the agent accepted a server under a signing key it does not pin")
		}
		if cErr := clientHandshake(t, server, clientConfig(wholeChain, agentCert)); cErr != nil {
			t.Errorf("agent trust chain: the agent rejected the server after the signing key changed: %v", cErr)
		}
		// The agent's own certificate was issued by the old key, which the
		// server no longer trusts: the agent has to re-enroll.
		if cErr, sErr := handshake(t, serverConfig(rekeyed, rekeyedChain), clientConfig(wholeChain, agentCert)); cErr == nil && sErr == nil {
			t.Error("the server accepted a client certificate issued by the replaced signing key")
		}
	})
}
