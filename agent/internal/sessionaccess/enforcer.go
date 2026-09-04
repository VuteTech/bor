// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package sessionaccess implements Layer-1 enforcement of the SessionAccess
// policy: an adaptive polling loop that warns users as an allowed window
// closes, locks or logs off sessions at the boundary, and re-locks a session
// that is unlocked outside its allowed hours (the watchdog). The
// unlock-prevention guarantee (Layer 2, pam_time) is separate; on GNOME the
// watchdog here is the primary unlock enforcement because GDM ignores the
// PAM account phase on unlock (see docs/session-access-plan.md §11).
package sessionaccess

import (
	"context"
	"fmt"
	"log"
	"os/user"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/logind"
	"github.com/VuteTech/Bor/agent/internal/policy"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Polling cadence. Far from any boundary the loop is lazy; near a boundary
// and while any targeted session is denied it tightens so the watchdog
// re-lock race window stays small.
const (
	farInterval  = 60 * time.Second
	nearInterval = 5 * time.Second
	nearWindow   = 5 * time.Minute
)

// Alerter delivers a schedule warning to a single user's session.
type Alerter interface {
	Alert(sess *logind.Session, summary, body string, critical bool)
}

// Enforcer runs the enforcement loop. Its evaluate pass runs only on the Run
// goroutine, so per-run state (warning dedup) needs no locking; rules are
// swapped under mu by SetRules from the policy-sync path.
type Enforcer struct {
	logind   logind.Manager
	alerter  Alerter
	groupsOf func(username string) []string
	now      func() time.Time

	mu    sync.Mutex
	rules []policy.SessionRule

	trigger chan struct{}

	// warned dedups warnings by user+boundary+threshold; value is the
	// boundary time so stale keys can be pruned.
	warned map[string]time.Time

	// groupCache memoizes group membership per user for groupCacheTTL so the
	// 5 s denied-tick and the D-Bus unlock kicks do not hammer NSS/LDAP on
	// directory-backed hosts. Only touched on the Run goroutine.
	groupCache map[string]groupCacheEntry
}

type groupCacheEntry struct {
	groups  []string
	expires time.Time
}

// groupCacheTTL bounds how stale a user's group membership may be for Layer-1
// matching; a policy re-sync does not depend on it (Layer 2 expands groups
// freshly at apply time).
const groupCacheTTL = time.Minute

// New builds an Enforcer with the default system group resolver and clock.
func New(mgr logind.Manager, alerter Alerter) *Enforcer {
	return &Enforcer{
		logind:     mgr,
		alerter:    alerter,
		groupsOf:   systemGroupsOf,
		now:        time.Now,
		trigger:    make(chan struct{}, 1),
		warned:     make(map[string]time.Time),
		groupCache: make(map[string]groupCacheEntry),
	}
}

// cachedGroups returns the user's group names, memoized for groupCacheTTL.
func (e *Enforcer) cachedGroups(username string, now time.Time) []string {
	if ent, ok := e.groupCache[username]; ok && now.Before(ent.expires) {
		return ent.groups
	}
	groups := e.groupsOf(username)
	e.groupCache[username] = groupCacheEntry{groups: groups, expires: now.Add(groupCacheTTL)}
	return groups
}

// SetRules replaces the compiled rule set and nudges the loop to evaluate now
// so a newly bound policy takes effect immediately.
func (e *Enforcer) SetRules(rules []policy.SessionRule) {
	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()
	e.Kick()
}

// Kick asks the loop to run an enforcement pass as soon as possible. Used by
// the logind unlock watcher so a session that is unlocked outside its allowed
// hours is re-locked within milliseconds instead of at the next poll. Safe to
// call from any goroutine; coalesces if a pass is already pending.
func (e *Enforcer) Kick() {
	select {
	case e.trigger <- struct{}{}:
	default:
	}
}

// Run drives enforcement until ctx is cancelled, sleeping for the adaptive
// interval returned by each pass (or until SetRules triggers a re-evaluation).
func (e *Enforcer) Run(ctx context.Context) {
	log.Println("session access enforcer started")
	for {
		interval := e.evaluate()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-e.trigger:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// evaluate runs one enforcement pass over all sessions and returns how long
// to wait before the next pass.
func (e *Enforcer) evaluate() time.Duration {
	e.mu.Lock()
	rules := e.rules
	e.mu.Unlock()
	if len(rules) == 0 {
		return farInterval
	}

	now := e.now()
	e.pruneWarned(now)

	sessions, err := e.logind.ListSessions()
	if err != nil {
		// Retry quickly: a transient loginctl failure must not open a long
		// enforcement gap while a session may be denied.
		log.Printf("session access: failed to list sessions: %v", err)
		return nearInterval
	}

	interval := farInterval
	for i := range sessions {
		sess := &sessions[i]
		if !sess.IsGraphicalUser() || !sess.Active {
			continue
		}
		sched := policy.UserScheduleFor(rules, sess.Name, e.cachedGroups(sess.Name, now))
		if sched == nil {
			continue // user not targeted by any rule → unrestricted
		}
		if next := e.handleSession(sess, sched, now); next < interval {
			interval = next
		}
	}
	return clampInterval(interval)
}

// handleSession enforces one session and returns the suggested next interval.
func (e *Enforcer) handleSession(sess *logind.Session, sched *policy.UserSchedule, now time.Time) time.Duration {
	d := sched.DecisionAt(now)

	if !d.Allowed {
		e.enforceDenied(sess, &d)
		return nearInterval // stay tight: watchdog + boundary handling
	}
	if !d.HasBoundary {
		return farInterval // full-week allowance, nothing to watch
	}

	remaining := d.Boundary.Sub(now)
	e.maybeWarn(sess, &d, remaining)
	if remaining <= nearWindow {
		return nearInterval
	}
	return intervalUntilNextEvent(remaining, d.Warn)
}

// enforceDenied locks or terminates a session that is outside its allowed
// hours. LOCK re-locks only when currently unlocked (idempotent watchdog);
// LOGOUT terminates.
func (e *Enforcer) enforceDenied(sess *logind.Session, d *policy.Decision) {
	switch d.Action {
	case pb.SessionEndAction_SESSION_END_ACTION_LOGOUT:
		log.Printf("session access: logging off %s (session %s): outside allowed hours", sess.Name, sess.ID)
		if err := e.logind.Terminate(sess.ID); err != nil {
			log.Printf("session access: terminate-session %s failed: %v", sess.ID, err)
		}
	default: // LOCK (and UNSPECIFIED)
		if sess.LockedHint {
			return // already locked; nothing to do
		}
		log.Printf("session access: locking %s (session %s): outside allowed hours", sess.Name, sess.ID)
		if err := e.logind.Lock(sess.ID); err != nil {
			log.Printf("session access: lock-session %s failed: %v", sess.ID, err)
			return
		}
		e.alerter.Alert(sess, "Session locked",
			"Your allowed usage period has ended. "+nextPeriodPhrase(d), true)
	}
}

// maybeWarn fires at most one warning per pass: the smallest threshold that
// remaining has crossed but not yet been warned for. Larger thresholds that
// were crossed in the same pass (e.g. the agent started late, already inside
// the window) are marked consumed rather than emitted, so the user gets one
// timely message instead of a burst of stale ones. The smallest threshold is
// delivered at critical urgency (the forced final notification). warn is
// descending, e.g. [30, 15, 5].
func (e *Enforcer) maybeWarn(sess *logind.Session, d *policy.Decision, remaining time.Duration) {
	if len(d.Warn) == 0 {
		return
	}
	fire := -1
	for _, thr := range d.Warn {
		if remaining <= time.Duration(thr)*time.Minute {
			fire = thr // descending list → last match is the smallest crossed
		}
	}
	if fire < 0 {
		return // not yet within any threshold
	}
	key := warnKey(sess.Name, d.Boundary, fire)
	if _, done := e.warned[key]; done {
		return
	}
	// Consume this threshold and any larger ones crossed in the same pass.
	for _, thr := range d.Warn {
		if thr >= fire {
			e.warned[warnKey(sess.Name, d.Boundary, thr)] = d.Boundary
		}
	}
	final := d.Warn[len(d.Warn)-1]
	e.alerter.Alert(sess,
		fmt.Sprintf("Session %s in %d minutes", actionVerb(d.Action), fire),
		nextPeriodPhrase(d),
		fire == final)
}

func warnKey(username string, boundary time.Time, threshold int) string {
	return fmt.Sprintf("%s|%d|%d", username, boundary.Unix(), threshold)
}

func (e *Enforcer) pruneWarned(now time.Time) {
	for k, boundary := range e.warned {
		if now.Sub(boundary) > time.Hour {
			delete(e.warned, k)
		}
	}
	for u, ent := range e.groupCache {
		if !now.Before(ent.expires) {
			delete(e.groupCache, u)
		}
	}
}

// actionVerb is the user-facing verb for an end action.
func actionVerb(a pb.SessionEndAction) string {
	if a == pb.SessionEndAction_SESSION_END_ACTION_LOGOUT {
		return "ends"
	}
	return "locks"
}

// nextPeriodPhrase describes when the user may resume.
func nextPeriodPhrase(d *policy.Decision) string {
	if !d.HasNext {
		return "No further usage period is scheduled."
	}
	return "Next allowed period: " + d.NextStart.Format("Mon 15:04") + "."
}

// intervalUntilNextEvent returns the wait until the nearest upcoming event
// (a warn threshold or the boundary itself), clamped to the polling bounds.
func intervalUntilNextEvent(remaining time.Duration, warns []int) time.Duration {
	next := remaining // fall back to the boundary
	for _, w := range warns {
		thr := time.Duration(w) * time.Minute
		if remaining > thr {
			if d := remaining - thr; d < next {
				next = d
			}
		}
	}
	return clampInterval(next)
}

func clampInterval(d time.Duration) time.Duration {
	if d < nearInterval {
		return nearInterval
	}
	if d > farInterval {
		return farInterval
	}
	return d
}

// systemGroupsOf returns the group names the given username belongs to.
func systemGroupsOf(username string) []string {
	u, err := user.Lookup(username)
	if err != nil {
		return nil
	}
	gids, err := u.GroupIds()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(gids))
	for _, gid := range gids {
		if g, err := user.LookupGroupId(gid); err == nil {
			names = append(names, g.Name)
		}
	}
	return names
}
