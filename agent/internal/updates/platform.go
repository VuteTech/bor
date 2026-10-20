// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package updates drives the distribution's unattended update engine for
// the Software updates policy type: dnf-automatic (DNF 4 and 5),
// unattended-upgrades, os-update and rpm-ostree's automatic update
// policy. It renders their configuration, reads what they would install,
// and tells whether the node needs a reboot. Every engine's own reboot
// stays off; the agent decides reboots.
package updates

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Engine identifies the update mechanism of a node.
type Engine string

// The engines Bor knows.
const (
	EngineNone          Engine = "none"
	EngineDnf           Engine = "dnf-automatic"
	EngineApt           Engine = "unattended-upgrades"
	EngineOsUpdate      Engine = "os-update"
	EngineTransactional Engine = "transactional-update"
	EngineRpmOstree     Engine = "rpm-ostree"
)

// KnownEngines lists the engines an agent configuration may force.
var KnownEngines = []Engine{EngineDnf, EngineApt, EngineOsUpdate, EngineTransactional, EngineRpmOstree}

// Mode is the effective update mode of a policy or a node.
type Mode string

// The modes, in the words the inventory and the items use.
const (
	ModeKeep     Mode = ""
	ModeOff      Mode = "off"
	ModeCheck    Mode = "check"
	ModeSecurity Mode = "security"
	ModeAll      Mode = "all"
)

// Runner executes a command and returns its combined output. The agent
// injects a fake in tests.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultRunner runs the command with a C.UTF-8 locale (dnf5 prints Qt
// locale warnings otherwise) and returns stdout; stderr is appended on
// failure so the error carries the tool's message.
func DefaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: fixed argv built by this package
	cmd.Env = append(os.Environ(), "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DEBIAN_FRONTEND=noninteractive")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return stdout.Bytes(), &RunError{Err: err, Stderr: msg}
		}
	}
	return stdout.Bytes(), err
}

// RunError is a failed command with its last stderr lines.
type RunError struct {
	Err    error
	Stderr string
}

func (e *RunError) Error() string {
	last := e.Stderr
	if i := strings.LastIndex(strings.TrimRight(last, "\n"), "\n"); i >= 0 {
		last = last[i+1:]
	}
	return e.Err.Error() + ": " + last
}

func (e *RunError) Unwrap() error { return e.Err }

