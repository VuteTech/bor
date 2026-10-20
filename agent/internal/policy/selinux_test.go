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
	"time"

	"github.com/VuteTech/Bor/agent/internal/mac"
	"github.com/VuteTech/Bor/agent/internal/selinux"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// fakeSelinux is a node with selinuxfs files, a config file and recorded
// tool invocations; semodule and setsebool update its in-memory store.
type fakeSelinux struct {
	t        *testing.T
	root     string
	deps     SelinuxDeps
	opts     SelinuxSyncOptions
	calls    []string
	modules  map[string]selinux.Module
	logins   map[string]selinux.Login
	failTool string
}

func newFakeSelinux(t *testing.T) *fakeSelinux {
	t.Helper()
	root := t.TempDir()
	f := &fakeSelinux{t: t, root: root, modules: map[string]selinux.Module{
		"abrt": {Name: "abrt", Priority: 100, Enabled: true, Lang: "pp"},
		"zos":  {Name: "zos", Priority: 100, Enabled: true, Lang: "pp"},
	}, logins: map[string]selinux.Login{"__default__": {Seuser: "unconfined_u", Range: "s0-s0:c0.c1023"}}}
	paths := selinux.Paths{
		SysFS:        filepath.Join(root, "sys"),
		Config:       filepath.Join(root, "etc", "selinux", "config"),
		SemanageConf: filepath.Join(root, "etc", "selinux", "semanage.conf"),
		Autorelabel:  filepath.Join(root, ".autorelabel"),
	}
	_ = os.MkdirAll(filepath.Join(paths.SysFS, "booleans"), 0o755)
	_ = os.MkdirAll(filepath.Dir(paths.Config), 0o755)
	tools := &selinux.Tools{SetseboolBin: "setsebool", SemoduleBin: "semodule", RestoreconBin: "restorecon", SemanageBin: "semanage", LockRetries: 1, Sleep: func(time.Duration) {}, LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil }}
	tools.Run = f.run
	macPaths := mac.Paths{LSMList: filepath.Join(root, "lsm"), SelinuxFS: paths.SysFS, ApparmorEnabled: filepath.Join(root, "aa"), Cmdline: filepath.Join(root, "cmdline")}
	_ = os.WriteFile(macPaths.LSMList, []byte("lockdown,selinux,bpf"), 0o600)
	f.deps = SelinuxDeps{Paths: paths, Tools: tools, Mac: macPaths}
	f.setBool("deny_ptrace", false)
	f.setBool("use_nfs_home_dirs", false)
	f.setBool("abrt_anon_write", true)
	f.opts = SelinuxSyncOptions{ModulesDir: filepath.Join(root, "etc", "bor", "selinux"), ModulePriority: 450, StatePath: filepath.Join(root, "state.json"), AllowModeChange: true, AllowCustomModules: true}
	return f
}

func (f *fakeSelinux) activate(enforcing bool, configMode string) {
	v := "0"
	if enforcing {
		v = "1"
	}
	_ = os.WriteFile(filepath.Join(f.deps.Paths.SysFS, "enforce"), []byte(v), 0o600)
	_ = os.WriteFile(filepath.Join(f.deps.Paths.SysFS, "policyvers"), []byte("35"), 0o600)
	_ = os.WriteFile(f.deps.Paths.Config, []byte("# config\nSELINUX="+configMode+"\nSELINUXTYPE=targeted\n"), 0o644)
}

func (f *fakeSelinux) setBool(name string, v bool) {
	s := "0 0"
	if v {
		s = "1 1"
	}
	_ = os.WriteFile(filepath.Join(f.deps.Paths.SysFS, "booleans", name), []byte(s), 0o600)
}

