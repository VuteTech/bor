// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package reports

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/VuteTech/Bor/server/pkg/report"
)

// SourceNode is a node in scope, as the service loads it.
type SourceNode struct {
	ID, Name, FQDN                string
	OSName, OSVersion, DesktopEnv string
	AgentVersion, Status          string
	LastSeen                      *time.Time
	GroupIDs, GroupNames          []string
	LSM, LSMMode                  string
	CertNotAfter                  *time.Time
	DiskEncryption                *SourceDiskEncryption
	Updates                       *SourceUpdates
}

// SourceDiskEncryption is the disk encryption inventory of a node; nil
// when the node never reported one.
type SourceDiskEncryption struct {
	SystemVolumeEncrypted   bool
	Cipher                  string
	Escrowed                bool
	TPM2Present             *bool
	SecureBoot              string
	UnencryptedSystemMounts int
	ReportedAt              *time.Time
}

// SourceUpdates is the Software updates inventory of a node; nil when the
// node never reported one.
type SourceUpdates struct {
	Engine, EffectiveMode         string
	PendingTotal, PendingSecurity int
	RebootRequired                bool
	RebootSince, ReportedAt       *time.Time
}

// SourceResult is one live compliance result in scope.
type SourceResult struct {
	NodeID, PolicyID       string
	PolicyName, PolicyType string
	PolicyVersion          int
	Status, Message        string
	ReportedAt             time.Time
	ItemsJSON              []byte
}

// Source is everything a report is built from.
type Source struct {
	Nodes   []SourceNode
	Results []SourceResult
}

// Control is a benchmark control an item maps to.
type Control struct {
	Framework, ID, Title string
}

// Options parameterise a build.
type Options struct {
	ID            string
	GeneratedAt   time.Time
	Trigger       string
	Actor         report.Actor
	Schedule      *report.ScheduleRef
	ServerVersion string
	Label, Notes  string
	Scope         report.Scope
	// StaleAfter is the age after which a result is stale and a silent node
	// unreachable; zero disables both flags.
	StaleAfter time.Duration
	// IncludeItems keeps the per-setting items in the results. Controls are
	// aggregated from the items either way.
	IncludeItems bool
	// ControlsFor maps an item schema id to its benchmark controls; nil
	// means no controls.
	ControlsFor func(schemaID string) []Control
}

