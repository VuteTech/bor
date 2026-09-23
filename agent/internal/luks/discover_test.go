// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"os"
	"path/filepath"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// buildFakeTree creates a synthetic /proc + /sys tree:
//
//	/          ext4 on dm-1 (LVM LV) -> slaves dm-0 (CRYPT-LUKS2) -> sda3
//	/home      ext4 on dm-2 (CRYPT-LUKS2 directly) -> sdb1 (removable)
//	/boot      ext4 on sda1 (unencrypted)
//	swap       dm-3 (CRYPT-PLAIN, random key) -> sda2
func buildFakeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}

	write("proc/self/mountinfo", `1 0 253:1 / / rw - ext4 /dev/mapper/vg-root rw
2 0 253:2 / /home rw - ext4 /dev/mapper/luks-aaaabbbb rw
3 0 8:1 / /boot rw - ext4 /dev/sda1 rw
4 0 0:99 / /proc rw - proc proc rw
`)
	write("proc/swaps", "Filename\tType\tSize\tUsed\tPriority\n/dev/dm-3 partition 8388604 0 -2\n")

	// dm-0: LUKS2 crypt under the LVM root.
	write("sys/class/block/dm-0/dm/uuid", "CRYPT-LUKS2-3f2a9c1e111122223333444455556666-luks-3f2a9c1e\n")
	mkdir("sys/class/block/dm-0/slaves/sda3")
	// dm-1: the LVM LV backing / - slave is dm-0.
	write("sys/class/block/dm-1/dm/uuid", "LVM-abcdef\n")
	mkdir("sys/class/block/dm-1/slaves/dm-0")
	// dm-2: LUKS2 crypt backing /home directly, on removable sdb1.
	write("sys/class/block/dm-2/dm/uuid", "CRYPT-LUKS2-aaaabbbbccccddddeeeeffff00001111-luks-aaaabbbb\n")
	mkdir("sys/class/block/dm-2/slaves/sdb1")
	// dm-3: plain crypt swap.
	write("sys/class/block/dm-3/dm/uuid", "CRYPT-PLAIN-swap\n")
	mkdir("sys/class/block/dm-3/slaves/sda2")

	// /sys/dev/block major:minor -> class nodes (symlinks).
	mkdir("sys/dev/block")
	link := func(mm, name string) {
		t.Helper()
		if err := os.Symlink(filepath.Join(root, "sys", "class", "block", name), filepath.Join(root, "sys", "dev", "block", mm)); err != nil {
			t.Fatalf("symlink %s: %v", mm, err)
		}
	}
	link("253:1", "dm-1")
	link("253:2", "dm-2")
	link("8:1", "sda1")

	// /dev/dm-3 for the swap walk.
	write("dev/dm-3", "")

	// Removable flags: sdb1's parent disk sdb is removable.
	write("sys/class/block/sda3/removable", "0\n")
	write("sys/class/block/sdb1/removable", "1\n")
	write("sys/class/block/sda2/removable", "0\n")
	write("sys/class/block/sda1/removable", "0\n")

	return root
}

