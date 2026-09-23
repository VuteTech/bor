// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package escrow

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeKEKFile(t *testing.T, dir, name string, raw bool) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	path := filepath.Join(dir, name)
	data := key
	if !raw {
		data = []byte(base64.StdEncoding.EncodeToString(key))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write KEK file: %v", err)
	}
	return path
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	path := writeKEKFile(t, t.TempDir(), "kek", true)
	w, err := NewFileWrapper(FileKEKConfig{Path: path, ID: "test-1"})
	if err != nil {
		t.Fatalf("NewFileWrapper: %v", err)
	}
	return NewService(w)
}

func TestSealOpenRoundTrip(t *testing.T) {
	svc := newTestService(t)
	key := []byte("cbdefghi-jklnrtuv-cbdefghi-jklnrtuv-cbdefghi-jklnrtuv-cbdefghi-jklnrtuv")

	wrapped, ct, kekID, err := svc.Seal(key, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if kekID != "test-1" {
		t.Fatalf("kekID = %q, want test-1", kekID)
	}
	if bytes.Contains(ct, key) {
		t.Fatal("ciphertext contains the plaintext key")
	}

	got, err := svc.Open(wrapped, ct, kekID, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatalf("Open returned %q, want %q", got, key)
	}
}

func TestOpenAADMismatchFails(t *testing.T) {
	svc := newTestService(t)
	key := []byte("secret-recovery-key")
	wrapped, ct, kekID, err := svc.Seal(key, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	cases := []struct {
		name                         string
		volumeID, luksUUID, escrowID string
	}{
		{"wrong volume", "vol-2", "uuid-1", "escrow-1"},
		{"wrong luks uuid", "vol-1", "uuid-2", "escrow-1"},
		{"wrong escrow id", "vol-1", "uuid-1", "escrow-2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Open(wrapped, ct, kekID, tc.volumeID, tc.luksUUID, tc.escrowID); err == nil {
				t.Fatal("Open succeeded with a mismatched AAD")
			}
		})
	}
}

func TestOpenTamperedCiphertextFails(t *testing.T) {
	svc := newTestService(t)
	wrapped, ct, kekID, err := svc.Seal([]byte("secret"), "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	ct[len(ct)-1] ^= 0x01
	if _, err := svc.Open(wrapped, ct, kekID, "vol-1", "uuid-1", "escrow-1"); err == nil {
		t.Fatal("Open succeeded with a tampered ciphertext")
	}
}

func TestFailClosedWithoutKEK(t *testing.T) {
	svc := NewService(nil)
	if svc.Configured() {
		t.Fatal("Configured() = true with a nil wrapper")
	}
	if _, _, _, err := svc.Seal([]byte("k"), "v", "u", "e"); !errors.Is(err, ErrNoKEK) {
		t.Fatalf("Seal error = %v, want ErrNoKEK", err)
	}
	if _, err := svc.Open(nil, nil, "id", "v", "u", "e"); !errors.Is(err, ErrNoKEK) {
		t.Fatalf("Open error = %v, want ErrNoKEK", err)
	}
}

func TestKEKRotationRewrap(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeKEKFile(t, dir, "kek-old", true)
	newPath := writeKEKFile(t, dir, "kek-new", false)

	oldWrapper, err := NewFileWrapper(FileKEKConfig{Path: oldPath, ID: "file-1"})
	if err != nil {
		t.Fatalf("old wrapper: %v", err)
	}
	oldSvc := NewService(oldWrapper)
	key := []byte("recovery-key-material")
	wrapped, ct, kekID, err := oldSvc.Seal(key, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// New server generation: new current KEK, old one configured as previous.
	newWrapper, err := NewFileWrapper(FileKEKConfig{
		Path: newPath, ID: "file-2",
		PreviousFiles: map[string]string{"file-1": oldPath},
	})
	if err != nil {
		t.Fatalf("new wrapper: %v", err)
	}
	newSvc := NewService(newWrapper)

	// Old records stay readable.
	got, err := newSvc.Open(wrapped, ct, kekID, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Open with previous KEK: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatal("previous-KEK Open returned wrong plaintext")
	}

	// Rewrap migrates to the current KEK.
	newWrapped, newCT, newKEKID, err := newSvc.Rewrap(wrapped, ct, kekID, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Rewrap: %v", err)
	}
	if newKEKID != "file-2" {
		t.Fatalf("Rewrap kekID = %q, want file-2", newKEKID)
	}
	got, err = newSvc.Open(newWrapped, newCT, newKEKID, "vol-1", "uuid-1", "escrow-1")
	if err != nil {
		t.Fatalf("Open after rewrap: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatal("post-rewrap Open returned wrong plaintext")
	}

	// A server without the previous KEK cannot read old records.
	soloWrapper, err := NewFileWrapper(FileKEKConfig{Path: newPath, ID: "file-2"})
	if err != nil {
		t.Fatalf("solo wrapper: %v", err)
	}
	if _, err := NewService(soloWrapper).Open(wrapped, ct, "file-1", "vol-1", "uuid-1", "escrow-1"); !errors.Is(err, ErrUnknownKEK) {
		t.Fatalf("Open with unknown KEK = %v, want ErrUnknownKEK", err)
	}
}

func TestLoadKEKFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := writeKEKFile(t, dir, "kek", true)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, err := NewFileWrapper(FileKEKConfig{Path: path}); err == nil {
		t.Fatal("NewFileWrapper accepted a world-readable KEK file")
	}
}

func TestLoadKEKFileBadLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kek")
	if err := os.WriteFile(path, []byte("too short"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewFileWrapper(FileKEKConfig{Path: path}); err == nil {
		t.Fatal("NewFileWrapper accepted a short KEK file")
	}
}

func TestParsePreviousKEKFiles(t *testing.T) {
	m, err := ParsePreviousKEKFiles([]string{"file-0=/etc/bor/kek0", " file-1 = /etc/bor/kek1 "})
	if err != nil {
		t.Fatalf("ParsePreviousKEKFiles: %v", err)
	}
	if m["file-0"] != "/etc/bor/kek0" || m["file-1"] != "/etc/bor/kek1" {
		t.Fatalf("unexpected map: %v", m)
	}
	if _, err := ParsePreviousKEKFiles([]string{"no-equals"}); err == nil {
		t.Fatal("accepted an entry without id=path")
	}
	if _, err := ParsePreviousKEKFiles([]string{"a=/x", "a=/y"}); err == nil {
		t.Fatal("accepted duplicate ids")
	}
}
