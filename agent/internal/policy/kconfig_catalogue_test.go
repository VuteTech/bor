// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

const dolphinKcfg = `<?xml version="1.0" encoding="UTF-8"?>
<kcfg xmlns="http://www.kde.org/standards/kcfg/1.0">
  <kcfgfile name="dolphinrc"/>
  <group name="General">
    <entry name="ShowFullPath" type="Bool">
      <label>Should the full path be shown inside the location bar</label>
      <default>false</default>
    </entry>
    <entry name="ShowStatusBar" type="Enum">
      <label>Statusbar</label>
      <choices><choice name="Small"/><choice name="FullWidth"/><choice name="Disabled"/></choices>
      <default>0</default>
    </entry>
    <entry name="HomeUrl" type="String">
      <label>Home URL</label>
      <default code="true">QUrl::fromLocalFile(QDir::homePath()).toDisplayString()</default>
    </entry>
    <entry name="Version" type="Int">
      <default>0</default>
      <min>0</min>
      <max>99</max>
    </entry>
    <entry name="ViewPropsTimestamp" type="DateTime"/>
    <entry name="nameConstant$(ConstIndex)" type="String"/>
  </group>
  <group name="$(masterGroup)">
    <entry name="Skipped" type="Bool"/>
  </group>
  <group name="Scoring" parentGroupName="Plugin-org.kde.ActivityManager">
    <entry name="whatToRemember" key="what-to-remember" type="Int">
      <tooltip>Which data to keep</tooltip>
      <default>0</default>
    </entry>
  </group>
</kcfg>`

const kwinKcfg = `<kcfg xmlns="http://www.kde.org/standards/kcfg/1.0">
  <kcfgfile arg="true"/>
  <group name="Windows">
    <entry name="BorderlessMaximizedWindows" type="Bool"><default>false</default></entry>
  </group>
</kcfg>`

const unknownArgKcfg = `<kcfg xmlns="http://www.kde.org/standards/kcfg/1.0">
  <kcfgfile arg="true"/>
  <group name="General"><entry name="X" type="Bool"/></group>
</kcfg>`

func TestScanKConfigSchemasFrom(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dolphin_generalsettings.kcfg"), []byte(dolphinKcfg))
	writeFile(t, filepath.Join(dir, "kwin.kcfg"), []byte(kwinKcfg))
	writeFile(t, filepath.Join(dir, "mystery.kcfg"), []byte(unknownArgKcfg))
	writeFile(t, filepath.Join(dir, "broken.kcfg"), []byte("<kcfg><group"))
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("ignored"))

	entries, err := ScanKConfigSchemasFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]*pb.KConfigCatalogueEntry)
	for _, e := range entries {
		byKey[e.GetFile()+"/"+strings.Join(e.GetGroupPath(), ".")+"/"+e.GetKey()] = e
	}

	full := byKey["dolphinrc/General/ShowFullPath"]
	if full == nil {
		t.Fatalf("ShowFullPath missing; got %v", keysOf(byKey))
	}
	if full.GetType() != "Bool" || full.GetDefaultValue() != "false" || full.GetSource() != "kcfg" || full.GetCategory() != "Node: dolphinrc" || full.GetReload() != "app" {
		t.Errorf("unexpected entry: %+v", full)
	}
	if full.GetLabel() != "Should the full path be shown inside the location bar" {
		t.Errorf("label not taken from <label>: %q", full.GetLabel())
	}
	if full.GetId() != "kcfg/dolphinrc/General/ShowFullPath" {
		t.Errorf("unexpected id %q", full.GetId())
	}

	status := byKey["dolphinrc/General/ShowStatusBar"]
	if status == nil || status.GetType() != "Enum" || len(status.GetChoices()) != 3 || status.GetDefaultValue() != "Small" {
		t.Errorf("enum default index should map to the choice name: %+v", status)
	}

	if home := byKey["dolphinrc/General/HomeUrl"]; home == nil || home.GetDefaultValue() != "" {
		t.Errorf("computed defaults must be dropped: %+v", home)
	}
	if v := byKey["dolphinrc/General/Version"]; v == nil || v.GetRangeMin() != "0" || v.GetRangeMax() != "99" || v.GetLabel() != "Version" {
		t.Errorf("range / fallback label wrong: %+v", v)
	}
	if _, ok := byKey["dolphinrc/General/ViewPropsTimestamp"]; ok {
		t.Error("unsupported DateTime type must be skipped")
	}
	for k := range byKey {
		if strings.Contains(k, "$(") || strings.Contains(k, "Skipped") {
			t.Errorf("parameterised entry or group leaked: %s", k)
		}
	}

	scoring := byKey["dolphinrc/Plugin-org.kde.ActivityManager.Scoring/what-to-remember"]
	if scoring == nil || len(scoring.GetGroupPath()) != 2 || scoring.GetDescription() != "Which data to keep" {
		t.Errorf("parent group / key attribute / tooltip wrong: %+v", scoring)
	}

	if kwin := byKey["kwinrc/Windows/BorderlessMaximizedWindows"]; kwin == nil || kwin.GetReload() != "kwin" {
		t.Errorf("arg=true kwin.kcfg should map to kwinrc with kwin reload: %+v", kwin)
	}
	for k := range byKey {
		if strings.HasPrefix(k, "/") || strings.Contains(k, "mystery") {
			t.Errorf("schema with unknown runtime file name must be skipped: %s", k)
		}
	}

	if got, err := ScanKConfigSchemasFrom(filepath.Join(dir, "missing")); err != nil || got != nil {
		t.Errorf("missing directory should yield nil, nil; got %v, %v", got, err)
	}
}