func (f *fakeSelinux) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name == f.failTool {
		return []byte("simulated failure"), os.ErrPermission
	}
	switch name {
	case "setsebool":
		for _, a := range args[1:] {
			k, v, _ := strings.Cut(a, "=")
			f.setBool(k, v == "on")
		}
	case "semodule":
		if len(args) == 1 && args[0] == "--list-modules=full" {
			var b strings.Builder
			for _, m := range f.modules {
				dis := ""
				if !m.Enabled {
					dis = " disabled"
				}
				b.WriteString(strings.Join([]string{itoa(m.Priority), m.Name, m.Lang}, " ") + dis + "\n")
			}
			return []byte(b.String()), nil
		}
		prio := 400
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "-X":
				prio = atoi(args[i+1])
				i++
			case "-i":
				n := strings.TrimSuffix(filepath.Base(args[i+1]), ".cil")
				f.modules[n] = selinux.Module{Name: n, Priority: prio, Enabled: true, Lang: "cil"}
				i++
			case "-r":
				delete(f.modules, args[i+1])
				i++
			case "-e", "-d":
				m := f.modules[args[i+1]]
				m.Enabled = args[i] == "-e"
				f.modules[args[i+1]] = m
				i++
			}
		}
	case "semanage":
		if len(args) >= 2 && args[0] == "login" {
			switch args[1] {
			case "-l":
				var b strings.Builder
				for l, v := range f.logins {
					b.WriteString(l + " " + v.Seuser + " " + v.Range + " *\n")
				}
				return []byte(b.String()), nil
			case "-a", "-m":
				login := args[len(args)-1]
				l := selinux.Login{}
				for i := 2; i < len(args)-1; i++ {
					switch args[i] {
					case "-s":
						l.Seuser = args[i+1]
					case "-r":
						l.Range = args[i+1]
					}
				}
				f.logins[login] = l
			case "-d":
				delete(f.logins, args[2])
			}
		}
	}
	return nil, nil
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(n)) }
func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func (f *fakeSelinux) called(sub string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func selinuxEntry(id string, prio int32, p *pb.SelinuxPolicy) SelinuxEntry {
	return SelinuxEntry{ID: id, Name: "policy-" + id, Priority: prio, Policy: p}
}

func itemStatus(items []*pb.ComplianceItemResult, schema, key string) (status pb.ComplianceStatus, message string) {
	for _, it := range items {
		if it.GetSchemaId() == schema && it.GetKey() == key {
			return it.GetStatus(), it.GetMessage()
		}
	}
	return pb.ComplianceStatus_COMPLIANCE_STATUS_UNKNOWN, "missing"
}

func TestMergeSelinuxPolicies(t *testing.T) {
	a := selinuxEntry("a", 10, &pb.SelinuxPolicy{Mode: pb.SelinuxMode_SELINUX_MODE_PERMISSIVE, Booleans: []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: true}}, PermissiveDomains: []string{"x_t"}})
	b := selinuxEntry("b", 10, &pb.SelinuxPolicy{Mode: pb.SelinuxMode_SELINUX_MODE_ENFORCING, Booleans: []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: false}}, PermissiveDomains: []string{"y_t"}})
	c := selinuxEntry("c", 20, &pb.SelinuxPolicy{Mode: pb.SelinuxMode_SELINUX_MODE_PERMISSIVE, Booleans: []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: false}}})
	d := MergeSelinuxPolicies([]SelinuxEntry{a, b})
	if d.Mode != pb.SelinuxMode_SELINUX_MODE_ENFORCING {
		t.Error("equal priority: enforcing should win")
	}
	if d.Booleans["deny_ptrace"].Conflict == nil {
		t.Error("equal priority boolean conflict not recorded")
	}
	if len(d.Permissive) != 2 {
		t.Error("permissive domains not unioned")
	}
	d = MergeSelinuxPolicies([]SelinuxEntry{a, b, c})
	if d.Mode != pb.SelinuxMode_SELINUX_MODE_PERMISSIVE || d.ModeBy.ID != "c" {
		t.Error("higher priority mode did not win")
	}
	if d.Booleans["deny_ptrace"].Value || d.Booleans["deny_ptrace"].Conflict != nil || d.Booleans["deny_ptrace"].By.ID != "c" {
		t.Error("higher priority boolean did not win cleanly")
	}
}

func TestRenderCIL(t *testing.T) {
	got := string(RenderPermissiveCIL([]string{"b_t", "a_t"}))
	if !strings.Contains(got, "(typepermissive a_t)\n(typepermissive b_t)\n") {
		t.Errorf("permissive CIL:\n%s", got)
	}
	got = string(RenderFileContextsCIL([]*selinuxFContextDesired{{PathRegex: "/opt/corpapp(/.*)?", Type: "bin_t", Kind: pb.SelinuxFileKind_SELINUX_FILE_KIND_UNSPECIFIED}, {PathRegex: "/opt/corpapp/etc(/.*)?", Type: "etc_t", Kind: pb.SelinuxFileKind_SELINUX_FILE_KIND_DIR}}))
	if !strings.Contains(got, `(filecon "/opt/corpapp(/.*)?" any (system_u object_r bin_t ((s0) (s0))))`) || !strings.Contains(got, `(filecon "/opt/corpapp/etc(/.*)?" dir (system_u object_r etc_t ((s0) (s0))))`) {
		t.Errorf("filecon CIL:\n%s", got)
	}
}

