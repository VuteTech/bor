// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
	"google.golang.org/protobuf/proto"
)

// SystemdSystemDir holds administrator unit files and drop-ins.
const SystemdSystemDir = "/etc/systemd/system"

// SystemdDropInName is the one drop-in Bor writes per unit. It sorts after
// the usual distribution drop-ins (10-, 50-) so Bor's values win.
const SystemdDropInName = "90-bor.conf"

// systemdDropInHeader marks a drop-in as Bor's; ListBorManagedSystemdFiles
// recognises files by it.
const systemdDropInHeader = "# Managed by Bor - changes are overwritten"

// Compliance item schema ids.
const (
	systemdItemSchema   = "systemd:unit"
	systemdTaskSchema   = "systemd:task"
	systemdPresetSchema = "systemd:preset"
)

// SystemdPresetDir holds administrator preset files.
const SystemdPresetDir = "/etc/systemd/system-preset"

// SystemdPresetName is the one preset file Bor writes; "10-" sorts before
// the distribution's 90-/99-default.preset, and the first match wins.
const SystemdPresetName = "10-bor.preset"

// SystemdScriptsDir holds the scripts of Bor-deployed tasks. The directory
// belongs to Bor: every file in it is managed.
const SystemdScriptsDir = "/etc/bor/systemd"

// SystemdUserDir holds the administrator's units and drop-ins for every
// user's manager (the global scope).
const SystemdUserDir = "/etc/systemd/user"

// SystemdUserPresetDir holds administrator preset files for user managers.
const SystemdUserPresetDir = "/etc/systemd/user-preset"

// SystemdDirs are the directories a sync reads and writes.
type SystemdDirs struct {
	SystemDir     string
	PresetDir     string
	ScriptsDir    string
	UserDir       string
	UserPresetDir string
}

func (d *SystemdDirs) systemDir() string {
	if d.SystemDir == "" {
		return SystemdSystemDir
	}
	return d.SystemDir
}

func (d *SystemdDirs) presetDir() string {
	if d.PresetDir == "" {
		return SystemdPresetDir
	}
	return d.PresetDir
}

func (d *SystemdDirs) userDir() string {
	if d.UserDir == "" {
		return SystemdUserDir
	}
	return d.UserDir
}

func (d *SystemdDirs) userPresetDir() string {
	if d.UserPresetDir == "" {
		return SystemdUserPresetDir
	}
	return d.UserPresetDir
}

// UnitDir returns the unit directory of a scope.
func (d *SystemdDirs) UnitDir(global bool) string {
	if global {
		return d.userDir()
	}
	return d.systemDir()
}

// UserPresetPath returns the path of Bor's preset file for user managers.
func (d *SystemdDirs) UserPresetPath() string {
	return filepath.Join(d.userPresetDir(), SystemdPresetName)
}

func (d *SystemdDirs) presetPath(global bool) string {
	if global {
		return d.UserPresetPath()
	}
	return d.PresetPath()
}

// TaskUnitPath returns the path of a task unit file in a scope.
func (d *SystemdDirs) TaskUnitPath(global bool, unit string) string {
	return filepath.Join(d.UnitDir(global), unit)
}

func (d *SystemdDirs) scriptsDir() string {
	if d.ScriptsDir == "" {
		return SystemdScriptsDir
	}
	return d.ScriptsDir
}

// ScriptsDirPath returns the directory of task scripts.
func (d *SystemdDirs) ScriptsDirPath() string {
	return d.scriptsDir()
}

// PresetPath returns the path of Bor's preset file.
func (d *SystemdDirs) PresetPath() string {
	return filepath.Join(d.presetDir(), SystemdPresetName)
}

// TaskServicePath returns the path of a task's service unit file.
func (d *SystemdDirs) TaskServicePath(name string) string {
	return filepath.Join(d.systemDir(), systemdunits.TaskServiceUnit(name))
}

// TaskTimerPath returns the path of a task's timer unit file.
func (d *SystemdDirs) TaskTimerPath(name string) string {
	return filepath.Join(d.systemDir(), systemdunits.TaskTimerUnit(name))
}

// TaskScriptPath returns the path of a task's script.
func (d *SystemdDirs) TaskScriptPath(name string) string {
	return filepath.Join(d.scriptsDir(), name+".sh")
}

// SystemdEntry pairs a SystemdPolicy with the metadata the merge and the
// per-policy compliance reports need.
type SystemdEntry struct {
	ID       string
	Name     string
	Priority int32
	Policy   *pb.SystemdPolicy
}

// systemdSource identifies the policy that won an axis of a unit.
type systemdSource struct {
	ID       string
	Name     string
	Priority int32
}

// SystemdUnitDesired is the merged desired state of one unit.
type SystemdUnitDesired struct {
	Name string
	// Global marks a unit of the user managers (/etc/systemd/user) rather
	// than the system manager.
	Global       bool
	Enablement   pb.SystemdEnablement
	EnablementBy systemdSource
	Activity     pb.SystemdActivity
	ActivityBy   systemdSource
	Timer        *pb.SystemdTimerSettings
	TimerBy      systemdSource
	Service      *pb.SystemdServiceSettings
	ServiceBy    map[string]systemdSource
	// Raw directives, one per section and key; RawBy records the policy
	// that won each "Section/Key".
	Raw                   []*pb.SystemdDirective
	RawBy                 map[string]systemdSource
	RestartOnChange       bool
	RequireLastRunSuccess bool
}

// SystemdTaskDesired is a Bor-deployed task after the merge: the whole
// task comes from the highest-priority policy that defines the name.
type SystemdTaskDesired struct {
	Name string
	Task *pb.SystemdTask
	By   systemdSource
	// Global marks a task deployed to the user managers.
	Global bool
}

// SystemdDesired is the merged desired state of every managed unit and
// task. Units are keyed by SystemdUnitKey: the unit name in the system
// scope, "global/<name>" in the user-manager scope.
type SystemdDesired struct {
	Units map[string]*SystemdUnitDesired
	Tasks map[string]*SystemdTaskDesired
	// WritePresets is on when any policy asks for the preset file.
	WritePresets bool
}

// systemdGlobalKeyPrefix marks the keys of user-manager units; "/" never
// occurs in a unit name.
const systemdGlobalKeyPrefix = "global/"

// SystemdUnitKey identifies a unit within the merged state.
func SystemdUnitKey(global bool, name string) string {
	if global {
		return systemdGlobalKeyPrefix + name
	}
	return name
}

// SystemdScopeIsGlobal reports whether a policy scope selects the user
// managers.
func SystemdScopeIsGlobal(scope pb.SystemdScope) bool {
	return scope == pb.SystemdScope_SYSTEMD_SCOPE_GLOBAL
}

// systemdSplitKey recovers the scope and name from a merged-state key.
func systemdSplitKey(key string) (name string, global bool) {
	if strings.HasPrefix(key, systemdGlobalKeyPrefix) {
		return strings.TrimPrefix(key, systemdGlobalKeyPrefix), true
	}
	return key, false
}

// Key returns the unit's key in the merged state.
func (u *SystemdUnitDesired) Key() string {
	return SystemdUnitKey(u.Global, u.Name)
}

