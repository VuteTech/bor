// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCrypttabOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crypttab")
	original := `# /etc/crypttab - created by the installer
luks-3f2a9c1e UUID=3f2a9c1e-1111-2222-3333-444455556666 none discard
swap /dev/sda3 /dev/urandom swap,cipher=aes-xts-plain64

other-volume UUID=abc none noauto
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	changed, err := EnsureCrypttabOptions(path, "luks-3f2a9c1e", []string{"tpm2-device=auto"})
	if err != nil {
		t.Fatalf("EnsureCrypttabOptions: %v", err)
	}
	if !changed {
		t.Fatal("expected a change")
	}
	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "discard,tpm2-device=auto") {
		t.Errorf("option token not appended:\n%s", content)
	}
	// Every other byte preserved: comments, other lines, blank lines.
	for _, keep := range []string{
		"# /etc/crypttab - created by the installer",
		"swap /dev/sda3 /dev/urandom swap,cipher=aes-xts-plain64",
		"other-volume UUID=abc none noauto",
	} {
		if !strings.Contains(content, keep) {
			t.Errorf("line lost: %q", keep)
		}
	}

	// Idempotent: a second run changes nothing.
	changed, err = EnsureCrypttabOptions(path, "luks-3f2a9c1e", []string{"tpm2-device=auto"})
	if err != nil {
		t.Fatalf("second EnsureCrypttabOptions: %v", err)
	}
	if changed {
		t.Error("second run must be a no-op")
	}
	data2, _ := os.ReadFile(path)
	if string(data2) != content {
		t.Error("idempotent run modified the file")
	}
}

func TestEnsureCrypttabOptionsNoKeyfileColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crypttab")
	if err := os.WriteFile(path, []byte("root UUID=abc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	changed, err := EnsureCrypttabOptions(path, "root", []string{"tpm2-device=auto"})
	if err != nil {
		t.Fatalf("EnsureCrypttabOptions: %v", err)
	}
	if !changed {
		t.Fatal("expected a change")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "root UUID=abc none tpm2-device=auto") {
		t.Errorf("missing keyfile column not padded:\n%s", string(data))
	}
}

func TestDracutDropInContent(t *testing.T) {
	c := DracutDropInContent(true, true)
	if !strings.Contains(c, `add_dracutmodules+=" tpm2-tss "`) {
		t.Error("tpm2-tss module missing")
	}
	if !strings.Contains(c, `hostonly_cmdline="yes"`) {
		t.Error("hostonly_cmdline missing")
	}
	if !strings.Contains(c, "Managed by Bor") {
		t.Error("management marker missing")
	}
	if c2 := DracutDropInContent(true, false); strings.Contains(c2, "hostonly_cmdline") {
		t.Error("hostonly_cmdline must only appear when Tang protects root")
	}
}
