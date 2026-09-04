// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"reflect"
	"testing"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func saWindow(days []string, start, end string) *pb.SessionAccessWindow {
	return &pb.SessionAccessWindow{Days: days, Start: start, End: end}
}

func saSource(name string, rules ...*pb.SessionAccessRule) SessionPolicySource {
	return SessionPolicySource{Name: name, Policy: &pb.SessionAccessPolicy{Rules: rules}}
}

// testMonday returns a fixed Monday 00:00 in the given location.
func testMonday(loc *time.Location) time.Time {
	t := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	for t.Weekday() != time.Monday {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// wt returns a wall-clock time on day (0 = Monday) of the test week in UTC.
func wt(day, hh, mm int) time.Time {
	base := testMonday(time.UTC)
	y, m, d := base.Date()
	return time.Date(y, m, d+day, hh, mm, 0, 0, time.UTC)
}

func mustSchedule(t *testing.T, user string, sources ...SessionPolicySource) *UserSchedule {
	t.Helper()
	rules, warnings := CompileSessionAccess(sources)
	if len(warnings) > 0 {
		t.Fatalf("unexpected compile warnings: %v", warnings)
	}
	s := UserScheduleFor(rules, user, nil)
	if s == nil {
		t.Fatalf("no schedule for user %q", user)
	}
	return s
}

func TestCompileSessionAccess(t *testing.T) {
	rules, warnings := CompileSessionAccess([]SessionPolicySource{saSource("Lab",
		&pb.SessionAccessRule{
			Users:   []string{"alice", "root", ""},
			Groups:  []string{"students", ""},
			Windows: []*pb.SessionAccessWindow{saWindow([]string{"mon", "tue", "wed", "thu", "fri"}, "08:00", "17:30")},
		},
	)})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(rules))
	}
	r := rules[0]
	if _, ok := r.Users["root"]; ok {
		t.Error("root must never be restricted")
	}
	if _, ok := r.Users["alice"]; !ok {
		t.Error("alice missing from rule users")
	}
	if _, ok := r.Groups["students"]; !ok {
		t.Error("students missing from rule groups")
	}
	if len(r.segments) != 5 {
		t.Errorf("segments = %d, want 5", len(r.segments))
	}
	if r.Action != pb.SessionEndAction_SESSION_END_ACTION_LOCK {
		t.Errorf("action = %v, want LOCK (default)", r.Action)
	}
	if !reflect.DeepEqual(r.Warn, []int{30, 15, 5}) {
		t.Errorf("warn = %v, want default [30 15 5]", r.Warn)
	}
}

func TestCompileSessionAccess_Defensive(t *testing.T) {
	// A rule whose only window is invalid is dropped entirely (fail open);
	// a rule with one bad and one good window keeps the good one.
	rules, warnings := CompileSessionAccess([]SessionPolicySource{saSource("Bad",
		&pb.SessionAccessRule{
			Users:   []string{"alice"},
			Windows: []*pb.SessionAccessWindow{saWindow([]string{"blursday"}, "08:00", "17:00")},
		},
		&pb.SessionAccessRule{
			Users: []string{"bob"},
			Windows: []*pb.SessionAccessWindow{
				saWindow([]string{"mon"}, "26:00", "17:00"),
				saWindow([]string{"tue"}, "08:00", "17:00"),
			},
		},
		&pb.SessionAccessRule{ // only root targeted → dropped silently
			Users:   []string{"root"},
			Windows: []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "08:00", "17:00")},
		},
	)})
	if len(warnings) != 3 {
		t.Fatalf("warnings = %v, want 3 (bad window + dropped rule + bad window)", warnings)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1 (alice's rule dropped, bob's kept)", len(rules))
	}
	if UserScheduleFor(rules, "alice", nil) != nil {
		t.Error("alice must be unrestricted after her rule was dropped")
	}
	if len(rules[0].segments) != 1 {
		t.Errorf("bob's segments = %d, want 1 (bad window skipped)", len(rules[0].segments))
	}
}

func TestUserScheduleFor_Matching(t *testing.T) {
	rules, _ := CompileSessionAccess([]SessionPolicySource{saSource("P",
		&pb.SessionAccessRule{
			Users:   []string{"alice"},
			Groups:  []string{"students"},
			Windows: []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "08:00", "17:00")},
		},
	)})
	if UserScheduleFor(rules, "alice", nil) == nil {
		t.Error("direct user match failed")
	}
	if UserScheduleFor(rules, "bob", []string{"students"}) == nil {
		t.Error("group match failed")
	}
	if UserScheduleFor(rules, "mallory", []string{"staff"}) != nil {
		t.Error("untargeted user must have no schedule (unrestricted)")
	}
}

