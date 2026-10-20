// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"7.2.11-100.fc43.x86_64", "6.17.1-300.fc43.x86_64", 1},
		{"7.0.0-34-generic", "7.0.0-38-generic", -1},
		{"6.12.101+deb13-amd64", "6.12.113+deb13-amd64", -1},
		{"7.2.11-1-default", "7.2.11-1-default", 0},
		{"1.0~rc1", "1.0", -1},
		{"1.10", "1.9", 1},
		{"6.12.101+deb13-amd64", "6.12.101+deb13-amd64", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if NewestKernel([]string{"7.0.0-34-generic", "7.0.0-38-generic", "7.0.0-15-generic"}) != "7.0.0-38-generic" {
		t.Error("NewestKernel picked the wrong version")
	}
	if !KernelRebootNeeded("7.0.0-34-generic", "7.0.0-38-generic") || KernelRebootNeeded("7.0.0-38-generic", "7.0.0-38-generic") || KernelRebootNeeded("", "x") {
		t.Error("KernelRebootNeeded wrong")
	}
}

func TestInstalledKernelsFromBoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "boot"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"vmlinuz-7.2.11-1-default", "vmlinuz-7.1.0-2-default", "vmlinuz-0-rescue-abc", "vmlinuz"} {
		if err := os.WriteFile(filepath.Join(root, "boot", n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := &Platform{Root: root, Engine: EngineOsUpdate}
	got := InstalledKernels(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("must not run packages")
		return nil, nil
	}, p)
	if len(got) != 2 || NewestKernel(got) != "7.2.11-1-default" {
		t.Errorf("InstalledKernels = %v", got)
	}
}

func TestRenderDnfAutomaticConf(t *testing.T) {
	sec := string(RenderDnfAutomaticConf(ModeSecurity, []string{"gnupg2*", "kernel*"}))
	for _, want := range []string{"# " + Header, "apply_updates = yes", "upgrade_type = security", "reboot = never", "[base]\nexcludepkgs = gnupg2* kernel*"} {
		if !strings.Contains(sec, want) {
			t.Errorf("security conf lacks %q:\n%s", want, sec)
		}
	}
	check := string(RenderDnfAutomaticConf(ModeCheck, nil))
	if !strings.Contains(check, "apply_updates = no") || strings.Contains(check, "[base]") {
		t.Errorf("check conf wrong:\n%s", check)
	}
	all := string(RenderDnfAutomaticConf(ModeAll, nil))
	if !strings.Contains(all, "apply_updates = yes\n") || !strings.Contains(all, "upgrade_type = default") {
		t.Errorf("all conf wrong:\n%s", all)
	}
}

func TestRenderAptConf(t *testing.T) {
	deb := string(RenderAptConf(ModeSecurity, "debian", []string{"linux-image-*", "apparmor"}, false))
	for _, want := range []string{
		"// " + Header,
		"#clear Unattended-Upgrade::Allowed-Origins;",
		"#clear Unattended-Upgrade::Origins-Pattern;",
		`"origin=Debian,codename=${distro_codename},label=Debian-Security";`,
		`"origin=Debian,codename=${distro_codename}-security,label=Debian-Security";`,
		`"^linux-image-.*$";`,
		`"^apparmor$";`,
		`Unattended-Upgrade::Automatic-Reboot "false";`,
		`APT::Periodic::Unattended-Upgrade "1";`,
	} {
		if !strings.Contains(deb, want) {
			t.Errorf("debian conf lacks %q:\n%s", want, deb)
		}
	}
	if strings.Contains(deb, "label=Debian\"") {
		t.Error("security-only must not include the plain Debian archive")
	}
	ubu := string(RenderAptConf(ModeAll, "ubuntu", nil, true))
	for _, want := range []string{`"origin=Ubuntu,archive=${distro_codename}-updates";`, `"origin=Ubuntu,archive=${distro_codename}-security";`, `Remove-Unused-Dependencies "true"`} {
		if !strings.Contains(ubu, want) {
			t.Errorf("ubuntu conf lacks %q", want)
		}
	}
	check := string(RenderAptConf(ModeCheck, "ubuntu", nil, false))
	if !strings.Contains(check, `APT::Periodic::Unattended-Upgrade "0";`) || strings.Contains(check, "Origins-Pattern {") {
		t.Errorf("check conf wrong:\n%s", check)
	}
	if HoldToRegex("lib+c.so?") != `^lib\+c\.so.$` {
		t.Errorf("HoldToRegex = %q", HoldToRegex("lib+c.so?"))
	}
}

func TestAptPendingAndLog(t *testing.T) {
	out := "Listing...\napparmor/resolute-updates 5.0.2-0ubuntu1~26.04.1 amd64 [upgradable from: 5.0.0~beta1-0ubuntu7]\n" +
		"bind9-host/resolute-security 1:9.20.29-1 amd64 [upgradable from: 1:9.20.26-1]\napache2-bin/stable-security 2.4.69-1~deb13u1 amd64 [upgradable from: 2.4.68-1~deb13u1]\n"
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte(out), nil }
	pending, err := AptPending(context.Background(), run, &Platform{AptGetBin: "/usr/bin/apt-get"})
	if err != nil || len(pending) != 3 {
		t.Fatalf("AptPending = %v, %v", pending, err)
	}
	if pending[0].Security || !pending[1].Security || !pending[2].Security || pending[1].Version != "1:9.20.29-1" {
		t.Errorf("security flags wrong: %+v", pending)
	}

	logPath := filepath.Join(t.TempDir(), "uu.log")
	log := "2026-10-20 02:02:41,448 INFO No packages found that can be upgraded unattended and no pending auto-removals\n" +
		"2026-10-20 12:52:47,100 INFO Starting unattended upgrades script\n" +
		"2026-10-20 12:52:47,961 INFO Packages that will be upgraded: apparmor bind9-host linux-image-7.0.0-38-generic\n" +
		"2026-10-20 12:55:09,400 INFO All upgrades installed\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	r := AptLastRun(logPath)
	if r == nil || !r.Success || r.Packages != 3 || r.Message != "All upgrades installed" || r.Time.Hour() != 12 {
		t.Errorf("AptLastRun = %+v", r)
	}
	failed := log + "2026-10-20 13:00:00,000 INFO Starting unattended upgrades script\n2026-10-20 13:00:00,100 ERROR Lock could not be acquired (another package manager running?)\n"
	if err := os.WriteFile(logPath, []byte(failed), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := AptLastRun(logPath); r == nil || r.Success || !strings.Contains(r.Message, "Lock") {
		t.Errorf("failed run not detected: %+v", r)
	}
}

func TestDnfParsers(t *testing.T) {
	checkOut := "Updating and loading repositories:\nRepositories loaded.\ngnupg2.x86_64                           2.4.9-5.fc43                  updates\n    gnupg2.x86_64                       2.4.8-4.fc43                  d15e55\nkernel-core.x86_64   7.2.11-100.fc43   updates\nObsoleting\nfoo.noarch 1-1 updates\n"
	advOut := `[{"name":"FEDORA-2026-1","type":"security","severity":"Important","nevra":"kernel-core-7.2.11-100.fc43.x86_64","buildtime":"x"},{"name":"FEDORA-2026-2","type":"security","severity":"Low","nevra":"kernel-core-7.2.11-100.fc43.x86_64","buildtime":"x"}]`
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "advisory") {
			return []byte(advOut), nil
		}
		return []byte(checkOut), &RunError{Err: &fakeExit{100}}
	}
	p := &Platform{DnfBin: "dnf5", DNF5: true}
	pending, err := DnfPending(context.Background(), run, p)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Pending{}
	for _, pe := range pending {
		byName[pe.Name] = pe
	}
	if len(pending) != 3 || !byName["kernel-core"].Security || byName["kernel-core"].Severity != "important" || byName["kernel-core"].Advisory != "FEDORA-2026-1" || byName["gnupg2"].Security {
		t.Errorf("DnfPending = %+v", pending)
	}
	hist := "ID Command line                           Date and time       Action(s) Altered\n17 dnf5 automatic                         2026-10-20 09:53:22               844\n16 dnf5 install -y dnf5-plugin-automatic  2026-10-20 09:51:24                12\n"
	runHist := func(context.Context, string, ...string) ([]byte, error) { return []byte(hist), nil }
	r := DnfLastRun(context.Background(), runHist, p)
	if r == nil || r.Packages != 844 || r.Time.Minute() != 53 {
		t.Errorf("DnfLastRun = %+v", r)
	}
	if nevraName("kf6-kcoreaddons-6.20.0-2.fc43.x86_64") != "kf6-kcoreaddons" {
		t.Error("nevraName wrong")
	}
}