// Names returns the keys of the managed units, sorted.
func (d *SystemdDesired) Names() []string {
	names := make([]string, 0, len(d.Units))
	for n := range d.Units {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TaskNames returns the task names, sorted.
func (d *SystemdDesired) TaskNames() []string {
	names := make([]string, 0, len(d.Tasks))
	for n := range d.Tasks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ── merge ───────────────────────────────────────────────────────────────

// systemdEnablementRank orders enablement values by restrictiveness.
func systemdEnablementRank(e pb.SystemdEnablement) int {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return 3
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return 2
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return 1
	}
	return 0
}

func systemdActivityRank(a pb.SystemdActivity) int {
	switch a {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return 2
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		return 1
	}
	return 0
}

// sortSystemdEntries orders entries by priority (highest first), then by
// name and id so the merge is deterministic.
func sortSystemdEntries(entries []SystemdEntry) []SystemdEntry {
	sorted := make([]SystemdEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

// MergeSystemdPolicies combines every bound policy into one desired state
// per unit. Highest binding priority wins each axis; on equal priority
// the more restrictive value wins (masked > disabled > enabled, stopped >
// running). A masked unit is always stopped. The timer block is atomic;
// service directives merge one by one; environment entries are a union
// keyed by variable name. The record of which policy won each axis feeds
// the per-policy compliance reports.
func MergeSystemdPolicies(entries []SystemdEntry) *SystemdDesired {
	d := &SystemdDesired{Units: map[string]*SystemdUnitDesired{}, Tasks: map[string]*SystemdTaskDesired{}}
	for _, e := range sortSystemdEntries(entries) {
		if e.Policy == nil {
			continue
		}
		src := systemdSource{ID: e.ID, Name: e.Name, Priority: e.Priority}
		d.WritePresets = d.WritePresets || e.Policy.GetWritePresets()
		for _, t := range e.Policy.GetTasks() {
			name := t.GetName()
			if systemdunits.ValidateTaskName(name) != nil || !systemdTaskWellFormed(t) {
				continue
			}
			if _, taken := d.Tasks[name]; taken {
				continue
			}
			d.Tasks[name] = &SystemdTaskDesired{Name: name, Task: proto.Clone(t).(*pb.SystemdTask), By: src, Global: SystemdScopeIsGlobal(t.GetScope())}
		}
		for _, u := range e.Policy.GetUnits() {
			name := u.GetName()
			global := SystemdScopeIsGlobal(u.GetScope())
			if systemdunits.ValidateName(name) != nil || systemdunits.IsProtected(name) || (global && systemdunits.IsProtectedGlobal(name)) {
				continue
			}
			key := SystemdUnitKey(global, name)
			cur := d.Units[key]
			if cur == nil {
				cur = &SystemdUnitDesired{Name: name, Global: global, ServiceBy: map[string]systemdSource{}, RawBy: map[string]systemdSource{}}
				d.Units[key] = cur
			}
			for _, raw := range u.GetDropIn().GetRaw() {
				if !systemdDirectiveWellFormed(raw) {
					continue
				}
				key := raw.GetSection() + "/" + raw.GetKey()
				if _, taken := cur.RawBy[key]; taken {
					continue
				}
				cur.Raw = append(cur.Raw, proto.Clone(raw).(*pb.SystemdDirective))
				cur.RawBy[key] = src
			}
			if en := u.GetEnablement(); en != pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
				if cur.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED ||
					(cur.EnablementBy.Priority == e.Priority && systemdEnablementRank(en) > systemdEnablementRank(cur.Enablement)) {
					cur.Enablement = en
					cur.EnablementBy = src
				}
			}
			if ac := u.GetActivity(); ac != pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED {
				if cur.Activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED ||
					(cur.ActivityBy.Priority == e.Priority && systemdActivityRank(ac) > systemdActivityRank(cur.Activity)) {
					cur.Activity = ac
					cur.ActivityBy = src
				}
			}
			if t := u.GetDropIn().GetTimer(); !systemdTimerEmpty(t) && cur.Timer == nil {
				cur.Timer = proto.Clone(t).(*pb.SystemdTimerSettings)
				cur.TimerBy = src
			}
			if s := u.GetDropIn().GetService(); !systemdServiceEmpty(s) {
				mergeSystemdService(cur, s, src)
			}
			cur.RestartOnChange = cur.RestartOnChange || u.GetRestartOnChange()
			cur.RequireLastRunSuccess = cur.RequireLastRunSuccess || u.GetRequireLastRunSuccess()
		}
	}
	for _, cur := range d.Units {
		if cur.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED &&
			cur.Activity != pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED {
			cur.Activity = pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED
			cur.ActivityBy = cur.EnablementBy
		}
	}
	return d
}

// mergeSystemdService merges one policy's service settings into the
// unit's merged settings. Entries are processed from the highest
// priority down, so a directive that is already set keeps its value.
func mergeSystemdService(cur *SystemdUnitDesired, s *pb.SystemdServiceSettings, src systemdSource) {
	if cur.Service == nil {
		cur.Service = &pb.SystemdServiceSettings{}
	}
	m := cur.Service
	setStr := func(key string, dst *string, val string) {
		if val != "" && *dst == "" {
			*dst = val
			cur.ServiceBy[key] = src
		}
	}
	setBool := func(key string, dst **bool, val *bool) {
		if val != nil && *dst == nil {
			v := *val
			*dst = &v
			cur.ServiceBy[key] = src
		}
	}
	setStr("Restart", &m.Restart, s.GetRestart())
	setStr("RestartSec", &m.RestartSec, s.GetRestartSec())
	setStr("TimeoutStartSec", &m.TimeoutStartSec, s.GetTimeoutStartSec())
	setStr("TimeoutStopSec", &m.TimeoutStopSec, s.GetTimeoutStopSec())
	setStr("MemoryMax", &m.MemoryMax, s.GetMemoryMax())
	setStr("CPUQuota", &m.CpuQuota, s.GetCpuQuota())
	setStr("TasksMax", &m.TasksMax, s.GetTasksMax())
	setStr("Nice", &m.Nice, s.GetNice())
	setStr("ProtectSystem", &m.ProtectSystem, s.GetProtectSystem())
	setStr("ProtectHome", &m.ProtectHome, s.GetProtectHome())
	setBool("NoNewPrivileges", &m.NoNewPrivileges, s.NoNewPrivileges)
	setBool("PrivateTmp", &m.PrivateTmp, s.PrivateTmp)
	for _, env := range s.GetEnvironment() {
		key, _, _ := strings.Cut(env, "=")
		if key == "" {
			continue
		}
		if _, taken := cur.ServiceBy["Environment:"+key]; taken {
			continue
		}
		m.Environment = append(m.Environment, env)
		cur.ServiceBy["Environment:"+key] = src
	}
}

func systemdTimerEmpty(t *pb.SystemdTimerSettings) bool {
	if t == nil {
		return true
	}
	return len(t.GetOnCalendar()) == 0 && t.GetOnBootSec() == "" && t.GetOnStartupSec() == "" &&
		t.GetOnActiveSec() == "" && t.GetOnUnitActiveSec() == "" && t.GetOnUnitInactiveSec() == "" &&
		t.Persistent == nil && t.GetRandomizedDelaySec() == "" && t.GetAccuracySec() == "" &&
		t.WakeSystem == nil && t.FixedRandomDelay == nil && t.OnClockChange == nil && t.OnTimezoneChange == nil
}

func systemdServiceEmpty(s *pb.SystemdServiceSettings) bool {
	if s == nil {
		return true
	}
	return s.GetRestart() == "" && s.GetRestartSec() == "" && s.GetTimeoutStartSec() == "" &&
		s.GetTimeoutStopSec() == "" && len(s.GetEnvironment()) == 0 && s.GetMemoryMax() == "" &&
		s.GetCpuQuota() == "" && s.GetTasksMax() == "" && s.GetNice() == "" && s.NoNewPrivileges == nil &&
		s.GetProtectSystem() == "" && s.GetProtectHome() == "" && s.PrivateTmp == nil
}

// ── rendering ───────────────────────────────────────────────────────────

func systemdYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// systemdQuote renders a value as a double-quoted unit file string.
func systemdQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// systemdSectionOrder is the order sections appear in rendered files.
var systemdSectionOrder = []string{"Unit", "Service", "Timer", "Socket", "Path", "Install"}

// renderSystemdTimerSettings writes [Timer] directives. With reset, every
// trigger directive is reset before it is set so a Bor schedule replaces
// the vendor schedule in a drop-in instead of adding to it.
func renderSystemdTimerSettings(b *bytes.Buffer, t *pb.SystemdTimerSettings, reset bool) {
	if len(t.GetOnCalendar()) > 0 {
		if reset {
			b.WriteString("OnCalendar=\n")
		}
		for _, c := range t.GetOnCalendar() {
			b.WriteString("OnCalendar=" + c + "\n")
		}
	}
	for _, kv := range []struct{ k, v string }{
		{"OnBootSec", t.GetOnBootSec()},
		{"OnStartupSec", t.GetOnStartupSec()},
		{"OnActiveSec", t.GetOnActiveSec()},
		{"OnUnitActiveSec", t.GetOnUnitActiveSec()},
		{"OnUnitInactiveSec", t.GetOnUnitInactiveSec()},
	} {
		if kv.v != "" {
			if reset {
				b.WriteString(kv.k + "=\n")
			}
			b.WriteString(kv.k + "=" + kv.v + "\n")
		}
	}
	if t.Persistent != nil {
		b.WriteString("Persistent=" + systemdYesNo(t.GetPersistent()) + "\n")
	}
	if v := t.GetRandomizedDelaySec(); v != "" {
		b.WriteString("RandomizedDelaySec=" + v + "\n")
	}
	if v := t.GetAccuracySec(); v != "" {
		b.WriteString("AccuracySec=" + v + "\n")
	}
	if t.WakeSystem != nil {
		b.WriteString("WakeSystem=" + systemdYesNo(t.GetWakeSystem()) + "\n")
	}
	if t.FixedRandomDelay != nil {
		b.WriteString("FixedRandomDelay=" + systemdYesNo(t.GetFixedRandomDelay()) + "\n")
	}
	if t.OnClockChange != nil {
		b.WriteString("OnClockChange=" + systemdYesNo(t.GetOnClockChange()) + "\n")
	}
	if t.OnTimezoneChange != nil {
		b.WriteString("OnTimezoneChange=" + systemdYesNo(t.GetOnTimezoneChange()) + "\n")
	}
}

// renderSystemdServiceSettings writes [Service] directives.
func renderSystemdServiceSettings(b *bytes.Buffer, s *pb.SystemdServiceSettings) {
	for _, kv := range []struct{ k, v string }{
		{"Restart", s.GetRestart()},
		{"RestartSec", s.GetRestartSec()},
		{"TimeoutStartSec", s.GetTimeoutStartSec()},
		{"TimeoutStopSec", s.GetTimeoutStopSec()},
	} {
		if kv.v != "" {
			b.WriteString(kv.k + "=" + kv.v + "\n")
		}
	}
	env := append([]string(nil), s.GetEnvironment()...)
	sort.Strings(env)
	for _, e := range env {
		b.WriteString("Environment=" + systemdQuote(e) + "\n")
	}
	for _, kv := range []struct{ k, v string }{
		{"MemoryMax", s.GetMemoryMax()},
		{"CPUQuota", s.GetCpuQuota()},
		{"TasksMax", s.GetTasksMax()},
		{"Nice", s.GetNice()},
	} {
		if kv.v != "" {
			b.WriteString(kv.k + "=" + kv.v + "\n")
		}
	}
	if s.NoNewPrivileges != nil {
		b.WriteString("NoNewPrivileges=" + systemdYesNo(s.GetNoNewPrivileges()) + "\n")
	}
	if v := s.GetProtectSystem(); v != "" {
		b.WriteString("ProtectSystem=" + v + "\n")
	}
	if v := s.GetProtectHome(); v != "" {
		b.WriteString("ProtectHome=" + v + "\n")
	}
	if s.PrivateTmp != nil {
		b.WriteString("PrivateTmp=" + systemdYesNo(s.GetPrivateTmp()) + "\n")
	}
}

// renderSystemdRawDirectives appends the raw directives of one section,
// sorted by key. A directive without values is written as "Key=" (reset);
// one with values is reset first and then set to exactly those values.
func renderSystemdRawDirectives(b *bytes.Buffer, raw []*pb.SystemdDirective) {
	sorted := append([]*pb.SystemdDirective(nil), raw...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].GetKey() < sorted[j].GetKey() })
	for _, d := range sorted {
		b.WriteString(d.GetKey() + "=\n")
		for _, v := range d.GetValues() {
			b.WriteString(d.GetKey() + "=" + v + "\n")
		}
	}
}

// RenderSystemdDropIn renders the 90-bor.conf drop-in for a unit, or nil
// when the unit has no override. Output is deterministic for a given
// desired state: sections in a fixed order, typed settings before raw
// directives, raw directives sorted by key.
func RenderSystemdDropIn(u *SystemdUnitDesired) []byte {
	if u == nil || (u.Timer == nil && u.Service == nil && len(u.Raw) == 0) {
		return nil
	}
	sections := map[string]*bytes.Buffer{}
	section := func(name string) *bytes.Buffer {
		if sections[name] == nil {
			sections[name] = &bytes.Buffer{}
		}
		return sections[name]
	}
	if u.Timer != nil {
		renderSystemdTimerSettings(section("Timer"), u.Timer, true)
	}
	if u.Service != nil {
		renderSystemdServiceSettings(section("Service"), u.Service)
	}
	bySection := map[string][]*pb.SystemdDirective{}
	for _, d := range u.Raw {
		bySection[d.GetSection()] = append(bySection[d.GetSection()], d)
	}
	for name, raw := range bySection {
		renderSystemdRawDirectives(section(name), raw)
	}

	var b bytes.Buffer
	b.WriteString(systemdDropInHeader + "\n")
	b.WriteString("# Unit: " + u.Name + "\n")
	for _, name := range systemdSectionOrder {
		sec := sections[name]
		if sec == nil || sec.Len() == 0 {
			continue
		}
		b.WriteString("\n[" + name + "]\n")
		b.Write(sec.Bytes())
	}
	return b.Bytes()
}

// SystemdDropInPath returns the path of Bor's drop-in for a unit.
func SystemdDropInPath(systemDir, unit string) string {
	if systemDir == "" {
		systemDir = SystemdSystemDir
	}
	return filepath.Join(systemDir, unit+".d", SystemdDropInName)
}

// ListBorManagedSystemdFiles returns every file Bor manages for this
// policy type: the drop-ins and task unit files under the system and the
// user unit directories that carry Bor's header, every file in the
// scripts directory, and the preset files when they carry the header.
func ListBorManagedSystemdFiles(dirs *SystemdDirs) ([]string, error) {
	var out []string
	for _, dir := range []string{dirs.systemDir(), dirs.userDir()} {
		dropIns, err := listBorSystemdDropIns(dir)
		if err != nil {
			return nil, err
		}
		units, err := listBorSystemdTaskUnits(dir)
		if err != nil {
			return nil, err
		}
		out = append(append(out, dropIns...), units...)
	}
	scripts, err := listBorSystemdScripts(dirs.scriptsDir())
	if err != nil {
		return nil, err
	}
	out = append(out, scripts...)
	for _, p := range []string{dirs.PresetPath(), dirs.UserPresetPath()} {
		if isBorSystemdDropIn(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// listBorSystemdDropIns returns the drop-ins under systemDir that carry
// Bor's header.
func listBorSystemdDropIns(systemDir string) ([]string, error) {
	entries, err := os.ReadDir(systemDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", systemDir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".d") {
			continue
		}
		p := filepath.Join(systemDir, e.Name(), SystemdDropInName)
		if isBorSystemdDropIn(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// listBorSystemdTaskUnits returns the bor-*.service and bor-*.timer files
// under systemDir that carry Bor's header.
func listBorSystemdTaskUnits(systemDir string) ([]string, error) {
	entries, err := os.ReadDir(systemDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", systemDir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !systemdunits.IsTaskUnit(e.Name()) {
			continue
		}
		p := filepath.Join(systemDir, e.Name())
		if isBorSystemdDropIn(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// listBorSystemdScripts returns every regular file in the scripts
// directory, which belongs to Bor.
func listBorSystemdScripts(scriptsDir string) ([]string, error) {
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", scriptsDir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, filepath.Join(scriptsDir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func isBorSystemdDropIn(path string) bool {
	f, err := os.Open(path) //nolint:gosec // G304: path under the configured unit directory
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, len(systemdDropInHeader))
	n, _ := f.Read(buf)
	return string(buf[:n]) == systemdDropInHeader
}

// systemdUnitFromDropInPath recovers the unit name from
// <dir>/<unit>.d/90-bor.conf.
func systemdUnitFromDropInPath(path string) string {
	return strings.TrimSuffix(filepath.Base(filepath.Dir(path)), ".d")
}

// removeSystemdDropIn deletes the drop-in and its directory when empty.
func removeSystemdDropIn(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Dir(path)) // only succeeds when empty
	return nil
}

// ── sync ────────────────────────────────────────────────────────────────

// SystemdSyncOptions are the node-specific inputs of a sync.
type SystemdSyncOptions struct {
	SystemDir     string
	PresetDir     string
	ScriptsDir    string
	UserDir       string
	UserPresetDir string
	StatePath     string
	// AllowTasks deploys Bor tasks; when false every task is reported as an
	// error and no task file is written (existing ones are removed).
	AllowTasks bool
	// Analyzer validates calendar expressions before a drop-in is written
	// and task unit files before the reload; nil skips the checks.
	Analyzer *systemd.Analyzer
	// Journal supplies the excerpt in the message of a failed task; nil
	// omits it.
	Journal *systemd.Journal
	// Globals performs the enablement operations of the user-manager
	// scope; nil uses the file-based implementation on UserDir.
	Globals *systemd.GlobalUnits
	// Users provides the managers of logged-in users for the activity of
	// user-manager units; nil means no user manager is reachable.
	Users systemd.UserManagers
	// Reconcile marks a pass that re-asserts an unchanged desired state,
	// so that every correction it makes is drift to report.
	Reconcile bool
}

func (o *SystemdSyncOptions) dirs() *SystemdDirs {
	return &SystemdDirs{SystemDir: o.SystemDir, PresetDir: o.PresetDir, ScriptsDir: o.ScriptsDir, UserDir: o.UserDir, UserPresetDir: o.UserPresetDir}
}

func (o *SystemdSyncOptions) globals() *systemd.GlobalUnits {
	if o.Globals != nil {
		return o.Globals
	}
	return &systemd.GlobalUnits{EtcDir: o.dirs().userDir()}
}

func (o *SystemdSyncOptions) statePath() string {
	if o.StatePath == "" {
		return SystemdStatePath
	}
	return o.StatePath
}

// SystemdPolicyResult is one policy's compliance after a sync.
type SystemdPolicyResult struct {
	Status  pb.ComplianceStatus
	Message string
	Items   []*pb.ComplianceItemResult
}

// SystemdDrift records a managed unit of the system scope whose state a
// reconcile pass found changed by hand and put back.
type SystemdDrift struct {
	Unit string
	// Kind is "activity" or "enablement".
	Kind string
	// Found is the state the pass found ("inactive (dead)", "enabled").
	Found string
	// Action is what the pass did ("started", "disabled").
	Action string
}

// Resource names the drift for a tamper report.
func (d SystemdDrift) Resource() string {
	return "unit:" + d.Unit
}

// Description renders the drift for a tamper report.
func (d SystemdDrift) Description() string {
	return fmt.Sprintf("%s changed by hand: found %s; %s", d.Kind, d.Found, d.Action)
}

// SystemdSyncResult is the outcome of a sync.
type SystemdSyncResult struct {
	// Policies maps policy id to its result.
	Policies map[string]SystemdPolicyResult
	// Changed reports whether the sync changed a file or a unit state, so
	// the caller can refresh the unit catalogue.
	Changed bool
	// Drifts lists the corrections a reconcile pass made to units that
	// were already under management (empty unless Reconcile was set).
	Drifts []SystemdDrift
}

// SystemdDesiredFiles returns every path a desired state renders (drop-ins,
// task units and scripts, the preset files), for the caller to suppress
// the file watcher before writing.
func SystemdDesiredFiles(dirs *SystemdDirs, d *SystemdDesired) []string {
	var out []string
	for _, key := range d.Names() {
		if u := d.Units[key]; u.Timer != nil || u.Service != nil || len(u.Raw) > 0 {
			out = append(out, SystemdDropInPath(dirs.UnitDir(u.Global), u.Name))
		}
	}
	for _, name := range d.TaskNames() {
		global := d.Tasks[name].Global
		out = append(out, dirs.TaskUnitPath(global, systemdunits.TaskServiceUnit(name)), dirs.TaskUnitPath(global, systemdunits.TaskTimerUnit(name)), dirs.TaskScriptPath(name))
	}
	if d.WritePresets {
		out = append(out, dirs.PresetPath(), dirs.UserPresetPath())
	}
	return out
}

// systemdUserRun is the state of one unit in one user's manager.
type systemdUserRun struct {
	sess   systemd.UserSession
	mgr    systemd.Manager
	status *systemd.UnitStatus
	err    string
}

// systemdUnitRun is the per-unit working state of one sync.
type systemdUnitRun struct {
	status        *systemd.UnitStatus
	notFound      bool
	queryErr      string
	dropInErr     string
	dropInChanged bool
	dropInWanted  bool
	errs          []string
	notes         []string
	startSkipped  bool
	// User-manager units: the global unit-file state and the unit in the
	// manager of every logged-in user.
	global      bool
	globalState string
	users       []*systemdUserRun
}

// systemdUserTarget is one logged-in user's manager, or why it cannot be
// reached.
type systemdUserTarget struct {
	sess systemd.UserSession
	mgr  systemd.Manager
	err  string
}

type systemdSyncer struct {
	ctx     context.Context
	mgr     systemd.Manager
	opts    *SystemdSyncOptions
	desired *SystemdDesired
	state   *SystemdState
	units   map[string]*systemdUnitRun
	tasks   map[string]*systemdTaskRun
	changed bool
	// presetErr records a failed preset write for the compliance item.
	presetErr string
	// userPresetErr is the same for the user-manager preset file.
	userPresetErr string
	// known lists the keys that had a state record before this sync: a
	// correction to one of them during a reconcile pass is drift.
	known map[string]bool
	// userReload is set when a file of the user-manager scope changed and
	// every user manager must reload.
	userReload bool
	// users caches the reachable user managers; nil until first needed.
	users       []*systemdUserTarget
	usersLoaded bool
	drifts      []SystemdDrift
}

// userTargets returns the managers of the logged-in users, connecting on
// first use. Without a provider there are none.
func (s *systemdSyncer) userTargets() []*systemdUserTarget {
	if s.usersLoaded {
		return s.users
	}
	s.usersLoaded = true
	if s.opts.Users == nil {
		return nil
	}
	sessions, err := s.opts.Users.Sessions(s.ctx)
	if err != nil {
		log.Printf("Warning: systemd: list user sessions: %v", err)
		return nil
	}
	for _, sess := range sessions {
		t := &systemdUserTarget{sess: sess}
		if mgr, err := s.opts.Users.Manager(s.ctx, sess); err != nil {
			t.err = err.Error()
			log.Printf("Warning: systemd: %v", err)
		} else {
			t.mgr = mgr
		}
		s.users = append(s.users, t)
	}
	return s.users
}

// reloadUsers reloads every reachable user manager when a file of the
// user-manager scope changed since the last reload.
func (s *systemdSyncer) reloadUsers() {
	if !s.userReload {
		return
	}
	s.userReload = false
	for _, t := range s.userTargets() {
		if t.mgr == nil {
			continue
		}
		if err := t.mgr.Reload(s.ctx); err != nil {
			log.Printf("Warning: systemd: reload user manager of %s: %v", t.sess, err)
		}
	}
}

// drift records a correction made during a reconcile pass to a system
// unit that was already under management.
func (s *systemdSyncer) drift(key, kind, found, action string) {
	name, global := systemdSplitKey(key)
	if global || !s.opts.Reconcile || !s.known[key] {
		return
	}
	s.drifts = append(s.drifts, SystemdDrift{Unit: name, Kind: kind, Found: found, Action: action})
}

// SyncSystemd applies the merged desired state of the entries to the node
// and returns per-policy compliance. With no entries it removes every Bor
// drop-in and restores the recorded original states.
func SyncSystemd(ctx context.Context, mgr systemd.Manager, entries []SystemdEntry, opts *SystemdSyncOptions) SystemdSyncResult {
	if opts == nil {
		opts = &SystemdSyncOptions{}
	}
	res := SystemdSyncResult{Policies: map[string]SystemdPolicyResult{}}
	desired := MergeSystemdPolicies(entries)

	fail := func(status pb.ComplianceStatus, msg string) SystemdSyncResult {
		for _, e := range entries {
			res.Policies[e.ID] = SystemdPolicyResult{Status: status, Message: msg}
		}
		return res
	}
	if mgr == nil {
		return fail(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "cannot reach systemd on the system bus")
	}
	if !mgr.Available() {
		return fail(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "systemd is not the init system on this node")
	}

	state, err := LoadSystemdState(opts.statePath())
	if err != nil {
		log.Printf("Warning: systemd: %v (starting with an empty record)", err)
	}
	s := &systemdSyncer{ctx: ctx, mgr: mgr, opts: opts, desired: desired, state: state, units: map[string]*systemdUnitRun{}, tasks: map[string]*systemdTaskRun{}, known: map[string]bool{}}
	for key := range state.Units {
		s.known[key] = true
	}
	for _, key := range desired.Names() {
		s.units[key] = &systemdUnitRun{global: desired.Units[key].Global}
	}
	for _, name := range desired.TaskNames() {
		s.tasks[name] = &systemdTaskRun{}
	}

	if err := s.syncFiles(); err != nil {
		return fail(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error())
	}
	s.restoreReleased()
	s.applyEnablement()
	s.applyActivity()
	s.applyTasks()
	if err := SaveSystemdState(opts.statePath(), s.state); err != nil {
		log.Printf("Warning: systemd: %v", err)
	}
	s.refreshStatus()

	for _, e := range entries {
		res.Policies[e.ID] = s.compliance(e)
	}
	res.Changed = s.changed
	res.Drifts = s.drifts
	return res
}

// syncFiles writes, updates and removes drop-ins and reloads systemd when
// anything changed; a drop-in systemd rejects is rolled back.
func (s *systemdSyncer) syncFiles() error {
	dirs := s.opts.dirs()
	reload := false
	var existing []string
	for _, dir := range []string{dirs.systemDir(), dirs.userDir()} {
		found, err := listBorSystemdDropIns(dir)
		if err != nil {
			return err
		}
		existing = append(existing, found...)
	}
	// changedIn notes a changed file of a scope: the system manager
	// reloads once, every user manager when a user-scope file changed.
	changedIn := func(global bool) {
		if global {
			s.userReload = true
		} else {
			reload = true
		}
	}
	for _, key := range s.desired.Names() {
		u := s.desired.Units[key]
		run := s.units[key]
		path := SystemdDropInPath(dirs.UnitDir(u.Global), u.Name)
		content := RenderSystemdDropIn(u)
		if content == nil {
			if _, err := os.Stat(path); err == nil && isBorSystemdDropIn(path) {
				if err := removeSystemdDropIn(path); err != nil {
					run.errs = append(run.errs, "remove drop-in: "+err.Error())
					continue
				}
				run.dropInChanged = true
				changedIn(u.Global)
			}
			continue
		}
		run.dropInWanted = true
		if u.Timer != nil && s.opts.Analyzer != nil && s.opts.Analyzer.Available() {
			for _, expr := range u.Timer.GetOnCalendar() {
				if err := s.opts.Analyzer.ValidateCalendar(s.ctx, expr); err != nil {
					run.dropInErr = err.Error()
					break
				}
			}
			if run.dropInErr != "" {
				if isBorSystemdDropIn(path) {
					_ = removeSystemdDropIn(path)
					changedIn(u.Global)
				}
				continue
			}
		}
		cur, _ := os.ReadFile(path) //nolint:gosec // G304: path built from a validated unit name under the configured directory
		if bytes.Equal(cur, content) {
			continue
		}
		if err := WriteFileAtomically(path, content); err != nil {
			run.errs = append(run.errs, "write drop-in: "+err.Error())
			continue
		}
		run.dropInChanged = true
		changedIn(u.Global)
	}
	for _, p := range existing {
		global := strings.HasPrefix(p, dirs.userDir()+string(filepath.Separator))
		if _, wanted := s.desired.Units[SystemdUnitKey(global, systemdUnitFromDropInPath(p))]; wanted {
			continue
		}
		if err := removeSystemdDropIn(p); err != nil {
			log.Printf("Warning: systemd: remove stale drop-in %s: %v", p, err)
			continue
		}
		log.Printf("systemd: removed stale drop-in %s", p)
		changedIn(global)
	}
	if err := s.syncTaskFiles(&reload); err != nil {
		return err
	}
	s.syncPreset()
	if s.userReload {
		s.changed = true
		s.reloadUsers()
	}
	if !reload {
		return nil
	}
	s.changed = true
	if err := s.mgr.Reload(s.ctx); err != nil {
		return err
	}
	// A drop-in or task unit systemd cannot load must not stay on disk.
	rollback := false
	for _, key := range s.desired.Names() {
		u := s.desired.Units[key]
		run := s.units[key]
		if u.Global || !run.dropInChanged || !run.dropInWanted {
			continue
		}
		st, err := s.mgr.Status(s.ctx, u.Name)
		if err != nil {
			continue
		}
		if st.LoadState == "bad-setting" || st.LoadState == "error" {
			run.dropInErr = fmt.Sprintf("systemd rejected the drop-in (load state %s); it was removed", st.LoadState)
			_ = removeSystemdDropIn(SystemdDropInPath(dirs.systemDir(), u.Name))
			rollback = true
		}
	}
	if s.rollbackBadTaskUnits() {
		rollback = true
	}
	if rollback {
		if err := s.mgr.Reload(s.ctx); err != nil {
			return err
		}
	}
	return nil
}

// restoreReleased returns units that left every policy to their recorded
// original state and drops their records.
func (s *systemdSyncer) restoreReleased() {
	if len(s.state.Units) == 0 {
		return
	}
	names := make([]string, 0, len(s.state.Units))
	for n := range s.state.Units {
		if _, still := s.desired.Units[n]; !still {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	reload := false
	for _, key := range names {
		rec := s.state.Units[key]
		name, global := systemdSplitKey(key)
		var err error
		if global {
			err = s.restoreGlobalUnit(name, rec)
		} else {
			err = s.restoreUnit(name, rec, &reload)
		}
		if err != nil {
			log.Printf("Warning: systemd: restore %s: %v", key, err)
		} else {
			log.Printf("systemd: restored %s to its original state (%s, active=%v)", key, rec.UnitFileState, rec.Active)
		}
		delete(s.state.Units, key)
		s.changed = true
	}
	if reload {
		if err := s.mgr.Reload(s.ctx); err != nil {
			log.Printf("Warning: systemd: %v", err)
		}
	}
	s.reloadUsers()
}

// restoreGlobalUnit puts a user-manager unit's enablement back; its
// activity in user sessions is left as it is.
func (s *systemdSyncer) restoreGlobalUnit(name string, rec SystemdOriginal) error {
	if systemdunits.ValidateName(name) != nil || systemdunits.IsProtectedGlobal(name) {
		return fmt.Errorf("refusing to touch %q", name)
	}
	g := s.opts.globals()
	cur := g.State(name)
	if cur == systemd.GlobalStateNotFound {
		return nil
	}
	var err error
	switch {
	case rec.UnitFileState == systemd.GlobalStateMasked && cur != systemd.GlobalStateMasked:
		err = g.Mask(name)
	case rec.UnitFileState != systemd.GlobalStateMasked && cur == systemd.GlobalStateMasked:
		if err = g.Unmask(name); err == nil && rec.UnitFileState == systemd.GlobalStateEnabled {
			err = g.Enable(name)
		}
	case rec.UnitFileState == systemd.GlobalStateEnabled && cur == systemd.GlobalStateDisabled:
		err = g.Enable(name)
	case rec.UnitFileState == systemd.GlobalStateDisabled && cur == systemd.GlobalStateEnabled:
		err = g.Disable(name)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	s.userReload = true
	return nil
}

func (s *systemdSyncer) restoreUnit(name string, rec SystemdOriginal, reload *bool) error {
	if systemdunits.ValidateName(name) != nil || systemdunits.IsProtected(name) {
		return fmt.Errorf("refusing to touch %q", name)
	}
	st, err := s.mgr.Status(s.ctx, name)
	if err != nil {
		return err
	}
	if !st.Exists() {
		return nil
	}
	names := []string{name}
	wasMasked := rec.UnitFileState == "masked" || rec.UnitFileState == "masked-runtime"
	switch {
	case wasMasked && !st.IsMasked():
		if mErr := s.mgr.Mask(s.ctx, names); mErr != nil {
			return mErr
		}
		*reload = true
	case !wasMasked && st.IsMasked():
		if uErr := s.mgr.Unmask(s.ctx, names); uErr != nil {
			return uErr
		}
		*reload = true
		if rec.LinkTarget != "" {
			// Put back the redirect the mask replaced.
			link := filepath.Join(s.opts.dirs().systemDir(), name)
			if _, lErr := os.Lstat(link); errors.Is(lErr, os.ErrNotExist) {
				if sErr := os.Symlink(rec.LinkTarget, link); sErr != nil {
					log.Printf("Warning: systemd: restore link %s -> %s: %v", link, rec.LinkTarget, sErr)
				} else {
					log.Printf("systemd: restored link %s -> %s", link, rec.LinkTarget)
				}
			}
		}
		// The unit can only be enabled or started once systemd sees the
		// mask gone.
		if rErr := s.mgr.Reload(s.ctx); rErr != nil {
			return rErr
		}
		*reload = false
		if st, err = s.mgr.Status(s.ctx, name); err != nil {
			return err
		}
	}
	if !wasMasked {
		switch rec.UnitFileState {
		case "enabled":
			if st.UnitFileState != "enabled" && st.HasInstall() {
				if err := s.mgr.Enable(s.ctx, names); err != nil && !errors.Is(err, systemd.ErrNoInstall) {
					return err
				}
				*reload = true
			}
		case "disabled":
			if st.UnitFileState != "disabled" && st.HasInstall() {
				if err := s.mgr.Disable(s.ctx, names); err != nil {
					return err
				}
				*reload = true
			}
		}
	}
	if wasMasked {
		return nil
	}
	switch {
	case rec.Active && !st.IsActive() && !st.IsOneshot():
		if r, err := s.mgr.Start(s.ctx, name); err != nil {
			return err
		} else if !r.OK() {
			return fmt.Errorf("start: %s", r)
		}
	case !rec.Active && st.IsActive():
		if r, err := s.mgr.Stop(s.ctx, name); err != nil {
			return err
		} else if !r.OK() {
			return fmt.Errorf("stop: %s", r)
		}
	}
	return nil
}

// loadStatus queries a system unit and records its original state on
// first touch. key is the merged-state key.
func (s *systemdSyncer) loadStatus(key string) *systemd.UnitStatus {
	run := s.units[key]
	name := s.desired.Units[key].Name
	st, err := s.mgr.Status(s.ctx, name)
	if err != nil {
		run.queryErr = err.Error()
		return nil
	}
	run.status = st
	if !st.Exists() {
		run.notFound = true
		return nil
	}
	rec, ok := s.state.Units[key]
	if !ok {
		rec = SystemdOriginal{UnitFileState: st.UnitFileState, Active: st.IsActive(), RecordedAt: time.Now().UTC()}
	}
	// A symlink of the unit's name in /etc/systemd/system (not a mask) is
	// replaced by a mask; remember where it pointed. A link still present
	// under an older record has not been replaced yet, so record it too.
	if rec.LinkTarget == "" {
		if target, err := os.Readlink(filepath.Join(s.opts.dirs().systemDir(), name)); err == nil && target != os.DevNull {
			rec.LinkTarget = target
		}
	}
	s.state.Units[key] = rec
	return st
}

// loadGlobal reads a user-manager unit: its global unit-file state and
// its status in every reachable user manager. It records the original
// enablement on first touch and reports whether the unit exists.
func (s *systemdSyncer) loadGlobal(key string) bool {
	run := s.units[key]
	name := s.desired.Units[key].Name
	run.globalState = s.opts.globals().State(name)
	if run.globalState == systemd.GlobalStateNotFound {
		run.notFound = true
		return false
	}
	if _, ok := s.state.Units[key]; !ok {
		s.state.Units[key] = SystemdOriginal{UnitFileState: run.globalState, RecordedAt: time.Now().UTC()}
	}
	run.users = run.users[:0]
	for _, t := range s.userTargets() {
		ur := &systemdUserRun{sess: t.sess, mgr: t.mgr, err: t.err}
		if t.mgr != nil {
			if st, err := t.mgr.Status(s.ctx, name); err != nil {
				ur.err = err.Error()
			} else {
				ur.status = st
			}
		}
		run.users = append(run.users, ur)
	}
	return true
}

// applyEnablement enables, disables or masks every unit as desired, then
// reloads once when anything changed.
func (s *systemdSyncer) applyEnablement() {
	reload := false
	for _, key := range s.desired.Names() {
		u := s.desired.Units[key]
		run := s.units[key]
		if u.Global {
			if s.loadGlobal(key) {
				s.applyGlobalEnablement(key)
			}
			continue
		}
		name := u.Name
		st := s.loadStatus(key)
		if st == nil || u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
			continue
		}
		names := []string{name}
		found := st.UnitFileState
		switch u.Enablement {
		case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED, pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
			if st.IsMasked() {
				if err := s.mgr.Unmask(s.ctx, names); err != nil {
					run.errs = append(run.errs, "unmask: "+err.Error())
					continue
				}
				reload = true
				run.notes = append(run.notes, "unmasked")
				s.drift(key, "enablement", found, "unmasked")
				// Enable and Disable need the unmasked file visible.
				if err := s.mgr.Reload(s.ctx); err != nil {
					run.errs = append(run.errs, err.Error())
					continue
				}
				if st = s.loadStatus(key); st == nil {
					continue
				}
			}
			switch st.UnitFileState {
			case "static", "generated", "transient", "":
				run.errs = append(run.errs, fmt.Sprintf("unit is %s and has no [Install] section; manage its activity instead", st.UnitFileState))
				continue
			}
			if u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED {
				if systemdEnabledState(st.UnitFileState) {
					continue
				}
				if err := s.mgr.Enable(s.ctx, names); err != nil {
					run.errs = append(run.errs, "enable: "+err.Error())
					continue
				}
				run.notes = append(run.notes, "enabled")
				s.drift(key, "enablement", found, "enabled")
			} else {
				if st.UnitFileState == "disabled" {
					continue
				}
				if err := s.mgr.Disable(s.ctx, names); err != nil {
					run.errs = append(run.errs, "disable: "+err.Error())
					continue
				}
				run.notes = append(run.notes, "disabled")
				s.drift(key, "enablement", found, "disabled")
			}
			reload = true
		case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
			if st.IsActive() {
				if r, err := s.mgr.Stop(s.ctx, name); err != nil {
					run.errs = append(run.errs, "stop: "+err.Error())
					continue
				} else if !r.OK() {
					run.errs = append(run.errs, "stop: job "+string(r))
					continue
				}
				run.notes = append(run.notes, "stopped")
				s.drift(key, "activity", fmt.Sprintf("%s (%s)", st.ActiveState, st.SubState), "stopped")
				s.changed = true
			}
			if st.IsMasked() {
				continue
			}
			if err := s.mgr.Mask(s.ctx, names); err != nil {
				run.errs = append(run.errs, "mask: "+err.Error())
				continue
			}
			run.notes = append(run.notes, "masked")
			s.drift(key, "enablement", found, "masked")
			reload = true
		}
	}
	if reload {
		s.changed = true
		if err := s.mgr.Reload(s.ctx); err != nil {
			log.Printf("Warning: systemd: %v", err)
		}
	}
	s.reloadUsers()
}

// applyGlobalEnablement enables, disables or masks a user-manager unit on
// the file system, as "systemctl --global" would.
func (s *systemdSyncer) applyGlobalEnablement(key string) {
	u := s.desired.Units[key]
	run := s.units[key]
	g := s.opts.globals()
	name := u.Name
	changed := false
	switch u.Enablement {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED, pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		if run.globalState == systemd.GlobalStateMasked {
			if err := g.Unmask(name); err != nil {
				run.errs = append(run.errs, "unmask: "+err.Error())
				return
			}
			run.notes = append(run.notes, "unmasked")
			changed = true
			run.globalState = g.State(name)
		}
		if run.globalState == systemd.GlobalStateStatic {
			run.errs = append(run.errs, "unit is static and has no [Install] section; manage its activity instead")
			return
		}
		if u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED {
			if run.globalState != systemd.GlobalStateEnabled {
				if err := g.Enable(name); err != nil {
					run.errs = append(run.errs, "enable: "+err.Error())
					return
				}
				run.notes = append(run.notes, "enabled")
				changed = true
			}
		} else if run.globalState != systemd.GlobalStateDisabled {
			if err := g.Disable(name); err != nil {
				run.errs = append(run.errs, "disable: "+err.Error())
				return
			}
			run.notes = append(run.notes, "disabled")
			changed = true
		}
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		if run.globalState != systemd.GlobalStateMasked {
			if err := g.Mask(name); err != nil {
				run.errs = append(run.errs, "mask: "+err.Error())
				return
			}
			run.notes = append(run.notes, "masked")
			changed = true
		}
	default:
		return
	}
	if changed {
		s.changed = true
		s.userReload = true
		run.globalState = g.State(name)
	}
}

// systemdEnabledState reports whether a unit-file state counts as enabled.
func systemdEnabledState(state string) bool {
	switch state {
	case "enabled", "enabled-runtime", "alias", "indirect", "linked", "linked-runtime":
		return true
	}
	return false
}

// systemdActivityJob decides the job for a unit's live state: "start",
// "stop", "restart" or "" for none. corrective reports whether the job
// puts back a state that differed from the desired one (as opposed to a
// restart after an override change).
func systemdActivityJob(u *SystemdUnitDesired, st *systemd.UnitStatus, dropInChanged bool) (verb string, corrective bool) {
	if st.IsMasked() && u.Enablement == pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED {
		return "", false
	}
	restartWanted := dropInChanged && (st.Type() == "timer" || u.RestartOnChange)
	switch u.Activity {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		switch {
		case st.IsOneshot():
			if st.Result == "success" && !st.ExecMainStart.IsZero() && !dropInChanged {
				return "", false
			}
			return "start", !dropInChanged
		case st.IsActive() && restartWanted:
			return "restart", false
		case st.IsActive():
			return "", false
		default:
			return "start", true
		}
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		if st.IsActive() {
			return "stop", true
		}
	default:
		if st.IsActive() && restartWanted {
			return "restart", false
		}
	}
	return "", false
}

// applyActivity starts, stops or restarts every unit as desired.
func (s *systemdSyncer) applyActivity() {
	for _, key := range s.desired.Names() {
		u := s.desired.Units[key]
		run := s.units[key]
		if run.notFound || run.queryErr != "" {
			continue
		}
		if u.Global {
			s.applyGlobalActivity(key)
			continue
		}
		if run.status == nil || run.dropInChanged || len(run.notes) > 0 {
			if st := s.loadStatus(key); st == nil {
				continue
			}
		}
		st := run.status
		verb, corrective := systemdActivityJob(u, st, run.dropInChanged)
		if verb == "" {
			continue
		}
		if s.runJob(key, verb) && corrective {
			s.drift(key, "activity", fmt.Sprintf("%s (%s)", st.ActiveState, st.SubState), map[string]string{"start": "started", "stop": "stopped"}[verb])
		}
	}
}

// applyGlobalActivity starts, stops or restarts a user-manager unit in the
// manager of every logged-in user.
func (s *systemdSyncer) applyGlobalActivity(key string) {
	u := s.desired.Units[key]
	run := s.units[key]
	if u.Activity == pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED && !run.dropInChanged {
		return
	}
	for _, ur := range run.users {
		if ur.mgr == nil {
			continue
		}
		if ur.status == nil || run.dropInChanged || len(run.notes) > 0 {
			st, err := ur.mgr.Status(s.ctx, u.Name)
			if err != nil {
				ur.err = err.Error()
				continue
			}
			ur.status = st
		}
		if !ur.status.Exists() {
			continue
		}
		verb, _ := systemdActivityJob(u, ur.status, run.dropInChanged)
		if verb == "" {
			continue
		}
		s.runUserJob(run, ur, u.Name, verb)
	}
}

// runJob runs a start, stop or restart job on a system unit and reports
// whether it reached its goal.
func (s *systemdSyncer) runJob(key, verb string) bool {
	run := s.units[key]
	name := s.desired.Units[key].Name
	var (
		r   systemd.JobResult
		err error
	)
	switch verb {
	case "start":
		r, err = s.mgr.Start(s.ctx, name)
	case "stop":
		r, err = s.mgr.Stop(s.ctx, name)
	default:
		r, err = s.mgr.Restart(s.ctx, name)
	}
	s.changed = true
	if err != nil {
		run.errs = append(run.errs, verb+": "+err.Error())
		return false
	}
	log.Printf("systemd: %s %s: %s", verb, name, r)
	switch {
	case r == systemd.JobSkipped && verb == "start":
		run.startSkipped = true
		run.notes = append(run.notes, "start skipped: a condition of the unit is not met")
		return false
	case !r.OK():
		run.errs = append(run.errs, fmt.Sprintf("%s failed (job result %s)", verb, r))
		return false
	}
	run.notes = append(run.notes, map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[verb])
	return true
}

// runUserJob runs a job in one user's manager; outcomes are noted per user.
func (s *systemdSyncer) runUserJob(run *systemdUnitRun, ur *systemdUserRun, name, verb string) {
	var (
		r   systemd.JobResult
		err error
	)
	switch verb {
	case "start":
		r, err = ur.mgr.Start(s.ctx, name)
	case "stop":
		r, err = ur.mgr.Stop(s.ctx, name)
	default:
		r, err = ur.mgr.Restart(s.ctx, name)
	}
	s.changed = true
	who := ur.sess.String()
	if err != nil {
		ur.err = verb + ": " + err.Error()
		run.errs = append(run.errs, who+": "+ur.err)
		return
	}
	log.Printf("systemd: %s %s for %s: %s", verb, name, who, r)
	switch {
	case r == systemd.JobSkipped && verb == "start":
		run.notes = append(run.notes, who+": start skipped (a condition of the unit is not met)")
	case !r.OK():
		ur.err = fmt.Sprintf("%s failed (job result %s)", verb, r)
		run.errs = append(run.errs, who+": "+ur.err)
	default:
		run.notes = append(run.notes, map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[verb]+" for "+who)
	}
}

// refreshStatus re-reads every unit after the actions for the reports.
func (s *systemdSyncer) refreshStatus() {
	for _, key := range s.desired.Names() {
		u := s.desired.Units[key]
		run := s.units[key]
		if run.notFound || run.queryErr != "" {
			continue
		}
		if u.Global {
			run.globalState = s.opts.globals().State(u.Name)
			for _, ur := range run.users {
				if ur.mgr == nil {
					continue
				}
				if st, err := ur.mgr.Status(s.ctx, u.Name); err == nil {
					ur.status = st
				}
			}
			continue
		}
		if st, err := s.mgr.Status(s.ctx, u.Name); err == nil {
			run.status = st
		}
	}
}

// ── compliance ──────────────────────────────────────────────────────────

func systemdSourceLabel(src systemdSource) string {
	return fmt.Sprintf("%s (priority %d)", src.Name, src.Priority)
}

// compliance builds one policy's items against the merged state and the
// live unit states.
func (s *systemdSyncer) compliance(e SystemdEntry) SystemdPolicyResult {
	if e.Policy == nil {
		return SystemdPolicyResult{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "policy has no content"}
	}
	var items []*pb.ComplianceItemResult
	for _, u := range e.Policy.GetUnits() {
		items = append(items, s.unitItem(e, u))
	}
	for _, t := range e.Policy.GetTasks() {
		items = append(items, s.taskItem(e, t))
	}
	if e.Policy.GetWritePresets() {
		items = append(items, s.presetItem(false))
		if s.desired.hasEnablement(true) {
			items = append(items, s.presetItem(true))
		}
	}
	status, msg := rollupSystemdItems(items)
	return SystemdPolicyResult{Status: status, Message: msg, Items: items}
}

// hasEnablement reports whether any unit of the scope carries an
// enablement decision (what a preset file is made of).
func (d *SystemdDesired) hasEnablement(global bool) bool {
	for _, u := range d.Units {
		if u.Global == global && u.Enablement != pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
			return true
		}
	}
	return false
}

// unitItem builds the compliance item of one unit entry: the entry's
// axes that won the merge are checked against the live state (the system
// manager, or the global unit-file state and every logged-in user's
// manager for the user-manager scope).
func (s *systemdSyncer) unitItem(e SystemdEntry, u *pb.SystemdUnit) *pb.ComplianceItemResult {
	name := u.GetName()
	global := SystemdScopeIsGlobal(u.GetScope())
	key := SystemdUnitKey(global, name)
	item := &pb.ComplianceItemResult{SchemaId: systemdItemSchema, Key: key}
	set := func(st pb.ComplianceStatus, msg string) *pb.ComplianceItemResult {
		item.Status = st
		item.Message = msg
		return item
	}
	if err := systemdunits.ValidateName(name); err != nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error())
	}
	if systemdunits.IsProtected(name) || (global && systemdunits.IsProtectedGlobal(name)) {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "protected unit; nothing applied")
	}
	d := s.desired.Units[key]
	run := s.units[key]
	if d == nil || run == nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "unit missing from the merged state")
	}
	if run.queryErr != "" {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, run.queryErr)
	}
	if run.notFound {
		if global {
			return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no user unit of that name is installed on this node")
		}
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "unit is not installed on this node")
	}
	st := run.status
	if !global && st == nil {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, "unit state unknown")
	}

	// Which of this policy's axes won the merge.
	var overridden []string
	wins := 0
	checks := 0
	var problems []string

	if en := u.GetEnablement(); en != pb.SystemdEnablement_SYSTEMD_ENABLEMENT_UNSPECIFIED {
		checks++
		if d.EnablementBy.ID != e.ID && d.Enablement != en {
			overridden = append(overridden, "enablement by "+systemdSourceLabel(d.EnablementBy))
		} else {
			wins++
			switch {
			case global && !systemdGlobalEnablementSatisfied(en, run.globalState):
				problems = append(problems, fmt.Sprintf("expected %s, unit file is %s", systemdEnablementWord(en), run.globalState))
			case !global && !systemdEnablementSatisfied(en, st):
				problems = append(problems, fmt.Sprintf("expected %s, unit file is %s", systemdEnablementWord(en), st.UnitFileState))
			}
		}
	}
	if ac := u.GetActivity(); ac != pb.SystemdActivity_SYSTEMD_ACTIVITY_UNSPECIFIED {
		checks++
		if d.ActivityBy.ID != e.ID && d.Activity != ac {
			overridden = append(overridden, "activity by "+systemdSourceLabel(d.ActivityBy))
		} else {
			wins++
			if global {
				for _, ur := range run.users {
					if ur.status != nil && ur.status.Exists() && !systemdActivitySatisfied(ac, ur.status) {
						problems = append(problems, fmt.Sprintf("%s: expected %s, unit is %s (%s)", ur.sess, systemdActivityWord(ac), ur.status.ActiveState, ur.status.SubState))
					}
				}
			} else if !run.startSkipped && !systemdActivitySatisfied(ac, st) {
				problems = append(problems, fmt.Sprintf("expected %s, unit is %s (%s)", systemdActivityWord(ac), st.ActiveState, st.SubState))
			}
		}
	}
	if t := u.GetDropIn().GetTimer(); !systemdTimerEmpty(t) {
		checks++
		if d.TimerBy.ID != e.ID && !proto.Equal(t, d.Timer) {
			overridden = append(overridden, "timer settings by "+systemdSourceLabel(d.TimerBy))
		} else {
			wins++
			if msg := s.dropInProblem(key, d); msg != "" {
				problems = append(problems, msg)
			}
		}
	}
	if sv := u.GetDropIn().GetService(); !systemdServiceEmpty(sv) {
		checks++
		if lost := systemdServiceOverridden(sv, d, e.ID); len(lost) > 0 {
			overridden = append(overridden, "service settings "+strings.Join(lost, ", "))
		} else {
			wins++
			if msg := s.dropInProblem(key, d); msg != "" {
				problems = append(problems, msg)
			}
		}
	}
	if raw := u.GetDropIn().GetRaw(); len(raw) > 0 {
		checks++
		if lost := systemdRawOverridden(raw, d, e.ID); len(lost) > 0 {
			overridden = append(overridden, "raw directives "+strings.Join(lost, ", "))
		} else {
			wins++
			if msg := s.dropInProblem(key, d); msg != "" {
				problems = append(problems, msg)
			}
		}
	}
	if u.GetRequireLastRunSuccess() {
		checks++
		wins++
		if global {
			for _, ur := range run.users {
				if ur.status == nil || !ur.status.Exists() {
					continue
				}
				if msg := systemdLastRunProblem(s.ctx, ur.mgr, ur.status); msg != "" {
					problems = append(problems, ur.sess.String()+": "+msg)
				}
			}
		} else if msg := s.lastRunProblem(st); msg != "" {
			problems = append(problems, msg)
		}
	}

	var summary string
	if global {
		summary = systemdGlobalSummary(run)
	} else {
		summary = systemdStateSummary(st)
	}
	if checks > 0 && wins == 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "overridden: "+strings.Join(overridden, "; ")+"; now "+summary)
	}
	var notes []string
	notes = append(notes, run.notes...)
	if len(overridden) > 0 {
		notes = append(notes, "overridden: "+strings.Join(overridden, "; "))
	}
	if run.dropInErr != "" && (d.TimerBy.ID == e.ID || len(d.ServiceBy) > 0 || len(d.RawBy) > 0) {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, run.dropInErr+"; "+summary)
	}
	if len(run.errs) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(run.errs, "; ")+"; "+summary)
	}
	if !global && run.startSkipped && u.GetActivity() == pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING && !st.IsActive() {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "start skipped: a condition of the unit is not met; "+summary)
	}
	msg := summary
	if len(notes) > 0 {
		msg += "; " + strings.Join(notes, "; ")
	}
	if len(problems) > 0 {
		return set(pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(problems, "; ")+"; "+msg)
	}
	return set(pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, msg)
}

