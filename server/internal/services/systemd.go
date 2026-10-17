// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
	"google.golang.org/protobuf/encoding/protojson"
)

// Limits for a Systemd policy. Every value reaches the agent, where it
// becomes a path component, a D-Bus argument or a line in a drop-in.
const (
	maxSystemdUnits          = 200
	maxSystemdCalendarExprs  = 16
	maxSystemdEnvironment    = 64
	maxSystemdValueLength    = 1024
	maxSystemdDescriptionLen = 512
)

var (
	systemdRestartValues = map[string]struct{}{
		"no": {}, "on-success": {}, "on-failure": {}, "on-abnormal": {},
		"on-abort": {}, "on-watchdog": {}, "always": {},
	}
	systemdProtectSystemValues = map[string]struct{}{"yes": {}, "no": {}, "full": {}, "strict": {}}
	systemdProtectHomeValues   = map[string]struct{}{"yes": {}, "no": {}, "read-only": {}, "tmpfs": {}}

	// systemdSizeRE matches resource-control sizes: bytes with an optional
	// binary suffix, a percentage of physical memory, or infinity.
	systemdSizeRE = regexp.MustCompile(`^(\d+(\.\d+)?[KMGTPE]?|\d+(\.\d+)?%|infinity)$`)
	// systemdPercentRE matches a CPU quota such as "50%" or "150%".
	systemdPercentRE = regexp.MustCompile(`^\d+(\.\d+)?%$`)
	// systemdEnvRE matches one KEY=value environment assignment.
	systemdEnvRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// ValidateSystemdContent validates Systemd policy JSON content: unit names
// and types, the protected list, the consistency of the requested states
// and every drop-in value.
func ValidateSystemdContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("systemd policy content is empty")
	}
	var pol pb.SystemdPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid systemd policy: %w", err)
	}
	units := pol.GetUnits()
	if len(units) == 0 {
		return fmt.Errorf("systemd policy needs at least one unit")
	}
	if len(units) > maxSystemdUnits {
		return fmt.Errorf("systemd policy has %d units; the limit is %d", len(units), maxSystemdUnits)
	}

	seen := make(map[string]struct{}, len(units))
	for i, u := range units {
		if err := validateSystemdUnit(u); err != nil {
			if name := u.GetName(); name != "" {
				return fmt.Errorf("unit %q: %w", name, err)
			}
			return fmt.Errorf("unit %d: %w", i+1, err)
		}
		if _, dup := seen[u.GetName()]; dup {
			return fmt.Errorf("unit %q is listed twice", u.GetName())
		}
		seen[u.GetName()] = struct{}{}
	}
	return nil
}

