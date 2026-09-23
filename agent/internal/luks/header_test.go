// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

const testUUID = "3f2a9c1e-1111-2222-3333-444455556666"

// b64url encodes for JWE protected headers.
func b64url(v interface{}) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// tangProtectedHeader builds a Clevis tang JWE protected header with an
// embedded advertisement carrying one signing and one exchange key.
func tangProtectedHeader(url, signX, signY string) string {
	return b64url(map[string]interface{}{
		"alg": "ECDH-ES",
		"enc": "A256GCM",
		"clevis": map[string]interface{}{
			"pin": "tang",
			"tang": map[string]interface{}{
				"url": url,
				"adv": map[string]interface{}{
					"keys": []map[string]interface{}{
						{"kty": "EC", "crv": "P-521", "x": signX, "y": signY, "alg": "ES512", "key_ops": []string{"verify"}},
						{"kty": "EC", "crv": "P-521", "x": "eX", "y": "eY", "alg": "ECMR", "key_ops": []string{"deriveKey"}},
					},
				},
			},
		},
	})
}

// headerFixture renders a LUKS2 JSON metadata document.
func headerFixture(keyslots, tokens string) []byte {
	return []byte(fmt.Sprintf(`{
	  "keyslots": {%s},
	  "tokens": {%s},
	  "segments": {"0": {"type": "crypt", "encryption": "aes-xts-plain64", "sector_size": 512}},
	  "digests": {},
	  "config": {"json_size": "12288", "keyslots_size": "16744448"}
	}`, keyslots, tokens))
}

const slotJSON = `"%d": {"type": "luks2", "key_size": 64,
  "kdf": {"type": "%s", "hash": "%s", "iterations": 1000, "salt": "%s"}}`

func slot(idx int, kdf, hash, salt string) string {
	return fmt.Sprintf(slotJSON, idx, kdf, hash, salt)
}

func TestParseHeaderClassification(t *testing.T) {
	tang := tangProtectedHeader("http://tang1.corp", "sX", "sY")
	keyslots := strings.Join([]string{
		slot(0, "argon2id", "sha256", "c2FsdDA="), // password (no token)
		slot(1, "pbkdf2", "sha512", "c2FsdDE="),   // tpm2
		slot(2, "pbkdf2", "sha512", "c2FsdDI="),   // bor recovery
		slot(3, "argon2id", "sha256", "c2FsdDM="), // clevis tang
		slot(4, "pbkdf2", "sha512", "c2FsdDQ="),   // foreign recovery
		slot(5, "argon2id", "sha256", "c2FsdDU="), // unknown token
	}, ",")
	tokens := strings.Join([]string{
		`"0": {"type": "systemd-tpm2", "keyslots": ["1"], "tpm2-pcrs": [7], "tpm2-blob": "AA"}`,
		`"1": {"type": "systemd-recovery", "keyslots": ["2"]}`,
		`"2": {"type": "clevis", "keyslots": ["3"], "jwe": {"protected": "` + tang + `", "ciphertext": ""}}`,
		`"3": {"type": "bor-escrow", "keyslots": [], "bor": {"v": 1, "escrow_id": "esc-1", "keyslot": "2", "server": "bor.example.com"}}`,
		`"4": {"type": "systemd-recovery", "keyslots": ["4"]}`,
		`"5": {"type": "vendor-magic", "keyslots": ["5"]}`,
	}, ",")

	h, err := ParseHeader(headerFixture(keyslots, tokens), testUUID)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.Cipher != "aes-xts-plain64" {
		t.Errorf("cipher = %q", h.Cipher)
	}
	if h.VolumeKeyBits != 512 {
		t.Errorf("volume key bits = %d, want 512", h.VolumeKeyBits)
	}
	if h.JSONAreaFree <= 0 || h.JSONAreaFree >= 12288 {
		t.Errorf("JSON area free = %d", h.JSONAreaFree)
	}
	if h.BorEscrow == nil || h.BorEscrow.EscrowID != "esc-1" || h.BorEscrow.TokenID != "3" {
		t.Fatalf("bor escrow token = %+v", h.BorEscrow)
	}

	want := map[int]pb.LuksKeyslotKind{
		0: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_PASSWORD,
		1: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_TPM2,
		2: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY,
		3: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS,
		4: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_RECOVERY,
		5: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_OTHER,
	}
	for idx, kind := range want {
		s := h.FindSlot(idx)
		if s == nil {
			t.Fatalf("slot %d missing", idx)
		}
		if s.Kind != kind {
			t.Errorf("slot %d kind = %v, want %v", idx, s.Kind, kind)
		}
	}

	if s := h.FindSlot(1); len(s.TPM2PCRs) != 1 || s.TPM2PCRs[0] != 7 {
		t.Errorf("tpm2 slot PCRs = %v", s.TPM2PCRs)
	}
	if s := h.FindSlot(2); s.BorEscrowID != "esc-1" {
		t.Errorf("bor recovery slot escrow id = %q", s.BorEscrowID)
	}
	if s := h.FindSlot(3); s.ClevisPin != "tang" || len(s.TangURLs) != 1 || s.TangURLs[0] != "http://tang1.corp" {
		t.Errorf("clevis slot = pin %q urls %v", s.ClevisPin, s.TangURLs)
	}
	if s := h.FindSlot(3); len(s.TangSigningThumbprints) != 1 {
		t.Errorf("clevis slot thumbprints = %v", s.TangSigningThumbprints)
	}
	if s := h.FindSlot(0); s.KDF != "argon2id" {
		t.Errorf("slot 0 kdf = %q", s.KDF)
	}
	if s := h.FindSlot(1); s.KDF != "pbkdf2-sha512" {
		t.Errorf("slot 1 kdf = %q", s.KDF)
	}
}

