// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package apparmor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const stub = `# This profile allows everything and only exists to give the
# application a name instead of having the label "unconfined"

abi <abi/4.0>,
include <tunables/global>

profile firefox /{usr/lib/firefox{,-esr,-beta,-devedition,-nightly},opt/firefox}/firefox{,-esr,-bin} flags=(unconfined) {
  userns,

  # Site-specific additions and overrides. See local/README for details.
  include if exists <local/firefox>
}
`

const cupsd = `#include <tunables/global>

/usr/sbin/cupsd flags=(attach_disconnected) {
  #include <abstractions/base>
  profile third_party {
    /usr/lib/cups/** rix,
  }
  # Site-specific additions and overrides. See local/README for details.
  #include <local/usr.sbin.cupsd>
}
`

func TestParseProfileText(t *testing.T) {
	pf := ParseProfileText(stub)
	if len(pf.Names) != 1 || pf.Names[0] != "firefox" || pf.ABI != "4.0" || pf.LocalInclude != "firefox" || pf.FileMode() != "unconfined" {
		t.Errorf("stub parsed wrong: %+v", pf)
	}
	if len(pf.Attachments) != 1 || !strings.HasPrefix(pf.Attachments[0], "/{usr/lib/firefox") {
		t.Errorf("attachment wrong: %v", pf.Attachments)
	}
	pf = ParseProfileText(cupsd)
	if len(pf.Names) != 1 || pf.Names[0] != "/usr/sbin/cupsd" || pf.Attachments[0] != "/usr/sbin/cupsd" || pf.LocalInclude != "usr.sbin.cupsd" || pf.FileMode() != "" || pf.ABI != "" {
		t.Errorf("cupsd parsed wrong: %+v", pf)
	}
	pf = ParseProfileText("profile Xorg /usr/lib/xorg/Xorg flags=(complain, attach_disconnected) {\n}\n")
	if pf.FileMode() != "complain" || len(pf.Flags) != 2 {
		t.Errorf("flags wrong: %+v", pf)
	}
}

func TestSetEnforceFlags(t *testing.T) {
	text := "abi <abi/4.0>,\nprofile Xorg /usr/lib/xorg/Xorg flags=(complain, attach_disconnected) {\n  profile child flags=(complain) {\n  }\n}\n"
	next, changed := SetEnforceFlags(text)
	if !changed || !strings.Contains(next, "flags=(attach_disconnected) {") || !strings.Contains(next, "profile child flags=(complain)") {
		t.Errorf("enforce flags:\n%s", next)
	}
	only := "profile x /x flags=(complain) {\n}\n"
	next, changed = SetEnforceFlags(only)
	if !changed || !strings.HasPrefix(next, "profile x /x {") {
		t.Errorf("sole flag:\n%s", next)
	}
	if _, changed = SetEnforceFlags("profile x /x {\n}\n"); changed {
		t.Error("enforce file reported changed")
	}
}

func TestScanAndLinks(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "firefox"), []byte(stub), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "usr.sbin.cupsd"), []byte(cupsd), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "usr.bin.man.dpkg-new"), []byte(cupsd), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "abstractions"), 0o755)
	files, err := ScanProfiles(dir)
	if err != nil || len(files) != 2 || files[0].File != "firefox" || files[1].File != "usr.sbin.cupsd" {
		t.Fatalf("scan: %v %+v", err, files)
	}
	p := Paths{ProfilesDir: dir}
	changed, err := p.SetLink(ForceComplainDir, "usr.sbin.cupsd", true)
	if err != nil || !changed {
		t.Fatalf("set link: %v %v", changed, err)
	}
	if d, c := p.LinkState("usr.sbin.cupsd"); d || !c {
		t.Errorf("link state: %v %v", d, c)
	}
	target, _ := os.Readlink(filepath.Join(dir, ForceComplainDir, "usr.sbin.cupsd"))
	if target != filepath.Join(dir, "usr.sbin.cupsd") {
		t.Errorf("link target: %s", target)
	}
	if changed, _ = p.SetLink(ForceComplainDir, "usr.sbin.cupsd", true); changed {
		t.Error("second set reported change")
	}
	if changed, _ = p.SetLink(ForceComplainDir, "usr.sbin.cupsd", false); !changed {
		t.Error("remove not reported")
	}
}

func TestManagedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local", "usr.sbin.cupsd")
	changed, created, err := WriteManagedBlock(path, "  /nfs/home/** rwk,")
	if err != nil || !changed || !created {
		t.Fatalf("create: %v %v %v", changed, created, err)
	}
	text, found := ReadManagedBlock(path)
	if !found || text != "  /nfs/home/** rwk,\n" {
		t.Errorf("read block: %q %v", text, found)
	}
	if changed, _, _ = WriteManagedBlock(path, "  /nfs/home/** rwk,\n"); changed {
		t.Error("identical write reported change")
	}
	// Admin content outside the block survives a rewrite and a removal.
	data, _ := os.ReadFile(path)
	_ = os.WriteFile(path, append([]byte("  /srv/** r,\n"), data...), 0o644)
	changed, _, _ = WriteManagedBlock(path, "  /export/** rwk,")
	data, _ = os.ReadFile(path)
	if !changed || !strings.HasPrefix(string(data), "  /srv/** r,\n"+BlockBegin) || !strings.Contains(string(data), "/export/**") || strings.Contains(string(data), "/nfs/home") {
		t.Errorf("rewrite:\n%s", data)
	}
	if changed, err = RemoveManagedBlock(path, true); err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "  /srv/** r,\n" {
		t.Errorf("after removal: %q", data)
	}
	// A file Bor created and emptied is deleted.
	path2 := filepath.Join(dir, "local", "bor-only")
	_, _, _ = WriteManagedBlock(path2, "x,")
	if _, err := RemoveManagedBlock(path2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path2); !os.IsNotExist(err) {
		t.Error("created file not deleted")
	}
	// A 0-byte vendor file (mandatory include) is kept.
	path3 := filepath.Join(dir, "local", "usr.bin.man")
	_ = os.WriteFile(path3, nil, 0o644)
	_, _, _ = WriteManagedBlock(path3, "x,")
	_, _ = RemoveManagedBlock(path3, false)
	if _, err := os.Stat(path3); err != nil {
		t.Error("vendor local file deleted")
	}
}

func TestParserArgs(t *testing.T) {
	var got [][]string
	p := &Parser{Binary: "/bin/true", Dir: "/etc/apparmor.d", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append(got, append([]string{name}, args...))
		return nil, nil
	}}
	ctx := context.Background()
	_ = p.Check(ctx, "/etc/apparmor.d/bor-x")
	_ = p.Replace(ctx, "/etc/apparmor.d/bor-x", true)
	_ = p.Remove(ctx, "/etc/apparmor.d/bor-x")
	want := []string{
		"/bin/true -Q -K -b /etc/apparmor.d /etc/apparmor.d/bor-x",
		"/bin/true -r -C -b /etc/apparmor.d /etc/apparmor.d/bor-x",
		"/bin/true -R -b /etc/apparmor.d /etc/apparmor.d/bor-x",
	}
	for i, w := range want {
		if strings.Join(got[i], " ") != w {
			t.Errorf("call %d: %v", i, got[i])
		}
	}
}

func TestStateReads(t *testing.T) {
	dir := t.TempDir()
	p := Paths{
		EnabledParam:  filepath.Join(dir, "enabled"),
		SecurityFS:    filepath.Join(dir, "securityfs"),
		ProfilesDir:   filepath.Join(dir, "apparmor.d"),
		Proc:          filepath.Join(dir, "proc"),
		ProcSysKernel: filepath.Join(dir, "sysctl"),
		LSMList:       filepath.Join(dir, "lsm"),
	}
	if p.Enabled() || p.InLSMList() {
		t.Error("enabled on empty dir")
	}
	_ = os.WriteFile(p.EnabledParam, []byte("Y\n"), 0o600)
	_ = os.WriteFile(p.LSMList, []byte("lockdown,apparmor"), 0o600)
	_ = os.MkdirAll(filepath.Join(p.SecurityFS, "features", "namespaces"), 0o755)
	_ = os.WriteFile(filepath.Join(p.SecurityFS, "features", "namespaces", "mask"), []byte("userns_create\n"), 0o600)
	_ = os.WriteFile(filepath.Join(p.SecurityFS, "profiles"), []byte("libreoffice-soffice (complain)\nlibreoffice-soffice//gpg (enforce)\nwpcom (unconfined)\n/usr/sbin/cupsd (enforce)\n"), 0o600)
	_ = os.MkdirAll(p.ProcSysKernel, 0o755)
	_ = os.WriteFile(filepath.Join(p.ProcSysKernel, SysctlRestrictUserns), []byte("1\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(p.Proc, "42", "attr", "apparmor"), 0o755)
	_ = os.WriteFile(filepath.Join(p.Proc, "42", "attr", "apparmor", "current"), []byte("/usr/sbin/cupsd (enforce)\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(p.Proc, "43", "attr", "apparmor"), 0o755)
	_ = os.WriteFile(filepath.Join(p.Proc, "43", "attr", "apparmor", "current"), []byte("unconfined\n"), 0o600)
	if !p.Enabled() || !p.InLSMList() || !p.UsernsMediation() {
		t.Error("state reads wrong")
	}
	loaded, err := p.LoadedProfiles()
	if err != nil || len(loaded) != 4 || loaded["/usr/sbin/cupsd"] != "enforce" || loaded["libreoffice-soffice//gpg"] != "enforce" || loaded["wpcom"] != "unconfined" {
		t.Errorf("loaded: %v %v", loaded, err)
	}
	if v, ok := p.Sysctl(SysctlRestrictUserns); !ok || v != 1 {
		t.Error("sysctl read wrong")
	}
	if _, ok := p.Sysctl(SysctlRestrictUnconfined); ok {
		t.Error("missing sysctl reported present")
	}
	if werr := p.SetSysctl(SysctlRestrictUserns, 0); werr != nil {
		t.Fatal(werr)
	}
	if v, _ := p.Sysctl(SysctlRestrictUserns); v != 0 {
		t.Error("sysctl write wrong")
	}
	labels, err := p.ProcessLabels()
	if err != nil || len(labels) != 2 || labels[0].Label != "/usr/sbin/cupsd" || labels[0].Mode != "enforce" || labels[1].Label != "unconfined" {
		t.Errorf("labels: %+v %v", labels, err)
	}
}
