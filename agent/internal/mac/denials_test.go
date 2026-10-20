// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package mac

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const journalSample = `{"MESSAGE":"AVC avc:  denied  { getattr } for  pid=2541 comm=\"pgrep\" path=\"/proc/2\" dev=\"proc\" ino=1658 scontext=system_u:system_r:virt_qemu_ga_t:s0 tcontext=system_u:system_r:kernel_t:s0 tclass=dir permissive=0","_TRANSPORT":"audit","_AUDIT_TYPE_NAME":"AVC","_AUDIT_ID":"1700","__REALTIME_TIMESTAMP":"1792446227088000"}
{"MESSAGE":"USER_AVC pid=1 uid=0 auid=4294967295 ses=4294967295 subj=system_u:system_r:init_t:s0 msg='avc:  denied  { status } for auid=n/a uid=0 gid=0 path=\"/usr/lib/systemd/system/bor-agent.service\" cmdline=\"\" function=\"mac_selinux_filter\" scontext=system_u:system_r:virt_qemu_ga_t:s0 tcontext=system_u:object_r:systemd_unit_file_t:s0 tclass=service permissive=0 exe=\"/usr/lib/systemd/systemd\" sauid=0 hostname=? addr=? terminal=?'","_TRANSPORT":"audit","_AUDIT_TYPE_NAME":"USER_AVC","_AUDIT_ID":"1701","__REALTIME_TIMESTAMP":"1792446227089000"}
{"MESSAGE":"MAC_STATUS enforcing=0 old_enforcing=1 auid=1000 ses=27 enabled=1 old-enabled=1 lsm=selinux res=1","_TRANSPORT":"audit","_AUDIT_TYPE_NAME":"MAC_STATUS","_AUDIT_ID":"1724","__REALTIME_TIMESTAMP":"1792446227090000"}
{"MESSAGE":"audit: type=1400 audit(1792447101.241:765): apparmor=\"DENIED\" operation=\"open\" class=\"file\" profile=\"bor-spike-cat\" name=\"/home/blago/secret\" pid=3914 comm=\"bor-cat\" requested_mask=\"r\" denied_mask=\"r\" fsuid=1000 ouid=0","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1792447101241000"}
{"MESSAGE":"audit: type=1400 audit(1792447101.261:767): apparmor=\"ALLOWED\" operation=\"open\" class=\"file\" profile=\"bor-spike-cat\" name=\"/etc/hostname\" pid=3921 comm=\"bor-cat\" requested_mask=\"r\" denied_mask=\"r\" fsuid=1000 ouid=0","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1792447101261000"}
{"MESSAGE":"audit: type=1400 audit(1792447669.809:256): apparmor=\"AUDIT\" operation=\"userns_create\" class=\"namespace\" info=\"Userns create - transitioning profile\" profile=\"unconfined\" pid=3980 comm=\"unshare\" requested=\"userns_create\" target=\"unprivileged_userns\" execpath=\"/usr/bin/unshare\"","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1792447669809000"}
{"MESSAGE":"audit: type=1400 audit(1792447669.812:258): apparmor=\"DENIED\" operation=\"capable\" class=\"cap\" profile=\"unprivileged_userns\" pid=3982 comm=\"unshare\" capability=21  capname=\"sys_admin\"","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1792447669812000"}
{"MESSAGE":"audit: type=1400 audit(1792447003.590:380): apparmor=\"STATUS\" operation=\"profile_replace\" info=\"same as current profile, skipping\" profile=\"unconfined\" name=\"brave\" pid=3326 comm=\"apparmor_parser\"","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1792447003590000"}
{"MESSAGE":"something unrelated","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1792447003590000"}
`

func TestParseJournalJSON(t *testing.T) {
	recs := ParseJournalJSON([]byte(journalSample))
	if len(recs) != 8 {
		t.Fatalf("expected 8 LSM records, got %d", len(recs))
	}
	avc := recs[0]
	if avc.Kind != "selinux" || avc.Type != "AVC" || avc.Subject() != "virt_qemu_ga_t" || avc.Fields["tclass"] != "dir" || len(avc.Perms) != 1 || avc.Perms[0] != "getattr" {
		t.Errorf("AVC parsed wrong: %+v", avc)
	}
	if !strings.HasPrefix(avc.Raw, "type=AVC msg=audit(1792446227.088:1700): avc:") {
		t.Errorf("raw line wrong: %s", avc.Raw)
	}
	user := recs[1]
	if user.Type != "USER_AVC" || user.Subject() != "virt_qemu_ga_t" || user.Fields["tclass"] != "service" {
		t.Errorf("USER_AVC parsed wrong: %+v", user.Fields)
	}
	status := recs[2]
	if status.Kind != "selinux" || status.Type != "MAC_STATUS" || status.Fields["auid"] != "1000" || status.Fields["enforcing"] != "0" {
		t.Errorf("MAC_STATUS parsed wrong: %+v", status)
	}
	if status.IsDenial() || status.Subject() != "" {
		t.Error("MAC_STATUS must not count as a denial")
	}
	if !strings.HasPrefix(status.Raw, "type=MAC_STATUS msg=audit(1792446227.090:1724): enforcing=0") {
		t.Errorf("MAC_STATUS raw wrong: %s", status.Raw)
	}
	aa := recs[3]
	if aa.Kind != "apparmor" || aa.Type != "DENIED" || aa.Fields["profile"] != "bor-spike-cat" || aa.Fields["name"] != "/home/blago/secret" {
		t.Errorf("AppArmor parsed wrong: %+v", aa.Fields)
	}
	if !strings.HasPrefix(aa.Raw, "type=AVC msg=audit(1792447101.241:765): apparmor=") {
		t.Errorf("AppArmor raw wrong: %s", aa.Raw)
	}
	if !recs[5].IsDenial() || recs[5].Fields["execpath"] != "/usr/bin/unshare" {
		t.Errorf("userns transition not counted: %+v", recs[5].Fields)
	}
	if recs[7].IsDenial() {
		t.Error("STATUS record counted as denial")
	}
	for _, g := range Group(recs, 50) {
		if g.Subject == "" {
			t.Errorf("group without subject: %+v", g)
		}
	}
}

