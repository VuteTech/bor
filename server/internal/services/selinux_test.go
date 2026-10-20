// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateSelinuxContent(t *testing.T) {
	valid := map[string]string{
		"mode only":      `{"mode":"SELINUX_MODE_ENFORCING"}`,
		"require active": `{"requireActive":true}`,
		"numeric mode":   `{"mode":1}`,
		"booleans":       `{"booleans":[{"name":"deny_ptrace","value":true},{"name":"use_nfs_home_dirs","value":true,"description":"NFS homes"}]}`,
		"permissive":     `{"permissiveDomains":["virt_qemu_ga_t"]}`,
		"modules":        `{"modules":[{"name":"abrt","enabled":false}]}`,
		"custom":         `{"customModules":[{"name":"corpapp","cil":"(typepermissive corpapp_t)"}]}`,
		"fcontext":       `{"fileContexts":[{"pathRegex":"/opt/corpapp(/.*)?","type":"bin_t","kind":"SELINUX_FILE_KIND_UNSPECIFIED","restorecon":true}]}`,
		"login":          `{"logins":[{"login":"kiosk","seuser":"user_u","range":"s0"}]}`,
		"prepare":        `{"mode":"SELINUX_MODE_PERMISSIVE","prepareDisabledNodes":true}`,
	}
	for name, content := range valid {
		if err := ValidateSelinuxContent(content); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
	invalid := map[string]string{
		"empty":             ``,
		"empty object":      `{}`,
		"bad json":          `{"mode":`,
		"nothing set":       `{"mode":"SELINUX_MODE_UNSPECIFIED","booleans":[]}`,
		"prepare no mode":   `{"prepareDisabledNodes":true}`,
		"protected boolean": `{"booleans":[{"name":"secure_mode_policyload","value":true}]}`,
		"bad boolean name":  `{"booleans":[{"name":"Deny Ptrace","value":true}]}`,
		"dup boolean":       `{"booleans":[{"name":"deny_ptrace","value":true},{"name":"deny_ptrace","value":false}]}`,
		"bad domain":        `{"permissiveDomains":["httpd"]}`,
		"bor module":        `{"modules":[{"name":"bor-x","enabled":false}]}`,
		"custom bad name":   `{"customModules":[{"name":"bor-corp","cil":"(typepermissive corpapp_t)"}]}`,
		"custom bad cil":    `{"customModules":[{"name":"corp","cil":"(class file (read))"}]}`,
		"custom unbalanced": `{"customModules":[{"name":"corp","cil":"(allow a_t b_t (file (read))"}]}`,
		"fcontext root":     `{"fileContexts":[{"pathRegex":"/usr(/.*)?","type":"bin_t"}]}`,
		"fcontext type":     `{"fileContexts":[{"pathRegex":"/opt/x(/.*)?","type":"bin"}]}`,
		"login default":     `{"logins":[{"login":"__default__","seuser":"user_u"}]}`,
		"login seuser":      `{"logins":[{"login":"kiosk","seuser":"system_u"}]}`,
		"login range":       `{"logins":[{"login":"kiosk","seuser":"user_u","range":"high"}]}`,
		"too many":          `{"permissiveDomains":[` + strings.Repeat(`"a_t",`, 64) + `"b_t"]}`,
	}
	for name, content := range invalid {
		if err := ValidateSelinuxContent(content); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSelinuxCustomSummary(t *testing.T) {
	content := `{"customModules":[{"name":"corp","cil":"(typepermissive corp_t)"}],"permissiveDomains":["x_t"],"fileContexts":[{"pathRegex":"/opt/x(/.*)?","type":"bin_t"}]}`
	if !SelinuxContentHasCustom(content) {
		t.Fatal("custom content not detected")
	}
	s := SelinuxCustomSummary(content)
	for _, want := range []string{`"bor-corp"`, `"sha256"`, `"x_t"`, `"file_contexts":1`} {
		if !strings.Contains(s, want) {
			t.Errorf("summary %s lacks %s", s, want)
		}
	}
	if SelinuxContentHasCustom(`{"mode":"SELINUX_MODE_ENFORCING"}`) {
		t.Error("mode-only policy counted as custom")
	}
}
