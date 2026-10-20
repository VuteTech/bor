// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
	"google.golang.org/protobuf/proto"
)

// systemdTaskDefaultTimeout bounds a task run when the policy sets none.
const systemdTaskDefaultTimeout = "1h"

// systemdHardenedDirectives are the sandboxing defaults of a task service.
// ProtectSystem= and ProtectHome= are not among them: maintenance scripts
// commonly write under /etc, /var and /home.
var systemdHardenedDirectives = []string{
	"NoNewPrivileges=yes",
	"PrivateTmp=yes",
	"ProtectKernelTunables=yes",
	"ProtectControlGroups=yes",
	"RestrictSUIDSGID=yes",
	"ProtectClock=yes",
	"ProtectHostname=yes",
}

// systemdHardenedDirectivesGlobal are the sandboxing defaults of a task
// in a user manager: the namespace-based protections need privileges a
// user manager does not have, so only the privilege and setuid controls
// remain.
var systemdHardenedDirectivesGlobal = []string{
	"NoNewPrivileges=yes",
	"RestrictSUIDSGID=yes",
}

// systemdEnvEntryRE matches one KEY=value environment entry.
var systemdEnvEntryRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// systemdCleanArgRE matches a command-line word that needs no quoting.
var systemdCleanArgRE = regexp.MustCompile(`^[A-Za-z0-9_./:=@+,-]+$`)

// systemdTimerHasTriggerDirective reports whether the settings carry a
// trigger (Persistent= alone schedules nothing).
func systemdTimerHasTriggerDirective(t *pb.SystemdTimerSettings) bool {
	return len(t.GetOnCalendar()) > 0 || t.GetOnBootSec() != "" || t.GetOnStartupSec() != "" ||
		t.GetOnActiveSec() != "" || t.GetOnUnitActiveSec() != "" || t.GetOnUnitInactiveSec() != ""
}

// systemdTimerSettingsWellFormed re-checks the syntax of every timer value
// (the agent treats the server as untrusted for what ends up in a file).
func systemdTimerSettingsWellFormed(t *pb.SystemdTimerSettings) bool {
	for _, c := range t.GetOnCalendar() {
		if systemdunits.ValidateCalendar(c) != nil {
			return false
		}
	}
	for _, v := range []string{t.GetOnBootSec(), t.GetOnStartupSec(), t.GetOnActiveSec(), t.GetOnUnitActiveSec(), t.GetOnUnitInactiveSec(), t.GetRandomizedDelaySec(), t.GetAccuracySec()} {
		if v != "" && systemdunits.ValidateTimeSpan(v) != nil {
			return false
		}
	}
	return true
}

// systemdTaskWellFormed re-validates a task before any of it becomes a
// file name, a unit-file line or a command line.
func systemdTaskWellFormed(t *pb.SystemdTask) bool {
	if t == nil || systemdunits.ValidateTaskName(t.GetName()) != nil {
		return false
	}
	switch t.GetTrigger() {
	case pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER:
		if !systemdTimerHasTriggerDirective(t.GetSchedule()) || !systemdTimerSettingsWellFormed(t.GetSchedule()) {
			return false
		}
	case pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_BOOT:
	default:
		return false
	}
	script, argv := t.GetScript(), t.GetExecStart()
	if (script == "") == (len(argv) == 0) {
		return false
	}
	if script != "" && (!strings.HasPrefix(script, "#!") || len(script) > systemdunits.MaxTaskScriptBytes || strings.ContainsRune(script, 0)) {
		return false
	}
	if len(argv) > 0 {
		if !strings.HasPrefix(argv[0], "/") {
			return false
		}
		for _, a := range argv {
			if systemdunits.ValidateDirectiveValue(a) != nil {
				return false
			}
		}
	}
	for _, n := range []string{t.GetUser(), t.GetGroup()} {
		if n != "" && systemdunits.ValidateUserName(n) != nil {
			return false
		}
	}
	if wd := t.GetWorkingDirectory(); wd != "" && (!strings.HasPrefix(wd, "/") || systemdunits.ValidateDirectiveValue(wd) != nil) {
		return false
	}
	for _, e := range t.GetEnvironment() {
		if !systemdEnvEntryRE.MatchString(e) || systemdunits.ValidateDirectiveValue(e) != nil {
			return false
		}
	}
	if v := t.GetTimeoutStartSec(); v != "" && systemdunits.ValidateTimeSpan(v) != nil {
		return false
	}
	if p := t.GetApparmorProfile(); p != "" && systemdunits.ValidateAppArmorProfile(p) != nil {
		return false
	}
	if c := t.GetSelinuxContext(); c != "" && systemdunits.ValidateSELinuxContext(c) != nil {
		return false
	}
	return true
}

