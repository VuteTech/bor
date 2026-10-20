// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestValidateSystemdContent(t *testing.T) {
	// Keep the optional systemd-analyze check deterministic.
	systemdAnalyzeOnce.Do(func() {})
	saved := systemdAnalyzeRunner
	systemdAnalyzeRunner = func(expr string) error {
		if strings.Contains(expr, "Funday") {
			return fmt.Errorf("Failed to parse calendar specification")
		}
		return nil
	}
	defer func() { systemdAnalyzeRunner = saved }()

	valid := map[string]string{
		"mask":            `{"units":[{"name":"avahi-daemon.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"disable stop":    `{"units":[{"name":"bluetooth.service","enablement":"SYSTEMD_ENABLEMENT_DISABLED","activity":"SYSTEMD_ACTIVITY_STOPPED"}]}`,
		"timer schedule":  `{"units":[{"name":"fstrim.timer","enablement":"SYSTEMD_ENABLEMENT_ENABLED","activity":"SYSTEMD_ACTIVITY_RUNNING","requireLastRunSuccess":true,"dropIn":{"timer":{"onCalendar":["Sat *-*-* 03:00:00"],"persistent":true,"randomizedDelaySec":"1h"}}}]}`,
		"service limits":  `{"units":[{"name":"cups.service","restartOnChange":true,"dropIn":{"service":{"restart":"on-failure","restartSec":"5s","memoryMax":"512M","cpuQuota":"50%","tasksMax":"200","nice":"5","environment":["FOO=bar baz"],"protectSystem":"full","protectHome":"read-only","privateTmp":true,"noNewPrivileges":true}}}]}`,
		"target mask":     `{"units":[{"name":"ctrl-alt-del.target","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"socket":          `{"units":[{"name":"cups.socket","enablement":"SYSTEMD_ENABLEMENT_DISABLED","activity":"SYSTEMD_ACTIVITY_STOPPED"}]}`,
		"last run only":   `{"units":[{"name":"backup.timer","requireLastRunSuccess":true}]}`,
		"instance":        `{"units":[{"name":"getty@tty2.service","enablement":"SYSTEMD_ENABLEMENT_ENABLED"}]}`,
		"numeric enum":    `{"units":[{"name":"bluetooth.service","enablement":2}]}`,
		"monotonic timer": `{"units":[{"name":"backup.timer","dropIn":{"timer":{"onBootSec":"15min","onUnitActiveSec":"1h"}}}]}`,
	}
	for name, content := range valid {
		if err := ValidateSystemdContent(content); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}

	invalid := map[string]string{
		"empty":               ``,
		"empty object":        `{}`,
		"bad json":            `{"units":[`,
		"no units":            `{"units":[]}`,
		"no settings":         `{"units":[{"name":"bluetooth.service"}]}`,
		"bad name":            `{"units":[{"name":"../x.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"slash":               `{"units":[{"name":"foo/bar.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"mount":               `{"units":[{"name":"home.mount","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"no suffix":           `{"units":[{"name":"bluetooth","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"protected":           `{"units":[{"name":"bor-agent.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"protected target":    `{"units":[{"name":"multi-user.target","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`,
		"template":            `{"units":[{"name":"getty@.service","enablement":"SYSTEMD_ENABLEMENT_ENABLED"}]}`,
		"masked running":      `{"units":[{"name":"bluetooth.service","enablement":"SYSTEMD_ENABLEMENT_MASKED","activity":"SYSTEMD_ACTIVITY_RUNNING"}]}`,
		"target activity":     `{"units":[{"name":"ctrl-alt-del.target","activity":"SYSTEMD_ACTIVITY_STOPPED"}]}`,
		"duplicate":           `{"units":[{"name":"bluetooth.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"},{"name":"bluetooth.service","activity":"SYSTEMD_ACTIVITY_STOPPED"}]}`,
		"unknown enum":        `{"units":[{"name":"bluetooth.service","enablement":"SYSTEMD_ENABLEMENT_PURGED"}]}`,
		"enum out of range":   `{"units":[{"name":"bluetooth.service","enablement":9}]}`,
		"last run on socket":  `{"units":[{"name":"cups.socket","requireLastRunSuccess":true}]}`,
		"restart on timer":    `{"units":[{"name":"fstrim.timer","restartOnChange":true,"enablement":"SYSTEMD_ENABLEMENT_ENABLED"}]}`,
		"timer on service":    `{"units":[{"name":"cups.service","dropIn":{"timer":{"onCalendar":["daily"]}}}]}`,
		"service on timer":    `{"units":[{"name":"fstrim.timer","dropIn":{"service":{"restart":"always"}}}]}`,
		"bad calendar chars":  `{"units":[{"name":"fstrim.timer","dropIn":{"timer":{"onCalendar":["daily; rm -rf /"]}}}]}`,
		"bad calendar syntax": `{"units":[{"name":"fstrim.timer","dropIn":{"timer":{"onCalendar":["Funday 03:00"]}}}]}`,
		"bad span":            `{"units":[{"name":"fstrim.timer","dropIn":{"timer":{"randomizedDelaySec":"an hour"}}}]}`,
		"bad restart":         `{"units":[{"name":"cups.service","dropIn":{"service":{"restart":"sometimes"}}}]}`,
		"bad restart sec":     `{"units":[{"name":"cups.service","dropIn":{"service":{"restartSec":"5 lightyears"}}}]}`,
		"bad memory":          `{"units":[{"name":"cups.service","dropIn":{"service":{"memoryMax":"lots"}}}]}`,
		"bad cpu":             `{"units":[{"name":"cups.service","dropIn":{"service":{"cpuQuota":"50"}}}]}`,
		"bad nice":            `{"units":[{"name":"cups.service","dropIn":{"service":{"nice":"42"}}}]}`,
		"bad env":             `{"units":[{"name":"cups.service","dropIn":{"service":{"environment":["1BAD=x"]}}}]}`,
		"env newline":         `{"units":[{"name":"cups.service","dropIn":{"service":{"environment":["A=b\nExecStart=/bin/sh"]}}}]}`,
		"bad protect system":  `{"units":[{"name":"cups.service","dropIn":{"service":{"protectSystem":"maybe"}}}]}`,
		"bad protect home":    `{"units":[{"name":"cups.service","dropIn":{"service":{"protectHome":"full"}}}]}`,
	}
	for name, content := range invalid {
		if err := ValidateSystemdContent(content); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}

	// Limits.
	var sb strings.Builder
	sb.WriteString(`{"units":[`)
	for i := 0; i <= maxSystemdUnits; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"name":"unit%d.service","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}`, i)
	}
	sb.WriteString("]}")
	if err := ValidateSystemdContent(sb.String()); err == nil {
		t.Error("too many units: expected error")
	}
	long := strings.Repeat("*", 200)
	if err := ValidateSystemdContent(`{"units":[{"name":"x.timer","dropIn":{"timer":{"onCalendar":["` + long + `"]}}}]}`); err == nil {
		t.Error("long calendar: expected error")
	}
}

func TestValidateSystemdContentErrorNamesUnit(t *testing.T) {
	err := ValidateSystemdContent(`{"units":[{"name":"bor-agent.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`)
	if err == nil || !strings.Contains(err.Error(), `"bor-agent.service"`) || !strings.Contains(err.Error(), "protected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSystemdTasksAndRaw(t *testing.T) {
	systemdAnalyzeOnce.Do(func() {})
	saved := systemdAnalyzeRunner
	systemdAnalyzeRunner = func(string) error { return nil }
	defer func() { systemdAnalyzeRunner = saved }()

	script := "#!/bin/sh\nrm -rf /var/cache/bor-lab/*\n"
	valid := map[string]string{
		"timer task":      `{"tasks":[{"name":"nightly-cleanup","trigger":"SYSTEMD_TASK_TRIGGER_TIMER","schedule":{"onCalendar":["*-*-* 02:00:00"],"persistent":true,"randomizedDelaySec":"30min"},"script":"` + strings.ReplaceAll(script, "\n", `\n`) + `","requireLastRunSuccess":true}]}`,
		"boot argv task":  `{"tasks":[{"name":"fix-perms","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/usr/bin/chmod","0755","/srv/share"],"user":"root","workingDirectory":"/srv","environment":["LANG=C"],"timeoutStartSec":"10min","hardened":false,"enabled":false}]}`,
		"tasks and units": `{"units":[{"name":"bluetooth.service","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}],"tasks":[{"name":"t","trigger":"SYSTEMD_TASK_TRIGGER_TIMER","schedule":{"onBootSec":"5min","onUnitActiveSec":"1h"},"execStart":["/bin/true"]}],"writePresets":true}`,
		"raw directives":  `{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"IPAddressDeny","values":["any"]},{"section":"Unit","key":"StartLimitBurst","values":["3"]},{"section":"Service","key":"ReadWritePaths","values":[]}]}}]}`,
		"raw exec":        `{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"ExecStartPre","values":["/usr/bin/true"]}]}}]}`,
		"confined task":   `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"apparmorProfile":"-bor-corpapp","selinuxContext":"system_u:system_r:bor_task_t:s0"}]}`,
	}
	for name, content := range valid {
		if err := ValidateSystemdContent(content); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}

	invalid := map[string]string{
		"empty":                  `{"tasks":[]}`,
		"bad task name":          `{"tasks":[{"name":"Nightly","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"#!/bin/sh\n"}]}`,
		"bor prefix typed":       `{"tasks":[{"name":"bor-x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"#!/bin/sh\n"}]}`,
		"no trigger":             `{"tasks":[{"name":"x","script":"#!/bin/sh\n"}]}`,
		"timer without schedule": `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_TIMER","schedule":{"persistent":true},"script":"#!/bin/sh\n"}]}`,
		"boot with schedule":     `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","schedule":{"onCalendar":["daily"]},"script":"#!/bin/sh\n"}]}`,
		"no program":             `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT"}]}`,
		"both script and argv":   `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"#!/bin/sh\n","execStart":["/bin/true"]}]}`,
		"no shebang":             `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"echo hi\n"}]}`,
		"relative program":       `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["true"]}]}`,
		"newline in arg":         `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true","a\nb"]}]}`,
		"bad user":               `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"user":"Root User"}]}`,
		"relative workdir":       `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"workingDirectory":"srv"}]}`,
		"bad env":                `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"environment":["1BAD=x"]}]}`,
		"bad timeout":            `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"timeoutStartSec":"soon"}]}`,
		"bad apparmor profile":   `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"apparmorProfile":"bor corpapp"}]}`,
		"bad selinux context":    `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"],"selinuxContext":"bor_task_t"}]}`,
		"duplicate task":         `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"]},{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/false"]}]}`,
		"presets without unit":   `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"]}],"writePresets":true}`,
		"presets without enable": `{"units":[{"name":"cups.service","activity":"SYSTEMD_ACTIVITY_STOPPED"}],"writePresets":true}`,
		"raw bad section":        `{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Mount","key":"What","values":["x"]}]}}]}`,
		"raw section type":       `{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Timer","key":"OnCalendar","values":["daily"]}]}}]}`,
		"raw bad key":            `{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"exec start","values":["x"]}]}}]}`,
		"raw newline":            `{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"Environment","values":["A=1\nB=2"]}]}}]}`,
	}
	for name, content := range invalid {
		if err := ValidateSystemdContent(content); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	big := `{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"#!/bin/sh\n` + strings.Repeat("#", 65*1024) + `"}]}`
	if err := ValidateSystemdContent(big); err == nil || !strings.Contains(err.Error(), "larger than 64 KiB") {
		t.Errorf("oversized script: %v", err)
	}
}

func TestSystemdContentRunsCommands(t *testing.T) {
	cases := map[string]bool{
		`{"units":[{"name":"cups.service","enablement":"SYSTEMD_ENABLEMENT_MASKED"}]}`:                                                 false,
		`{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"IPAddressDeny","values":["any"]}]}}]}`:          false,
		`{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"ExecStartPre","values":["/usr/bin/true"]}]}}]}`: true,
		`{"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","execStart":["/bin/true"]}]}`:                                     true,
		`not json`: false,
	}
	for content, want := range cases {
		if got := SystemdContentRunsCommands(content); got != want {
			t.Errorf("SystemdContentRunsCommands(%s) = %v, want %v", content, got, want)
		}
	}
	summary := SystemdCommandSummary(`{"units":[{"name":"cups.service","dropIn":{"raw":[{"section":"Service","key":"ExecStartPre","values":["/usr/bin/true"]}]}}],"tasks":[{"name":"x","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"#!/bin/sh\n"}]}`)
	if !strings.Contains(summary, `"name":"x"`) || !strings.Contains(summary, `"sha256":"`) || !strings.Contains(summary, `"cups.service: ExecStartPre"`) {
		t.Errorf("summary = %s", summary)
	}
}

func TestPreviewSystemdCalendar(t *testing.T) {
	systemdAnalyzeOnce.Do(func() {})
	saved := systemdAnalyzePreview
	defer func() { systemdAnalyzePreview = saved }()

	systemdAnalyzePreview = nil
	res, err := PreviewSystemdCalendar("Sat *-*-* 03:00:00", 5)
	if err != nil || res.Available {
		t.Fatalf("without the binary: %+v, %v", res, err)
	}

	systemdAnalyzePreview = func(expr string, iterations int) ([]byte, error) {
		if iterations != 3 {
			t.Errorf("iterations = %d", iterations)
		}
		if strings.Contains(expr, "25:00") {
			return []byte("Failed to parse calendar specification '" + expr + "': Invalid argument\n"), fmt.Errorf("exit status 1")
		}
		return []byte("  Original form: daily\nNormalized form: *-*-* 00:00:00\n    Next elapse: Mon 2026-10-19 00:00:00 EEST\n       (in UTC): Sun 2026-10-18 21:00:00 UTC\n       From now: 20h left\n   Iteration #2: Tue 2026-10-20 00:00:00 EEST\n       (in UTC): Mon 2026-10-19 21:00:00 UTC\n   Iteration #3: Wed 2026-10-21 00:00:00 EEST\n"), nil
	}
	res, err = PreviewSystemdCalendar("daily", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Available || res.Normalized != "*-*-* 00:00:00" || len(res.Next) != 3 || res.Next[0] != "Mon 2026-10-19 00:00:00 EEST" || res.Next[2] != "Wed 2026-10-21 00:00:00 EEST" {
		t.Errorf("preview = %+v", res)
	}
	if _, err := PreviewSystemdCalendar("Sat 25:00", 3); err == nil || !strings.Contains(err.Error(), "Failed to parse") {
		t.Errorf("bad expression: %v", err)
	}
	if _, err := PreviewSystemdCalendar("rm -rf /; daily", 3); err == nil {
		t.Error("charset violation must be rejected before the binary runs")
	}
}

func TestValidateSystemdScopes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "same unit in both scopes",
			content: `{"units":[{"name":"dbus-broker.service","activity":"SYSTEMD_ACTIVITY_RUNNING"},{"name":"syncthing.service","scope":"SYSTEMD_SCOPE_GLOBAL","enablement":"SYSTEMD_ENABLEMENT_ENABLED"},{"name":"syncthing.service","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}]}`,
			wantErr: "dbus-broker.service",
		},
		{
			name:    "two scopes of one unit are distinct",
			content: `{"units":[{"name":"syncthing.service","scope":"SYSTEMD_SCOPE_GLOBAL","enablement":"SYSTEMD_ENABLEMENT_ENABLED"},{"name":"syncthing.service","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}]}`,
		},
		{
			name:    "same scope twice",
			content: `{"units":[{"name":"syncthing.service","scope":"SYSTEMD_SCOPE_GLOBAL","enablement":"SYSTEMD_ENABLEMENT_ENABLED"},{"name":"syncthing.service","scope":"SYSTEMD_SCOPE_GLOBAL","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}]}`,
			wantErr: "listed twice",
		},
		{
			name:    "user-scope protected target",
			content: `{"units":[{"name":"graphical-session.target","scope":"SYSTEMD_SCOPE_GLOBAL","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}]}`,
			wantErr: "protected in the user scope",
		},
		{
			name:    "user-scope protection does not apply to the system scope",
			content: `{"units":[{"name":"sockets.target","enablement":"SYSTEMD_ENABLEMENT_DISABLED"}]}`,
		},
		{
			name:    "global task with a user",
			content: `{"tasks":[{"name":"cache-clean","scope":"SYSTEMD_SCOPE_GLOBAL","trigger":"SYSTEMD_TASK_TRIGGER_TIMER","schedule":{"onCalendar":["daily"]},"script":"#!/bin/sh\ntrue\n","user":"alice"}]}`,
			wantErr: "leave user and group empty",
		},
		{
			name:    "global task at login",
			content: `{"tasks":[{"name":"cache-clean","scope":"SYSTEMD_SCOPE_GLOBAL","trigger":"SYSTEMD_TASK_TRIGGER_BOOT","script":"#!/bin/sh\ntrue\n"}]}`,
		},
		{
			name:    "unknown scope value",
			content: `{"units":[{"name":"cups.service","scope":7,"activity":"SYSTEMD_ACTIVITY_RUNNING"}]}`,
			wantErr: "unknown scope",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSystemdContent(tc.content)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
	if SystemdScopeKey(pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL, "x.service") != "global/x.service" || SystemdScopeKey(pb.SystemdScope_SYSTEMD_SCOPE_SYSTEM, "x.service") != "x.service" {
		t.Error("SystemdScopeKey")
	}
}
