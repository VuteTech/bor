// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	maxSystemdUnits           = 200
	maxSystemdTasks           = 50
	maxSystemdCalendarExprs   = 16
	maxSystemdEnvironment     = 64
	maxSystemdValueLength     = 1024
	maxSystemdDescriptionLen  = 512
	maxSystemdRawDirectives   = 32
	maxSystemdDirectiveValues = 16
	maxSystemdExecArgs        = 64
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
// and types, the protected list, the consistency of the requested states,
// every drop-in value, raw directives and Bor-deployed tasks.
func ValidateSystemdContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("systemd policy content is empty")
	}
	var pol pb.SystemdPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid systemd policy: %w", err)
	}
	units := pol.GetUnits()
	tasks := pol.GetTasks()
	if len(units) == 0 && len(tasks) == 0 {
		return fmt.Errorf("systemd policy needs at least one unit or task")
	}
	if len(units) > maxSystemdUnits {
		return fmt.Errorf("systemd policy has %d units; the limit is %d", len(units), maxSystemdUnits)
	}
	if len(tasks) > maxSystemdTasks {
		return fmt.Errorf("systemd policy has %d tasks; the limit is %d", len(tasks), maxSystemdTasks)
	}

	seen := make(map[string]struct{}, len(units))
	hasEnablement := false
	for i, u := range units {
		if err := validateSystemdUnit(u); err != nil {
			if name := u.GetName(); name != "" {
				return fmt.Errorf("unit %q: %w", name, err)
			}
			return fmt.Errorf("unit %d: %w", i+1, err)
		}
		// The same unit may appear once per scope (the system manager and
		// the user managers load different files of the same name).
		key := SystemdScopeKey(u.GetScope(), u.GetName())
		if _, dup := seen[key]; dup {
			return fmt.Errorf("unit %q is listed twice", u.GetName())
		}
		seen[key] = struct{}{}
		if u.GetEnablement() != pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
			hasEnablement = true
		}
	}
	seenTasks := make(map[string]struct{}, len(tasks))
	for i, t := range tasks {
		if err := validateSystemdTask(t); err != nil {
			if name := t.GetName(); name != "" {
				return fmt.Errorf("task %q: %w", name, err)
			}
			return fmt.Errorf("task %d: %w", i+1, err)
		}
		if _, dup := seenTasks[t.GetName()]; dup {
			return fmt.Errorf("task %q is listed twice", t.GetName())
		}
		seenTasks[t.GetName()] = struct{}{}
	}
	if pol.GetWritePresets() && !hasEnablement {
		return fmt.Errorf("the preset file needs at least one unit with an enablement")
	}
	return nil
}

// SystemdContentRunsCommands reports whether a Systemd policy deploys
// tasks or sets raw Exec* directives, which run administrator-supplied
// commands on nodes and therefore need the systemd:exec permission.
// Content that does not parse runs nothing (validation rejects it later).
func SystemdContentRunsCommands(content string) bool {
	var pol pb.SystemdPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return false
	}
	if len(pol.GetTasks()) > 0 {
		return true
	}
	for _, u := range pol.GetUnits() {
		for _, d := range u.GetDropIn().GetRaw() {
			if systemdunits.IsExecDirective(d.GetKey()) {
				return true
			}
		}
	}
	return false
}

// SystemdCommandSummary describes the command-running parts of a Systemd
// policy for the audit log: every task with the SHA-256 of its script (or
// of its argv) and every raw Exec* directive. The result is a JSON object.
func SystemdCommandSummary(content string) string {
	var pol pb.SystemdPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return "{}"
	}
	type task struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	}
	summary := struct {
		Tasks          []task   `json:"tasks"`
		ExecDirectives []string `json:"exec_directives"`
	}{Tasks: []task{}, ExecDirectives: []string{}}
	for _, t := range pol.GetTasks() {
		payload := t.GetScript()
		if payload == "" {
			payload = strings.Join(t.GetExecStart(), "\x00")
		}
		sum := sha256.Sum256([]byte(payload))
		summary.Tasks = append(summary.Tasks, task{Name: t.GetName(), SHA256: hex.EncodeToString(sum[:])})
	}
	for _, u := range pol.GetUnits() {
		for _, d := range u.GetDropIn().GetRaw() {
			if systemdunits.IsExecDirective(d.GetKey()) {
				summary.ExecDirectives = append(summary.ExecDirectives, u.GetName()+": "+d.GetKey())
			}
		}
	}
	out, err := json.Marshal(summary)
	if err != nil {
		return "{}"
	}
	return string(out)
}

