// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package ratelimit provides a fixed-window request limiter keyed by client
// address, shared by the HTTP API and the gRPC enrollment service.
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	count       int
	windowStart time.Time
}

// Limiter allows at most Rate requests per Window for each key (normally a
// client IP address).
type Limiter struct {
	mu      sync.Mutex
	clients map[string]*bucket
	rate    int
	window  time.Duration
	now     func() time.Time
}

// New creates a Limiter and starts a background goroutine that drops idle
// entries to bound memory use.
func New(rate int, window time.Duration) *Limiter {
	l := newLimiter(rate, window, time.Now)
	go l.cleanupLoop()
	return l
}

func newLimiter(rate int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{
		clients: make(map[string]*bucket),
		rate:    rate,
		window:  window,
		now:     now,
	}
}

// Window returns the length of the limiter's window.
func (l *Limiter) Window() time.Duration { return l.window }

// Allow reports whether a request for key is within the limit, counting it
// if so.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.clients[key]
	if !ok || now.Sub(b.windowStart) >= l.window {
		l.clients[key] = &bucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= l.rate {
		return false
	}
	b.count++
	return true
}

// cleanupLoop removes stale entries every two windows.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.window * 2)
	defer ticker.Stop()
	for range ticker.C {
		l.sweep()
	}
}

func (l *Limiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for key, b := range l.clients {
		if now.Sub(b.windowStart) >= l.window*2 {
			delete(l.clients, key)
		}
	}
}
