// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"
)

// AptLogPath is the unattended-upgrades log.
const AptLogPath = "/var/log/unattended-upgrades/unattended-upgrades.log"

// aptSecurityPatterns are the origins of security updates, per
// distribution, in unattended-upgrades' Origins-Pattern syntax. Debian's
// security archive carries its own codename.
func aptSecurityPatterns(osID string) []string {
	if osID == "debian" {
		return []string{
			"origin=Debian,codename=${distro_codename},label=Debian-Security",
			"origin=Debian,codename=${distro_codename}-security,label=Debian-Security",
		}
	}
	return []string{
		"origin=Ubuntu,archive=${distro_codename}-security",
		"origin=UbuntuESMApps,archive=${distro_codename}-apps-security",
		"origin=UbuntuESM,archive=${distro_codename}-infra-security",
	}
}

// aptAllPatterns are the origins of every update of the release.
func aptAllPatterns(osID string) []string {
	if osID == "debian" {
		return []string{
			"origin=Debian,codename=${distro_codename},label=Debian",
			"origin=Debian,codename=${distro_codename},label=Debian-Security",
			"origin=Debian,codename=${distro_codename}-security,label=Debian-Security",
			"origin=Debian,codename=${distro_codename}-updates,label=Debian",
		}
	}
	return append([]string{
		"origin=Ubuntu,archive=${distro_codename}",
		"origin=Ubuntu,archive=${distro_codename}-updates",
	}, aptSecurityPatterns(osID)...)
}

var aptRegexMeta = regexp.MustCompile(`[.+?()\[\]{}|^$\\]`)

// HoldToRegex turns a package pattern ("linux-image-*", "firefox") into
// the anchored regular expression unattended-upgrades expects.
func HoldToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(aptRegexMeta.ReplaceAllString(string(r), `\$0`))
		}
	}
	b.WriteString("$")
	return b.String()
}

// RenderAptConf renders /etc/apt/apt.conf.d/90bor-updates. apt lists are
// additive and can only be cleared, so the file clears both origin lists
// and the blacklist before declaring its own.
func RenderAptConf(mode Mode, osID string, holds []string, removeUnused bool) []byte {
	var b strings.Builder
	b.WriteString("// " + Header + "\n")
	unattended := "0"
	var patterns []string
	switch mode {
	case ModeSecurity:
		unattended = "1"
		patterns = aptSecurityPatterns(osID)
	case ModeAll:
		unattended = "1"
		patterns = aptAllPatterns(osID)
	}
	b.WriteString(`APT::Periodic::Update-Package-Lists "1";` + "\n")
	b.WriteString(`APT::Periodic::Download-Upgradeable-Packages "1";` + "\n")
	b.WriteString(`APT::Periodic::Unattended-Upgrade "` + unattended + `";` + "\n")
	b.WriteString("#clear Unattended-Upgrade::Allowed-Origins;\n")
	b.WriteString("#clear Unattended-Upgrade::Origins-Pattern;\n")
	if len(patterns) > 0 {
		b.WriteString("Unattended-Upgrade::Origins-Pattern {\n")
		for _, pat := range patterns {
			b.WriteString("  \"" + pat + "\";\n")
		}
		b.WriteString("};\n")
	}
	b.WriteString("#clear Unattended-Upgrade::Package-Blacklist;\n")
	if len(holds) > 0 {
		b.WriteString("Unattended-Upgrade::Package-Blacklist {\n")
		for _, h := range holds {
			b.WriteString("  \"" + HoldToRegex(h) + "\";\n")
		}
		b.WriteString("};\n")
	}
	b.WriteString(`Unattended-Upgrade::Automatic-Reboot "false";` + "\n")
	if removeUnused {
		b.WriteString(`Unattended-Upgrade::Remove-Unused-Dependencies "true";` + "\n")
	} else {
		b.WriteString(`Unattended-Upgrade::Remove-Unused-Dependencies "false";` + "\n")
	}
	b.WriteString(`Unattended-Upgrade::SyslogEnable "true";` + "\n")
	return []byte(b.String())
}

