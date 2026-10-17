// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"strings"
	"testing"
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
