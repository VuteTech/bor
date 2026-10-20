// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/agent/internal/apparmor"
	"github.com/VuteTech/Bor/agent/internal/mac"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

const aaCupsd = "#include <tunables/global>\n/usr/sbin/cupsd flags=(attach_disconnected) {\n  #include <abstractions/base>\n  #include <local/usr.sbin.cupsd>\n}\n"
const aaMan = "abi <abi/4.0>,\ninclude <tunables/global>\nprofile man /usr/bin/man {\n  include if exists <local/usr.bin.man>\n}\n"
const aaXorg = "abi <abi/4.0>,\nprofile Xorg /usr/lib/xorg/Xorg flags=(complain, attach_disconnected) {\n  include if exists <local/Xorg>\n}\n"
const aaFirefox = "abi <abi/4.0>,\nprofile firefox /usr/lib/firefox/firefox flags=(unconfined) {\n  userns,\n}\n"
const aaStub = "abi <abi/4.0>,\ninclude <tunables/global>\nprofile bor-corpapp /opt/corpapp/app flags=(unconfined) {\n  userns,\n}\n"

type fakeApparmor struct {
	root  string
	deps  ApparmorDeps
	opts  ApparmorSyncOptions
	calls []string
	fail  string
}

func newFakeApparmor(t *testing.T, enabled bool) *fakeApparmor {
	t.Helper()
	root := t.TempDir()
	f := &fakeApparmor{root: root}
	paths := apparmor.Paths{
		EnabledParam:  filepath.Join(root, "enabled"),
		SecurityFS:    filepath.Join(root, "securityfs"),
		ProfilesDir:   filepath.Join(root, "apparmor.d"),
		Proc:          filepath.Join(root, "proc"),
		ProcSysKernel: filepath.Join(root, "sysctl"),
		LSMList:       filepath.Join(root, "lsm"),
	}
	_ = os.MkdirAll(paths.ProfilesDir, 0o755)
	_ = os.MkdirAll(filepath.Join(paths.ProfilesDir, "local"), 0o755)
	_ = os.MkdirAll(filepath.Join(paths.SecurityFS, "features", "namespaces"), 0o755)
	_ = os.MkdirAll(paths.ProcSysKernel, 0o755)
	_ = os.MkdirAll(paths.Proc, 0o755)
	_ = os.WriteFile(paths.LSMList, []byte("lockdown,apparmor"), 0o600)
	if enabled {
		_ = os.WriteFile(paths.EnabledParam, []byte("Y\n"), 0o600)
	}
	for name, text := range map[string]string{"usr.sbin.cupsd": aaCupsd, "usr.bin.man": aaMan, "Xorg": aaXorg, "firefox": aaFirefox} {
		_ = os.WriteFile(filepath.Join(paths.ProfilesDir, name), []byte(text), 0o644)
	}
	_ = os.WriteFile(filepath.Join(paths.ProfilesDir, "local", "usr.bin.man"), nil, 0o644)
	_ = os.WriteFile(filepath.Join(paths.SecurityFS, "profiles"), []byte("/usr/sbin/cupsd (enforce)\nman (enforce)\nXorg (complain)\nfirefox (unconfined)\n"), 0o600)
	_ = os.WriteFile(filepath.Join(paths.SecurityFS, "features", "namespaces", "mask"), []byte("userns_create"), 0o600)
	_ = os.WriteFile(filepath.Join(paths.ProcSysKernel, apparmor.SysctlRestrictUserns), []byte("0\n"), 0o600)
	parser := &apparmor.Parser{Binary: "/bin/true", Dir: paths.ProfilesDir}
	parser.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, strings.Join(args, " "))
		if f.fail != "" && strings.Contains(strings.Join(args, " "), f.fail) {
			return []byte("AppArmor parser error: simulated"), os.ErrInvalid
		}
		f.kernel(args)
		return nil, nil
	}
	f.deps = ApparmorDeps{Paths: paths, Parser: parser, Mac: mac.Paths{LSMList: paths.LSMList, ApparmorEnabled: paths.EnabledParam, Cmdline: filepath.Join(root, "cmdline")}}
	f.opts = ApparmorSyncOptions{SysctlPath: filepath.Join(root, "sysctl.d", "zz-bor-apparmor.conf"), StatePath: filepath.Join(root, "state.json"), BackupDir: filepath.Join(root, "backups"), AllowCustomProfiles: true, AllowVendorFlagEdits: true}
	return f
}