// systemdGlobalEnablementSatisfied compares a desired enablement with a
// global unit-file state.
func systemdGlobalEnablementSatisfied(e pb.SystemdEnablement, state string) bool {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return systemd.IsEnabledState(state)
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return state == systemd.GlobalStateDisabled
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return state == systemd.GlobalStateMasked
	}
	return true
}

// systemdGlobalSummary renders "enabled for all users; active (running)
// for alice, inactive (dead) for bob" for a user-manager unit.
func systemdGlobalSummary(run *systemdUnitRun) string {
	parts := []string{run.globalState + " for all users"}
	if len(run.users) == 0 {
		return parts[0] + "; no user session"
	}
	byState := map[string][]string{}
	var order []string
	for _, ur := range run.users {
		state := "unreachable"
		switch {
		case ur.status != nil && !ur.status.Exists():
			state = "not loaded"
		case ur.status != nil:
			state = fmt.Sprintf("%s (%s)", ur.status.ActiveState, ur.status.SubState)
		}
		if _, ok := byState[state]; !ok {
			order = append(order, state)
		}
		byState[state] = append(byState[state], ur.sess.String())
	}
	for _, state := range order {
		parts = append(parts, state+" for "+strings.Join(byState[state], ", "))
	}
	return strings.Join(parts, "; ")
}

