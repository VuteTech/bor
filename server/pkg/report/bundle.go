// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package report defines the compliance report bundle, the document a
// generated report is stored and exported as (apiVersion bor.report.v1).
//
// The bundle is the evidence: its canonical bytes are hashed, optionally
// signed, and every other format (CSV, HTML, PDF) is rendered from them.
// The schema is public so that server extensions render and sign bundles
// and so that auditors can parse an exported bundle with their own tools.
//
// Field order and the sort order of every list are part of the format:
// two bundles with the same content marshal to the same bytes. Timestamps
// are RFC 3339 in UTC with second precision.
package report

import (
	"encoding/json"
	"fmt"
	"time"
)

// APIVersion is the schema version of the bundle.
const APIVersion = "bor.report.v1"

// Kind is the document kind.
const Kind = "ComplianceReport"

// Trigger values.
const (
	TriggerManual       = "manual"
	TriggerScheduled    = "scheduled"
	TriggerDailySummary = "daily_summary"
	TriggerAPI          = "api"
)

// Compliance statuses, as the agents report them.
const (
	StatusCompliant    = "compliant"
	StatusNonCompliant = "non_compliant"
	StatusError        = "error"
	StatusInapplicable = "inapplicable"
	StatusUnknown      = "unknown"
)

// Bundle is one compliance report.
type Bundle struct {
	APIVersion  string `json:"apiVersion"`
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	GeneratedAt string `json:"generated_at"`
	// Trigger is manual, scheduled, daily_summary or api.
	Trigger     string       `json:"trigger"`
	GeneratedBy Actor        `json:"generated_by"`
	Schedule    *ScheduleRef `json:"schedule,omitempty"`
	Server      Server       `json:"server"`
	Label       string       `json:"label"`
	Notes       string       `json:"notes"`
	Scope       Scope        `json:"scope"`
	Thresholds  Thresholds   `json:"thresholds"`
	Summary     Summary      `json:"summary"`
	// Controls aggregates the compliance items by the benchmark controls
	// they map to, sorted by framework then control id.
	Controls []ControlSummary `json:"controls"`
	// Policies summarises every policy with a result, sorted by name then id.
	Policies []PolicySummary `json:"policies"`
	// Nodes lists every node in scope, sorted by name.
	Nodes []Node `json:"nodes"`
	// Results holds one entry per node and policy, sorted by node name then
	// policy name then policy id.
	Results []Result `json:"results"`
}

// Actor is who generated the report: a user, or a schedule's name.
type Actor struct {
	UserID   string `json:"user_id,omitempty"`
	Username string `json:"username"`
}

// ScheduleRef names the schedule that generated the report.
type ScheduleRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Server identifies the server that generated the report.
type Server struct {
	Version string `json:"version"`
}

// Ref is an id and the name it had when the report was generated.
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Scope records what the report covered.
type Scope struct {
	// AllNodes is set when the report covered every node rather than the
	// members of NodeGroups.
	AllNodes   bool  `json:"all_nodes"`
	NodeGroups []Ref `json:"node_groups"`
	// PolicyTypes limits the results to these policy types; empty means all.
	PolicyTypes []string `json:"policy_types"`
	// Policies limits the results to these policies; empty means all.
	Policies []Ref `json:"policies"`
}

// Thresholds are the rules the stale and unreachable flags were computed with.
type Thresholds struct {
	StaleAfterSeconds int64 `json:"stale_after_seconds"`
}

// StatusCounts counts results or items by compliance status.
type StatusCounts struct {
	Compliant    int `json:"compliant"`
	NonCompliant int `json:"non_compliant"`
	Error        int `json:"error"`
	Inapplicable int `json:"inapplicable"`
	Unknown      int `json:"unknown"`
}

// Add counts one status.
func (c *StatusCounts) Add(status string) {
	switch status {
	case StatusCompliant:
		c.Compliant++
	case StatusNonCompliant:
		c.NonCompliant++
	case StatusError:
		c.Error++
	case StatusInapplicable:
		c.Inapplicable++
	default:
		c.Unknown++
	}
}

// Total is the number of counted entries.
func (c StatusCounts) Total() int {
	return c.Compliant + c.NonCompliant + c.Error + c.Inapplicable + c.Unknown
}

// Score is the compliance score in percent: compliant results over the
// results that could be evaluated (inapplicable and unknown results do not
// count). nil when nothing could be evaluated.
func (c StatusCounts) Score() *float64 {
	denominator := c.Compliant + c.NonCompliant + c.Error
	if denominator == 0 {
		return nil
	}
	score := float64(c.Compliant) * 100 / float64(denominator)
	rounded := float64(int64(score*10+0.5)) / 10
	return &rounded
}

