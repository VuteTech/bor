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
	for _, s := range []string{"", "abc", "15 minutes ago", "1h;rm", "-5s", "1h\n", "5 lightyears"} {
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
