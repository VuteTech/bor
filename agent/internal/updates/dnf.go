// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RenderDnfAutomaticConf renders /etc/dnf/automatic.conf for the dnf5
// automatic plugin and DNF 4's dnf-automatic alike. The engine never
// reboots on its own; Bor does.
func RenderDnfAutomaticConf(mode Mode, holds []string, offline bool) []byte {
	apply := "no"
	upgradeType := "default"
	switch mode {
	case ModeSecurity:
		apply = "yes"
		upgradeType = "security"
	case ModeAll:
		apply = "yes"
	}
	var b strings.Builder
	b.WriteString("# " + Header + "\n")
	if offline && apply == "yes" {
		// The engine downloads; Bor stores the offline transaction and the
		// reboot installs it.
		apply = "no"
		b.WriteString(DnfOfflineMarker + "\n")
	}
	b.WriteString("[commands]\n")
	b.WriteString("apply_updates = " + apply + "\n")
	b.WriteString("download_updates = yes\n")
	b.WriteString("upgrade_type = " + upgradeType + "\n")
	b.WriteString("random_sleep = 0\n")
	b.WriteString("network_online_timeout = 60\n")
	b.WriteString("reboot = never\n")
	b.WriteString("\n[emitters]\n")
	b.WriteString("emit_via = stdio\n")
	b.WriteString("emit_no_updates = no\n")
	if len(holds) > 0 {
		b.WriteString("\n[base]\n")
		b.WriteString("excludepkgs = " + strings.Join(holds, " ") + "\n")
	}
	return []byte(b.String())
}

// DnfOfflineMarker is the comment Bor writes into automatic.conf when the
// updates are installed offline at the reboot, so the effective mode
// reads as the policy's mode although apply_updates is off.
const DnfOfflineMarker = "# offline_updates = yes"

// Pending is one update a node can install.
type Pending struct {
	Name     string
	Version  string
	Advisory string
	Severity string
	CVEs     []string
	Security bool
}

// Run is the last engine run a node could find.
type Run struct {
	Time     time.Time
	Success  bool
	Packages int
	Message  string
}

type dnfAdvisory struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Nevra    string `json:"nevra"`
}

// nevraName strips the version from name-version-release.arch.
func nevraName(nevra string) string {
	// name-[epoch:]version-release.arch: the name ends before the second
	// dash from the end.
	parts := strings.Split(nevra, "-")
	if len(parts) < 3 {
		return nevra
	}
	return strings.Join(parts[:len(parts)-2], "-")
}

var dnfSeverityWords = map[string]string{"critical": "critical", "important": "important", "moderate": "moderate", "low": "low"}

