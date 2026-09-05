// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// fakeFlatpak records every invocation and answers from a scripted table.
type fakeFlatpak struct {
	mu       sync.Mutex
	calls    [][]string
	remotes  string // output of `flatpak remotes`
	list     string // output of `flatpak list`
	listNext string // output of `flatpak list` after the first call ("" = same)
	listSeen int
	fail     map[string]string // subcommand+" "+ref → stderr text that triggers an error
}

func (f *fakeFlatpak) install(t *testing.T) {
	t.Helper()
	origRun, origLook := flatpakRun, flatpakLookPath
	flatpakRun = func(_ context.Context, env []string, _ string, args ...string) ([]byte, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, append([]string{}, args...))
		for _, e := range env {
			if e == "LC_ALL=C" {
				goto ok
			}
		}
		t.Errorf("LC_ALL=C missing from env %v", env)
	ok:
		if len(args) == 0 {
			return nil, errors.New("no args")
		}
		switch args[0] {
		case "--version":
			return []byte("Flatpak 1.18.0\n"), nil
		case "remotes":
			return []byte(f.remotes), nil
		case "list":
			f.listSeen++
			if f.listSeen > 1 && f.listNext != "" {
				return []byte(f.listNext), nil
			}
			return []byte(f.list), nil
		}
		ref := args[len(args)-1]
		if msg, ok := f.fail[args[0]+" "+ref]; ok {
			return []byte("error: " + msg + "\n"), errors.New("exit status 1")
		}
		return []byte(""), nil
	}
	flatpakLookPath = func(string) error { return nil }
	t.Cleanup(func() { flatpakRun, flatpakLookPath = origRun, origLook })
}

func (f *fakeFlatpak) find(sub string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if len(c) > 0 && c[0] == sub {
			out = append(out, c)
		}
	}
	return out
}

func testOpts(t *testing.T) *FlatpakOptions {
	t.Helper()
	dir := t.TempDir()
	return &FlatpakOptions{
		StateDir:         filepath.Join(dir, "etc-bor-flatpak"),
		StateFile:        filepath.Join(dir, "state", "flatpak-state.json"),
		OperationTimeout: time.Minute,
	}
}

func TestValidateFlatpakIdentifiers(t *testing.T) {
	good := []string{"flathub", "flathub-beta", "my.remote_1"}
	for _, n := range good {
		if err := ValidateFlatpakRemoteName(n); err != nil {
			t.Errorf("remote %q rejected: %v", n, err)
		}
	}
	bad := []string{"", "../etc", "a/b", "with space", "-leading", strings.Repeat("a", 65)}
	for _, n := range bad {
		if err := ValidateFlatpakRemoteName(n); err == nil {
			t.Errorf("remote %q accepted", n)
		}
	}
	if err := ValidateFlatpakAppID("org.mozilla.firefox"); err != nil {
		t.Errorf("app id rejected: %v", err)
	}
	for _, id := range []string{"", "firefox", "org..evil.App", "org/x/App", "org.x.App;rm", "1org.x.App"} {
		if err := ValidateFlatpakAppID(id); err == nil {
			t.Errorf("app id %q accepted", id)
		}
	}
	if err := ValidateFlatpakBranch("stable"); err != nil {
		t.Error(err)
	}
	if err := ValidateFlatpakBranch("sta ble"); err == nil {
		t.Error("branch with space accepted")
	}
	if err := ValidateFlatpakFilterRef("app/org.gnome.*/*/*"); err != nil {
		t.Error(err)
	}
	if err := ValidateFlatpakFilterRef("app/x;rm -rf /"); err == nil {
		t.Error("filter ref with shell chars accepted")
	}
}