// Build assembles the bundle. The result is deterministic for the same
// source and options: every list is sorted.
func Build(src *Source, opts *Options) *report.Bundle {
	b := &report.Bundle{
		APIVersion:  report.APIVersion,
		Kind:        report.Kind,
		ID:          opts.ID,
		GeneratedAt: report.FormatTime(opts.GeneratedAt),
		Trigger:     opts.Trigger,
		GeneratedBy: opts.Actor,
		Schedule:    opts.Schedule,
		Server:      report.Server{Version: opts.ServerVersion},
		Label:       opts.Label,
		Notes:       opts.Notes,
		Scope:       normalizeScope(&opts.Scope),
		Thresholds:  report.Thresholds{StaleAfterSeconds: int64(opts.StaleAfter / time.Second)},
		Controls:    []report.ControlSummary{},
		Policies:    []report.PolicySummary{},
		Nodes:       []report.Node{},
		Results:     []report.Result{},
	}

	nodes := make(map[string]*report.Node, len(src.Nodes))
	for i := range src.Nodes {
		n := buildNode(&src.Nodes[i], opts)
		nodes[n.ID] = n
	}

	policies := make(map[string]*report.PolicySummary)
	controls := make(map[[2]string]*controlAgg)
	withResults := make(map[string]bool)

	for i := range src.Results {
		r := &src.Results[i]
		node, ok := nodes[r.NodeID]
		if !ok {
			continue // a result of a node outside the scope
		}
		withResults[r.NodeID] = true
		age := opts.GeneratedAt.Sub(r.ReportedAt)
		if age < 0 {
			age = 0
		}
		res := report.Result{
			Node:       node.Name,
			NodeID:     r.NodeID,
			PolicyID:   r.PolicyID,
			Status:     normalizeStatus(r.Status),
			Message:    r.Message,
			ReportedAt: report.FormatTime(r.ReportedAt),
			AgeSeconds: int64(age / time.Second),
			Stale:      opts.StaleAfter > 0 && age > opts.StaleAfter,
		}
		items := parseItems(r.ItemsJSON, opts.ControlsFor)
		for i := range items {
			for _, c := range items[i].Controls {
				key := [2]string{c.Framework, c.ID}
				agg := controls[key]
				if agg == nil {
					agg = &controlAgg{title: controlTitle(opts.ControlsFor, items[i].SchemaID, c), failing: make(map[string]bool)}
					controls[key] = agg
				}
				agg.add(items[i].Status, r.NodeID)
			}
		}
		if opts.IncludeItems && len(items) > 0 {
			res.Items = items
		}
		b.Results = append(b.Results, res)
		b.Summary.ByStatus.Add(res.Status)
		if res.Stale {
			b.Summary.ResultsStale++
		}
		node.ByStatus.Add(res.Status)

		p := policies[r.PolicyID]
		if p == nil {
			p = &report.PolicySummary{ID: r.PolicyID, Name: r.PolicyName, Type: r.PolicyType, Version: r.PolicyVersion}
			policies[r.PolicyID] = p
		}
		p.ByStatus.Add(res.Status)
	}

	for _, n := range nodes {
		b.Nodes = append(b.Nodes, *n)
		b.Summary.Nodes++
		if n.Status == "online" {
			b.Summary.NodesOnline++
		}
		if n.Unreachable {
			b.Summary.NodesUnreachable++
		}
		if !withResults[n.ID] {
			b.Summary.NodesWithoutResults++
		}
	}
	for _, p := range policies {
		b.Policies = append(b.Policies, *p)
	}
	for key, agg := range controls {
		b.Controls = append(b.Controls, report.ControlSummary{
			Framework: key[0], ID: key[1], Title: agg.title,
			Pass: agg.pass, Fail: agg.fail, Error: agg.errors, Inapplicable: agg.inapplicable,
			NodesFailing: len(agg.failing),
		})
	}
	b.Summary.Results = len(b.Results)
	b.Summary.ScorePct = b.Summary.ByStatus.Score()

	sort.Slice(b.Nodes, func(i, j int) bool {
		if b.Nodes[i].Name != b.Nodes[j].Name {
			return b.Nodes[i].Name < b.Nodes[j].Name
		}
		return b.Nodes[i].ID < b.Nodes[j].ID
	})
	sort.Slice(b.Policies, func(i, j int) bool {
		if b.Policies[i].Name != b.Policies[j].Name {
			return b.Policies[i].Name < b.Policies[j].Name
		}
		return b.Policies[i].ID < b.Policies[j].ID
	})
	policyName := func(id string) string {
		if p := policies[id]; p != nil {
			return p.Name
		}
		return ""
	}
	sort.Slice(b.Results, func(i, j int) bool {
		a, c := b.Results[i], b.Results[j]
		if a.Node != c.Node {
			return a.Node < c.Node
		}
		if a.NodeID != c.NodeID {
			return a.NodeID < c.NodeID
		}
		if pa, pc := policyName(a.PolicyID), policyName(c.PolicyID); pa != pc {
			return pa < pc
		}
		return a.PolicyID < c.PolicyID
	})
	sort.Slice(b.Controls, func(i, j int) bool {
		if b.Controls[i].Framework != b.Controls[j].Framework {
			return b.Controls[i].Framework < b.Controls[j].Framework
		}
		return b.Controls[i].ID < b.Controls[j].ID
	})
	return b
}

type controlAgg struct {
	title                            string
	pass, fail, errors, inapplicable int
	failing                          map[string]bool
}

func (a *controlAgg) add(status, nodeID string) {
	switch status {
	case report.StatusCompliant:
		a.pass++
	case report.StatusNonCompliant:
		a.fail++
		a.failing[nodeID] = true
	case report.StatusError:
		a.errors++
		a.failing[nodeID] = true
	case report.StatusInapplicable:
		a.inapplicable++
	}
}

func controlTitle(controlsFor func(string) []Control, schemaID string, ref report.ControlRef) string {
	if controlsFor == nil {
		return ""
	}
	for _, c := range controlsFor(schemaID) {
		if c.Framework == ref.Framework && c.ID == ref.ID {
			return c.Title
		}
	}
	return ""
}

