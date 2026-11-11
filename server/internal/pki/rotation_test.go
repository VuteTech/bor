// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pemCount(data []byte) int {
	n := 0
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return n
		}
		n++
	}
}

// runningCA loads the CA on disk the way a server does at startup.
func runningCA(t *testing.T, store CAStore) (*CAChain, *testCA) {
	t.Helper()
	chain, err := LoadCAChain(store.CertPath)
	if err != nil {
		t.Fatalf("LoadCAChain() error = %v", err)
	}
	_, key, err := LoadCA(store.CertPath, store.KeyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}
	if err = chain.Validate(key); err != nil {
		t.Fatalf("CA on disk is not usable: %v", err)
	}
	return chain, &testCA{cert: chain.Signing, key: key}
}

func accepts(pool *x509.CertPool, cert *x509.Certificate) bool {
	_, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err == nil
}

// A whole rotation to a new self-signed CA, as the server sees it across
// the restart.
func TestCARotation_SelfSigned(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	store := CAStore{CertPath: certPath, KeyPath: keyPath}
	rot := CARotation{Store: store}

	oldChain, oldCA := runningCA(t, store)
	trust, err := NewTrustState(oldChain, store, false)
	if err != nil {
		t.Fatalf("NewTrustState() error = %v", err)
	}
	oldAgent := issueLeaf(t, oldCA, "node-1")
	bundle, shaBefore := trust.AgentTrust()
	if pemCount(bundle) != 1 {
		t.Fatalf("agent trust holds %d certificates before the rotation, want 1", pemCount(bundle))
	}

	if _, err = rot.Activate(); err == nil {
		t.Error("Activate() without a staged CA succeeded")
	}

	// Stage.
	staged, err := rot.StageSelfSigned(3 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("StageSelfSigned() error = %v", err)
	}
	if staged.Signing.Subject.CommonName == oldChain.Signing.Subject.CommonName {
		t.Error("the staged CA has the same name as the active one")
	}
	if _, err = rot.StageSelfSigned(0); err == nil {
		t.Error("staging a second CA succeeded")
	}
	if err = trust.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	bundle, shaStaged := trust.AgentTrust()
	if pemCount(bundle) != 2 || shaStaged == shaBefore {
		t.Errorf("agent trust after staging: %d certificates, fingerprint changed = %v", pemCount(bundle), shaStaged != shaBefore)
	}
	_, stagedKey, err := LoadCA(rot.stagedStore().CertPath, rot.stagedStore().KeyPath)
	if err != nil {
		t.Fatalf("LoadCA(staged) error = %v", err)
	}
	stagedCA := &testCA{cert: staged.Signing, key: stagedKey}
	if accepts(trust.ClientCAs(), issueLeaf(t, stagedCA, "node-1")) {
		t.Error("a certificate issued by the staged CA is accepted before activation")
	}

	// Activate: the running server still uses the old CA.
	if _, err = rot.Activate(); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err = trust.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	bundle, shaActivated := trust.AgentTrust()
	if pemCount(bundle) != 2 {
		t.Errorf("agent trust between activation and restart holds %d certificates, want 2", pemCount(bundle))
	}
	if shaActivated != shaStaged {
		t.Error("activation changed what agents must trust; agents would fetch it again for nothing")
	}
	if trust.ActiveIssuer() != CertFingerprint(oldChain.Signing) || !accepts(trust.ClientCAs(), oldAgent) {
		t.Error("the running server stopped using the old CA before its restart")
	}

	// Restart.
	newChain, newCA := runningCA(t, store)
	if !newChain.Signing.Equal(staged.Signing) {
		t.Fatal("the CA on disk after activation is not the staged one")
	}
	trust, err = NewTrustState(newChain, store, false)
	if err != nil {
		t.Fatalf("NewTrustState() after restart error = %v", err)
	}
	bundle, shaRestarted := trust.AgentTrust()
	if pemCount(bundle) != 2 || shaRestarted != shaStaged {
		t.Errorf("agent trust after the restart: %d certificates, same fingerprint = %v", pemCount(bundle), shaRestarted == shaStaged)
	}
	newAgent := issueLeaf(t, newCA, "node-1")
	if !accepts(trust.ClientCAs(), oldAgent) || !accepts(trust.ClientCAs(), newAgent) {
		t.Error("after the restart certificates of the old and the new CA must both be accepted")
	}
	previous, err := rot.Previous()
	if err != nil || len(previous) != 1 || !previous[0].Equal(oldChain.Signing) {
		t.Fatalf("Previous() = %d certificates, %v", len(previous), err)
	}

	// Retire.
	n, err := rot.RetirePrevious()
	if err != nil || n != 1 {
		t.Fatalf("RetirePrevious() = %d, %v", n, err)
	}
	if err = trust.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if bundle, _ = trust.AgentTrust(); pemCount(bundle) != 1 {
		t.Errorf("agent trust after retiring holds %d certificates, want 1", pemCount(bundle))
	}
	if accepts(trust.ClientCAs(), oldAgent) || !accepts(trust.ClientCAs(), newAgent) {
		t.Error("after retiring only certificates of the new CA must be accepted")
	}
	entries, _ := os.ReadDir(rot.previousDir())
	if len(entries) != 0 {
		t.Errorf("%d files of retired CAs are left, their keys among them", len(entries))
	}
}

