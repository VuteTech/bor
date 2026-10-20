// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package reports

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/VuteTech/Bor/server/pkg/report"
)

// CSV parts of a report.
const (
	CSVResults  = "results"
	CSVItems    = "items"
	CSVNodes    = "nodes"
	CSVControls = "controls"
)

// CSVParts lists the CSV parts in display order.
var CSVParts = []string{CSVResults, CSVItems, CSVNodes, CSVControls}

// WriteCSV writes one part of the bundle as CSV.
func WriteCSV(w io.Writer, b *report.Bundle, part string) error {
	cw := csv.NewWriter(w)
	var err error
	switch part {
	case CSVResults:
		err = writeResults(cw, b)
	case CSVItems:
		err = writeItems(cw, b)
	case CSVNodes:
		err = writeNodes(cw, b)
	case CSVControls:
		err = writeControls(cw, b)
	default:
		return fmt.Errorf("reports: unknown csv part %q", part)
	}
	if err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

func writeResults(cw *csv.Writer, b *report.Bundle) error {
	policies := policyIndex(b)
	groups := groupIndex(b)
	if err := cw.Write([]string{"node", "node_groups", "policy", "policy_type", "policy_version", "status", "message", "reported_at", "age_seconds", "stale", "items", "items_failed"}); err != nil {
		return err
	}
	for i := range b.Results {
		r := &b.Results[i]
		p := policies[r.PolicyID]
		failed := 0
		for _, it := range r.Items {
			if it.Status == report.StatusNonCompliant || it.Status == report.StatusError {
				failed++
			}
		}
		row := []string{
			safe(r.Node), safe(strings.Join(groups[r.NodeID], "; ")), safe(p.Name), p.Type, strconv.Itoa(p.Version),
			r.Status, safe(r.Message), r.ReportedAt, strconv.FormatInt(r.AgeSeconds, 10), strconv.FormatBool(r.Stale),
			strconv.Itoa(len(r.Items)), strconv.Itoa(failed),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func writeItems(cw *csv.Writer, b *report.Bundle) error {
	policies := policyIndex(b)
	if err := cw.Write([]string{"node", "policy", "policy_type", "schema_id", "key", "status", "message", "controls"}); err != nil {
		return err
	}
	for i := range b.Results {
		r := &b.Results[i]
		p := policies[r.PolicyID]
		for _, it := range r.Items {
			refs := make([]string, 0, len(it.Controls))
			for _, c := range it.Controls {
				refs = append(refs, c.Framework+" "+c.ID)
			}
			if err := cw.Write([]string{safe(r.Node), safe(p.Name), p.Type, safe(it.SchemaID), safe(it.Key), it.Status, safe(it.Message), safe(strings.Join(refs, "; "))}); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeNodes(cw *csv.Writer, b *report.Bundle) error {
	if err := cw.Write([]string{"node", "fqdn", "os", "os_version", "desktop", "agent_version", "status", "last_seen", "unreachable", "node_groups",
		"lsm", "lsm_mode", "system_volume_encrypted", "recovery_key_escrowed", "tpm2_present", "secure_boot",
		"updates_engine", "pending_security", "reboot_required", "compliant", "non_compliant", "error", "inapplicable", "unknown"}); err != nil {
		return err
	}
	for i := range b.Nodes {
		n := &b.Nodes[i]
		encrypted, escrowed, tpm, secureBoot := "", "", "", ""
		if d := n.Posture.DiskEncryption; d != nil {
			encrypted = strconv.FormatBool(d.SystemVolumeEncrypted)
			escrowed = strconv.FormatBool(d.RecoveryKeyEscrowed)
			if d.TPM2Present != nil {
				tpm = strconv.FormatBool(*d.TPM2Present)
			}
			secureBoot = d.SecureBoot
		}
		engine, pending, reboot := "", "", ""
		if u := n.Posture.Updates; u != nil {
			engine = u.Engine
			pending = strconv.Itoa(u.PendingSecurity)
			reboot = strconv.FormatBool(u.RebootRequired)
		}
		c := n.ByStatus
		row := []string{
			safe(n.Name), safe(n.FQDN), safe(n.OSName), safe(n.OSVersion), safe(n.DesktopEnv), safe(n.AgentVersion), n.Status, n.LastSeen,
			strconv.FormatBool(n.Unreachable), safe(strings.Join(n.Groups, "; ")),
			n.Posture.LSM, n.Posture.LSMMode, encrypted, escrowed, tpm, secureBoot,
			engine, pending, reboot,
			strconv.Itoa(c.Compliant), strconv.Itoa(c.NonCompliant), strconv.Itoa(c.Error), strconv.Itoa(c.Inapplicable), strconv.Itoa(c.Unknown),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func writeControls(cw *csv.Writer, b *report.Bundle) error {
	if err := cw.Write([]string{"framework", "control_id", "title", "pass", "fail", "error", "inapplicable", "nodes_failing"}); err != nil {
		return err
	}
	for i := range b.Controls {
		c := &b.Controls[i]
		if err := cw.Write([]string{safe(c.Framework), safe(c.ID), safe(c.Title), strconv.Itoa(c.Pass), strconv.Itoa(c.Fail), strconv.Itoa(c.Error), strconv.Itoa(c.Inapplicable), strconv.Itoa(c.NodesFailing)}); err != nil {
			return err
		}
	}
	return nil
}

func policyIndex(b *report.Bundle) map[string]report.PolicySummary {
	idx := make(map[string]report.PolicySummary, len(b.Policies))
	for _, p := range b.Policies {
		idx[p.ID] = p
	}
	return idx
}

func groupIndex(b *report.Bundle) map[string][]string {
	idx := make(map[string][]string, len(b.Nodes))
	for i := range b.Nodes {
		idx[b.Nodes[i].ID] = b.Nodes[i].Groups
	}
	return idx
}

// safe neutralises spreadsheet formula injection: a cell that starts with
// = + - @ or a tab or carriage return gets a leading apostrophe.
func safe(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		return "'" + v
	}
	return v
}
