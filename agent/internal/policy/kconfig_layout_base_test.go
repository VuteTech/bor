// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

const breezeDarkDefaults = `[kdeglobals][KDE]
widgetStyle=Breeze

[kdeglobals][General]
ColorScheme=BreezeDark

[kdeglobals][Icons]
Theme=breeze-dark

[Wallpaper]
Image=Next

[ksplashrc][KSplash]
Theme=org.kde.breezedark.desktop
`

func TestMergeKConfigPolicies_LayoutUsesOwnGlobalThemeAsBase(t *testing.T) {
	sources := []KConfigPolicySource{{ID: "bbbb2222", Name: "Branding", Priority: 5, Policy: &pb.KConfigPolicy{
		Settings: []*pb.KConfigSetting{
			{File: "kdeglobals", GroupPath: []string{"KDE"}, Key: "LookAndFeelPackage", Value: "org.kde.breezedark.desktop", Immutable: true},
			{File: "kdeglobals", GroupPath: []string{"Icons"}, Key: "Theme", Value: "breeze-dark"},
		},
		Layout: &pb.KConfigLayout{WallpaperImage: "/srv/corp.png"},
	}}}
	res, err := MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.LayoutBaseTheme != "org.kde.breezedark.desktop" {
		t.Fatalf("base theme = %q", res.LayoutBaseTheme)
	}
	globals := string(res.Files["kdeglobals"])
	// The explicit theme is not written; the generated package (whose id
	// carries the base, so a changed base re-applies the defaults) is
	// selected and, because the explicit setting was enforced, locked.
	pkgID := LayoutPackageIDFor("bbbb2222", "org.kde.breezedark.desktop")
	if pkgID == LayoutPackageID("bbbb2222") || !strings.HasPrefix(pkgID, "org.bor.bbbb2222-") || !strings.HasSuffix(pkgID, ".desktop") {
		t.Fatalf("package id with base = %q", pkgID)
	}
	if strings.Contains(globals, "org.kde.breezedark.desktop") || !strings.Contains(globals, "LookAndFeelPackage[$i]="+pkgID) {
		t.Errorf("kdeglobals wrong:\n%s", globals)
	}
	if !strings.Contains(globals, "Theme=breeze-dark") {
		t.Errorf("other settings must still be written:\n%s", globals)
	}
	var baseItem, pkgItem bool
	for _, it := range res.Items["bbbb2222"] {
		if it.Key == "[KDE] LookAndFeelPackage" && it.Status == pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
			if strings.Contains(it.Message, "Base of the generated") {
				baseItem = true
			}
			if strings.Contains(it.Message, "based on org.kde.breezedark.desktop") {
				pkgItem = true
			}
		}
	}
	if !baseItem || !pkgItem {
		t.Errorf("items wrong (base=%v pkg=%v): %+v", baseItem, pkgItem, res.Items["bbbb2222"])
	}

	// Without a layout the explicit theme is an ordinary setting.
	sources[0].Policy.Layout = nil
	res, err = MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.LayoutBaseTheme != "" || !strings.Contains(string(res.Files["kdeglobals"]), "LookAndFeelPackage[$i]=org.kde.breezedark.desktop") {
		t.Errorf("explicit theme must be written when there is no layout:\n%s", res.Files["kdeglobals"])
	}

	// A group-scoped policy carries its layout into the group scope: the
	// explicit theme becomes the base there and the group overlay selects
	// the generated package, locked because the theme setting was enforced.
	// Nothing reaches the node-wide files.
	sources[0].Policy.Layout = &pb.KConfigLayout{WallpaperImage: "/srv/corp.png"}
	sources[0].Groups = []string{"students"}
	res, err = MergeKConfigPolicies(sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	sel := res.GroupLayouts["students"]
	if res.Layout != nil || res.LayoutBaseTheme != "" || sel == nil || sel.BaseTheme != "org.kde.breezedark.desktop" || !sel.Enforced || sel.PolicyID != "bbbb2222" {
		t.Fatalf("scoped policy wrong: node layout=%v base=%q group selection=%+v", res.Layout, res.LayoutBaseTheme, sel)
	}
	if sel.PackageID() != pkgID {
		t.Errorf("group package id = %q, want %q", sel.PackageID(), pkgID)
	}
	overlay := string(res.GroupFiles["students"]["kdeglobals"])
	if strings.Contains(overlay, "org.kde.breezedark.desktop") || !strings.Contains(overlay, "LookAndFeelPackage[$i]="+pkgID) {
		t.Errorf("group overlay wrong:\n%s", overlay)
	}
	if strings.Contains(string(res.Files["kdeglobals"]), "LookAndFeelPackage") {
		t.Errorf("scoped layout leaked into the node-wide file:\n%s", res.Files["kdeglobals"])
	}
}

