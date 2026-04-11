// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

const (
	cefVersion       = "CEF:0"
	cefDeviceVendor  = "Vute Tech"
	cefDeviceProduct = "Bor"
	cefDeviceVersion = "1.0"
)

// FormatCEF renders an AuditEvent as a CEF string (without the syslog header).
//
// Format: CEF:Version|Vendor|Product|Version|SignatureID|Name|Severity|Extensions
//
// CEF severity scale 0-10:
//
//	tamper_detected → 8 (high)
//	delete          → 6 (medium-high)
//	create/update   → 3 (low)
//	other           → 1 (informational)
func FormatCEF(event *auditpb.AuditEvent) string {
	sigID := cefEscape(event.GetAction())
	name := cefEscape(fmt.Sprintf("%s %s", event.GetResource().GetType(), event.GetAction()))
	severity := cefSeverity(event.GetAction())

	var ext strings.Builder

	// Standard CEF extension fields
	writeExt(&ext, "rt", cefTimestamp(event.GetOccurredAt().AsTime()))
	writeExt(&ext, "src", event.GetSrcIp())
	writeExt(&ext, "suser", event.GetActor().GetUsername())

	if rid := event.GetResource().GetId(); rid != "" {
		writeExt(&ext, "cs1Label", "resourceId")
		writeExt(&ext, "cs1", rid)
	}
	if rname := event.GetResource().GetName(); rname != "" {
		writeExt(&ext, "cs2Label", "resourceName")
		writeExt(&ext, "cs2", rname)
	}
	writeExt(&ext, "cs3Label", "resourceType")
	writeExt(&ext, "cs3", event.GetResource().GetType())
	writeExt(&ext, "outcome", outcomeString(event.GetOutcome()))

	// Payload-specific extensions
	switch p := event.GetPayload().(type) {
	case *auditpb.AuditEvent_HttpChange:
		writeExt(&ext, "requestMethod", p.HttpChange.GetMethod())
		writeExt(&ext, "request", p.HttpChange.GetPath())
		if body := flattenBodyJSON(p.HttpChange.GetBodyJson()); body != "" {
			writeExt(&ext, "msg", cefEscapeVal(body))
		}

	case *auditpb.AuditEvent_Tamper:
		writeExt(&ext, "filePath", cefEscapeVal(p.Tamper.GetFilePath()))
		if len(p.Tamper.GetProcesses()) > 0 {
			parts := make([]string, 0, len(p.Tamper.GetProcesses()))
			for _, pr := range p.Tamper.GetProcesses() {
				parts = append(parts, fmt.Sprintf("%s(pid=%d,user=%s)", pr.GetComm(), pr.GetPid(), pr.GetUser()))
			}
			writeExt(&ext, "msg", cefEscapeVal(strings.Join(parts, "; ")))
		}
	}

	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%s",
		cefVersion,
		cefDeviceVendor,
		cefDeviceProduct,
		cefDeviceVersion,
		sigID,
		name,
		severity,
		strings.TrimSpace(ext.String()),
	)
}

// writeExt appends a key=value pair to the CEF extension string.
func writeExt(b *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteByte(' ')
	}
	b.WriteString(key)
	b.WriteByte('=')
	b.WriteString(value)
}

// cefSeverity maps Bor action names to CEF severity (0–10).
func cefSeverity(action string) int {
	switch action {
	case "tamper_detected":
		return 8
	case "delete":
		return 6
	case "create", "update":
		return 3
	default:
		return 1
	}
}

// cefTimestamp formats a time as milliseconds since epoch for CEF rt field.
func cefTimestamp(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixMilli())
}

// outcomeString converts the proto Outcome enum to a CEF-friendly string.
func outcomeString(o auditpb.Outcome) string {
	switch o {
	case auditpb.Outcome_OUTCOME_SUCCESS:
		return "success"
	case auditpb.Outcome_OUTCOME_FAILURE:
		return "failure"
	default:
		return "unknown"
	}
}

// flattenBodyJSON converts a JSON body object to "key=value key=value" pairs
// for the CEF msg field, skipping nested objects and [REDACTED] values.
func flattenBodyJSON(bodyJSON string) string {
	if bodyJSON == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(bodyJSON), &m); err != nil {
		return bodyJSON
	}
	var parts []string
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s == "[REDACTED]" {
			parts = append(parts, fmt.Sprintf("%s=[REDACTED]", k))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s", k, s))
		}
	}
	return strings.Join(parts, " ")
}

// cefEscape escapes pipe characters in CEF header fields.
func cefEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

// cefEscapeVal escapes special characters in CEF extension values.
func cefEscapeVal(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "=", "\\=")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