func TestKeyslotFingerprintStability(t *testing.T) {
	fp1 := KeyslotFingerprint(testUUID, 2, "c2FsdA==")
	fp2 := KeyslotFingerprint(testUUID, 2, "c2FsdA==")
	if fp1 != fp2 {
		t.Error("fingerprint is not deterministic")
	}
	if len(fp1) != 64 {
		t.Errorf("fingerprint length = %d, want 64 hex chars", len(fp1))
	}
	// A re-keyed slot has a new salt -> new fingerprint (drift detection).
	if fp1 == KeyslotFingerprint(testUUID, 2, "b3RoZXI=") {
		t.Error("fingerprint must change with the salt")
	}
	if fp1 == KeyslotFingerprint(testUUID, 3, "c2FsdA==") {
		t.Error("fingerprint must change with the slot index")
	}
	if fp1 == KeyslotFingerprint("other-uuid", 2, "c2FsdA==") {
		t.Error("fingerprint must change with the volume UUID")
	}
}

func TestDecodeClevisSSS(t *testing.T) {
	inner1 := tangProtectedHeader("http://tang1.corp", "aX", "aY")
	inner2 := tangProtectedHeader("http://tang2.corp", "bX", "bY")
	outer := b64url(map[string]interface{}{
		"alg": "dir",
		"enc": "A256GCM",
		"clevis": map[string]interface{}{
			"pin": "sss",
			"sss": map[string]interface{}{
				"t":   2,
				"jwe": []string{inner1 + ".e.i.c.t", inner2 + ".e.i.c.t"},
			},
		},
	})
	info, err := DecodeClevisProtected(outer)
	if err != nil {
		t.Fatalf("DecodeClevisProtected: %v", err)
	}
	if info.Pin != "sss" || info.Threshold != 2 {
		t.Errorf("pin=%q threshold=%d", info.Pin, info.Threshold)
	}
	if len(info.TangURLs) != 2 {
		t.Errorf("urls = %v", info.TangURLs)
	}
	if len(info.SigningThumbprints) != 2 {
		t.Errorf("thumbprints = %v", info.SigningThumbprints)
	}
	for _, thp := range info.SigningThumbprints {
		if len(thp) != 43 {
			t.Errorf("thumbprint %q is not 43 chars", thp)
		}
	}
}

func TestGenerateRecoveryKeyFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		key, err := GenerateRecoveryKey()
		if err != nil {
			t.Fatalf("GenerateRecoveryKey: %v", err)
		}
		s := string(key)
		if len(s) != 71 {
			t.Fatalf("key length = %d, want 71", len(s))
		}
		groups := strings.Split(s, "-")
		if len(groups) != 8 {
			t.Fatalf("key has %d groups, want 8", len(groups))
		}
		for _, g := range groups {
			if len(g) != 8 {
				t.Fatalf("group %q is not 8 chars", g)
			}
			for _, c := range g {
				if !strings.ContainsRune(modhexAlphabet, c) {
					t.Fatalf("character %q is not modhex", c)
				}
			}
		}
		if seen[s] {
			t.Fatal("duplicate key generated")
		}
		seen[s] = true
	}
}

// TestRecoveryKeyServerParity keeps the generator in lockstep with the
// server-side validation regex.
func TestRecoveryKeyServerParity(t *testing.T) {
	// The server validates ^([cbdefghijklnrtuv]{8}-){7}[cbdefghijklnrtuv]{8}$.
	key, err := GenerateRecoveryKey()
	if err != nil {
		t.Fatalf("GenerateRecoveryKey: %v", err)
	}
	groups := strings.Split(string(key), "-")
	if len(groups) != 8 {
		t.Fatalf("groups = %d", len(groups))
	}
	for _, g := range groups {
		if len(g) != 8 || strings.Trim(g, modhexAlphabet) != "" {
			t.Fatalf("group %q does not match the server regex", g)
		}
	}
}

func TestCanWipeSlot(t *testing.T) {
	h := &Header{Keyslots: []*Keyslot{
		{Index: 0, Kind: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_PASSWORD},
		{Index: 1, Kind: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY},
		{Index: 2, Kind: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY},
	}}
	if err := CanWipeSlot(h, 2, 1); err != nil {
		t.Errorf("wiping the old recovery slot must be allowed: %v", err)
	}
	if err := CanWipeSlot(h, 1, 1); err == nil {
		t.Error("wiping the confirmed recovery slot must be refused")
	}
	if err := CanWipeSlot(h, 0, 9); err == nil {
		t.Error("wiping with a missing confirmed slot must be refused")
	}
	// Only the recovery slot would remain -> refuse.
	h2 := &Header{Keyslots: []*Keyslot{
		{Index: 0, Kind: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_PASSWORD},
		{Index: 1, Kind: pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY},
	}}
	if err := CanWipeSlot(h2, 0, 1); err == nil {
		t.Error("wiping the last non-recovery protector must be refused")
	}
}