// systemdDirectiveWellFormed re-validates a raw directive.
func systemdDirectiveWellFormed(d *pb.SystemdDirective) bool {
	if d == nil || systemdunits.ValidateDirectiveSection(d.GetSection()) != nil || systemdunits.ValidateDirectiveKey(d.GetKey()) != nil {
		return false
	}
	for _, v := range d.GetValues() {
		if systemdunits.ValidateDirectiveValue(v) != nil {
			return false
		}
	}
	return true
}

// ── rendering ───────────────────────────────────────────────────────────

// systemdExecArg renders one word of a command line so systemd takes it
// literally: specifiers and variables are escaped and anything beyond a
// plain word is double-quoted.
func systemdExecArg(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")
	if systemdCleanArgRE.MatchString(s) {
		return s
	}
	return systemdQuote(s)
}

// systemdTaskDescription returns the one-line description of a task.
func systemdTaskDescription(t *pb.SystemdTask) string {
	if d := strings.Join(strings.Fields(t.GetDescription()), " "); d != "" {
		return d
	}
	return "Bor task " + t.GetName()
}

// RenderSystemdTaskService renders bor-<name>.service for a task. The
// script path is the task's script when the task carries one.
func RenderSystemdTaskService(t *pb.SystemdTask, scriptPath string) []byte {
	name := t.GetName()
	var b bytes.Buffer
	b.WriteString(systemdDropInHeader + "\n# Task: " + name + "\n")
	b.WriteString("\n[Unit]\nDescription=" + systemdTaskDescription(t) + "\n")
	b.WriteString("\n[Service]\nType=oneshot\n")
	if t.GetScript() != "" {
		b.WriteString("ExecStart=" + systemdExecArg(scriptPath) + "\n")
	} else {
		words := make([]string, 0, len(t.GetExecStart()))
		for _, a := range t.GetExecStart() {
			words = append(words, systemdExecArg(a))
		}
		b.WriteString("ExecStart=" + strings.Join(words, " ") + "\n")
	}
	timeout := t.GetTimeoutStartSec()
	if timeout == "" {
		timeout = systemdTaskDefaultTimeout
	}
	b.WriteString("TimeoutStartSec=" + timeout + "\n")
	b.WriteString("SyslogIdentifier=bor-task-" + name + "\n")
	if u := t.GetUser(); u != "" {
		b.WriteString("User=" + u + "\n")
	}
	if g := t.GetGroup(); g != "" {
		b.WriteString("Group=" + g + "\n")
	}
	if wd := t.GetWorkingDirectory(); wd != "" {
		b.WriteString("WorkingDirectory=" + wd + "\n")
	}
	if p := t.GetApparmorProfile(); p != "" {
		b.WriteString("AppArmorProfile=" + p + "\n")
	}
	if c := t.GetSelinuxContext(); c != "" {
		b.WriteString("SELinuxContext=" + c + "\n")
	}
	env := append([]string(nil), t.GetEnvironment()...)
	sort.Strings(env)
	for _, e := range env {
		b.WriteString("Environment=" + systemdQuote(e) + "\n")
	}
	if t.Hardened == nil || t.GetHardened() {
		hardened := systemdHardenedDirectives
		if SystemdScopeIsGlobal(t.GetScope()) {
			hardened = systemdHardenedDirectivesGlobal
		}
		for _, d := range hardened {
			b.WriteString(d + "\n")
		}
	}
	if t.GetTrigger() == pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_BOOT {
		// In a user manager the task runs at login instead of at boot.
		target := "multi-user.target"
		if SystemdScopeIsGlobal(t.GetScope()) {
			target = "default.target"
		}
		b.WriteString("\n[Install]\nWantedBy=" + target + "\n")
	}
	return b.Bytes()
}

// RenderSystemdTaskTimer renders bor-<name>.timer for a timer-triggered
// task, or nil for a boot-triggered one.
func RenderSystemdTaskTimer(t *pb.SystemdTask) []byte {
	if t.GetTrigger() != pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER {
		return nil
	}
	var b bytes.Buffer
	b.WriteString(systemdDropInHeader + "\n# Task: " + t.GetName() + "\n")
	b.WriteString("\n[Unit]\nDescription=Timer for " + systemdTaskDescription(t) + "\n")
	b.WriteString("\n[Timer]\n")
	renderSystemdTimerSettings(&b, t.GetSchedule(), false)
	b.WriteString("\n[Install]\nWantedBy=timers.target\n")
	return b.Bytes()
}

