// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ClevisInfo is the metadata decoded from a Clevis LUKS2 token's JWE
// protected header - more than `clevis luks list` shows: the pin, URLs,
// threshold, and the signing-key thumbprints of the advertisement embedded
// at bind time. No key material is involved; the JWE payload is
// never touched.
type ClevisInfo struct {
	// Pin is "tang", "sss" or "tpm2".
	Pin string
	// Threshold is the sss "t" (0 when not sss).
	Threshold uint32
	// TangURLs are every Tang server URL found (nested pins included).
	TangURLs []string
	// SigningThumbprints are the RFC 7638 S256 thumbprints of the signing
	// keys in the embedded advertisements. A binding whose thumbprints do
	// not include the registry's preferred key is stale.
	SigningThumbprints []string
}

// clevisProtected mirrors the JWE protected header layout Clevis writes.
type clevisProtected struct {
	Clevis struct {
		Pin  string `json:"pin"`
		Tang struct {
			URL string          `json:"url"`
			Adv json.RawMessage `json:"adv"`
		} `json:"tang"`
		SSS struct {
			T    uint32   `json:"t"`
			JWEs []string `json:"jwe"`
		} `json:"sss"`
	} `json:"clevis"`
}

// clevisJWK is the subset of a JWK needed for RFC 7638 thumbprints.
type clevisJWK struct {
	Kty    string   `json:"kty"`
	Crv    string   `json:"crv"`
	X      string   `json:"x"`
	Y      string   `json:"y"`
	Alg    string   `json:"alg"`
	KeyOps []string `json:"key_ops"`
}

// DecodeClevisProtected decodes a Clevis token's base64url protected header,
// following nested sss JWEs recursively.
func DecodeClevisProtected(protected string) (*ClevisInfo, error) {
	info := &ClevisInfo{}
	if err := decodeClevisInto(protected, info, 0); err != nil {
		return nil, err
	}
	sort.Strings(info.TangURLs)
	sort.Strings(info.SigningThumbprints)
	return info, nil
}

// decodeClevisInto accumulates pin metadata into info. depth caps sss
// nesting (Clevis itself only nests one level).
func decodeClevisInto(protected string, info *ClevisInfo, depth int) error {
	if depth > 3 {
		return errors.New("luks: clevis sss nesting too deep")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(protected, "="))
	if err != nil {
		return fmt.Errorf("luks: clevis protected header: %w", err)
	}
	var hdr clevisProtected
	if err := json.Unmarshal(raw, &hdr); err != nil {
		return fmt.Errorf("luks: clevis protected header JSON: %w", err)
	}
	if info.Pin == "" {
		info.Pin = hdr.Clevis.Pin
	}
	switch hdr.Clevis.Pin {
	case "tang":
		if u := hdr.Clevis.Tang.URL; u != "" && !slices.Contains(info.TangURLs, u) {
			info.TangURLs = append(info.TangURLs, u)
		}
		for _, thp := range advSigningThumbprints(hdr.Clevis.Tang.Adv) {
			if !slices.Contains(info.SigningThumbprints, thp) {
				info.SigningThumbprints = append(info.SigningThumbprints, thp)
			}
		}
	case "sss":
		if depth == 0 {
			info.Threshold = hdr.Clevis.SSS.T
		}
		for _, compact := range hdr.Clevis.SSS.JWEs {
			// Compact JWE: protected.encrypted_key.iv.ciphertext.tag -
			// only the protected header is decoded.
			parts := strings.SplitN(compact, ".", 2)
			if len(parts) == 0 || parts[0] == "" {
				continue
			}
			if err := decodeClevisInto(parts[0], info, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// advSigningThumbprints extracts the RFC 7638 thumbprints of the signing
// keys in an embedded Tang advertisement (a bare JWKSet or a JWS whose
// payload is one).
func advSigningThumbprints(adv json.RawMessage) []string {
	if len(adv) == 0 {
		return nil
	}
	var keySet struct {
		Keys    []*clevisJWK `json:"keys"`
		Payload string       `json:"payload"`
	}
	if err := json.Unmarshal(adv, &keySet); err != nil {
		return nil
	}
	if len(keySet.Keys) == 0 && keySet.Payload != "" {
		// JWS form: the JWKSet is the payload.
		payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(keySet.Payload, "="))
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(payload, &keySet); err != nil {
			return nil
		}
	}
	var out []string
	for _, k := range keySet.Keys {
		if k.Kty != "EC" {
			continue
		}
		isSigner := slices.Contains(k.KeyOps, "verify") ||
			k.Alg == "ES512" || k.Alg == "ES384" || k.Alg == "ES256"
		if !isSigner {
			continue
		}
		if thp, err := jwkThumbprint(k); err == nil {
			out = append(out, thp)
		}
	}
	return out
}

// jwkThumbprint computes the RFC 7638 SHA-256 thumbprint of an EC JWK
// (base64url, 43 chars). Kept in parity with the server's tang package.
func jwkThumbprint(k *clevisJWK) (string, error) {
	if k.Kty != "EC" || k.Crv == "" || k.X == "" || k.Y == "" {
		return "", errors.New("luks: thumbprint requires an EC key with crv, x and y")
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
