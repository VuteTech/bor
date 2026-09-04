// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Layer 2 of SessionAccess: render pam_time rules into the managed block of
// /etc/security/time.conf. pam_time denies a login/unlock when the current
// time is outside the rule's allowed set, so each targeted user gets one line
// whose times field is the union of their allowed windows. Each
// SessionAccessWindow maps directly to one pam_time term (days + HHMM-HHMM),
// and pam_time treats a finish time smaller than the start as spilling into
// the next day — matching our midnight-crossing semantics.
//
// pam_time only affects services that (a) include the account stack pam_time
// was added to and (b) match the rule's services field, so cron/sudo/etc. are
// never gated. Unlock is deliberately not blockable this way on GNOME/KDE
// (see docs/session-access-plan.md §11–§12) — the Layer-1 watchdog handles it;
// these rules block fresh logins, VT logins and (optionally) sshd.

const (
	// TimeConfPath is the single file pam_time reads (it has no drop-in dir).
	TimeConfPath = "/etc/security/time.conf"

	// Markers are pure ASCII so encoding normalisation by any tool can never
	// stop them matching (which would leave a stale block and append a
	// second one).
	timeConfBeginMarker = "# BEGIN bor-managed (session access) - managed by Bor, do not edit"
	timeConfEndMarker   = "# END bor-managed (session access)"
	// legacyTimeConfBeginMarker is the pre-release marker (em dash); still
	// recognised so an upgraded node replaces rather than duplicates its block.
	legacyTimeConfBeginMarker = "# BEGIN bor-managed (session access) — managed by Bor, do not edit"
)

// sessionLoginServices are the PAM services gated for a restricted user: the
// display-manager login services and the console login. Listing services not
// present on a node is harmless — pam_time only matches services actually in
// use. sshd is appended per-user when include_ssh is set.
var sessionLoginServices = []string{"login", "gdm-password", "sddm", "lightdm", "xdm"}

// pamDayCode maps a day token to its pam_time two-letter code.
var pamDayCode = map[string]string{
	"mon": "Mo", "tue": "Tu", "wed": "We", "thu": "Th", "fri": "Fr", "sat": "Sa", "sun": "Su",
}

// pamDayOrder fixes the emission order so terms are deterministic.
var pamDayOrder = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// timeConfLineRE validates a rendered managed line as a defence-in-depth
// self-check before the block is written. The user field mirrors the
// server-side target charset (services.ValidateSessionAccessContent): a
// leading "@" is refused because pam_time reads it as a netgroup, while a
// non-leading "@" is allowed for directory-style names such as user@domain.
var timeConfLineRE = regexp.MustCompile(
	`^[A-Za-z0-9_.|-]+;\*;[A-Za-z0-9_.-][A-Za-z0-9_.@-]*;!?(?:(?:Mo|Tu|We|Th|Fr|Sa|Su)+\d{4}-\d{4})(?:\|(?:Mo|Tu|We|Th|Fr|Sa|Su)+\d{4}-\d{4})*$`)

// pamAgg is the per-user aggregation used to build one time.conf line.
type pamAgg struct {
	windows    []*pb.SessionAccessWindow
	includeSSH bool
	enforced   bool // targeted by at least one enforce_pam policy
}

// RenderTimeConfLines builds the managed pam_time lines for every user that a
// bound enforce_pam SessionAccess policy restricts. groupMembers expands a
// group name to its member usernames on this node. It returns the lines (one
// per restricted user, sorted) and any warnings for skipped input. Users with
// no valid window fail open (no line), matching Layer 1.
func RenderTimeConfLines(sources []SessionPolicySource, groupMembers func(group string) []string) (lines, warnings []string) {
	agg := aggregatePamUsers(sources, groupMembers)

	users := make([]string, 0, len(agg))
	for u := range agg {
		users = append(users, u)
	}
	sort.Strings(users)

	for _, user := range users {
		a := agg[user]
		if !a.enforced {
			continue // only Layer-1 policies target this user
		}
		terms := make([]string, 0, len(a.windows))
		for _, w := range a.windows {
			term, err := pamTimeTerm(w)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("user %q: window skipped: %v", user, err))
				continue
			}
			terms = append(terms, term)
		}
		if len(terms) == 0 {
			warnings = append(warnings, fmt.Sprintf("user %q: no valid windows, leaving unrestricted", user))
			continue // fail open
		}
		services := sessionLoginServices
		if a.includeSSH {
			services = append(append([]string(nil), sessionLoginServices...), "sshd")
		}
		line := fmt.Sprintf("%s;*;%s;%s", strings.Join(services, "|"), user, strings.Join(dedupeTerms(terms), "|"))
		if !timeConfLineRE.MatchString(line) {
			warnings = append(warnings, fmt.Sprintf("user %q: generated line failed self-check, skipped", user))
			continue
		}
		lines = append(lines, line)
	}
	return lines, warnings
}

