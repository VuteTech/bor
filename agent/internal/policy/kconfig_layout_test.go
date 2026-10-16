// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestLayoutPackageID(t *testing.T) {
	cases := map[string]string{
		"3f9a2b1c-7d4e-4a5b-9c8d-1e2f3a4b5c6d": "org.bor.3f9a2b1c7d4e.desktop",
		"ABC":                                  "org.bor.abc.desktop",
		"":                                     "org.bor.policy.desktop",
		"../etc":                               "org.bor.etc.desktop",
	}
	for in, want := range cases {
		if got := LayoutPackageID(in); got != want {
			t.Errorf("LayoutPackageID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWallpaperHelpers(t *testing.T) {
	if got := WallpaperURL("/usr/share/backgrounds/corp.png"); got != "file:///usr/share/backgrounds/corp.png" {
		t.Errorf("WallpaperURL path: %q", got)
	}
	if got := WallpaperURL("file:///x.png"); got != "file:///x.png" {
		t.Errorf("WallpaperURL url: %q", got)
	}
	if got := WallpaperURL("Next"); got != "Next" {
		t.Errorf("WallpaperURL package name: %q", got)
	}
	if got := WallpaperPath("file:///x.png"); got != "/x.png" {
		t.Errorf("WallpaperPath: %q", got)
	}
	for mode, want := range map[string]string{"0": "stretch", "1": "preserveAspectFit", "2": "preserveAspectCrop", "": "preserveAspectCrop", "3": "", "6": ""} {
		if got := WallpaperFillModeName(mode); got != want {
			t.Errorf("WallpaperFillModeName(%q) = %q, want %q", mode, got, want)
		}
	}
}

func TestRenderLayoutFiles(t *testing.T) {
	if files := RenderLayoutFiles(&pb.KConfigLayout{ApplyOnce: true}, "id", "n"); files != nil {
		t.Errorf("empty layout must render nothing, got %v", files)
	}

	layout := &pb.KConfigLayout{
		WallpaperImage:    "/usr/share/backgrounds/corp \"quoted\".png",
		WallpaperFillMode: "1",
		LayoutScript:      "loadTemplate(\"org.kde.plasma.desktop.defaultPanel\")",
		ApplyOnce:         true,
		LockPackage:       true,
	}
	files := RenderLayoutFiles(layout, "3f9a2b1c-7d4e", "Lab desktop")
	pkg := "plasma/look-and-feel/org.bor.3f9a2b1c7d4e.desktop"

	meta, ok := files[pkg+"/metadata.json"]
	if !ok {
		t.Fatalf("metadata.json missing; files: %v", keys(files))
	}
	var parsed map[string]any
	if err := json.Unmarshal(meta, &parsed); err != nil {
		t.Fatalf("metadata.json is not JSON: %v", err)
	}
	if parsed["KPackageStructure"] != "Plasma/LookAndFeel" {
		t.Errorf("unexpected package structure: %v", parsed["KPackageStructure"])
	}
	plugin := parsed["KPlugin"].(map[string]any)
	if plugin["Id"] != "org.bor.3f9a2b1c7d4e.desktop" || !strings.Contains(plugin["Name"].(string), "Lab desktop") {
		t.Errorf("unexpected plugin metadata: %v", plugin)
	}

	defaults := string(files[pkg+"/contents/defaults"])
	if !strings.Contains(defaults, "[Wallpaper]\nImage=file:///usr/share/backgrounds/corp \"quoted\".png\n") {
		t.Errorf("defaults wrong:\n%s", defaults)
	}

	script := string(files[pkg+"/contents/layouts/org.kde.plasma.desktop-layout.js"])
	if !strings.Contains(script, "loadTemplate(\"org.kde.plasma.desktop.defaultPanel\")") {
		t.Errorf("layout script wrong:\n%s", script)
	}

	var update string
	var updateName string
	for name, data := range files {
		if strings.HasPrefix(name, "plasma/shells/org.kde.plasma.desktop/contents/updates/bor-") && strings.HasSuffix(name, ".js") {
			update = string(data)
			updateName = name
		}
	}
	if update == "" {
		t.Fatalf("update script missing; files: %v", keys(files))
	}
	for _, want := range []string{
		`var borImage = "file:///usr/share/backgrounds/corp \"quoted\".png";`,
		"var borFillMode = 1;",
		`d.wallpaperPlugin = "org.kde.image";`,
		`d.writeConfig("Image", borImage);`,
		"borPanels[p].remove();",
		"loadTemplate(\"org.kde.plasma.desktop.defaultPanel\")",
	} {
		if !strings.Contains(update, want) {
			t.Errorf("update script lacks %q:\n%s", want, update)
		}
	}

	// A different wallpaper yields a different update script name, so it
	// runs again for users who already received the old one.
	layout2 := &pb.KConfigLayout{WallpaperImage: "/other.png", ApplyOnce: true}
	files2 := RenderLayoutFiles(layout2, "3f9a2b1c-7d4e", "Lab desktop")
	for name := range files2 {
		if strings.HasSuffix(name, ".js") && strings.Contains(name, "/updates/") && name == updateName {
			t.Error("update script name must change with its content")
		}
	}
	if _, has := files2[pkg+"/contents/layouts/org.kde.plasma.desktop-layout.js"]; has {
		t.Error("no layout script means no layouts file")
	}
	if _, has := RenderLayoutFiles(&pb.KConfigLayout{WallpaperImage: "/w.png"}, "x", "n")["plasma/shells/org.kde.plasma.desktop/contents/updates/"]; has {
		t.Error("without apply_once there must be no update script")
	}
	for name := range RenderLayoutFiles(&pb.KConfigLayout{WallpaperImage: "/w.png"}, "x", "n") {
		if strings.Contains(name, "/updates/") {
			t.Errorf("without apply_once there must be no update script, got %s", name)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSyncDataFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "share")

	changed, err := SyncDataFiles(dir, map[string][]byte{
		"plasma/look-and-feel/org.bor.a.desktop/metadata.json":     []byte("{}\n"),
		"plasma/look-and-feel/org.bor.a.desktop/contents/defaults": []byte("[Wallpaper]\nImage=x\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Errorf("expected 2 written files, got %v", changed)
	}
	if got := ListDataFiles(dir); len(got) != 2 {
		t.Errorf("ListDataFiles = %v", got)
	}

	// Unchanged content is not rewritten; a stale file is removed and its
	// empty directories pruned.
	changed, err = SyncDataFiles(dir, map[string][]byte{
		"plasma/look-and-feel/org.bor.a.desktop/metadata.json": []byte("{}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || !strings.HasSuffix(changed[0], "contents/defaults") {
		t.Errorf("expected only the stale file to change, got %v", changed)
	}
	if _, err := os.Stat(filepath.Join(dir, "plasma", "look-and-feel", "org.bor.a.desktop", "contents")); !os.IsNotExist(err) {
		t.Error("empty contents directory should be pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "plasma", "look-and-feel", "org.bor.a.desktop", "metadata.json")); err != nil {
		t.Error("kept file must still exist")
	}

	// An empty map clears everything but keeps the data directory itself.
	if _, err := SyncDataFiles(dir, nil); err != nil {
		t.Fatal(err)
	}
	if got := ListDataFiles(dir); len(got) != 0 {
		t.Errorf("expected no files after clearing, got %v", got)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Error("data directory itself must stay")
	}

	// Guards.
	if _, err := SyncDataFiles("/", nil); err == nil {
		t.Error("refusing to manage / is required")
	}
	if _, err := SyncDataFiles(dir, map[string][]byte{"../escape": []byte("x")}); err == nil {
		t.Error("paths escaping the data directory must be rejected")
	}
}

func TestMergeKConfigPolicies_LayoutWinnerAndPackageSelection(t *testing.T) {
	sources := []KConfigPolicySource{
		{ID: "aaaa1111", Name: "Branding A", Priority: 1, Policy: &pb.KConfigPolicy{Layout: &pb.KConfigLayout{WallpaperImage: "/a.png"}}},
		{ID: "bbbb2222", Name: "Branding B", Priority: 9, Policy: &pb.KConfigPolicy{Layout: &pb.KConfigLayout{WallpaperImage: "/b.png", LockPackage: true}}},
	}
	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Layout == nil || res.LayoutPolicyID != "bbbb2222" || res.Layout.GetWallpaperImage() != "/b.png" {
		t.Fatalf("highest-priority layout should win, got %+v (%s)", res.Layout, res.LayoutPolicyID)
	}
	content := string(res.Files["kdeglobals"])
	if !strings.Contains(content, "[KDE][$i]\nLookAndFeelPackage=org.bor.bbbb2222.desktop\n") {
		t.Errorf("expected locked LookAndFeelPackage selection, got:\n%s", content)
	}
	var sawOverride bool
	for _, it := range res.Items["aaaa1111"] {
		if it.SchemaId == "layout" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE && strings.Contains(it.Message, "Branding B") {
			sawOverride = true
		}
	}
	if !sawOverride {
		t.Errorf("losing layout should report an inapplicable item, got %+v", res.Items["aaaa1111"])
	}

	// A catalogue setting for the global theme in a higher-priority policy
	// keeps the selection; the package is still generated.
	sources = append(sources, KConfigPolicySource{ID: "cccc3333", Name: "Theme", Priority: 50, Policy: &pb.KConfigPolicy{Settings: []*pb.KConfigSetting{
		{File: "kdeglobals", GroupPath: []string{"KDE"}, Key: "LookAndFeelPackage", Value: "org.kde.breezedark.desktop", Immutable: true},
	}}})
	res, err = MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	content = string(res.Files["kdeglobals"])
	if !strings.Contains(content, "LookAndFeelPackage=org.kde.breezedark.desktop") || strings.Contains(content, "org.bor.bbbb2222") {
		t.Errorf("explicit theme setting must win, got:\n%s", content)
	}
	if res.Layout == nil {
		t.Error("the layout package is still generated for the winning layout policy")
	}
	var sawNotSelected bool
	for _, it := range res.Items["bbbb2222"] {
		if it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE && strings.Contains(it.Message, "not selected") {
			sawNotSelected = true
		}
	}
	if !sawNotSelected {
		t.Errorf("layout policy should report that its package is not selected, got %+v", res.Items["bbbb2222"])
	}
}

func TestEnvScriptContent_DataDirs(t *testing.T) {
	got := envScriptContent("/etc/bor/xdg", "/etc/bor/share", nil)
	for _, want := range []string{
		`export XDG_CONFIG_DIRS="/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}"`,
		`export XDG_DATA_DIRS="/etc/bor/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"`,
		`*":/etc/bor/share:"*) ;;`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hook script lacks %q:\n%s", want, got)
		}
	}
}