// SystemdScopeIsGlobal reports whether a scope value selects the user
// managers rather than the system manager.
func SystemdScopeIsGlobal(scope pb.SystemdScope) bool {
	return scope == pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL
}

// SystemdScopeKey identifies a unit within a policy: the bare name in the
// system scope, "global/<name>" in the user-manager scope.
func SystemdScopeKey(scope pb.SystemdScope, name string) string {
	if SystemdScopeIsGlobal(scope) {
		return "global/" + name
	}
	return name
}

func validateSystemdScope(scope pb.SystemdScope) error {
	if _, ok := pb.SystemdScope_name[int32(scope)]; !ok {
		return fmt.Errorf("unknown scope value %d", scope)
	}
	return nil
}

func validateSystemdUnit(u *pb.SystemdUnit) error {
	name := u.GetName()
	if err := systemdunits.ValidateName(name); err != nil {
		return err
	}
	if err := validateSystemdScope(u.GetScope()); err != nil {
		return err
	}
	if systemdunits.IsProtected(name) {
		return fmt.Errorf("this unit is protected and cannot be managed by a policy")
	}
	if SystemdScopeIsGlobal(u.GetScope()) && systemdunits.IsProtectedGlobal(name) {
		return fmt.Errorf("this unit is protected in the user scope and cannot be managed by a policy")
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
		if raw := dropIn.GetRaw(); len(raw) > 0 {
			if len(raw) > maxSystemdRawDirectives {
				return fmt.Errorf("%d raw directives; the limit is %d", len(raw), maxSystemdRawDirectives)
			}
			for _, d := range raw {
				if err := validateSystemdDirective(d, typ); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateSystemdDirective checks one raw directive against the unit type
// it is written for.
func validateSystemdDirective(d *pb.SystemdDirective, unitType string) error {
	section := d.GetSection()
	if err := systemdunits.ValidateDirectiveSection(section); err != nil {
		return err
	}
	switch section {
	case "Service", "Timer", "Socket", "Path":
		if !strings.EqualFold(section, unitType) {
			return fmt.Errorf("a [%s] directive applies to .%s units only", section, strings.ToLower(section))
		}
	}
	key := d.GetKey()
	if err := systemdunits.ValidateDirectiveKey(key); err != nil {
		return err
	}
	values := d.GetValues()
	if len(values) > maxSystemdDirectiveValues {
		return fmt.Errorf("directive %s has %d values; the limit is %d", key, len(values), maxSystemdDirectiveValues)
	}
	for _, v := range values {
		if len(v) > maxSystemdValueLength {
			return fmt.Errorf("directive %s: a value is longer than %d characters", key, maxSystemdValueLength)
		}
		if err := systemdunits.ValidateDirectiveValue(v); err != nil {
			return fmt.Errorf("directive %s: %w", key, err)
		}
	}
	return nil
}

// systemdTimerHasTrigger reports whether the settings include at least one
// trigger directive (Persistent= alone is not a schedule).
func systemdTimerHasTrigger(t *pb.SystemdTimerSettings) bool {
	return len(t.GetOnCalendar()) > 0 || t.GetOnBootSec() != "" || t.GetOnStartupSec() != "" ||
		t.GetOnActiveSec() != "" || t.GetOnUnitActiveSec() != "" || t.GetOnUnitInactiveSec() != ""
}

// validateSystemdTask checks one Bor-deployed task.
func validateSystemdTask(t *pb.SystemdTask) error {
	if err := systemdunits.ValidateTaskName(t.GetName()); err != nil {
		return err
	}
	if err := validateSystemdScope(t.GetScope()); err != nil {
		return err
	}
	if SystemdScopeIsGlobal(t.GetScope()) && (t.GetUser() != "" || t.GetGroup() != "") {
		return fmt.Errorf("a task for user managers runs as the logged-in user; leave user and group empty")
	}
	if len(t.GetDescription()) > maxSystemdDescriptionLen {
		return fmt.Errorf("description is longer than %d characters", maxSystemdDescriptionLen)
	}
	switch t.GetTrigger() {
	case pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER:
		sched := t.GetSchedule()
		if !systemdTimerHasTrigger(sched) {
			return fmt.Errorf("a timer task needs a schedule: a calendar expression or a monotonic trigger such as OnBootSec")
		}
		if err := validateSystemdTimer(sched); err != nil {
			return err
		}
	case pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_BOOT:
		if !systemdTimerIsEmpty(t.GetSchedule()) {
			return fmt.Errorf("a schedule applies to the timer trigger only")
		}
	default:
		return fmt.Errorf("set a trigger: timer or boot")
	}
	script := t.GetScript()
	argv := t.GetExecStart()
	switch {
	case script == "" && len(argv) == 0:
		return fmt.Errorf("set a script or a program to run")
	case script != "" && len(argv) > 0:
		return fmt.Errorf("set either a script or a program to run, not both")
	}
	if script != "" {
		if len(script) > systemdunits.MaxTaskScriptBytes {
			return fmt.Errorf("script is larger than %d KiB", systemdunits.MaxTaskScriptBytes/1024)
		}
		if !strings.HasPrefix(script, "#!") {
			return fmt.Errorf("script must start with an interpreter line such as #!/bin/sh")
		}
		if strings.ContainsRune(script, 0) {
			return fmt.Errorf("script contains a NUL byte")
		}
	}
	if len(argv) > 0 {
		if len(argv) > maxSystemdExecArgs {
			return fmt.Errorf("%d program arguments; the limit is %d", len(argv), maxSystemdExecArgs)
		}
		if !strings.HasPrefix(argv[0], "/") {
			return fmt.Errorf("the program must be an absolute path, got %q", argv[0])
		}
		for _, a := range argv {
			if len(a) > maxSystemdValueLength {
				return fmt.Errorf("a program argument is longer than %d characters", maxSystemdValueLength)
			}
			if err := systemdunits.ValidateDirectiveValue(a); err != nil {
				return fmt.Errorf("program argument %q: %w", a, err)
			}
		}
	}
	if u := t.GetUser(); u != "" {
		if err := systemdunits.ValidateUserName(u); err != nil {
			return err
		}
	}
	if g := t.GetGroup(); g != "" {
		if err := systemdunits.ValidateUserName(g); err != nil {
			return err
		}
	}
	if wd := t.GetWorkingDirectory(); wd != "" {
		if !strings.HasPrefix(wd, "/") || len(wd) > maxSystemdValueLength {
			return fmt.Errorf("working directory must be an absolute path")
		}
		if err := systemdunits.ValidateDirectiveValue(wd); err != nil {
			return fmt.Errorf("working directory: %w", err)
		}
	}
	if err := validateSystemdEnvironment(t.GetEnvironment()); err != nil {
		return err
	}
	if v := t.GetTimeoutStartSec(); v != "" {
		if err := systemdunits.ValidateTimeSpan(v); err != nil {
			return fmt.Errorf("TimeoutStartSec: %w", err)
		}
	}
	if p := t.GetApparmorProfile(); p != "" {
		if err := systemdunits.ValidateAppArmorProfile(p); err != nil {
			return fmt.Errorf("AppArmorProfile: %w", err)
		}
	}
	if c := t.GetSelinuxContext(); c != "" {
		if err := systemdunits.ValidateSELinuxContext(c); err != nil {
			return fmt.Errorf("SELinuxContext: %w", err)
		}
	}
	return nil
}

// validateSystemdEnvironment checks KEY=value entries.
func validateSystemdEnvironment(env []string) error {
	if n := len(env); n > maxSystemdEnvironment {
		return fmt.Errorf("%d environment entries; the limit is %d", n, maxSystemdEnvironment)
	}
	for _, e := range env {
		if len(e) > maxSystemdValueLength {
			return fmt.Errorf("environment entry is longer than %d characters", maxSystemdValueLength)
		}
		if !systemdEnvRE.MatchString(e) || strings.ContainsAny(e, "\n\r\x00") {
			return fmt.Errorf("environment entry %q must be KEY=value on one line", e)
		}
	}
	return nil
}

// systemdDropInIsEmpty reports whether the drop-in sets nothing.
func systemdDropInIsEmpty(d *pb.SystemdDropIn) bool {
	return d == nil || (systemdTimerIsEmpty(d.GetTimer()) && systemdServiceIsEmpty(d.GetService()) && len(d.GetRaw()) == 0)
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
	if err := validateSystemdEnvironment(s.GetEnvironment()); err != nil {
		return err
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
	// systemdAnalyzePreview runs "systemd-analyze calendar --iterations=N
	// -- <expr>" and returns its output; nil when the binary is unavailable.
	systemdAnalyzePreview func(expr string, iterations int) ([]byte, error)
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
	systemdAnalyzePreview = func(expr string, iterations int) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, bin, "calendar", fmt.Sprintf("--iterations=%d", iterations), "--", expr).CombinedOutput() //nolint:gosec // G204: constant binary and subcommand, a formatted integer flag; the expression is a single argument after "--" and was validated against a strict charset
	}
}

// SystemdCalendarPreview is the result of SystemdCalendarPreview.
type SystemdCalendarPreview struct {
	// Available is false when the server has no systemd-analyze.
	Available bool `json:"available"`
	// Normalized is the expression in systemd's canonical form.
	Normalized string `json:"normalized,omitempty"`
	// Next lists the next elapse times as systemd prints them, in the
	// server's time zone.
	Next []string `json:"next"`
}

// maxSystemdPreviewIterations caps how many elapse times a preview lists.
const maxSystemdPreviewIterations = 10

// PreviewSystemdCalendar evaluates a calendar expression with the server's
// systemd-analyze. A malformed expression is an error; a server without
// the binary answers Available=false.
func PreviewSystemdCalendar(expr string, iterations int) (*SystemdCalendarPreview, error) {
	if err := systemdunits.ValidateCalendar(expr); err != nil {
		return nil, err
	}
	if iterations < 1 {
		iterations = 1
	}
	if iterations > maxSystemdPreviewIterations {
		iterations = maxSystemdPreviewIterations
	}
	systemdAnalyzeOnce.Do(lookupSystemdAnalyze)
	if systemdAnalyzePreview == nil {
		return &SystemdCalendarPreview{Available: false, Next: []string{}}, nil
	}
	out, err := systemdAnalyzePreview(expr, iterations)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("invalid calendar expression %q: %s", expr, msg)
	}
	return parseSystemdCalendarOutput(out), nil
}

// parseSystemdCalendarOutput reads the "Normalized form:", "Next elapse:"
// and "Iteration #N:" lines of systemd-analyze calendar.
func parseSystemdCalendarOutput(out []byte) *SystemdCalendarPreview {
	res := &SystemdCalendarPreview{Available: true, Next: []string{}}
	for _, line := range strings.Split(string(out), "\n") {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		label = strings.TrimSpace(label)
		value = strings.TrimSpace(value)
		switch {
		case label == "Normalized form":
			res.Normalized = value
		case label == "Next elapse", strings.HasPrefix(label, "Iteration #"):
			if value != "" && value != "never" {
				res.Next = append(res.Next, value)
			}
		}
	}
	return res
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