func systemdEnablementWord(e pb.SystemdEnablement) string {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return "enabled"
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return "disabled"
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return "masked"
	}
	return "unchanged"
}

func systemdActivityWord(a pb.SystemdActivity) string {
	switch a {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		return "running"
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return "stopped"
	}
	return "unchanged"
}

func systemdEnablementSatisfied(e pb.SystemdEnablement, st *systemd.UnitStatus) bool {
	switch e {
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_ENABLED:
		return systemdEnabledState(st.UnitFileState)
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_DISABLED:
		return st.UnitFileState == "disabled"
	case pb.SystemdEnablement_SYSTEMD_ENABLEMENT_MASKED:
		return st.IsMasked()
	}
	return true
}

func systemdActivitySatisfied(a pb.SystemdActivity, st *systemd.UnitStatus) bool {
	switch a {
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_RUNNING:
		if st.IsOneshot() {
			return st.IsActive() || (st.Result == "success" && !st.ExecMainStart.IsZero())
		}
		return st.IsActive()
	case pb.SystemdActivity_SYSTEMD_ACTIVITY_STOPPED:
		return !st.IsActive()
	}
	return true
}

// systemdServiceOverridden lists the directives of a policy's service
// settings that a higher-priority policy won with a different value.
func systemdServiceOverridden(sv *pb.SystemdServiceSettings, d *SystemdUnitDesired, policyID string) []string {
	if d.Service == nil {
		return nil
	}
	var lost []string
	check := func(key, mine, merged string) {
		if mine == "" {
			return
		}
		if src, ok := d.ServiceBy[key]; ok && src.ID != policyID && merged != mine {
			lost = append(lost, key+" by "+systemdSourceLabel(src))
		}
	}
	checkBool := func(key string, mine, merged *bool) {
		if mine == nil {
			return
		}
		if src, ok := d.ServiceBy[key]; ok && src.ID != policyID && (merged == nil || *merged != *mine) {
			lost = append(lost, key+" by "+systemdSourceLabel(src))
		}
	}
	m := d.Service
	check("Restart", sv.GetRestart(), m.GetRestart())
	check("RestartSec", sv.GetRestartSec(), m.GetRestartSec())
	check("TimeoutStartSec", sv.GetTimeoutStartSec(), m.GetTimeoutStartSec())
	check("TimeoutStopSec", sv.GetTimeoutStopSec(), m.GetTimeoutStopSec())
	check("MemoryMax", sv.GetMemoryMax(), m.GetMemoryMax())
	check("CPUQuota", sv.GetCpuQuota(), m.GetCpuQuota())
	check("TasksMax", sv.GetTasksMax(), m.GetTasksMax())
	check("Nice", sv.GetNice(), m.GetNice())
	check("ProtectSystem", sv.GetProtectSystem(), m.GetProtectSystem())
	check("ProtectHome", sv.GetProtectHome(), m.GetProtectHome())
	checkBool("NoNewPrivileges", sv.NoNewPrivileges, m.NoNewPrivileges)
	checkBool("PrivateTmp", sv.PrivateTmp, m.PrivateTmp)
	merged := map[string]string{}
	for _, env := range m.GetEnvironment() {
		k, v, _ := strings.Cut(env, "=")
		merged[k] = v
	}
	for _, env := range sv.GetEnvironment() {
		k, v, _ := strings.Cut(env, "=")
		if src, ok := d.ServiceBy["Environment:"+k]; ok && src.ID != policyID && merged[k] != v {
			lost = append(lost, "Environment "+k+" by "+systemdSourceLabel(src))
		}
	}
	return lost
}