// RenderSystemdPreset renders 10-bor.preset for the system manager; see
// RenderSystemdPresetScope.
func RenderSystemdPreset(d *SystemdDesired) []byte {
	return RenderSystemdPresetScope(d, false)
}

// RenderSystemdPresetScope renders 10-bor.preset from the enablement
// decisions of the merged units of one scope: "enable" for enabled units,
// "disable" for disabled and masked ones (presets cannot mask). nil when
// presets are off or no unit of the scope carries an enablement.
func RenderSystemdPresetScope(d *SystemdDesired, global bool) []byte {
	if d == nil || !d.WritePresets {
		return nil
	}
	var lines []string
	for _, key := range d.Names() {
		u := d.Units[key]
		if u.Global != global {
			continue
		}
		switch u.Enablement {
		case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
			lines = append(lines, "enable "+u.Name)
		case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED, pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
			lines = append(lines, "disable "+u.Name)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(systemdDropInHeader + "\n" + strings.Join(lines, "\n") + "\n")
}

// ── sync ────────────────────────────────────────────────────────────────

// systemdTaskRun is the per-task working state of one sync.
type systemdTaskRun struct {
	// disabled is set when the node does not allow tasks.
	disabled bool
	// changed is set when a file of the task was written or removed.
	changed   bool
	verifyErr string
	errs      []string
	notes     []string
}

// writeSystemdTaskFile writes a task file atomically; scripts become
// executable.
func writeSystemdTaskFile(path string, data []byte, executable bool) error {
	if err := WriteFileAtomically(path, data); err != nil {
		return err
	}
	if executable {
		if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // G302: the task script must be executable by systemd
			return err
		}
	}
	return nil
}

// removeSystemdTaskFile deletes a task file; a missing file is fine.
func removeSystemdTaskFile(path string) bool {
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: systemd: remove %s: %v", path, err)
		}
		return false
	}
	return true
}

// retireTaskUnit stops and disables a Bor task unit before its file goes:
// in the system manager, or (global) in every logged-in user's manager
// plus the global enablement.
func (s *systemdSyncer) retireTaskUnit(global bool, unit string) {
	if !systemdunits.IsTaskUnit(unit) {
		return
	}
	if global {
		for _, t := range s.userTargets() {
			if t.mgr == nil {
				continue
			}
			if st, err := t.mgr.Status(s.ctx, unit); err == nil && st.Exists() && st.IsActive() {
				if r, err := t.mgr.Stop(s.ctx, unit); err != nil {
					log.Printf("Warning: systemd: stop %s for %s: %v", unit, t.sess, err)
				} else {
					log.Printf("systemd: stop %s for %s: %s", unit, t.sess, r)
				}
			}
		}
		if systemd.IsEnabledState(s.opts.globals().State(unit)) {
			if err := s.opts.globals().Disable(unit); err != nil {
				log.Printf("Warning: systemd: disable %s (global): %v", unit, err)
			}
		}
		s.userReload = true
		return
	}
	st, err := s.mgr.Status(s.ctx, unit)
	if err != nil || !st.Exists() {
		return
	}
	if st.IsActive() {
		if r, err := s.mgr.Stop(s.ctx, unit); err != nil {
			log.Printf("Warning: systemd: stop %s: %v", unit, err)
		} else {
			log.Printf("systemd: stop %s: %s", unit, r)
		}
	}
	if systemdEnabledState(st.UnitFileState) {
		if err := s.mgr.Disable(s.ctx, []string{unit}); err != nil {
			log.Printf("Warning: systemd: disable %s: %v", unit, err)
		}
	}
}

// retireTask removes every file of a task after stopping its units.
func (s *systemdSyncer) retireTask(global bool, name string) {
	dirs := s.opts.dirs()
	s.retireTaskUnit(global, systemdunits.TaskTimerUnit(name))
	s.retireTaskUnit(global, systemdunits.TaskServiceUnit(name))
	for _, p := range []string{dirs.TaskUnitPath(global, systemdunits.TaskTimerUnit(name)), dirs.TaskUnitPath(global, systemdunits.TaskServiceUnit(name)), dirs.TaskScriptPath(name)} {
		removeSystemdTaskFile(p)
	}
}

