// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"sort"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// The session access schedule engine works on a weekly minute grid:
// Monday 00:00 = 0, half-open intervals [start, end), one week =
// saMinutesPerWeek. All times are node-local wall clock; conversions back
// to time.Time go through time.Date so DST transitions resolve by wall
// clock (a boundary inside a DST gap normalizes to the next valid time).

const (
	saMinutesPerDay  = 24 * 60
	saMinutesPerWeek = 7 * saMinutesPerDay
)

// DefaultSessionWarnMinutes is used when a rule specifies no warn_minutes.
var DefaultSessionWarnMinutes = []int{30, 15, 5}

// sessionDayIndex maps day tokens to their weekly index (Monday = 0).
var sessionDayIndex = map[string]int{
	"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6,
}

// sessionSegment is a half-open [start, end) interval on the weekly grid,
// already split at the week boundary (0 <= start < end <= saMinutesPerWeek).
type sessionSegment struct {
	start, end int
}

// SessionPolicySource pairs a SessionAccessPolicy with its display name so
// compile warnings can identify the offending policy in compliance items.
type SessionPolicySource struct {
	Name   string
	Policy *pb.SessionAccessPolicy
}

// SessionRule is one compiled SessionAccessRule: resolved end action,
// normalized warn thresholds and the rule's windows as weekly segments.
type SessionRule struct {
	Users    map[string]struct{}
	Groups   map[string]struct{}
	Action   pb.SessionEndAction
	Warn     []int // strictly descending, deduplicated
	segments []sessionSegment
}

// Matches reports whether the rule targets the given user, directly or via
// one of the user's groups.
func (r *SessionRule) Matches(user string, groups []string) bool {
	if _, ok := r.Users[user]; ok {
		return true
	}
	for _, g := range groups {
		if _, ok := r.Groups[g]; ok {
			return true
		}
	}
	return false
}

// CompileSessionAccess compiles all bound SessionAccessPolicies into rules.
// The server validates policies at save time, so malformed input here is
// unexpected; the engine stays defensive and fails open: a rule whose
// windows are all invalid is dropped entirely (the user stays unrestricted
// rather than locked out around the clock), and every skipped element is
// reported as a warning for the compliance report. "root" is never
// restricted and is silently dropped from user lists.
func CompileSessionAccess(sources []SessionPolicySource) (rules []SessionRule, warnings []string) {
	for _, src := range sources {
		for ri, rule := range src.Policy.GetRules() {
			cr := SessionRule{
				Users:  make(map[string]struct{}),
				Groups: make(map[string]struct{}),
				Action: rule.GetEndAction(),
				Warn:   normalizeSessionWarn(rule.GetWarnMinutes()),
			}
			if cr.Action == pb.SessionEndAction_SESSION_END_ACTION_UNSPECIFIED {
				cr.Action = pb.SessionEndAction_SESSION_END_ACTION_LOCK
			}
			for _, u := range rule.GetUsers() {
				if u == "root" || u == "" {
					continue
				}
				cr.Users[u] = struct{}{}
			}
			for _, g := range rule.GetGroups() {
				if g != "" {
					cr.Groups[g] = struct{}{}
				}
			}
			if len(cr.Users) == 0 && len(cr.Groups) == 0 {
				continue
			}
			for wi, w := range rule.GetWindows() {
				segs, err := sessionWindowSegments(w)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("policy %q rule %d window %d skipped: %v", src.Name, ri+1, wi+1, err))
					continue
				}
				cr.segments = append(cr.segments, segs...)
			}
			if len(cr.segments) == 0 {
				warnings = append(warnings, fmt.Sprintf("policy %q rule %d skipped: no valid windows", src.Name, ri+1))
				continue
			}
			rules = append(rules, cr)
		}
	}
	return rules, warnings
}

// sessionWindowSegments expands one window into weekly segments. An end at
// or before the start crosses midnight; equal start and end is a full 24h
// window. Segments crossing the week boundary are split.
func sessionWindowSegments(w *pb.SessionAccessWindow) ([]sessionSegment, error) {
	if len(w.GetDays()) == 0 {
		return nil, fmt.Errorf("no days")
	}
	start, err := parseSessionTime(w.GetStart())
	if err != nil {
		return nil, err
	}
	end, err := parseSessionTime(w.GetEnd())
	if err != nil {
		return nil, err
	}
	length := end - start
	if length <= 0 {
		length += saMinutesPerDay
	}
	var segs []sessionSegment
	for _, d := range w.GetDays() {
		di, ok := sessionDayIndex[d]
		if !ok {
			return nil, fmt.Errorf("invalid day %q", d)
		}
		s := di*saMinutesPerDay + start
		e := s + length
		if e <= saMinutesPerWeek {
			segs = append(segs, sessionSegment{s, e})
		} else {
			segs = append(segs, sessionSegment{s, saMinutesPerWeek}, sessionSegment{0, e - saMinutesPerWeek})
		}
	}
	return segs, nil
}

