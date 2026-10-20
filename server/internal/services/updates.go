// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
	"google.golang.org/protobuf/encoding/protojson"
)

// Limits of a Software updates policy.
const (
	maxUpdatesHolds        = 200
	maxUpdatesMessageChars = 512
	maxUpdatesForceDays    = 365
	maxUpdatesWarnMinutes  = 1440
	maxUpdatesLimitDays    = 3650
)

var (
	// A hold is a package name or a shell-style pattern.
	updatesHoldRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+*?-]{0,127}$`)
	updatesWindowRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]-([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// ValidateUpdatesContent validates Software updates policy JSON: the
// enums, the schedule expressions, the reboot rule and the holds.
func ValidateUpdatesContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("software updates policy content is empty")
	}
	var pol pb.UpdatesPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid Software updates policy: %w", err)
	}
	if !updatesPolicyHasSettings(&pol) {
		return fmt.Errorf("software updates policy does not change anything: set a mode, a schedule, a reboot rule or a hold")
	}
	switch pol.GetMode() {
	case pb.UpdatesMode_UPDATES_MODE_UNSPECIFIED, pb.UpdatesMode_UPDATES_MODE_OFF, pb.UpdatesMode_UPDATES_MODE_CHECK,
		pb.UpdatesMode_UPDATES_MODE_SECURITY, pb.UpdatesMode_UPDATES_MODE_ALL:
	default:
		return fmt.Errorf("unknown update mode %d", pol.GetMode())
	}
	if s := pol.GetSchedule(); s != nil {
		if s.GetOnCalendar() == "" {
			if s.GetRandomizedDelay() != "" || s.Persistent != nil {
				return fmt.Errorf("schedule: a calendar expression is required")
			}
		} else {
			if err := systemdunits.ValidateCalendar(s.GetOnCalendar()); err != nil {
				return fmt.Errorf("schedule: %w", err)
			}
			if err := checkCalendarWithSystemdAnalyze(s.GetOnCalendar()); err != nil {
				return fmt.Errorf("schedule: %w", err)
			}
		}
		if v := s.GetRandomizedDelay(); v != "" {
			if err := systemdunits.ValidateTimeSpan(v); err != nil {
				return fmt.Errorf("schedule: randomized delay: %w", err)
			}
		}
	}
	if r := pol.GetReboot(); r != nil {
		switch r.GetPolicy() {
		case pb.UpdatesRebootPolicy_UPDATES_REBOOT_UNSPECIFIED, pb.UpdatesRebootPolicy_UPDATES_REBOOT_NEVER,
			pb.UpdatesRebootPolicy_UPDATES_REBOOT_WHEN_NEEDED, pb.UpdatesRebootPolicy_UPDATES_REBOOT_ALWAYS:
		default:
			return fmt.Errorf("unknown reboot policy %d", r.GetPolicy())
		}
		if w := r.GetWindow(); w != "" {
			if !updatesWindowRE.MatchString(w) {
				return fmt.Errorf("reboot window %q: use HH:MM-HH:MM", w)
			}
			if w[:5] == w[6:] {
				return fmt.Errorf("reboot window %q: start and end are the same", w)
			}
		}
		if d := r.GetForceAfterDays(); d < 0 || d > maxUpdatesForceDays {
			return fmt.Errorf("force after days must be between 0 and %d", maxUpdatesForceDays)
		}
		if m := r.GetWarnMinutes(); m < 0 || m > maxUpdatesWarnMinutes {
			return fmt.Errorf("warning minutes must be between 0 and %d", maxUpdatesWarnMinutes)
		}
		if msg := r.GetMessage(); len(msg) > maxUpdatesMessageChars {
			return fmt.Errorf("the reboot message is longer than %d characters", maxUpdatesMessageChars)
		} else if strings.ContainsAny(msg, "\x00\n\r") {
			return fmt.Errorf("the reboot message must be one line")
		}
	}
	if l := pol.GetLimits(); l != nil {
		for name, v := range map[string]int32{"security": l.GetSecurityMaxAgeDays(), "reboot": l.GetRebootMaxAgeDays(), "check": l.GetCheckMaxAgeDays()} {
			if v < 0 || v > maxUpdatesLimitDays {
				return fmt.Errorf("the %s age limit must be between 0 and %d days", name, maxUpdatesLimitDays)
			}
		}
	}
	if n := len(pol.GetHold()); n > maxUpdatesHolds {
		return fmt.Errorf("%d holds; the limit is %d", n, maxUpdatesHolds)
	}
	seen := map[string]struct{}{}
	for _, h := range pol.GetHold() {
		if !updatesHoldRE.MatchString(h) {
			return fmt.Errorf("hold %q: use a package name or a pattern with * and ?", h)
		}
		if _, dup := seen[h]; dup {
			return fmt.Errorf("hold %q is listed twice", h)
		}
		seen[h] = struct{}{}
	}
	return nil
}

func updatesPolicyHasSettings(p *pb.UpdatesPolicy) bool {
	return p.GetMode() != pb.UpdatesMode_UPDATES_MODE_UNSPECIFIED ||
		(p.GetSchedule() != nil && p.GetSchedule().GetOnCalendar() != "") ||
		(p.GetReboot() != nil && p.GetReboot().GetPolicy() != pb.UpdatesRebootPolicy_UPDATES_REBOOT_UNSPECIFIED) ||
		len(p.GetHold()) > 0 || p.GetRemoveUnusedDependencies() || p.GetRequireEngine() || p.InstallEngine != nil ||
		updatesLimitsSet(p.GetLimits()) || (p.GetFirmware() != nil && (p.GetFirmware().RefreshMetadata != nil || p.GetFirmware().GetReportPending()))
}

func updatesLimitsSet(l *pb.UpdatesLimits) bool {
	return l != nil && (l.GetSecurityMaxAgeDays() > 0 || l.GetRebootMaxAgeDays() > 0 || l.GetCheckMaxAgeDays() > 0)
}

// UpdatesContentHasHolds reports whether a policy holds packages back
// from updates, which is audited: a hold keeps a vulnerable package.
func UpdatesContentHasHolds(policyType, content string) bool {
	if policyType != "Updates" {
		return false
	}
	var pol pb.UpdatesPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return false
	}
	return len(pol.GetHold()) > 0
}

// UpdatesHoldSummary is the audit detail of a policy's holds.
func UpdatesHoldSummary(content string) string {
	var pol pb.UpdatesPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return "[]"
	}
	holds := pol.GetHold()
	if holds == nil {
		holds = []string{}
	}
	data, _ := json.Marshal(holds)
	return string(data)
}