// syncTaskFiles writes the unit files and scripts of the desired tasks,
// verifies new unit files with systemd-analyze, and removes the files of
// tasks that are no longer desired (or of every task when the node does
// not allow them). reload is set when a system unit file changed; a
// user-manager unit file sets userReload instead.
func (s *systemdSyncer) syncTaskFiles(reload *bool) error {
	dirs := s.opts.dirs()
	var existingUnits []string
	for _, dir := range []string{dirs.systemDir(), dirs.userDir()} {
		found, err := listBorSystemdTaskUnits(dir)
		if err != nil {
			return err
		}
		existingUnits = append(existingUnits, found...)
	}
	existingScripts, err := listBorSystemdScripts(dirs.scriptsDir())
	if err != nil {
		return err
	}
	changedIn := func(global bool) {
		if global {
			s.userReload = true
		} else {
			*reload = true
		}
	}
	wanted := map[string]bool{}
	for _, name := range s.desired.TaskNames() {
		run := s.tasks[name]
		d := s.desired.Tasks[name]
		t := d.Task
		if !s.opts.AllowTasks {
			run.disabled = true
			continue
		}
		global := d.Global
		scriptPath := dirs.TaskScriptPath(name)
		servicePath := dirs.TaskUnitPath(global, systemdunits.TaskServiceUnit(name))
		timerPath := dirs.TaskUnitPath(global, systemdunits.TaskTimerUnit(name))
		type file struct {
			path string
			data []byte
			exec bool
		}
		var files []file
		if t.GetScript() != "" {
			files = append(files, file{scriptPath, []byte(t.GetScript()), true})
		}
		files = append(files, file{servicePath, RenderSystemdTaskService(t, scriptPath), false})
		if timer := RenderSystemdTaskTimer(t); timer != nil {
			files = append(files, file{timerPath, timer, false})
		}
		for _, f := range files {
			wanted[f.path] = true
		}
		// A task that switched from a script to a program, from a timer
		// to a boot trigger, or between scopes leaves a file behind.
		if !wanted[timerPath] && isBorSystemdDropIn(timerPath) {
			s.retireTaskUnit(global, systemdunits.TaskTimerUnit(name))
			if removeSystemdTaskFile(timerPath) {
				run.changed = true
				changedIn(global)
			}
		}
		if !wanted[scriptPath] && removeSystemdTaskFile(scriptPath) {
			run.changed = true
		}
		var writtenUnits []string
		for _, f := range files {
			cur, _ := os.ReadFile(f.path) //nolint:gosec // G304: path built from a validated task name under the configured directories
			if bytes.Equal(cur, f.data) {
				if f.exec {
					_ = os.Chmod(f.path, 0o755) //nolint:gosec // G302: the task script must be executable by systemd
				}
				continue
			}
			if err := writeSystemdTaskFile(f.path, f.data, f.exec); err != nil {
				run.errs = append(run.errs, "write "+filepath.Base(f.path)+": "+err.Error())
				break
			}
			run.changed = true
			if !f.exec {
				writtenUnits = append(writtenUnits, f.path)
				changedIn(global)
			}
		}
		if len(run.errs) > 0 {
			continue
		}
		if len(writtenUnits) > 0 && s.opts.Analyzer != nil && s.opts.Analyzer.Available() {
			for _, p := range writtenUnits {
				var err error
				if global {
					err = s.opts.Analyzer.VerifyUserUnit(s.ctx, p)
				} else {
					err = s.opts.Analyzer.VerifyUnit(s.ctx, p)
				}
				if err != nil {
					run.verifyErr = "systemd-analyze rejected " + filepath.Base(p) + ": " + err.Error() + "; the task files were removed"
					break
				}
			}
			if run.verifyErr != "" {
				s.retireTask(global, name)
				for _, f := range files {
					delete(wanted, f.path)
				}
				changedIn(global)
			}
		}
	}
	for _, p := range existingUnits {
		if wanted[p] {
			continue
		}
		global := strings.HasPrefix(p, dirs.userDir()+string(filepath.Separator))
		s.retireTaskUnit(global, filepath.Base(p))
		if removeSystemdTaskFile(p) {
			log.Printf("systemd: removed stale task unit %s", p)
			changedIn(global)
			s.changed = true
		}
	}
	for _, p := range existingScripts {
		if wanted[p] {
			continue
		}
		if removeSystemdTaskFile(p) {
			log.Printf("systemd: removed stale task script %s", p)
			s.changed = true
		}
	}
	return nil
}

