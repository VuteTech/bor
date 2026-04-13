// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package audit

import (
	"encoding/json"
	"time"

	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

// OCSF class IDs used by Bor.
// https://schema.ocsf.io
const (
	ocsfClassFileActivity  = 1001 // File System Activity  — tamper events
	ocsfClassAPIActivity   = 6003 // API Activity          — REST CRUD operations
	ocsfClassAccountChange = 3002 // Account Change        — user create/update/delete
)

// OCSF activity IDs (common across classes).
const (
	ocsfActivityCreate = 1
	ocsfActivityUpdate = 3 // also used for "Modify" in File Activity
	ocsfActivityDelete = 4
	ocsfActivityOther  = 99
)

// ocsfEvent is the top-level OCSF JSON envelope.
// Only the fields Bor can populate are included; conformant SIEM parsers
// treat unknown/absent fields as optional.
type ocsfEvent struct {
	// Required OCSF fields
	ClassUID   int    `json:"class_uid"`
	ClassName  string `json:"class_name"`
	ActivityID int    `json:"activity_id"`
	Activity   string `json:"activity"`
	CategoryID int    `json:"category_uid"`
	Category   string `json:"category_name"`
	TypeUID    int    `json:"type_uid"` // class_uid * 100 + activity_id
	Severity   string `json:"severity"`
	SeverityID int    `json:"severity_id"`
	Time       int64  `json:"time"` // epoch milliseconds
	Message    string `json:"message,omitempty"`

	// Metadata block
	Metadata *ocsfMetadata `json:"metadata"`

	Actor *ocsfActor `json:"actor,omitempty"`

	// Network endpoint (request source)
	SrcEndpoint *ocsfEndpoint `json:"src_endpoint,omitempty"`

	// Payload-specific fields (populated per class)
	APIActivity  *ocsfAPIActivity  `json:"api,omitempty"`
	FileActivity *ocsfFileActivity `json:"file,omitempty"`
	User         *ocsfUser         `json:"user,omitempty"`
}

type ocsfMetadata struct {
	Product   ocsfProduct `json:"product"`
	Version   string      `json:"version"`
	EventCode string      `json:"event_code,omitempty"`
}

type ocsfProduct struct {
	Name    string `json:"name"`
	Vendor  string `json:"vendor_name"`
	Version string `json:"version"`
}

type ocsfActor struct {
	User ocsfUser `json:"user,omitempty"`
}

type ocsfUser struct {
	Name string `json:"name,omitempty"`
	UID  string `json:"uid,omitempty"`
}

type ocsfEndpoint struct {
	IP string `json:"ip,omitempty"`
}

type ocsfAPIActivity struct {
	Operation string          `json:"operation"`
	Request   *ocsfAPIRequest `json:"request,omitempty"`
}

type ocsfAPIRequest struct {
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
	Body   string `json:"body,omitempty"` // redacted JSON
}

type ocsfFileActivity struct {
	Path      string        `json:"path,omitempty"`
	Processes []ocsfProcess `json:"process,omitempty"`
}

type ocsfProcess struct {
	PID  int32  `json:"pid,omitempty"`
	Name string `json:"name,omitempty"`
	User string `json:"user,omitempty"`
}

// FormatOCSF renders an AuditEvent as an OCSF-compliant JSON string.
func FormatOCSF(event *auditpb.AuditEvent) (string, error) {
	ev := buildOCSFEvent(event)
	b, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func buildOCSFEvent(event *auditpb.AuditEvent) ocsfEvent {
	ts := time.Now().UnixMilli()
	if event.GetOccurredAt() != nil {
		ts = event.GetOccurredAt().AsTime().UnixMilli()
	}

	classUID, className, categoryID, categoryName := classForEvent(event)
	activityID, activityName := activityForAction(event.GetAction())
	severityID, severityName := severityForAction(event.GetAction())

	ev := ocsfEvent{
		ClassUID:   classUID,
		ClassName:  className,
		ActivityID: activityID,
		Activity:   activityName,
		CategoryID: categoryID,
		Category:   categoryName,
		TypeUID:    classUID*100 + activityID,
		Severity:   severityName,
		SeverityID: severityID,
		Time:       ts,
		Metadata: &ocsfMetadata{
			Version: "1.3.0",
			Product: ocsfProduct{
				Name:    "Bor",
				Vendor:  "Vute Tech",
				Version: "1.0",
			},
			EventCode: event.GetAction(),
		},
		Actor: &ocsfActor{
			User: ocsfUser{
				Name: event.GetActor().GetUsername(),
				UID:  event.GetActor().GetUserId(),
			},
		},
	}

	if ip := event.GetSrcIp(); ip != "" {
		ev.SrcEndpoint = &ocsfEndpoint{IP: ip}
	}

	// Populate the user block for account-change events.
	if classUID == ocsfClassAccountChange {
		ev.User = &ocsfUser{
			Name: event.GetResource().GetName(),
			UID:  event.GetResource().GetId(),
		}
	}

	switch p := event.GetPayload().(type) {
	case *auditpb.AuditEvent_HttpChange:
		ev.APIActivity = &ocsfAPIActivity{
			Operation: event.GetAction(),
			Request: &ocsfAPIRequest{
				Method: p.HttpChange.GetMethod(),
				URL:    p.HttpChange.GetPath(),
				Body:   p.HttpChange.GetBodyJson(),
			},
		}
		ev.Message = event.GetAction() + " " + event.GetResource().GetType() +
			" " + event.GetResource().GetId()

	case *auditpb.AuditEvent_Tamper:
		fa := &ocsfFileActivity{
			Path: p.Tamper.GetFilePath(),
		}
		for _, pr := range p.Tamper.GetProcesses() {
			fa.Processes = append(fa.Processes, ocsfProcess{
				PID:  pr.GetPid(),
				Name: pr.GetComm(),
				User: pr.GetUser(),
			})
		}
		ev.FileActivity = fa
		ev.Message = "managed file tampered: " + p.Tamper.GetFilePath()
	}

	return ev
}

// classForEvent selects the OCSF class based on the resource type and action.
func classForEvent(event *auditpb.AuditEvent) (classUID int, className string, catID int, catName string) {
	if _, ok := event.GetPayload().(*auditpb.AuditEvent_Tamper); ok {
		return ocsfClassFileActivity, "File System Activity", 1, "System Activity"
	}
	// REST API events
	switch event.GetResource().GetType() {
	case "users":
		return ocsfClassAccountChange, "Account Change", 3, "Identity & Access Management"
	default:
		return ocsfClassAPIActivity, "API Activity", 6, "Application Activity"
	}
}

// activityForAction maps Bor action verbs to OCSF activity IDs.
func activityForAction(action string) (activityID int, activityName string) {
	switch action {
	case "create":
		return ocsfActivityCreate, "Create"
	case "update":
		return ocsfActivityUpdate, "Update"
	case "delete":
		return ocsfActivityDelete, "Delete"
	case "tamper_detected":
		return ocsfActivityUpdate, "Modify"
	default:
		return ocsfActivityOther, "Other"
	}
}

// severityForAction maps Bor action verbs to OCSF severity IDs.
// OCSF severity: 0=Unknown 1=Informational 2=Low 3=Medium 4=High 5=Critical
func severityForAction(action string) (severityID int, severityName string) {
	switch action {
	case "tamper_detected":
		return 4, "High"
	case "delete":
		return 3, "Medium"
	case "create", "update":
		return 2, "Low"
	default:
		return 1, "Informational"
	}
}
