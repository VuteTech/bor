// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemdunits

import "testing"

func TestValidateName(t *testing.T) {
	valid := []string{
		"bluetooth.service", "fstrim.timer", "cups.socket", "ctrl-alt-del.target",
		"systemd-tmpfiles-clean.timer", "getty@tty1.service", "getty@.service",
		"cups.path", "systemd-fsck@dev-disk-by\\x2duuid-abc.service", "a:b.service",
	}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{
		"", ".", "..", ".service", "foo", "foo.mount", "foo.swap", "foo.slice",
		"foo.scope", "foo.device", "../x.service", "foo/bar.service", "foo bar.service",
		"foo\nbar.service", "foo.Service",
	}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
	long := make([]byte, 260)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateName(string(long) + ".service"); err == nil {
		t.Error("ValidateName(long) = nil, want error")
	}
}

func TestType(t *testing.T) {
	cases := map[string]string{
		"bluetooth.service": "service", "fstrim.timer": "timer", "cups.socket": "socket",
		"x.target": "target", "x.path": "path", "x.mount": "", "x": "", "x.service.bak": "",
	}
	for in, want := range cases {
		if got := Type(in); got != want {
			t.Errorf("Type(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTemplate(t *testing.T) {
	if !IsTemplate("getty@.service") {
		t.Error("getty@.service should be a template")
	}
	if IsTemplate("getty@tty1.service") || IsTemplate("getty.service") {
		t.Error("instances and plain units are not templates")
	}
}

func TestIsProtected(t *testing.T) {
	for _, n := range []string{
		"bor-agent.service", "dbus.service", "dbus-broker.service", "systemd-journald.service",
		"systemd-journald@foo.service", "systemd-udevd.service", "user@1000.service",
		"multi-user.target", "init.scope", "-.slice", "polkit.service",
	} {
		if !IsProtected(n) {
			t.Errorf("IsProtected(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"ctrl-alt-del.target", "bluetooth.service", "fstrim.timer", "userdbd.service", "NetworkManager.service"} {
		if IsProtected(n) {
			t.Errorf("IsProtected(%q) = true, want false", n)
		}
	}
}

func TestIsRisky(t *testing.T) {
	for _, n := range []string{"NetworkManager.service", "sshd.service", "gdm.service", "systemd-networkd.service", "systemd-resolved.service", "firewalld.service"} {
		if !IsRisky(n) {
			t.Errorf("IsRisky(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"bluetooth.service", "fstrim.timer", "avahi-daemon.service"} {
		if IsRisky(n) {
			t.Errorf("IsRisky(%q) = true, want false", n)
		}
	}
}

func TestValidateTimeSpan(t *testing.T) {
	for _, s := range []string{"15min", "1h 30min", "90s", "1.5h", "0", "300", "infinity", "2 weeks", "1h30min", "500ms"} {
		if err := ValidateTimeSpan(s); err != nil {
			t.Errorf("ValidateTimeSpan(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"", "abc", "15 minutes ago", "1h;rm", "-5s", "1h\n", "1h\t30min", "1.51.5", "5 lightyears"} {
		if err := ValidateTimeSpan(s); err == nil {
			t.Errorf("ValidateTimeSpan(%q) = nil, want error", s)
		}
	}
}

func TestValidateCalendar(t *testing.T) {
	for _, s := range []string{"daily", "Sat *-*-* 03:00:00", "Mon..Fri 07:00", "*-*-01 00:00:00", "hourly", "*:0/15", "Sat 2026-10-18 03:00 Europe/Sofia", "*-*~01"} {
		if err := ValidateCalendar(s); err != nil {
			t.Errorf("ValidateCalendar(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"", "daily; rm -rf /", "Sat\n03:00", "$(date)", "a|b"} {
		if err := ValidateCalendar(s); err == nil {
			t.Errorf("ValidateCalendar(%q) = nil, want error", s)
		}
	}
	long := make([]byte, MaxCalendarLength+1)
	for i := range long {
		long[i] = '*'
	}
	if err := ValidateCalendar(string(long)); err == nil {
		t.Error("ValidateCalendar(long) = nil, want error")
	}
}

func TestValidateTaskName(t *testing.T) {
	ok := []string{"nightly-cleanup", "a", "cache0", "x-1-2"}
	for _, n := range ok {
		if err := ValidateTaskName(n); err != nil {
			t.Errorf("ValidateTaskName(%q) = %v, want nil", n, err)
		}
	}
	bad := []string{"", "-lead", "Upper", "with space", "bor-prefixed", "a.b", "toolong-toolong-toolong-toolong-toolong-toolong-x", "../x"}
	for _, n := range bad {
		if err := ValidateTaskName(n); err == nil {
			t.Errorf("ValidateTaskName(%q) = nil, want error", n)
		}
	}
	if TaskServiceUnit("cleanup") != "bor-cleanup.service" || TaskTimerUnit("cleanup") != "bor-cleanup.timer" {
		t.Error("task unit names")
	}
	for unit, want := range map[string]bool{
		"bor-cleanup.service": true, "bor-cleanup.timer": true, "bor-x.socket": false,
		"cleanup.service": false, "bor-.service": false, "bor-Bad.service": false, "bor-a.service.timer": false,
	} {
		if got := IsTaskUnit(unit); got != want {
			t.Errorf("IsTaskUnit(%q) = %v, want %v", unit, got, want)
		}
	}
}

func TestValidateUserName(t *testing.T) {
	for _, n := range []string{"root", "_svc", "bor-task", "nobody"} {
		if err := ValidateUserName(n); err != nil {
			t.Errorf("ValidateUserName(%q) = %v", n, err)
		}
	}
	for _, n := range []string{"", "0user", "Root", "a b", "x;y", "toolongtoolongtoolongtoolongtoolong"} {
		if err := ValidateUserName(n); err == nil {
			t.Errorf("ValidateUserName(%q) = nil, want error", n)
		}
	}
}

func TestDirectives(t *testing.T) {
	if err := ValidateDirectiveSection("Service"); err != nil {
		t.Error(err)
	}
	if err := ValidateDirectiveSection("Mount"); err == nil {
		t.Error("Mount must be rejected")
	}
	for _, k := range []string{"IPAddressDeny", "ExecStartPre", "X"} {
		if err := ValidateDirectiveKey(k); err != nil {
			t.Errorf("ValidateDirectiveKey(%q) = %v", k, err)
		}
	}
	for _, k := range []string{"", "lower", "Has Space", "Key=", "A-B", "\n"} {
		if err := ValidateDirectiveKey(k); err == nil {
			t.Errorf("ValidateDirectiveKey(%q) = nil, want error", k)
		}
	}
	if !IsExecDirective("ExecStartPost") || IsExecDirective("Environment") {
		t.Error("IsExecDirective")
	}
	if err := ValidateDirectiveValue("a=b c"); err != nil {
		t.Error(err)
	}
	if err := ValidateDirectiveValue("a\nb"); err == nil {
		t.Error("newline must be rejected")
	}
}

func TestGlobalScopeLists(t *testing.T) {
	for _, name := range []string{"graphical-session.target", "dbus.service", "bor-agent.service", "timers.target"} {
		if !IsProtectedGlobal(name) {
			t.Errorf("IsProtectedGlobal(%q) = false", name)
		}
	}
	for _, name := range []string{"pipewire.service", "syncthing.service", "ssh-agent.service"} {
		if IsProtectedGlobal(name) {
			t.Errorf("IsProtectedGlobal(%q) = true", name)
		}
	}
	for _, name := range []string{"pipewire.service", "pipewire-pulse.socket", "plasma-kwin_wayland.service", "xdg-desktop-portal-kde.service", "sshd.service"} {
		if !IsRiskyGlobal(name) {
			t.Errorf("IsRiskyGlobal(%q) = false", name)
		}
	}
	if IsRiskyGlobal("syncthing.service") {
		t.Error("IsRiskyGlobal(syncthing.service) = true")
	}
}

func TestValidateUnitAction(t *testing.T) {
	ok := []struct{ unit, action string }{
		{"cups.service", "restart"}, {"fstrim.timer", "start"}, {"cups.socket", "stop"}, {"bor-cleanup.timer", "stop"},
	}
	for _, c := range ok {
		if err := ValidateUnitAction(c.unit, c.action); err != nil {
			t.Errorf("ValidateUnitAction(%q, %q) = %v", c.unit, c.action, err)
		}
	}
	bad := []struct{ unit, action string }{
		{"cups.service", "enable"}, {"cups.service", ""}, {"dbus.service", "restart"}, {"bor-agent.service", "stop"},
		{"multi-user.target", "start"}, {"getty@.service", "restart"}, {"../x.service", "start"}, {"x.mount", "start"},
	}
	for _, c := range bad {
		if err := ValidateUnitAction(c.unit, c.action); err == nil {
			t.Errorf("ValidateUnitAction(%q, %q) = nil, want error", c.unit, c.action)
		}
	}
}