// Summary is the headline of the report.
type Summary struct {
	Nodes               int          `json:"nodes"`
	NodesOnline         int          `json:"nodes_online"`
	NodesUnreachable    int          `json:"nodes_unreachable"`
	NodesWithoutResults int          `json:"nodes_without_results"`
	Results             int          `json:"results"`
	ResultsStale        int          `json:"results_stale"`
	ByStatus            StatusCounts `json:"by_status"`
	ScorePct            *float64     `json:"score_pct"`
}

// ControlSummary aggregates the items that map to one benchmark control.
type ControlSummary struct {
	Framework    string `json:"framework"`
	ID           string `json:"id"`
	Title        string `json:"title"`
	Pass         int    `json:"pass"`
	Fail         int    `json:"fail"`
	Error        int    `json:"error"`
	Inapplicable int    `json:"inapplicable"`
	NodesFailing int    `json:"nodes_failing"`
}

// PolicySummary is one policy with results in the report.
type PolicySummary struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Type     string       `json:"type"`
	Version  int          `json:"version"`
	ByStatus StatusCounts `json:"by_status"`
}

// Node is a node in scope with the facts it reported.
type Node struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	FQDN         string `json:"fqdn,omitempty"`
	OSName       string `json:"os_name,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	DesktopEnv   string `json:"desktop_env,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	// Status is the node's connection state when the report was generated:
	// online, offline or unknown.
	Status   string `json:"status"`
	LastSeen string `json:"last_seen,omitempty"`
	// Unreachable is set when the node was not online and had not been seen
	// within the stale threshold.
	Unreachable bool         `json:"unreachable"`
	Groups      []string     `json:"groups"`
	Posture     Posture      `json:"posture"`
	ByStatus    StatusCounts `json:"by_status"`
}

// Posture is the security posture the node reported outside policies.
type Posture struct {
	LSM                 string                 `json:"lsm,omitempty"`
	LSMMode             string                 `json:"lsm_mode,omitempty"`
	DiskEncryption      *DiskEncryptionPosture `json:"disk_encryption,omitempty"`
	Updates             *UpdatesPosture        `json:"updates,omitempty"`
	CertificateNotAfter string                 `json:"certificate_not_after,omitempty"`
}

// DiskEncryptionPosture is what the disk encryption inventory says about
// the node.
type DiskEncryptionPosture struct {
	SystemVolumeEncrypted   bool   `json:"system_volume_encrypted"`
	Cipher                  string `json:"cipher,omitempty"`
	RecoveryKeyEscrowed     bool   `json:"recovery_key_escrowed"`
	TPM2Present             *bool  `json:"tpm2_present,omitempty"`
	SecureBoot              string `json:"secure_boot,omitempty"`
	UnencryptedSystemMounts int    `json:"unencrypted_system_mounts"`
	ReportedAt              string `json:"reported_at,omitempty"`
}

// UpdatesPosture is what the Software updates inventory says about the node.
type UpdatesPosture struct {
	Engine          string `json:"engine"`
	EffectiveMode   string `json:"effective_mode,omitempty"`
	PendingTotal    int    `json:"pending_total"`
	PendingSecurity int    `json:"pending_security"`
	RebootRequired  bool   `json:"reboot_required"`
	RebootSince     string `json:"reboot_since,omitempty"`
	ReportedAt      string `json:"reported_at,omitempty"`
}

// Result is the agent's last compliance result for one policy on one node.
type Result struct {
	Node       string `json:"node"`
	NodeID     string `json:"node_id"`
	PolicyID   string `json:"policy_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	ReportedAt string `json:"reported_at"`
	// AgeSeconds is how old the result was when the report was generated.
	AgeSeconds int64 `json:"age_seconds"`
	// Stale is set when AgeSeconds exceeds the stale threshold.
	Stale bool   `json:"stale"`
	Items []Item `json:"items,omitempty"`
}

// Item is one setting of a policy result.
type Item struct {
	SchemaID string       `json:"schema_id"`
	Key      string       `json:"key"`
	Status   string       `json:"status"`
	Message  string       `json:"message,omitempty"`
	Controls []ControlRef `json:"controls,omitempty"`
}

// ControlRef names a benchmark control an item contributes to.
type ControlRef struct {
	Framework string `json:"framework"`
	ID        string `json:"id"`
}

// FormatTime renders a timestamp the way the bundle stores it.
func FormatTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// Parse decodes a bundle and checks its schema version.
func Parse(data []byte) (*Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("report: parse bundle: %w", err)
	}
	if b.APIVersion != APIVersion || b.Kind != Kind {
		return nil, fmt.Errorf("report: unsupported document %s/%s", b.APIVersion, b.Kind)
	}
	return &b, nil
}