// systemdRawOverridden lists the raw directives of a policy that a
// higher-priority policy won with a different value.
func systemdRawOverridden(raw []*pb.SystemdDirective, d *SystemdUnitDesired, policyID string) []string {
	merged := map[string]*pb.SystemdDirective{}
	for _, m := range d.Raw {
		merged[m.GetSection()+"/"+m.GetKey()] = m
	}
	var lost []string
	for _, r := range raw {
		key := r.GetSection() + "/" + r.GetKey()
		src, ok := d.RawBy[key]
		if !ok || src.ID == policyID {
			continue
		}
		if m := merged[key]; m != nil && !proto.Equal(m, r) {
			lost = append(lost, key+" by "+systemdSourceLabel(src))
		}
	}
	return lost
}

// dropInProblem reports a drop-in that is missing or differs from the
// rendered content. key is the merged-state key.
func (s *systemdSyncer) dropInProblem(key string, d *SystemdUnitDesired) string {
	run := s.units[key]
	if run.dropInErr != "" {
		return ""
	}
	want := RenderSystemdDropIn(d)
	if want == nil {
		return ""
	}
	path := SystemdDropInPath(s.opts.dirs().UnitDir(d.Global), d.Name)
	cur, err := os.ReadFile(path) //nolint:gosec // G304: path built from a validated unit name under the configured directory
	if err != nil {
		return "drop-in " + path + " is missing"
	}
	if !bytes.Equal(cur, want) {
		return "drop-in " + path + " differs from the policy"
	}
	if run.status != nil && run.status.NeedDaemonReload {
		return "systemd has not reloaded the drop-in yet"
	}
	return ""
}

