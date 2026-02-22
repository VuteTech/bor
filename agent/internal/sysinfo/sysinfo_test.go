// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyOS_Fedora(t *testing.T) {
	f := map[string]string{"ID": "fedora", "VERSION_ID": "40"}
	info := classifyOS(f)
	if info.Name != "Fedora" {
		t.Errorf("got %q, want %q", info.Name, "Fedora")
	}
	if info.Version != "40" {
		t.Errorf("version: got %q, want %q", info.Version, "40")
	}
}

func TestClassifyOS_FedoraKinoite(t *testing.T) {
	f := map[string]string{"ID": "fedora", "VERSION_ID": "40", "VARIANT_ID": "kinoite"}
	info := classifyOS(f)
	if info.Name != "Fedora Atomic" {
		t.Errorf("got %q, want %q", info.Name, "Fedora Atomic")
	}
}

func TestClassifyOS_FedoraSilverblue(t *testing.T) {
	f := map[string]string{"ID": "fedora", "VERSION_ID": "40", "VARIANT_ID": "silverblue"}
	info := classifyOS(f)
	if info.Name != "Fedora Atomic" {
		t.Errorf("got %q, want %q", info.Name, "Fedora Atomic")
	}
}

func TestClassifyOS_Ubuntu(t *testing.T) {
	f := map[string]string{"ID": "ubuntu", "VERSION_ID": "22.04", "PRETTY_NAME": "Ubuntu 22.04.3 LTS"}
	info := classifyOS(f)
	if info.Name != "Ubuntu" {
		t.Errorf("got %q, want %q", info.Name, "Ubuntu")
	}
	if info.Version != "22.04" {
		t.Errorf("version: got %q, want %q", info.Version, "22.04")
	}
}

func TestClassifyOS_OpenSUSELeap(t *testing.T) {
	f := map[string]string{"ID": "opensuse-leap", "VERSION_ID": "15.6"}
	info := classifyOS(f)
	if info.Name != "openSUSE Leap" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE Leap")
	}
}

func TestClassifyOS_OpenSUSETumbleweed(t *testing.T) {
	f := map[string]string{"ID": "opensuse-tumbleweed", "VERSION_ID": "20240601"}
	info := classifyOS(f)
	if info.Name != "openSUSE Tumbleweed" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE Tumbleweed")
	}
}

func TestClassifyOS_SLES(t *testing.T) {
	f := map[string]string{"ID": "sles", "VERSION_ID": "15.6"}
	info := classifyOS(f)
	if info.Name != "SLES" {
		t.Errorf("got %q, want %q", info.Name, "SLES")
	}
}

func TestClassifyOS_OpenSUSEAeon(t *testing.T) {
	f := map[string]string{"ID": "opensuse-aeon", "VERSION_ID": ""}
	info := classifyOS(f)
	if info.Name != "openSUSE Aeon" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE Aeon")
	}
}

func TestClassifyOS_OpenSUSEKalpa(t *testing.T) {
	f := map[string]string{"ID": "opensuse-kalpa", "VERSION_ID": ""}
	info := classifyOS(f)
	if info.Name != "openSUSE Kalpa" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE Kalpa")
	}
}

func TestClassifyOS_MicroOSAeonVariant(t *testing.T) {
	f := map[string]string{"ID": "opensuse-microos", "VARIANT_ID": "Aeon"}
	info := classifyOS(f)
	if info.Name != "openSUSE Aeon" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE Aeon")
	}
}

func TestClassifyOS_MicroOSKalpaVariant(t *testing.T) {
	f := map[string]string{"ID": "opensuse-microos", "VARIANT_ID": "Kalpa"}
	info := classifyOS(f)
	if info.Name != "openSUSE Kalpa" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE Kalpa")
	}
}

func TestClassifyOS_MicroOS(t *testing.T) {
	f := map[string]string{"ID": "opensuse-microos", "VARIANT_ID": "MicroOS"}
	info := classifyOS(f)
	if info.Name != "openSUSE MicroOS" {
		t.Errorf("got %q, want %q", info.Name, "openSUSE MicroOS")
	}
}

func TestClassifyOS_Unknown(t *testing.T) {
	f := map[string]string{"ID": "gentoo", "VERSION_ID": "2.14", "PRETTY_NAME": "Gentoo Linux"}
	info := classifyOS(f)
	if info.Name != "Gentoo Linux" {
		t.Errorf("got %q, want %q", info.Name, "Gentoo Linux")
	}
}

func TestClassifyOS_UnknownNoPrettyName(t *testing.T) {
	f := map[string]string{"ID": "someos"}
	info := classifyOS(f)
	if info.Name != "someos" {
		t.Errorf("got %q, want %q", info.Name, "someos")
	}
}

func TestParseOSRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "os-release")
	content := `NAME="Fedora Linux"
VERSION="40 (Workstation Edition)"
ID=fedora
VERSION_ID=40
PRETTY_NAME="Fedora Linux 40 (Workstation Edition)"
VARIANT_ID=workstation
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	fields, err := parseOSRelease(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct{ key, want string }{
		{"ID", "fedora"},
		{"VERSION_ID", "40"},
		{"VARIANT_ID", "workstation"},
		{"PRETTY_NAME", "Fedora Linux 40 (Workstation Edition)"},
	}
	for _, tt := range tests {
		if got := fields[tt.key]; got != tt.want {
			t.Errorf("fields[%q] = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestDesktopInfoString(t *testing.T) {
	tests := []struct {
		de   DesktopInfo
		want string
	}{
		{DesktopInfo{"KDE Plasma", "6.1.4"}, "KDE Plasma 6.1.4"},
		{DesktopInfo{"GNOME", "46.1"}, "GNOME 46.1"},
		{DesktopInfo{"KDE Plasma", ""}, "KDE Plasma"},
	}
	for _, tt := range tests {
		if got := tt.de.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestCollectMachineID_Missing(t *testing.T) {
	// Should not panic when file doesn't exist
	id := collectMachineID()
	_ = id // may be empty or a real ID depending on test environment
}

func TestCollectIPAddress_NoPanic(t *testing.T) {
	// Should not panic even if dial fails
	ip := collectIPAddress()
	_ = ip
}
