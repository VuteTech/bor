// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package exthost

import (
	"context"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/pki"
	"github.com/VuteTech/Bor/server/pkg/ext"
)

// newRotationHost is a server running with a fresh self-generated CA.
func newRotationHost(t *testing.T) (ext.CARotator, *Deps, *int) {
	t.Helper()
	certPath, keyPath, err := pki.EnsureCA(t.TempDir())
	if err != nil {
		t.Fatalf("EnsureCA() error = %v", err)
	}
	return rotationHostFor(t, pki.CAStore{CertPath: certPath, KeyPath: keyPath})
}

// rotationHostFor starts (or restarts) a server on the CA in store.
func rotationHostFor(t *testing.T, store pki.CAStore) (ext.CARotator, *Deps, *int) {
	t.Helper()
	chain, err := pki.LoadCAChain(store.CertPath)
	if err != nil {
		t.Fatalf("LoadCAChain() error = %v", err)
	}
	_, key, err := pki.LoadCA(store.CertPath, store.KeyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}
	trust, err := pki.NewTrustState(chain, store, false)
	if err != nil {
		t.Fatalf("NewTrustState() error = %v", err)
	}
	asked := new(int)
	deps := &Deps{
		CACert: chain.Signing, CAKey: key, CATrust: trust,
		CAManager: &pki.CAManager{Store: store, Key: key},
		CATrustStats: func(_ context.Context, bundleSHA, issuer string) (*database.CATrustStats, error) {
			return &database.CATrustStats{Nodes: 5, HoldingBundle: 3, NeverReported: 1, OtherIssuer: 2, Lagging: []string{bundleSHA[:4], issuer[:4]}}, nil
		},
		RequestMetadata: func() int { *asked++; return 4 },
	}
	var hostPKI ext.PKI = &pkiHost{deps: deps, module: "test"}
	rot, ok := hostPKI.(ext.CARotator)
	if !ok {
		t.Fatal("the host PKI does not implement ext.CARotator")
	}
	return rot, deps, asked
}

func TestPKIHost_CARotator(t *testing.T) {
	ctx := context.Background()
	rot, deps, asked := newRotationHost(t)
	store := deps.CAManager.Store

	st, err := rot.CARotationStatus(ctx)
	if err != nil {
		t.Fatalf("CARotationStatus() error = %v", err)
	}
	if !st.Supported || st.Staged != nil || st.ActivationPending || len(st.Previous) != 0 ||
		st.Nodes != 5 || st.NodesHoldingTrust != 3 || st.NodesNeverReported != 1 || st.NodesOnOtherCA != 2 || len(st.LaggingNodes) != 2 {
		t.Errorf("initial status = %+v", st)
	}
	if _, err = rot.ActivateStagedCA(ctx); err == nil {
		t.Error("ActivateStagedCA() without a staged CA succeeded")
	}

	staged, err := rot.StageCA(ctx, 0)
	if err != nil {
		t.Fatalf("StageCA() error = %v", err)
	}
	if *asked != 1 {
		t.Errorf("agents asked %d times after staging, want 1", *asked)
	}
	if got := strings.Count(string(deps.CATrust.AgentTrustPEM()), "BEGIN CERTIFICATE"); got != 2 {
		t.Errorf("agents are handed %d certificates after staging, want 2", got)
	}
	if st, _ = rot.CARotationStatus(ctx); st.Staged == nil || st.Staged.Fingerprint != staged.Fingerprint || !st.Staged.SelfSigned {
		t.Errorf("status after staging = %+v", st)
	}

	if err = rot.DiscardStagedCA(ctx); err != nil {
		t.Fatalf("DiscardStagedCA() error = %v", err)
	}
	if st, _ = rot.CARotationStatus(ctx); st.Staged != nil {
		t.Error("staged CA survived the discard")
	}
	if got := strings.Count(string(deps.CATrust.AgentTrustPEM()), "BEGIN CERTIFICATE"); got != 1 {
		t.Errorf("agents are handed %d certificates after the discard, want 1", got)
	}

	if staged, err = rot.StageCA(ctx, 0); err != nil {
		t.Fatalf("StageCA() again error = %v", err)
	}
	activated, err := rot.ActivateStagedCA(ctx)
	if err != nil {
		t.Fatalf("ActivateStagedCA() error = %v", err)
	}
	if activated.Fingerprint != staged.Fingerprint {
		t.Error("the activated CA is not the staged one")
	}
	st, err = rot.CARotationStatus(ctx)
	if err != nil {
		t.Fatalf("CARotationStatus() error = %v", err)
	}
	if !st.ActivationPending || st.Staged != nil || len(st.Previous) != 1 {
		t.Errorf("status after activation = %+v", st)
	}
	// Until the restart nothing more may be changed.
	if _, err = rot.StageCA(ctx, 0); err == nil {
		t.Error("StageCA() while a restart is pending succeeded")
	}
	if _, err = rot.RetirePreviousCAs(ctx); err == nil {
		t.Error("RetirePreviousCAs() while a restart is pending succeeded: it would drop the CA still in use")
	}

	// Restart on the new CA.
	rot, deps, _ = rotationHostFor(t, store)
	st, err = rot.CARotationStatus(ctx)
	if err != nil {
		t.Fatalf("CARotationStatus() after restart error = %v", err)
	}
	if st.ActivationPending || len(st.Previous) != 1 || pki.CertFingerprint(deps.CACert) != staged.Fingerprint {
		t.Errorf("status after restart = %+v", st)
	}
	n, err := rot.RetirePreviousCAs(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RetirePreviousCAs() = %d, %v", n, err)
	}
	if st, _ = rot.CARotationStatus(ctx); len(st.Previous) != 0 {
		t.Errorf("previous CAs after retiring = %d", len(st.Previous))
	}
	if got := strings.Count(string(deps.CATrust.AgentTrustPEM()), "BEGIN CERTIFICATE"); got != 1 {
		t.Errorf("agents are handed %d certificates after retiring, want 1", got)
	}
}

func TestPKIHost_CARotator_Unavailable(t *testing.T) {
	ctx := context.Background()
	bare := &pkiHost{deps: &Deps{}, module: "test"}
	if _, err := bare.CARotationStatus(ctx); err == nil {
		t.Error("CARotationStatus() without a trust state succeeded")
	}
	if _, err := bare.StageCA(ctx, 0); err == nil {
		t.Error("StageCA() without a trust state succeeded")
	}

	// A CA whose key is not a file reports why it cannot be rotated.
	rot, deps, _ := newRotationHost(t)
	hsm, err := pki.NewTrustState(&pki.CAChain{Signing: deps.CACert}, pki.CAStore{CertPath: deps.CAManager.Store.CertPath}, false)
	if err != nil {
		t.Fatal(err)
	}
	deps.CATrust = hsm
	st, err := rot.CARotationStatus(ctx)
	if err != nil {
		t.Fatalf("CARotationStatus() error = %v", err)
	}
	if st.Supported || !strings.Contains(st.UnsupportedReason, "PKCS#11") {
		t.Errorf("status with a token key = %+v", st)
	}
}