func TestRenderLayoutFilesWithBase(t *testing.T) {
	layout := &pb.KConfigLayout{WallpaperImage: "/srv/corp.png"}
	const baseID = "org.kde.breezedark.desktop"
	base := &LookAndFeelBase{ID: baseID, Defaults: []byte(breezeDarkDefaults), LayoutScript: []byte("loadTemplate(\"org.kde.plasma.desktop.defaultPanel\")\n"), Preview: []byte{0x89, 'P', 'N', 'G'}}
	files := RenderLayoutFilesWithBase(layout, "bbbb2222", "Branding", baseID, base)
	pkg := "plasma/look-and-feel/" + LayoutPackageIDFor("bbbb2222", baseID)

	defaults := string(files[pkg+"/contents/defaults"])
	for _, want := range []string{"# Copied from org.kde.breezedark.desktop", "[kdeglobals][General]\nColorScheme=BreezeDark\n", "[ksplashrc][KSplash]\nTheme=org.kde.breezedark.desktop\n\n[Wallpaper]\nImage=file:///srv/corp.png\n"} {
		if !strings.Contains(defaults, want) {
			t.Errorf("defaults lack %q:\n%s", want, defaults)
		}
	}
	if strings.Contains(defaults, "Image=Next") || strings.Count(defaults, "[Wallpaper]") != 1 {
		t.Errorf("the base wallpaper must be replaced exactly once:\n%s", defaults)
	}
	if script := string(files[pkg+"/"+layoutScriptPath]); !strings.Contains(script, "inherited from org.kde.breezedark.desktop") || !strings.Contains(script, "defaultPanel") {
		t.Errorf("base layout script must be inherited:\n%s", script)
	}
	if string(files[pkg+"/contents/previews/preview.png"]) != "\x89PNG" {
		t.Error("base preview must be copied")
	}
	if !strings.Contains(string(files[pkg+"/metadata.json"]), "based on org.kde.breezedark.desktop") {
		t.Error("metadata should mention the base")
	}

	// The policy's own layout script wins over the inherited one; without
	// a wallpaper the base [Wallpaper] group stays.
	files = RenderLayoutFilesWithBase(&pb.KConfigLayout{LayoutScript: "var x = 1;"}, "bbbb2222", "Branding", baseID, base)
	if script := string(files[pkg+"/"+layoutScriptPath]); !strings.Contains(script, "var x = 1;") || strings.Contains(script, "inherited") {
		t.Errorf("policy script must win:\n%s", script)
	}
	if defaults := string(files[pkg+"/contents/defaults"]); !strings.Contains(defaults, "[Wallpaper]\nImage=Next\n") {
		t.Errorf("base wallpaper must stay without a policy wallpaper:\n%s", defaults)
	}

	// A configured base the node lacks: the id still carries the base (it
	// must match the selection written to kdeglobals), the content falls
	// back to the wallpaper only.
	missing := RenderLayoutFilesWithBase(layout, "bbbb2222", "Branding", baseID, nil)
	if string(missing[pkg+"/contents/defaults"]) != "# Managed by Bor. Defaults of the deployed look-and-feel package.\n[Wallpaper]\nImage=file:///srv/corp.png\n" {
		t.Errorf("missing-base defaults wrong:\n%s", missing[pkg+"/contents/defaults"])
	}

	// No base: same output as before.
	plainPkg := "plasma/look-and-feel/" + LayoutPackageID("bbbb2222")
	plain := RenderLayoutFiles(layout, "bbbb2222", "Branding")
	if string(plain[plainPkg+"/contents/defaults"]) != "# Managed by Bor. Defaults of the deployed look-and-feel package.\n[Wallpaper]\nImage=file:///srv/corp.png\n" {
		t.Errorf("plain defaults changed:\n%s", plain[plainPkg+"/contents/defaults"])
	}
	if _, has := plain[plainPkg+"/contents/previews/preview.png"]; has {
		t.Error("no preview without a base")
	}
}

func TestLoadLookAndFeelBaseFrom(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "org.kde.breezedark.desktop", "contents")
	if err := os.MkdirAll(filepath.Join(dir, "layouts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "defaults"), []byte(breezeDarkDefaults))
	writeFile(t, filepath.Join(dir, "layouts", "org.kde.plasma.desktop-layout.js"), []byte("// layout\n"))

	base, ok := loadLookAndFeelBaseFrom([]string{filepath.Join(root, "missing"), root}, "org.kde.breezedark.desktop")
	if !ok || base.ID != "org.kde.breezedark.desktop" || !strings.Contains(string(base.Defaults), "BreezeDark") || string(base.LayoutScript) != "// layout\n" || base.Preview != nil {
		t.Errorf("base = %+v ok=%v", base, ok)
	}
	if _, ok := loadLookAndFeelBaseFrom([]string{root}, "org.example.absent.desktop"); ok {
		t.Error("absent theme must not load")
	}
	if _, ok := loadLookAndFeelBaseFrom([]string{root}, "../etc"); ok {
		t.Error("path traversal must be refused")
	}
}
