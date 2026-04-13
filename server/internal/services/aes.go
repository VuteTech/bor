// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package services contains the business logic for the Bor server.
package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// hkdfSalt is a fixed, application-specific salt for HKDF key derivation.
// Changing this value would invalidate all existing encrypted data.
var hkdfSalt = []byte("bor-mfa-aes-key-v1") //nolint:gochecknoglobals // fixed cryptographic parameter

// deriveAESKey derives a 256-bit AES key from an arbitrary-length passphrase
// using HKDF-SHA256 (FIPS 140-3 / BSI TR-02102 compliant).
func deriveAESKey(passphrase string) []byte {
	r := hkdf.New(sha256.New, []byte(passphrase), hkdfSalt, []byte("mfa-secret-encryption"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		panic("hkdf: " + err.Error()) // only fails if output exceeds 255*HashLen
	}
	return key
}

// deriveLegacyAESKey derives a key using the old SHA-256 method (pre-HKDF).
// Used only for migrating existing encrypted TOTP secrets.
func deriveLegacyAESKey(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

// aesEncrypt encrypts plaintext with AES-256-GCM using a random nonce.
// Returns base64(nonce || ciphertext || tag).
func aesEncrypt(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("aes gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// aesDecrypt decrypts a base64-encoded AES-256-GCM ciphertext produced by aesEncrypt.
func aesDecrypt(key []byte, encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}
