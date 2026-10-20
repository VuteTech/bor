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

	"github.com/VuteTech/Bor/agent/internal/apparmor"
	"github.com/VuteTech/Bor/agent/internal/mac"
	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/macrules"
)

// Default locations of the AppArmor type.
const (
	ApparmorSysctlPath = "/etc/sysctl.d/zz-bor-apparmor.conf"
	ApparmorBackupDir  = "/var/lib/bor/agent/apparmor-backups"
	apparmorService    = "apparmor.service"
)

// Compliance item schema ids of the AppArmor type.
const (
	apparmorItemEnabled    = "apparmor:enabled"
	apparmorItemService    = "apparmor:service"
	apparmorItemUserns     = "apparmor:userns"
	apparmorItemUnconfined = "apparmor:unconfined"
	apparmorItemProfile    = "apparmor:profile"
	apparmorItemCustom     = "apparmor:custom"
	apparmorItemLocal      = "apparmor:local"
	apparmorItemHomeDirs   = "apparmor:homedirs"
)

// ApparmorEntry is one bound AppArmor policy with its binding priority.
type ApparmorEntry struct {
	ID       string
	Name     string
	Priority int32
	Policy   *pb.ApparmorPolicy
}

type apparmorModeRule struct {
	Match     string
	Mode      pb.ApparmorMode
	EditFlags bool
	By        macSource
	exact     bool
	spec      int
}

type apparmorTextDesired struct {
	Text     string
	By       macSource
	Conflict *macSource
}

// ApparmorDesired is the merged state of every bound AppArmor policy.
type ApparmorDesired struct {
	RequireEnabled bool
	EnsureService  bool
	Userns         pb.ApparmorSwitch
	UsernsBy       macSource
	Unconfined     pb.ApparmorSwitch
	UnconfinedBy   macSource
	Rules          []apparmorModeRule
	Custom         map[string]*apparmorTextDesired
	Overrides      map[string]*apparmorTextDesired
	HomeDirs       []string
}

func apparmorModeRank(m pb.ApparmorMode) int {
	switch m {
	case pb.ApparmorMode_APPARMOR_MODE_ENFORCE:
		return 3
	case pb.ApparmorMode_APPARMOR_MODE_COMPLAIN:
		return 2
	case pb.ApparmorMode_APPARMOR_MODE_DISABLED:
		return 1
	}
	return 0
}

func apparmorSwitchRank(s pb.ApparmorSwitch) int {
	switch s {
	case pb.ApparmorSwitch_APPARMOR_SWITCH_ON:
		return 2
	case pb.ApparmorSwitch_APPARMOR_SWITCH_OFF:
		return 1
	}
	return 0
}