func TestSyncSelinuxInactive(t *testing.T) {
	f := newFakeSelinux(t)
	pol := &pb.SelinuxPolicy{Mode: pb.SelinuxMode_SELINUX_MODE_ENFORCING, RequireActive: true}
	res := SyncSelinux(context.Background(), &f.deps, []SelinuxEntry{selinuxEntry("a", 10, pol)}, f.opts)
	if res.Active {
		t.Fatal("reported active")
	}
	if res.Policies["a"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(res.Policies["a"].Message, "not active") {
		t.Errorf("required: %+v", res.Policies["a"])
	}
	pol.RequireActive = false
	res = SyncSelinux(context.Background(), &f.deps, []SelinuxEntry{selinuxEntry("a", 10, pol)}, f.opts)
	if res.Policies["a"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("optional: %+v", res.Policies["a"])
	}
	pol.PrepareDisabledNodes = true
	res = SyncSelinux(context.Background(), &f.deps, []SelinuxEntry{selinuxEntry("a", 10, pol)}, f.opts)
	cfg, _ := f.deps.Paths.ReadConfig()
	if cfg.Mode != "permissive" || !f.deps.Paths.RelabelPending() || !strings.Contains(res.Policies["a"].Message, "reboot") {
		t.Errorf("prepare: cfg=%+v relabel=%v msg=%s", cfg, f.deps.Paths.RelabelPending(), res.Policies["a"].Message)
	}
}

func TestSyncSelinuxApplyAndRestore(t *testing.T) {
	f := newFakeSelinux(t)
	f.activate(false, "permissive")
	pol := &pb.SelinuxPolicy{
		Mode:              pb.SelinuxMode_SELINUX_MODE_ENFORCING,
		Booleans:          []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: true}, {Name: "nonexistent_bool", Value: true}},
		PermissiveDomains: []string{"virt_qemu_ga_t"},
		Modules:           []*pb.SelinuxModule{{Name: "zos", Enabled: false}, {Name: "missing", Enabled: false}},
		CustomModules:     []*pb.SelinuxCustomModule{{Name: "corp", Cil: "(allow a_t b_t (file (read)))"}},
		FileContexts:      []*pb.SelinuxFileContext{{PathRegex: "/opt/corpapp(/.*)?", Type: "bin_t", Restorecon: true}},
		Logins:            []*pb.SelinuxLogin{{Login: "kiosk", Seuser: "user_u", Range: "s0"}},
	}
	entries := []SelinuxEntry{selinuxEntry("a", 10, pol)}
	res := SyncSelinux(context.Background(), &f.deps, entries, f.opts)
	r := res.Policies["a"]
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Fatalf("status %s: %s", r.Status, r.Message)
	}
	if !res.Changed {
		t.Error("changed not reported")
	}
	enf, _ := f.deps.Paths.Enforcing()
	cfg, _ := f.deps.Paths.ReadConfig()
	if !enf || cfg.Mode != "enforcing" {
		t.Error("mode not applied")
	}
	if !f.called("setsebool -P deny_ptrace=on") {
		t.Errorf("setsebool not called: %v", f.calls)
	}
	if st, msg := itemStatus(r.Items, selinuxItemBoolean, "nonexistent_bool"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("nonexistent boolean: %s %s", st, msg)
	}
	if st, _ := itemStatus(r.Items, selinuxItemModule, "missing"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Error("missing module should be inapplicable")
	}
	if m := f.modules["zos"]; m.Enabled {
		t.Error("zos not disabled")
	}
	for _, name := range []string{"bor-permissive", "bor-filecontexts", "bor-corp"} {
		if m, ok := f.modules[name]; !ok || m.Priority != 450 {
			t.Errorf("module %s not installed at 450: %+v", name, m)
		}
		if _, err := os.Stat(filepath.Join(f.opts.ModulesDir, name+".cil")); err != nil {
			t.Errorf("source %s missing", name)
		}
	}
	if !f.called("restorecon -R /opt/corpapp") {
		t.Errorf("restorecon not called: %v", f.calls)
	}
	if l := f.logins["kiosk"]; l.Seuser != "user_u" {
		t.Error("login not mapped")
	}
	// One semodule run for every module operation.
	semoduleRuns := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "semodule -X") {
			semoduleRuns++
		}
	}
	if semoduleRuns != 1 {
		t.Errorf("expected one semodule commit, got %d: %v", semoduleRuns, f.calls)
	}
	state, _ := LoadSelinuxState(f.opts.StatePath)
	if state.Mode == nil || state.Mode.ConfigMode != "permissive" || state.Booleans["deny_ptrace"] != false || state.Modules["zos"] != true || !state.Logins["kiosk"].Existed == false {
		t.Errorf("state: %+v", state)
	}

	// A second sync is idempotent.
	f.calls = nil
	res = SyncSelinux(context.Background(), &f.deps, entries, f.opts)
	if res.Changed || f.called("setsebool") || f.called("semodule -X") {
		t.Errorf("second sync changed things: %v", f.calls)
	}

	// Drift: a manual setenforce 0 and setsebool are corrected and reported
	// on a reconcile pass.
	_ = f.deps.Paths.SetEnforcing(false)
	f.setBool("deny_ptrace", false)
	opts := f.opts
	opts.Reconcile = true
	res = SyncSelinux(context.Background(), &f.deps, entries, opts)
	if len(res.Drifts) != 2 {
		t.Errorf("expected 2 drifts, got %+v", res.Drifts)
	}
	enf, _ = f.deps.Paths.Enforcing()
	if !enf {
		t.Error("drifted mode not corrected")
	}

	// Unbind: everything returns to the recorded state.
	f.calls = nil
	res = SyncSelinux(context.Background(), &f.deps, nil, f.opts)
	enf, _ = f.deps.Paths.Enforcing()
	cfg, _ = f.deps.Paths.ReadConfig()
	if enf || cfg.Mode != "permissive" {
		t.Error("mode not restored")
	}
	if !f.called("setsebool -P deny_ptrace=off") {
		t.Errorf("boolean not restored: %v", f.calls)
	}
	if !f.modules["zos"].Enabled {
		t.Error("zos not re-enabled")
	}
	for _, name := range []string{"bor-permissive", "bor-filecontexts", "bor-corp"} {
		if _, ok := f.modules[name]; ok {
			t.Errorf("module %s not removed", name)
		}
	}
	if _, ok := f.logins["kiosk"]; ok {
		t.Error("login not removed")
	}
	if files, _ := ListBorSelinuxSources(f.opts.ModulesDir); len(files) != 0 {
		t.Errorf("sources left: %v", files)
	}
	if _, err := os.Stat(f.opts.StatePath); !os.IsNotExist(err) {
		t.Error("state file not removed")
	}
}

