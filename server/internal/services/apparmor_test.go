// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

const apparmorStub = "abi <abi/4.0>,\\ninclude <tunables/global>\\nprofile bor-corpapp /opt/corpapp/app flags=(unconfined) {\\n  userns,\\n}\\n"

func TestValidateApparmorContent(t *testing.T) {
	valid := map[string]string{
		"require":   `{"requireEnabled":true}`,
		"service":   `{"ensureService":true}`,
		"userns":    `{"restrictUnprivilegedUserns":"APPARMOR_SWITCH_ON"}`,
		"profiles":  `{"profiles":[{"match":"usr.sbin.cupsd","mode":"APPARMOR_MODE_ENFORCE"},{"match":"usr.bin.*","mode":"APPARMOR_MODE_COMPLAIN"},{"match":"*","mode":"APPARMOR_MODE_ENFORCE"}]}`,
		"disable":   `{"profiles":[{"match":"usr.bin.man","mode":"APPARMOR_MODE_DISABLED"}]}`,
		"flags":     `{"profiles":[{"match":"Xorg","mode":"APPARMOR_MODE_ENFORCE","editVendorFlags":true}]}`,
		"custom":    `{"customProfiles":[{"name":"corpapp","text":"` + apparmorStub + `"}]}`,
		"override":  `{"localOverrides":[{"profileFile":"usr.sbin.cupsd","text":"  /nfs/home/** rwk,\\n"}]}`,
		"home dirs": `{"extraHomeDirs":["/nfs/home/"]}`,
	}
	for name, content := range valid {
		if err := ValidateApparmorContent(content); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
	invalid := map[string]string{
		"empty":            ``,
		"empty object":     `{}`,
		"nothing":          `{"profiles":[]}`,
		"no mode":          `{"profiles":[{"match":"usr.sbin.cupsd"}]}`,
		"disable glob":     `{"profiles":[{"match":"*","mode":"APPARMOR_MODE_DISABLED"}]}`,
		"disable protect":  `{"profiles":[{"match":"unprivileged_userns","mode":"APPARMOR_MODE_DISABLED"}]}`,
		"disable flags":    `{"profiles":[{"match":"usr.bin.man","mode":"APPARMOR_MODE_DISABLED","editVendorFlags":true}]}`,
		"bor profile":      `{"profiles":[{"match":"bor-corpapp","mode":"APPARMOR_MODE_ENFORCE"}]}`,
		"reserved":         `{"profiles":[{"match":"local","mode":"APPARMOR_MODE_ENFORCE"}]}`,
		"dup":              `{"profiles":[{"match":"a","mode":"APPARMOR_MODE_ENFORCE"},{"match":"a","mode":"APPARMOR_MODE_COMPLAIN"}]}`,
		"custom name":      `{"customProfiles":[{"name":"Corp","text":"` + apparmorStub + `"}]}`,
		"custom wrong hdr": `{"customProfiles":[{"name":"other","text":"` + apparmorStub + `"}]}`,
		"custom bare":      `{"customProfiles":[{"name":"corpapp","text":"/opt/x {\\n}\\n"}]}`,
		"override bor":     `{"localOverrides":[{"profileFile":"bor-x","text":"/x r,"}]}`,
		"override braces":  `{"localOverrides":[{"profileFile":"usr.sbin.cupsd","text":"}"}]}`,
		"home relative":    `{"extraHomeDirs":["nfs/home/"]}`,
		"home root":        `{"extraHomeDirs":["/"]}`,
	}
	for name, content := range invalid {
		if err := ValidateApparmorContent(content); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestApparmorCustomSummary(t *testing.T) {
	content := `{"customProfiles":[{"name":"corpapp","text":"` + apparmorStub + `"}],"localOverrides":[{"profileFile":"usr.sbin.cupsd","text":"/x r,"}]}`
	if !ApparmorContentHasCustom(content) || !MacContentHasCustom("Apparmor", content) {
		t.Fatal("custom content not detected")
	}
	s := MacCustomSummary("Apparmor", content)
	for _, want := range []string{`"bor-corpapp"`, `"usr.sbin.cupsd"`, `"sha256"`} {
		if !strings.Contains(s, want) {
			t.Errorf("summary %s lacks %s", s, want)
		}
	}
	if MacContentHasCustom("Apparmor", `{"requireEnabled":true}`) || MacContentHasCustom("Firefox", content) {
		t.Error("non-custom content counted as custom")
	}
}