func validateSystemdUnit(u *pb.SystemdUnit) error {
	name := u.GetName()
	if err := systemdunits.ValidateName(name); err != nil {
		return err
	}
	if systemdunits.IsProtected(name) {
		return fmt.Errorf("this unit is protected and cannot be managed by a policy")
	}
	if systemdunits.IsTemplate(name) {
		return fmt.Errorf("template units cannot be managed; name an instance such as %s", strings.Replace(name, "@.", "@instance.", 1))
	}
	typ := systemdunits.Type(name)

	enablement := u.GetEnablement()
	if _, ok := pb.SystemdEnablement_name[int32(enablement)]; !ok {
		return fmt.Errorf("unknown enablement value %d", enablement)
	}
	activity := u.GetActivity()
	if _, ok := pb.SystemdActivity_name[int32(activity)]; !ok {
		return fmt.Errorf("unknown activity value %d", activity)
	}
	dropIn := u.GetDropIn()
	hasDropIn := !systemdDropInIsEmpty(dropIn)

	if enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED &&
		activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED &&
		!hasDropIn && !u.GetRequireLastRunSuccess() {
		return fmt.Errorf("set an enablement, an activity, an override or a last-run check")
	}
	if enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED &&
		activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING {
		return fmt.Errorf("a masked unit cannot be running")
	}
	if typ == "target" && activity != pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED {
		return fmt.Errorf("targets can only be enabled, disabled or masked")
	}
	if u.GetRequireLastRunSuccess() && typ != "timer" && typ != "service" {
		return fmt.Errorf("the last-run check applies to timers and services only")
	}
	if u.GetRestartOnChange() && typ != "service" {
		return fmt.Errorf("restart on change applies to services only")
	}
	if len(u.GetDescription()) > maxSystemdDescriptionLen {
		return fmt.Errorf("description is longer than %d characters", maxSystemdDescriptionLen)
	}

	if hasDropIn {
		if t := dropIn.GetTimer(); !systemdTimerIsEmpty(t) {
			if typ != "timer" {
				return fmt.Errorf("timer settings apply to .timer units only")
			}
			if err := validateSystemdTimer(t); err != nil {
				return err
			}
		}
		if s := dropIn.GetService(); !systemdServiceIsEmpty(s) {
			if typ != "service" {
				return fmt.Errorf("service settings apply to .service units only")
			}
			if err := validateSystemdService(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// systemdDropInIsEmpty reports whether the drop-in sets nothing.
func systemdDropInIsEmpty(d *pb.SystemdDropIn) bool {
	return d == nil || (systemdTimerIsEmpty(d.GetTimer()) && systemdServiceIsEmpty(d.GetService()))
}

func systemdTimerIsEmpty(t *pb.SystemdTimerSettings) bool {
	if t == nil {
		return true
	}
	return len(t.GetOnCalendar()) == 0 && t.GetOnBootSec() == "" && t.GetOnStartupSec() == "" &&
		t.GetOnActiveSec() == "" && t.GetOnUnitActiveSec() == "" && t.GetOnUnitInactiveSec() == "" &&
		t.Persistent == nil && t.GetRandomizedDelaySec() == "" && t.GetAccuracySec() == "" &&
		t.WakeSystem == nil && t.FixedRandomDelay == nil && t.OnClockChange == nil && t.OnTimezoneChange == nil
}

func systemdServiceIsEmpty(s *pb.SystemdServiceSettings) bool {
	if s == nil {
		return true
	}
	return s.GetRestart() == "" && s.GetRestartSec() == "" && s.GetTimeoutStartSec() == "" &&
		s.GetTimeoutStopSec() == "" && len(s.GetEnvironment()) == 0 && s.GetMemoryMax() == "" &&
		s.GetCpuQuota() == "" && s.GetTasksMax() == "" && s.GetNice() == "" && s.NoNewPrivileges == nil &&
		s.GetProtectSystem() == "" && s.GetProtectHome() == "" && s.PrivateTmp == nil
}

func validateSystemdTimer(t *pb.SystemdTimerSettings) error {
	if n := len(t.GetOnCalendar()); n > maxSystemdCalendarExprs {
		return fmt.Errorf("%d calendar expressions; the limit is %d", n, maxSystemdCalendarExprs)
	}
	for _, expr := range t.GetOnCalendar() {
		if err := systemdunits.ValidateCalendar(expr); err != nil {
			return err
		}
		if err := checkCalendarWithSystemdAnalyze(expr); err != nil {
			return err
		}
	}
	spans := []struct{ label, value string }{
		{"OnBootSec", t.GetOnBootSec()},
		{"OnStartupSec", t.GetOnStartupSec()},
		{"OnActiveSec", t.GetOnActiveSec()},
		{"OnUnitActiveSec", t.GetOnUnitActiveSec()},
		{"OnUnitInactiveSec", t.GetOnUnitInactiveSec()},
		{"RandomizedDelaySec", t.GetRandomizedDelaySec()},
		{"AccuracySec", t.GetAccuracySec()},
	}
	for _, s := range spans {
		if s.value == "" {
			continue
		}
		if err := systemdunits.ValidateTimeSpan(s.value); err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
	}
	return nil
}

func validateSystemdService(s *pb.SystemdServiceSettings) error {
	if r := s.GetRestart(); r != "" {
		if _, ok := systemdRestartValues[r]; !ok {
			return fmt.Errorf("restart value %q must be one of no, on-success, on-failure, on-abnormal, on-abort, on-watchdog, always", r)
		}
	}
	spans := []struct{ label, value string }{
		{"RestartSec", s.GetRestartSec()},
		{"TimeoutStartSec", s.GetTimeoutStartSec()},
		{"TimeoutStopSec", s.GetTimeoutStopSec()},
	}
	for _, sp := range spans {
		if sp.value == "" {
			continue
		}
		if err := systemdunits.ValidateTimeSpan(sp.value); err != nil {
			return fmt.Errorf("%s: %w", sp.label, err)
		}
	}
	if n := len(s.GetEnvironment()); n > maxSystemdEnvironment {
		return fmt.Errorf("%d environment entries; the limit is %d", n, maxSystemdEnvironment)
	}
	for _, e := range s.GetEnvironment() {
		if len(e) > maxSystemdValueLength {
			return fmt.Errorf("environment entry is longer than %d characters", maxSystemdValueLength)
		}
		if !systemdEnvRE.MatchString(e) || strings.ContainsAny(e, "\n\r\x00") {
			return fmt.Errorf("environment entry %q must be KEY=value on one line", e)
		}
	}
	if v := s.GetMemoryMax(); v != "" && !systemdSizeRE.MatchString(v) {
		return fmt.Errorf("MemoryMax %q must be a size such as 512M, a percentage such as 20%%, or infinity", v)
	}
	if v := s.GetCpuQuota(); v != "" && !systemdPercentRE.MatchString(v) {
		return fmt.Errorf("CPUQuota %q must be a percentage such as 50%%", v)
	}
	if v := s.GetTasksMax(); v != "" && !systemdSizeRE.MatchString(v) {
		return fmt.Errorf("TasksMax %q must be a number, a percentage or infinity", v)
	}
	if v := s.GetNice(); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < -20 || n > 19 {
			return fmt.Errorf("nice value %q must be an integer between -20 and 19", v)
		}
	}
	if v := s.GetProtectSystem(); v != "" {
		if _, ok := systemdProtectSystemValues[v]; !ok {
			return fmt.Errorf("ProtectSystem %q must be one of yes, no, full, strict", v)
		}
	}
	if v := s.GetProtectHome(); v != "" {
		if _, ok := systemdProtectHomeValues[v]; !ok {
			return fmt.Errorf("ProtectHome %q must be one of yes, no, read-only, tmpfs", v)
		}
	}
	return nil
}

// ── optional authoritative calendar check ──────────────────────────────────

// systemdAnalyzeRunner runs systemd-analyze with a constant argv and the
// expression as a single argument after "--". Package var so tests can
// stub it; nil means the binary is unavailable.
var (
	systemdAnalyzeOnce   sync.Once
	systemdAnalyzeRunner func(expr string) error
)

func lookupSystemdAnalyze() {
	bin := os.Getenv("BOR_SYSTEMD_ANALYZE")
	if bin == "" {
		p, err := exec.LookPath("systemd-analyze")
		if err != nil {
			return
		}
		bin = p
	}
	systemdAnalyzeRunner = func(expr string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "calendar", "--", expr).CombinedOutput() //nolint:gosec // G204: constant binary and subcommand; the expression is a single argument after "--" and was validated against a strict charset
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("%s", msg)
		}
		return nil
	}
}

// checkCalendarWithSystemdAnalyze validates a calendar expression with
// systemd-analyze when the server runs on a host that has it. Without the
// binary the charset check above is the only server-side check and the
// agent validates on the node.
func checkCalendarWithSystemdAnalyze(expr string) error {
	systemdAnalyzeOnce.Do(lookupSystemdAnalyze)
	if systemdAnalyzeRunner == nil {
		return nil
	}
	if err := systemdAnalyzeRunner(expr); err != nil {
		return fmt.Errorf("invalid calendar expression %q: %v", expr, err)
	}
	return nil
}
