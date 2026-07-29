// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"context"
	"testing"

	"github.com/VuteTech/Bor/server/internal/database"
)

func TestAESEncryptDecrypt_Roundtrip(t *testing.T) {
	key := deriveAESKey("test-passphrase")
	plaintext := []byte("JBSWY3DPEHPK3PXP")

	ct, err := aesEncrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	pt, err := aesDecrypt(key, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("roundtrip = %q, want %q", pt, plaintext)
	}
}

func TestAESDecrypt_WrongKeyFails(t *testing.T) {
	ct, err := aesEncrypt(deriveAESKey("key-a"), []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := aesDecrypt(deriveAESKey("key-b"), ct); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestDeriveAESKey_DistinctFromLegacy(t *testing.T) {
	current := deriveAESKey("passphrase")
	legacy := deriveLegacyAESKey("passphrase")
	if len(current) != 32 || len(legacy) != 32 {
		t.Fatalf("key lengths = %d, %d, want 32", len(current), len(legacy))
	}
	if bytes.Equal(current, legacy) {
		t.Error("HKDF-derived key must differ from legacy SHA-256 key")
	}
}

// TestDecryptSecret_CurrentAndLegacyKeys verifies that decryptSecret handles
// both current (HKDF) and legacy (raw SHA-256) ciphertexts, and rejects
// ciphertexts encrypted under an unrelated key. mfaRepo is nil, so the
// legacy-migration write is skipped (nil-guarded), keeping this a pure
// crypto test.
func TestDecryptSecret_CurrentAndLegacyKeys(t *testing.T) {
	svc := &MFAService{
		aesKey:       deriveAESKey("mfa-secret"),
		legacyAESKey: deriveLegacyAESKey("mfa-secret"),
	}
	ctx := context.Background()

	tests := []struct {
		name    string
		key     []byte
		wantErr bool
	}{
		{"current key", svc.aesKey, false},
		{"legacy key", svc.legacyAESKey, false},
		{"unrelated key", deriveAESKey("other-secret"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := aesEncrypt(tt.key, []byte("JBSWY3DPEHPK3PXP"))
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
			row := &database.UserMFARow{UserID: "user-1", TOTPSecret: enc}
			secret, err := svc.decryptSecret(ctx, row)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("decryptSecret: %v", err)
			}
			if secret != "JBSWY3DPEHPK3PXP" {
				t.Errorf("secret = %q, want %q", secret, "JBSWY3DPEHPK3PXP")
			}
		})
	}
}
