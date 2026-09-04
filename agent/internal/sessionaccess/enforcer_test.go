// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package sessionaccess

import (
	"errors"
	"testing"
	"time"

	"github.com/VuteTech/Bor/agent/internal/logind"
	"github.com/VuteTech/Bor/agent/internal/policy"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// fakeLogind is a scriptable Manager recording lock/terminate calls.
type fakeLogind struct {
	sessions   []logind.Session
	listErr    error
	locked     []string
	terminated []string
}

func (f *fakeLogind) ListSessions() ([]logind.Session, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.sessions, nil
}
func (f *fakeLogind) Lock(id string) error {
	f.locked = append(f.locked, id)
	for i := range f.sessions {
		if f.sessions[i].ID == id {
			f.sessions[i].LockedHint = true
		}
	}
	return nil
}
func (f *fakeLogind) Terminate(id string) error {
	f.terminated = append(f.terminated, id)
	return nil
}

type alertRec struct {
	user     string
	summary  string
	critical bool
}

type fakeAlerter struct{ alerts []alertRec }

func (a *fakeAlerter) Alert(sess *logind.Session, summary, _ string, critical bool) {
	a.alerts = append(a.alerts, alertRec{sess.Name, summary, critical})
}

// weekday returns a fixed Monday-of-test-week wall time in UTC.
func weekday(day, hh, mm int) time.Time {
	base := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC) // a Monday
	return time.Date(base.Year(), base.Month(), base.Day()+day, hh, mm, 0, 0, time.UTC)
}

func newTestEnforcer(fl *fakeLogind, fa *fakeAlerter, now time.Time) *Enforcer {
	e := New(fl, fa)
	e.groupsOf = func(string) []string { return nil }
	e.now = func() time.Time { return now }
	return e
}

func rulesFor(t *testing.T, rule *pb.SessionAccessRule) []policy.SessionRule {
	t.Helper()
	rules, warnings := policy.CompileSessionAccess([]policy.SessionPolicySource{
		{Name: "Test", Policy: &pb.SessionAccessPolicy{Rules: []*pb.SessionAccessRule{rule}}},
	})
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	return rules
}

func weekdayWindow() *pb.SessionAccessRule {
	return &pb.SessionAccessRule{
		Users:   []string{"alice"},
		Windows: []*pb.SessionAccessWindow{{Days: []string{"mon", "tue", "wed", "thu", "fri"}, Start: "08:00", End: "17:00"}},
	}
}

func aliceSession() logind.Session {
	return logind.Session{ID: "5", Name: "alice", UID: 1000, Seat: "seat0", Type: "wayland", Class: "user", Active: true, LockedHint: false}
}

func TestEnforce_AllowedFarFromBoundary(t *testing.T) {
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(0, 9, 0)) // Mon 09:00, 8h left
	e.SetRules(rulesFor(t, weekdayWindow()))

	interval := e.evaluate()
	if len(fl.locked)+len(fl.terminated) != 0 {
		t.Errorf("no enforcement expected far from boundary; locked=%v term=%v", fl.locked, fl.terminated)
	}
	if len(fa.alerts) != 0 {
		t.Errorf("no alerts expected far from boundary; got %v", fa.alerts)
	}
	if interval != farInterval {
		t.Errorf("interval = %v, want far", interval)
	}
}

func TestEnforce_WarningLadder(t *testing.T) {
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	// Mon 16:46 → 14 min to 17:00 boundary; default warns [30,15,5].
	e := newTestEnforcer(fl, fa, weekday(0, 16, 46))
	e.SetRules(rulesFor(t, weekdayWindow()))

	e.evaluate()
	if len(fa.alerts) != 1 || fa.alerts[0].critical {
		t.Fatalf("expected one non-critical 15-min warning, got %+v", fa.alerts)
	}
	// Same minute again: deduped, no new alert.
	e.evaluate()
	if len(fa.alerts) != 1 {
		t.Fatalf("warning should be deduped; got %+v", fa.alerts)
	}
	// Advance to 16:56 → 4 min left, crosses the final 5-min threshold (critical).
	e.now = func() time.Time { return weekday(0, 16, 56) }
	e.evaluate()
	if len(fa.alerts) != 2 || !fa.alerts[1].critical {
		t.Fatalf("expected critical final warning, got %+v", fa.alerts)
	}
}