// DnfPending lists the upgrades dnf would install, with the security
// advisory of each package when it has one.
func DnfPending(ctx context.Context, run Runner, p *Platform) ([]Pending, error) {
	out, err := run(ctx, p.DnfBin, "-q", "-C", "check-upgrade")
	// dnf exits 100 when updates are available, 0 when none.
	if err != nil && ExitCode(err) != 100 {
		return nil, err
	}
	var pending []Pending
	seen := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || strings.HasPrefix(line, " ") || !strings.Contains(f[0], ".") {
			continue
		}
		name := f[0]
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = len(pending)
		pending = append(pending, Pending{Name: name, Version: f[1]})
	}
	advisories, aerr := dnfSecurityAdvisories(ctx, run, p)
	if aerr == nil {
		cves := map[string][]string{}
		if len(advisories) > 0 {
			cves = dnfAdvisoryCVEs(ctx, run, p)
		}
		for _, a := range advisories {
			name := nevraName(a.Nevra)
			i, ok := seen[name]
			if !ok {
				continue
			}
			pending[i].Security = true
			if pending[i].Advisory == "" {
				pending[i].Advisory = a.Name
			}
			if sev := dnfSeverityWords[strings.ToLower(a.Severity)]; sev != "" && severityRank(sev) > severityRank(pending[i].Severity) {
				pending[i].Severity = sev
			}
			for _, c := range cves[a.Name] {
				if !containsStr(pending[i].CVEs, c) {
					pending[i].CVEs = append(pending[i].CVEs, c)
				}
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Name < pending[j].Name })
	return pending, nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var cveRE = regexp.MustCompile(`CVE-\d{4}-\d{4,}`)

// dnfAdvisoryCVEs maps security advisories to their CVE ids. dnf5's
// advisory info lists references typed "cve" on RHEL and bugzilla entries
// whose titles name the CVE on Fedora; dnf4's updateinfo info prints a
// CVEs block per advisory.
func dnfAdvisoryCVEs(ctx context.Context, run Runner, p *Platform) map[string][]string {
	out := map[string][]string{}
	add := func(name, cve string) {
		if !containsStr(out[name], cve) {
			out[name] = append(out[name], cve)
		}
	}
	if p.DNF5 {
		data, err := run(ctx, p.DnfBin, "-q", "-C", "advisory", "info", "--security", "--json")
		if err != nil {
			return out
		}
		for name, cves := range ParseDnf5AdvisoryInfo(data) {
			for _, c := range cves {
				add(name, c)
			}
		}
		return out
	}
	data, err := run(ctx, p.DnfBin, "-q", "-C", "updateinfo", "info", "--security")
	if err != nil {
		return out
	}
	for name, cves := range ParseDnf4UpdateinfoInfo(data) {
		for _, c := range cves {
			add(name, c)
		}
	}
	return out
}

// ParseDnf5AdvisoryInfo extracts the CVE ids of each advisory from
// "dnf5 advisory info --json".
func ParseDnf5AdvisoryInfo(data []byte) map[string][]string {
	var doc map[string]struct {
		Description string `json:"Description"`
		References  []struct {
			Title string `json:"Title"`
			ID    string `json:"Id"`
			Type  string `json:"Type"`
		} `json:"references"`
	}
	out := map[string][]string{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out
	}
	for name, adv := range doc {
		seen := map[string]bool{}
		add := func(c string) {
			if c != "" && !seen[c] {
				seen[c] = true
				out[name] = append(out[name], c)
			}
		}
		for _, r := range adv.References {
			if strings.EqualFold(r.Type, "cve") {
				add(cveRE.FindString(r.ID))
				continue
			}
			for _, c := range cveRE.FindAllString(r.Title, -1) {
				add(c)
			}
		}
		for _, c := range cveRE.FindAllString(adv.Description, -1) {
			add(c)
		}
		sort.Strings(out[name])
	}
	return out
}

// ParseDnf4UpdateinfoInfo extracts the CVE ids of each advisory from
// "dnf updateinfo info" (Update ID and CVEs blocks).
func ParseDnf4UpdateinfoInfo(data []byte) map[string][]string {
	out := map[string][]string{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "Update ID" {
			current = v
			continue
		}
		if current == "" || (k != "CVEs" && k != "") {
			continue
		}
		for _, c := range cveRE.FindAllString(v, -1) {
			if !containsStr(out[current], c) {
				out[current] = append(out[current], c)
			}
		}
	}
	return out
}

var dnf4UpdateinfoRE = regexp.MustCompile(`^(\S+)\s+(\S+)/Sec\.\s+(\S+)$`)

func dnfSecurityAdvisories(ctx context.Context, run Runner, p *Platform) ([]dnfAdvisory, error) {
	if p.DNF5 {
		out, err := run(ctx, p.DnfBin, "-q", "-C", "advisory", "list", "--security", "--json")
		if err != nil {
			return nil, err
		}
		var list []dnfAdvisory
		if jerr := json.Unmarshal(out, &list); jerr != nil {
			return nil, jerr
		}
		return list, nil
	}
	out, err := run(ctx, p.DnfBin, "-q", "-C", "updateinfo", "list", "--security")
	if err != nil {
		return nil, err
	}
	var list []dnfAdvisory
	for _, line := range strings.Split(string(out), "\n") {
		m := dnf4UpdateinfoRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		list = append(list, dnfAdvisory{Name: m[1], Type: "security", Severity: m[2], Nevra: m[3]})
	}
	return list, nil
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "important":
		return 3
	case "moderate":
		return 2
	case "low":
		return 1
	}
	return 0
}

var dnfHistoryRE = regexp.MustCompile(`^\s*(\d+)\s*\|?\s*(.*?)\s*\|?\s{2,}(\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?)\s*\|?\s*(.*?)\s*\|?\s+(\d+)\s*\|?\s*$`)

// DnfLastRun finds the newest "automatic" transaction in the dnf history.
func DnfLastRun(ctx context.Context, run Runner, p *Platform) *Run {
	out, err := run(ctx, p.DnfBin, "-q", "history", "list")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		m := dnfHistoryRE.FindStringSubmatch(line)
		if len(m) < 6 || !strings.Contains(m[2], "automatic") {
			continue
		}
		t, terr := time.ParseInLocation("2006-01-02 15:04:05", m[3], time.Local)
		if terr != nil {
			t, terr = time.ParseInLocation("2006-01-02 15:04", m[3], time.Local)
			if terr != nil {
				continue
			}
		}
		n, _ := strconv.Atoi(m[5])
		return &Run{Time: t, Success: true, Packages: n, Message: strings.TrimSpace(m[2])}
	}
	return nil
}

// DnfNeedsRestarting runs needs-restarting: exit 1 means a reboot is
// needed. The reason is the tool's first line.
func DnfNeedsRestarting(ctx context.Context, run Runner, p *Platform) (needed bool, reason string) {
	var out []byte
	var err error
	if p.DNF5 {
		out, err = run(ctx, p.DnfBin, "-q", "-C", "needs-restarting")
	} else {
		out, err = run(ctx, p.DnfBin, "-q", "-C", "needs-restarting", "-r")
	}
	if err == nil {
		return false, ""
	}
	if ExitCode(err) != 1 {
		return false, ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "*") {
			return true, "needs-restarting: " + line
		}
	}
	return true, "needs-restarting: core libraries or services were updated"
}
