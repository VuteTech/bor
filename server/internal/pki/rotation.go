// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CertFingerprint returns the hex SHA-256 of a certificate, the identifier
// used for CA certificates in rotation state and node records.
func CertFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// CARotation replaces the CA with a new one that has its own key, in steps
// that keep enrolled agents working:
//
//  1. Stage: a new CA is created next to the active one. Agents are told to
//     trust both (see TrustState); nothing is signed by the new CA yet.
//  2. Activate: the staged CA becomes the CA on disk and the replaced one
//     is kept as "previous". The server uses the new CA after a restart;
//     certificates issued by the previous CA stay accepted, and agents
//     renew theirs from the new CA.
//  3. Retire: the previous CA is removed once no agent depends on it.
//
// State lives in the directory "rotation" next to the CA certificate:
// next.crt, next.key and next.csr for the staged CA, and
// previous/<fingerprint>.crt (and .key) for replaced ones.
//
// Rotation needs the CA key in a file; a key in a PKCS#11 token is not
// supported.
type CARotation struct {
	Store CAStore
}

// ErrRotationUnsupported is returned when the CA key is not a file.
var ErrRotationUnsupported = errors.New("CA rotation requires the CA key in a file; a key in a PKCS#11 token is not supported")

func (r CARotation) dir() string         { return filepath.Join(filepath.Dir(r.Store.CertPath), "rotation") }
func (r CARotation) previousDir() string { return filepath.Join(r.dir(), "previous") }

// stagedStore is where the staged CA lives.
func (r CARotation) stagedStore() CAStore {
	return CAStore{CertPath: filepath.Join(r.dir(), "next.crt"), KeyPath: filepath.Join(r.dir(), "next.key")}
}

// Supported reports whether the CA can be rotated.
func (r CARotation) Supported() bool { return r.Store.KeyPath != "" }

// Staged returns the staged CA, or nil when none is staged (or it is still
// waiting for its certificate).
func (r CARotation) Staged() (*CAChain, error) {
	staged := r.stagedStore()
	if !fileExists(staged.CertPath) {
		return nil, nil //nolint:nilnil // "nothing staged" is not an error
	}
	return LoadCAChain(staged.CertPath)
}

// StagedRequestPending reports whether a key and signing request were
// created for the staged CA and its certificate is not installed yet.
func (r CARotation) StagedRequestPending() bool {
	return r.stagedStore().RequestPending()
}

// Previous returns the signing certificates of replaced CAs that are still
// trusted, oldest first.
func (r CARotation) Previous() ([]*x509.Certificate, error) {
	entries, err := os.ReadDir(r.previousDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list previous CA certificates: %w", err)
	}
	var out []*x509.Certificate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		cert, loadErr := loadCACertFile(filepath.Join(r.previousDir(), e.Name()))
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, cert)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NotBefore.Before(out[j].NotBefore) })
	return out, nil
}

func loadCACertFile(path string) (*x509.Certificate, error) {
	chain, err := LoadCAChain(path)
	if err != nil {
		return nil, err
	}
	return chain.Signing, nil
}

func (r CARotation) ensureNothingStaged() error {
	if !r.Supported() {
		return ErrRotationUnsupported
	}
	staged := r.stagedStore()
	if fileExists(staged.CertPath) || fileExists(staged.KeyPath) {
		return errors.New("a new CA is already staged; activate or discard it first")
	}
	return nil
}

// StageSelfSigned creates a new self-signed CA (ECDSA P-384, valid for
// lifetime; non-positive means DefaultCALifetime) and stages it.
func (r CARotation) StageSelfSigned(lifetime time.Duration) (*CAChain, error) {
	if err := r.ensureNothingStaged(); err != nil {
		return nil, err
	}
	if lifetime <= 0 {
		lifetime = DefaultCALifetime
	}
	staged := r.stagedStore()
	key, _, err := LoadOrCreateCAKey(staged.KeyPath)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA serial number: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Bor"},
			// The date tells the CA apart from the one it replaces.
			CommonName: "Bor Internal CA " + now.UTC().Format("2006-01-02"),
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(lifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		_ = os.Remove(staged.KeyPath)
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}
	if err = writePEM(staged.CertPath, "CERTIFICATE", der); err != nil {
		_ = os.Remove(staged.KeyPath)
		return nil, err
	}
	return LoadCAChain(staged.CertPath)
}