// rollbackBadTaskUnits removes the files of a task whose unit systemd
// could not load after the reload; it reports whether another reload is
// needed.
func (s *systemdSyncer) rollbackBadTaskUnits() bool {
	rollback := false
	for _, name := range s.desired.TaskNames() {
		run := s.tasks[name]
		if s.desired.Tasks[name].Global || !run.changed || run.disabled || run.verifyErr != "" || len(run.errs) > 0 {
			continue
		}
		for _, unit := range []string{systemdunits.TaskServiceUnit(name), systemdunits.TaskTimerUnit(name)} {
			st, err := s.mgr.Status(s.ctx, unit)
			if err != nil || !st.Exists() {
				continue
			}
			if st.LoadState == "bad-setting" || st.LoadState == "error" {
				run.verifyErr = fmt.Sprintf("systemd rejected %s (load state %s); the task files were removed", unit, st.LoadState)
				s.retireTask(false, name)
				rollback = true
				break
			}
		}
	}
	return rollback
}

// syncPreset writes or removes 10-bor.preset for the system manager and
// for the user managers.
func (s *systemdSyncer) syncPreset() {
	for _, global := range []bool{false, true} {
		path := s.opts.dirs().presetPath(global)
		want := RenderSystemdPresetScope(s.desired, global)
		if want == nil {
			if isBorSystemdDropIn(path) && removeSystemdTaskFile(path) {
				log.Printf("systemd: removed preset file %s", path)
				s.changed = true
			}
			continue
		}
		cur, _ := os.ReadFile(path) //nolint:gosec // G304: path under the configured preset directory
		if bytes.Equal(cur, want) {
			continue
		}
		if err := WriteFileAtomically(path, want); err != nil {
			if global {
				s.userPresetErr = "write preset: " + err.Error()
			} else {
				s.presetErr = "write preset: " + err.Error()
			}
			continue
		}
		log.Printf("systemd: wrote preset file %s", path)
		s.changed = true
	}
}

// taskEnabled reports whether a task's trigger should be active.
func taskEnabled(t *pb.SystemdTask) bool {
	return t.Enabled == nil || t.GetEnabled()
}

// applyTasks enables and starts (or disables and stops) the trigger unit
// of every deployed task.
func (s *systemdSyncer) applyTasks() {
	for _, name := range s.desired.TaskNames() {
		run := s.tasks[name]
		d := s.desired.Tasks[name]
		t := d.Task
		if run.disabled || run.verifyErr != "" || len(run.errs) > 0 {
			continue
		}
		if d.Global {
			s.applyGlobalTask(name, run, t)
			continue
		}
		unit := systemdunits.TaskServiceUnit(name)
		timer := t.GetTrigger() == pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER
		if timer {
			unit = systemdunits.TaskTimerUnit(name)
		}
		st, err := s.mgr.Status(s.ctx, unit)
		if err != nil {
			run.errs = append(run.errs, err.Error())
			continue
		}
		if !st.Exists() {
			run.errs = append(run.errs, unit+" is not loaded")
			continue
		}
		names := []string{unit}
		if taskEnabled(t) {
			if !systemdEnabledState(st.UnitFileState) {
				if err := s.mgr.Enable(s.ctx, names); err != nil {
					run.errs = append(run.errs, "enable "+unit+": "+err.Error())
					continue
				}
				run.notes = append(run.notes, "enabled")
				s.changed = true
				if s.opts.Reconcile && !run.changed {
					s.drifts = append(s.drifts, SystemdDrift{Unit: unit, Kind: "enablement", Found: st.UnitFileState, Action: "enabled"})
				}
			}
			if !timer {
				continue
			}
			switch {
			case !st.IsActive():
				if s.runTaskJob(run, unit, "start") && s.opts.Reconcile && !run.changed {
					s.drifts = append(s.drifts, SystemdDrift{Unit: unit, Kind: "activity", Found: fmt.Sprintf("%s (%s)", st.ActiveState, st.SubState), Action: "started"})
				}
			case run.changed:
				s.runTaskJob(run, unit, "restart")
			}
			continue
		}
		if timer && st.IsActive() {
			s.runTaskJob(run, unit, "stop")
		}
		if systemdEnabledState(st.UnitFileState) {
			if err := s.mgr.Disable(s.ctx, names); err != nil {
				run.errs = append(run.errs, "disable "+unit+": "+err.Error())
				continue
			}
			run.notes = append(run.notes, "disabled")
			s.changed = true
		}
	}
}