func TestGroup(t *testing.T) {
	recs := ParseJournalJSON([]byte(journalSample))
	groups := Group(recs, 50)
	byKey := map[string]int{}
	for _, g := range groups {
		byKey[g.Subject+"/"+g.Operation+"/"+g.Target] = int(g.Count)
		if strings.Contains(g.Example, "blago") || strings.Contains(g.Example, "auid=") {
			t.Errorf("example not redacted: %s", g.Example)
		}
	}
	if byKey["virt_qemu_ga_t/getattr/kernel_t"] != 1 || byKey["virt_qemu_ga_t/status/systemd_unit_file_t"] != 1 {
		t.Errorf("selinux groups wrong: %v", byKey)
	}
	if byKey["bor-spike-cat/open//home/*/secret"] != 1 {
		t.Errorf("apparmor file group wrong: %v", byKey)
	}
	if byKey["unconfined/userns_create/unprivileged_userns"] != 1 || byKey["unprivileged_userns/capable/sys_admin"] != 1 {
		t.Errorf("userns groups wrong: %v", byKey)
	}
	var permissive int
	for _, g := range groups {
		if g.Permissive {
			permissive++
		}
	}
	if permissive != 1 {
		t.Errorf("expected one ALLOWED group, got %d", permissive)
	}
	if len(Group(recs, 2)) != 2 {
		t.Error("max not honoured")
	}
}

func TestRedact(t *testing.T) {
	in := `type=AVC msg=audit(1:2): avc: denied { read } for pid=1 comm="x" path="/home/alice/file" auid=1000 uid=1000 scontext=a:b:c_t:s0 AUID="alice"`
	out := Redact(in)
	for _, bad := range []string{"alice", "auid=", "uid=1000", "AUID="} {
		if strings.Contains(out, bad) {
			t.Errorf("%q still in %q", bad, out)
		}
	}
	if !strings.Contains(out, "/home/*/file") {
		t.Errorf("home not collapsed: %s", out)
	}
}

func TestProposeApparmorRules(t *testing.T) {
	recs := ParseJournalJSON([]byte(journalSample))
	text := ProposeApparmorRules(ForSubject(recs, "apparmor", "bor-spike-cat"))
	if !strings.Contains(text, "@{HOME}/secret r,") || !strings.Contains(text, "/etc/hostname r,") {
		t.Errorf("file rules missing: %s", text)
	}
	text = ProposeApparmorRules(ForSubject(recs, "apparmor", "unprivileged_userns"))
	if !strings.Contains(text, "capability sys_admin,") {
		t.Errorf("capability rule missing: %s", text)
	}
	if apparmorMask("wr") != "rw" || apparmorMask("x") != "ix" || apparmorMask("rwk") != "rwk" {
		t.Errorf("mask ordering wrong: %q %q", apparmorMask("wr"), apparmorMask("x"))
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	p := Paths{
		LSMList:         filepath.Join(dir, "lsm"),
		SelinuxFS:       filepath.Join(dir, "selinux"),
		ApparmorEnabled: filepath.Join(dir, "aa-enabled"),
		Cmdline:         filepath.Join(dir, "cmdline"),
	}
	if lsm, mode := Summary(p); lsm != "none" || mode != "" {
		t.Errorf("empty: %s %s", lsm, mode)
	}
	_ = os.WriteFile(p.LSMList, []byte("lockdown,capability,yama,selinux,bpf\n"), 0o600)
	if lsm, mode := Summary(p); lsm != "none" || mode != "disabled" {
		t.Errorf("selinux in list only: %s %s", lsm, mode)
	}
	_ = os.MkdirAll(p.SelinuxFS, 0o755)
	_ = os.WriteFile(filepath.Join(p.SelinuxFS, "enforce"), []byte("1\n"), 0o600)
	if lsm, mode := Summary(p); lsm != "selinux" || mode != "enforcing" {
		t.Errorf("enforcing: %s %s", lsm, mode)
	}
	_ = os.WriteFile(filepath.Join(p.SelinuxFS, "enforce"), []byte("0"), 0o600)
	if _, mode := Summary(p); mode != "permissive" {
		t.Errorf("permissive: %s", mode)
	}
	_ = os.RemoveAll(p.SelinuxFS)
	_ = os.WriteFile(p.LSMList, []byte("lockdown,capability,landlock,yama,apparmor,bpf"), 0o600)
	_ = os.WriteFile(p.ApparmorEnabled, []byte("Y\n"), 0o600)
	if lsm, mode := Summary(p); lsm != "apparmor" || mode != "enabled" {
		t.Errorf("apparmor: %s %s", lsm, mode)
	}
	_ = os.WriteFile(p.Cmdline, []byte("BOOT_IMAGE=/vmlinuz root=UUID=x ro quiet selinux=0 apparmor=1 security=apparmor"), 0o600)
	flags := ReadBootFlags(p)
	if len(flags) != 3 || flags[0] != "selinux=0" {
		t.Errorf("boot flags: %v", flags)
	}
}
