// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package sysinfo

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"strings"
)

// OSInfo holds detected operating system information.
type OSInfo struct {
	// Name is the distribution name.
	// Officially supported: "Fedora", "Fedora Atomic", "openSUSE Leap",
	// "openSUSE Tumbleweed", "SLES", "openSUSE Aeon", "openSUSE Kalpa", "Ubuntu".
	// Other distributions are detected and named but not officially supported.
	Name string
	// Version is the VERSION_ID from /etc/os-release (e.g. "40", "22.04").
	Version string
}

// DesktopInfo holds detected desktop environment information.
type DesktopInfo struct {
	Name    string // "KDE Plasma" or "GNOME"
	Version string // e.g. "6.1.4" or "46.1"
}

// String returns a combined display string, e.g. "KDE Plasma 6.1.4".
func (d DesktopInfo) String() string {
	if d.Version != "" {
		return d.Name + " " + d.Version
	}
	return d.Name
}

// Info holds all collected system metadata.
type Info struct {
	FQDN        string
	IPAddress   string
	OS          OSInfo
	DesktopEnvs []DesktopInfo
	MachineID   string
}

// Collect gathers system metadata. Individual failures are silently skipped;
// callers always receive a partial (potentially empty) result.
func Collect() *Info {
	return &Info{
		FQDN:        collectFQDN(),
		IPAddress:   collectIPAddress(),
		OS:          collectOS(),
		DesktopEnvs: collectDesktopEnvs(),
		MachineID:   collectMachineID(),
	}
}

func collectFQDN() string {
	if out, err := exec.Command("hostname", "--fqdn").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	h, _ := os.Hostname()
	return h
}

func collectIPAddress() string {
	// UDP dial does not transmit data; it only selects the outbound interface.
	if conn, err := net.Dial("udp4", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	return collectIPFallback()
}

func collectIPFallback() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

func collectMachineID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func collectOS() OSInfo {
	fields, err := parseOSRelease("/etc/os-release")
	if err != nil {
		return OSInfo{}
	}
	return classifyOS(fields)
}

func parseOSRelease(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		m[line[:idx]] = strings.Trim(line[idx+1:], `"`)
	}
	return m, scanner.Err()
}

func classifyOS(f map[string]string) OSInfo {
	id := strings.ToLower(f["ID"])
	variantID := strings.ToLower(f["VARIANT_ID"])
	version := f["VERSION_ID"]

	switch id {
	case "fedora":
		atomicVariants := map[string]bool{
			"kinoite": true, "silverblue": true, "sericea": true,
			"onyx": true, "aurora": true, "iot": true, "coreos": true,
		}
		if atomicVariants[variantID] || isOstree() {
			return OSInfo{Name: "Fedora Atomic", Version: version}
		}
		return OSInfo{Name: "Fedora", Version: version}

	case "ubuntu":
		return OSInfo{Name: "Ubuntu", Version: version}
	case "opensuse-leap":
		return OSInfo{Name: "openSUSE Leap", Version: version}
	case "opensuse-tumbleweed":
		return OSInfo{Name: "openSUSE Tumbleweed", Version: version}
	case "sles":
		return OSInfo{Name: "SLES", Version: version}
	case "opensuse-aeon":
		return OSInfo{Name: "openSUSE Aeon", Version: version}
	case "opensuse-kalpa":
		return OSInfo{Name: "openSUSE Kalpa", Version: version}
	case "opensuse-microos":
		switch variantID {
		case "aeon":
			return OSInfo{Name: "openSUSE Aeon", Version: version}
		case "kalpa":
			return OSInfo{Name: "openSUSE Kalpa", Version: version}
		default:
			return OSInfo{Name: "openSUSE MicroOS", Version: version}
		}
	case "debian":
		return OSInfo{Name: "Debian", Version: version}
	case "rhel":
		return OSInfo{Name: "Red Hat Enterprise Linux", Version: version}
	case "rocky":
		return OSInfo{Name: "Rocky Linux", Version: version}
	case "almalinux":
		return OSInfo{Name: "AlmaLinux", Version: version}
	case "arch":
		return OSInfo{Name: "Arch Linux", Version: version}
	case "manjaro":
		return OSInfo{Name: "Manjaro", Version: version}
	default:
		if pretty := f["PRETTY_NAME"]; pretty != "" {
			return OSInfo{Name: pretty, Version: version}
		}
		return OSInfo{Name: id, Version: version}
	}
}

// isOstree returns true when the system is booted via OSTree (Fedora Atomic variants).
func isOstree() bool {
	_, err := os.Stat("/run/ostree-booted")
	return err == nil
}

func collectDesktopEnvs() []DesktopInfo {
	var envs []DesktopInfo
	if de, ok := detectKDE(); ok {
		envs = append(envs, de)
	}
	if de, ok := detectGNOME(); ok {
		envs = append(envs, de)
	}
	return envs
}

func detectKDE() (DesktopInfo, bool) {
	out, err := exec.Command("plasmashell", "--version").Output()
	if err != nil {
		return DesktopInfo{}, false
	}
	// "plasmashell 6.1.4"
	line := strings.TrimSpace(string(out))
	ver := ""
	if parts := strings.Fields(line); len(parts) >= 2 {
		ver = parts[len(parts)-1]
	}
	return DesktopInfo{Name: "KDE Plasma", Version: ver}, true
}

func detectGNOME() (DesktopInfo, bool) {
	out, err := exec.Command("gnome-shell", "--version").Output()
	if err != nil {
		return DesktopInfo{}, false
	}
	// "GNOME Shell 46.1"
	line := strings.TrimSpace(string(out))
	ver := ""
	if parts := strings.Fields(line); len(parts) >= 3 {
		ver = parts[len(parts)-1]
	}
	return DesktopInfo{Name: "GNOME", Version: ver}, true
}
