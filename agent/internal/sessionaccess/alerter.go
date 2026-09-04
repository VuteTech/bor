// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package sessionaccess

import (
	"log"
	"os/user"
	"strconv"
	"sync"

	"github.com/VuteTech/Bor/agent/internal/logind"
	"github.com/VuteTech/Bor/agent/internal/notify"
)

// NotifyAlerter is the production Alerter: it posts to the user's session bus
// via notify.SendAlert and reuses the returned id per user so successive
// warnings update in place (a countdown) instead of stacking.
type NotifyAlerter struct {
	mu     sync.Mutex
	lastID map[uint32]uint32
}

// NewNotifyAlerter returns a ready NotifyAlerter.
func NewNotifyAlerter() *NotifyAlerter {
	return &NotifyAlerter{lastID: make(map[uint32]uint32)}
}

// Alert implements Alerter.
func (n *NotifyAlerter) Alert(sess *logind.Session, summary, body string, critical bool) {
	n.mu.Lock()
	replaceID := n.lastID[sess.UID]
	n.mu.Unlock()

	id, err := notify.SendAlert(notify.AlertTarget{
		UID:       sess.UID,
		GID:       gidOf(sess),
		User:      sess.Name,
		Summary:   summary,
		Body:      body,
		Critical:  critical,
		ReplaceID: replaceID,
	})
	if err != nil {
		log.Printf("session access: failed to alert %s: %v", sess.Name, err)
		return
	}
	n.mu.Lock()
	n.lastID[sess.UID] = id
	n.mu.Unlock()
}

// gidOf resolves the primary GID for a session's user, falling back to the
// UID (the common single-user-group layout) when lookup fails.
func gidOf(sess *logind.Session) uint32 {
	if u, err := user.LookupId(strconv.FormatUint(uint64(sess.UID), 10)); err == nil {
		if gid, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
			return uint32(gid)
		}
	}
	return sess.UID
}