// A rotation to a CA certified by an external authority, and discarding.
func TestCARotation_ExternalAndDiscard(t *testing.T) {
	root := newTestCA(t, "Corp Root", nil, nil)
	dir := t.TempDir()
	certPath, keyPath, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	store := CAStore{CertPath: certPath, KeyPath: keyPath}
	rot := CARotation{Store: store}
	active, _ := runningCA(t, store)

	if _, err = rot.StageInstall(EncodeCertPEM(root.cert)); err == nil {
		t.Error("StageInstall() without a request succeeded")
	}
	csrPEM, err := rot.StageRequest("")
	if err != nil {
		t.Fatalf("StageRequest() error = %v", err)
	}
	if !rot.StagedRequestPending() {
		t.Error("StagedRequestPending() = false after StageRequest")
	}
	if staged, _ := rot.Staged(); staged != nil {
		t.Error("a CA is staged before its certificate is installed")
	}
	if _, err = rot.StageInstall(EncodeCertPEM(newTestCA(t, "Other", root, nil).cert)); err == nil {
		t.Error("StageInstall() accepted a certificate for another key")
	}
	issued := append(signCARequest(t, csrPEM, root), EncodeCertPEM(root.cert)...)
	chain, err := rot.StageInstall(issued)
	if err != nil {
		t.Fatalf("StageInstall() error = %v", err)
	}
	if chain.Signing.Subject.CommonName != active.Signing.Subject.CommonName || len(chain.Issuers) != 1 {
		t.Errorf("staged CA: subject %q, %d issuers", chain.Signing.Subject.CommonName, len(chain.Issuers))
	}
	if rot.StagedRequestPending() {
		t.Error("the request is still pending after StageInstall")
	}

	// Agents get the external root only in "chain" mode.
	for includeIssuers, want := range map[bool]int{false: 2, true: 3} {
		trust, trustErr := NewTrustState(active, store, includeIssuers)
		if trustErr != nil {
			t.Fatalf("NewTrustState() error = %v", trustErr)
		}
		if got := pemCount(trust.AgentTrustPEM()); got != want {
			t.Errorf("includeIssuers=%v: agent trust holds %d certificates, want %d", includeIssuers, got, want)
		}
	}

	if err = rot.Discard(); err != nil {
		t.Fatalf("Discard() error = %v", err)
	}
	if staged, _ := rot.Staged(); staged != nil || rot.StagedRequestPending() {
		t.Error("the staged CA survived Discard")
	}
	if fileExists(rot.stagedStore().KeyPath) {
		t.Error("the staged key survived Discard")
	}
	if _, err = LoadCAChain(certPath); err != nil {
		t.Errorf("Discard() touched the active CA: %v", err)
	}
}

func TestCARotation_UnsupportedWithoutKeyFile(t *testing.T) {
	rot := CARotation{Store: CAStore{CertPath: filepath.Join(t.TempDir(), "ca.crt")}}
	if rot.Supported() {
		t.Fatal("Supported() = true without a key file")
	}
	if _, err := rot.StageSelfSigned(0); err == nil || !strings.Contains(err.Error(), "PKCS#11") {
		t.Errorf("StageSelfSigned() error = %v", err)
	}
	if _, err := rot.StageRequest("x"); err == nil {
		t.Error("StageRequest() succeeded")
	}
	if _, err := rot.Activate(); err == nil {
		t.Error("Activate() succeeded")
	}
}