func TestEnforce_LockAtBoundary(t *testing.T) {
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(0, 17, 0)) // exactly at boundary → denied
	e.SetRules(rulesFor(t, weekdayWindow()))

	interval := e.evaluate()
	if len(fl.locked) != 1 || fl.locked[0] != "5" {
		t.Fatalf("expected lock at boundary, got %v", fl.locked)
	}
	if interval != nearInterval {
		t.Errorf("interval = %v, want near while denied", interval)
	}
	// Next pass: already locked → no repeat lock.
	e.evaluate()
	if len(fl.locked) != 1 {
		t.Errorf("lock must not repeat while LockedHint is set; got %v", fl.locked)
	}
}

func TestEnforce_Watchdog_RelocksUnlocked(t *testing.T) {
	// Denied period, session found unlocked → re-lock.
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(5, 12, 0)) // Saturday — no window
	e.SetRules(rulesFor(t, weekdayWindow()))

	e.evaluate()
	if len(fl.locked) != 1 {
		t.Fatalf("watchdog should re-lock an unlocked denied session, got %v", fl.locked)
	}
}

func TestEnforce_Logout(t *testing.T) {
	rule := weekdayWindow()
	rule.EndAction = pb.SessionEndAction_SESSION_END_ACTION_LOGOUT
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(0, 17, 30)) // past boundary
	e.SetRules(rulesFor(t, rule))

	e.evaluate()
	if len(fl.terminated) != 1 || fl.terminated[0] != "5" {
		t.Fatalf("expected terminate for LOGOUT action, got %v", fl.terminated)
	}
	if len(fl.locked) != 0 {
		t.Errorf("LOGOUT must not lock; got %v", fl.locked)
	}
}

func TestEnforce_UntargetedUserIgnored(t *testing.T) {
	sess := aliceSession()
	sess.Name = "bob" // not targeted
	fl := &fakeLogind{sessions: []logind.Session{sess}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(5, 12, 0))
	e.SetRules(rulesFor(t, weekdayWindow()))

	e.evaluate()
	if len(fl.locked)+len(fl.terminated) != 0 {
		t.Errorf("untargeted user must be unrestricted; locked=%v term=%v", fl.locked, fl.terminated)
	}
}

func TestEnforce_NonGraphicalSessionIgnored(t *testing.T) {
	mgr := aliceSession()
	mgr.ID = "6"
	mgr.Class = "manager"
	mgr.Type = ""
	fl := &fakeLogind{sessions: []logind.Session{mgr}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(5, 12, 0))
	e.SetRules(rulesFor(t, weekdayWindow()))

	e.evaluate()
	if len(fl.locked)+len(fl.terminated) != 0 {
		t.Errorf("manager session must be ignored; locked=%v term=%v", fl.locked, fl.terminated)
	}
}

func TestEnforce_ListErrorRetriesFast(t *testing.T) {
	// A transient loginctl failure must schedule a quick retry, not a
	// 60 s enforcement gap while a session may be denied.
	fl := &fakeLogind{listErr: errors.New("loginctl: boom")}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(5, 12, 0))
	e.SetRules(rulesFor(t, weekdayWindow()))

	if got := e.evaluate(); got != nearInterval {
		t.Errorf("interval on list error = %v, want near (%v)", got, nearInterval)
	}
}

func TestEnforce_GroupLookupIsCached(t *testing.T) {
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(0, 9, 0))
	e.SetRules(rulesFor(t, weekdayWindow()))
	lookups := 0
	e.groupsOf = func(string) []string { lookups++; return nil }

	e.evaluate()
	e.evaluate()
	e.evaluate()
	if lookups != 1 {
		t.Errorf("group lookups within TTL = %d, want 1 (cached)", lookups)
	}
	// Past the TTL the membership is refreshed.
	e.now = func() time.Time { return weekday(0, 9, 0).Add(groupCacheTTL + time.Second) }
	e.evaluate()
	if lookups != 2 {
		t.Errorf("group lookups after TTL = %d, want 2", lookups)
	}
}

func TestEnforce_NoRulesNoWork(t *testing.T) {
	fl := &fakeLogind{sessions: []logind.Session{aliceSession()}}
	fa := &fakeAlerter{}
	e := newTestEnforcer(fl, fa, weekday(5, 12, 0))
	// no SetRules

	if got := e.evaluate(); got != farInterval {
		t.Errorf("interval = %v, want far when no rules", got)
	}
	if len(fl.locked) != 0 {
		t.Error("no enforcement without rules")
	}
}
