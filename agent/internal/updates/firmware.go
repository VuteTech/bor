// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// FwupdRefreshTimer is fwupd's daily metadata refresh.
const FwupdRefreshTimer = "fwupd-refresh.timer"

type fwupdUpdatesJSON struct {
	Devices []struct {
		Name     string `json:"Name"`
		DeviceID string `json:"DeviceId"`
		Version  string `json:"Version"`
		Releases []struct {
			Version string   `json:"Version"`
			Urgency string   `json:"Urgency"`
			Summary string   `json:"Summary"`
			Issues  []string `json:"Issues"`
		} `json:"Releases"`
	} `json:"Devices"`
}

// ParseFwupdUpdates parses "fwupdmgr get-updates --json": one pending
// entry per device, with the newest release. An urgency of critical or
// high marks the update as a security one, the way fwupd's own client
// highlights it.
func ParseFwupdUpdates(data []byte) ([]Pending, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return nil, nil
	}
	var doc fwupdUpdatesJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []Pending
	for _, d := range doc.Devices {
		if len(d.Releases) == 0 || d.Name == "" {
			continue
		}
		r := d.Releases[0]
		p := Pending{Name: d.Name, Version: r.Version, Advisory: r.Summary}
		switch strings.ToLower(r.Urgency) {
		case "critical":
			p.Severity, p.Security = "critical", true
		case "high":
			p.Severity, p.Security = "important", true
		case "medium":
			p.Severity = "moderate"
		case "low":
			p.Severity = "low"
		}
		for _, issue := range r.Issues {
			if strings.HasPrefix(issue, "CVE-") {
				p.CVEs = append(p.CVEs, issue)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// FwupdPending lists the firmware updates fwupd knows about. fwupdmgr
// exits non-zero when there is nothing to update, so an error with no
// document means no updates.
func FwupdPending(ctx context.Context, run Runner, p *Platform) []Pending {
	if p.FwupdmgrBin == "" {
		return nil
	}
	out, _ := run(ctx, p.FwupdmgrBin, "get-updates", "--json")
	pending, err := ParseFwupdUpdates(out)
	if err != nil {
		return nil
	}
	return pending
}
