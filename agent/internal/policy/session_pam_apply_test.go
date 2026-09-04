// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTimeConfAt_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "time.conf")
	if err := os.WriteFile(path, []byte("# admin\ngames;*;!waster;Wk1800-0800\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeTimeConfAt(path, []string{"login;*;alice;Mo0800-1700"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "games;*;!waster;Wk1800-0800") {
		t.Error("admin content lost")
	}
	if !strings.Contains(string(got), "login;*;alice;Mo0800-1700") {
		t.Error("managed line missing")
	}

	// Mode preserved.
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", fi.Mode().Perm())
	}

	// Removing strips the block, keeps admin content.
	if err := writeTimeConfAt(path, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if HasManagedBlock(string(got)) {
		t.Error("managed block should be gone")
	}
	if !strings.Contains(string(got), "games;*;!waster;Wk1800-0800") {
		t.Error("admin content lost on removal")
	}
}

func TestWriteTimeConfAt_RejectsUnsafeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "time.conf")

	// A line targeting root must be refused outright.
	if err := writeTimeConfAt(path, []string{"login;*;root;Mo0800-1700"}); err == nil {
		t.Fatal("expected refusal of a root-targeting line")
	}
	// A wildcard user field must be refused.
	if err := writeTimeConfAt(path, []string{"login;*;*;Mo0800-1700"}); err == nil {
		t.Fatal("expected refusal of a wildcard user line")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no file should have been written on refusal")
	}
}

func TestWriteTimeConfAt_CreatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "time.conf")
	if err := writeTimeConfAt(path, []string{"login;*;alice;Mo0800-1700"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "alice") {
		t.Error("file not created with managed line")
	}
}
