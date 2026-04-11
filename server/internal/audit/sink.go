// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package audit provides the canonical AuditEvent type and the sink
// infrastructure for persisting and forwarding audit events.
package audit

import (
	"context"
	"encoding/json"
	"log"

	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

// Sink is the interface implemented by every audit output target.
// Emit must be non-blocking and must not return errors to the caller —
// failures are logged internally so that a broken sink never interrupts
// the request path.
type Sink interface {
	Emit(ctx context.Context, event *auditpb.AuditEvent)
}

// DatabaseSink persists AuditEvents to the audit_logs table.
// It converts the typed proto event back into the flat models.AuditLog shape
// that the existing DB layer (and the frontend) already understand.
type DatabaseSink struct {
	create func(ctx context.Context, entry *DBEntry) error
}

// DBEntry is the minimal flat struct the DatabaseSink needs to persist.
// It mirrors models.AuditLog without importing that package from here.
type DBEntry struct {
	UserID       *string
	Username     string
	Action       string
	ResourceType string
	ResourceID   string
	Details      string
	IPAddress    string
}

// NewDatabaseSink creates a DatabaseSink.  The create function must insert
// one row into audit_logs; pass a closure that calls AuditLogRepository.Create.
func NewDatabaseSink(create func(ctx context.Context, entry *DBEntry) error) *DatabaseSink {
	return &DatabaseSink{create: create}
}

// Emit converts an AuditEvent to a flat DB row and persists it.
func (s *DatabaseSink) Emit(ctx context.Context, event *auditpb.AuditEvent) {
	entry := &DBEntry{
		Username:     event.GetActor().GetUsername(),
		Action:       event.GetAction(),
		ResourceType: event.GetResource().GetType(),
		ResourceID:   event.GetResource().GetId(),
		IPAddress:    event.GetSrcIp(),
	}

	if uid := event.GetActor().GetUserId(); uid != "" {
		entry.UserID = &uid
	}

	entry.Details = marshalDetails(event)

	if err := s.create(ctx, entry); err != nil {
		log.Printf("audit DatabaseSink: failed to persist event: %v", err)
	}
}

// marshalDetails serializes the event payload to the JSON shape the frontend
// already knows how to render, keeping backward compatibility with Option 1.
func marshalDetails(event *auditpb.AuditEvent) string {
	switch p := event.GetPayload().(type) {
	case *auditpb.AuditEvent_HttpChange:
		// The body_json field is already a redacted JSON object string.
		// Return it directly so the frontend's parseBodyDetails() keeps working.
		if p.HttpChange.GetBodyJson() != "" {
			return p.HttpChange.GetBodyJson()
		}
		return ""

	case *auditpb.AuditEvent_Tamper:
		// Match the exact JSON shape the frontend parses for tamper_detected events:
		// {"file": "...", "node": "...", "processes": [...]}
		type procEntry struct {
			PID  int32  `json:"pid"`
			Comm string `json:"comm"`
			User string `json:"user"`
		}
		procs := make([]procEntry, 0, len(p.Tamper.GetProcesses()))
		for _, pr := range p.Tamper.GetProcesses() {
			procs = append(procs, procEntry{PID: pr.GetPid(), Comm: pr.GetComm(), User: pr.GetUser()})
		}
		b, err := json.Marshal(struct {
			File      string      `json:"file"`
			Node      string      `json:"node"`
			Processes []procEntry `json:"processes,omitempty"`
		}{
			File:      p.Tamper.GetFilePath(),
			Node:      event.GetActor().GetUsername(),
			Processes: procs,
		})
		if err != nil {
			return ""
		}
		return string(b)

	default:
		return ""
	}
}