// StageRequest creates the key of a new CA and returns a certificate
// signing request for it, for a CA that an external authority certifies.
// An empty commonName requests the subject of the active CA. The CA is
// staged once StageInstall has stored its certificate.
func (r CARotation) StageRequest(commonName string) ([]byte, error) {
	if !r.Supported() {
		return nil, ErrRotationUnsupported
	}
	staged := r.stagedStore()
	if fileExists(staged.CertPath) {
		return nil, errors.New("a new CA is already staged; activate or discard it first")
	}
	if strings.TrimSpace(commonName) == "" {
		active, err := LoadCAChain(r.Store.CertPath)
		if err != nil {
			return nil, err
		}
		commonName = active.Signing.Subject.CommonName
	}
	key, _, err := LoadOrCreateCAKey(staged.KeyPath)
	if err != nil {
		return nil, err
	}
	return (&CAManager{Store: staged, Key: key}).CreateRequest(commonName)
}

// StageInstall stores the certificate an authority issued for the request
// of StageRequest: the new signing certificate, optionally followed by its
// issuers up to the root.
func (r CARotation) StageInstall(bundlePEM []byte) (*CAChain, error) {
	if !r.Supported() {
		return nil, ErrRotationUnsupported
	}
	staged := r.stagedStore()
	if !fileExists(staged.KeyPath) {
		return nil, errors.New("no signing request was created for a new CA")
	}
	if fileExists(staged.CertPath) {
		return nil, errors.New("a new CA is already staged; activate or discard it first")
	}
	key, _, err := LoadOrCreateCAKey(staged.KeyPath)
	if err != nil {
		return nil, err
	}
	res, err := (&CAManager{Store: staged, Key: key}).Install(bundlePEM)
	if err != nil {
		return nil, err
	}
	return res.Chain, nil
}

// Discard removes the staged CA (or its pending request). Agents that
// already trust it stop doing so with their next trust update.
func (r CARotation) Discard() error {
	staged := r.stagedStore()
	for _, p := range []string{staged.CertPath, staged.KeyPath, staged.RequestPath(), staged.PreviousPath()} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to remove %s: %w", p, err)
		}
	}
	return nil
}

// Activate makes the staged CA the CA on disk. The replaced certificate and
// key move to the previous directory, so certificates it issued stay
// accepted and the step can be undone by hand until RetirePrevious. A
// running server keeps using the CA it loaded at startup.
func (r CARotation) Activate() (*CAChain, error) {
	if !r.Supported() {
		return nil, ErrRotationUnsupported
	}
	staged := r.stagedStore()
	next, err := r.Staged()
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("no new CA is staged")
	}
	nextKey, _, err := LoadOrCreateCAKey(staged.KeyPath)
	if err != nil {
		return nil, err
	}
	if err = next.Validate(nextKey); err != nil {
		return nil, fmt.Errorf("the staged CA is not usable: %w", err)
	}
	active, err := LoadCAChain(r.Store.CertPath)
	if err != nil {
		return nil, err
	}

	// Keep the replaced CA first: if anything below fails, the active CA is
	// still in place.
	if err = os.MkdirAll(r.previousDir(), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", r.previousDir(), err)
	}
	base := filepath.Join(r.previousDir(), CertFingerprint(active.Signing)[:16])
	if err = os.WriteFile(base+".crt", EncodeCertPEM(active.Signing), 0o600); err != nil {
		return nil, fmt.Errorf("failed to keep the replaced CA certificate: %w", err)
	}
	oldKey, err := os.ReadFile(r.Store.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the CA key: %w", err)
	}
	if err = os.WriteFile(base+".key", oldKey, 0o600); err != nil { //nolint:gosec // path derives from the admin-configured CA certificate path
		return nil, fmt.Errorf("failed to keep the replaced CA key: %w", err)
	}

	// Key first: a crash between the two renames leaves a key without its
	// certificate, which the server refuses to start with (key mismatch)
	// instead of silently running on the old CA.
	if err = os.Rename(staged.KeyPath, r.Store.KeyPath); err != nil {
		return nil, fmt.Errorf("failed to install the new CA key: %w", err)
	}
	if err = os.Rename(staged.CertPath, r.Store.CertPath); err != nil {
		return nil, fmt.Errorf("failed to install the new CA certificate (restore %s.key to %s to undo): %w",
			base, r.Store.KeyPath, err)
	}
	_ = os.Remove(staged.PreviousPath())
	return next, nil
}