func keysOf(m map[string]*pb.KConfigCatalogueEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestInstalledKCMModulesFrom(t *testing.T) {
	dir := t.TempDir()
	apps := filepath.Join(dir, "applications")
	plugins := filepath.Join(dir, "plugins", "plasma", "kcms", "systemsettings")
	for _, d := range []string{apps, plugins} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(apps, "kcm_access.desktop"), []byte("[Desktop Entry]\nType=Application\nName=Accessibility\nName[de]=Zugangshilfen\n"))
	writeFile(t, filepath.Join(apps, "kcm_users.desktop"), []byte("# no name\n[Other]\nName=Wrong\n"))
	writeFile(t, filepath.Join(plugins, "kcm_bluetooth.so"), []byte{0})

	mods := InstalledKCMModulesFrom([]string{apps}, []string{filepath.Join(dir, "plugins", "plasma", "kcms", "*", "*.so")})
	want := map[string]string{"kcm_access": "Accessibility", "kcm_bluetooth": "kcm_bluetooth", "kcm_users": "kcm_users"}
	if len(mods) != len(want) {
		t.Fatalf("got %d modules, want %d: %v", len(mods), len(want), mods)
	}
	for _, m := range mods {
		if want[m.GetId()] != m.GetName() {
			t.Errorf("%s: name %q, want %q", m.GetId(), m.GetName(), want[m.GetId()])
		}
	}
}