func TestParseFlatpakList(t *testing.T) {
	out := "com.github.Flacon\tflathub\tstable\tx86_64\t11.1.0\tsystem\tapp/com.github.Flacon/x86_64/stable\n" +
		"org.example.NoVersion\tcustom\tmaster\tx86_64\t\tsystem\tapp/org.example.NoVersion/x86_64/master\n\n"
	apps := parseFlatpakList(out)
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}
	if apps[0].App != "com.github.Flacon" || apps[0].Origin != "flathub" || apps[0].Version != "11.1.0" || apps[0].Branch != "stable" {
		t.Errorf("unexpected first row: %+v", apps[0])
	}
	if apps[1].Version != "" || apps[1].Ref != "app/org.example.NoVersion/x86_64/master" {
		t.Errorf("unexpected second row: %+v", apps[1])
	}
}

func TestParseFlatpakRemotes(t *testing.T) {
	out := "flathub\thttps://dl.flathub.org/repo/\tFlathub\t-\t-\t1\tsystem\torg.flathub.Stable\n" +
		"beta\thttps://dl.flathub.org/beta-repo/\tFlathub Beta\tverified\t/etc/bor/flatpak/beta.filter\t5\tsystem,disabled,no-enumerate,no-gpg-verify\t-\n"
	rs := parseFlatpakRemotes(out)
	if len(rs) != 2 {
		t.Fatalf("got %d remotes, want 2", len(rs))
	}
	if rs[0].Subset != "" || rs[0].Filter != "" || rs[0].Priority != 1 || rs[0].Disabled || rs[0].Collection != "org.flathub.Stable" {
		t.Errorf("unexpected flathub row: %+v", rs[0])
	}
	b := rs[1]
	if b.Subset != "verified" || b.Filter != "/etc/bor/flatpak/beta.filter" || b.Priority != 5 || !b.Disabled || !b.NoEnumerate || !b.NoGPGVerify {
		t.Errorf("unexpected beta row: %+v", b)
	}
}

func TestClassifyFlatpakFailure(t *testing.T) {
	cases := []struct {
		err  error
		out  string
		kind flatpakFailureKind
	}{
		{nil, "", flatpakFailureNone},
		{errors.New("exit status 1"), "error: Nothing matches org.x in remote flathub", flatpakFailureNotFound},
		{errors.New("exit status 1"), "Warning: x\nerror: No remote refs found similar to 'org.x'", flatpakFailureNotFound},
		{fmt.Errorf("%w: signal: killed", context.DeadlineExceeded), "", flatpakFailureTimeout},
		{errors.New("exit status 1"), "error: Unknown installation 'kiosk'", flatpakFailureUnknownInstallation},
		{errors.New("exit status 1"), "error: Failed to connect: network down", flatpakFailureOther},
	}
	for i, c := range cases {
		kind, msg := classifyFlatpakFailure(c.err, c.out)
		if kind != c.kind {
			t.Errorf("case %d: kind %v, want %v (msg %q)", i, kind, c.kind, msg)
		}
		if kind != flatpakFailureNone && kind != flatpakFailureTimeout && strings.HasPrefix(msg, "error: ") {
			t.Errorf("case %d: message should strip the error prefix: %q", i, msg)
		}
	}
}