// RetirePrevious removes every replaced CA: certificates they issued are
// no longer accepted and agents stop trusting them. Their keys are deleted.
// It returns how many were removed.
func (r CARotation) RetirePrevious() (int, error) {
	entries, err := os.ReadDir(r.previousDir())
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to list previous CA certificates: %w", err)
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err = os.Remove(filepath.Join(r.previousDir(), e.Name())); err != nil {
			return removed, fmt.Errorf("failed to remove %s: %w", e.Name(), err)
		}
		if strings.HasSuffix(e.Name(), ".crt") {
			removed++
		}
	}
	return removed, nil
}

// TrustState is what a running server trusts and tells agents to trust. It
// follows the rotation state on disk: call Reload after every rotation
// step.
type TrustState struct {
	active         *CAChain // the CA the server runs with
	includeIssuers bool     // agent trust "chain": hand out the issuers too
	store          CAStore
	rotation       CARotation

	mu        sync.RWMutex
	bundle    []byte
	bundleSHA string
	clientCAs *x509.CertPool
}

// NewTrustState creates the trust state of a server running with the CA
// active, stored at store. With includeIssuers agents receive the
// authorities above each CA as well.
func NewTrustState(active *CAChain, store CAStore, includeIssuers bool) (*TrustState, error) {
	t := &TrustState{active: active, includeIssuers: includeIssuers, store: store, rotation: CARotation{Store: store}}
	if err := t.Reload(); err != nil {
		return nil, err
	}
	return t, nil
}

// Reload recomputes the trust from the rotation state on disk.
//
// Agents are told to trust the CA the server runs with, the CA on disk when
// it differs (an activated CA waiting for the restart), the staged CA and
// the previous CAs. Client certificates are accepted from the running CA
// and the previous ones.
func (t *TrustState) Reload() error {
	chains := []*CAChain{t.active}
	if onDisk, err := LoadCAChain(t.store.CertPath); err == nil {
		chains = append(chains, onDisk)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	staged, err := t.rotation.Staged()
	if err != nil {
		return err
	}
	if staged != nil {
		chains = append(chains, staged)
	}
	previous, err := t.rotation.Previous()
	if err != nil {
		return err
	}
	for _, cert := range previous {
		chains = append(chains, &CAChain{Signing: cert})
	}

	// The certificates are ordered by fingerprint, so the bundle (and with
	// it the fingerprint agents report) depends only on which CAs are
	// trusted, not on which of them is active.
	certs := make(map[string]*x509.Certificate)
	for _, chain := range chains {
		certs[CertFingerprint(chain.Signing)] = chain.Signing
		if t.includeIssuers {
			for _, issuer := range chain.Issuers {
				certs[CertFingerprint(issuer)] = issuer
			}
		}
	}
	fingerprints := make([]string, 0, len(certs))
	for fp := range certs {
		fingerprints = append(fingerprints, fp)
	}
	sort.Strings(fingerprints)
	var bundle []byte
	for _, fp := range fingerprints {
		bundle = append(bundle, EncodeCertPEM(certs[fp])...)
	}

	pool := x509.NewCertPool()
	pool.AddCert(t.active.Signing)
	for _, cert := range previous {
		pool.AddCert(cert)
	}
	sum := sha256.Sum256(bundle)

	t.mu.Lock()
	t.bundle, t.bundleSHA, t.clientCAs = bundle, hex.EncodeToString(sum[:]), pool
	t.mu.Unlock()
	return nil
}

// AgentTrust returns the CA certificates agents must trust, in PEM, and
// their hex SHA-256 (what an agent that holds exactly this file reports).
func (t *TrustState) AgentTrust() (bundlePEM []byte, sha string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.bundle, t.bundleSHA
}

// AgentTrustPEM is AgentTrust without the fingerprint.
func (t *TrustState) AgentTrustPEM() []byte {
	bundle, _ := t.AgentTrust()
	return bundle
}

// ClientCAs returns the pool client (agent) certificates are verified
// against.
func (t *TrustState) ClientCAs() *x509.CertPool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.clientCAs
}

// ActiveIssuer returns the fingerprint of the signing certificate the
// server issues agent certificates from.
func (t *TrustState) ActiveIssuer() string { return CertFingerprint(t.active.Signing) }

// Rotation returns the rotation state this trust follows.
func (t *TrustState) Rotation() CARotation { return t.rotation }
