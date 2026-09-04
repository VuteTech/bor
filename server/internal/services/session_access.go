// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// sessionAccessTimeRE matches a strict 24h "HH:MM" clock time.
var sessionAccessTimeRE = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// sessionAccessTargetRE is the user/group name charset. It is deliberately the
// same set the agent's time.conf self-check accepts, so a name that saves here
// is guaranteed to be enforceable on the node instead of being silently
// skipped there. A leading "@" is refused because pam_time reads it as a
// netgroup; a non-leading "@" is allowed for directory-style names
// (user@domain, as SSSD fully-qualified names produce).
var sessionAccessTargetRE = regexp.MustCompile(`^[A-Za-z0-9_.-][A-Za-z0-9_.@-]*$`)

// sessionAccessDays maps valid day tokens to their index (Monday = 0), the
// order used by the agent's weekly schedule grid.
var sessionAccessDays = map[string]int{
	"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6,
}

// Size caps keep the generated pam_time configuration and the agent's
// schedule grid bounded.
const (
	sessionAccessMaxRules   = 200
	sessionAccessMaxUsers   = 500
	sessionAccessMaxGroups  = 100
	sessionAccessMaxWindows = 100
	sessionAccessMaxWarns   = 10
	sessionAccessMaxName    = 256
	minutesPerDay           = 24 * 60
	minutesPerWeek          = 7 * minutesPerDay
)

// ValidateSessionAccessContent validates session access policy JSON content.
func ValidateSessionAccessContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("session access policy content is empty")
	}
	var pol pb.SessionAccessPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid session access policy: %w", err)
	}

	if len(pol.GetRules()) == 0 {
		return fmt.Errorf("session access policy needs at least one rule")
	}
	if len(pol.GetRules()) > sessionAccessMaxRules {
		return fmt.Errorf("too many rules: %d (maximum %d)", len(pol.GetRules()), sessionAccessMaxRules)
	}

	for i, rule := range pol.GetRules() {
		if err := validateSessionAccessRule(rule); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
	}
	return nil
}

func validateSessionAccessRule(rule *pb.SessionAccessRule) error {
	if len(rule.GetUsers()) == 0 && len(rule.GetGroups()) == 0 {
		return fmt.Errorf("needs at least one target user or group")
	}
	if len(rule.GetUsers()) > sessionAccessMaxUsers {
		return fmt.Errorf("too many users: %d (maximum %d)", len(rule.GetUsers()), sessionAccessMaxUsers)
	}
	if len(rule.GetGroups()) > sessionAccessMaxGroups {
		return fmt.Errorf("too many groups: %d (maximum %d)", len(rule.GetGroups()), sessionAccessMaxGroups)
	}
	for _, u := range rule.GetUsers() {
		if err := validateSessionAccessTarget(u); err != nil {
			return fmt.Errorf("invalid user: %w", err)
		}
		if u == "root" {
			return fmt.Errorf("user \"root\" must not be restricted")
		}
	}
	for _, g := range rule.GetGroups() {
		if err := validateSessionAccessTarget(g); err != nil {
			return fmt.Errorf("invalid group: %w", err)
		}
	}

	if len(rule.GetWindows()) == 0 {
		return fmt.Errorf("needs at least one window")
	}
	if len(rule.GetWindows()) > sessionAccessMaxWindows {
		return fmt.Errorf("too many windows: %d (maximum %d)", len(rule.GetWindows()), sessionAccessMaxWindows)
	}

	// Collect each window as half-open segments on the weekly minute grid
	// (Monday 00:00 = 0) to detect overlaps; touching segments are allowed.
	var segments [][2]int
	for i, w := range rule.GetWindows() {
		segs, err := sessionAccessWindowSegments(w)
		if err != nil {
			return fmt.Errorf("window %d: %w", i+1, err)
		}
		segments = append(segments, segs...)
	}
	sort.Slice(segments, func(a, b int) bool { return segments[a][0] < segments[b][0] })
	for i := 1; i < len(segments); i++ {
		if segments[i][0] < segments[i-1][1] {
			return fmt.Errorf("windows overlap (windows within one rule must not overlap, but may touch)")
		}
	}

	if len(rule.GetWarnMinutes()) > sessionAccessMaxWarns {
		return fmt.Errorf("too many warn thresholds: %d (maximum %d)", len(rule.GetWarnMinutes()), sessionAccessMaxWarns)
	}
	for i, m := range rule.GetWarnMinutes() {
		if m < 1 || m > minutesPerDay {
			return fmt.Errorf("warn minutes must be between 1 and %d, got %d", minutesPerDay, m)
		}
		if i > 0 && m >= rule.GetWarnMinutes()[i-1] {
			return fmt.Errorf("warn minutes must be strictly descending (e.g. [30, 15, 5])")
		}
	}
	return nil
}

// validateSessionAccessTarget validates a user or group name against the
// charset the agent will accept when it writes /etc/security/time.conf (see
// sessionAccessTargetRE), so problems surface at save time rather than as a
// silent skip on the node.
func validateSessionAccessTarget(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(name) > sessionAccessMaxName {
		return fmt.Errorf("name %q is too long (maximum %d characters)", name, sessionAccessMaxName)
	}
	if strings.HasPrefix(name, "@") {
		return fmt.Errorf("name %q must not start with \"@\" (pam_time treats that as a netgroup)", name)
	}
	if !sessionAccessTargetRE.MatchString(name) {
		return fmt.Errorf("name %q may only contain letters, digits, \".\", \"_\", \"-\" and a non-leading \"@\"", name)
	}
	return nil
}

// sessionAccessWindowSegments validates a single window and expands it to
// half-open [start, end) segments on the weekly minute grid, splitting at
// midnight and at the week boundary. An end at or before the start crosses
// midnight into the next day; equal start and end is a full 24h window.
func sessionAccessWindowSegments(w *pb.SessionAccessWindow) ([][2]int, error) {
	if len(w.GetDays()) == 0 {
		return nil, fmt.Errorf("needs at least one day")
	}
	seen := map[string]bool{}
	for _, d := range w.GetDays() {
		if _, ok := sessionAccessDays[d]; !ok {
			return nil, fmt.Errorf("invalid day %q: must be one of mon, tue, wed, thu, fri, sat, sun", d)
		}
		if seen[d] {
			return nil, fmt.Errorf("duplicate day %q", d)
		}
		seen[d] = true
	}
	start, err := parseSessionAccessTime(w.GetStart())
	if err != nil {
		return nil, fmt.Errorf("invalid start: %w", err)
	}
	end, err := parseSessionAccessTime(w.GetEnd())
	if err != nil {
		return nil, fmt.Errorf("invalid end: %w", err)
	}

	length := end - start
	if length <= 0 {
		length += minutesPerDay // crosses midnight; equal start/end = 24h
	}
	var segments [][2]int
	for _, d := range w.GetDays() {
		s := sessionAccessDays[d]*minutesPerDay + start
		e := s + length
		if e <= minutesPerWeek {
			segments = append(segments, [2]int{s, e})
		} else {
			segments = append(segments, [2]int{s, minutesPerWeek}, [2]int{0, e - minutesPerWeek})
		}
	}
	return segments, nil
}

// parseSessionAccessTime parses a strict "HH:MM" into minutes since 00:00.
func parseSessionAccessTime(s string) (int, error) {
	if !sessionAccessTimeRE.MatchString(s) {
		return 0, fmt.Errorf("time %q must be HH:MM (24h)", s)
	}
	return int(s[0]-'0')*600 + int(s[1]-'0')*60 + int(s[3]-'0')*10 + int(s[4]-'0'), nil
}