type fakeExit struct{ code int }

func (f *fakeExit) Error() string { return "exit" }
func (f *fakeExit) ExitCode() int { return f.code }

func TestRpmOstreeStatus(t *testing.T) {
	data := `{"deployments":[{"version":"43.20261020.0","staged":true},{"version":"43.20260221.0","booted":true}],"transaction":null,
	"cached-update":{"version":"43.20261020.0","timestamp":1792456931,"advisories":[["FEDORA-2026-d6",1,2,["qt5-qtbase-5.15.18-2.fc43.x86_64","qt5-qtbase-gui-5.15.18-2.fc43.x86_64"],{"cve_references":[["https://bz","CVE-2026-19248 qt5-qtbase: DoS"]]}],["FEDORA-2026-b",2,0,["coreutils-9.7-10.fc43.x86_64"],{}]]}}`
	st, err := ParseRpmOstreeStatus([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if st.Booted != "43.20260221.0" || st.Staged != "43.20261020.0" || st.Transaction || len(st.Pending) != 3 {
		t.Errorf("status = %+v", st)
	}
	var qt Pending
	for _, p := range st.Pending {
		if p.Name == "qt5-qtbase" {
			qt = p
		}
	}
	if !qt.Security || qt.Severity != "moderate" || len(qt.CVEs) != 1 || qt.CVEs[0] != "CVE-2026-19248" {
		t.Errorf("qt5 advisory wrong: %+v", qt)
	}
	conf := []byte("# Entries\n[Daemon]\n#AutomaticUpdatePolicy=none\n#IdleExitTimeout=60\n")
	out := SetRpmOstreePolicy(conf, "stage")
	if RpmOstreePolicy(out) != "stage" || strings.Contains(string(out), "#AutomaticUpdatePolicy") || !strings.Contains(string(out), "#IdleExitTimeout=60") {
		t.Errorf("SetRpmOstreePolicy = %q", out)
	}
	if RpmOstreePolicy(conf) != "none" {
		t.Error("commented policy must read as none")
	}
	if RpmOstreePolicyForMode(ModeSecurity) != "check" || RpmOstreePolicyForMode(ModeAll) != "stage" || RpmOstreePolicyForMode(ModeOff) != "none" {
		t.Error("policy mapping wrong")
	}
}

func TestZypperParsers(t *testing.T) {
	patches := `<?xml version='1.0'?><stream><update-status version="0.6"><update-list>
<update kind="patch" name="openSUSE-2026-1" edition="1" category="security" severity="important"><issue-list><issue type="cve" id="CVE-2026-1234"/><issue type="bugzilla" id="1"/></issue-list></update>
</update-list></update-status></stream>`
	got, err := ParseZypperPatches([]byte(patches))
	if err != nil || len(got) != 1 || !got[0].Security || got[0].Severity != "important" || len(got[0].CVEs) != 1 {
		t.Errorf("ParseZypperPatches = %+v, %v", got, err)
	}
	updates := `<?xml version='1.0'?><stream><update-status version="0.6"><update-list><update kind="package" name="libopenh264-8" edition="2.6.0-2" arch="x86_64" edition-old="2.6.0-1"><summary>x</summary></update></update-list></update-status></stream>`
	pk, err := ParseZypperUpdates([]byte(updates))
	if err != nil || len(pk) != 1 || pk[0].Name != "libopenh264-8" || pk[0].Security {
		t.Errorf("ParseZypperUpdates = %+v, %v", pk, err)
	}
	if OsUpdateMode(RenderOsUpdateConf(ModeSecurity)) != ModeSecurity || !strings.Contains(string(RenderOsUpdateConf(ModeAll)), `REBOOT_CMD="none"`) {
		t.Error("os-update conf wrong")
	}
}

func TestWindow(t *testing.T) {
	w, err := ParseWindow("02:00-05:30")
	if err != nil || w.String() != "02:00-05:30" {
		t.Fatalf("ParseWindow: %v %v", w, err)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, time.Local) }
	if !w.Contains(at(3, 0)) || w.Contains(at(5, 30)) || w.Contains(at(1, 59)) {
		t.Error("daytime window wrong")
	}
	wrap, _ := ParseWindow("22:00-04:00")
	if !wrap.Contains(at(23, 0)) || !wrap.Contains(at(1, 0)) || wrap.Contains(at(12, 0)) {
		t.Error("wrapping window wrong")
	}
	if _, err := ParseWindow("25:00-01:00"); err == nil {
		t.Error("bad hour accepted")
	}
	if n, err := ParseWindow(""); err != nil || n != nil || !n.Contains(at(0, 0)) {
		t.Error("empty window must match always")
	}
	drop := string(RenderTimerDropIn("*-*-* 02:00", "30m", nil))
	if !strings.Contains(drop, "OnCalendar=\n") || !strings.Contains(drop, "OnCalendar=*-*-* 02:00\n") || !strings.Contains(drop, "OnBootSec=\n") || !strings.Contains(drop, "RandomizedDelaySec=30m") {
		t.Errorf("drop-in wrong:\n%s", drop)
	}
}

func TestParseFwupdUpdates(t *testing.T) {
	doc := `{"Devices":[{"Name":"System Firmware","DeviceId":"abc","Version":"1.10","Releases":[{"Version":"1.12","Urgency":"high","Summary":"Fixes a boot issue","Issues":["CVE-2026-1111","LVFS-1"]},{"Version":"1.11","Urgency":"low"}]},{"Name":"No releases","Releases":[]}]}`
	got, err := ParseFwupdUpdates([]byte(doc))
	if err != nil || len(got) != 1 {
		t.Fatalf("expected one firmware update, got %v (%v)", got, err)
	}
	fw := got[0]
	if fw.Name != "System Firmware" || fw.Version != "1.12" || fw.Severity != "important" || !fw.Security || len(fw.CVEs) != 1 || fw.CVEs[0] != "CVE-2026-1111" || fw.Advisory != "Fixes a boot issue" {
		t.Errorf("firmware entry wrong: %+v", fw)
	}
	if got, err := ParseFwupdUpdates([]byte("No updatable devices\n")); err != nil || got != nil {
		t.Errorf("non-JSON output must mean no updates, got %v %v", got, err)
	}
}

func TestEngineRunCommand(t *testing.T) {
	dnf5 := &Platform{Engine: EngineDnf, DNF5: true, Installed: true, DnfBin: "/usr/bin/dnf5", EngineRunBin: "/usr/bin/dnf5"}
	apt := &Platform{Engine: EngineApt, Installed: true, EngineRunBin: "/usr/bin/unattended-upgrade"}
	ostree := &Platform{Engine: EngineRpmOstree, Installed: true, RpmOstreeBin: "/usr/bin/rpm-ostree", EngineRunBin: "/usr/bin/rpm-ostree"}
	cases := []struct {
		name     string
		p        *Platform
		mode     Mode
		wantBin  string
		wantArgs string
		refused  bool
	}{
		{"dnf5 security", dnf5, ModeSecurity, "/usr/bin/dnf5", "automatic", false},
		{"apt all", apt, ModeAll, "/usr/bin/unattended-upgrade", "-v", false},
		{"check refused", apt, ModeCheck, "", "", true},
		{"off refused", dnf5, ModeOff, "", "", true},
		{"keep refused", apt, ModeKeep, "", "", true},
		{"ostree check", ostree, ModeCheck, "/usr/bin/rpm-ostree", "upgrade --check", false},
		{"ostree all", ostree, ModeAll, "/usr/bin/rpm-ostree", "upgrade", false},
		{"not installed", &Platform{Engine: EngineApt}, ModeAll, "", "", true},
	}
	for _, c := range cases {
		bin, args, reason := EngineRunCommand(c.p, c.mode)
		if bin != c.wantBin || strings.Join(args, " ") != c.wantArgs || (reason == "") == c.refused {
			t.Errorf("%s: got %q %q (%q)", c.name, bin, args, reason)
		}
	}
	if bin, args := RefreshCommand(apt); bin != "" || args != nil {
		t.Errorf("apt without apt-get must have no refresh command, got %q %q", bin, args)
	}
	if bin, args := RefreshCommand(&Platform{Engine: EngineApt, AptGetBin: "/usr/bin/apt-get"}); bin != "/usr/bin/apt-get" || strings.Join(args, " ") != "update" {
		t.Errorf("apt refresh wrong: %q %q", bin, args)
	}
}

func TestMetadataRefreshTime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "var", "lib", "apt", "lists"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &Platform{Engine: EngineApt, Root: root}
	if !MetadataRefreshTime(p).IsZero() {
		t.Error("no cache means no refresh time")
	}
	list := filepath.Join(root, "var", "lib", "apt", "lists", "archive_dists_resolute_main_binary-amd64_Packages")
	if err := os.WriteFile(list, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(list, old, old); err != nil {
		t.Fatal(err)
	}
	if got := MetadataRefreshTime(p); got.Sub(old).Abs() > time.Second {
		t.Errorf("expected the list mtime %v, got %v", old, got)
	}
}