// aggregatePamUsers walks all sources and collects, per user, the union of
// windows targeting them plus whether any enforce_pam policy (and any
// include_ssh policy) applies. root and empty names are never included.
func aggregatePamUsers(sources []SessionPolicySource, groupMembers func(group string) []string) map[string]*pamAgg {
	agg := map[string]*pamAgg{}
	get := func(u string) *pamAgg {
		a := agg[u]
		if a == nil {
			a = &pamAgg{}
			agg[u] = a
		}
		return a
	}
	for _, src := range sources {
		if src.Policy == nil {
			continue
		}
		// Absent enforce_pam defaults to true (Layer 2 on).
		pamOn := src.Policy.EnforcePam == nil || src.Policy.GetEnforcePam()
		ssh := src.Policy.GetIncludeSsh()
		for _, rule := range src.Policy.GetRules() {
			targets := map[string]struct{}{}
			for _, u := range rule.GetUsers() {
				if u != "" && u != "root" {
					targets[u] = struct{}{}
				}
			}
			for _, g := range rule.GetGroups() {
				for _, u := range groupMembers(g) {
					if u != "" && u != "root" {
						targets[u] = struct{}{}
					}
				}
			}
			for u := range targets {
				a := get(u)
				a.windows = append(a.windows, rule.GetWindows()...)
				if pamOn {
					a.enforced = true
					if ssh {
						a.includeSSH = true
					}
				}
			}
		}
	}
	return agg
}

// pamTimeTerm converts one window to a pam_time term, e.g.
// {days:[mon,wed,fri] 08:00 17:00} -> "MoWeFr0800-1700". A window whose end is
// at or before its start is a 24h/overnight window: equal times render as the
// full day (0000-2400); an earlier end relies on pam_time's own "finish <
// start means next day" rule.
func pamTimeTerm(w *pb.SessionAccessWindow) (string, error) {
	if len(w.GetDays()) == 0 {
		return "", fmt.Errorf("no days")
	}
	seen := map[string]bool{}
	var codes strings.Builder
	for _, d := range pamDayOrder {
		for _, wd := range w.GetDays() {
			if wd == d && !seen[d] {
				code, ok := pamDayCode[d]
				if !ok {
					return "", fmt.Errorf("invalid day %q", d)
				}
				codes.WriteString(code)
				seen[d] = true
			}
		}
	}
	for _, wd := range w.GetDays() {
		if _, ok := pamDayCode[wd]; !ok {
			return "", fmt.Errorf("invalid day %q", wd)
		}
	}
	start, err := hhmm(w.GetStart())
	if err != nil {
		return "", fmt.Errorf("start: %w", err)
	}
	end, err := hhmm(w.GetEnd())
	if err != nil {
		return "", fmt.Errorf("end: %w", err)
	}
	if start == end {
		end = "2400" // full-day window
	}
	return codes.String() + start + "-" + end, nil
}

// hhmm converts "HH:MM" to pam_time's "HHMM", validating the range.
func hhmm(s string) (string, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return "", fmt.Errorf("invalid time %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return "", fmt.Errorf("time %q out of range", s)
	}
	return fmt.Sprintf("%02d%02d", h, m), nil
}

// dedupeTerms removes duplicate terms while preserving order.
func dedupeTerms(terms []string) []string {
	seen := map[string]bool{}
	out := terms[:0:0]
	for _, t := range terms {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// MergeTimeConf returns the new content of time.conf with the Bor-managed
// block set to lines, preserving all admin content outside the markers. An
// empty lines slice removes the managed block entirely. The result always
// ends with a single trailing newline. A BEGIN marker without its END marker
// is refused rather than guessed at, so admin rules after a damaged block are
// never silently discarded.
func MergeTimeConf(existing string, lines []string) (string, error) {
	before, after, err := splitAroundManaged(existing)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(before, "\n"))
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	if len(lines) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(timeConfBeginMarker)
		b.WriteString("\n")
		for _, l := range lines {
			b.WriteString(l)
			b.WriteString("\n")
		}
		b.WriteString(timeConfEndMarker)
		b.WriteString("\n")
	}
	tail := strings.TrimLeft(after, "\n")
	if tail != "" {
		b.WriteString(tail)
		if !strings.HasSuffix(tail, "\n") {
			b.WriteString("\n")
		}
	}
	out := b.String()
	if out == "" {
		return "", nil
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

// splitAroundManaged returns the content before the managed BEGIN marker
// (current or legacy) and after the END marker. When no managed block is
// present, before is the whole input and after is empty.
func splitAroundManaged(existing string) (before, after string, err error) {
	beginIdx, beginLen := findBeginMarker(existing)
	if beginIdx < 0 {
		return existing, "", nil
	}
	before = existing[:beginIdx]
	rest := existing[beginIdx+beginLen:]
	endIdx := strings.Index(rest, timeConfEndMarker)
	if endIdx < 0 {
		return "", "", fmt.Errorf("%s: managed block has a BEGIN marker but no END marker; refusing to rewrite — repair the file by hand", TimeConfPath)
	}
	after = rest[endIdx+len(timeConfEndMarker):]
	return before, after, nil
}

// findBeginMarker locates the current or legacy BEGIN marker, returning its
// offset and length, or -1.
func findBeginMarker(content string) (idx, length int) {
	for _, m := range []string{timeConfBeginMarker, legacyTimeConfBeginMarker} {
		if i := strings.Index(content, m); i >= 0 {
			return i, len(m)
		}
	}
	return -1, 0
}

// HasManagedBlock reports whether content contains the Bor-managed block.
func HasManagedBlock(content string) bool {
	idx, _ := findBeginMarker(content)
	return idx >= 0
}