// lastRunProblem checks the last result of a timer's service or of a
// service in the system manager.
func (s *systemdSyncer) lastRunProblem(st *systemd.UnitStatus) string {
	return systemdLastRunProblem(s.ctx, s.mgr, st)
}

// systemdLastRunProblem checks the last result of a timer's service (read
// from mgr) or of a service.
func systemdLastRunProblem(ctx context.Context, mgr systemd.Manager, st *systemd.UnitStatus) string {
	switch st.Type() {
	case "timer":
		if st.LastTrigger.IsZero() || mgr == nil {
			return ""
		}
		target := st.TimerUnit
		if target == "" {
			target = strings.TrimSuffix(st.Name, ".timer") + ".service"
		}
		ts, err := mgr.Status(ctx, target)
		if err != nil || !ts.Exists() {
			return ""
		}
		if ts.Result != "success" {
			return fmt.Sprintf("last run of %s failed (%s, exit status %d)", target, ts.Result, ts.ExecMainStatus)
		}
	case "service":
		if st.Result != "success" && st.Result != "" {
			return fmt.Sprintf("last run failed (%s, exit status %d)", st.Result, st.ExecMainStatus)
		}
	}
	return ""
}

// systemdStateSummary renders "enabled, active (running)" plus timer
// details for the compliance message.
func systemdStateSummary(st *systemd.UnitStatus) string {
	state := st.UnitFileState
	if state == "" {
		state = st.LoadState
	}
	return state + ", " + systemdActivitySummary(st)
}