func TestProbeKConfigEffective(t *testing.T) {
	entries := []*pb.KConfigEntry{
		{File: "kdeglobals", Group: "KDE Action Restrictions", Key: "shell_access", Value: "false", Enforced: true},
		{File: "kscreenlockerrc", Group: "Daemon", Key: "Timeout", Value: "10"},
		{File: "kscreenlockerrc", Group: "Greeter][Wallpaper][org.kde.image][General", Key: "Image", Value: "/srv/lock.png"},
		{File: "kdeglobals", Group: "General", Key: "Gone", Delete: true},
		{File: "kdeglobals", Group: "KDE URL Restrictions", Key: "rule_1", Value: "x"},
		{File: "kdeglobals", Group: "General", Key: "Name", Value: "x", Locale: "de"},
	}
	var calls [][]string
	run := func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch {
		case strings.Contains(strings.Join(args, " "), "shell_access"):
			return "false\n", nil
		case strings.Contains(strings.Join(args, " "), "Timeout"):
			return "5", nil // user override
		case strings.Contains(strings.Join(args, " "), "Image"):
			return "", errors.New("exit status 1")
		}
		return "", nil
	}

	results := ProbeKConfigEffective(entries, run, "kreadconfig6")

	// Deleted entries, URL rules and localized keys are not probed; the
	// failing command is skipped.
	if len(calls) != 3 {
		t.Fatalf("expected 3 probes, got %d: %v", len(calls), calls)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %+v", results)
	}
	if results[0].Mismatch || results[0].ItemKey != "[KDE Action Restrictions] shell_access" {
		t.Errorf("shell_access should match: %+v", results[0])
	}
	if !results[1].Mismatch || results[1].Actual != "5" || results[1].Expected != "10" {
		t.Errorf("Timeout should be reported as overridden: %+v", results[1])
	}
	// Nested groups become repeated --group arguments.
	want := []string{"kreadconfig6", "--file", "kscreenlockerrc", "--group", "Greeter", "--group", "Wallpaper", "--group", "org.kde.image", "--group", "General", "--key", "Image"}
	if strings.Join(calls[2], " ") != strings.Join(want, " ") {
		t.Errorf("nested group args wrong:\n got %v\nwant %v", calls[2], want)
	}

	if got := ProbeKConfigEffective(entries, nil, "kreadconfig6"); got != nil {
		t.Error("nil runner must probe nothing")
	}
	if got := ProbeKConfigEffective(entries, run, ""); got != nil {
		t.Error("missing tool must probe nothing")
	}
}

func TestKConfigValuesEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"true", "true", true},
		{"true", "True", true},
		{"false", "0", true},
		{"10", "10.0", true},
		{"10", "5", false},
		{"breeze", "breeze", true},
		{"breeze", "Breeze", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := kconfigValuesEqual(c.a, c.b); got != c.want {
			t.Errorf("kconfigValuesEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestKConfigReadTool(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "kreadconfig5" {
			return "/usr/bin/kreadconfig5", nil
		}
		return "", errors.New("not found")
	}
	if got := KConfigReadTool(look); got != "kreadconfig5" {
		t.Errorf("got %q, want kreadconfig5", got)
	}
	none := func(string) (string, error) { return "", errors.New("not found") }
	if got := KConfigReadTool(none); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSessionSeesConfigDirIn(t *testing.T) {
	proc := t.TempDir()
	mkProc := func(pid, comm, uid, environ string) {
		d := filepath.Join(proc, pid)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(d, "comm"), []byte(comm+"\n"))
		writeFile(t, filepath.Join(d, "status"), []byte("Name:\t"+comm+"\nUid:\t"+uid+"\t"+uid+"\t"+uid+"\t"+uid+"\n"))
		writeFile(t, filepath.Join(d, "environ"), []byte(environ))
	}
	mkProc("100", "plasmashell", "1000", "HOME=/home/a\x00XDG_CONFIG_DIRS=/etc/bor/xdg:/etc/xdg\x00")
	mkProc("200", "plasmashell", "1001", "XDG_CONFIG_DIRS=/etc/xdg\x00")
	mkProc("300", "bash", "1001", "XDG_CONFIG_DIRS=/etc/bor/xdg\x00")
	writeFile(t, filepath.Join(proc, "version"), []byte("Linux"))

	if found, running := sessionSeesConfigDirIn(proc, 1000, "/etc/bor/xdg"); !found || !running {
		t.Errorf("uid 1000: found=%v running=%v, want true/true", found, running)
	}
	if found, running := sessionSeesConfigDirIn(proc, 1001, "/etc/bor/xdg"); found || !running {
		t.Errorf("uid 1001: found=%v running=%v, want false/true (bash does not count)", found, running)
	}
	if found, running := sessionSeesConfigDirIn(proc, 1002, "/etc/bor/xdg"); found || running {
		t.Errorf("uid 1002: found=%v running=%v, want false/false", found, running)
	}
}
