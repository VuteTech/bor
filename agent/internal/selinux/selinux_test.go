// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package selinux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	p := Paths{
		SysFS:        filepath.Join(dir, "sys"),
		Config:       filepath.Join(dir, "etc", "config"),
		SemanageConf: filepath.Join(dir, "etc", "semanage.conf"),
		Autorelabel:  filepath.Join(dir, ".autorelabel"),
	}
	_ = os.MkdirAll(filepath.Join(p.SysFS, "booleans"), 0o755)
	_ = os.MkdirAll(filepath.Dir(p.Config), 0o755)
	return p
}

func TestStateReads(t *testing.T) {
	p := tempPaths(t)
	if p.Active() {
		t.Error("active without enforce file")
	}
	_ = os.WriteFile(filepath.Join(p.SysFS, "enforce"), []byte("1\n"), 0o600)
	_ = os.WriteFile(filepath.Join(p.SysFS, "policyvers"), []byte("35"), 0o600)
	_ = os.WriteFile(filepath.Join(p.SysFS, "booleans", "deny_ptrace"), []byte("0 1"), 0o600)
	_ = os.WriteFile(filepath.Join(p.SysFS, "booleans", "secure_mode_policyload"), []byte("0 0"), 0o600)
	if !p.Active() || p.PolicyVersion() != 35 {
		t.Error("active/version wrong")
	}
	on, _ := p.Enforcing()
	if !on {
		t.Error("expected enforcing")
	}
	if err := p.SetEnforcing(false); err != nil {
		t.Fatal(err)
	}
	if on, _ = p.Enforcing(); on {
		t.Error("expected permissive after SetEnforcing(false)")
	}
	bools, err := p.Booleans()
	if err != nil || bools["deny_ptrace"].Current || !bools["deny_ptrace"].Pending {
		t.Errorf("booleans: %v %v", bools, err)
	}
	if p.PolicyloadLocked() {
		t.Error("policyload reported locked")
	}
}

func TestConfig(t *testing.T) {
	p := tempPaths(t)
	cfg, err := p.ReadConfig()
	if err != nil || cfg.Mode != "" {
		t.Fatalf("missing config: %+v %v", cfg, err)
	}
	_ = os.WriteFile(p.Config, []byte("# comment\nSELINUX=enforcing\nSELINUXTYPE=targeted\n"), 0o644)
	cfg, _ = p.ReadConfig()
	if cfg.Mode != "enforcing" || cfg.Type != "targeted" {
		t.Errorf("config: %+v", cfg)
	}
	changed, err := p.WriteConfigMode("permissive")
	if err != nil || !changed {
		t.Fatalf("write: %v %v", changed, err)
	}
	data, _ := os.ReadFile(p.Config)
	if string(data) != "# comment\nSELINUX=permissive\nSELINUXTYPE=targeted\n" {
		t.Errorf("rewritten config:\n%s", data)
	}
	if changed, _ = p.WriteConfigMode("permissive"); changed {
		t.Error("second write reported a change")
	}
	if _, err := p.WriteConfigMode("disabled"); err == nil {
		t.Error("disabled accepted")
	}
	_ = os.WriteFile(p.Config, []byte("SELINUXTYPE=targeted\n"), 0o644)
	_, _ = p.WriteConfigMode("enforcing")
	data, _ = os.ReadFile(p.Config)
	if !strings.Contains(string(data), "SELINUX=enforcing\n") {
		t.Errorf("mode not appended: %s", data)
	}
}

func TestStoreRootAndRelabel(t *testing.T) {
	p := tempPaths(t)
	if p.StoreRoot() != "/var/lib/selinux" {
		t.Error("default store root")
	}
	_ = os.WriteFile(p.SemanageConf, []byte("module-store = direct\nstore-root=/etc/selinux\n"), 0o644)
	if p.StoreRoot() != "/etc/selinux" {
		t.Errorf("store root: %s", p.StoreRoot())
	}
	if p.RelabelPending() {
		t.Error("relabel pending on empty dir")
	}
	if err := p.RequestRelabel(); err != nil || !p.RelabelPending() {
		t.Error("relabel request failed")
	}
}

func TestParseModuleList(t *testing.T) {
	out := []byte("400 extra_binsbin                          cil         \n200 container                              pp          \n100 zosremote                              pp  disabled\n450 bor-spike                              cil\n")
	mods := ParseModuleList(out)
	if len(mods) != 4 || mods[2].Name != "zosremote" || mods[2].Enabled || mods[2].Priority != 100 || mods[3].Priority != 450 || mods[0].Lang != "cil" {
		t.Errorf("modules: %+v", mods)
	}
}

func TestToolsBatchingAndRetry(t *testing.T) {
	var calls [][]string
	attempts := 0
	tools := &Tools{SetseboolBin: "setsebool", SemoduleBin: "semodule", RestoreconBin: "restorecon", LockRetries: 3, RetryBase: time.Millisecond, Sleep: func(time.Duration) {}}
	tools.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "setsebool" {
			attempts++
			if attempts < 3 {
				return nil, errors.New("exit status 255")
			}
		}
		return nil, nil
	}
	if err := tools.SetBooleans(context.Background(), map[string]bool{"deny_ptrace": true, "abrt_anon_write": false}); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	last := calls[len(calls)-1]
	if strings.Join(last, " ") != "setsebool -P abrt_anon_write=off deny_ptrace=on" {
		t.Errorf("setsebool argv: %v", last)
	}
	ops := ModuleOps{Priority: 450, Install: []string{"/etc/bor/selinux/bor-a.cil"}, Remove: []string{"bor-b"}, Enable: []string{"x"}, Disable: []string{"y"}}
	if err := tools.ApplyModules(context.Background(), &ops); err != nil {
		t.Fatal(err)
	}
	last = calls[len(calls)-1]
	if strings.Join(last, " ") != "semodule -X 450 -i /etc/bor/selinux/bor-a.cil -r bor-b -e x -d y" {
		t.Errorf("semodule argv: %v", last)
	}
	tools.Run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("libsemanage.semanage_direct_remove_key: Unable to remove module bor-b at priority 400.\nsemodule:  Failed!"), errors.New("exit status 1")
	}
	err := tools.ApplyModules(context.Background(), &ModuleOps{Priority: 450, Remove: []string{"bor-b"}})
	if err == nil || !strings.Contains(err.Error(), "Failed!") {
		t.Errorf("non-busy error not surfaced: %v", err)
	}
}

func TestParseLoginList(t *testing.T) {
	out := []byte("__default__          unconfined_u         s0-s0:c0.c1023       *\nroot                 unconfined_u         s0-s0:c0.c1023       *\nkiosk                user_u               s0                   *\n")
	logins := ParseLoginList(out)
	if len(logins) != 3 || logins["kiosk"].Seuser != "user_u" || logins["kiosk"].Range != "s0" {
		t.Errorf("logins: %+v", logins)
	}
}
