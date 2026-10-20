// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"fmt"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/VuteTech/Bor/agent/internal/logind"
)

// UserSession is a logged-in user the reboot rule has to respect.
type UserSession struct {
	UID       uint32
	GID       uint32
	User      string
	Graphical bool
}

// Sessions lists the user-class sessions (graphical and SSH alike) of
// the node. The greeter and the user managers are not sessions.
func Sessions(mgr logind.Manager) []UserSession {
	if mgr == nil {
		return nil
	}
	list, err := mgr.ListSessions()
	if err != nil {
		return nil
	}
	var out []UserSession
	seen := map[uint32]bool{}
	for i := range list {
		s := &list[i]
		if s.Class != "user" && s.Class != "" {
			continue
		}
		us := UserSession{UID: s.UID, GID: s.UID, User: s.Name, Graphical: s.IsGraphicalUser()}
		if u, lerr := user.LookupId(strconv.FormatUint(uint64(s.UID), 10)); lerr == nil {
			if g, gerr := strconv.ParseUint(u.Gid, 10, 32); gerr == nil {
				us.GID = uint32(g)
			}
		}
		if us.Graphical || !seen[s.UID] {
			out = append(out, us)
		}
		seen[s.UID] = true
	}
	return out
}

// Window is a daily "HH:MM-HH:MM" range; a window past midnight wraps.
type Window struct {
	StartMin, EndMin int
}

// ParseWindow parses "HH:MM-HH:MM".
func ParseWindow(s string) (*Window, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil, fmt.Errorf("window %q: use HH:MM-HH:MM", s)
	}
	start, err := parseHHMM(a)
	if err != nil {
		return nil, fmt.Errorf("window %q: %w", s, err)
	}
	end, err := parseHHMM(b)
	if err != nil {
		return nil, fmt.Errorf("window %q: %w", s, err)
	}
	if start == end {
		return nil, fmt.Errorf("window %q: start and end are the same", s)
	}
	return &Window{StartMin: start, EndMin: end}, nil
}

func parseHHMM(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	hh, err := strconv.Atoi(h)
	if err != nil || hh < 0 || hh > 23 {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	mm, err := strconv.Atoi(m)
	if err != nil || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return hh*60 + mm, nil
}

// Contains reports whether the local time of t falls into the window.
func (w *Window) Contains(t time.Time) bool {
	if w == nil {
		return true
	}
	now := t.Hour()*60 + t.Minute()
	if w.StartMin < w.EndMin {
		return now >= w.StartMin && now < w.EndMin
	}
	return now >= w.StartMin || now < w.EndMin
}

// String renders the window back.
func (w *Window) String() string {
	if w == nil {
		return ""
	}
	return fmt.Sprintf("%02d:%02d-%02d:%02d", w.StartMin/60, w.StartMin%60, w.EndMin/60, w.EndMin%60)
}
