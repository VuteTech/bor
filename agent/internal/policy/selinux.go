// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VuteTech/Bor/agent/internal/mac"
	"github.com/VuteTech/Bor/agent/internal/selinux"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/macrules"
)

// SelinuxModulesDir is the default directory of Bor's CIL module sources.
const SelinuxModulesDir = "/etc/bor/selinux"

// Names of the modules Bor renders from policy fields.
const (
	selinuxPermissiveModule   = "bor-permissive"
	selinuxFileContextsModule = "bor-filecontexts"
)

// Compliance item schema ids of the SELinux type.
const (
	selinuxItemActive     = "selinux:active"
	selinuxItemMode       = "selinux:mode"
	selinuxItemBoolean    = "selinux:boolean"
	selinuxItemPermissive = "selinux:permissive"
	selinuxItemModule     = "selinux:module"
	selinuxItemCustom     = "selinux:custom"
	selinuxItemFContext   = "selinux:fcontext"
	selinuxItemLogin      = "selinux:login"
)

// SelinuxEntry is one bound SELinux policy with its binding priority.
type SelinuxEntry struct {
	ID       string
	Name     string
	Priority int32
	Policy   *pb.SelinuxPolicy
}

type selinuxBoolDesired struct {
	Value    bool
	By       macSource
	Conflict *macSource
}

type selinuxModuleDesired struct {
	Enabled  bool
	By       macSource
	Conflict *macSource
}

type selinuxCustomDesired struct {
	CIL      string
	By       macSource
	Conflict *macSource
}

type selinuxFContextDesired struct {
	PathRegex  string
	Type       string
	Kind       pb.SelinuxFileKind
	Restorecon bool
	By         macSource
	Conflict   *macSource
}

type selinuxLoginDesired struct {
	Seuser   string
	Range    string
	By       macSource
	Conflict *macSource
}

// SelinuxDesired is the merged state of every bound SELinux policy.
type SelinuxDesired struct {
	Mode   pb.SelinuxMode
	ModeBy macSource
	// RequireActive and PrepareDisabled are set when any policy sets them.
	RequireActive   bool
	PrepareDisabled bool
	Booleans        map[string]*selinuxBoolDesired
	// Permissive maps a domain to the first policy that lists it (union).
	Permissive   map[string]macSource
	Modules      map[string]*selinuxModuleDesired
	Custom       map[string]*selinuxCustomDesired
	FileContexts map[string]*selinuxFContextDesired
	Logins       map[string]*selinuxLoginDesired
}

func selinuxModeRank(m pb.SelinuxMode) int {
	switch m {
	case pb.SelinuxMode_SELINUX_MODE_ENFORCING:
		return 2
	case pb.SelinuxMode_SELINUX_MODE_PERMISSIVE:
		return 1
	}
	return 0
}