// parseSessionTime parses "HH:MM" into minutes since midnight.
func parseSessionTime(s string) (int, error) {
	var hh, mm int
	if _, err := fmt.Sscanf(s, "%d:%d", &hh, &mm); err != nil {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("time %q out of range", s)
	}
	return hh*60 + mm, nil
}

// normalizeSessionWarn deduplicates and sorts warn thresholds descending;
// empty input yields the default ladder.
func normalizeSessionWarn(warn []int32) []int {
	if len(warn) == 0 {
		return append([]int(nil), DefaultSessionWarnMinutes...)
	}
	seen := make(map[int]struct{}, len(warn))
	var out []int
	for _, m := range warn {
		v := int(m)
		if v < 1 {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	if len(out) == 0 {
		return append([]int(nil), DefaultSessionWarnMinutes...)
	}
	return out
}

// scheduleWindow is a merged allowed interval of a single user's schedule.
type scheduleWindow struct {
	start, end int
	action     pb.SessionEndAction
	warn       []int
}

// UserSchedule is the union of all windows from every rule targeting one
// user: sorted, disjoint, non-touching merged intervals on the weekly grid.
// Two intervals may still touch across the week boundary (Sunday midnight);
// queries join them so no spurious boundary fires there.
type UserSchedule struct {
	windows []scheduleWindow
}

// UserScheduleFor merges every matching rule's windows for the given user
// (with their group memberships). Returns nil when no rule targets the
// user — the user is unrestricted.
func UserScheduleFor(rules []SessionRule, user string, groups []string) *UserSchedule {
	var raw []scheduleWindow
	for i := range rules {
		r := &rules[i]
		if !r.Matches(user, groups) {
			continue
		}
		for _, s := range r.segments {
			raw = append(raw, scheduleWindow{start: s.start, end: s.end, action: r.Action, warn: r.Warn})
		}
	}
	if len(raw) == 0 {
		return nil
	}
	sort.Slice(raw, func(a, b int) bool {
		if raw[a].start != raw[b].start {
			return raw[a].start < raw[b].start
		}
		return raw[a].end < raw[b].end
	})

	// Sweep-merge overlapping or touching intervals. The merged interval's
	// end action belongs to the interval that defines the merged end; when
	// two rules end at the same minute, LOCK (the less destructive action)
	// wins. Warn thresholds are unioned across all contributors.
	merged := []scheduleWindow{{start: raw[0].start, end: raw[0].end, action: raw[0].action, warn: append([]int(nil), raw[0].warn...)}}
	for _, n := range raw[1:] {
		cur := &merged[len(merged)-1]
		if n.start > cur.end {
			merged = append(merged, scheduleWindow{start: n.start, end: n.end, action: n.action, warn: append([]int(nil), n.warn...)})
			continue
		}
		switch {
		case n.end > cur.end:
			cur.end = n.end
			cur.action = n.action
		case n.end == cur.end:
			cur.action = preferLock(cur.action, n.action)
		}
		cur.warn = unionSessionWarn(cur.warn, n.warn)
	}
	return &UserSchedule{windows: merged}
}

func preferLock(a, b pb.SessionEndAction) pb.SessionEndAction {
	if a == pb.SessionEndAction_SESSION_END_ACTION_LOCK || b == pb.SessionEndAction_SESSION_END_ACTION_LOCK {
		return pb.SessionEndAction_SESSION_END_ACTION_LOCK
	}
	return a
}

// unionSessionWarn merges two descending threshold lists without duplicates.
func unionSessionWarn(a, b []int) []int {
	seen := make(map[int]struct{}, len(a)+len(b))
	var out []int
	for _, v := range a {
		if _, dup := seen[v]; !dup {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	for _, v := range b {
		if _, dup := seen[v]; !dup {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// weeklyMinuteOf returns t's minute on the weekly grid (Monday 00:00 = 0),
// using t's wall clock in its own location.
func weeklyMinuteOf(t time.Time) int {
	return (int(t.Weekday())+6)%7*saMinutesPerDay + t.Hour()*60 + t.Minute()
}

// find returns the index of the window containing wm, or -1.
func (s *UserSchedule) find(wm int) int {
	for i, w := range s.windows {
		if wm >= w.start && wm < w.end {
			return i
		}
	}
	return -1
}

// fullWeek reports whether the schedule covers the entire week (the user is
// always allowed; there is no boundary).
func (s *UserSchedule) fullWeek() bool {
	return len(s.windows) == 1 && s.windows[0].start == 0 && s.windows[0].end == saMinutesPerWeek
}

// wrapJoined reports whether the last window touches the first across the
// week boundary (…–Sun 24:00 joined with Mon 00:00–…).
func (s *UserSchedule) wrapJoined() bool {
	return len(s.windows) >= 2 && s.windows[0].start == 0 && s.windows[len(s.windows)-1].end == saMinutesPerWeek
}

// Allowed reports whether t falls inside the user's allowed time.
func (s *UserSchedule) Allowed(t time.Time) bool {
	return s.find(weeklyMinuteOf(t)) >= 0
}

// CurrentWindow returns the end of the allowed window containing t along
// with the end action and warn thresholds that apply at that boundary.
// ok is false when t is outside every window, or when the schedule covers
// the whole week (no boundary ever fires).
func (s *UserSchedule) CurrentWindow(t time.Time) (end time.Time, action pb.SessionEndAction, warn []int, ok bool) {
	if s.fullWeek() {
		return time.Time{}, pb.SessionEndAction_SESSION_END_ACTION_UNSPECIFIED, nil, false
	}
	wm := weeklyMinuteOf(t)
	i := s.find(wm)
	if i < 0 {
		return time.Time{}, pb.SessionEndAction_SESSION_END_ACTION_UNSPECIFIED, nil, false
	}
	w := s.windows[i]
	endWM := w.end
	action = w.action
	warn = w.warn
	if endWM == saMinutesPerWeek && s.windows[0].start == 0 {
		// Continues past Sunday midnight into the first window: the real
		// boundary is the first window's end, next week on the grid.
		first := s.windows[0]
		endWM = first.end + saMinutesPerWeek
		action = first.action
		warn = unionSessionWarn(warn, first.warn)
	}
	return resolveWeeklyMinute(t, wm, endWM), action, warn, true
}

// NextWindowStart returns the start of the next allowed window strictly
// after t. ok is false when the schedule covers the whole week.
func (s *UserSchedule) NextWindowStart(t time.Time) (start time.Time, ok bool) {
	if s.fullWeek() {
		return time.Time{}, false
	}
	wm := weeklyMinuteOf(t)
	// A window starting at Monday 00:00 that is wrap-joined is a
	// continuation of the Sunday tail, not a real start.
	skipFirst := s.wrapJoined()
	var starts []int
	for i, w := range s.windows {
		if i == 0 && skipFirst {
			continue
		}
		starts = append(starts, w.start)
	}
	if len(starts) == 0 {
		return time.Time{}, false
	}
	for _, st := range starts {
		if st > wm {
			return resolveWeeklyMinute(t, wm, st), true
		}
	}
	return resolveWeeklyMinute(t, wm, starts[0]+saMinutesPerWeek), true
}

// resolveWeeklyMinute converts a weekly-grid minute target (>= wm, possibly
// beyond one week for wrap-joined ends) into a concrete wall-clock time in
// t's location. Constructing via time.Date keeps wall-clock semantics
// across DST: a target inside a spring-forward gap normalizes to the next
// valid time.
func resolveWeeklyMinute(t time.Time, wm, target int) time.Time {
	dayDelta := target/saMinutesPerDay - wm/saMinutesPerDay
	minute := target % saMinutesPerDay
	y, m, d := t.Date()
	return time.Date(y, m, d+dayDelta, minute/60, minute%60, 0, 0, t.Location())
}

// Decision is what the enforcer should do for a user at a given instant.
type Decision struct {
	// Allowed reports whether the user may use the session right now.
	Allowed bool
	// Action is the end action at the upcoming boundary (when Allowed) or the
	// action to enforce right now (when denied — the most recently ended
	// window's action, defaulting to LOCK).
	Action pb.SessionEndAction
	// Boundary is when the current allowed window ends (only when Allowed and
	// HasBoundary).
	Boundary time.Time
	// HasBoundary is false for a full-week schedule (no boundary ever fires).
	HasBoundary bool
	// Warn is the descending warn-threshold ladder for the upcoming boundary.
	Warn []int
	// NextStart is the start of the next allowed window (for user messaging);
	// valid only when HasNext is true.
	NextStart time.Time
	HasNext   bool
}

// DecisionAt evaluates the schedule for the given instant. It composes
// Allowed/CurrentWindow/NextWindowStart into the single verdict the enforcer
// acts on, and resolves the enforcement action to apply while denied.
func (s *UserSchedule) DecisionAt(t time.Time) Decision {
	d := Decision{Allowed: s.Allowed(t)}
	next, hasNext := s.NextWindowStart(t)
	d.NextStart, d.HasNext = next, hasNext

	if d.Allowed {
		end, action, warn, ok := s.CurrentWindow(t)
		d.Boundary, d.Action, d.Warn, d.HasBoundary = end, action, warn, ok
		return d
	}
	d.Action = s.deniedAction(weeklyMinuteOf(t))
	return d
}

// deniedAction returns the end action of the window that most recently ended
// at or before wm. When wm precedes every window this week, the previous
// week's last window applies (wrap). Defaults to LOCK for an empty schedule.
func (s *UserSchedule) deniedAction(wm int) pb.SessionEndAction {
	best := -1
	for i, w := range s.windows {
		if w.end <= wm && (best < 0 || w.end > s.windows[best].end) {
			best = i
		}
	}
	if best < 0 {
		if len(s.windows) == 0 {
			return pb.SessionEndAction_SESSION_END_ACTION_LOCK
		}
		best = len(s.windows) - 1 // wrap: last window of the previous week
	}
	return s.windows[best].action
}