var aptListRE = regexp.MustCompile(`^([^/\s]+)/(\S+)\s+(\S+)\s+\S+\s+\[upgradable from: ([^\]]*)\]`)

// AptPending parses "apt list --upgradable": security updates are the
// ones from a security archive.
func AptPending(ctx context.Context, run Runner, p *Platform) ([]Pending, error) {
	bin := "apt"
	if p.AptGetBin != "" {
		bin = strings.TrimSuffix(p.AptGetBin, "-get")
	}
	out, err := run(ctx, bin, "list", "--upgradable")
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var pending []Pending
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		m := aptListRE.FindStringSubmatch(line)
		if len(m) < 5 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		archive := m[2]
		pending = append(pending, Pending{Name: m[1], Version: m[3], Security: strings.Contains(archive, "security")})
	}
	return pending, nil
}

var aptLogLineRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}),\d+ (\w+) (.*)$`)

// AptLastRun reads the unattended-upgrades log for the last run: its
// start, whether it installed anything, and its outcome line.
func AptLastRun(logPath string) *Run {
	data, err := os.ReadFile(logPath) //nolint:gosec // G304: fixed log path
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	// Walk backwards to the last "Starting" line, then forward through
	// that run.
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "Starting unattended upgrades script") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	m := aptLogLineRE.FindStringSubmatch(lines[start])
	if m == nil {
		return nil
	}
	t, terr := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
	if terr != nil {
		return nil
	}
	r := &Run{Time: t, Success: true}
	for _, line := range lines[start+1:] {
		lm := aptLogLineRE.FindStringSubmatch(line)
		if lm == nil {
			continue
		}
		level, msg := lm[2], lm[3]
		switch {
		case strings.HasPrefix(msg, "Packages that will be upgraded:"):
			r.Packages = len(strings.Fields(strings.TrimPrefix(msg, "Packages that will be upgraded:")))
		case strings.HasPrefix(msg, "All upgrades installed"):
			r.Message = msg
		case strings.HasPrefix(msg, "No packages found that can be upgraded unattended"):
			r.Message = "No packages found that can be upgraded unattended"
		case level == "ERROR" || level == "WARNING" && strings.Contains(msg, "Lock could not be acquired"):
			r.Success = false
			r.Message = msg
		}
	}
	if r.Message == "" {
		r.Message = "run started"
	}
	return r
}

// NeedrestartKernel parses "needrestart -b": KSTA 3 means the installed
// kernel differs from the running one; 2 means an ABI-compatible upgrade.
func NeedrestartKernel(ctx context.Context, run Runner, p *Platform) (needed bool, reason string, services []string) {
	if p.NeedrestartBin == "" {
		return false, "", nil
	}
	out, _ := run(ctx, p.NeedrestartBin, "-b")
	var cur, exp, sta string
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		switch k {
		case "NEEDRESTART-KCUR":
			cur = v
		case "NEEDRESTART-KEXP":
			exp = v
		case "NEEDRESTART-KSTA":
			sta = v
		case "NEEDRESTART-SVC":
			services = append(services, v)
		}
	}
	if sta == "3" || sta == "2" {
		return true, "kernel " + exp + " installed, " + cur + " running (needrestart)", services
	}
	return false, "", services
}

// RebootRequiredFile reports Debian and Ubuntu's /run/reboot-required and
// the packages that asked for it.
func RebootRequiredFile(root string) (required bool, packages []string) {
	if _, err := os.Stat(root + "/run/reboot-required"); err != nil {
		return false, nil
	}
	data, _ := os.ReadFile(root + "/run/reboot-required.pkgs") //nolint:gosec // G304: fixed path
	var pkgs []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			pkgs = append(pkgs, l)
		}
	}
	return true, pkgs
}