// applyGlobalTask enables the trigger unit of a user-manager task on the
// file system and starts its timer in every logged-in user's manager.
func (s *systemdSyncer) applyGlobalTask(name string, run *systemdTaskRun, t *pb.SystemdTask) {
	g := s.opts.globals()
	unit := systemdunits.TaskServiceUnit(name)
	timer := t.GetTrigger() == pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER
	if timer {
		unit = systemdunits.TaskTimerUnit(name)
	}
	state := g.State(unit)
	if state == systemd.GlobalStateNotFound {
		run.errs = append(run.errs, unit+" is not installed")
		return
	}
	if taskEnabled(t) {
		if !systemd.IsEnabledState(state) {
			if err := g.Enable(unit); err != nil {
				run.errs = append(run.errs, "enable "+unit+": "+err.Error())
				return
			}
			run.notes = append(run.notes, "enabled")
			s.changed = true
			s.userReload = true
		}
		if !timer {
			return
		}
		s.reloadUsers()
		for _, ut := range s.userTargets() {
			if ut.mgr == nil {
				continue
			}
			st, err := ut.mgr.Status(s.ctx, unit)
			if err != nil || !st.Exists() {
				continue
			}
			switch {
			case !st.IsActive():
				s.runUserTaskJob(run, ut, unit, "start")
			case run.changed:
				s.runUserTaskJob(run, ut, unit, "restart")
			}
		}
		return
	}
	if timer {
		for _, ut := range s.userTargets() {
			if ut.mgr == nil {
				continue
			}
			if st, err := ut.mgr.Status(s.ctx, unit); err == nil && st.Exists() && st.IsActive() {
				s.runUserTaskJob(run, ut, unit, "stop")
			}
		}
	}
	if systemd.IsEnabledState(state) {
		if err := g.Disable(unit); err != nil {
			run.errs = append(run.errs, "disable "+unit+": "+err.Error())
			return
		}
		run.notes = append(run.notes, "disabled")
		s.changed = true
		s.userReload = true
	}
}

// runUserTaskJob runs a task trigger job in one user's manager.
func (s *systemdSyncer) runUserTaskJob(run *systemdTaskRun, ut *systemdUserTarget, unit, verb string) {
	var (
		r   systemd.JobResult
		err error
	)
	switch verb {
	case "start":
		r, err = ut.mgr.Start(s.ctx, unit)
	case "stop":
		r, err = ut.mgr.Stop(s.ctx, unit)
	default:
		r, err = ut.mgr.Restart(s.ctx, unit)
	}
	s.changed = true
	who := ut.sess.String()
	if err != nil {
		run.errs = append(run.errs, fmt.Sprintf("%s %s for %s: %v", verb, unit, who, err))
		return
	}
	if !r.OK() {
		run.errs = append(run.errs, fmt.Sprintf("%s %s for %s failed (job result %s)", verb, unit, who, r))
		return
	}
	run.notes = append(run.notes, map[string]string{"start": "timer started", "stop": "timer stopped", "restart": "timer restarted"}[verb]+" for "+who)
	log.Printf("systemd: %s %s for %s: %s", verb, unit, who, r)
}

// runTaskJob runs a task trigger job in the system manager and reports
// whether it reached its goal.
func (s *systemdSyncer) runTaskJob(run *systemdTaskRun, unit, verb string) bool {
	var (
		r   systemd.JobResult
		err error
	)
	switch verb {
	case "start":
		r, err = s.mgr.Start(s.ctx, unit)
	case "stop":
		r, err = s.mgr.Stop(s.ctx, unit)
	default:
		r, err = s.mgr.Restart(s.ctx, unit)
	}
	s.changed = true
	if err != nil {
		run.errs = append(run.errs, verb+" "+unit+": "+err.Error())
		return false
	}
	if !r.OK() {
		run.errs = append(run.errs, fmt.Sprintf("%s %s failed (job result %s)", verb, unit, r))
		return false
	}
	run.notes = append(run.notes, map[string]string{"start": "timer started", "stop": "timer stopped", "restart": "timer restarted"}[verb])
	log.Printf("systemd: %s %s: %s", verb, unit, r)
	return true
}

// ── compliance ──────────────────────────────────────────────────────────

