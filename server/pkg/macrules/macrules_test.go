// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package macrules

import (
	"strings"
	"testing"
)

func TestValidateCustomName(t *testing.T) {
	for _, ok := range []string{"corpapp", "corp-app.v2", "a", "x_y"} {
		if err := ValidateCustomName(ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Corp", "bor-x", "-x", "a b", "a/b", "..", strings.Repeat("a", 65)} {
		if err := ValidateCustomName(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestSelinuxNames(t *testing.T) {
	if err := ValidateBooleanName("deny_ptrace"); err != nil {
		t.Error(err)
	}
	if err := ValidateBooleanName("Deny"); err == nil {
		t.Error("uppercase boolean accepted")
	}
	if err := ValidateDomain("httpd_t"); err != nil {
		t.Error(err)
	}
	if err := ValidateDomain("httpd"); err == nil {
		t.Error("type without _t accepted")
	}
	if err := ValidateSeuser("user_u"); err != nil {
		t.Error(err)
	}
	if err := ValidateSeuser("system_u"); err == nil {
		t.Error("system_u accepted")
	}
	if err := ValidateLogin("%students"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"__default__", "root", "a b"} {
		if err := ValidateLogin(bad); err == nil {
			t.Errorf("login %q accepted", bad)
		}
	}
	for _, ok := range []string{"", "s0", "s0-s0:c0.c1023", "s0:c1,c5.c7"} {
		if err := ValidateRange(ok); err != nil {
			t.Errorf("range %q: %v", ok, err)
		}
	}
	if err := ValidateRange("x"); err == nil {
		t.Error("bad range accepted")
	}
}

func TestFileContextRegex(t *testing.T) {
	prefix, err := ValidateFileContextRegex(`/opt/corpapp(/.*)?`)
	if err != nil || prefix != "/opt/corpapp" {
		t.Errorf("got %q, %v", prefix, err)
	}
	prefix, err = ValidateFileContextRegex(`/srv/www/site[0-9]+/data`)
	if err != nil || prefix != "/srv/www/" {
		t.Errorf("got %q, %v", prefix, err)
	}
	for _, bad := range []string{"", "opt/x", "/", "/usr(/.*)?", "/etc/.*", "/opt/(", "/home/.*"} {
		if _, err := ValidateFileContextRegex(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRisky(t *testing.T) {
	if !IsRiskyBoolean("unconfined_login", false) || IsRiskyBoolean("unconfined_login", true) {
		t.Error("unconfined_login risk wrong")
	}
	if !IsRiskyBoolean("xguest_use_bluetooth", true) {
		t.Error("xguest not risky")
	}
	if !IsRiskyBoolean("use_nfs_home_dirs", false) || IsRiskyBoolean("use_nfs_home_dirs", true) {
		t.Error("home dirs risk wrong")
	}
	if !IsProtectedBoolean("secure_mode_policyload") {
		t.Error("policyload not protected")
	}
	if !IsRiskyModule("unconfined") || IsRiskyModule("abrt") {
		t.Error("module risk wrong")
	}
}

func TestCheckCIL(t *testing.T) {
	valid := []string{
		"(typepermissive virt_qemu_ga_t)",
		`(filecon "/opt/corpapp(/.*)?" any (system_u object_r bin_t ((s0) (s0))))`,
		"; comment\n(allow corpapp_t etc_t (file (read open getattr)))\n(dontaudit corpapp_t self (capability (sys_ptrace)))",
		"(optional corp (allow a_t b_t (file (read))))",
		"(block corp (type corp_t) (roletype system_r corp_t))",
		"(booleanif deny_ptrace (true (allow a_t b_t (process (ptrace)))))",
	}
	for _, v := range valid {
		if err := CheckCIL(v); err != nil {
			t.Errorf("%q: %v", v, err)
		}
	}
	invalid := map[string]string{
		"empty":        "",
		"unbalanced":   "(allow a_t b_t (file (read))",
		"extra close":  "(allow a_t b_t (file (read))))",
		"loose text":   "allow a_t b_t",
		"class":        "(class file (read))",
		"sid":          "(sid kernel)",
		"user":         "(user corp_u)",
		"policycap":    "(policycap open_perms)",
		"nul":          "(allow a_t b_t (file (read)))\x00",
		"unterminated": `(filecon "/opt any (system_u object_r bin_t ((s0) (s0))))`,
		"too big":      "(" + strings.Repeat("allow a_t b_t (file (read))\n", 4000) + ")",
	}
	for name, v := range invalid {
		if err := CheckCIL(v); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestProfileMatch(t *testing.T) {
	for _, ok := range []string{"usr.sbin.cupsd", "firefox", "usr.sbin.*", "*", "snap.?"} {
		if err := ValidateProfileMatch(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "local", "abi", "../x", "a/b", "usr.bin.man.dpkg-new", "x~", "[a]"} {
		if err := ValidateProfileMatch(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if !MatchProfileFile("usr.sbin.*", "usr.sbin.cupsd") || MatchProfileFile("usr.sbin.*", "usr.bin.man") || !MatchProfileFile("firefox", "firefox") {
		t.Error("glob matching wrong")
	}
	if GlobSpecificity("usr.sbin.*") <= GlobSpecificity("*") {
		t.Error("specificity wrong")
	}
}

func TestManagedBy(t *testing.T) {
	cases := map[[2]string]string{
		{"snap.firefox.firefox", ""}:          "snapd",
		{"/snap/snapd/1/usr/lib/snapd/x", ""}: "snapd",
		{"docker-default", ""}:                "docker",
		{"lxd-c1_</var/lib/lxd>", ""}:         "lxd",
		{"libvirt-abc", ""}:                   "libvirt",
		{"bor-corpapp", "bor-corpapp"}:        "bor",
		{"/usr/sbin/cupsd", "usr.sbin.cupsd"}: "vendor",
	}
	for in, want := range cases {
		if got := ManagedBy(in[0], in[1]); got != want {
			t.Errorf("%v: got %q want %q", in, got, want)
		}
	}
	if !IsDynamicManaged("snapd") || IsDynamicManaged("vendor") || IsDynamicManaged("bor") {
		t.Error("dynamic classification wrong")
	}
}

func TestCheckProfileText(t *testing.T) {
	good := "abi <abi/4.0>,\ninclude <tunables/global>\n\n# stub\nprofile bor-corpapp /opt/corpapp/bin/app flags=(unconfined) {\n  userns,\n  include if exists <local/bor-corpapp>\n}\n"
	if err := CheckProfileText("corpapp", good); err != nil {
		t.Errorf("good profile: %v", err)
	}
	withChild := "profile bor-corpapp /opt/x {\n  profile helper /opt/y {\n    /etc/** r,\n  }\n}\n"
	if err := CheckProfileText("corpapp", withChild); err != nil {
		t.Errorf("child profile: %v", err)
	}
	bad := map[string]string{
		"wrong name":  "profile bor-other /opt/x {\n}\n",
		"no profile":  "abi <abi/4.0>,\n",
		"bare attach": "/opt/x {\n}\n",
		"two":         "profile bor-corpapp /opt/x {\n}\nprofile bor-corpapp /opt/y {\n}\n",
		"unbalanced":  "profile bor-corpapp /opt/x {\n",
		"loose":       "profile bor-corpapp /opt/x {\n}\nallow all,\n",
	}
	for name, text := range bad {
		if err := CheckProfileText("corpapp", text); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := CheckLocalOverrideText("  /nfs/home/** rwk,\n  priority=1 /etc/hostname r,\n"); err != nil {
		t.Errorf("override: %v", err)
	}
	if err := CheckLocalOverrideText("}"); err == nil {
		t.Error("unbalanced override accepted")
	}
}

func TestHomeDir(t *testing.T) {
	for _, ok := range []string{"/nfs/home/", "/export/users/"} {
		if err := ValidateHomeDir(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/", "nfs/home/", "/nfs/home", "/nfs/../home/", "/a b/"} {
		if err := ValidateHomeDir(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