// kernel mirrors what apparmor_parser would make the kernel list after a
// load or unload: the first profile of the file with its mode.
func (f *fakeApparmor) kernel(args []string) {
	if len(args) == 0 {
		return
	}
	target := args[len(args)-1]
	profilesFile := filepath.Join(f.deps.Paths.SecurityFS, "profiles")
	loaded, _ := f.deps.Paths.LoadedProfiles()
	apply := func(path string, remove bool) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		pf := apparmor.ParseProfileText(string(data))
		if len(pf.Names) == 0 {
			return
		}
		if remove {
			delete(loaded, pf.Names[0])
			return
		}
		mode := "enforce"
		switch {
		case pf.FileMode() == "unconfined":
			mode = "unconfined"
		case pf.FileMode() == "complain" || args[1] == "-C":
			mode = "complain"
		}
		if _, c := f.deps.Paths.LinkState(filepath.Base(path)); c {
			mode = "complain"
		}
		loaded[pf.Names[0]] = mode
	}
	switch args[0] {
	case "-r":
		if target == f.deps.Paths.ProfilesDir {
			entries, _ := os.ReadDir(target)
			for _, e := range entries {
				if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					apply(filepath.Join(target, e.Name()), false)
				}
			}
		} else {
			apply(target, false)
		}
	case "-R":
		apply(target, true)
	}
	var b strings.Builder
	for name, mode := range loaded {
		b.WriteString(name + " (" + mode + ")\n")
	}
	_ = os.WriteFile(profilesFile, []byte(b.String()), 0o600)
}

