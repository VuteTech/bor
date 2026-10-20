// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package apparmor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Markers of the block Bor owns inside a site-local file. Everything
// outside the block belongs to the local administrator.
const (
	BlockBegin = "# BEGIN Bor managed - changes inside this block are overwritten"
	BlockEnd   = "# END Bor managed"
)

// splitBlock returns the text before, inside and after the managed block;
// found is false when the file has no block.
func splitBlock(content string) (before, inside, after string, found bool) {
	start := strings.Index(content, BlockBegin)
	if start < 0 {
		return content, "", "", false
	}
	rest := content[start+len(BlockBegin):]
	end := strings.Index(rest, BlockEnd)
	if end < 0 {
		return content[:start], strings.TrimPrefix(rest, "\n"), "", true
	}
	inside = strings.TrimPrefix(rest[:end], "\n")
	after = rest[end+len(BlockEnd):]
	after = strings.TrimPrefix(after, "\n")
	return content[:start], inside, after, true
}

// ReadManagedBlock returns the text inside the managed block of a file.
func ReadManagedBlock(path string) (text string, found bool) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path under the profile directory
	if err != nil {
		return "", false
	}
	_, inside, _, found := splitBlock(string(data))
	return inside, found
}

// WriteManagedBlock sets the managed block of a file to text, creating
// the file when it does not exist and keeping everything outside the
// block. It reports whether the file changed and whether it was created.
func WriteManagedBlock(path, text string) (changed, created bool, err error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path under the profile directory
	switch {
	case errors.Is(err, os.ErrNotExist):
		created = true
		data = nil
	case err != nil:
		return false, false, err
	}
	before, _, after, _ := splitBlock(string(data))
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if before != "" && !strings.HasSuffix(before, "\n") {
		before += "\n"
	}
	next := before + BlockBegin + "\n" + text + BlockEnd + "\n" + after
	if string(data) == next {
		return false, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, false, err
	}
	return true, created, writeAtomic(path, []byte(next))
}

// RemoveManagedBlock removes the managed block; when the rest of the file
// is empty and Bor created it, the file is deleted. It reports whether
// anything changed.
func RemoveManagedBlock(path string, createdByBor bool) (bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path under the profile directory
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	before, _, after, found := splitBlock(string(data))
	if !found {
		return false, nil
	}
	rest := before + after
	if strings.TrimSpace(rest) == "" && createdByBor {
		return true, os.Remove(path)
	}
	return true, writeAtomic(path, []byte(rest))
}

// writeAtomic writes through a temporary file next to the target. The
// temporary name starts with a dot so the parser never picks it up.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".bor-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// WriteFile writes a whole file atomically (profiles and tunables Bor
// owns).
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return writeAtomic(path, data)
}