func sortApparmorEntries(entries []ApparmorEntry) []ApparmorEntry {
	sorted := append([]ApparmorEntry(nil), entries...)
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

// MergeApparmorPolicies merges bound policies by binding priority. Modes
// are resolved per profile file (exact name over glob, then priority,
// specificity and strictness); texts conflict on equal priority.
func MergeApparmorPolicies(entries []ApparmorEntry) *ApparmorDesired {
	d := &ApparmorDesired{Custom: map[string]*apparmorTextDesired{}, Overrides: map[string]*apparmorTextDesired{}}
	homes := map[string]struct{}{}
	for _, e := range sortApparmorEntries(entries) {
		p := e.Policy
		if p == nil {
			continue
		}
		src := macSource{ID: e.ID, Name: e.Name, Priority: e.Priority}
		d.RequireEnabled = d.RequireEnabled || p.GetRequireEnabled()
		d.EnsureService = d.EnsureService || p.GetEnsureService()
		mergeSwitch := func(cur *pb.ApparmorSwitch, curBy *macSource, v pb.ApparmorSwitch) {
			if v == pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED {
				return
			}
			switch {
			case *cur == pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED:
				*cur, *curBy = v, src
			case curBy.Priority == src.Priority && apparmorSwitchRank(v) > apparmorSwitchRank(*cur):
				*cur, *curBy = v, src
			}
		}
		mergeSwitch(&d.Userns, &d.UsernsBy, p.GetRestrictUnprivilegedUserns())
		mergeSwitch(&d.Unconfined, &d.UnconfinedBy, p.GetRestrictUnprivilegedUnconfined())
		for _, pm := range p.GetProfiles() {
			if pm.GetMode() == pb.ApparmorMode_APPARMOR_MODE_UNSPECIFIED || macrules.ValidateProfileMatch(pm.GetMatch()) != nil {
				continue
			}
			d.Rules = append(d.Rules, apparmorModeRule{Match: pm.GetMatch(), Mode: pm.GetMode(), EditFlags: pm.GetEditVendorFlags(), By: src, exact: !macrules.IsGlob(pm.GetMatch()), spec: macrules.GlobSpecificity(pm.GetMatch())})
		}
		for _, c := range p.GetCustomProfiles() {
			cur, ok := d.Custom[c.GetName()]
			switch {
			case !ok:
				d.Custom[c.GetName()] = &apparmorTextDesired{Text: c.GetText(), By: src}
			case cur.By.Priority == src.Priority && cur.Text != c.GetText() && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
		for _, o := range p.GetLocalOverrides() {
			cur, ok := d.Overrides[o.GetProfileFile()]
			switch {
			case !ok:
				d.Overrides[o.GetProfileFile()] = &apparmorTextDesired{Text: o.GetText(), By: src}
			case cur.By.Priority == src.Priority && cur.Text != o.GetText() && cur.Conflict == nil:
				s := src
				cur.Conflict = &s
			}
		}
		for _, h := range p.GetExtraHomeDirs() {
			homes[h] = struct{}{}
		}
	}
	d.HomeDirs = sortedKeys(homes)
	return d
}

// ResolveMode returns the rule that decides a profile file's mode, or nil.
func (d *ApparmorDesired) ResolveMode(file string) *apparmorModeRule {
	var best *apparmorModeRule
	for i := range d.Rules {
		r := &d.Rules[i]
		if !macrules.MatchProfileFile(r.Match, file) {
			continue
		}
		if best == nil || apparmorRuleBetter(r, best) {
			best = r
		}
	}
	return best
}

func apparmorRuleBetter(a, b *apparmorModeRule) bool {
	if a.exact != b.exact {
		return a.exact
	}
	if a.By.Priority != b.By.Priority {
		return a.By.Priority > b.By.Priority
	}
	if a.spec != b.spec {
		return a.spec > b.spec
	}
	return apparmorModeRank(a.Mode) > apparmorModeRank(b.Mode)
}

// ─── rendering ───────────────────────────────────────────────────────────────

// RenderApparmorSysctl renders the sysctl.d file for the two switches.
func RenderApparmorSysctl(userns, unconfined pb.ApparmorSwitch) []byte {
	var b strings.Builder
	b.WriteString("# " + macHeader + "\n# AppArmor restriction of unprivileged user namespaces.\n")
	write := func(name string, v pb.ApparmorSwitch) {
		switch v {
		case pb.ApparmorSwitch_APPARMOR_SWITCH_ON:
			fmt.Fprintf(&b, "kernel.%s = 1\n", name)
		case pb.ApparmorSwitch_APPARMOR_SWITCH_OFF:
			fmt.Fprintf(&b, "kernel.%s = 0\n", name)
		}
	}
	write(apparmor.SysctlRestrictUserns, userns)
	write(apparmor.SysctlRestrictUnconfined, unconfined)
	return []byte(b.String())
}

// RenderApparmorHomeTunable renders tunables/home.d/bor.
func RenderApparmorHomeTunable(dirs []string) []byte {
	var b strings.Builder
	b.WriteString("# " + macHeader + "\n# Extra home directory roots for @{HOME}.\n")
	sorted := append([]string(nil), dirs...)
	sort.Strings(sorted)
	if len(sorted) > 0 {
		b.WriteString("@{HOMEDIRS}+=" + strings.Join(sorted, " ") + "\n")
	}
	return []byte(b.String())
}

// RenderApparmorProfile wraps a custom profile's text with the Bor header.
func RenderApparmorProfile(text string) []byte {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte("# " + macHeader + "\n" + text)
}

// ─── sync ────────────────────────────────────────────────────────────────────

// ApparmorDeps are the node interfaces a sync uses. Systemd may be nil.
type ApparmorDeps struct {
	Paths   apparmor.Paths
	Parser  *apparmor.Parser
	Systemd systemd.Manager
	Mac     mac.Paths
}

// ApparmorSyncOptions are the agent settings a sync honours.
type ApparmorSyncOptions struct {
	SysctlPath           string
	StatePath            string
	BackupDir            string
	AllowCustomProfiles  bool
	AllowVendorFlagEdits bool
	Reconcile            bool
}

// ApparmorPolicyResult is the compliance of one policy after a sync.
type ApparmorPolicyResult struct {
	Status  pb.ComplianceStatus
	Message string
	Items   []*pb.ComplianceItemResult
}

// ApparmorSyncResult is the outcome of a sync.
type ApparmorSyncResult struct {
	Policies map[string]ApparmorPolicyResult
	Drifts   []MacDrift
	Changed  bool
	Enabled  bool
}

type apparmorFileResult struct {
	status pb.ComplianceStatus
	msg    string
	by     macSource
	mode   pb.ApparmorMode
}

type apparmorSyncer struct {
	ctx     context.Context
	deps    *ApparmorDeps
	opts    ApparmorSyncOptions
	desired *ApparmorDesired
	state   *ApparmorState
	result  *ApparmorSyncResult

	enabled    bool
	parserOK   bool
	files      map[string]apparmor.ProfileFile
	loaded     map[string]string
	fullReload bool

	serviceMsg  string
	serviceOK   bool
	sysctlMsg   map[string]string
	sysctlOK    map[string]bool
	homeMsg     string
	homeOK      bool
	customMsg   map[string]string
	overrideMsg map[string]string
	overrideOK  map[string]bool
	modeResults map[string]apparmorFileResult
	reloadMsg   string
}

func (s *apparmorSyncer) drift(resource, description string) {
	if s.opts.Reconcile {
		s.result.Drifts = append(s.result.Drifts, MacDrift{Resource: resource, Description: description})
	}
}

func (s *apparmorSyncer) changed() { s.result.Changed = true }

// SyncApparmor applies the merged policies, restores what left every
// policy, and computes per-policy compliance.
func SyncApparmor(ctx context.Context, deps *ApparmorDeps, entries []ApparmorEntry, opts ApparmorSyncOptions) ApparmorSyncResult {
	result := ApparmorSyncResult{Policies: make(map[string]ApparmorPolicyResult, len(entries))}
	state, err := LoadApparmorState(opts.StatePath)
	if err != nil {
		state = newApparmorState()
	}
	s := &apparmorSyncer{ctx: ctx, deps: deps, opts: opts, desired: MergeApparmorPolicies(entries), state: state, result: &result,
		sysctlMsg: map[string]string{}, sysctlOK: map[string]bool{}, customMsg: map[string]string{}, overrideMsg: map[string]string{},
		overrideOK: map[string]bool{}, modeResults: map[string]apparmorFileResult{}}
	s.enabled = deps.Paths.Enabled()
	s.parserOK = deps.Parser != nil && deps.Parser.Available()
	result.Enabled = s.enabled
	s.files = map[string]apparmor.ProfileFile{}
	if files, err := apparmor.ScanProfiles(deps.Paths.ProfilesDir); err == nil {
		for _, f := range files {
			s.files[f.File] = f
		}
	}
	s.loaded = map[string]string{}
	if s.enabled {
		if loaded, err := deps.Paths.LoadedProfiles(); err == nil {
			s.loaded = loaded
		}
	}
	if len(entries) == 0 {
		s.restoreAll()
		s.finishReload()
		_ = SaveApparmorState(opts.StatePath, state)
		return result
	}
	s.ensureService()
	s.applySysctls()
	s.applyHomeDirs()
	s.applyCustoms()
	s.applyOverrides()
	s.applyModes()
	s.finishReload()
	_ = SaveApparmorState(opts.StatePath, state)
	for _, e := range entries {
		result.Policies[e.ID] = s.compliance(e)
	}
	return result
}

func (s *apparmorSyncer) canLoad() bool { return s.enabled && s.parserOK }

func (s *apparmorSyncer) finishReload() {
	if s.fullReload && s.canLoad() {
		if err := s.deps.Parser.ReplaceDir(s.ctx); err != nil {
			s.reloadMsg = err.Error()
		}
	}
}

// reload loads one file in the mode its links decide; errors are returned
// for the caller's item.
func (s *apparmorSyncer) reload(file string, complain bool) error {
	if !s.canLoad() {
		return nil
	}
	return s.deps.Parser.Replace(s.ctx, s.deps.Paths.ProfilePath(file), complain)
}

func (s *apparmorSyncer) unload(file string) error {
	if !s.canLoad() {
		return nil
	}
	return s.deps.Parser.Remove(s.ctx, s.deps.Paths.ProfilePath(file))
}

// isLoaded reports whether any profile of a file is loaded and its mode.
func (s *apparmorSyncer) isLoaded(file string) (string, bool) {
	pf, ok := s.files[file]
	if !ok || len(pf.Names) == 0 {
		return "", false
	}
	mode, loaded := s.loaded[pf.Names[0]]
	return mode, loaded
}

func (s *apparmorSyncer) restoreAll() {
	st := s.state
	for name := range st.Customs {
		file := macrules.ProfileFileName(name)
		_ = s.unload(file)
		_ = os.Remove(s.deps.Paths.ProfilePath(file))
		delete(st.Customs, name)
		s.changed()
	}
	for file, orig := range st.Overrides {
		if changed, err := apparmor.RemoveManagedBlock(s.deps.Paths.LocalPath(file), orig.Created); err == nil && changed {
			_ = s.reload(file, false)
			s.changed()
		}
		delete(st.Overrides, file)
	}
	if st.HomeDirs {
		if err := os.Remove(s.deps.Paths.HomeTunablePath()); err == nil {
			s.fullReload = true
			s.changed()
		}
		st.HomeDirs = false
	}
	for file, backup := range st.FlagEdits {
		if data, err := os.ReadFile(backup); err == nil { //nolint:gosec // G304: path recorded by this agent
			if err := apparmor.WriteFile(s.deps.Paths.ProfilePath(file), data); err == nil {
				_ = os.Remove(backup)
				_ = s.reload(file, false)
				s.changed()
			}
		}
		delete(st.FlagEdits, file)
	}
	for file, orig := range st.Links {
		cd, _ := s.deps.Paths.SetLink(apparmor.DisableDir, file, orig.Disabled)
		cc, _ := s.deps.Paths.SetLink(apparmor.ForceComplainDir, file, orig.Complain)
		if cd || cc {
			s.changed()
			if _, exists := s.files[file]; exists {
				if orig.Disabled {
					_ = s.unload(file)
				} else {
					_ = s.reload(file, orig.Complain)
				}
			}
		}
		delete(st.Links, file)
	}
	for name, orig := range st.Sysctls {
		if cur, ok := s.deps.Paths.Sysctl(name); ok && cur != orig {
			if err := s.deps.Paths.SetSysctl(name, orig); err == nil {
				s.changed()
			}
		}
		delete(st.Sysctls, name)
	}
	if st.SysctlFile {
		if err := os.Remove(s.opts.SysctlPath); err == nil {
			s.changed()
		}
		st.SysctlFile = false
	}
}

func (s *apparmorSyncer) ensureService() {
	if !s.desired.EnsureService || !s.enabled || s.deps.Systemd == nil {
		return
	}
	st, err := s.deps.Systemd.Status(s.ctx, apparmorService)
	if err != nil {
		s.serviceMsg = err.Error()
		return
	}
	if !st.Exists() {
		s.serviceMsg = "apparmor.service is not installed"
		return
	}
	if !st.IsActive() {
		if err := s.deps.Systemd.Enable(s.ctx, []string{apparmorService}); err != nil {
			s.serviceMsg = "enable: " + err.Error()
			return
		}
		if r, err := s.deps.Systemd.Start(s.ctx, apparmorService); err != nil || !r.OK() {
			s.serviceMsg = fmt.Sprintf("start: %v %s", err, r)
			return
		}
		s.drift("apparmor:service", "found inactive, expected running; started")
		s.changed()
	}
	s.serviceOK = true
}

func (s *apparmorSyncer) applySysctls() {
	st := s.state
	want := map[string]pb.ApparmorSwitch{apparmor.SysctlRestrictUserns: s.desired.Userns, apparmor.SysctlRestrictUnconfined: s.desired.Unconfined}
	wantAny := false
	for name, sw := range want {
		if sw == pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED {
			if orig, ok := st.Sysctls[name]; ok {
				if cur, present := s.deps.Paths.Sysctl(name); present && cur != orig {
					if err := s.deps.Paths.SetSysctl(name, orig); err == nil {
						s.changed()
					}
				}
				delete(st.Sysctls, name)
			}
			continue
		}
		wantAny = true
		cur, present := s.deps.Paths.Sysctl(name)
		if !present {
			s.sysctlMsg[name] = "this kernel has no " + name + " switch"
			continue
		}
		v := 0
		if sw == pb.ApparmorSwitch_APPARMOR_SWITCH_ON {
			v = 1
		}
		if cur != v {
			if _, recorded := st.Sysctls[name]; !recorded {
				st.Sysctls[name] = cur
			}
			s.drift("apparmor:sysctl:"+name, fmt.Sprintf("found %d, expected %d; set %d", cur, v, v))
			if err := s.deps.Paths.SetSysctl(name, v); err != nil {
				s.sysctlMsg[name] = err.Error()
				continue
			}
			s.changed()
		}
		s.sysctlOK[name] = true
	}
	if wantAny {
		data := RenderApparmorSysctl(s.desired.Userns, s.desired.Unconfined)
		existing, _ := os.ReadFile(s.opts.SysctlPath) //nolint:gosec // G304: path from agent configuration
		if !bytes.Equal(existing, data) {
			if err := WriteFileAtomically(s.opts.SysctlPath, data); err != nil {
				for name := range want {
					if want[name] != pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED && s.sysctlMsg[name] == "" {
						s.sysctlMsg[name] = "sysctl file: " + err.Error()
					}
				}
			} else {
				s.changed()
			}
		}
		st.SysctlFile = true
	} else if st.SysctlFile {
		if err := os.Remove(s.opts.SysctlPath); err == nil || errors.Is(err, os.ErrNotExist) {
			st.SysctlFile = false
			s.changed()
		}
	}
}

func (s *apparmorSyncer) applyHomeDirs() {
	st := s.state
	path := s.deps.Paths.HomeTunablePath()
	if len(s.desired.HomeDirs) == 0 {
		if st.HomeDirs {
			if err := os.Remove(path); err == nil {
				s.fullReload = true
				s.changed()
			}
			st.HomeDirs = false
		}
		return
	}
	data := RenderApparmorHomeTunable(s.desired.HomeDirs)
	existing, _ := os.ReadFile(path) //nolint:gosec // G304: path under the profile directory
	if !bytes.Equal(existing, data) {
		if err := apparmor.WriteFile(path, data); err != nil {
			s.homeMsg = err.Error()
			return
		}
		s.fullReload = true
		s.changed()
	}
	st.HomeDirs = true
	s.homeOK = true
}

// attachmentOwners maps a literal attachment to the files that use it.
func (s *apparmorSyncer) attachmentOwners() map[string][]string {
	owners := map[string][]string{}
	for file, pf := range s.files {
		for _, a := range pf.Attachments {
			if !strings.ContainsAny(a, "*?[{") {
				owners[a] = append(owners[a], file)
			}
		}
	}
	return owners
}

func (s *apparmorSyncer) applyCustoms() {
	st := s.state
	owners := s.attachmentOwners()
	want := map[string]bool{}
	if s.opts.AllowCustomProfiles {
		for name, c := range s.desired.Custom {
			if c.Conflict != nil {
				continue
			}
			file := macrules.ProfileFileName(name)
			want[file] = true
			data := RenderApparmorProfile(c.Text)
			hash := macHash(data)
			parsed := apparmor.ParseProfileText(c.Text)
			conflict := ""
			for _, a := range parsed.Attachments {
				for _, other := range owners[a] {
					if other != file {
						conflict = fmt.Sprintf("attachment %s is already used by profile file %s; add a local override to that profile instead", a, other)
					}
				}
			}
			if conflict != "" {
				s.customMsg[name] = conflict
				if _, installed := st.Customs[name]; installed {
					_ = s.unload(file)
					_ = os.Remove(s.deps.Paths.ProfilePath(file))
					delete(st.Customs, name)
					s.changed()
				}
				continue
			}
			path := s.deps.Paths.ProfilePath(file)
			existing, _ := os.ReadFile(path) //nolint:gosec // G304: path under the profile directory
			_, loadedNow := s.loaded[macrules.ProfileFileName(name)]
			if bytes.Equal(existing, data) && st.Customs[name] == hash && (!s.canLoad() || loadedNow) {
				continue
			}
			if !bytes.Equal(existing, data) && st.Customs[name] != "" && len(existing) > 0 {
				s.drift("apparmor:custom:"+file, "profile file changed; rewritten")
			}
			tmp := filepath.Join(s.deps.Paths.ProfilesDir, "."+file+".bor-new")
			if err := apparmor.WriteFile(tmp, data); err != nil {
				s.customMsg[name] = err.Error()
				continue
			}
			if s.parserOK {
				if err := s.deps.Parser.Check(s.ctx, tmp); err != nil {
					_ = os.Remove(tmp)
					s.customMsg[name] = err.Error()
					continue
				}
			}
			if err := os.Rename(tmp, path); err != nil {
				_ = os.Remove(tmp)
				s.customMsg[name] = err.Error()
				continue
			}
			s.changed()
			if err := s.reload(file, false); err != nil {
				s.customMsg[name] = err.Error()
				continue
			}
			st.Customs[name] = hash
			s.loaded[macrules.ProfileFileName(name)] = "enforce"
		}
	}
	// Bor profile files no policy wants any more (or custom profiles
	// switched off on this node).
	for file := range s.files {
		if !strings.HasPrefix(file, macrules.BorPrefix) || want[file] {
			continue
		}
		_ = s.unload(file)
		if err := os.Remove(s.deps.Paths.ProfilePath(file)); err == nil {
			s.changed()
		}
		delete(st.Customs, strings.TrimPrefix(file, macrules.BorPrefix))
		delete(s.files, file)
	}
	for name := range st.Customs {
		if !want[macrules.ProfileFileName(name)] {
			delete(st.Customs, name)
		}
	}
}

func (s *apparmorSyncer) applyOverrides() {
	st := s.state
	want := map[string]bool{}
	if s.opts.AllowCustomProfiles {
		for file, o := range s.desired.Overrides {
			if o.Conflict != nil {
				continue
			}
			pf, exists := s.files[file]
			switch {
			case !exists:
				s.overrideMsg[file] = "vendor profile is not installed on this node"
				continue
			case pf.LocalInclude == "":
				s.overrideMsg[file] = "the vendor profile does not include a local/ file; use a custom profile"
				continue
			}
			want[file] = true
			local := s.deps.Paths.LocalPath(pf.LocalInclude)
			hash := macHash([]byte(o.Text))
			changed, created, err := apparmor.WriteManagedBlock(local, o.Text)
			if err != nil {
				s.overrideMsg[file] = err.Error()
				continue
			}
			orig, recorded := st.Overrides[file]
			if !recorded {
				orig = ApparmorOverrideOriginal{Created: created}
			}
			if changed && recorded && orig.Hash != "" {
				s.drift("apparmor:local:"+file, "local override changed; rewritten")
			}
			if changed {
				s.changed()
				if s.parserOK {
					if err := s.deps.Parser.Check(s.ctx, s.deps.Paths.ProfilePath(file)); err != nil {
						_, _ = apparmor.RemoveManagedBlock(local, orig.Created)
						s.overrideMsg[file] = err.Error()
						if !recorded {
							continue
						}
						delete(st.Overrides, file)
						continue
					}
				}
				if err := s.reload(file, false); err != nil {
					s.overrideMsg[file] = err.Error()
				}
			}
			orig.Hash = hash
			st.Overrides[file] = orig
			if s.overrideMsg[file] == "" {
				s.overrideOK[file] = true
			}
		}
	}
	for file, orig := range st.Overrides {
		if want[file] {
			continue
		}
		pf := s.files[file]
		local := s.deps.Paths.LocalPath(file)
		if pf.LocalInclude != "" {
			local = s.deps.Paths.LocalPath(pf.LocalInclude)
		}
		if changed, err := apparmor.RemoveManagedBlock(local, orig.Created); err == nil && changed {
			s.changed()
			_ = s.reload(file, false)
		}
		delete(st.Overrides, file)
	}
}

func (s *apparmorSyncer) applyModes() {
	st := s.state
	for _, file := range sortedKeys(s.files) {
		pf := s.files[file]
		if strings.HasPrefix(file, macrules.BorPrefix) || macrules.IsDynamicManaged(macrules.ManagedBy(firstName(&pf), file)) {
			continue
		}
		rule := s.desired.ResolveMode(file)
		if rule == nil {
			s.releaseFile(file)
			continue
		}
		fileMode := pf.FileMode()
		res := apparmorFileResult{by: rule.By, mode: rule.Mode}
		if !rule.exact && fileMode != "" && fileMode != "complain" {
			res.status = pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
			res.msg = "profile is " + fileMode + " by design; a pattern does not change it"
			s.modeResults[file] = res
			continue
		}
		_, wasRecorded := st.Links[file]
		if !wasRecorded {
			d, c := s.deps.Paths.LinkState(file)
			st.Links[file] = ApparmorLinkOriginal{Disabled: d, Complain: c}
		}
		wantDisable := rule.Mode == pb.ApparmorMode_APPARMOR_MODE_DISABLED
		wantComplain := rule.Mode == pb.ApparmorMode_APPARMOR_MODE_COMPLAIN
		beforeD, beforeC := s.deps.Paths.LinkState(file)
		changedD, errD := s.deps.Paths.SetLink(apparmor.DisableDir, file, wantDisable)
		changedC, errC := s.deps.Paths.SetLink(apparmor.ForceComplainDir, file, wantComplain)
		if errD != nil || errC != nil {
			res.status = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
			res.msg = fmt.Sprintf("links: %v %v", errD, errC)
			s.modeResults[file] = res
			continue
		}
		if changedD || changedC {
			s.changed()
			// A file Bor already managed whose links differ from what the
			// last sync left was changed by hand.
			if wasRecorded {
				s.drift("apparmor:profile:"+file, fmt.Sprintf("links changed by hand (disable=%v, complain=%v); restored", beforeD, beforeC))
			}
		}
		edited := false
		if rule.Mode == pb.ApparmorMode_APPARMOR_MODE_ENFORCE && fileMode == "complain" && rule.EditFlags && s.opts.AllowVendorFlagEdits {
			edited = s.editVendorFlags(file, &res)
			if res.status == pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
				s.modeResults[file] = res
				continue
			}
		} else if backup, ok := st.FlagEdits[file]; ok && (rule.Mode != pb.ApparmorMode_APPARMOR_MODE_ENFORCE || !rule.EditFlags || !s.opts.AllowVendorFlagEdits) {
			if data, err := os.ReadFile(backup); err == nil { //nolint:gosec // G304: path recorded by this agent
				if err := apparmor.WriteFile(s.deps.Paths.ProfilePath(file), data); err == nil {
					_ = os.Remove(backup)
					edited = true
					s.changed()
				}
			}
			delete(st.FlagEdits, file)
			pf = s.files[file]
		}
		loadedMode, loaded := s.isLoaded(file)
		switch {
		case !s.canLoad():
			res.status = pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
			res.msg = "links set; AppArmor is not enabled on this node, the mode applies at the next boot"
			if !s.enabled {
				res.msg = "AppArmor is not enabled on this node"
			} else {
				res.msg = "apparmor_parser is not installed"
			}
		case wantDisable:
			if loaded {
				s.drift("apparmor:profile:"+file, "found loaded ("+loadedMode+"), expected disabled; unloaded")
				if err := s.unload(file); err != nil {
					res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()
					break
				}
				s.changed()
				for _, n := range pf.Names {
					delete(s.loaded, n)
				}
			}
			res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "disabled"
		default:
			wantMode := "enforce"
			if wantComplain {
				wantMode = "complain"
			}
			// What a reload can achieve: a file that sets complain mode
			// itself stays complain until its flags are edited.
			effective := wantMode
			curFile := s.files[file]
			if wantMode == "enforce" && curFile.FileMode() == "complain" {
				effective = "complain"
			}
			if !loaded || loadedMode != effective || changedD || changedC || edited {
				if loaded && loadedMode != effective && !changedD && !changedC {
					s.drift("apparmor:profile:"+file, fmt.Sprintf("found %s, expected %s; reloaded", loadedMode, effective))
				}
				if err := s.reload(file, wantComplain); err != nil {
					res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()
					break
				}
				s.changed()
				for _, n := range pf.Names {
					s.loaded[n] = effective
				}
				loadedMode = effective
			}
			switch {
			case loadedMode == wantMode:
				res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, wantMode
			case wantMode == "enforce" && loadedMode == "complain" && curFile.FileMode() == "complain":
				res.status = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
				res.msg = "the profile file sets complain mode itself; enable 'edit vendor flags' on the entry (and apparmor.allow_vendor_flag_edits on the node) to enforce it"
			default:
				res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, fmt.Sprintf("expected %s, loaded as %s", wantMode, loadedMode)
			}
		}
		s.modeResults[file] = res
	}
	// Files that vanished but still have recorded links or edits.
	for file := range st.Links {
		if _, exists := s.files[file]; !exists {
			orig := st.Links[file]
			_, _ = s.deps.Paths.SetLink(apparmor.DisableDir, file, orig.Disabled)
			_, _ = s.deps.Paths.SetLink(apparmor.ForceComplainDir, file, orig.Complain)
			delete(st.Links, file)
		}
	}
}

func firstName(pf *apparmor.ProfileFile) string {
	if len(pf.Names) > 0 {
		return pf.Names[0]
	}
	return pf.File
}

// editVendorFlags strips the complain flag from a vendor file after
// backing it up. It reports whether the file changed.
func (s *apparmorSyncer) editVendorFlags(file string, res *apparmorFileResult) bool {
	st := s.state
	path := s.deps.Paths.ProfilePath(file)
	data, err := os.ReadFile(path) //nolint:gosec // G304,G703: path under the profile directory, file name validated by macrules
	if err != nil {
		res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()
		return false
	}
	next, changed := apparmor.SetEnforceFlags(string(data))
	if !changed {
		return false
	}
	if _, recorded := st.FlagEdits[file]; !recorded {
		backup := filepath.Join(s.opts.BackupDir, file)
		if err := os.MkdirAll(s.opts.BackupDir, 0o700); err != nil {
			res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()
			return false
		}
		if err := os.WriteFile(backup, data, 0o600); err != nil { //nolint:gosec // G703: backup directory from agent configuration, file name validated by macrules
			res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()
			return false
		}
		st.FlagEdits[file] = backup
	}
	if err := apparmor.WriteFile(path, []byte(next)); err != nil {
		res.status, res.msg = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()
		return false
	}
	pf := apparmor.ParseProfileText(next)
	pf.File = file
	s.files[file] = pf
	s.changed()
	return true
}

// releaseFile restores a file no rule covers any more.
func (s *apparmorSyncer) releaseFile(file string) {
	st := s.state
	reload := false
	if backup, ok := st.FlagEdits[file]; ok {
		if data, err := os.ReadFile(backup); err == nil { //nolint:gosec // G304: path recorded by this agent
			if err := apparmor.WriteFile(s.deps.Paths.ProfilePath(file), data); err == nil {
				_ = os.Remove(backup)
				reload = true
			}
		}
		delete(st.FlagEdits, file)
	}
	if orig, ok := st.Links[file]; ok {
		cd, _ := s.deps.Paths.SetLink(apparmor.DisableDir, file, orig.Disabled)
		cc, _ := s.deps.Paths.SetLink(apparmor.ForceComplainDir, file, orig.Complain)
		reload = reload || cd || cc
		delete(st.Links, file)
		if reload {
			s.changed()
			if orig.Disabled {
				_ = s.unload(file)
			} else {
				_ = s.reload(file, orig.Complain)
			}
		}
	} else if reload {
		s.changed()
		_ = s.reload(file, false)
	}
}

// ─── compliance ──────────────────────────────────────────────────────────────

func (s *apparmorSyncer) compliance(e ApparmorEntry) ApparmorPolicyResult {
	p := e.Policy
	if p == nil {
		return ApparmorPolicyResult{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "policy has no content"}
	}
	ok := pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
	bad := pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
	errSt := pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
	na := pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE
	var items []*pb.ComplianceItemResult
	disabledMsg := "AppArmor is not enabled on this node"
	if !s.enabled {
		if s.deps.Paths.InLSMList() {
			disabledMsg += " (module parameter enabled=N)"
		} else {
			disabledMsg += " (not in the kernel's LSM list; add apparmor=1 security=apparmor to the kernel command line)"
		}
	}
	if p.GetRequireEnabled() {
		if s.enabled {
			items = append(items, macItem(apparmorItemEnabled, "enabled", ok, "AppArmor is enabled"))
		} else {
			items = append(items, macItem(apparmorItemEnabled, "enabled", bad, disabledMsg))
		}
	}
	if p.GetEnsureService() {
		switch {
		case !s.enabled:
			items = append(items, macItem(apparmorItemService, apparmorService, na, disabledMsg))
		case s.deps.Systemd == nil:
			items = append(items, macItem(apparmorItemService, apparmorService, errSt, "cannot reach systemd on the system bus"))
		case s.serviceMsg != "":
			items = append(items, macItem(apparmorItemService, apparmorService, errSt, s.serviceMsg))
		case s.serviceOK:
			items = append(items, macItem(apparmorItemService, apparmorService, ok, "running"))
		default:
			items = append(items, macItem(apparmorItemService, apparmorService, bad, "not running"))
		}
	}
	switchItem := func(schema, name string, want pb.ApparmorSwitch, merged pb.ApparmorSwitch, by macSource) {
		if want == pb.ApparmorSwitch_APPARMOR_SWITCH_UNSPECIFIED {
			return
		}
		switch {
		case by.ID != e.ID && merged != want:
			items = append(items, macItem(schema, name, na, "overridden by policy "+by.label()))
		case s.sysctlMsg[name] != "":
			if strings.HasPrefix(s.sysctlMsg[name], "this kernel has no") {
				items = append(items, macItem(schema, name, na, s.sysctlMsg[name]))
			} else {
				items = append(items, macItem(schema, name, errSt, s.sysctlMsg[name]))
			}
		case s.sysctlOK[name]:
			items = append(items, macItem(schema, name, ok, strings.ToLower(strings.TrimPrefix(want.String(), "APPARMOR_SWITCH_"))))
		default:
			items = append(items, macItem(schema, name, bad, "not applied"))
		}
	}
	switchItem(apparmorItemUserns, apparmor.SysctlRestrictUserns, p.GetRestrictUnprivilegedUserns(), s.desired.Userns, s.desired.UsernsBy)
	switchItem(apparmorItemUnconfined, apparmor.SysctlRestrictUnconfined, p.GetRestrictUnprivilegedUnconfined(), s.desired.Unconfined, s.desired.UnconfinedBy)

	for _, pm := range p.GetProfiles() {
		match := pm.GetMatch()
		if macrules.IsGlob(match) {
			items = append(items, s.globItem(e, pm))
			continue
		}
		res, ok := s.modeResults[match]
		switch {
		case !ok:
			if _, exists := s.files[match]; !exists {
				items = append(items, macItem(apparmorItemProfile, match, na, "profile file is not installed on this node"))
			} else {
				items = append(items, macItem(apparmorItemProfile, match, na, "profile is managed by other software on this node"))
			}
		case res.by.ID != e.ID && res.mode != pm.GetMode():
			items = append(items, macItem(apparmorItemProfile, match, na, "overridden by policy "+res.by.label()))
		default:
			items = append(items, macItem(apparmorItemProfile, match, res.status, res.msg))
		}
	}
	for _, c := range p.GetCustomProfiles() {
		name := c.GetName()
		file := macrules.ProfileFileName(name)
		d := s.desired.Custom[name]
		switch {
		case !s.opts.AllowCustomProfiles:
			items = append(items, macItem(apparmorItemCustom, file, na, "custom profiles are disabled in the agent configuration (apparmor.allow_custom_profiles)"))
		case d == nil:
			items = append(items, macItem(apparmorItemCustom, file, errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(apparmorItemCustom, file, errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && d.Text != c.GetText():
			items = append(items, macItem(apparmorItemCustom, file, na, "overridden by policy "+d.By.label()))
		case s.customMsg[name] != "":
			items = append(items, macItem(apparmorItemCustom, file, errSt, s.customMsg[name]))
		case !s.enabled:
			items = append(items, macItem(apparmorItemCustom, file, na, "profile written; "+disabledMsg))
		case !s.parserOK:
			items = append(items, macItem(apparmorItemCustom, file, errSt, "apparmor_parser is not installed"))
		default:
			items = append(items, macItem(apparmorItemCustom, file, ok, "loaded ("+s.loaded[file]+")"))
		}
	}
	for _, o := range p.GetLocalOverrides() {
		file := o.GetProfileFile()
		d := s.desired.Overrides[file]
		switch {
		case !s.opts.AllowCustomProfiles:
			items = append(items, macItem(apparmorItemLocal, file, na, "custom profiles are disabled in the agent configuration (apparmor.allow_custom_profiles)"))
		case d == nil:
			items = append(items, macItem(apparmorItemLocal, file, errSt, "missing from the merged state"))
		case d.Conflict != nil:
			items = append(items, macItem(apparmorItemLocal, file, errSt, conflictMessage(d.By, *d.Conflict)))
		case d.By.ID != e.ID && d.Text != o.GetText():
			items = append(items, macItem(apparmorItemLocal, file, na, "overridden by policy "+d.By.label()))
		case s.overrideMsg[file] != "":
			if strings.HasPrefix(s.overrideMsg[file], "vendor profile is not installed") || strings.HasPrefix(s.overrideMsg[file], "the vendor profile does not include") {
				items = append(items, macItem(apparmorItemLocal, file, na, s.overrideMsg[file]))
			} else {
				items = append(items, macItem(apparmorItemLocal, file, errSt, s.overrideMsg[file]))
			}
		case !s.enabled:
			items = append(items, macItem(apparmorItemLocal, file, na, "override written; "+disabledMsg))
		case s.overrideOK[file]:
			items = append(items, macItem(apparmorItemLocal, file, ok, "applied"))
		default:
			items = append(items, macItem(apparmorItemLocal, file, bad, "not applied"))
		}
	}
	if len(p.GetExtraHomeDirs()) > 0 {
		switch {
		case s.homeMsg != "":
			items = append(items, macItem(apparmorItemHomeDirs, "home.d/bor", errSt, s.homeMsg))
		case s.reloadMsg != "":
			items = append(items, macItem(apparmorItemHomeDirs, "home.d/bor", errSt, s.reloadMsg))
		case !s.enabled:
			items = append(items, macItem(apparmorItemHomeDirs, "home.d/bor", na, "tunable written; "+disabledMsg))
		case s.homeOK:
			items = append(items, macItem(apparmorItemHomeDirs, "home.d/bor", ok, strings.Join(s.desired.HomeDirs, " ")))
		default:
			items = append(items, macItem(apparmorItemHomeDirs, "home.d/bor", bad, "not applied"))
		}
	}
	status, msg := rollupMacItems(items, "policy sets nothing that applies here", disabledMsg)
	return ApparmorPolicyResult{Status: status, Message: msg, Items: items}
}

// globItem aggregates the files a pattern entry matched.
func (s *apparmorSyncer) globItem(e ApparmorEntry, pm *pb.ApparmorProfileMode) *pb.ComplianceItemResult {
	match := pm.GetMatch()
	var matched, won, good, badCount, errCount, skipped int
	var problems []string
	for _, file := range sortedKeys(s.files) {
		if !macrules.MatchProfileFile(match, file) {
			continue
		}
		res, ok := s.modeResults[file]
		if !ok {
			continue
		}
		matched++
		if res.by.ID != e.ID && res.mode != pm.GetMode() {
			continue
		}
		won++
		switch res.status {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT:
			good++
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
			skipped++
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			errCount++
			problems = append(problems, file+": "+res.msg)
		default:
			badCount++
			problems = append(problems, file+": "+res.msg)
		}
	}
	if matched == 0 {
		return macItem(apparmorItemProfile, match, pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no profile file matches on this node")
	}
	if won == 0 {
		return macItem(apparmorItemProfile, match, pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "every matching profile is decided by another policy")
	}
	summary := fmt.Sprintf("%d profiles: %d as requested, %d not, %d errors, %d left alone", won, good, badCount, errCount, skipped)
	if len(problems) > 5 {
		problems = append(problems[:5], fmt.Sprintf("and %d more", len(problems)-5))
	}
	switch {
	case errCount > 0:
		return macItem(apparmorItemProfile, match, pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, summary+"; "+strings.Join(problems, "; "))
	case badCount > 0:
		return macItem(apparmorItemProfile, match, pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, summary+"; "+strings.Join(problems, "; "))
	case good == 0:
		return macItem(apparmorItemProfile, match, pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, summary)
	}
	return macItem(apparmorItemProfile, match, pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, summary)
}

// ─── inventory ───────────────────────────────────────────────────────────────

// ApparmorInventory is what the node reports about AppArmor.
type ApparmorInventory struct {
	Status   *pb.ApparmorStatus
	Profiles []*pb.ApparmorProfileInfo
}

// ScanApparmorInventory reads the status and the profiles of the node:
// every file under the profile directory and every loaded profile.
func ScanApparmorInventory(ctx context.Context, deps *ApparmorDeps, state *ApparmorState) *ApparmorInventory {
	p := deps.Paths
	inv := &ApparmorInventory{Status: &pb.ApparmorStatus{}}
	st := inv.Status
	st.Enabled = p.Enabled()
	st.InLsmList = p.InLSMList()
	st.BootFlags = mac.ReadBootFlags(deps.Mac)
	st.RestrictUserns, st.RestrictUnconfined = -1, -1
	if v, ok := p.Sysctl(apparmor.SysctlRestrictUserns); ok {
		st.RestrictUserns = int32(min(max(v, 0), 1)) //nolint:gosec // G115: bounded
	}
	if v, ok := p.Sysctl(apparmor.SysctlRestrictUnconfined); ok {
		st.RestrictUnconfined = int32(min(max(v, 0), 1)) //nolint:gosec // G115: bounded
	}
	if deps.Parser != nil && deps.Parser.Available() {
		st.ParserVersion = deps.Parser.Version(ctx)
	}
	if deps.Systemd != nil {
		if us, err := deps.Systemd.Status(ctx, apparmorService); err == nil && us.Exists() {
			st.ServiceActive = us.IsActive()
			st.ServiceEnabled = us.UnitFileState == "enabled" || us.UnitFileState == "static"
		}
	}
	files, _ := apparmor.ScanProfiles(p.ProfilesDir)
	st.ProfileFileCount = int32(min(len(files), 1<<30)) //nolint:gosec // G115: bounded
	loaded := map[string]string{}
	if st.Enabled {
		st.UsernsMediation = p.UsernsMediation()
		loaded, _ = p.LoadedProfiles()
		for _, mode := range loaded {
			switch mode {
			case "enforce":
				st.EnforceCount++
			case "complain":
				st.ComplainCount++
			case "unconfined":
				st.UnconfinedCount++
			case "kill":
				st.KillCount++
			}
		}
	}
	labels, _ := p.ProcessLabels()
	attached := map[string]int32{}
	unconfinedExes := map[string]bool{}
	for _, l := range labels {
		if l.Label == "unconfined" {
			if l.Exe != "" {
				unconfinedExes[l.Exe] = true
			}
			continue
		}
		attached[l.Label]++
	}
	attachmentFiles := map[string][]string{}
	for _, f := range files {
		for _, a := range f.Attachments {
			if !strings.ContainsAny(a, "*?[{") {
				attachmentFiles[a] = append(attachmentFiles[a], f.File)
			}
		}
	}
	seen := map[string]bool{}
	for i := range files {
		f := &files[i]
		name := firstName(f)
		seen[name] = true
		info := &pb.ApparmorProfileInfo{
			Name:            name,
			File:            f.File,
			LoadedMode:      loaded[name],
			FileMode:        f.FileMode(),
			ManagedBy:       macrules.ManagedBy(name, f.File),
			HasLocalInclude: f.LocalInclude != "",
			Attachments:     f.Attachments,
		}
		for _, n := range f.Names {
			info.AttachedProcesses += attached[n]
		}
		for _, a := range f.Attachments {
			if unconfinedExes[a] && info.LoadedMode != "" {
				info.UnconfinedWithProfile = true
			}
			if len(attachmentFiles[a]) > 1 {
				info.AttachmentConflict = true
			}
		}
		d, c := p.LinkState(f.File)
		orig, managed := state.Links[f.File]
		info.ForeignDisableLink = d && (!managed || orig.Disabled)
		info.ForeignComplainLink = c && (!managed || orig.Complain)
		inv.Profiles = append(inv.Profiles, info)
	}
	for name, mode := range loaded {
		if seen[name] || strings.Contains(name, "//") {
			continue
		}
		managed := macrules.ManagedBy(name, "")
		if managed == "vendor" {
			managed = "runtime"
		}
		inv.Profiles = append(inv.Profiles, &pb.ApparmorProfileInfo{Name: name, LoadedMode: mode, ManagedBy: managed, AttachedProcesses: attached[name]})
	}
	sort.Slice(inv.Profiles, func(i, j int) bool { return inv.Profiles[i].Name < inv.Profiles[j].Name })
	return inv
}
