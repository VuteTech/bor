// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledLookAndFeelPackagesFrom(t *testing.T) {
	root := t.TempDir()
	mk := func(dir, file, content string) {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, dir, file), []byte(content))
	}
	mk("a/org.kde.breezedark.desktop", "metadata.json", `{"KPackageStructure": "Plasma/LookAndFeel", "KPlugin": {"Id": "org.kde.breezedark.desktop", "Name": "Breeze Dark"}}`)
	mk("a/org.example.old.desktop", "metadata.desktop", "[Desktop Entry]\nName=Old Theme\nX-KDE-PluginInfo-Name=org.example.old.desktop\n")
	mk("a/noname.desktop", "metadata.json", `{"KPlugin": {}}`)
	mk("b/org.kde.breezedark.desktop", "metadata.json", `{"KPlugin": {"Id": "org.kde.breezedark.desktop", "Name": "Duplicate from a later dir"}}`)
	writeFile(t, filepath.Join(root, "a", "stray-file"), []byte("not a package"))

	got := InstalledLookAndFeelPackagesFrom([]string{filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "missing")})
	want := map[string]string{"noname.desktop": "noname.desktop", "org.example.old.desktop": "Old Theme", "org.kde.breezedark.desktop": "Breeze Dark"}
	if len(got) != len(want) {
		t.Fatalf("got %d packages, want %d: %v", len(got), len(want), got)
	}
	for i, p := range got {
		if want[p.GetId()] != p.GetName() {
			t.Errorf("%s: name %q, want %q", p.GetId(), p.GetName(), want[p.GetId()])
		}
		if i > 0 && got[i-1].GetId() > p.GetId() {
			t.Errorf("packages not sorted: %s after %s", p.GetId(), got[i-1].GetId())
		}
	}
}