func TestRenderFlatpakRepoFile(t *testing.T) {
	r := &pb.FlatpakRemoteEntry{
		Name: "flathub", Url: "https://dl.flathub.org/repo/", Title: "Flathub", Comment: "multi\nline",
		Homepage: "https://flathub.org/", DefaultBranch: "stable", Subset: "verified", CollectionId: "org.flathub.Stable",
		NoUseForDeps: true, GpgVerify: true, GpgKeyData: []byte("KEYBYTES"),
		FilterMode: pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_ALLOWLIST,
	}
	got := string(renderFlatpakRepoFile(r, "/etc/bor/flatpak/flathub.filter"))
	for _, want := range []string{"[Flatpak Repo]", "Url=https://dl.flathub.org/repo/", "Title=Flathub", "Comment=multi line",
		"Subset=verified", "CollectionID=org.flathub.Stable", "NoDeps=true", "Filter=/etc/bor/flatpak/flathub.filter",
		"GPGKey=S0VZQllURVM=", "DefaultBranch=stable"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	plain := string(renderFlatpakRepoFile(&pb.FlatpakRemoteEntry{Name: "r", Url: "https://x/"}, "/tmp/r.filter"))
	if strings.Contains(plain, "Filter=") || strings.Contains(plain, "GPGKey=") {
		t.Errorf("unexpected keys for a plain remote:\n%s", plain)
	}
}

func TestRenderFlatpakFilter(t *testing.T) {
	apps := []*pb.FlatpakAppEntry{
		{AppId: "org.gimp.GIMP", Remote: "flathub", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
		{AppId: "org.example.Gone", Remote: "flathub", State: pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT},
		{AppId: "org.other.App", Remote: "other", State: pb.FlatpakAppState_FLATPAK_APP_STATE_LATEST},
		{AppId: "org.any.App", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
	}
	allow := &pb.FlatpakRemoteEntry{Name: "flathub", FilterMode: pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_ALLOWLIST,
		FilterRefs: []string{"app/org.gnome.*", "bad;ref"}}
	got := string(renderFlatpakFilter(allow, apps))
	lines := strings.Split(strings.TrimSpace(got), "\n")
	want := []string{"# Managed by Bor. Do not edit manually.", "deny *", "allow runtime/*", "allow app/org.gnome.*",
		"allow app/org.gimp.GIMP/*/*", "allow app/org.any.App/*/*"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("allowlist mismatch:\n got %q\nwant %q", lines, want)
	}
	deny := &pb.FlatpakRemoteEntry{Name: "flathub", FilterMode: pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_DENYLIST,
		FilterRefs: []string{"app/com.evil.*"}}
	if got := string(renderFlatpakFilter(deny, apps)); !strings.Contains(got, "deny app/com.evil.*") || strings.Contains(got, "allow") {
		t.Errorf("denylist mismatch:\n%s", got)
	}
	if renderFlatpakFilter(&pb.FlatpakRemoteEntry{Name: "x"}, apps) != nil {
		t.Error("no filter mode should render nothing")
	}
}

func TestRemoteModifyArgs(t *testing.T) {
	cur := &flatpakRemoteState{Name: "flathub", URL: "https://dl.flathub.org/repo/", Title: "Flathub", Priority: 1}
	desired := &pb.FlatpakRemoteEntry{Name: "flathub", Url: "https://dl.flathub.org/repo/", Title: "Flathub", Enabled: true,
		Subset: "verified", Priority: 3, GpgVerify: true, GpgKeyData: []byte("k"),
		FilterMode: pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_DENYLIST, FilterRefs: []string{"app/x"}}
	args := remoteModifyArgs(cur, desired, "/etc/bor/flatpak/flathub.filter", "/etc/bor/flatpak/flathub.gpg", true)
	want := []string{"--subset=verified", "--filter=/etc/bor/flatpak/flathub.filter", "--prio=3", "--gpg-import=/etc/bor/flatpak/flathub.gpg"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", args, want)
	}
	// Already in the desired state → nothing to do.
	same := &flatpakRemoteState{Name: "flathub", URL: "https://dl.flathub.org/repo/", Title: "Flathub", Subset: "verified",
		Filter: "/etc/bor/flatpak/flathub.filter", Priority: 3}
	if noop := remoteModifyArgs(same, desired, "/etc/bor/flatpak/flathub.filter", "/etc/bor/flatpak/flathub.gpg", false); len(noop) != 0 {
		t.Errorf("expected no args, got %v", noop)
	}
	// Disabling, dropping the filter and turning GPG off.
	off := &pb.FlatpakRemoteEntry{Name: "flathub", Url: "https://dl.flathub.org/repo/", Enabled: false}
	args = remoteModifyArgs(same, off, "/etc/bor/flatpak/flathub.filter", "/etc/bor/flatpak/flathub.gpg", false)
	for _, w := range []string{"--subset=", "--no-filter", "--prio=1", "--disable", "--no-gpg-verify"} {
		if !strings.Contains(strings.Join(args, " "), w) {
			t.Errorf("missing %q in %v", w, args)
		}
	}
}

func TestRestoreModifyArgs(t *testing.T) {
	cur := &flatpakRemoteState{Name: "flathub", URL: "https://dl.flathub.org/repo/", Subset: "verified", Filter: "/f", Priority: 3, Disabled: true}
	orig := &flatpakRemoteOriginal{URL: "https://dl.flathub.org/repo/", Subset: "", Filter: "", Priority: 1, Enabled: true, GPGVerify: true}
	args := restoreModifyArgs(cur, orig)
	if strings.Join(args, " ") != "--subset= --no-filter --prio=1 --enable" {
		t.Errorf("args = %v", args)
	}
}

func TestMergeFlatpakEntries(t *testing.T) {
	low := &pb.FlatpakPolicy{
		Remotes: []*pb.FlatpakRemoteEntry{{Name: "flathub", Url: "https://a/"}},
		Apps: []*pb.FlatpakAppEntry{
			{AppId: "org.x.App", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
			{AppId: "org.y.App", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
		},
		AutoUpdate: true, OperationTimeoutMinutes: 10,
	}
	high := &pb.FlatpakPolicy{
		Remotes: []*pb.FlatpakRemoteEntry{{Name: "flathub", Url: "https://b/"}},
		Apps: []*pb.FlatpakAppEntry{
			{AppId: "org.x.App", State: pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT},
			{AppId: "org.x.App", Scope: pb.FlatpakScope_FLATPAK_SCOPE_USER, State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
		},
		UninstallUnused: true, Installation: "kiosk", AutoUpdateIntervalHours: 6,
	}
	// Deliberately pass high first to prove sorting by priority, not order.
	d := MergeFlatpakEntries([]FlatpakEntry{{Priority: 50, Policy: high}, {Priority: 10, Policy: low}, {Priority: 99, Policy: nil}})
	if len(d.Remotes) != 1 || d.Remotes[0].GetUrl() != "https://b/" {
		t.Errorf("remote merge wrong: %+v", d.Remotes)
	}
	if len(d.Apps) != 3 {
		t.Fatalf("got %d apps, want 3", len(d.Apps))
	}
	if d.Apps[0].GetAppId() != "org.x.App" || d.Apps[0].GetState() != pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT {
		t.Errorf("higher priority should win for org.x.App: %+v", d.Apps[0])
	}
	if d.Apps[1].GetAppId() != "org.y.App" {
		t.Errorf("first-seen order not preserved: %+v", d.Apps)
	}
	if !d.AutoUpdate || !d.UninstallUnused || d.Installation != "kiosk" {
		t.Errorf("scalar merge wrong: %+v", d)
	}
	if d.AutoUpdateInterval != 6*time.Hour || d.OperationTimeout != 10*time.Minute {
		t.Errorf("interval/timeout wrong: %v %v", d.AutoUpdateInterval, d.OperationTimeout)
	}
	empty := MergeFlatpakEntries(nil)
	if !empty.Empty() || empty.AutoUpdateInterval != flatpakDefaultUpdateInterval || empty.OperationTimeout != flatpakDefaultOperationTimeout {
		t.Errorf("defaults wrong: %+v", empty)
	}
}

func TestRollupFlatpakCompliance(t *testing.T) {
	c := pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
	n := pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
	e := pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
	i := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
	cases := []struct {
		items []ComplianceItem
		want  pb.ComplianceStatus
	}{
		{nil, c},
		{[]ComplianceItem{{Key: "a", Status: c}}, c},
		{[]ComplianceItem{{Key: "a", Status: i}, {Key: "b", Status: i}}, i},
		{[]ComplianceItem{{Key: "a", Status: i}, {Key: "b", Status: c}}, c},
		{[]ComplianceItem{{Key: "a", Status: n, Message: "x"}, {Key: "b", Status: c}}, n},
		{[]ComplianceItem{{Key: "a", Status: n, Message: "x"}, {Key: "b", Status: e, Message: "y"}}, e},
	}
	for idx, tc := range cases {
		if got, _ := RollupFlatpakCompliance(tc.items); got != tc.want {
			t.Errorf("case %d: got %v want %v", idx, got, tc.want)
		}
	}
	if s, k := SplitFlatpakItemKey("app:org.x.App"); s != "flatpak:app" || k != "org.x.App" {
		t.Errorf("split app: %q %q", s, k)
	}
	if s, k := SplitFlatpakItemKey("remote:flathub"); s != "flatpak:remote" || k != "flathub" {
		t.Errorf("split remote: %q %q", s, k)
	}
}

func TestListBorManagedFlatpakFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"flathub.flatpakrepo", "flathub.filter", "flathub.gpg", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := ListBorManagedFlatpakFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Errorf("got %v", files)
	}
	if files, err := ListBorManagedFlatpakFiles(filepath.Join(dir, "missing")); err != nil || files != nil {
		t.Errorf("missing dir: %v %v", files, err)
	}
}

func TestFlatpakOptionsEnvAndInstallation(t *testing.T) {
	o := &FlatpakOptions{ProxyURL: "http://proxy:3128"}
	env := strings.Join(o.env(), " ")
	for _, w := range []string{"LC_ALL=C", "LANG=C", "https_proxy=http://proxy:3128", "HTTP_PROXY=http://proxy:3128"} {
		if !strings.Contains(env, w) {
			t.Errorf("missing %q in %s", w, env)
		}
	}
	if f, err := (&FlatpakOptions{}).instFlag(); err != nil || f != "--system" {
		t.Errorf("default inst flag %q %v", f, err)
	}
	if f, err := (&FlatpakOptions{Installation: "kiosk"}).instFlag(); err != nil || f != "--installation=kiosk" {
		t.Errorf("named inst flag %q %v", f, err)
	}
	if _, err := (&FlatpakOptions{Installation: "../x"}).instFlag(); err == nil {
		t.Error("traversal installation accepted")
	}
}

func TestSyncFlatpakApps_Plan(t *testing.T) {
	fake := &fakeFlatpak{
		list: "org.present.App\tflathub\tstable\tx86_64\t1.0\tsystem\tapp/org.present.App/x86_64/stable\n" +
			"org.remove.App\tflathub\tstable\tx86_64\t2.0\tsystem\tapp/org.remove.App/x86_64/stable\n",
		listNext: "org.present.App\tflathub\tstable\tx86_64\t1.0\tsystem\tapp/org.present.App/x86_64/stable\n" +
			"org.install.App\tflathub\tstable\tx86_64\t3.0\tsystem\tapp/org.install.App/x86_64/stable\n" +
			"org.latest.App\tflathub\tbeta\tx86_64\t4.0\tsystem\tapp/org.latest.App/x86_64/beta\n",
		fail: map[string]string{
			"install org.missing.App": "Nothing matches org.missing.App in remote flathub",
			"install org.broken.App":  "Failed to connect to dl.flathub.org",
		},
	}
	fake.install(t)
	opts := testOpts(t)
	desired := &FlatpakDesired{
		UninstallUnused: true,
		Apps: []*pb.FlatpakAppEntry{
			{AppId: "org.present.App", Remote: "flathub", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
			{AppId: "org.install.App", Remote: "flathub", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
			{AppId: "org.latest.App", Remote: "flathub", Branch: "beta", State: pb.FlatpakAppState_FLATPAK_APP_STATE_LATEST},
			{AppId: "org.remove.App", State: pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT, DeleteData: true},
			{AppId: "org.missing.App", Remote: "flathub", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT, Optional: true},
			{AppId: "org.broken.App", Remote: "flathub", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
			{AppId: "org.user.App", Scope: pb.FlatpakScope_FLATPAK_SCOPE_USER, State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
			{AppId: "bad id", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT},
		},
	}
	items, err := SyncFlatpakApps(context.Background(), opts, desired)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]ComplianceItem{}
	for _, it := range items {
		status[it.Key] = it
	}
	expect := map[string]pb.ComplianceStatus{
		"app:org.present.App": pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		"app:org.install.App": pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		"app:org.latest.App":  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		"app:org.remove.App":  pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
		"app:org.missing.App": pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
		"app:org.broken.App":  pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
		"app:org.user.App":    pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
		"app:bad id":          pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
	}
	for k, want := range expect {
		got, ok := status[k]
		if !ok {
			t.Errorf("missing item %s", k)
			continue
		}
		if got.Status != want {
			t.Errorf("%s: status %v (%s), want %v", k, got.Status, got.Message, want)
		}
	}
	joined := func(c []string) string { return strings.Join(c, " ") }
	installs := fake.find("install")
	var seen []string
	for _, c := range installs {
		seen = append(seen, joined(c))
	}
	for _, want := range []string{
		"install --system --noninteractive --assumeyes flathub org.install.App",
		"install --system --noninteractive --assumeyes --or-update flathub org.latest.App//beta",
	} {
		found := false
		for _, s := range seen {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing invocation %q in %v", want, seen)
		}
	}
	for _, s := range seen {
		if strings.Contains(s, "org.present.App") {
			t.Errorf("already installed app must not be reinstalled: %s", s)
		}
	}
	un := fake.find("uninstall")
	if len(un) != 2 {
		t.Fatalf("expected uninstall + --unused, got %v", un)
	}
	if joined(un[0]) != "uninstall --system --noninteractive --assumeyes --delete-data org.remove.App" {
		t.Errorf("uninstall argv = %v", un[0])
	}
	if joined(un[1]) != "uninstall --system --unused --noninteractive --assumeyes" {
		t.Errorf("unused argv = %v", un[1])
	}
}

func TestSyncFlatpakRemotes_AddAdoptCleanup(t *testing.T) {
	fake := &fakeFlatpak{
		remotes: "flathub\thttps://dl.flathub.org/repo/\tFlathub\t-\t-\t1\tsystem\torg.flathub.Stable\n",
	}
	fake.install(t)
	opts := testOpts(t)
	key := []byte("PUBLIC-KEY")
	desired := &FlatpakDesired{
		Remotes: []*pb.FlatpakRemoteEntry{
			{Name: "flathub", Url: "https://dl.flathub.org/repo/", Title: "Flathub", Enabled: true, Subset: "verified",
				Priority: 2, GpgVerify: true, GpgKeyData: key},
			{Name: "corp", Url: "https://repo.corp.example/flatpak/", Title: "Corp", Enabled: true, GpgVerify: false,
				FilterMode: pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_ALLOWLIST, FilterRefs: []string{"app/com.corp.*"}},
			{Name: "../evil", Url: "https://x/"},
		},
		Apps: []*pb.FlatpakAppEntry{{AppId: "com.corp.Tool", Remote: "corp", State: pb.FlatpakAppState_FLATPAK_APP_STATE_PRESENT}},
	}
	items, err := SyncFlatpakRemotes(context.Background(), opts, desired)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]ComplianceItem{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	if it := byKey["remote:flathub"]; it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("flathub: %+v", it)
	}
	if it := byKey["remote:corp"]; it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT || !strings.Contains(it.Message, "GPG") {
		t.Errorf("corp should carry the GPG reminder: %+v", it)
	}
	if it := byKey["remote:../evil"]; it.Status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Errorf("invalid name must be rejected: %+v", it)
	}

	// Files rendered.
	repoPath, filterPath, gpgPath := flatpakRemoteFiles(opts.StateDir, "corp")
	for _, p := range []string{repoPath, filterPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected managed file %s: %v", p, err)
		}
	}
	if _, err := os.Stat(gpgPath); err == nil {
		t.Errorf("corp has no key; %s should not exist", gpgPath)
	}
	if data, _ := os.ReadFile(filterPath); !strings.Contains(string(data), "allow app/com.corp.Tool/*/*") {
		t.Errorf("filter should auto-allow the desired app:\n%s", data)
	}
	_, _, fhGPG := flatpakRemoteFiles(opts.StateDir, "flathub")
	if data, err := os.ReadFile(fhGPG); err != nil || string(data) != "PUBLIC-KEY" {
		t.Errorf("flathub key file: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(opts.StateDir, "..", "evil.flatpakrepo")); err == nil {
		t.Error("path traversal produced a file")
	}

	// CLI calls: corp added from its repofile, flathub modified (adopted).
	adds := fake.find("remote-add")
	if len(adds) != 1 || strings.Join(adds[0], " ") != "remote-add --system --if-not-exists --from corp "+repoPath {
		t.Errorf("remote-add calls = %v", adds)
	}
	mods := fake.find("remote-modify")
	if len(mods) != 1 || mods[0][len(mods[0])-1] != "flathub" {
		t.Fatalf("remote-modify calls = %v", mods)
	}
	modLine := strings.Join(mods[0], " ")
	for _, w := range []string{"--subset=verified", "--prio=2", "--gpg-import=" + fhGPG} {
		if !strings.Contains(modLine, w) {
			t.Errorf("missing %q in %s", w, modLine)
		}
	}

	// State file: flathub adopted with original, corp created.
	st := loadFlatpakState(opts.StateFile)
	if rec := st.Remotes["flathub"]; rec.Created || rec.Original == nil || rec.Original.Subset != "" || rec.Original.Priority != 1 {
		t.Errorf("flathub record: %+v", rec)
	}
	if rec := st.Remotes["corp"]; !rec.Created {
		t.Errorf("corp record: %+v", rec)
	}

	// Second sync with the same desired state and the node now reflecting it:
	// no further CLI changes.
	fake.remotes = "flathub\thttps://dl.flathub.org/repo/\tFlathub\tverified\t-\t2\tsystem\torg.flathub.Stable\n" +
		"corp\thttps://repo.corp.example/flatpak/\tCorp\t-\t" + filterPath + "\t1\tsystem,no-gpg-verify\t-\n"
	before := len(fake.find("remote-modify")) + len(fake.find("remote-add"))
	if _, err := SyncFlatpakRemotes(context.Background(), opts, desired); err != nil {
		t.Fatal(err)
	}
	if after := len(fake.find("remote-modify")) + len(fake.find("remote-add")); after != before {
		t.Errorf("idempotent sync issued %d extra calls", after-before)
	}

	// Policy removed: corp deleted, flathub restored, files and state gone.
	CleanupFlatpak(context.Background(), opts)
	dels := fake.find("remote-delete")
	if len(dels) != 1 || strings.Join(dels[0], " ") != "remote-delete --system corp" {
		t.Errorf("remote-delete calls = %v", dels)
	}
	mods = fake.find("remote-modify")
	restore := strings.Join(mods[len(mods)-1], " ")
	if !strings.HasSuffix(restore, "flathub") || !strings.Contains(restore, "--subset=") || !strings.Contains(restore, "--prio=1") {
		t.Errorf("restore call = %s", restore)
	}
	if files, _ := ListBorManagedFlatpakFiles(opts.StateDir); len(files) != 0 {
		t.Errorf("managed files left behind: %v", files)
	}
	if _, err := os.Stat(opts.StateFile); err == nil {
		t.Error("state file should be removed")
	}
}

func TestSyncFlatpakRemotes_UnknownInstallation(t *testing.T) {
	fake := &fakeFlatpak{}
	fake.install(t)
	orig := flatpakRun
	flatpakRun = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "remotes" {
			return []byte("error: Unknown installation 'kiosk'"), errors.New("exit status 1")
		}
		return orig(ctx, env, name, args...)
	}
	opts := testOpts(t)
	opts.Installation = "kiosk"
	_, err := SyncFlatpakRemotes(context.Background(), opts, &FlatpakDesired{Remotes: []*pb.FlatpakRemoteEntry{{Name: "r", Url: "https://x/"}}})
	var inapp *FlatpakInapplicableError
	if !errors.As(err, &inapp) {
		t.Fatalf("expected FlatpakInapplicableError, got %v", err)
	}
}

func TestFlatpakAvailable(t *testing.T) {
	orig := flatpakLookPath
	t.Cleanup(func() { flatpakLookPath = orig })
	flatpakLookPath = func(string) error { return errors.New("not found") }
	if FlatpakAvailable("") {
		t.Error("expected unavailable")
	}
	flatpakLookPath = func(string) error { return nil }
	if !FlatpakAvailable("flatpak") {
		t.Error("expected available")
	}
}
