// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Volume is one discovered dm-crypt volume.
type Volume struct {
	// LuksUUID is the LUKS container UUID (empty for plain dm-crypt swap).
	LuksUUID string
	// MappingName is the dm mapping name (e.g. "luks-3f2a…").
	MappingName string
	// DevicePath addresses the LUKS container for cryptsetup operations.
	DevicePath string
	// Mountpoints backed by this volume ("[swap]" for swap).
	Mountpoints []string
	// IsSystem: backs /, /usr, /var, /home or active swap.
	IsSystem bool
	// IsPlain: plain dm-crypt (CRYPT-PLAIN-…, e.g. random-key swap).
	IsPlain bool
	// Removable: the underlying disk is removable (excluded from ALL_FIXED).
	Removable bool
}

// Mount is one mounted filesystem that is in scope for require_encryption.
type Mount struct {
	Mountpoint string
	Source     string
	Fstype     string
	MajorMinor string
	// Volume is the backing dm-crypt volume, nil when unencrypted.
	Volume *Volume
}

// systemMountpoints are the mounts the SYSTEM volume scope covers.
var systemMountpoints = map[string]bool{
	"/": true, "/usr": true, "/var": true, "/home": true,
}

// virtualFstypes are never candidates for encryption checks.
var virtualFstypes = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true,
	"cgroup": true, "cgroup2": true, "securityfs": true, "debugfs": true,
	"tracefs": true, "pstore": true, "efivarfs": true, "bpf": true, "mqueue": true,
	"hugetlbfs": true, "autofs": true, "configfs": true, "fusectl": true,
	"ramfs": true, "binfmt_misc": true, "squashfs": true, "overlay": true,
	"nsfs": true, "rpc_pipefs": true, "selinuxfs": true, "fuse.portal": true,
	"fuse.gvfsd-fuse": true, "nfs": true, "nfs4": true, "cifs": true, "9p": true,
	"virtiofs": true, "vfat": true, // the ESP is unencrypted by design
}

// Discover walks mountinfo, /proc/swaps and sysfs to map every relevant
// mount to its dm-crypt container. Pure Go: no cryptsetup calls.
func Discover() (volumes []*Volume, mounts []*Mount, err error) {
	byUUID := map[string]*Volume{}
	addVolume := func(v *Volume) *Volume {
		key := v.LuksUUID
		if key == "" {
			key = "plain:" + v.MappingName
		}
		if existing, ok := byUUID[key]; ok {
			return existing
		}
		byUUID[key] = v
		volumes = append(volumes, v)
		return v
	}

	// ── Mounted filesystems ─────────────────────────────────────────────
	mountRows, err := parseMountinfo(rooted("proc/self/mountinfo"))
	if err != nil {
		return nil, nil, err
	}
	seenMounts := map[string]bool{}
	for _, m := range mountRows {
		if virtualFstypes[m.Fstype] || strings.HasPrefix(m.Mountpoint, "/proc") ||
			strings.HasPrefix(m.Mountpoint, "/sys") || strings.HasPrefix(m.Mountpoint, "/dev") ||
			strings.HasPrefix(m.Mountpoint, "/run") {
			continue
		}
		if seenMounts[m.Mountpoint] {
			continue
		}
		seenMounts[m.Mountpoint] = true

		mount := &Mount{Mountpoint: m.Mountpoint, Source: m.Source, Fstype: m.Fstype, MajorMinor: m.MajorMinor}
		if crypt := findCryptBacking(sysDevBlockPath(m.MajorMinor), 0); crypt != nil {
			vol := addVolume(crypt)
			vol.Mountpoints = append(vol.Mountpoints, m.Mountpoint)
			if systemMountpoints[m.Mountpoint] {
				vol.IsSystem = true
			}
			mount.Volume = vol
		}
		mounts = append(mounts, mount)
	}

	// ── Active swap ─────────────────────────────────────────────────────
	for _, dev := range parseSwaps(rooted("proc/swaps")) {
		name := resolveBlockName(dev)
		if name == "" {
			continue
		}
		mount := &Mount{Mountpoint: "[swap]", Source: dev, Fstype: "swap"}
		if crypt := findCryptBacking(rooted("sys/class/block/"+name), 0); crypt != nil {
			vol := addVolume(crypt)
			vol.Mountpoints = append(vol.Mountpoints, "[swap]")
			vol.IsSystem = true
			mount.Volume = vol
		}
		mounts = append(mounts, mount)
	}

	for _, v := range volumes {
		sort.Strings(v.Mountpoints)
		v.Mountpoints = dedupeStrings(v.Mountpoints)
	}
	return volumes, mounts, nil
}

