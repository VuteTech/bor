// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package escrow implements envelope encryption for LUKS recovery keys
// .
//
// Each escrowed key is encrypted with a random per-record 256-bit DEK
// (AES-256-GCM). The DEK is wrapped by a KEK that lives outside the
// database - a key file (BOR_ESCROW_KEK_FILE) or an HSM. The AAD binds a
// ciphertext to its volume, escrow id and KEK, so a row cannot be moved to
// another volume. Without a configured KEK the escrow service fails closed.
package escrow

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// AADPrefix versions the AAD layout. Changing the layout requires a new prefix.
const AADPrefix = "bor-escrow-v1"

// dekAADPrefix is the AAD used when wrapping the DEK itself with a file KEK.
const dekAADPrefix = "bor-escrow-dek-v1"

// ErrNoKEK is returned when no key-encryption key is configured.
var ErrNoKEK = errors.New("escrow: no KEK configured (set BOR_ESCROW_KEK_FILE)")

// ErrUnknownKEK is returned when a record was wrapped with a KEK this server
// no longer knows.
var ErrUnknownKEK = errors.New("escrow: record was wrapped with an unknown KEK id")

// KeyWrapper wraps and unwraps per-record DEKs with the KEK.
// Implementations: file KEK (this file) and PKCS#11 (pkcs11 build tag).
type KeyWrapper interface {
	// KEKID returns the identifier stored on records wrapped by Wrap.
	KEKID() string
	// Wrap encrypts a 32-byte DEK under the current KEK.
	Wrap(dek []byte) ([]byte, error)
	// Unwrap decrypts a DEK wrapped under kekID (the current KEK or a
	// configured previous one). Returns ErrUnknownKEK for unknown ids.
	Unwrap(kekID string, wrapped []byte) ([]byte, error)
}

// Service seals and opens recovery keys with envelope encryption.
type Service struct {
	wrapper KeyWrapper
}

// NewService creates an escrow service. wrapper may be nil, in which case
// every operation fails closed with ErrNoKEK.
func NewService(wrapper KeyWrapper) *Service {
	return &Service{wrapper: wrapper}
}

// Configured reports whether a KEK is available.
func (s *Service) Configured() bool {
	return s != nil && s.wrapper != nil
}

// KEKID returns the current KEK id, or "" when no KEK is configured.
func (s *Service) KEKID() string {
	if !s.Configured() {
		return ""
	}
	return s.wrapper.KEKID()
}

// AAD builds the additional authenticated data for one record:
// "bor-escrow-v1|<volume_id>|<luks_uuid>|<escrow_id>|<kek_id>".
func AAD(volumeID, luksUUID, escrowID, kekID string) []byte {
	return []byte(AADPrefix + "|" + volumeID + "|" + luksUUID + "|" + escrowID + "|" + kekID)
}

// Seal encrypts a recovery key. It returns the wrapped DEK, the ciphertext
// (nonce || ct || tag) and the KEK id to store alongside them.
func (s *Service) Seal(recoveryKey []byte, volumeID, luksUUID, escrowID string) (wrappedDEK, ciphertext []byte, kekID string, err error) {
	if !s.Configured() {
		return nil, nil, "", ErrNoKEK
	}
	kekID = s.wrapper.KEKID()

	dek := make([]byte, 32)
	if _, randErr := io.ReadFull(rand.Reader, dek); randErr != nil {
		return nil, nil, "", fmt.Errorf("escrow: generate DEK: %w", randErr)
	}
	defer zero(dek)

	ciphertext, err = gcmSeal(dek, recoveryKey, AAD(volumeID, luksUUID, escrowID, kekID))
	if err != nil {
		return nil, nil, "", err
	}

	wrappedDEK, err = s.wrapper.Wrap(dek)
	if err != nil {
		return nil, nil, "", fmt.Errorf("escrow: wrap DEK: %w", err)
	}
	return wrappedDEK, ciphertext, kekID, nil
}

// Open decrypts a sealed recovery key. The caller should zero the returned
// slice after use.
func (s *Service) Open(wrappedDEK, ciphertext []byte, kekID, volumeID, luksUUID, escrowID string) ([]byte, error) {
	if !s.Configured() {
		return nil, ErrNoKEK
	}
	dek, err := s.wrapper.Unwrap(kekID, wrappedDEK)
	if err != nil {
		return nil, err
	}
	defer zero(dek)
	key, err := gcmOpen(dek, ciphertext, AAD(volumeID, luksUUID, escrowID, kekID))
	if err != nil {
		return nil, fmt.Errorf("escrow: open recovery key: %w", err)
	}
	return key, nil
}

