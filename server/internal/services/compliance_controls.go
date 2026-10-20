// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "encoding/json"

// ComplianceControl is a benchmark control that a compliance item
// contributes to, so results can be read by the controls an auditor asks
// about. Short is the label shown next to the item.
type ComplianceControl struct {
	Framework string `json:"framework"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Short     string `json:"short"`
}

const (
	cisRHEL9  = "CIS Red Hat Enterprise Linux 9 Benchmark v2.0.0"
	cisUbuntu = "CIS Ubuntu Linux 24.04 LTS Benchmark v1.0.0"
)

func cis(framework, short, id, title string) ComplianceControl {
	return ComplianceControl{Framework: framework, ID: id, Title: title, Short: short + " " + id}
}

// complianceControls maps a compliance item schema id to the controls it
// contributes to. The mandatory access control items map onto the SELinux
// and AppArmor sections of the CIS benchmarks; the numbering is that of
// the benchmark version named, other versions of the same benchmark use
// other numbers for the same controls.
var complianceControls = map[string][]ComplianceControl{
	"selinux:active": {
		cis(cisRHEL9, "CIS RHEL 9", "1.3.1.1", "Ensure SELinux is installed"),
		cis(cisRHEL9, "CIS RHEL 9", "1.3.1.2", "Ensure SELinux is not disabled in bootloader configuration"),
		cis(cisRHEL9, "CIS RHEL 9", "1.3.1.4", "Ensure the SELinux mode is not disabled"),
	},
	"selinux:mode": {
		cis(cisRHEL9, "CIS RHEL 9", "1.3.1.4", "Ensure the SELinux mode is not disabled"),
		cis(cisRHEL9, "CIS RHEL 9", "1.3.1.5", "Ensure the SELinux mode is enforcing"),
	},
	"apparmor:enabled": {
		cis(cisUbuntu, "CIS Ubuntu 24.04", "1.3.1.1", "Ensure AppArmor is installed"),
		cis(cisUbuntu, "CIS Ubuntu 24.04", "1.3.1.2", "Ensure AppArmor is enabled in the bootloader configuration"),
	},
	"apparmor:profile": {
		cis(cisUbuntu, "CIS Ubuntu 24.04", "1.3.1.3", "Ensure all AppArmor Profiles are in enforce or complain mode"),
		cis(cisUbuntu, "CIS Ubuntu 24.04", "1.3.1.4", "Ensure all AppArmor Profiles are enforcing"),
	},
}

// ComplianceControlsFor returns the benchmark controls an item with the
// given schema id contributes to, or nil when none is known.
func ComplianceControlsFor(schemaID string) []ComplianceControl {
	controls := complianceControls[schemaID]
	if len(controls) == 0 {
		return nil
	}
	out := make([]ComplianceControl, len(controls))
	copy(out, controls)
	return out
}

// AnnotateComplianceItems adds a "controls" list to every item of a
// compliance result's items document whose schema id maps to benchmark
// controls. Documents that do not parse, and items without controls, are
// returned as they are.
func AnnotateComplianceItems(items json.RawMessage) json.RawMessage {
	if len(items) == 0 {
		return items
	}
	var decoded []map[string]json.RawMessage
	if err := json.Unmarshal(items, &decoded); err != nil {
		return items
	}
	changed := false
	for _, item := range decoded {
		var schemaID string
		if raw, ok := item["schema_id"]; ok {
			_ = json.Unmarshal(raw, &schemaID)
		}
		controls := ComplianceControlsFor(schemaID)
		if controls == nil {
			continue
		}
		encoded, err := json.Marshal(controls)
		if err != nil {
			continue
		}
		item["controls"] = encoded
		changed = true
	}
	if !changed {
		return items
	}
	out, err := json.Marshal(decoded)
	if err != nil {
		return items
	}
	return out
}