// DiscoverAllFixed additionally enumerates every active LUKS mapping on a
// non-removable disk (the ALL_FIXED volume scope), whether mounted or not.
func DiscoverAllFixed() ([]*Volume, error) {
	entries, err := os.ReadDir(rooted("sys/class/block"))
	if err != nil {
		return nil, fmt.Errorf("luks: list block devices: %w", err)
	}
	var out []*Volume
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "dm-") {
			continue
		}
		v := cryptVolumeFromNode(rooted("sys/class/block/" + e.Name()))
		if v == nil || v.IsPlain || v.Removable {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// mountinfoRow is one parsed mountinfo line.
type mountinfoRow struct {
	MajorMinor string
	Mountpoint string
	Fstype     string
	Source     string
}

// parseMountinfo parses /proc/self/mountinfo.
func parseMountinfo(path string) ([]mountinfoRow, error) {
	data, err := os.ReadFile(path) //nolint:gosec // fixed procfs path (test-rooted)
	if err != nil {
		return nil, fmt.Errorf("luks: read mountinfo: %w", err)
	}
	var rows []mountinfoRow
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		// [0]=id [1]=parent [2]=maj:min [3]=root [4]=mountpoint [5]=options
		// [6..]=optional fields until "-", then fstype, source, super opts.
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) {
			continue
		}
		rows = append(rows, mountinfoRow{
			MajorMinor: fields[2],
			Mountpoint: unescapeMountPath(fields[4]),
			Fstype:     fields[sep+1],
			Source:     unescapeMountPath(fields[sep+2]),
		})
	}
	return rows, nil
}

// parseSwaps returns the device paths of active block-device swap.
func parseSwaps(path string) []string {
	data, err := os.ReadFile(path) //nolint:gosec // fixed procfs path (test-rooted)
	if err != nil {
		return nil
	}
	var devs []string
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 { // header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "partition" {
			continue
		}
		devs = append(devs, unescapeMountPath(fields[0]))
	}
	return devs
}

// sysDevBlockPath returns the sysfs node for a major:minor.
func sysDevBlockPath(majorMinor string) string {
	return rooted("sys/dev/block/" + majorMinor)
}

// resolveBlockName maps a /dev path to its kernel block name ("dm-3").
func resolveBlockName(dev string) string {
	resolved, err := filepath.EvalSymlinks(rooted(strings.TrimPrefix(dev, "/")))
	if err != nil {
		resolved = dev
	}
	return filepath.Base(resolved)
}

// findCryptBacking walks a block node's slaves down through LVM/MD layers to
// the first CRYPT-LUKS2 (or CRYPT-PLAIN) dm device.
func findCryptBacking(node string, depth int) *Volume {
	if depth > 8 {
		return nil
	}
	if v := cryptVolumeFromNode(node); v != nil {
		return v
	}
	slaves, err := os.ReadDir(filepath.Join(node, "slaves"))
	if err != nil {
		return nil
	}
	for _, s := range slaves {
		if v := findCryptBacking(rooted("sys/class/block/"+s.Name()), depth+1); v != nil {
			return v
		}
	}
	return nil
}