func sortSelinuxEntries(entries []SelinuxEntry) []SelinuxEntry {
	sorted := append([]SelinuxEntry(nil), entries...)
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

func fcontextKey(regex string, kind pb.SelinuxFileKind) string {
	return fmt.Sprintf("%s\x00%d", regex, kind)
}

// MergeSelinuxPolicies merges bound policies by binding priority. The
// highest priority wins every key; on equal priority the stricter mode
// wins and other conflicting values are recorded, so compliance can
// report them as errors.
func MergeSelinuxPolicies(entries []SelinuxEntry) *SelinuxDesired {
	d := &SelinuxDesired{
		Booleans:     map[string]*selinuxBoolDesired{},
		Permissive:   map[string]macSource{},
		Modules:      map[string]*selinuxModuleDesired{},
		Custom:       map[string]*selinuxCustomDesired{},
		FileContexts: map[string]*selinuxFContextDesired{},
		Logins:       map[string]*selinuxLoginDesired{},
	}
	for _, e := range sortSelinuxEntries(entries) {
		p := e.Policy
		if p == nil {
			continue
		}
		src := macSource{ID: e.ID, Name: e.Name, Priority: e.Priority}
		if m := p.GetMode(); m != pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED {
			switch {
			case d.Mode == pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED:
				d.Mode, d.ModeBy = m, src
			case d.ModeBy.Priority == src.Priority && selinuxModeRank(m) > selinuxModeRank(d.Mode):
				d.Mode, d.ModeBy = m, src
			}
		}
		d.RequireActive = d.RequireActive || p.GetRequireActive()
		d.PrepareDisabled = d.PrepareDisabled || p.GetPrepareDisabledNodes()
		for _, b := range p.GetBooleans() {
			cur, ok := d.Booleans[b.GetName()]
			switch {
			case !ok:
				d.Booleans[b.GetName()] = &selinuxBoolDesired{Value: b.GetValue(), By: src}
			case cur.By.Priority == src.Priority && cur.Value != b.GetValue() && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
		for _, dom := range p.GetPermissiveDomains() {
			if _, ok := d.Permissive[dom]; !ok {
				d.Permissive[dom] = src
			}
		}
		for _, m := range p.GetModules() {
			cur, ok := d.Modules[m.GetName()]
			switch {
			case !ok:
				d.Modules[m.GetName()] = &selinuxModuleDesired{Enabled: m.GetEnabled(), By: src}
			case cur.By.Priority == src.Priority && cur.Enabled != m.GetEnabled() && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
		for _, c := range p.GetCustomModules() {
			cur, ok := d.Custom[c.GetName()]
			switch {
			case !ok:
				d.Custom[c.GetName()] = &selinuxCustomDesired{CIL: c.GetCil(), By: src}
			case cur.By.Priority == src.Priority && cur.CIL != c.GetCil() && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
		for _, fc := range p.GetFileContexts() {
			key := fcontextKey(fc.GetPathRegex(), fc.GetKind())
			cur, ok := d.FileContexts[key]
			switch {
			case !ok:
				d.FileContexts[key] = &selinuxFContextDesired{PathRegex: fc.GetPathRegex(), Type: fc.GetType(), Kind: fc.GetKind(), Restorecon: fc.GetRestorecon(), By: src}
			case cur.By.Priority == src.Priority && cur.Type != fc.GetType() && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
		for _, l := range p.GetLogins() {
			cur, ok := d.Logins[l.GetLogin()]
			switch {
			case !ok:
				d.Logins[l.GetLogin()] = &selinuxLoginDesired{Seuser: l.GetSeuser(), Range: l.GetRange(), By: src}
			case cur.By.Priority == src.Priority && (cur.Seuser != l.GetSeuser() || cur.Range != l.GetRange()) && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
	}
	return d
}

// ─── rendering ───────────────────────────────────────────────────────────────

var selinuxFileKindWords = map[pb.SelinuxFileKind]string{
	pb.SelinuxFileKind_SELINUX_FILE_KIND_UNSPECIFIED: "any",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_FILE:        "file",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_DIR:         "dir",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_CHAR:        "char",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_BLOCK:       "block",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_SOCKET:      "socket",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_PIPE:        "pipe",
	pb.SelinuxFileKind_SELINUX_FILE_KIND_SYMLINK:     "symlink",
}

// RenderPermissiveCIL renders the module that makes the listed domains
// permissive.
func RenderPermissiveCIL(domains []string) []byte {
	var b strings.Builder
	b.WriteString("; " + macHeader + "\n; Domains that run permissive while the rest of the system is enforcing.\n")
	sorted := append([]string(nil), domains...)
	sort.Strings(sorted)
	for _, d := range sorted {
		fmt.Fprintf(&b, "(typepermissive %s)\n", d)
	}
	return []byte(b.String())
}

// RenderFileContextsCIL renders the module that labels the policy's paths.
func RenderFileContextsCIL(fcs []*selinuxFContextDesired) []byte {
	var b strings.Builder
	b.WriteString("; " + macHeader + "\n; File context rules; restorecon applies them to existing files.\n")
	sorted := append([]*selinuxFContextDesired(nil), fcs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].PathRegex != sorted[j].PathRegex {
			return sorted[i].PathRegex < sorted[j].PathRegex
		}
		return sorted[i].Kind < sorted[j].Kind
	})
	for _, fc := range sorted {
		kind := selinuxFileKindWords[fc.Kind]
		if kind == "" {
			kind = "any"
		}
		fmt.Fprintf(&b, "(filecon %q %s (system_u object_r %s ((s0) (s0))))\n", fc.PathRegex, kind, fc.Type)
	}
	return []byte(b.String())
}

// RenderCustomCIL wraps a custom module's source with the Bor header.
func RenderCustomCIL(name, cil string) []byte {
	if !strings.HasSuffix(cil, "\n") {
		cil += "\n"
	}
	return []byte("; " + macHeader + "\n; Bor module " + macrules.ModuleName(name) + "\n" + cil)
}

// selinuxModuleSources returns the CIL files to install, by module name.
func selinuxModuleSources(d *SelinuxDesired) map[string][]byte {
	out := make(map[string][]byte)
	if len(d.Permissive) > 0 {
		out[selinuxPermissiveModule] = RenderPermissiveCIL(sortedKeys(d.Permissive))
	}
	if len(d.FileContexts) > 0 {
		fcs := make([]*selinuxFContextDesired, 0, len(d.FileContexts))
		for _, fc := range d.FileContexts {
			if fc.Conflict == nil {
				fcs = append(fcs, fc)
			}
		}
		if len(fcs) > 0 {
			out[selinuxFileContextsModule] = RenderFileContextsCIL(fcs)
		}
	}
	for name, c := range d.Custom {
		if c.Conflict == nil {
			out[macrules.ModuleName(name)] = RenderCustomCIL(name, c.CIL)
		}
	}
	return out
}

// selinuxFileContextPrefixes returns the paths restorecon walks for the
// merged file contexts.
func selinuxFileContextPrefixes(d *SelinuxDesired) []string {
	var out []string
	for _, fc := range d.FileContexts {
		if fc.Conflict != nil || !fc.Restorecon {
			continue
		}
		if prefix := macrules.LiteralPrefix(fc.PathRegex); prefix != "" && prefix != "/" {
			out = append(out, prefix)
		}
	}
	sort.Strings(out)
	return out
}

// ListBorSelinuxSources returns Bor's CIL source files in the modules
// directory.
func ListBorSelinuxSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), macrules.BorPrefix) && strings.HasSuffix(e.Name(), ".cil") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// ─── sync ────────────────────────────────────────────────────────────────────

// SelinuxDeps are the node interfaces a sync uses.
type SelinuxDeps struct {
	Paths selinux.Paths
	Tools *selinux.Tools
	Mac   mac.Paths
}

// SelinuxSyncOptions are the agent settings a sync honours.
type SelinuxSyncOptions struct {
	ModulesDir         string
	ModulePriority     int
	StatePath          string
	AllowModeChange    bool
	AllowCustomModules bool
	// Reconcile marks a pass that re-asserts an unchanged desired state;
	// its corrections are drift.
	Reconcile bool
}

// SelinuxPolicyResult is the compliance of one policy after a sync.
type SelinuxPolicyResult struct {
	Status  pb.ComplianceStatus
	Message string
	Items   []*pb.ComplianceItemResult
}

// SelinuxSyncResult is the outcome of a sync.
type SelinuxSyncResult struct {
	Policies map[string]SelinuxPolicyResult
	Drifts   []MacDrift
	// Changed reports whether anything on the node changed.
	Changed bool
	// Active reports whether SELinux is active on the node.
	Active bool
}

type selinuxSyncer struct {
	ctx     context.Context
	deps    *SelinuxDeps
	opts    SelinuxSyncOptions
	desired *SelinuxDesired
	state   *SelinuxState
	result  *SelinuxSyncResult

	enforcing  bool
	configMode string
	booleans   map[string]selinux.BoolState
	modules    map[string]selinux.Module
	logins     map[string]selinux.Login
	loginsErr  string
	semanageOK bool

	modeErr      string
	boolErr      string
	moduleErr    string
	moduleAbsent map[string]bool
	installed    map[string]bool
	installErr   string
	loginErr     map[string]string
}

func (s *selinuxSyncer) drift(resource, description string) {
	if s.opts.Reconcile {
		s.result.Drifts = append(s.result.Drifts, MacDrift{Resource: resource, Description: description})
	}
}

// SyncSelinux applies the merged policies, restores what left every
// policy, and computes per-policy compliance.
func SyncSelinux(ctx context.Context, deps *SelinuxDeps, entries []SelinuxEntry, opts SelinuxSyncOptions) SelinuxSyncResult {
	result := SelinuxSyncResult{Policies: make(map[string]SelinuxPolicyResult, len(entries))}
	state, err := LoadSelinuxState(opts.StatePath)
	if err != nil {
		state = newSelinuxState()
	}
	s := &selinuxSyncer{ctx: ctx, deps: deps, opts: opts, desired: MergeSelinuxPolicies(entries), state: state, result: &result,
		moduleAbsent: map[string]bool{}, installed: map[string]bool{}, loginErr: map[string]string{}}
	result.Active = deps.Paths.Active()

	if !result.Active {
		s.inactive(entries)
		_ = SaveSelinuxState(opts.StatePath, state)
		return result
	}
	s.readState()
	if len(entries) == 0 {
		s.restoreAll()
		_ = SaveSelinuxState(opts.StatePath, state)
		return result
	}
	s.applyMode()
	s.applyBooleans()
	s.applyModules()
	s.applyLogins()
	_ = SaveSelinuxState(opts.StatePath, state)
	for _, e := range entries {
		result.Policies[e.ID] = s.compliance(e)
	}
	return result
}

// inactive handles a node where SELinux is not active: nothing can be
// applied, a policy can only ask for the node to be prepared.
func (s *selinuxSyncer) inactive(entries []SelinuxEntry) {
	prepared := ""
	if s.desired.PrepareDisabled && s.desired.Mode != pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED {
		changed, err := s.deps.Paths.WriteConfigMode("permissive")
		if err != nil {
			prepared = "could not prepare the node: " + err.Error()
		} else {
			if rerr := s.deps.Paths.RequestRelabel(); rerr != nil {
				prepared = "config set to permissive, but the relabel could not be requested: " + rerr.Error()
			} else {
				prepared = "prepared: /etc/selinux/config set to permissive and a relabel requested; reboot the node, then the policy switches it to " + strings.ToLower(strings.TrimPrefix(s.desired.Mode.String(), "SELINUX_MODE_"))
				s.state.RelabelRequested = true
				if changed {
					s.result.Changed = true
				}
			}
		}
	}
	lsms := strings.Join(mac.Detect(s.deps.Mac).LSMs, ",")
	for _, e := range entries {
		var items []*pb.ComplianceItemResult
		msg := "SELinux is not active on this node"
		if lsms != "" {
			msg += " (LSM list: " + lsms + ")"
		}
		status := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
		if e.Policy.GetRequireActive() {
			status = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
			msg += "; set SELINUX=enforcing in /etc/selinux/config, run fixfiles -F onboot and reboot"
		}
		if prepared != "" && e.Policy.GetPrepareDisabledNodes() {
			msg += "; " + prepared
		}
		items = append(items, macItem(selinuxItemActive, "active", status, msg))
		st, m := rollupMacItems(items, msg, msg)
		s.result.Policies[e.ID] = SelinuxPolicyResult{Status: st, Message: m, Items: items}
	}
}

func (s *selinuxSyncer) readState() {
	s.enforcing, _ = s.deps.Paths.Enforcing()
	cfg, _ := s.deps.Paths.ReadConfig()
	s.configMode = cfg.Mode
	s.booleans, _ = s.deps.Paths.Booleans()
	if s.booleans == nil {
		s.booleans = map[string]selinux.BoolState{}
	}
	s.modules = map[string]selinux.Module{}
	if mods, err := s.deps.Tools.ListModules(s.ctx); err == nil {
		for _, m := range mods {
			// Keep the highest priority entry of a name: that is the active one.
			if cur, ok := s.modules[m.Name]; !ok || m.Priority > cur.Priority {
				s.modules[m.Name] = m
			}
		}
	} else {
		s.moduleErr = err.Error()
	}
	s.semanageOK = s.deps.Tools.Present(s.deps.Tools.SemanageBin)
}

// restoreAll undoes every recorded change (no policy applies any more).
func (s *selinuxSyncer) restoreAll() {
	st := s.state
	if st.Mode != nil {
		s.setMode(st.Mode.ConfigMode, st.Mode.Enforcing)
		st.Mode = nil
	}
	if len(st.Booleans) > 0 {
		values := map[string]bool{}
		for name, v := range st.Booleans {
			if _, exists := s.booleans[name]; exists {
				values[name] = v
			}
		}
		if err := s.deps.Tools.SetBooleans(s.ctx, values); err == nil {
			st.Booleans = map[string]bool{}
			s.result.Changed = true
		}
	}
	ops := &selinux.ModuleOps{Priority: s.opts.ModulePriority}
	for name, enabled := range st.Modules {
		if m, ok := s.modules[name]; ok && m.Enabled != enabled {
			if enabled {
				ops.Enable = append(ops.Enable, name)
			} else {
				ops.Disable = append(ops.Disable, name)
			}
		}
	}
	for name, m := range s.modules {
		if strings.HasPrefix(name, macrules.BorPrefix) && m.Priority == s.opts.ModulePriority {
			ops.Remove = append(ops.Remove, name)
		}
	}
	sort.Strings(ops.Remove)
	if !ops.Empty() {
		if err := s.deps.Tools.ApplyModules(s.ctx, ops); err == nil {
			st.Modules = map[string]bool{}
			st.Installed = map[string]string{}
			s.result.Changed = true
			if len(st.FileContextPrefixes) > 0 {
				_ = s.deps.Tools.Restorecon(s.ctx, st.FileContextPrefixes)
				st.FileContextPrefixes = nil
			}
		}
	} else {
		st.Modules = map[string]bool{}
		st.Installed = map[string]string{}
	}
	if files, err := ListBorSelinuxSources(s.opts.ModulesDir); err == nil {
		for _, f := range files {
			_ = os.Remove(f)
		}
	}
	if len(st.Logins) > 0 && s.semanageOK {
		for login, orig := range st.Logins {
			var err error
			if orig.Existed {
				err = s.deps.Tools.SetLogin(s.ctx, login, orig.Seuser, orig.Range, true)
			} else {
				err = s.deps.Tools.DeleteLogin(s.ctx, login)
			}
			if err == nil {
				delete(st.Logins, login)
				s.result.Changed = true
			}
		}
	}
	st.RelabelRequested = false
}

// setMode writes the config and runtime mode.
func (s *selinuxSyncer) setMode(configMode string, enforcing bool) {
	if configMode != "" && configMode != s.configMode {
		if changed, err := s.deps.Paths.WriteConfigMode(configMode); err != nil {
			s.modeErr = err.Error()
		} else if changed {
			s.configMode = configMode
			s.result.Changed = true
		}
	}
	if enforcing != s.enforcing {
		if err := s.deps.Paths.SetEnforcing(enforcing); err != nil {
			s.modeErr = err.Error()
		} else {
			s.enforcing = enforcing
			s.result.Changed = true
		}
	}
}

func (s *selinuxSyncer) applyMode() {
	st := s.state
	if s.desired.Mode == pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED || !s.opts.AllowModeChange {
		if st.Mode != nil {
			s.setMode(st.Mode.ConfigMode, st.Mode.Enforcing)
			st.Mode = nil
		}
		return
	}
	want := s.desired.Mode == pb.SelinuxMode_SELINUX_MODE_ENFORCING
	wantWord := "permissive"
	if want {
		wantWord = "enforcing"
	}
	if st.Mode == nil && (s.enforcing != want || s.configMode != wantWord) {
		st.Mode = &SelinuxModeOriginal{ConfigMode: s.configMode, Enforcing: s.enforcing, RecordedAt: time.Now()}
	}
	if s.enforcing != want {
		found := "permissive"
		if s.enforcing {
			found = "enforcing"
		}
		s.drift("selinux:mode", fmt.Sprintf("found %s, expected %s; set %s", found, wantWord, wantWord))
	}
	s.setMode(wantWord, want)
}

func (s *selinuxSyncer) applyBooleans() {
	st := s.state
	values := map[string]bool{}
	for name, b := range s.desired.Booleans {
		if b.Conflict != nil {
			continue
		}
		cur, exists := s.booleans[name]
		if !exists {
			continue
		}
		if cur.Current == b.Value && cur.Pending == b.Value {
			continue
		}
		if _, recorded := st.Booleans[name]; !recorded {
			st.Booleans[name] = cur.Pending
		}
		if cur.Current != b.Value {
			s.drift("selinux:boolean:"+name, fmt.Sprintf("found %s, expected %s; set %s", onOff(cur.Current), onOff(b.Value), onOff(b.Value)))
		}
		values[name] = b.Value
	}
	// Booleans that left every policy return to their recorded value.
	for name, orig := range st.Booleans {
		if d, ok := s.desired.Booleans[name]; ok && d.Conflict == nil {
			continue
		}
		if cur, exists := s.booleans[name]; exists && (cur.Current != orig || cur.Pending != orig) {
			values[name] = orig
		}
		delete(st.Booleans, name)
	}
	if len(values) == 0 {
		return
	}
	if err := s.deps.Tools.SetBooleans(s.ctx, values); err != nil {
		s.boolErr = err.Error()
		return
	}
	s.result.Changed = true
	for name, v := range values {
		s.booleans[name] = selinux.BoolState{Current: v, Pending: v}
	}
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func (s *selinuxSyncer) applyModules() {
	st := s.state
	ops := &selinux.ModuleOps{Priority: s.opts.ModulePriority}
	// Enabled flags of existing modules.
	for name, d := range s.desired.Modules {
		if d.Conflict != nil {
			continue
		}
		m, ok := s.modules[name]
		if !ok {
			s.moduleAbsent[name] = true
			continue
		}
		if m.Enabled == d.Enabled {
			continue
		}
		if _, recorded := st.Modules[name]; !recorded {
			st.Modules[name] = m.Enabled
		}
		s.drift("selinux:module:"+name, fmt.Sprintf("found %s, expected %s", enabledWord(m.Enabled), enabledWord(d.Enabled)))
		if d.Enabled {
			ops.Enable = append(ops.Enable, name)
		} else {
			ops.Disable = append(ops.Disable, name)
		}
	}
	for name, orig := range st.Modules {
		if d, ok := s.desired.Modules[name]; ok && d.Conflict == nil {
			continue
		}
		if m, ok := s.modules[name]; ok && m.Enabled != orig {
			if orig {
				ops.Enable = append(ops.Enable, name)
			} else {
				ops.Disable = append(ops.Disable, name)
			}
		}
		delete(st.Modules, name)
	}

	// Bor modules: render, compare, install or remove.
	sources := map[string][]byte{}
	if s.opts.AllowCustomModules {
		sources = selinuxModuleSources(s.desired)
	}
	if err := os.MkdirAll(s.opts.ModulesDir, 0o750); err != nil {
		s.installErr = err.Error()
	}
	wantFiles := map[string]bool{}
	for _, name := range sortedKeys(sources) {
		data := sources[name]
		path := filepath.Join(s.opts.ModulesDir, name+".cil")
		wantFiles[path] = true
		hash := macHash(data)
		existing, _ := os.ReadFile(path) //nolint:gosec // G304: path under the modules directory
		if !bytes.Equal(existing, data) {
			if err := WriteFileAtomically(path, data); err != nil {
				s.installErr = err.Error()
				continue
			}
			s.result.Changed = true
		}
		m, inStore := s.modules[name]
		if inStore && m.Priority == s.opts.ModulePriority && st.Installed[name] == hash {
			s.installed[name] = true
			continue
		}
		if inStore && st.Installed[name] != hash && st.Installed[name] != "" {
			s.drift("selinux:custom:"+name, "module source changed; reinstalled")
		}
		ops.Install = append(ops.Install, path)
	}
	for name, m := range s.modules {
		if strings.HasPrefix(name, macrules.BorPrefix) && m.Priority == s.opts.ModulePriority {
			if _, wanted := sources[name]; !wanted {
				ops.Remove = append(ops.Remove, name)
			}
		}
	}
	if files, err := ListBorSelinuxSources(s.opts.ModulesDir); err == nil {
		for _, f := range files {
			if !wantFiles[f] {
				_ = os.Remove(f)
			}
		}
	}
	sort.Strings(ops.Install)
	sort.Strings(ops.Remove)
	sort.Strings(ops.Enable)
	sort.Strings(ops.Disable)

	prefixes := selinuxFileContextPrefixes(s.desired)
	relabel := append([]string(nil), st.FileContextPrefixes...)
	if !ops.Empty() {
		if err := s.deps.Tools.ApplyModules(s.ctx, ops); err != nil {
			s.installErr = err.Error()
			s.moduleErr = err.Error()
			return
		}
		s.result.Changed = true
		for _, path := range ops.Install {
			name := strings.TrimSuffix(filepath.Base(path), ".cil")
			st.Installed[name] = macHash(sources[name])
			s.installed[name] = true
			s.modules[name] = selinux.Module{Name: name, Priority: s.opts.ModulePriority, Enabled: true, Lang: "cil"}
		}
		for _, name := range ops.Remove {
			delete(st.Installed, name)
			delete(s.modules, name)
		}
		for _, name := range ops.Enable {
			m := s.modules[name]
			m.Enabled = true
			s.modules[name] = m
		}
		for _, name := range ops.Disable {
			m := s.modules[name]
			m.Enabled = false
			s.modules[name] = m
		}
		relabel = append(relabel, prefixes...)
	}
	for name := range st.Installed {
		if _, ok := sources[name]; !ok {
			delete(st.Installed, name)
		}
	}
	if len(relabel) > 0 {
		sort.Strings(relabel)
		relabel = uniqueStrings(relabel)
		if err := s.deps.Tools.Restorecon(s.ctx, relabel); err != nil {
			s.installErr = err.Error()
		}
	}
	st.FileContextPrefixes = prefixes
}

func enabledWord(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

func uniqueStrings(in []string) []string {
	out := in[:0]
	for i, v := range in {
		if i == 0 || v != in[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func (s *selinuxSyncer) applyLogins() {
	st := s.state
	if len(s.desired.Logins) == 0 && len(st.Logins) == 0 {
		return
	}
	if !s.semanageOK {
		return
	}
	logins, err := s.deps.Tools.ListLogins(s.ctx)
	if err != nil {
		s.loginsErr = err.Error()
		return
	}
	s.logins = logins
	for login, d := range s.desired.Logins {
		if d.Conflict != nil {
			continue
		}
		cur, exists := logins[login]
		if exists && cur.Seuser == d.Seuser && (d.Range == "" || cur.Range == d.Range) {
			continue
		}
		if _, recorded := st.Logins[login]; !recorded {
			st.Logins[login] = SelinuxLoginOriginal{Existed: exists, Seuser: cur.Seuser, Range: cur.Range}
		}
		if exists {
			s.drift("selinux:login:"+login, fmt.Sprintf("found %s, expected %s", cur.Seuser, d.Seuser))
		}
		if err := s.deps.Tools.SetLogin(s.ctx, login, d.Seuser, d.Range, exists); err != nil {
			s.loginErr[login] = err.Error()
			continue
		}
		s.logins[login] = selinux.Login{Seuser: d.Seuser, Range: d.Range}
		s.result.Changed = true
	}
	for login, orig := range st.Logins {
		if d, ok := s.desired.Logins[login]; ok && d.Conflict == nil {
			continue
		}
		var err error
		if orig.Existed {
			err = s.deps.Tools.SetLogin(s.ctx, login, orig.Seuser, orig.Range, true)
		} else {
			err = s.deps.Tools.DeleteLogin(s.ctx, login)
		}
		if err == nil {
			delete(st.Logins, login)
			s.result.Changed = true
		}
	}
}

// ─── compliance ──────────────────────────────────────────────────────────────

func (s *selinuxSyncer) compliance(e SelinuxEntry) SelinuxPolicyResult {
	p := e.Policy
	if p == nil {
		return SelinuxPolicyResult{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "policy has no content"}
	}
	var items []*pb.ComplianceItemResult
	ok := pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
	bad := pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
	errSt := pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
	na := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE

	if p.GetRequireActive() {
		items = append(items, macItem(selinuxItemActive, "active", ok, "SELinux is active"))
	}
	if m := p.GetMode(); m != pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED {
		want := m == pb.SelinuxMode_SELINUX_MODE_ENFORCING
		wantWord := strings.ToLower(strings.TrimPrefix(m.String(), "SELINUX_MODE_"))
		switch {
		case !s.opts.AllowModeChange:
			items = append(items, macItem(selinuxItemMode, "mode", na, "mode changes are disabled in the agent configuration (selinux.allow_mode_change)"))
		case s.desired.ModeBy.ID != e.ID && s.desired.Mode != m:
			items = append(items, macItem(selinuxItemMode, "mode", na, "overridden by policy "+s.desired.ModeBy.label()))
		case s.modeErr != "":
			items = append(items, macItem(selinuxItemMode, "mode", errSt, s.modeErr))
		case s.enforcing != want:
			items = append(items, macItem(selinuxItemMode, "mode", bad, fmt.Sprintf("expected %s, runtime is %s", wantWord, modeWord(s.enforcing))))
		case s.configMode != wantWord:
			items = append(items, macItem(selinuxItemMode, "mode", bad, fmt.Sprintf("expected %s, /etc/selinux/config says %q", wantWord, s.configMode)))
		default:
			items = append(items, macItem(selinuxItemMode, "mode", ok, wantWord))
		}
	}
	for _, b := range p.GetBooleans() {
		name := b.GetName()
		d := s.desired.Booleans[name]
		switch {
		case d == nil:
			items = append(items, macItem(selinuxItemBoolean, name, errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(selinuxItemBoolean, name, errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && d.Value != b.GetValue():
			items = append(items, macItem(selinuxItemBoolean, name, na, "overridden by policy "+d.By.label()))
		default:
			cur, exists := s.booleans[name]
			switch {
			case !exists:
				items = append(items, macItem(selinuxItemBoolean, name, na, "boolean does not exist in this node's policy"))
			case cur.Current != b.GetValue():
				msg := fmt.Sprintf("expected %s, found %s", onOff(b.GetValue()), onOff(cur.Current))
				if s.boolErr != "" {
					items = append(items, macItem(selinuxItemBoolean, name, errSt, msg+": "+s.boolErr))
				} else {
					items = append(items, macItem(selinuxItemBoolean, name, bad, msg))
				}
			default:
				items = append(items, macItem(selinuxItemBoolean, name, ok, onOff(b.GetValue())))
			}
		}
	}
	for _, dom := range p.GetPermissiveDomains() {
		switch {
		case !s.opts.AllowCustomModules:
			items = append(items, macItem(selinuxItemPermissive, dom, na, "custom modules are disabled in the agent configuration (selinux.allow_custom_modules)"))
		case s.installed[selinuxPermissiveModule]:
			items = append(items, macItem(selinuxItemPermissive, dom, ok, "permissive"))
		default:
			items = append(items, macItem(selinuxItemPermissive, dom, errSt, s.errOr(s.installErr, "module not installed")))
		}
	}
	for _, m := range p.GetModules() {
		name := m.GetName()
		d := s.desired.Modules[name]
		switch {
		case d == nil:
			items = append(items, macItem(selinuxItemModule, name, errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(selinuxItemModule, name, errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && d.Enabled != m.GetEnabled():
			items = append(items, macItem(selinuxItemModule, name, na, "overridden by policy "+d.By.label()))
		case s.moduleAbsent[name]:
			items = append(items, macItem(selinuxItemModule, name, na, "module is not installed on this node"))
		default:
			cur, exists := s.modules[name]
			switch {
			case !exists && s.moduleErr != "":
				items = append(items, macItem(selinuxItemModule, name, errSt, s.moduleErr))
			case !exists:
				items = append(items, macItem(selinuxItemModule, name, na, "module is not installed on this node"))
			case cur.Enabled != m.GetEnabled():
				items = append(items, macItem(selinuxItemModule, name, s.badOrErr(s.moduleErr), s.errOr(s.moduleErr, fmt.Sprintf("expected %s, found %s", enabledWord(m.GetEnabled()), enabledWord(cur.Enabled)))))
			default:
				items = append(items, macItem(selinuxItemModule, name, ok, enabledWord(m.GetEnabled())))
			}
		}
	}
	for _, c := range p.GetCustomModules() {
		name := c.GetName()
		modName := macrules.ModuleName(name)
		d := s.desired.Custom[name]
		switch {
		case !s.opts.AllowCustomModules:
			items = append(items, macItem(selinuxItemCustom, modName, na, "custom modules are disabled in the agent configuration (selinux.allow_custom_modules)"))
		case d == nil:
			items = append(items, macItem(selinuxItemCustom, modName, errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(selinuxItemCustom, modName, errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && d.CIL != c.GetCil():
			items = append(items, macItem(selinuxItemCustom, modName, na, "overridden by policy "+d.By.label()))
		case s.installed[modName]:
			items = append(items, macItem(selinuxItemCustom, modName, ok, "installed"))
		default:
			items = append(items, macItem(selinuxItemCustom, modName, errSt, s.errOr(s.installErr, "module not installed")))
		}
	}
	for _, fc := range p.GetFileContexts() {
		key := fcontextKey(fc.GetPathRegex(), fc.GetKind())
		d := s.desired.FileContexts[key]
		switch {
		case !s.opts.AllowCustomModules:
			items = append(items, macItem(selinuxItemFContext, fc.GetPathRegex(), na, "custom modules are disabled in the agent configuration (selinux.allow_custom_modules)"))
		case d == nil:
			items = append(items, macItem(selinuxItemFContext, fc.GetPathRegex(), errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(selinuxItemFContext, fc.GetPathRegex(), errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && d.Type != fc.GetType():
			items = append(items, macItem(selinuxItemFContext, fc.GetPathRegex(), na, "overridden by policy "+d.By.label()))
		case s.installed[selinuxFileContextsModule]:
			items = append(items, macItem(selinuxItemFContext, fc.GetPathRegex(), ok, fc.GetType()))
		default:
			items = append(items, macItem(selinuxItemFContext, fc.GetPathRegex(), errSt, s.errOr(s.installErr, "module not installed")))
		}
	}
	for _, l := range p.GetLogins() {
		login := l.GetLogin()
		d := s.desired.Logins[login]
		switch {
		case !s.semanageOK:
			items = append(items, macItem(selinuxItemLogin, login, na, "semanage is not installed on this node (policycoreutils-python-utils)"))
		case s.loginsErr != "":
			items = append(items, macItem(selinuxItemLogin, login, errSt, s.loginsErr))
		case d == nil:
			items = append(items, macItem(selinuxItemLogin, login, errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(selinuxItemLogin, login, errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && (d.Seuser != l.GetSeuser() || d.Range != l.GetRange()):
			items = append(items, macItem(selinuxItemLogin, login, na, "overridden by policy "+d.By.label()))
		case s.loginErr[login] != "":
			items = append(items, macItem(selinuxItemLogin, login, errSt, s.loginErr[login]))
		default:
			cur, exists := s.logins[login]
			if exists && cur.Seuser == l.GetSeuser() && (l.GetRange() == "" || cur.Range == l.GetRange()) {
				items = append(items, macItem(selinuxItemLogin, login, ok, l.GetSeuser()))
			} else {
				items = append(items, macItem(selinuxItemLogin, login, bad, fmt.Sprintf("expected %s, found %s", l.GetSeuser(), cur.Seuser)))
			}
		}
	}
	if p.GetPrepareDisabledNodes() && !p.GetRequireActive() && p.GetMode() == pb.SelinuxMode_SELINUX_MODE_UNSPECIFIED && len(items) == 0 {
		items = append(items, macItem(selinuxItemActive, "active", ok, "SELinux is active"))
	}
	status, msg := rollupMacItems(items, "policy sets nothing that applies here", "nothing applies on this node")
	return SelinuxPolicyResult{Status: status, Message: msg, Items: items}
}

func (s *selinuxSyncer) errOr(err, fallback string) string {
	if err != "" {
		return err
	}
	return fallback
}

func (s *selinuxSyncer) badOrErr(err string) pb.ComplianceStatus {
	if err != "" {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
	}
	return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
}

func modeWord(enforcing bool) string {
	if enforcing {
		return "enforcing"
	}
	return "permissive"
}

// ─── inventory ───────────────────────────────────────────────────────────────

// SelinuxInventory is what the node reports about SELinux.
type SelinuxInventory struct {
	Status   *pb.SelinuxStatus
	Booleans []*pb.SelinuxBooleanInfo
	Modules  []*pb.SelinuxModuleInfo
}

// ScanSelinuxInventory reads the status, booleans and modules of the node.
func ScanSelinuxInventory(ctx context.Context, deps *SelinuxDeps) *SelinuxInventory {
	p := deps.Paths
	inv := &SelinuxInventory{Status: &pb.SelinuxStatus{}}
	st := inv.Status
	st.Active = p.Active()
	cfg, _ := p.ReadConfig()
	st.ConfigMode = cfg.Mode
	st.PolicyType = cfg.Type
	st.StoreRoot = p.StoreRoot()
	st.BootFlags = mac.ReadBootFlags(deps.Mac)
	st.RelabelPending = p.RelabelPending()
	st.SemanagePresent = deps.Tools.Present(deps.Tools.SemanageBin)
	st.Audit2AllowPresent = deps.Tools.Present(deps.Tools.Audit2allowBin)
	st.SetroubleshootPresent = selinux.ToolPresent("sealert")
	if !st.Active {
		st.Mode = "disabled"
		return inv
	}
	enforcing, _ := p.Enforcing()
	st.Mode = modeWord(enforcing)
	st.PolicyVersion = int32(min(p.PolicyVersion(), 1<<30)) //nolint:gosec // G115: bounded
	st.PolicyloadLocked = p.PolicyloadLocked()
	if bools, err := p.Booleans(); err == nil {
		for _, name := range sortedKeysBool(bools) {
			b := bools[name]
			inv.Booleans = append(inv.Booleans, &pb.SelinuxBooleanInfo{Name: name, Current: b.Current, Persistent: b.Pending})
		}
		st.BooleanCount = int32(min(len(inv.Booleans), 1<<30)) //nolint:gosec // G115: bounded
	}
	if mods, err := deps.Tools.ListModules(ctx); err == nil {
		for _, m := range mods {
			inv.Modules = append(inv.Modules, &pb.SelinuxModuleInfo{Name: m.Name, Priority: int32(min(m.Priority, 999)), Enabled: m.Enabled, Lang: m.Lang}) //nolint:gosec // G115: bounded
		}
		st.ModuleCount = int32(min(len(inv.Modules), 1<<30)) //nolint:gosec // G115: bounded
	}
	return inv
}

func sortedKeysBool(m map[string]selinux.BoolState) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