// systemdActivitySummary renders "active (running)" plus timer details,
// without the unit-file state (which is global for user-manager units).
func systemdActivitySummary(st *systemd.UnitStatus) string {
	parts := []string{fmt.Sprintf("%s (%s)", st.ActiveState, st.SubState)}
	if st.Type() == "timer" {
		if !st.NextElapse.IsZero() {
			parts = append(parts, "next "+st.NextElapse.Local().Format("Mon 2006-01-02 15:04"))
		}
		if !st.LastTrigger.IsZero() {
			last := "last " + st.LastTrigger.Local().Format("Mon 2006-01-02 15:04")
			if st.TimerResult != "" {
				last += " (" + st.TimerResult + ")"
			}
			parts = append(parts, last)
		}
	}
	return strings.Join(parts, ", ")
}

// rollupSystemdItems derives a policy status from its items: any error ->
// ERROR, any non-compliant -> NON_COMPLIANT, all inapplicable ->
// INAPPLICABLE, otherwise COMPLIANT.
func rollupSystemdItems(items []*pb.ComplianceItemResult) (status pb.ComplianceStatus, message string) {
	if len(items) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "policy lists no units"
	}
	allInapplicable := true
	var msgs []string
	var hasError, hasNonCompliant bool
	for _, it := range items {
		switch it.GetStatus() {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			allInapplicable = false
			hasError = true
			msgs = append(msgs, it.GetKey()+": "+it.GetMessage())
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			allInapplicable = false
			hasNonCompliant = true
			msgs = append(msgs, it.GetKey()+": "+it.GetMessage())
		default:
			allInapplicable = false
		}
	}
	switch {
	case allInapplicable:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no unit applies on this node (not installed or overridden)"
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"
	}
}