func TestUserSchedule_Allowed(t *testing.T) {
	s := mustSchedule(t, "alice", saSource("P", &pb.SessionAccessRule{
		Users:   []string{"alice"},
		Windows: []*pb.SessionAccessWindow{saWindow([]string{"mon", "tue", "wed", "thu", "fri"}, "08:00", "17:30")},
	}))
	cases := []struct {
		t    time.Time
		want bool
	}{
		{wt(0, 8, 0), true},   // Mon 08:00 — first allowed minute
		{wt(0, 7, 59), false}, // Mon 07:59
		{wt(4, 17, 29), true}, // Fri 17:29 — last allowed minute
		{wt(4, 17, 30), false},
		{wt(5, 12, 0), false}, // Sat
	}
	for _, c := range cases {
		if got := s.Allowed(c.t); got != c.want {
			t.Errorf("Allowed(%s) = %v, want %v", c.t.Format("Mon 15:04"), got, c.want)
		}
	}
}

func TestUserSchedule_MidnightCrossing(t *testing.T) {
	s := mustSchedule(t, "bob", saSource("P", &pb.SessionAccessRule{
		Users:   []string{"bob"},
		Windows: []*pb.SessionAccessWindow{saWindow([]string{"fri"}, "22:00", "02:00")},
	}))
	if !s.Allowed(wt(4, 23, 0)) || !s.Allowed(wt(5, 1, 59)) {
		t.Error("midnight-crossing window must cover Fri 23:00 and Sat 01:59")
	}
	if s.Allowed(wt(5, 2, 0)) || s.Allowed(wt(4, 21, 59)) {
		t.Error("midnight-crossing window covers too much")
	}
	end, _, _, ok := s.CurrentWindow(wt(4, 23, 0))
	if !ok || !end.Equal(wt(5, 2, 0)) {
		t.Errorf("CurrentWindow(Fri 23:00) end = %v ok=%v, want Sat 02:00", end, ok)
	}
}

func TestUserSchedule_WeekWrap(t *testing.T) {
	s := mustSchedule(t, "bob", saSource("P", &pb.SessionAccessRule{
		Users:   []string{"bob"},
		Windows: []*pb.SessionAccessWindow{saWindow([]string{"sun"}, "22:00", "02:00")},
	}))
	if !s.Allowed(wt(6, 23, 30)) || !s.Allowed(wt(0, 1, 0)) {
		t.Error("Sunday-night window must cover Sun 23:30 and Mon 01:00")
	}
	// No spurious boundary at Sunday midnight: the window ends Mon 02:00.
	end, _, _, ok := s.CurrentWindow(wt(6, 23, 59))
	if !ok || !end.Equal(wt(7, 2, 0)) {
		t.Errorf("CurrentWindow(Sun 23:59) end = %v ok=%v, want Mon 02:00 next day", end, ok)
	}
	// Inside the Monday tail the end is the same boundary.
	end, _, _, ok = s.CurrentWindow(wt(0, 1, 0))
	if !ok || !end.Equal(wt(0, 2, 0)) {
		t.Errorf("CurrentWindow(Mon 01:00) end = %v ok=%v, want Mon 02:00", end, ok)
	}
	// Next start after the Monday tail is Sunday 22:00, not Monday 00:00.
	start, ok := s.NextWindowStart(wt(0, 3, 0))
	if !ok || !start.Equal(wt(6, 22, 0)) {
		t.Errorf("NextWindowStart(Mon 03:00) = %v ok=%v, want Sun 22:00", start, ok)
	}
}

