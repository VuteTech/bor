// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestParseFlatpakrepo(t *testing.T) {
	key := []byte{0x99, 0x02, 0x0d, 0x04, 0x59, 0x43, 0xda, 0xc0}
	data := "[Flatpak Repo]\nTitle=Flathub\nUrl=https://dl.flathub.org/repo/\nHomepage=https://flathub.org/\n" +
		"Comment=Central repository of Flatpak applications\nDescription=Central repository\n" +
		"Icon=https://dl.flathub.org/repo/logo.svg\nGPGKey=" + base64.StdEncoding.EncodeToString(key) + "\n" +
		"DefaultBranch=stable\nSubset=verified\nDeployCollectionID=org.flathub.Stable\nNoDeps=true\nPrio=2\n# comment\nUnknown=x\n"
	rf, err := ParseFlatpakrepo([]byte(data))
	if err != nil {
		t.Fatalf("ParseFlatpakrepo: %v", err)
	}
	if rf.Title != "Flathub" || rf.URL != "https://dl.flathub.org/repo/" || rf.Homepage != "https://flathub.org/" {
		t.Errorf("basic fields: %+v", rf)
	}
	if !bytes.Equal(rf.GPGKey, key) {
		t.Errorf("GPGKey = %x", rf.GPGKey)
	}
	if rf.DefaultBranch != "stable" || rf.Subset != "verified" || rf.CollectionID != "org.flathub.Stable" || !rf.NoDeps || rf.Prio != 2 {
		t.Errorf("extra fields: %+v", rf)
	}
}

func TestParseFlatpakrepo_Errors(t *testing.T) {
	if _, err := ParseFlatpakrepo([]byte("[Other]\nUrl=https://x/\n")); !errors.Is(err, ErrNotFlatpakrepo) {
		t.Errorf("expected ErrNotFlatpakrepo, got %v", err)
	}
	if _, err := ParseFlatpakrepo([]byte("[Flatpak Repo]\nTitle=x\n")); err == nil {
		t.Error("expected missing Url error")
	}
	if _, err := ParseFlatpakrepo([]byte("[Flatpak Repo]\nUrl=https://x/\nGPGKey=!!!notbase64!!!\n")); err == nil {
		t.Error("expected invalid GPGKey error")
	}
	// Unpadded base64 is tolerated.
	rf, err := ParseFlatpakrepo([]byte("[Flatpak Repo]\nUrl=https://x/\nGPGKey=mQIN\n"))
	if err != nil || len(rf.GPGKey) != 3 {
		t.Errorf("unpadded key: %v %v", rf, err)
	}
}