func TestDiscover(t *testing.T) {
	oldRoot := fsRoot
	fsRoot = buildFakeTree(t)
	defer func() { fsRoot = oldRoot }()

	volumes, mounts, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	byUUID := map[string]*Volume{}
	var plain *Volume
	for _, v := range volumes {
		if v.IsPlain {
			plain = v
			continue
		}
		byUUID[v.LuksUUID] = v
	}

	rootVol := byUUID["3f2a9c1e-1111-2222-3333-444455556666"]
	if rootVol == nil {
		t.Fatalf("root volume not discovered (LVM-on-LUKS walk); volumes: %+v", volumes)
	}
	if !rootVol.IsSystem {
		t.Error("root volume must be a system volume")
	}
	if rootVol.MappingName != "luks-3f2a9c1e" {
		t.Errorf("root mapping name = %q", rootVol.MappingName)
	}
	if rootVol.DevicePath != "/dev/sda3" {
		t.Errorf("root device = %q, want /dev/sda3", rootVol.DevicePath)
	}
	if rootVol.Removable {
		t.Error("root volume must not be removable")
	}

	homeVol := byUUID["aaaabbbb-cccc-dddd-eeee-ffff00001111"]
	if homeVol == nil {
		t.Fatal("home volume not discovered")
	}
	if !homeVol.Removable {
		t.Error("home volume is on a removable disk")
	}
	if !homeVol.IsSystem {
		t.Error("/home is a system mountpoint")
	}

	if plain == nil {
		t.Fatal("plain swap volume not discovered")
	}
	if !plain.IsSystem || plain.Mountpoints[0] != "[swap]" {
		t.Errorf("plain swap = %+v", plain)
	}

	// /boot is unencrypted and must appear as a mount without a volume.
	var boot *Mount
	for _, m := range mounts {
		if m.Mountpoint == "/boot" {
			boot = m
		}
		if m.Mountpoint == "/proc" {
			t.Error("virtual mounts must be filtered out")
		}
	}
	if boot == nil || boot.Volume != nil {
		t.Errorf("/boot mount = %+v, want unencrypted", boot)
	}
}

func TestScopeVolumes(t *testing.T) {
	vols := []*Volume{
		{LuksUUID: "a", IsSystem: true, Mountpoints: []string{"/"}},
		{LuksUUID: "b", IsSystem: false, Mountpoints: []string{"/data"}},
		{LuksUUID: "c", IsSystem: false, Mountpoints: []string{"/media/usb"}, Removable: true},
	}
	if got := ScopeVolumes(vols, pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_SYSTEM, nil); len(got) != 1 || got[0].LuksUUID != "a" {
		t.Errorf("SYSTEM scope = %v", got)
	}
	if got := ScopeVolumes(vols, pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_MOUNTPOINTS, []string{"/data"}); len(got) != 1 || got[0].LuksUUID != "b" {
		t.Errorf("MOUNTPOINTS scope = %v", got)
	}
	if got := ScopeVolumes(vols, pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_ALL_FIXED, nil); len(got) != 2 {
		t.Errorf("ALL_FIXED scope = %v (removable must be excluded)", got)
	}
}

func TestCheckStaticUnencrypted(t *testing.T) {
	inv := &Inventory{
		Platform: &Platform{TPM2Present: true, SecureBoot: pb.SecureBootState_SECURE_BOOT_STATE_ENABLED},
		Mounts: []*Mount{
			{Mountpoint: "/", Source: "/dev/sda2", Fstype: "ext4"},
			{Mountpoint: "/data", Source: "/dev/sdb1", Fstype: "xfs"},
		},
	}
	pol := &pb.DiskEncryptionPolicy{RequireEncryption: true, RequireTpm2: true, RequireSecureBoot: true}
	items := CheckStatic(inv, pol)

	var encItems, nonCompliant int
	for _, it := range items {
		if it.SchemaID == "luks:encryption" {
			encItems++
			if it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
				nonCompliant++
			}
		}
	}
	// SYSTEM scope: only / is in scope, /data is not.
	if encItems != 1 || nonCompliant != 1 {
		t.Errorf("encryption items = %d (non-compliant %d), want 1/1", encItems, nonCompliant)
	}
	status, _ := Rollup(items)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("rollup = %v", status)
	}
}

func TestRollup(t *testing.T) {
	ok := Item{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT}
	bad := Item{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "x"}
	errItem := Item{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "y"}
	inapp := Item{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE}

	if s, _ := Rollup([]Item{ok, ok}); s != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("all ok = %v", s)
	}
	if s, _ := Rollup([]Item{ok, bad}); s != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("one bad = %v", s)
	}
	if s, _ := Rollup([]Item{ok, bad, errItem}); s != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Errorf("error wins = %v", s)
	}
	if s, _ := Rollup([]Item{inapp, inapp}); s != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("all inapplicable = %v", s)
	}
	if s, _ := Rollup(nil); s != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("empty = %v", s)
	}
}