func TestUserSchedule_UnionAcrossRules(t *testing.T) {
	s := mustSchedule(t, "alice", saSource("P",
		&pb.SessionAccessRule{
			Users:       []string{"alice"},
			Windows:     []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "08:00", "12:00")},
			EndAction:   pb.SessionEndAction_SESSION_END_ACTION_LOCK,
			WarnMinutes: []int32{30},
		},
		&pb.SessionAccessRule{
			Users:       []string{"alice"},
			Windows:     []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "10:00", "17:00")},
			EndAction:   pb.SessionEndAction_SESSION_END_ACTION_LOGOUT,
			WarnMinutes: []int32{10},
		},
	))
	end, action, warn, ok := s.CurrentWindow(wt(0, 9, 0))
	if !ok || !end.Equal(wt(0, 17, 0)) {
		t.Fatalf("merged end = %v ok=%v, want Mon 17:00", end, ok)
	}
	if action != pb.SessionEndAction_SESSION_END_ACTION_LOGOUT {
		t.Errorf("action = %v, want LOGOUT (the rule defining the merged end)", action)
	}
	if !reflect.DeepEqual(warn, []int{30, 10}) {
		t.Errorf("warn = %v, want union [30 10]", warn)
	}
	// No boundary at 12:00: still allowed straight through.
	if !s.Allowed(wt(0, 12, 30)) {
		t.Error("union must be continuous across the overlap")
	}
}

func TestUserSchedule_SameEndPrefersLock(t *testing.T) {
	s := mustSchedule(t, "alice", saSource("P",
		&pb.SessionAccessRule{
			Users:     []string{"alice"},
			Windows:   []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "08:00", "17:00")},
			EndAction: pb.SessionEndAction_SESSION_END_ACTION_LOGOUT,
		},
		&pb.SessionAccessRule{
			Users:     []string{"alice"},
			Windows:   []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "09:00", "17:00")},
			EndAction: pb.SessionEndAction_SESSION_END_ACTION_LOCK,
		},
	))
	_, action, _, ok := s.CurrentWindow(wt(0, 10, 0))
	if !ok || action != pb.SessionEndAction_SESSION_END_ACTION_LOCK {
		t.Errorf("action = %v ok=%v, want LOCK when two rules end at the same minute", action, ok)
	}
}

func TestUserSchedule_TouchingWindows(t *testing.T) {
	s := mustSchedule(t, "alice", saSource("P", &pb.SessionAccessRule{
		Users: []string{"alice"},
		Windows: []*pb.SessionAccessWindow{
			saWindow([]string{"mon"}, "08:00", "12:00"),
			saWindow([]string{"mon"}, "12:00", "17:00"),
		},
	}))
	end, _, _, ok := s.CurrentWindow(wt(0, 11, 59))
	if !ok || !end.Equal(wt(0, 17, 0)) {
		t.Errorf("touching windows: end = %v ok=%v, want Mon 17:00 (no boundary at noon)", end, ok)
	}
}

func TestUserSchedule_FullWeek(t *testing.T) {
	s := mustSchedule(t, "kiosk", saSource("P", &pb.SessionAccessRule{
		Users:   []string{"kiosk"},
		Windows: []*pb.SessionAccessWindow{saWindow([]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}, "00:00", "00:00")},
	}))
	if !s.Allowed(wt(3, 3, 33)) {
		t.Error("full-week schedule must always allow")
	}
	if _, _, _, ok := s.CurrentWindow(wt(3, 3, 33)); ok {
		t.Error("full-week schedule must have no boundary")
	}
	if _, ok := s.NextWindowStart(wt(3, 3, 33)); ok {
		t.Error("full-week schedule must have no next start")
	}
}

func TestUserSchedule_NextWindowStartAcrossWeek(t *testing.T) {
	s := mustSchedule(t, "alice", saSource("P", &pb.SessionAccessRule{
		Users:   []string{"alice"},
		Windows: []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "08:00", "17:00")},
	}))
	start, ok := s.NextWindowStart(wt(4, 18, 0)) // Friday evening
	if !ok || !start.Equal(wt(7, 8, 0)) {
		t.Errorf("NextWindowStart(Fri 18:00) = %v ok=%v, want next Mon 08:00", start, ok)
	}
}

