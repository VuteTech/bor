// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateUpdatesContent(t *testing.T) {
	systemdAnalyzeOnce.Do(func() {})
	saved := systemdAnalyzeRunner
	systemdAnalyzeRunner = func(string) error { return nil }
	defer func() { systemdAnalyzeRunner = saved }()

	valid := map[string]string{
		"mode only":   `{"mode":"UPDATES_MODE_SECURITY"}`,
		"full":        `{"mode":"UPDATES_MODE_SECURITY","schedule":{"onCalendar":"*-*-* 02:00","randomizedDelay":"1h","persistent":true},"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","window":"03:00-05:00","forceAfterDays":7,"warnMinutes":10,"message":"Restarting soon"},"hold":["kernel*","firefox"],"removeUnusedDependencies":true}`,
		"reboot only": `{"reboot":{"policy":"UPDATES_REBOOT_NEVER"}}`,
		"holds only":  `{"hold":["vim"]}`,
		"wrap window": `{"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","window":"22:00-04:00"}}`,
		"engine off":  `{"installEngine":false}`,
		"check mode":  `{"mode":"UPDATES_MODE_CHECK","requireEngine":true}`,
		"limits only": `{"limits":{"securityMaxAgeDays":14,"rebootMaxAgeDays":7,"checkMaxAgeDays":3}}`,
		"offline":     `{"mode":"UPDATES_MODE_ALL","offline":true}`,
		"firmware":    `{"firmware":{"refreshMetadata":true,"reportPending":true}}`,
	}
	for name, content := range valid {
		if err := ValidateUpdatesContent(content); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
	invalid := map[string]string{
		"empty":          `{}`,
		"nothing set":    `{"mode":"UPDATES_MODE_UNSPECIFIED"}`,
		"limit too big":  `{"limits":{"securityMaxAgeDays":4000}}`,
		"offline check":  `{"mode":"UPDATES_MODE_CHECK","offline":true}`,
		"limit negative": `{"mode":"UPDATES_MODE_ALL","limits":{"checkMaxAgeDays":-1}}`,
		"bad mode":       `{"mode":"UPDATES_MODE_YES"}`,
		"delay no cal":   `{"schedule":{"randomizedDelay":"1h"}}`,
		"bad delay":      `{"schedule":{"onCalendar":"daily","randomizedDelay":"soon"}}`,
		"bad calendar":   `{"schedule":{"onCalendar":"every now and then\n"}}`,
		"bad window":     `{"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","window":"2-5"}}`,
		"same window":    `{"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","window":"02:00-02:00"}}`,
		"force too long": `{"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","forceAfterDays":400}}`,
		"warn too long":  `{"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","warnMinutes":5000}}`,
		"multiline msg":  `{"reboot":{"policy":"UPDATES_REBOOT_WHEN_NEEDED","message":"a\nb"}}`,
		"bad hold":       `{"hold":["bad name"]}`,
		"duplicate hold": `{"hold":["vim","vim"]}`,
		"unknown field":  `{"mode":"UPDATES_MODE_ALL","bogus":1}`,
	}
	for name, content := range invalid {
		if err := ValidateUpdatesContent(content); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if !UpdatesContentHasHolds("Updates", `{"hold":["vim"]}`) || UpdatesContentHasHolds("Updates", `{"mode":"UPDATES_MODE_ALL"}`) || UpdatesContentHasHolds("Selinux", `{"hold":["x"]}`) {
		t.Error("UpdatesContentHasHolds wrong")
	}
	if s := UpdatesHoldSummary(`{"hold":["vim","kernel*"]}`); !strings.Contains(s, `"kernel*"`) {
		t.Errorf("UpdatesHoldSummary = %s", s)
	}
}