func TestSyncSelinuxConflictsAndOverrides(t *testing.T) {
	f := newFakeSelinux(t)
	f.activate(true, "enforcing")
	a := selinuxEntry("a", 10, &pb.SelinuxPolicy{Booleans: []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: true}}})
	b := selinuxEntry("b", 10, &pb.SelinuxPolicy{Booleans: []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: false}}})
	c := selinuxEntry("c", 5, &pb.SelinuxPolicy{Booleans: []*pb.SelinuxBoolean{{Name: "use_nfs_home_dirs", Value: true}}, Mode: pb.SelinuxMode_SELINUX_MODE_PERMISSIVE})
	d := selinuxEntry("d", 20, &pb.SelinuxPolicy{Mode: pb.SelinuxMode_SELINUX_MODE_ENFORCING})
	res := SyncSelinux(context.Background(), &f.deps, []SelinuxEntry{a, b, c, d}, f.opts)
	if res.Policies["a"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR || !strings.Contains(res.Policies["a"].Message, "conflicting") {
		t.Errorf("conflict: %+v", res.Policies["a"])
	}
	if f.called("deny_ptrace") {
		t.Error("conflicting boolean was applied")
	}
	if st, msg := itemStatus(res.Policies["c"].Items, selinuxItemMode, "mode"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE || !strings.Contains(msg, "overridden") {
		t.Errorf("override: %s %s", st, msg)
	}
	if res.Policies["c"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("c should be compliant on its boolean: %+v", res.Policies["c"])
	}
	if res.Policies["d"].Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("d: %+v", res.Policies["d"])
	}
}

func TestSyncSelinuxToolFailure(t *testing.T) {
	f := newFakeSelinux(t)
	f.activate(true, "enforcing")
	f.failTool = "semodule"
	pol := &pb.SelinuxPolicy{PermissiveDomains: []string{"x_t"}, Booleans: []*pb.SelinuxBoolean{{Name: "deny_ptrace", Value: true}}}
	res := SyncSelinux(context.Background(), &f.deps, []SelinuxEntry{selinuxEntry("a", 10, pol)}, f.opts)
	r := res.Policies["a"]
	if r.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Fatalf("status %s: %s", r.Status, r.Message)
	}
	if st, _ := itemStatus(r.Items, selinuxItemPermissive, "x_t"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Error("permissive item should be an error")
	}
	if st, _ := itemStatus(r.Items, selinuxItemBoolean, "deny_ptrace"); st != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Error("boolean should still be compliant")
	}
}

func TestScanSelinuxInventory(t *testing.T) {
	f := newFakeSelinux(t)
	f.activate(true, "enforcing")
	inv := ScanSelinuxInventory(context.Background(), &f.deps)
	if !inv.Status.Active || inv.Status.Mode != "enforcing" || inv.Status.ConfigMode != "enforcing" || inv.Status.PolicyType != "targeted" || inv.Status.PolicyVersion != 35 {
		t.Errorf("status: %+v", inv.Status)
	}
	if len(inv.Booleans) != 3 || len(inv.Modules) != 2 || inv.Status.BooleanCount != 3 {
		t.Errorf("inventory: %d booleans %d modules", len(inv.Booleans), len(inv.Modules))
	}
}