func TestUserSchedule_DecisionAt(t *testing.T) {
	// Weekday 08:00–17:00 LOGOUT for alice.
	s := mustSchedule(t, "alice", saSource("P", &pb.SessionAccessRule{
		Users:     []string{"alice"},
		Windows:   []*pb.SessionAccessWindow{saWindow([]string{"mon", "tue", "wed", "thu", "fri"}, "08:00", "17:00")},
		EndAction: pb.SessionEndAction_SESSION_END_ACTION_LOGOUT,
	}))

	// Inside the window: allowed, boundary at 17:00, LOGOUT action.
	d := s.DecisionAt(wt(0, 9, 0))
	if !d.Allowed || !d.HasBoundary || !d.Boundary.Equal(wt(0, 17, 0)) {
		t.Fatalf("in-window decision = %+v", d)
	}
	if d.Action != pb.SessionEndAction_SESSION_END_ACTION_LOGOUT {
		t.Errorf("in-window action = %v, want LOGOUT", d.Action)
	}

	// After the window: denied, action from the window that just ended
	// (LOGOUT), next start tomorrow 08:00.
	d = s.DecisionAt(wt(0, 18, 0))
	if d.Allowed {
		t.Fatal("18:00 must be denied")
	}
	if d.Action != pb.SessionEndAction_SESSION_END_ACTION_LOGOUT {
		t.Errorf("denied action = %v, want LOGOUT (previous window)", d.Action)
	}
	if !d.HasNext || !d.NextStart.Equal(wt(1, 8, 0)) {
		t.Errorf("next start = %v (has=%v), want Tue 08:00", d.NextStart, d.HasNext)
	}

	// Before the first window of the week (Mon 06:00): denied, action wraps
	// to the previous week's last window.
	d = s.DecisionAt(wt(0, 6, 0))
	if d.Allowed {
		t.Fatal("Mon 06:00 must be denied")
	}
	if d.Action != pb.SessionEndAction_SESSION_END_ACTION_LOGOUT {
		t.Errorf("pre-first-window action = %v, want LOGOUT (wrap)", d.Action)
	}
	if !d.HasNext || !d.NextStart.Equal(wt(0, 8, 0)) {
		t.Errorf("next start = %v, want Mon 08:00", d.NextStart)
	}
}

func TestUserSchedule_DecisionAt_DeniedDefaultsLock(t *testing.T) {
	// A LOGOUT window only on Wednesday; on Monday (no window this week yet
	// and none before it except the wrap) the action still comes from the
	// single window. With mixed actions the nearest previous window wins.
	s := mustSchedule(t, "alice", saSource("P",
		&pb.SessionAccessRule{
			Users:     []string{"alice"},
			Windows:   []*pb.SessionAccessWindow{saWindow([]string{"mon"}, "08:00", "12:00")},
			EndAction: pb.SessionEndAction_SESSION_END_ACTION_LOCK,
		},
		&pb.SessionAccessRule{
			Users:     []string{"alice"},
			Windows:   []*pb.SessionAccessWindow{saWindow([]string{"wed"}, "08:00", "12:00")},
			EndAction: pb.SessionEndAction_SESSION_END_ACTION_LOGOUT,
		},
	))
	// Tuesday: most recently ended window is Monday's LOCK.
	if a := s.DecisionAt(wt(1, 10, 0)).Action; a != pb.SessionEndAction_SESSION_END_ACTION_LOCK {
		t.Errorf("Tue action = %v, want LOCK (Mon window)", a)
	}
	// Thursday: most recently ended is Wednesday's LOGOUT.
	if a := s.DecisionAt(wt(3, 10, 0)).Action; a != pb.SessionEndAction_SESSION_END_ACTION_LOGOUT {
		t.Errorf("Thu action = %v, want LOGOUT (Wed window)", a)
	}
}

func TestUserSchedule_DSTSpringForward(t *testing.T) {
	sofia, err := time.LoadLocation("Europe/Sofia")
	if err != nil {
		t.Skip("tzdata not available")
	}
	// EU DST 2026: clocks jump 03:00 → 04:00 EET/EEST on Sun 2026-03-29.
	dstSunday := time.Date(2026, 3, 29, 1, 30, 0, 0, sofia)
	if dstSunday.Weekday() != time.Sunday {
		t.Fatal("2026-03-29 expected to be a Sunday")
	}
	s := mustSchedule(t, "alice", saSource("P", &pb.SessionAccessRule{
		Users:   []string{"alice"},
		Windows: []*pb.SessionAccessWindow{saWindow([]string{"sun"}, "01:00", "03:30")},
	}))
	if !s.Allowed(dstSunday) {
		t.Fatal("Sun 01:30 must be allowed")
	}
	end, _, _, ok := s.CurrentWindow(dstSunday)
	if !ok {
		t.Fatal("expected a window boundary")
	}
	// 03:30 does not exist that day; wall-clock construction normalizes it
	// into the valid range after the jump. The exact instant must match
	// what time.Date yields for the same wall clock, and it must be after
	// the DST transition (04:00 EEST / 01:00 UTC).
	want := time.Date(2026, 3, 29, 3, 30, 0, 0, sofia)
	if !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
	transition := time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)
	if end.Before(transition) {
		t.Errorf("end %v resolved before the DST transition %v", end, transition)
	}
}