func buildNode(src *SourceNode, opts *Options) *report.Node {
	n := &report.Node{
		ID:           src.ID,
		Name:         src.Name,
		FQDN:         src.FQDN,
		OSName:       src.OSName,
		OSVersion:    src.OSVersion,
		DesktopEnv:   src.DesktopEnv,
		AgentVersion: src.AgentVersion,
		Status:       src.Status,
		Groups:       append([]string{}, src.GroupNames...),
		Posture:      report.Posture{LSM: src.LSM, LSMMode: src.LSMMode},
	}
	if n.Status == "" {
		n.Status = report.StatusUnknown
	}
	sort.Strings(n.Groups)
	if src.LastSeen != nil {
		n.LastSeen = report.FormatTime(*src.LastSeen)
	}
	if opts.StaleAfter > 0 && n.Status != "online" {
		n.Unreachable = src.LastSeen == nil || opts.GeneratedAt.Sub(*src.LastSeen) > opts.StaleAfter
	}
	if src.CertNotAfter != nil {
		n.Posture.CertificateNotAfter = report.FormatTime(*src.CertNotAfter)
	}
	if d := src.DiskEncryption; d != nil {
		n.Posture.DiskEncryption = &report.DiskEncryptionPosture{
			SystemVolumeEncrypted:   d.SystemVolumeEncrypted,
			Cipher:                  d.Cipher,
			RecoveryKeyEscrowed:     d.Escrowed,
			TPM2Present:             d.TPM2Present,
			SecureBoot:              d.SecureBoot,
			UnencryptedSystemMounts: d.UnencryptedSystemMounts,
		}
		if d.ReportedAt != nil {
			n.Posture.DiskEncryption.ReportedAt = report.FormatTime(*d.ReportedAt)
		}
	}
	if u := src.Updates; u != nil {
		n.Posture.Updates = &report.UpdatesPosture{
			Engine:          u.Engine,
			EffectiveMode:   u.EffectiveMode,
			PendingTotal:    u.PendingTotal,
			PendingSecurity: u.PendingSecurity,
			RebootRequired:  u.RebootRequired,
		}
		if u.RebootSince != nil {
			n.Posture.Updates.RebootSince = report.FormatTime(*u.RebootSince)
		}
		if u.ReportedAt != nil {
			n.Posture.Updates.ReportedAt = report.FormatTime(*u.ReportedAt)
		}
	}
	return n
}

// storedItem is the shape the agent's items are stored in compliance_results.
type storedItem struct {
	SchemaID string `json:"schema_id"`
	Key      string `json:"key"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

func parseItems(raw []byte, controlsFor func(string) []Control) []report.Item {
	if len(raw) == 0 {
		return nil
	}
	var stored []storedItem
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil
	}
	items := make([]report.Item, 0, len(stored))
	for _, it := range stored {
		item := report.Item{SchemaID: it.SchemaID, Key: it.Key, Status: normalizeStatus(it.Status), Message: it.Message}
		if controlsFor != nil {
			for _, c := range controlsFor(it.SchemaID) {
				item.Controls = append(item.Controls, report.ControlRef{Framework: c.Framework, ID: c.ID})
			}
		}
		items = append(items, item)
	}
	return items
}

func normalizeStatus(s string) string {
	switch s {
	case report.StatusCompliant, report.StatusNonCompliant, report.StatusError, report.StatusInapplicable:
		return s
	default:
		return report.StatusUnknown
	}
}

func normalizeScope(s *report.Scope) report.Scope {
	out := report.Scope{
		AllNodes:    s.AllNodes,
		NodeGroups:  append([]report.Ref{}, s.NodeGroups...),
		PolicyTypes: append([]string{}, s.PolicyTypes...),
		Policies:    append([]report.Ref{}, s.Policies...),
	}
	sort.Slice(out.NodeGroups, func(i, j int) bool { return refLess(out.NodeGroups[i], out.NodeGroups[j]) })
	sort.Strings(out.PolicyTypes)
	sort.Slice(out.Policies, func(i, j int) bool { return refLess(out.Policies[i], out.Policies[j]) })
	return out
}

func refLess(a, b report.Ref) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}
