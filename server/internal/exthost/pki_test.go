// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package exthost

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/pki"
	"github.com/VuteTech/Bor/server/pkg/ext"
)

// A module re-certifies the running CA: status, signing request, install,
// and the "restart required" state that follows.
func TestPKIHost_CAManager(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, err := pki.EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	caCert, caKey, err := pki.LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}
	deps := &Deps{
		CACert:       caCert,
		CAKey:        caKey,
		CAManager:    &pki.CAManager{Store: pki.CAStore{CertPath: certPath, KeyPath: keyPath}, Key: caKey},
		CAAgentTrust: "signing",
		CAKeyStorage: "file",
	}
	var hostPKI ext.PKI = &pkiHost{deps: deps, module: "test"}
	ca, ok := hostPKI.(ext.CAManager)
	if !ok {
		t.Fatal("the host PKI does not implement ext.CAManager")
	}

	st, err := ca.CAStatus()
	if err != nil {
		t.Fatalf("CAStatus() error = %v", err)
	}
	if !st.SelfSigned || st.RestartRequired || st.RequestPending || st.KeyAlgorithm != "ECDSA P-384" ||
		st.KeyStorage != "file" || st.AgentTrust != "signing" {
		t.Errorf("CAStatus() = %+v", st)
	}

	csrPEM, err := ca.CreateCARequest(context.Background(), "")
	if err != nil {
		t.Fatalf("CreateCARequest() error = %v", err)
	}
	if st, _ = ca.CAStatus(); !st.RequestPending {
		t.Error("RequestPending = false after creating a request")
	}

	// An external root issues the certificate for the request.
	rootDir := t.TempDir()
	rootCertPath, rootKeyPath, err := pki.EnsureCA(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, rootKey, err := pki.LoadCA(rootCertPath, rootKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(csrPEM)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		RawSubject:            csr.RawSubject,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}, rootCert, csr.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	// The self-generated root in this test has path length 0, so it cannot
	// have a subordinate CA: the install must refuse the chain.
	bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pki.EncodeCertPEM(rootCert)...)
	if _, err = ca.InstallCACertificate(context.Background(), bundle); err == nil {
		t.Fatal("InstallCACertificate() accepted a chain whose root forbids a subordinate CA")
	}
	if st, _ = ca.CAStatus(); st.RestartRequired {
		t.Error("a rejected certificate changed the CA on disk")
	}

	// Without the root in the bundle the path length cannot be checked and
	// the certificate is installed as a single subordinate certificate.
	res, err := ca.InstallCACertificate(context.Background(), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err != nil {
		t.Fatalf("InstallCACertificate() error = %v", err)
	}
	if !res.Replaced || !res.SameIdentity || res.Issuer != rootCert.Subject.String() {
		t.Errorf("InstallCACertificate() = %+v", res)
	}
	st, err = ca.CAStatus()
	if err != nil {
		t.Fatalf("CAStatus() error = %v", err)
	}
	if !st.RestartRequired || st.RequestPending || st.InstalledIssuer != rootCert.Subject.String() || !st.SelfSigned {
		t.Errorf("CAStatus() after install = %+v", st)
	}

	// A server without CA management reports that instead of panicking.
	bare := &pkiHost{deps: &Deps{}, module: "test"}
	if _, err = bare.CAStatus(); err == nil {
		t.Error("CAStatus() without a CA manager succeeded")
	}
	if _, err = bare.CreateCARequest(context.Background(), ""); err == nil {
		t.Error("CreateCARequest() without a CA manager succeeded")
	}
	if _, err = bare.InstallCACertificate(context.Background(), nil); err == nil {
		t.Error("InstallCACertificate() without a CA manager succeeded")
	}
}