// ExitCode returns the exit status of a command error, or -1.
func ExitCode(err error) int {
	var re *RunError
	if errors.As(err, &re) {
		err = re.Err
	}
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// Platform is what the node runs: the engine Bor manages there, where its
// files live and which units drive it.
type Platform struct {
	Engine Engine
	// OS id, variant and codename from os-release ("ubuntu", "resolute";
	// VARIANT_ID "microos").
	OSID     string
	Variant  string
	Codename string
	// DNF5 says the dnf-automatic engine is the dnf5 plugin.
	DNF5 bool
	// Installed reports whether the engine's package is present.
	Installed bool
	// Package is the engine's package name, installed when a policy asks.
	Package string
	// ConfigPath is the engine's configuration file Bor writes.
	ConfigPath string
	// TimerUnit and ServiceUnit drive the engine; InstallTimers lists every
	// timer the mode switches (apt has two).
	TimerUnit   string
	ServiceUnit string
	// Binaries the drivers call.
	DnfBin         string
	AptGetBin      string
	ZypperBin      string
	RpmOstreeBin   string
	RpmBin         string
	DpkgQueryBin   string
	NeedrestartBin string
	SystemctlBin   string
	// FwupdmgrBin is fwupd's client, for the firmware inventory.
	FwupdmgrBin string
	// EngineRunBin runs the engine once with the node's configuration
	// (dnf5 automatic, dnf-automatic, unattended-upgrade, os-update,
	// rpm-ostree); empty when the engine is not installed.
	EngineRunBin string
	// Root is prepended to every absolute path (tests).
	Root string
}

// Path joins Root and an absolute path.
func (p *Platform) Path(abs string) string {
	if p.Root == "" {
		return abs
	}
	return filepath.Join(p.Root, abs)
}

// Detect reads os-release and the installed tools to pick the engine.
func Detect() *Platform { return DetectEngine("") }

// DetectEngine picks the engine like Detect, or the named one when force
// is a known engine (the agent's updates.engine setting), for systems the
// detection gets wrong.
func DetectEngine(force string) *Platform {
	p := &Platform{Engine: EngineNone}
	osr := readOSRelease("/etc/os-release")
	p.OSID = strings.ToLower(osr["ID"])
	p.Variant = strings.ToLower(osr["VARIANT_ID"])
	p.Codename = osr["VERSION_CODENAME"]
	if p.Codename == "" {
		p.Codename = osr["UBUNTU_CODENAME"]
	}
	look := func(names ...string) string {
		for _, n := range names {
			if path, err := exec.LookPath(n); err == nil {
				return path
			}
			for _, dir := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
				if st, err := os.Stat(filepath.Join(dir, n)); err == nil && !st.IsDir() {
					return filepath.Join(dir, n)
				}
			}
		}
		return ""
	}
	p.SystemctlBin = look("systemctl")
	p.NeedrestartBin = look("needrestart")
	p.RpmBin = look("rpm")
	p.DpkgQueryBin = look("dpkg-query")
	p.FwupdmgrBin = look("fwupdmgr")
	// SUSE ships dnf as an alternative client of the same repositories;
	// its update mechanism is zypper's.
	suse := strings.Contains(p.OSID, "suse") || p.OSID == "sles" || strings.Contains(strings.ToLower(osr["ID_LIKE"]), "suse")
	forced := Engine(force)
	switch {
	case forced == EngineRpmOstree || forced == "" && fileExists("/run/ostree-booted") && look("rpm-ostree") != "":
		p.Engine = EngineRpmOstree
		p.RpmOstreeBin = look("rpm-ostree")
		p.Installed = true
		p.ConfigPath = "/etc/rpm-ostreed.conf"
		p.TimerUnit = "rpm-ostreed-automatic.timer"
		p.ServiceUnit = "rpm-ostreed-automatic.service"
		p.EngineRunBin = p.RpmOstreeBin
	case forced == EngineTransactional || forced == "" && look("transactional-update") != "" && rootReadOnly("/proc/self/mountinfo"):
		// MicroOS, Aeon, Kalpa, SLE Micro: a read-only root updated through
		// snapshots.
		p.Engine = EngineTransactional
		p.ZypperBin = look("zypper")
		p.Package = "transactional-update"
		p.ConfigPath = "/etc/transactional-update.conf"
		p.TimerUnit = "transactional-update.timer"
		p.ServiceUnit = "transactional-update.service"
		p.EngineRunBin = look("transactional-update")
		p.Installed = p.EngineRunBin != ""
	case forced == EngineOsUpdate || forced == "" && suse && look("zypper") != "":
		p.Engine = EngineOsUpdate
		p.ZypperBin = look("zypper")
		p.Package = "os-update"
		p.ConfigPath = "/etc/os-update.conf"
		p.TimerUnit = "os-update.timer"
		p.ServiceUnit = "os-update.service"
		p.Installed = fileExists("/usr/libexec/os-update") || fileExists("/usr/lib/systemd/system/os-update.timer")
		if fileExists("/usr/libexec/os-update") {
			p.EngineRunBin = "/usr/libexec/os-update"
		}
	case forced == EngineDnf && look("dnf5") != "" || forced == "" && look("dnf5") != "":
		p.Engine = EngineDnf
		p.DNF5 = true
		p.DnfBin = look("dnf5")
		p.Package = "dnf5-plugin-automatic"
		p.ConfigPath = "/etc/dnf/automatic.conf"
		p.TimerUnit = "dnf5-automatic.timer"
		p.ServiceUnit = "dnf5-automatic.service"
		p.Installed = fileExists("/usr/lib64/dnf5/plugins/automatic_cmd_plugin.so") || fileExists("/usr/lib/dnf5/plugins/automatic_cmd_plugin.so") || fileExists("/usr/lib/systemd/system/dnf5-automatic.timer")
		if p.Installed {
			p.EngineRunBin = p.DnfBin
		}
	case forced == EngineDnf || forced == "" && look("dnf") != "":
		p.Engine = EngineDnf
		p.DnfBin = look("dnf")
		p.Package = "dnf-automatic"
		p.ConfigPath = "/etc/dnf/automatic.conf"
		p.TimerUnit = "dnf-automatic.timer"
		p.ServiceUnit = "dnf-automatic.service"
		p.Installed = look("dnf-automatic") != "" || fileExists("/usr/lib/systemd/system/dnf-automatic.timer")
		p.EngineRunBin = look("dnf-automatic")
	case forced == EngineApt || forced == "" && look("apt-get") != "":
		p.Engine = EngineApt
		p.AptGetBin = look("apt-get")
		p.Package = "unattended-upgrades"
		p.ConfigPath = "/etc/apt/apt.conf.d/90bor-updates"
		p.TimerUnit = "apt-daily-upgrade.timer"
		p.ServiceUnit = "apt-daily-upgrade.service"
		p.Installed = look("unattended-upgrade") != ""
		p.EngineRunBin = look("unattended-upgrade")
	case forced == "" && look("zypper") != "":
		p.Engine = EngineOsUpdate
		p.ZypperBin = look("zypper")
		p.Package = "os-update"
		p.ConfigPath = "/etc/os-update.conf"
		p.TimerUnit = "os-update.timer"
		p.ServiceUnit = "os-update.service"
		p.Installed = fileExists("/usr/libexec/os-update") || fileExists("/usr/lib/systemd/system/os-update.timer")
		if fileExists("/usr/libexec/os-update") {
			p.EngineRunBin = "/usr/libexec/os-update"
		}
	}
	return p
}

