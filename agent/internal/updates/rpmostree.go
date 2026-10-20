// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// RpmOstreePolicyForMode maps a mode to rpm-ostreed's AutomaticUpdatePolicy.
// "apply" is never used: Bor owns the reboot and stages instead.
func RpmOstreePolicyForMode(mode Mode) string {
	switch mode {
	case ModeCheck, ModeSecurity:
		return "check"
	case ModeAll:
		return "stage"
	}
	return "none"
}

var rpmOstreePolicyLineRE = regexp.MustCompile(`(?m)^#?\s*AutomaticUpdatePolicy\s*=.*$`)

// SetRpmOstreePolicy rewrites the AutomaticUpdatePolicy line of
// rpm-ostreed.conf, keeping everything else, and adds a [Daemon] section
// when the file has none.
func SetRpmOstreePolicy(content []byte, policy string) []byte {
	line := "AutomaticUpdatePolicy=" + policy
	s := string(content)
	if rpmOstreePolicyLineRE.MatchString(s) {
		return []byte(rpmOstreePolicyLineRE.ReplaceAllString(s, line))
	}
	if strings.Contains(s, "[Daemon]") {
		return []byte(strings.Replace(s, "[Daemon]", "[Daemon]\n"+line, 1))
	}
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s + "[Daemon]\n" + line + "\n")
}

// RpmOstreePolicy reads the effective AutomaticUpdatePolicy of the file
// ("none" when commented out or absent).
func RpmOstreePolicy(content []byte) string {
	for _, l := range strings.Split(string(content), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "AutomaticUpdatePolicy=") {
			return strings.TrimSpace(strings.TrimPrefix(l, "AutomaticUpdatePolicy="))
		}
	}
	return "none"
}

// RpmOstreeStatus is the part of "rpm-ostree status --json" Bor reads.
type RpmOstreeStatus struct {
	Booted        string
	Staged        string
	Pending       []Pending
	Transaction   bool
	CachedUpdate  time.Time
	CachedVersion string
}

type rpmOstreeStatusJSON struct {
	Deployments []struct {
		Version string `json:"version"`
		Booted  bool   `json:"booted"`
		Staged  bool   `json:"staged"`
	} `json:"deployments"`
	Transaction  json.RawMessage `json:"transaction"`
	CachedUpdate *struct {
		Version    string            `json:"version"`
		Timestamp  int64             `json:"timestamp"`
		Advisories []json.RawMessage `json:"advisories"`
	} `json:"cached-update"`
}

var rpmOstreeSeverity = []string{"", "low", "moderate", "important", "critical"}

// ParseRpmOstreeStatus parses the JSON of rpm-ostree status. Advisories
// are arrays: [id, kind, severity, [packages], {cve_references: [[url, title]]}].
func ParseRpmOstreeStatus(data []byte) (*RpmOstreeStatus, error) {
	var raw rpmOstreeStatusJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	st := &RpmOstreeStatus{}
	for _, d := range raw.Deployments {
		if d.Booted {
			st.Booted = d.Version
		}
		if d.Staged {
			st.Staged = d.Version
		}
	}
	st.Transaction = len(raw.Transaction) > 0 && string(raw.Transaction) != "null"
	if raw.CachedUpdate == nil {
		return st, nil
	}
	st.CachedVersion = raw.CachedUpdate.Version
	if raw.CachedUpdate.Timestamp > 0 {
		st.CachedUpdate = time.Unix(raw.CachedUpdate.Timestamp, 0)
	}
	seen := map[string]int{}
	for _, adv := range raw.CachedUpdate.Advisories {
		var tuple []json.RawMessage
		if json.Unmarshal(adv, &tuple) != nil || len(tuple) < 4 {
			continue
		}
		var id string
		var kind, sev int
		var pkgs []string
		_ = json.Unmarshal(tuple[0], &id)
		_ = json.Unmarshal(tuple[1], &kind)
		_ = json.Unmarshal(tuple[2], &sev)
		_ = json.Unmarshal(tuple[3], &pkgs)
		var cves []string
		if len(tuple) > 4 {
			var meta struct {
				CveReferences [][]string `json:"cve_references"`
			}
			if json.Unmarshal(tuple[4], &meta) == nil {
				for _, ref := range meta.CveReferences {
					if len(ref) > 1 {
						if f := strings.Fields(ref[1]); len(f) > 0 && strings.HasPrefix(f[0], "CVE-") {
							cves = append(cves, f[0])
						}
					}
				}
			}
		}
		severity := ""
		if sev > 0 && sev < len(rpmOstreeSeverity) {
			severity = rpmOstreeSeverity[sev]
		}
		for _, nevra := range pkgs {
			name := nevraName(nevra)
			i, ok := seen[name]
			if !ok {
				seen[name] = len(st.Pending)
				st.Pending = append(st.Pending, Pending{Name: name, Version: strings.TrimPrefix(nevra, name+"-")})
				i = seen[name]
			}
			if kind == 1 {
				st.Pending[i].Security = true
				if st.Pending[i].Advisory == "" {
					st.Pending[i].Advisory = id
				}
				if severityRank(severity) > severityRank(st.Pending[i].Severity) {
					st.Pending[i].Severity = severity
				}
				st.Pending[i].CVEs = append(st.Pending[i].CVEs, cves...)
			}
		}
	}
	return st, nil
}

// RpmOstreeReadStatus runs rpm-ostree status --json.
func RpmOstreeReadStatus(ctx context.Context, run Runner, p *Platform) (*RpmOstreeStatus, error) {
	out, err := run(ctx, p.RpmOstreeBin, "status", "--json")
	if err != nil {
		return nil, err
	}
	return ParseRpmOstreeStatus(out)
}