// taskItem builds the compliance item of one task for one policy.
func (s *systemdSyncer) taskItem(e SystemdEntry, t *pb.SystemdTask) *pb.ComplianceItemResult {
	name := t.GetName()
	item := &pb.ComplianceItemResult{SchemaId: systemdTaskSchema, Key: name}
	set := func(st pb.ComplianceStatus, msg string) *pb.ComplianceItemResult {
		item.Status = st
		item.Message = msg
		return item
	}
	if err := systemdunits.ValidateTaskName(name); err != nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error())
	}
	if !systemdTaskWellFormed(t) {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "task definition is incomplete or invalid; nothing applied")
	}
	d := s.desired.Tasks[name]
	run := s.tasks[name]
	if d == nil || run == nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "task missing from the merged state")
	}
	if d.By.ID != e.ID && !proto.Equal(d.Task, t) {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "overridden by "+systemdSourceLabel(d.By))
	}
	if run.disabled {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "Bor tasks are disabled on this node (systemd.allow_tasks is off); nothing applied")
	}
	if run.verifyErr != "" {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, run.verifyErr)
	}
	if d.Global {
		return s.globalTaskItem(set, t, run)
	}

	serviceUnit := systemdunits.TaskServiceUnit(name)
	timerUnit := systemdunits.TaskTimerUnit(name)
	timer := t.GetTrigger() == pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER
	svc, svcErr := s.mgr.Status(s.ctx, serviceUnit)
	var tmr *systemd.UnitStatus
	var tmrErr error
	if timer {
		tmr, tmrErr = s.mgr.Status(s.ctx, timerUnit)
	}
	summary := "unit state unknown"
	switch {
	case timer && tmrErr == nil && tmr.Exists():
		summary = "timer " + systemdStateSummary(tmr)
	case !timer && svcErr == nil && svc.Exists():
		summary = "service " + systemdStateSummary(svc)
	}
	if svcErr == nil && svc.Exists() && !svc.ExecMainStart.IsZero() {
		summary += fmt.Sprintf(", last result %s", svc.Result)
		if svc.Result != "success" {
			summary += fmt.Sprintf(" (exit status %d)", svc.ExecMainStatus)
		}
	}
	if len(run.errs) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(run.errs, "; ")+"; "+summary)
	}

	var problems []string
	problems = append(problems, s.taskFileProblems(t, false)...)
	enabled := taskEnabled(t)
	switch {
	case timer && (tmrErr != nil || !tmr.Exists()):
		problems = append(problems, timerUnit+" is not loaded")
	case timer && enabled:
		if !systemdEnabledState(tmr.UnitFileState) {
			problems = append(problems, fmt.Sprintf("timer is %s, expected enabled", tmr.UnitFileState))
		}
		if !tmr.IsActive() {
			problems = append(problems, fmt.Sprintf("timer is %s (%s), expected active", tmr.ActiveState, tmr.SubState))
		}
	case timer:
		if tmr.IsActive() {
			problems = append(problems, "timer is active, expected stopped (task not enabled)")
		}
	case svcErr != nil || !svc.Exists():
		problems = append(problems, serviceUnit+" is not loaded")
	case enabled && !systemdEnabledState(svc.UnitFileState):
		problems = append(problems, fmt.Sprintf("service is %s, expected enabled at boot", svc.UnitFileState))
	}
	if t.GetRequireLastRunSuccess() && svcErr == nil && svc.Exists() && !svc.ExecMainStart.IsZero() && svc.Result != "success" {
		msg := fmt.Sprintf("last run failed (%s, exit status %d)", svc.Result, svc.ExecMainStatus)
		if s.opts.Journal != nil {
			if excerpt := s.opts.Journal.Excerpt(s.ctx, serviceUnit); excerpt != "" {
				msg += "; journal: " + excerpt
			}
		}
		problems = append(problems, msg)
	}
	msg := summary
	if len(run.notes) > 0 {
		msg += "; " + strings.Join(run.notes, "; ")
	}
	if len(problems) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(problems, "; ")+"; "+msg)
	}
	return set(pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, msg)
}

// taskFileProblems compares a task's files on disk with the policy.
func (s *systemdSyncer) taskFileProblems(t *pb.SystemdTask, global bool) []string {
	dirs := s.opts.dirs()
	name := t.GetName()
	var problems []string
	for _, f := range []struct {
		path string
		want []byte
	}{
		{dirs.TaskUnitPath(global, systemdunits.TaskServiceUnit(name)), RenderSystemdTaskService(t, dirs.TaskScriptPath(name))},
		{dirs.TaskUnitPath(global, systemdunits.TaskTimerUnit(name)), RenderSystemdTaskTimer(t)},
		{dirs.TaskScriptPath(name), []byte(t.GetScript())},
	} {
		if len(f.want) == 0 {
			continue
		}
		cur, err := os.ReadFile(f.path) //nolint:gosec // G304: path built from a validated task name under the configured directories
		switch {
		case err != nil:
			problems = append(problems, filepath.Base(f.path)+" is missing")
		case !bytes.Equal(cur, f.want):
			problems = append(problems, filepath.Base(f.path)+" differs from the policy")
		}
	}
	return problems
}