func (f *fakeApparmor) calledExact(call string) int {
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (f *fakeApparmor) called(sub string) int {
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func aaEntry(id string, prio int32, p *pb.ApparmorPolicy) ApparmorEntry {
	return ApparmorEntry{ID: id, Name: "policy-" + id, Priority: prio, Policy: p}
}

func TestResolveApparmorMode(t *testing.T) {
	a := aaEntry("a", 10, &pb.ApparmorPolicy{Profiles: []*pb.ApparmorProfileMode{{Match: "*", Mode: pb.ApparmorMode_APPARMOR_MODE_ENFORCE}, {Match: "usr.sbin.*", Mode: pb.ApparmorMode_APPARMOR_MODE_COMPLAIN}}})
	b := aaEntry("b", 5, &pb.ApparmorPolicy{Profiles: []*pb.ApparmorProfileMode{{Match: "usr.sbin.cupsd", Mode: pb.ApparmorMode_APPARMOR_MODE_DISABLED}}})
	c := aaEntry("c", 10, &pb.ApparmorPolicy{Profiles: []*pb.ApparmorProfileMode{{Match: "usr.sbin.*", Mode: pb.ApparmorMode_APPARMOR_MODE_ENFORCE}}})
	d := MergeApparmorPolicies([]ApparmorEntry{a, b, c})
	if r := d.ResolveMode("usr.sbin.cupsd"); r == nil || r.Mode != pb.ApparmorMode_APPARMOR_MODE_DISABLED || r.By.ID != "b" {
		t.Errorf("exact name should win over globs regardless of priority: %+v", r)
	}
	if r := d.ResolveMode("usr.sbin.sssd"); r == nil || r.Mode != pb.ApparmorMode_APPARMOR_MODE_ENFORCE || r.By.ID != "c" {
		t.Errorf("equal priority, same specificity: enforce should win: %+v", r)
	}
	if r := d.ResolveMode("usr.bin.man"); r == nil || r.Mode != pb.ApparmorMode_APPARMOR_MODE_ENFORCE || r.Match != "*" {
		t.Errorf("catch-all: %+v", r)
	}
	if d.ResolveMode("snap.x") == nil {
		t.Error("catch-all should match any file")
	}
}

func TestSyncApparmorDisabledNode(t *testing.T) {
	f := newFakeApparmor(t, false)
	pol := &pb.ApparmorPolicy{RequireEnabled: true, Profiles: []*pb.ApparmorProfileMode{{Match: "usr.sbin.cupsd", Mode: pb.ApparmorMode_APPARMOR_MODE_COMPLAIN}}}
	res := SyncApparmor(context.Background(), &f.deps, []ApparmorEntry{aaEntry("a", 10, pol)}, f.opts)
	r := res.Policies["a"]
	if res.Enabled || r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Fatalf("disabled node: %+v", r)
	}
	if _, c := f.deps.Paths.LinkState("usr.sbin.cupsd"); !c {
		t.Error("link should be prepared even while AppArmor is disabled")
	}
	if f.called("-r") > 0 {
		t.Error("parser load attempted while disabled")
	}
	if st, _ := itemStatus(r.Items, apparmorItemProfile, "usr.sbin.cupsd"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Error("mode item should be inapplicable while disabled")
	}
}

func TestSyncApparmorApplyAndRestore(t *testing.T) {
	f := newFakeApparmor(t, true)
	pol := &pb.ApparmorPolicy{
		RequireEnabled:                 true,
		RestrictUnprivilegedUserns:     pb.ApparmorSwitch_APPARMOR_SWITCH_ON,
		RestrictUnprivilegedUnconfined: pb.ApparmorSwitch_APPARMOR_SWITCH_ON,
		Profiles: []*pb.ApparmorProfileMode{
			{Match: "usr.bin.man", Mode: pb.ApparmorMode_APPARMOR_MODE_COMPLAIN},
			{Match: "*", Mode: pb.ApparmorMode_APPARMOR_MODE_ENFORCE},
			{Match: "missing-profile", Mode: pb.ApparmorMode_APPARMOR_MODE_ENFORCE},
		},
		CustomProfiles: []*pb.ApparmorCustomProfile{{Name: "corpapp", Text: aaStub}},
		LocalOverrides: []*pb.ApparmorLocalOverride{{ProfileFile: "usr.sbin.cupsd", Text: "  /nfs/home/** rwk,"}, {ProfileFile: "usr.bin.other", Text: "/x r,"}},
		ExtraHomeDirs:  []string{"/nfs/home/"},
	}
	entries := []ApparmorEntry{aaEntry("a", 10, pol)}
	res := SyncApparmor(context.Background(), &f.deps, entries, f.opts)
	r := res.Policies["a"]
	// Xorg ships with flags=(complain): the catch-all cannot enforce it
	// without a vendor flag edit, so the policy is non-compliant on that
	// one file and says why.
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(r.Message, "Xorg") {
		t.Fatalf("status %s: %s\n%v", r.Status, r.Message, r.Items)
	}
	for _, key := range []string{"usr.bin.man", "missing-profile"} {
		if st, _ := itemStatus(r.Items, apparmorItemProfile, key); st == pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
			t.Errorf("%s errored", key)
		}
	}
	// Links: man complain, cupsd enforce (no links), firefox left alone.
	if d, c := f.deps.Paths.LinkState("usr.bin.man"); d || !c {
		t.Error("man should have a force-complain link")
	}
	if d, c := f.deps.Paths.LinkState("usr.sbin.cupsd"); d || c {
		t.Error("cupsd should have no links")
	}
	if st, msg := itemStatus(r.Items, apparmorItemProfile, "*"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(msg, "2 as requested, 1 not") || !strings.Contains(msg, "1 left alone") {
		t.Errorf("glob item: %s %s", st, msg)
	}
	if st, _ := itemStatus(r.Items, apparmorItemProfile, "missing-profile"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Error("missing profile should be inapplicable")
	}
	// Userns sysctl applied, unconfined switch missing on this kernel.
	if v, _ := f.deps.Paths.Sysctl(apparmor.SysctlRestrictUserns); v != 1 {
		t.Error("userns sysctl not set")
	}
	if st, _ := itemStatus(r.Items, apparmorItemUnconfined, apparmor.SysctlRestrictUnconfined); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Error("missing sysctl should be inapplicable")
	}
	if data, err := os.ReadFile(f.opts.SysctlPath); err != nil || !strings.Contains(string(data), "kernel.apparmor_restrict_unprivileged_userns = 1") {
		t.Errorf("sysctl file: %s %v", data, err)
	}
	// Custom profile checked, written and loaded.
	custom := filepath.Join(f.deps.Paths.ProfilesDir, "bor-corpapp")
	if data, err := os.ReadFile(custom); err != nil || !strings.Contains(string(data), "profile bor-corpapp") {
		t.Errorf("custom profile: %v", err)
	}
	if f.called("-Q -K") == 0 || f.called("-r -b "+f.deps.Paths.ProfilesDir+" "+custom) == 0 {
		t.Errorf("parser calls: %v", f.calls)
	}
	// Override written into the local file, parser re-checked cupsd.
	if text, ok := apparmor.ReadManagedBlock(f.deps.Paths.LocalPath("usr.sbin.cupsd")); !ok || !strings.Contains(text, "/nfs/home/**") {
		t.Error("override block missing")
	}
	if st, _ := itemStatus(r.Items, apparmorItemLocal, "usr.bin.other"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Error("override for a missing profile should be inapplicable")
	}
	// Home tunable written and a full reload requested.
	if data, err := os.ReadFile(f.deps.Paths.HomeTunablePath()); err != nil || !strings.Contains(string(data), "@{HOMEDIRS}+=/nfs/home/") {
		t.Errorf("home tunable: %s %v", data, err)
	}
	if f.calledExact("-r -b "+f.deps.Paths.ProfilesDir+" "+f.deps.Paths.ProfilesDir) != 1 {
		t.Errorf("expected one directory reload: %v", f.calls)
	}
	state, _ := LoadApparmorState(f.opts.StatePath)
	if !state.HomeDirs || !state.SysctlFile || state.Sysctls[apparmor.SysctlRestrictUserns] != 0 || state.Customs["corpapp"] == "" || !state.Overrides["usr.sbin.cupsd"].Created || len(state.Links) == 0 {
		t.Errorf("state: %+v", state)
	}

	// Second run: idempotent.
	f.calls = nil
	res = SyncApparmor(context.Background(), &f.deps, entries, f.opts)
	if res.Changed || len(f.calls) > 0 {
		t.Errorf("second run changed things: %v", f.calls)
	}

	// Drift: someone removes the complain link and adds a disable link.
	_, _ = f.deps.Paths.SetLink(apparmor.ForceComplainDir, "usr.bin.man", false)
	_, _ = f.deps.Paths.SetLink(apparmor.DisableDir, "usr.sbin.cupsd", true)
	opts := f.opts
	opts.Reconcile = true
	res = SyncApparmor(context.Background(), &f.deps, entries, opts)
	if len(res.Drifts) != 2 {
		t.Errorf("drifts: %+v", res.Drifts)
	}
	if d, c := f.deps.Paths.LinkState("usr.bin.man"); d || !c {
		t.Error("man link not restored")
	}

	// Unbind: everything restored.
	f.calls = nil
	res = SyncApparmor(context.Background(), &f.deps, nil, f.opts)
	if _, err := os.Stat(custom); !os.IsNotExist(err) {
		t.Error("custom profile not removed")
	}
	if f.called("-R -b "+f.deps.Paths.ProfilesDir+" "+custom) == 0 {
		t.Errorf("custom profile not unloaded: %v", f.calls)
	}
	if _, ok := apparmor.ReadManagedBlock(f.deps.Paths.LocalPath("usr.sbin.cupsd")); ok {
		t.Error("override block not removed")
	}
	if _, err := os.Stat(f.deps.Paths.LocalPath("usr.sbin.cupsd")); !os.IsNotExist(err) {
		t.Error("local file Bor created not deleted")
	}
	if _, err := os.Stat(f.deps.Paths.HomeTunablePath()); !os.IsNotExist(err) {
		t.Error("home tunable not removed")
	}
	if _, err := os.Stat(f.opts.SysctlPath); !os.IsNotExist(err) {
		t.Error("sysctl file not removed")
	}
	if v, _ := f.deps.Paths.Sysctl(apparmor.SysctlRestrictUserns); v != 0 {
		t.Error("sysctl not restored")
	}
	if _, c := f.deps.Paths.LinkState("usr.bin.man"); c {
		t.Error("man complain link not removed on unbind")
	}
	if _, err := os.Stat(f.opts.StatePath); !os.IsNotExist(err) {
		t.Error("state file not removed")
	}
}

