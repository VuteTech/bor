// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package tang parses and verifies Tang server advertisements: JWS-signed
// JWK sets served at GET /adv (github.com/latchset/tang). Signatures are
// ES512 (raw r||s) and key identity is the RFC 7638 SHA-256 thumbprint.
// The Phase 2 Bor Tang responder will live in this package too.
package tang

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"slices"
)

// JWK is the subset of a JSON Web Key that Tang advertisements carry.
type JWK struct {
	Kty    string   `json:"kty"`
	Crv    string   `json:"crv,omitempty"`
	X      string   `json:"x,omitempty"`
	Y      string   `json:"y,omitempty"`
	Alg    string   `json:"alg,omitempty"`
	KeyOps []string `json:"key_ops,omitempty"`
}

// Advertisement is a parsed, verified Tang /adv response.
type Advertisement struct {
	// SigningKeys are the ES512 verification keys, by RFC 7638 thumbprint.
	SigningKeys map[string]*JWK
	// ExchangeKeys are the ECMR derive keys, by RFC 7638 thumbprint.
	ExchangeKeys map[string]*JWK
	// VerifiedBy lists the thumbprints of the signing keys whose signature
	// over this advertisement checked out.
	VerifiedBy []string
	// Raw is the original JWS document.
	Raw []byte
}

// jws is the JSON (general or flattened) serialization of a JWS.
type jws struct {
	Payload    string         `json:"payload"`
	Protected  string         `json:"protected,omitempty"`
	Signature  string         `json:"signature,omitempty"`
	Signatures []jwsSignature `json:"signatures,omitempty"`
}

type jwsSignature struct {
	Protected string `json:"protected"`
	Signature string `json:"signature"`
}

// ParseAdvertisement parses a Tang /adv JWS and verifies its signatures
// against the embedded verification keys. At least one valid signature is
// required; VerifiedBy carries the thumbprints of the keys that verified.
// The trust decision (comparing VerifiedBy against admin-confirmed
// thumbprints) is the caller's.
func ParseAdvertisement(raw []byte) (*Advertisement, error) {
	var doc jws
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("tang: advertisement is not a JSON JWS: %w", err)
	}
	if doc.Payload == "" {
		return nil, errors.New("tang: advertisement has no payload")
	}
	payload, err := b64url(doc.Payload)
	if err != nil {
		return nil, fmt.Errorf("tang: payload: %w", err)
	}

	var keySet struct {
		Keys []*JWK `json:"keys"`
	}
	if err := json.Unmarshal(payload, &keySet); err != nil {
		return nil, fmt.Errorf("tang: payload is not a JWK set: %w", err)
	}
	if len(keySet.Keys) == 0 {
		return nil, errors.New("tang: advertisement carries no keys")
	}

	adv := &Advertisement{
		SigningKeys:  map[string]*JWK{},
		ExchangeKeys: map[string]*JWK{},
		Raw:          raw,
	}
	for _, k := range keySet.Keys {
		if k.Kty != "EC" {
			continue // Tang only uses EC keys; ignore anything else.
		}
		thp, err := Thumbprint(k)
		if err != nil {
			return nil, err
		}
		switch {
		case slices.Contains(k.KeyOps, "verify") || k.Alg == "ES512" || k.Alg == "ES256" || k.Alg == "ES384":
			adv.SigningKeys[thp] = k
		case slices.Contains(k.KeyOps, "deriveKey") || k.Alg == "ECMR":
			adv.ExchangeKeys[thp] = k
		}
	}
	if len(adv.SigningKeys) == 0 {
		return nil, errors.New("tang: advertisement carries no signing keys")
	}

	sigs := doc.Signatures
	if len(sigs) == 0 && doc.Signature != "" {
		sigs = []jwsSignature{{Protected: doc.Protected, Signature: doc.Signature}}
	}
	if len(sigs) == 0 {
		return nil, errors.New("tang: advertisement carries no signatures")
	}

	for _, sig := range sigs {
		signingInput := []byte(sig.Protected + "." + doc.Payload)
		rawSig, err := b64url(sig.Signature)
		if err != nil {
			continue
		}
		for thp, key := range adv.SigningKeys {
			if slices.Contains(adv.VerifiedBy, thp) {
				continue
			}
			ok, err := verifyECDSA(key, signingInput, rawSig)
			if err == nil && ok {
				adv.VerifiedBy = append(adv.VerifiedBy, thp)
			}
		}
	}
	if len(adv.VerifiedBy) == 0 {
		return nil, errors.New("tang: no signature verified against the advertised signing keys")
	}
	slices.Sort(adv.VerifiedBy)
	return adv, nil
}

// Thumbprint computes the RFC 7638 SHA-256 thumbprint of an EC JWK
// (base64url, 43 characters).
func Thumbprint(k *JWK) (string, error) {
	if k.Kty != "EC" || k.Crv == "" || k.X == "" || k.Y == "" {
		return "", errors.New("tang: thumbprint requires an EC key with crv, x and y")
	}
	// RFC 7638 §3.2: required members only, lexicographic order, no spaces.
	canonical, err := json.Marshal(struct {
		Crv string `json:"crv"`
		Kty string `json:"kty"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}{k.Crv, k.Kty, k.X, k.Y})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// verifyECDSA checks a raw r||s JWS signature with the given EC JWK.
func verifyECDSA(k *JWK, signingInput, sig []byte) (bool, error) {
	pub, err := publicKey(k)
	if err != nil {
		return false, err
	}
	byteLen := (pub.Curve.Params().BitSize + 7) / 8
	if len(sig) != 2*byteLen {
		return false, fmt.Errorf("tang: signature length %d does not match curve %s", len(sig), k.Crv)
	}
	r := new(big.Int).SetBytes(sig[:byteLen])
	s := new(big.Int).SetBytes(sig[byteLen:])

	var h hash.Hash
	switch k.Crv {
	case "P-256":
		h = sha256.New()
	case "P-384":
		h = sha512.New384()
	case "P-521":
		h = sha512.New()
	default:
		return false, fmt.Errorf("tang: unsupported curve %q", k.Crv)
	}
	h.Write(signingInput)
	return ecdsa.Verify(pub, h.Sum(nil), r, s), nil
}

// publicKey converts an EC JWK to an ecdsa.PublicKey.
// ecdsa.ParseUncompressedPublicKey validates that the point is on the curve.
func publicKey(k *JWK) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("tang: unsupported curve %q", k.Crv)
	}
	xb, err := b64url(k.X)
	if err != nil {
		return nil, fmt.Errorf("tang: jwk x: %w", err)
	}
	yb, err := b64url(k.Y)
	if err != nil {
		return nil, fmt.Errorf("tang: jwk y: %w", err)
	}
	byteLen := (curve.Params().BitSize + 7) / 8
	if len(xb) > byteLen || len(yb) > byteLen {
		return nil, errors.New("tang: jwk coordinate too long for curve")
	}
	// Left-pad the coordinates into the 0x04 || X || Y encoding.
	uncompressed := make([]byte, 1+2*byteLen)
	uncompressed[0] = 4
	copy(uncompressed[1+byteLen-len(xb):1+byteLen], xb)
	copy(uncompressed[1+2*byteLen-len(yb):], yb)
	pub, err := ecdsa.ParseUncompressedPublicKey(curve, uncompressed)
	if err != nil {
		return nil, fmt.Errorf("tang: invalid jwk point: %w", err)
	}
	return pub, nil
}

func b64url(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