// cryptVolumeFromNode parses a dm node's dm/uuid into a Volume, or nil when
// the node is not a dm-crypt device.
func cryptVolumeFromNode(node string) *Volume {
	data, err := os.ReadFile(filepath.Join(node, "dm", "uuid")) //nolint:gosec // sysfs path derived from fixed roots
	if err != nil {
		return nil
	}
	dmUUID := strings.TrimSpace(string(data))
	v := &Volume{}
	switch {
	case strings.HasPrefix(dmUUID, "CRYPT-LUKS2-"), strings.HasPrefix(dmUUID, "CRYPT-LUKS1-"):
		rest := strings.TrimPrefix(strings.TrimPrefix(dmUUID, "CRYPT-LUKS2-"), "CRYPT-LUKS1-")
		if len(rest) < 33 {
			return nil
		}
		v.LuksUUID = reinsertUUIDDashes(rest[:32])
		v.MappingName = rest[33:]
	case strings.HasPrefix(dmUUID, "CRYPT-PLAIN-"):
		v.IsPlain = true
		v.MappingName = strings.TrimPrefix(dmUUID, "CRYPT-PLAIN-")
	default:
		return nil
	}

	// The crypt device's (single) slave is the LUKS container.
	if slaves, err := os.ReadDir(filepath.Join(node, "slaves")); err == nil && len(slaves) > 0 {
		name := slaves[0].Name()
		v.DevicePath = "/dev/" + name
		v.Removable = diskRemovable(name)
	} else if v.LuksUUID != "" {
		v.DevicePath = "/dev/disk/by-uuid/" + v.LuksUUID
	}
	return v
}

// diskRemovable reports whether the disk holding a block device is removable
// .
func diskRemovable(name string) bool {
	// Whole disk: the node has its own removable flag.
	if data, err := os.ReadFile(rooted("sys/class/block/" + name + "/removable")); err == nil {
		return strings.TrimSpace(string(data)) == "1"
	}
	// Partition: the parent directory of the resolved sysfs node is the disk.
	resolved, err := filepath.EvalSymlinks(rooted("sys/class/block/" + name))
	if err != nil {
		return false
	}
	parent := filepath.Base(filepath.Dir(resolved))
	if data, err := os.ReadFile(rooted("sys/class/block/" + parent + "/removable")); err == nil {
		return strings.TrimSpace(string(data)) == "1"
	}
	return false
}

// reinsertUUIDDashes converts a 32-hex dm uuid segment to canonical form.
func reinsertUUIDDashes(s string) string {
	if len(s) != 32 {
		return s
	}
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// unescapeMountPath decodes the octal escapes procfs uses in paths.
func unescapeMountPath(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(s)
}

func dedupeStrings(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}

// ScopeVolumes filters discovered volumes to the policy's volume scope.
func ScopeVolumes(volumes []*Volume, scope pb.LuksVolumeScope, mountpoints []string) []*Volume {
	switch scope {
	case pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_MOUNTPOINTS:
		want := map[string]bool{}
		for _, m := range mountpoints {
			want[m] = true
		}
		var out []*Volume
		for _, v := range volumes {
			for _, m := range v.Mountpoints {
				if want[m] {
					out = append(out, v)
					break
				}
			}
		}
		return out
	case pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_ALL_FIXED:
		var out []*Volume
		for _, v := range volumes {
			if !v.Removable {
				out = append(out, v)
			}
		}
		return out
	default: // SYSTEM
		var out []*Volume
		for _, v := range volumes {
			if v.IsSystem {
				out = append(out, v)
			}
		}
		return out
	}
}

// ScopeMounts filters mounts to those in scope for require_encryption.
func ScopeMounts(mounts []*Mount, scope pb.LuksVolumeScope, mountpoints []string) []*Mount {
	switch scope {
	case pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_MOUNTPOINTS:
		want := map[string]bool{}
		for _, m := range mountpoints {
			want[m] = true
		}
		var out []*Mount
		for _, m := range mounts {
			if want[m.Mountpoint] {
				out = append(out, m)
			}
		}
		return out
	case pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_ALL_FIXED:
		return mounts
	default: // SYSTEM
		var out []*Mount
		for _, m := range mounts {
			if systemMountpoints[m.Mountpoint] || m.Mountpoint == "[swap]" {
				out = append(out, m)
			}
		}
		return out
	}
}