func TestSyncApparmorConflictsAndFlags(t *testing.T) {
	f := newFakeApparmor(t, true)
	conflicting := "profile bor-cups2 /usr/sbin/cupsd {\n}\n"
	pol := &pb.ApparmorPolicy{
		CustomProfiles: []*pb.ApparmorCustomProfile{{Name: "cups2", Text: conflicting}},
		Profiles:       []*pb.ApparmorProfileMode{{Match: "Xorg", Mode: pb.ApparmorMode_APPARMOR_MODE_ENFORCE, EditVendorFlags: true}},
	}
	res := SyncApparmor(context.Background(), &f.deps, []ApparmorEntry{aaEntry("a", 10, pol)}, f.opts)
	r := res.Policies["a"]
	if st, msg := itemStatus(r.Items, apparmorItemCustom, "bor-cups2"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(msg, "already used") {
		t.Errorf("conflict: %s %s", st, msg)
	}
	if _, err := os.Stat(filepath.Join(f.deps.Paths.ProfilesDir, "bor-cups2")); !os.IsNotExist(err) {
		t.Error("conflicting profile was written")
	}
	data, _ := os.ReadFile(filepath.Join(f.deps.Paths.ProfilesDir, "Xorg"))
	if strings.Contains(string(data), "complain") {
		t.Errorf("vendor flags not edited:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(f.opts.BackupDir, "Xorg")); err != nil {
		t.Error("no backup of the vendor file")
	}
	if st, msg := itemStatus(r.Items, apparmorItemProfile, "Xorg"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("Xorg: %s %s", st, msg)
	}
	// Without the agent switch the file stays and the item explains.
	f2 := newFakeApparmor(t, true)
	f2.opts.AllowVendorFlagEdits = false
	res = SyncApparmor(context.Background(), &f2.deps, []ApparmorEntry{aaEntry("a", 10, pol)}, f2.opts)
	if st, msg := itemStatus(res.Policies["a"].Items, apparmorItemProfile, "Xorg"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(msg, "edit vendor flags") {
		t.Errorf("Xorg without edits: %s %s", st, msg)
	}
	// Unbind restores the edited file.
	SyncApparmor(context.Background(), &f.deps, nil, f.opts)
	data, _ = os.ReadFile(filepath.Join(f.deps.Paths.ProfilesDir, "Xorg"))
	if string(data) != aaXorg {
		t.Errorf("vendor file not restored:\n%s", data)
	}
}

func TestSyncApparmorParserFailure(t *testing.T) {
	f := newFakeApparmor(t, true)
	f.fail = "-Q -K"
	pol := &pb.ApparmorPolicy{CustomProfiles: []*pb.ApparmorCustomProfile{{Name: "corpapp", Text: aaStub}}}
	res := SyncApparmor(context.Background(), &f.deps, []ApparmorEntry{aaEntry("a", 10, pol)}, f.opts)
	if res.Policies["a"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(res.Policies["a"].Message, "simulated") {
		t.Errorf("parser failure: %+v", res.Policies["a"])
	}
	if _, err := os.Stat(filepath.Join(f.deps.Paths.ProfilesDir, "bor-corpapp")); !os.IsNotExist(err) {
		t.Error("rejected profile was installed")
	}
	entries, _ := os.ReadDir(f.deps.Paths.ProfilesDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bor-new") {
			t.Error("temporary file left behind")
		}
	}
}

func TestScanApparmorInventory(t *testing.T) {
	f := newFakeApparmor(t, true)
	_ = os.WriteFile(filepath.Join(f.deps.Paths.SecurityFS, "profiles"), []byte("/usr/sbin/cupsd (enforce)\nman (enforce)\nXorg (complain)\nfirefox (unconfined)\nsnap.firefox.firefox (enforce)\n"), 0o600)
	inv := ScanApparmorInventory(context.Background(), &f.deps, newApparmorState())
	st := inv.Status
	if !st.Enabled || !st.InLsmList || !st.UsernsMediation || st.EnforceCount != 3 || st.ComplainCount != 1 || st.UnconfinedCount != 1 || st.RestrictUserns != 0 || st.RestrictUnconfined != -1 || st.ProfileFileCount != 4 {
		t.Errorf("status: %+v", st)
	}
	byName := map[string]*pb.ApparmorProfileInfo{}
	for _, p := range inv.Profiles {
		byName[p.Name] = p
	}
	if p := byName["/usr/sbin/cupsd"]; p == nil || p.File != "usr.sbin.cupsd" || p.LoadedMode != "enforce" || !p.HasLocalInclude || p.ManagedBy != "vendor" {
		t.Errorf("cupsd: %+v", p)
	}
	if p := byName["Xorg"]; p == nil || p.FileMode != "complain" {
		t.Errorf("Xorg: %+v", p)
	}
	if p := byName["snap.firefox.firefox"]; p == nil || p.ManagedBy != "snapd" || p.File != "" {
		t.Errorf("snap: %+v", p)
	}
}
