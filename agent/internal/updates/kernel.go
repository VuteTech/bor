// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// RunningKernel reads the running kernel release.
func RunningKernel(root string) string {
	data, err := os.ReadFile(root + "/proc/sys/kernel/osrelease") //nolint:gosec // G304: fixed path
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// BootTime returns when the node booted, from /proc/stat btime.
func BootTime(root string) time.Time {
	data, err := os.ReadFile(root + "/proc/stat") //nolint:gosec // G304: fixed path
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			if n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
	}
	return time.Time{}
}

// InstalledKernels lists the installed kernel versions in the running
// kernel's naming: the /boot/vmlinuz-<release> images, which every
// distribution names after uname -r (SUSE's package version differs from
// its release string, so packages are only the fallback).
func InstalledKernels(ctx context.Context, run Runner, p *Platform) []string {
	var out []string
	if entries, err := os.ReadDir(p.Path("/boot")); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "vmlinuz-") || e.IsDir() {
				continue
			}
			rel := strings.TrimPrefix(name, "vmlinuz-")
			if rel == "" || strings.HasPrefix(rel, "0-rescue") || strings.HasSuffix(rel, ".old") {
				continue
			}
			out = append(out, rel)
		}
		if len(out) > 0 {
			return uniqueSorted(out)
		}
	}
	switch {
	case p.RpmBin != "" && p.Engine != EngineApt:
		data, _ := run(ctx, p.RpmBin, "-q", "--qf", "%{VERSION}-%{RELEASE}.%{ARCH}\n", "kernel-core", "kernel", "kernel-default", "kernel-longterm", "kernel-base")
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.Contains(l, "not installed") {
				continue
			}
			out = append(out, l)
		}
	case p.DpkgQueryBin != "":
		data, _ := run(ctx, p.DpkgQueryBin, "-W", "-f", "${Package}\t${Status}\n", "linux-image-[0-9]*")
		for _, l := range strings.Split(string(data), "\n") {
			name, status, ok := strings.Cut(l, "\t")
			if !ok || !strings.Contains(status, "installed") || strings.Contains(status, "deinstall") {
				continue
			}
			v := strings.TrimPrefix(name, "linux-image-")
			v = strings.TrimSuffix(v, "-unsigned")
			out = append(out, v)
		}
	}
	return uniqueSorted(out)
}

// NewestKernel picks the highest version of the installed kernels.
func NewestKernel(installed []string) string {
	best := ""
	for _, k := range installed {
		if best == "" || CompareVersions(k, best) > 0 {
			best = k
		}
	}
	return best
}

// KernelRebootNeeded reports whether the newest installed kernel differs
// from and is newer than the running one.
func KernelRebootNeeded(running, newest string) bool {
	if running == "" || newest == "" || running == newest {
		return false
	}
	return CompareVersions(newest, running) > 0
}

// CompareVersions compares two version strings the way package managers
// roughly do: alternating numeric and alphabetic runs, numbers compared
// numerically, with a tilde sorting before anything.
func CompareVersions(a, b string) int {
	ta, tb := versionTokens(a), versionTokens(b)
	for i := 0; i < len(ta) || i < len(tb); i++ {
		var x, y string
		if i < len(ta) {
			x = ta[i]
		}
		if i < len(tb) {
			y = tb[i]
		}
		if x == y {
			continue
		}
		if x == "~" {
			return -1
		}
		if y == "~" {
			return 1
		}
		if x == "" {
			return -1
		}
		if y == "" {
			return 1
		}
		xd, yd := isDigits(x), isDigits(y)
		switch {
		case xd && yd:
			nx, _ := strconv.ParseInt(strings.TrimLeft(x, "0"), 10, 64)
			ny, _ := strconv.ParseInt(strings.TrimLeft(y, "0"), 10, 64)
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case xd:
			return 1
		case yd:
			return -1
		default:
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionTokens(v string) []string {
	var out []string
	cur := strings.Builder{}
	kind := 0 // 1 digits, 2 letters
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		kind = 0
	}
	for _, r := range v {
		switch {
		case unicode.IsDigit(r):
			if kind != 1 {
				flush()
				kind = 1
			}
			cur.WriteRune(r)
		case unicode.IsLetter(r):
			if kind != 2 {
				flush()
				kind = 2
			}
			cur.WriteRune(r)
		case r == '~':
			flush()
			out = append(out, "~")
		default:
			flush()
		}
	}
	flush()
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