// readOSRelease parses KEY=value lines; a missing file yields no keys.
func readOSRelease(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed path
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		out[k] = strings.Trim(v, `"'`)
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Header is the first line of every file Bor writes for this type.
const Header = "Managed by Bor - changes are overwritten"

// DropInName is the drop-in file of the engine timer. It differs from the
// Services & timers type's 90-bor.conf so that type does not treat it as
// one of its own stale drop-ins; it sorts after it, so the schedule wins
// when both manage the same timer.
const DropInName = "90-bor-updates.conf"

// legacyDropInName is the name earlier agents used; it is removed when
// found next to the current one.
const legacyDropInName = "90-bor.conf"

// LegacyTimerDropInPath is the drop-in path earlier agents wrote.
func LegacyTimerDropInPath(systemDir, timer string) string {
	return filepath.Join(systemDir, timer+".d", legacyDropInName)
}

// RenderTimerDropIn renders the [Timer] drop-in of the engine's timer: the
// calendar expression replaces the vendor triggers, so monotonic triggers
// are reset too.
func RenderTimerDropIn(onCalendar, randomizedDelay string, persistent *bool) []byte {
	var b strings.Builder
	b.WriteString("# " + Header + "\n[Timer]\n")
	if onCalendar != "" {
		for _, k := range []string{"OnCalendar", "OnBootSec", "OnStartupSec", "OnActiveSec", "OnUnitActiveSec", "OnUnitInactiveSec"} {
			b.WriteString(k + "=\n")
		}
		b.WriteString("OnCalendar=" + onCalendar + "\n")
	}
	if randomizedDelay != "" {
		b.WriteString("RandomizedDelaySec=" + randomizedDelay + "\n")
	}
	if persistent != nil {
		if *persistent {
			b.WriteString("Persistent=true\n")
		} else {
			b.WriteString("Persistent=false\n")
		}
	}
	return []byte(b.String())
}

// TimerDropInPath is the drop-in path of a timer unit under systemDir.
func TimerDropInPath(systemDir, timer string) string {
	return filepath.Join(systemDir, timer+".d", DropInName)
}

// HasHeader reports whether a file carries Bor's header in its first line.
func HasHeader(data []byte) bool {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	return bytes.Contains(first, []byte(Header))
}

// rootReadOnly reports whether the root file system is mounted read-only
// (a transactional system), from a mountinfo file.
func rootReadOnly(mountinfo string) bool {
	data, err := os.ReadFile(mountinfo) //nolint:gosec // G304: fixed proc path
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		// mount id, parent id, major:minor, root, mount point, options, ...
		if len(f) < 6 || f[4] != "/" {
			continue
		}
		for _, opt := range strings.Split(f[5], ",") {
			if opt == "ro" {
				return true
			}
		}
		return false
	}
	return false
}
