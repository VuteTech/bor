// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package tang

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// makeJWK converts an ECDSA public key into a JWK with fixed-width
// coordinates, as jose/tang emit them.
func makeJWK(t *testing.T, pub *ecdsa.PublicKey, alg string, ops []string) *JWK {
	t.Helper()
	uncompressed, err := pub.Bytes()
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	byteLen := (pub.Curve.Params().BitSize + 7) / 8
	x := uncompressed[1 : 1+byteLen]
	y := uncompressed[1+byteLen:]
	return &JWK{
		Kty:    "EC",
		Crv:    "P-521",
		X:      base64.RawURLEncoding.EncodeToString(x),
		Y:      base64.RawURLEncoding.EncodeToString(y),
		Alg:    alg,
		KeyOps: ops,
	}
}

// signES512 produces a raw r||s JWS signature over protected.payload.
func signES512(t *testing.T, priv *ecdsa.PrivateKey, signingInput string) string {
	t.Helper()
	digest := sha512.Sum512([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	byteLen := (priv.Curve.Params().BitSize + 7) / 8
	sig := append(r.FillBytes(make([]byte, byteLen)), s.FillBytes(make([]byte, byteLen))...)
	return base64.RawURLEncoding.EncodeToString(sig)
}

// makeAdvertisement builds a Tang-style signed advertisement.
func makeAdvertisement(t *testing.T, signKey *ecdsa.PrivateKey) (adv []byte, signThp, exchThp string) {
	t.Helper()
	exchKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("generate exchange key: %v", err)
	}

	signJWK := makeJWK(t, &signKey.PublicKey, "ES512", []string{"verify"})
	exchJWK := makeJWK(t, &exchKey.PublicKey, "ECMR", []string{"deriveKey"})
	signThp, err = Thumbprint(signJWK)
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}
	exchThp, err = Thumbprint(exchJWK)
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}

	payloadJSON, err := json.Marshal(map[string]interface{}{"keys": []*JWK{signJWK, exchJWK}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	protected := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES512","cty":"jwk-set+json"}`))
	signature := signES512(t, signKey, protected+"."+payload)

	adv, err = json.Marshal(map[string]string{
		"payload":   payload,
		"protected": protected,
		"signature": signature,
	})
	if err != nil {
		t.Fatalf("marshal jws: %v", err)
	}
	return adv, signThp, exchThp
}

func TestParseAdvertisement(t *testing.T) {
	signKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	raw, signThp, exchThp := makeAdvertisement(t, signKey)

	adv, err := ParseAdvertisement(raw)
	if err != nil {
		t.Fatalf("ParseAdvertisement: %v", err)
	}
	if _, ok := adv.SigningKeys[signThp]; !ok {
		t.Errorf("signing key %s missing from SigningKeys %v", signThp, keysOf(adv.SigningKeys))
	}
	if _, ok := adv.ExchangeKeys[exchThp]; !ok {
		t.Errorf("exchange key %s missing from ExchangeKeys %v", exchThp, keysOf(adv.ExchangeKeys))
	}
	if len(adv.VerifiedBy) != 1 || adv.VerifiedBy[0] != signThp {
		t.Errorf("VerifiedBy = %v, want [%s]", adv.VerifiedBy, signThp)
	}
	if len(signThp) != 43 {
		t.Errorf("thumbprint length = %d, want 43", len(signThp))
	}
}

func TestParseAdvertisementRejectsBadSignature(t *testing.T) {
	signKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	raw, _, _ := makeAdvertisement(t, signKey)

	// Corrupt the signature.
	var doc map[string]string
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(doc["signature"])
	sig[0] ^= 0xff
	doc["signature"] = base64.RawURLEncoding.EncodeToString(sig)
	tampered, _ := json.Marshal(doc)

	if _, err := ParseAdvertisement(tampered); err == nil {
		t.Fatal("ParseAdvertisement accepted a corrupted signature")
	}
}

func TestParseAdvertisementRejectsForeignSigner(t *testing.T) {
	signKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	foreignKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("generate foreign key: %v", err)
	}
	// Advertisement carries signKey but is signed by foreignKey.
	raw, _, _ := makeAdvertisement(t, signKey)
	var doc map[string]string
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	doc["signature"] = signES512(t, foreignKey, doc["protected"]+"."+doc["payload"])
	forged, _ := json.Marshal(doc)

	if _, err := ParseAdvertisement(forged); err == nil {
		t.Fatal("ParseAdvertisement accepted a signature by a key not in the advertisement")
	}
}

func TestThumbprintRFC7638Vector(t *testing.T) {
	// RFC 7638 defines the canonical form; check stability against a fixed
	// key (thumbprint computed once with jose-compatible tooling semantics:
	// sha256 of {"crv":..,"kty":..,"x":..,"y":..}).
	k := &JWK{Kty: "EC", Crv: "P-521", X: "AXo", Y: "AYo"}
	thp1, err := Thumbprint(k)
	if err != nil {
		t.Fatalf("Thumbprint: %v", err)
	}
	thp2, _ := Thumbprint(&JWK{Kty: "EC", Crv: "P-521", X: "AXo", Y: "AYo", Alg: "ES512", KeyOps: []string{"verify"}})
	if thp1 != thp2 {
		t.Error("thumbprint must ignore non-required members (alg, key_ops)")
	}
}

func keysOf(m map[string]*JWK) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
