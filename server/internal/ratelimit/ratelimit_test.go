// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLimiter(2, time.Minute, func() time.Time { return now })

	steps := []struct {
		name    string
		key     string
		advance time.Duration
		want    bool
	}{
		{"first", "a", 0, true},
		{"second", "a", 0, true},
		{"third over limit", "a", 0, false},
		{"other key independent", "b", 0, true},
		{"still limited inside window", "a", 59 * time.Second, false},
		{"new window", "a", time.Second, true},
	}
	for _, s := range steps {
		now = now.Add(s.advance)
		if got := l.Allow(s.key); got != s.want {
			t.Fatalf("%s: Allow(%q) = %v, want %v", s.name, s.key, got, s.want)
		}
	}
}

func TestLimiterSweep(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLimiter(1, time.Minute, func() time.Time { return now })
	l.Allow("a")
	now = now.Add(time.Minute)
	l.Allow("b")
	now = now.Add(time.Minute)
	l.sweep()
	if _, ok := l.clients["a"]; ok {
		t.Error("stale entry a was not removed")
	}
	if _, ok := l.clients["b"]; !ok {
		t.Error("recent entry b was removed")
	}
}