// Rewrap re-wraps a record's DEK under the current KEK, re-sealing the
// ciphertext because the AAD includes the KEK id. Only 32-byte DEKs are ever
// rewrapped, never recovery keys.
func (s *Service) Rewrap(wrappedDEK, ciphertext []byte, oldKEKID, volumeID, luksUUID, escrowID string) (newWrappedDEK, newCiphertext []byte, newKEKID string, err error) {
	key, err := s.Open(wrappedDEK, ciphertext, oldKEKID, volumeID, luksUUID, escrowID)
	if err != nil {
		return nil, nil, "", err
	}
	defer zero(key)
	return s.Seal(key, volumeID, luksUUID, escrowID)
}

// ─── File KEK ────────────────────────────────────────────────────────────

// fileWrapper wraps DEKs with AES-256-GCM under file-based KEKs.
type fileWrapper struct {
	currentID string
	current   []byte
	previous  map[string][]byte // kek_id -> key, readable during rotation
}

// FileKEKConfig configures NewFileWrapper.
type FileKEKConfig struct {
	// Path of the current KEK file: 32 random bytes, raw or base64.
	// The file must not be group- or world-readable.
	Path string
	// ID stored on records wrapped with the current KEK (default "file-1").
	ID string
	// PreviousFiles maps old KEK ids to their key files. Records wrapped
	// with them stay readable until the rewrap job has migrated them.
	PreviousFiles map[string]string
}

// NewFileWrapper loads the KEK file(s) and returns a KeyWrapper.
func NewFileWrapper(cfg FileKEKConfig) (KeyWrapper, error) {
	if cfg.Path == "" {
		return nil, ErrNoKEK
	}
	id := cfg.ID
	if id == "" {
		id = "file-1"
	}
	current, err := loadKEKFile(cfg.Path)
	if err != nil {
		return nil, err
	}
	prev := make(map[string][]byte, len(cfg.PreviousFiles))
	for prevID, path := range cfg.PreviousFiles {
		if prevID == id {
			return nil, fmt.Errorf("escrow: previous KEK id %q collides with the current KEK id", prevID)
		}
		k, err := loadKEKFile(path)
		if err != nil {
			return nil, fmt.Errorf("escrow: previous KEK %q: %w", prevID, err)
		}
		prev[prevID] = k
	}
	return &fileWrapper{currentID: id, current: current, previous: prev}, nil
}

// ParsePreviousKEKFiles parses BOR_ESCROW_KEK_PREVIOUS_FILES entries of the
// form "id=path", comma-separated.
func ParsePreviousKEKFiles(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		idx := strings.Index(e, "=")
		if idx < 1 || idx == len(e)-1 {
			return nil, fmt.Errorf("escrow: invalid previous KEK entry %q (expected id=path)", e)
		}
		id := strings.TrimSpace(e[:idx])
		path := strings.TrimSpace(e[idx+1:])
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("escrow: duplicate previous KEK id %q", id)
		}
		out[id] = path
	}
	return out, nil
}

// loadKEKFile reads a KEK file (32 raw bytes or base64 of 32 bytes) and
// refuses group- or world-readable files.
func loadKEKFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("escrow: KEK file: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("escrow: KEK file %s is group- or world-readable (mode %04o); chmod it to 0600", path, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is admin-controlled configuration
	if err != nil {
		return nil, fmt.Errorf("escrow: read KEK file: %w", err)
	}
	if len(data) == 32 {
		return data, nil
	}
	trimmed := strings.TrimSpace(string(data))
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := enc.DecodeString(trimmed); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("escrow: KEK file %s must hold exactly 32 random bytes, raw or base64 (got %d bytes)", path, len(data))
}

// KEKID implements KeyWrapper.
func (w *fileWrapper) KEKID() string { return w.currentID }

// Wrap implements KeyWrapper.
func (w *fileWrapper) Wrap(dek []byte) ([]byte, error) {
	return gcmSeal(w.current, dek, []byte(dekAADPrefix+"|"+w.currentID))
}

// Unwrap implements KeyWrapper.
func (w *fileWrapper) Unwrap(kekID string, wrapped []byte) ([]byte, error) {
	kek := w.current
	if kekID != w.currentID {
		var ok bool
		kek, ok = w.previous[kekID]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownKEK, kekID)
		}
	}
	dek, err := gcmOpen(kek, wrapped, []byte(dekAADPrefix+"|"+kekID))
	if err != nil {
		return nil, fmt.Errorf("escrow: unwrap DEK (kek %q): %w", kekID, err)
	}
	return dek, nil
}

// ─── AES-256-GCM helpers ─────────────────────────────────────────────────

// gcmSeal encrypts plaintext with AES-256-GCM and returns nonce || ct || tag.
func gcmSeal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("escrow: nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// gcmOpen decrypts nonce || ct || tag produced by gcmSeal.
func gcmOpen(key, data, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("escrow: aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("escrow: gcm: %w", err)
	}
	return gcm, nil
}

// zero overwrites a secret byte slice.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
