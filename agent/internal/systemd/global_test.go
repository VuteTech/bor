// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalUnitsFileOperations(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib")
	etc := filepath.Join(dir, "etc")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(lib, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("syncthing.service", "[Unit]\nDescription=Syncthing\n[Service]\nExecStart=/usr/bin/syncthing\n[Install]\nWantedBy=default.target\nAlias=sync.service\n")
	write("backup.timer", "[Timer]\nOnCalendar=daily\n[Install]\nWantedBy=timers.target\n")
	write("helper.service", "[Service]\nExecStart=/usr/bin/helper\n")
	g := GlobalUnits{EtcDir: etc, LibDirs: []string{lib}}

	if st := g.State("syncthing.service"); st != GlobalStateDisabled {
		t.Fatalf("fresh unit state = %q", st)
	}
	if st := g.State("helper.service"); st != GlobalStateStatic {
		t.Errorf("unit without [Install] = %q, want static", st)
	}
	if st := g.State("missing.service"); st != GlobalStateNotFound {
		t.Errorf("missing unit = %q", st)
	}
	if err := g.Enable("helper.service"); err != ErrNoInstall {
		t.Errorf("Enable(static) = %v, want ErrNoInstall", err)
	}

	if err := g.Enable("syncthing.service"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	for _, link := range []string{filepath.Join(etc, "default.target.wants", "syncthing.service"), filepath.Join(etc, "sync.service")} {
		target, err := os.Readlink(link)
		if err != nil || target != filepath.Join(lib, "syncthing.service") {
			t.Errorf("link %s -> %q, %v", link, target, err)
		}
	}
	if st := g.State("syncthing.service"); st != GlobalStateEnabled {
		t.Errorf("after Enable = %q", st)
	}
	if err := g.Enable("syncthing.service"); err != nil {
		t.Errorf("Enable twice: %v", err)
	}
	if err := g.Disable("syncthing.service"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if st := g.State("syncthing.service"); st != GlobalStateDisabled {
		t.Errorf("after Disable = %q", st)
	}
	if _, err := os.Stat(filepath.Join(etc, "default.target.wants")); !os.IsNotExist(err) {
		t.Error("an emptied .wants directory must be removed")
	}

	if err := g.Enable("backup.timer"); err != nil {
		t.Fatal(err)
	}
	if err := g.Mask("backup.timer"); err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(etc, "backup.timer")); err != nil || target != os.DevNull {
		t.Errorf("mask link -> %q, %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(etc, "timers.target.wants", "backup.timer")); !os.IsNotExist(err) {
		t.Error("Mask must drop the enablement link")
	}
	if st := g.State("backup.timer"); st != GlobalStateMasked {
		t.Errorf("after Mask = %q", st)
	}
	if err := g.Mask("backup.timer"); err != nil {
		t.Errorf("Mask twice: %v", err)
	}
	if err := g.Unmask("backup.timer"); err != nil {
		t.Fatalf("Unmask: %v", err)
	}
	if st := g.State("backup.timer"); st != GlobalStateDisabled {
		t.Errorf("after Unmask = %q", st)
	}

	// A unit whose own file lives in the etc directory cannot be masked.
	if err := os.WriteFile(filepath.Join(etc, "local.service"), []byte("[Service]\nExecStart=/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.Mask("local.service"); err == nil {
		t.Error("masking over a real unit file must fail")
	}
	if err := g.Unmask("local.service"); err == nil {
		t.Error("unmasking a real unit file must fail")
	}
	if err := g.Mask("../evil.service"); err == nil {
		t.Error("invalid names must be refused")
	}
}

func TestParseSecurityScores(t *testing.T) {
	out := []byte(`[{"unit":"cups.service","exposure":"9.6","predicate":"UNSAFE","happy":"x"},{"unit":"bor-agent.service","exposure":"1.2","predicate":"OK","happy":"y"},{"unit":"bad.service","exposure":"9.6; rm","predicate":"UNSAFE"},{"unit":"","exposure":"1","predicate":"OK"}]`)
	scores, err := ParseSecurityScores(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores["cups.service"].Predicate != "UNSAFE" || scores["bor-agent.service"].Exposure != "1.2" {
		t.Errorf("scores = %+v", scores)
	}
	if _, err := ParseSecurityScores([]byte("UNIT EXPOSURE PREDICATE HAPPY\ncups.service 9.6 UNSAFE")); err == nil {
		t.Error("table output must be rejected")
	}
}

func TestParseJournalActors(t *testing.T) {
	out := []byte(`{"MESSAGE":"   alice : TTY=pts/0 ; PWD=/home/alice ; USER=root ; COMMAND=/usr/bin/systemctl stop bluetooth.service","_PID":"4242","_COMM":"sudo"}
{"MESSAGE":"Operator of unix-session:3 successfully authenticated as unix-user:bob to gain ONE-SHOT authorization for action org.freedesktop.systemd1.manage-units for system-bus-name::1.9 [systemctl stop bluetooth.service] (owned by unix-user:bob)","_PID":"777","_COMM":"polkitd"}
{"MESSAGE":"   alice : TTY=pts/0 ; PWD=/home/alice ; USER=root ; COMMAND=/usr/bin/systemctl stop bluetooth.service","_PID":"4243","_COMM":"sudo"}
{"MESSAGE":"   carol : TTY=pts/1 ; PWD=/home/carol ; USER=root ; COMMAND=/usr/bin/systemctl stop cups.service","_PID":"4300","_COMM":"sudo"}
{"MESSAGE":[1,2,3],"_PID":"1","_COMM":"sudo"}
not json`)
	actors := ParseJournalActors(out, "bluetooth.service")
	if len(actors) != 2 {
		t.Fatalf("actors = %+v", actors)
	}
	if actors[0].User != "alice" || actors[0].Comm != "sudo" || actors[0].PID != 4242 {
		t.Errorf("first actor = %+v", actors[0])
	}
	if actors[1].User != "bob" || actors[1].Comm != "polkitd" || actors[1].PID != 777 {
		t.Errorf("second actor = %+v", actors[1])
	}
	if got := ParseJournalActors(out, "sshd.service"); len(got) != 0 {
		t.Errorf("unrelated unit: %+v", got)
	}
}

func TestParseUnitFileInstall(t *testing.T) {
	info := ParseUnitFile(stringsReader("[Unit]\nDescription=x\n[Install]\nWantedBy=default.target graphical-session.target\nWantedBy=timers.target\nRequiredBy=foo.target\nAlias=a.service\n"))
	if !info.HasInstall || len(info.WantedBy) != 3 || info.WantedBy[2] != "timers.target" || len(info.RequiredBy) != 1 || len(info.Alias) != 1 {
		t.Errorf("install = %+v", info)
	}
	reset := ParseUnitFile(stringsReader("[Install]\nWantedBy=default.target\nWantedBy=\n"))
	if len(reset.WantedBy) != 0 {
		t.Errorf("an empty assignment must reset the list: %+v", reset.WantedBy)
	}
}

func TestVerifyUserUnitEnvironment(t *testing.T) {
	var gotEnv, gotArgs []string
	a := &Analyzer{
		Binary: "systemd-analyze",
		Run: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("RunEnv must be used when set")
			return nil, nil
		},
		RunEnv: func(_ context.Context, env []string, _ string, args ...string) ([]byte, error) {
			gotEnv, gotArgs = env, args
			return nil, nil
		},
	}
	if err := a.VerifyUserUnit(context.Background(), "/etc/systemd/user/bor-x.service"); err != nil {
		t.Fatal(err)
	}
	if len(gotEnv) != 1 || gotEnv[0] != "XDG_RUNTIME_DIR=/run" {
		t.Errorf("env = %v", gotEnv)
	}
	if len(gotArgs) != 4 || gotArgs[0] != "--user" || gotArgs[1] != "verify" || gotArgs[3] != "/etc/systemd/user/bor-x.service" {
		t.Errorf("args = %v", gotArgs)
	}
	// Without RunEnv the plain runner serves user verification too.
	plain := &Analyzer{Binary: "systemd-analyze", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return nil, nil
	}}
	if err := plain.VerifyUserUnit(context.Background(), "/x.service"); err != nil || gotArgs[0] != "--user" {
		t.Errorf("fallback: %v %v", err, gotArgs)
	}
}
