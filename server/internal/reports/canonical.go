// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package reports

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/VuteTech/Bor/server/pkg/report"
)

// MaxBundleBytes bounds a decompressed bundle, so a corrupt row cannot make
// the server allocate without limit.
const MaxBundleBytes = 512 << 20

// Marshal renders the canonical bytes of a bundle: the struct field order
// of pkg/report, no HTML escaping, no trailing newline.
func Marshal(b *report.Bundle) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(b); err != nil {
		return nil, fmt.Errorf("reports: marshal bundle: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Hash is the SHA-256 of canonical bytes.
func Hash(canonical []byte) []byte {
	sum := sha256.Sum256(canonical)
	return sum[:]
}

// Gzip compresses canonical bytes for storage.
func Gzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, fmt.Errorf("reports: compress bundle: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("reports: compress bundle: %w", err)
	}
	return buf.Bytes(), nil
}

// Gunzip restores canonical bytes from storage.
func Gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("reports: decompress bundle: %w", err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, MaxBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reports: decompress bundle: %w", err)
	}
	if len(out) > MaxBundleBytes {
		return nil, fmt.Errorf("reports: bundle exceeds %d bytes", MaxBundleBytes)
	}
	return out, nil
}
