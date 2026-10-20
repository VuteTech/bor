// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package filewatcher

import (
	"testing"
	"time"
)

func TestSuppress_ExtendsButNeverShortens(t *testing.T) {
	fw := &FileWatcher{suppress: map[string]time.Time{}, managed: map[string]bool{}}
	p := "/etc/bor/managed.conf"

	fw.Suppress([]string{p, ""}, 2*time.Minute)
	long := fw.suppress[p]
	if long.Before(time.Now().Add(100 * time.Second)) {
		t.Fatalf("long window not recorded: %v", long)
	}

	// Another policy type's sync asks for the usual short window: the
	// long one must survive.
	fw.Suppress([]string{p}, 2*time.Second)
	if !fw.suppress[p].Equal(long) {
		t.Errorf("short Suppress shortened the window: %v -> %v", long, fw.suppress[p])
	}

	// A longer request extends it.
	fw.Suppress([]string{p}, 5*time.Minute)
	if !fw.suppress[p].After(long) {
		t.Errorf("longer Suppress did not extend the window")
	}

	// The explicit reset shortens it to the requested window.
	fw.SuppressReset([]string{p}, 2*time.Second)
	if fw.suppress[p].After(time.Now().Add(3 * time.Second)) {
		t.Errorf("SuppressReset did not shorten the window: %v", fw.suppress[p])
	}
	if _, ok := fw.suppress[""]; ok {
		t.Error("empty path must be ignored")
	}
}
