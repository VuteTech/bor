// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package exthost

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/VuteTech/Bor/server/internal/pki"
	"github.com/VuteTech/Bor/server/pkg/ext"
)

var (
	errNoCARotation      = errors.New("ext: the certificate authority cannot be rotated on this server")
	errCARestartRequired = errors.New("ext: a CA certificate was installed and the server has not been restarted yet; restart it first")
)

func caCertificate(cert *x509.Certificate) ext.CACertificate {
	return ext.CACertificate{
		Subject:     cert.Subject.String(),
		Issuer:      cert.Issuer.String(),
		SelfSigned:  bytes.Equal(cert.RawSubject, cert.RawIssuer),
		NotBefore:   cert.NotBefore,
		NotAfter:    cert.NotAfter,
		Fingerprint: pki.CertFingerprint(cert),
	}
}

// rotation returns the rotation state, or an error when the server has
// none.
func (p *pkiHost) rotation() (pki.CARotation, error) {
	if p.deps.CATrust == nil || p.deps.CACert == nil {
		return pki.CARotation{}, errNoCARotation
	}
	return p.deps.CATrust.Rotation(), nil
}

// restartPending reports whether the CA on disk differs from the one the
// server runs with.
func (p *pkiHost) restartPending(rot pki.CARotation) (bool, error) {
	onDisk, err := pki.LoadCAChain(rot.Store.CertPath)
	if err != nil {
		return false, fmt.Errorf("ext: reading the CA certificate file: %w", err)
	}
	return !onDisk.Signing.Equal(p.deps.CACert), nil
}

// changed makes a rotation step visible: the server recomputes what it
// trusts and asks the connected agents to fetch it.
func (p *pkiHost) changed(step string) error {
	if err := p.deps.CATrust.Reload(); err != nil {
		return fmt.Errorf("ext: reloading the CA trust: %w", err)
	}
	asked := 0
	if p.deps.RequestMetadata != nil {
		asked = p.deps.RequestMetadata()
	}
	log.Printf("ext: module %q: CA rotation: %s; %d connected agent(s) asked to update their CA certificates", p.module, step, asked)
	return nil
}

func (p *pkiHost) CARotationStatus(ctx context.Context) (ext.CARotationStatus, error) {
	rot, err := p.rotation()
	if err != nil {
		return ext.CARotationStatus{}, err
	}
	st := ext.CARotationStatus{Supported: rot.Supported()}
	if !st.Supported {
		st.UnsupportedReason = pki.ErrRotationUnsupported.Error()
		return st, nil
	}
	staged, err := rot.Staged()
	if err != nil {
		return st, fmt.Errorf("ext: reading the staged CA: %w", err)
	}
	if staged != nil {
		c := caCertificate(staged.Signing)
		st.Staged = &c
	}
	st.StagedRequestPending = rot.StagedRequestPending()
	previous, err := rot.Previous()
	if err != nil {
		return st, fmt.Errorf("ext: reading the previous CAs: %w", err)
	}
	for _, cert := range previous {
		st.Previous = append(st.Previous, caCertificate(cert))
	}
	// An activation is pending when the CA the server runs with has
	// already moved to the previous CAs.
	for _, cert := range previous {
		if cert.Equal(p.deps.CACert) {
			st.ActivationPending = true
		}
	}

	if p.deps.CATrustStats != nil {
		_, sha := p.deps.CATrust.AgentTrust()
		stats, statsErr := p.deps.CATrustStats(ctx, sha, p.deps.CATrust.ActiveIssuer())
		if statsErr != nil {
			return st, fmt.Errorf("ext: %w", statsErr)
		}
		st.Nodes, st.NodesHoldingTrust = stats.Nodes, stats.HoldingBundle
		st.NodesNeverReported, st.NodesOnOtherCA = stats.NeverReported, stats.OtherIssuer
		st.LaggingNodes = stats.Lagging
	}
	return st, nil
}

// stageable checks that a new CA may be staged now.
func (p *pkiHost) stageable() (pki.CARotation, error) {
	rot, err := p.rotation()
	if err != nil {
		return rot, err
	}
	pending, err := p.restartPending(rot)
	if err != nil {
		return rot, err
	}
	if pending {
		return rot, errCARestartRequired
	}
	return rot, nil
}

func (p *pkiHost) StageCA(_ context.Context, lifetime time.Duration) (ext.CACertificate, error) {
	rot, err := p.stageable()
	if err != nil {
		return ext.CACertificate{}, err
	}
	if lifetime <= 0 {
		lifetime = p.deps.CALifetime
	}
	chain, err := rot.StageSelfSigned(lifetime)
	if err != nil {
		return ext.CACertificate{}, err
	}
	if changedErr := p.changed(fmt.Sprintf("staged the new self-signed CA %q", chain.Signing.Subject)); changedErr != nil {
		return ext.CACertificate{}, changedErr
	}
	return caCertificate(chain.Signing), nil
}

func (p *pkiHost) StageCARequest(_ context.Context, commonName string) ([]byte, error) {
	rot, err := p.stageable()
	if err != nil {
		return nil, err
	}
	csrPEM, err := rot.StageRequest(commonName)
	if err != nil {
		return nil, err
	}
	log.Printf("ext: module %q: CA rotation: created the key and signing request of a new CA", p.module)
	return csrPEM, nil
}

func (p *pkiHost) InstallStagedCA(_ context.Context, bundlePEM []byte) (ext.CACertificate, error) {
	rot, err := p.stageable()
	if err != nil {
		return ext.CACertificate{}, err
	}
	chain, err := rot.StageInstall(bundlePEM)
	if err != nil {
		return ext.CACertificate{}, err
	}
	if changedErr := p.changed(fmt.Sprintf("staged the new CA %q issued by %q", chain.Signing.Subject, chain.Signing.Issuer)); changedErr != nil {
		return ext.CACertificate{}, changedErr
	}
	return caCertificate(chain.Signing), nil
}

func (p *pkiHost) DiscardStagedCA(_ context.Context) error {
	rot, err := p.rotation()
	if err != nil {
		return err
	}
	if discardErr := rot.Discard(); discardErr != nil {
		return discardErr
	}
	return p.changed("discarded the staged CA")
}

func (p *pkiHost) ActivateStagedCA(_ context.Context) (ext.CACertificate, error) {
	rot, err := p.stageable()
	if err != nil {
		return ext.CACertificate{}, err
	}
	chain, err := rot.Activate()
	if err != nil {
		return ext.CACertificate{}, err
	}
	if changedErr := p.changed(fmt.Sprintf("activated the CA %q; it is used from the next restart", chain.Signing.Subject)); changedErr != nil {
		return ext.CACertificate{}, changedErr
	}
	return caCertificate(chain.Signing), nil
}

func (p *pkiHost) RetirePreviousCAs(_ context.Context) (int, error) {
	rot, err := p.stageable()
	if err != nil {
		return 0, err
	}
	n, err := rot.RetirePrevious()
	if err != nil {
		return n, err
	}
	return n, p.changed(fmt.Sprintf("retired %d previous CA(s)", n))
}

var _ ext.CARotator = (*pkiHost)(nil)