// globalTaskItem finishes the compliance item of a user-manager task: the
// trigger's global enablement, its activity and last result in every
// logged-in user's manager.
func (s *systemdSyncer) globalTaskItem(set func(pb.ComplianceStatus, string) *pb.ComplianceItemResult, t *pb.SystemdTask, run *systemdTaskRun) *pb.ComplianceItemResult {
	name := t.GetName()
	serviceUnit := systemdunits.TaskServiceUnit(name)
	timerUnit := systemdunits.TaskTimerUnit(name)
	timer := t.GetTrigger() == pb.SystemdTaskTrigger_SYSTEMD_TASK_TRIGGER_TIMER
	trigger := serviceUnit
	if timer {
		trigger = timerUnit
	}
	state := s.opts.globals().State(trigger)
	summary := trigger + " " + state + " for all users"
	enabled := taskEnabled(t)
	var problems []string
	switch {
	case state == systemd.GlobalStateNotFound:
		problems = append(problems, trigger+" is not installed")
	case enabled && !systemd.IsEnabledState(state):
		problems = append(problems, fmt.Sprintf("%s is %s, expected enabled", trigger, state))
	case !enabled && systemd.IsEnabledState(state):
		problems = append(problems, fmt.Sprintf("%s is enabled, expected disabled (task not enabled)", trigger))
	}
	problems = append(problems, s.taskFileProblems(t, true)...)
	targets := s.userTargets()
	if len(targets) == 0 {
		summary += "; no user session"
	}
	for _, ut := range targets {
		who := ut.sess.String()
		if ut.mgr == nil {
			summary += fmt.Sprintf("; %s: %s", who, ut.err)
			continue
		}
		svc, svcErr := ut.mgr.Status(s.ctx, serviceUnit)
		if timer {
			tmr, err := ut.mgr.Status(s.ctx, timerUnit)
			switch {
			case err != nil || !tmr.Exists():
				summary += fmt.Sprintf("; %s: timer not loaded", who)
			default:
				summary += fmt.Sprintf("; %s: timer %s", who, systemdActivitySummary(tmr))
				if enabled && !tmr.IsActive() {
					problems = append(problems, fmt.Sprintf("%s: timer is %s (%s), expected active", who, tmr.ActiveState, tmr.SubState))
				}
				if !enabled && tmr.IsActive() {
					problems = append(problems, who+": timer is active, expected stopped (task not enabled)")
				}
			}
		}
		if svcErr == nil && svc.Exists() && !svc.ExecMainStart.IsZero() {
			summary += fmt.Sprintf(", last result %s", svc.Result)
			if svc.Result != "success" {
				summary += fmt.Sprintf(" (exit status %d)", svc.ExecMainStatus)
				if t.GetRequireLastRunSuccess() {
					problems = append(problems, fmt.Sprintf("%s: last run failed (%s, exit status %d)", who, svc.Result, svc.ExecMainStatus))
				}
			}
		}
	}
	if len(run.errs) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(run.errs, "; ")+"; "+summary)
	}
	msg := summary
	if len(run.notes) > 0 {
		msg += "; " + strings.Join(run.notes, "; ")
	}
	if len(problems) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(problems, "; ")+"; "+msg)
	}
	return set(pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, msg)
}

// presetItem builds the compliance item of a preset file: the system
// manager's, or (global) the user managers'.
func (s *systemdSyncer) presetItem(global bool) *pb.ComplianceItemResult {
	key := SystemdPresetName
	presetErr := s.presetErr
	if global {
		key = "user/" + SystemdPresetName
		presetErr = s.userPresetErr
	}
	item := &pb.ComplianceItemResult{SchemaId: systemdPresetSchema, Key: key}
	set := func(st pb.ComplianceStatus, msg string) *pb.ComplianceItemResult {
		item.Status = st
		item.Message = msg
		return item
	}
	if presetErr != "" {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, presetErr)
	}
	path := s.opts.dirs().presetPath(global)
	want := RenderSystemdPresetScope(s.desired, global)
	if want == nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no unit with an enablement to preset")
	}
	cur, err := os.ReadFile(path) //nolint:gosec // G304: path under the configured preset directory
	switch {
	case err != nil:
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, "preset file "+path+" is missing")
	case !bytes.Equal(cur, want):
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, "preset file "+path+" differs from the policy")
	}
	lines := bytes.Count(want, []byte("\n")) - 1
	return set(pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, fmt.Sprintf("%d preset %s in %s", lines, pluralSystemd(lines, "line", "lines"), path))
}

func pluralSystemd(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
