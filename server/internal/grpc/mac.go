// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/macrules"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// macRepository is the subset of database.MacRepository used by PolicyServer.
type macRepository interface {
	ReplaceNodeInventory(ctx context.Context, nodeID string, inv *database.MacInventory) error
}

// macActionSink receives the results of on-demand mandatory access control
// actions (services.MacActionBroker).
type macActionSink interface {
	Deliver(id, node string, r services.MacActionResult) bool
}

// WithMacRepository enables the mandatory access control inventory RPC.
func (s *PolicyServer) WithMacRepository(repo macRepository) *PolicyServer {
	s.macRepo = repo
	return s
}

// WithMacActions routes the results of on-demand actions to the requests
// waiting for them.
func (s *PolicyServer) WithMacActions(sink macActionSink) *PolicyServer {
	s.macActions = sink
	return s
}

// Limits for the node-reported inventory; agent data is untrusted input.
const (
	maxReportedMacBooleans = 2000
	maxReportedMacModules  = 2000
	maxReportedMacProfiles = 4000
	maxReportedMacDenials  = 100
	maxMacWord             = 64
	maxMacShortString      = 255
	maxMacPath             = 512
	maxMacAttachments      = 8
	maxMacBootFlags        = 8
	maxMacActionText       = 64 * 1024
	maxMacActionMessage    = 2048
)

var (
	macWordPattern     = regexp.MustCompile(`^[a-z_-]{0,32}$`)
	macActionIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	macProfilePattern  = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,255}$`)
)

// ReportMacInventory accepts the SELinux or AppArmor inventory of an
// agent's node and replaces the node's rows.
func (s *PolicyServer) ReportMacInventory(ctx context.Context, req *pb.ReportMacInventoryRequest) (*pb.ReportMacInventoryResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if s.macRepo == nil {
		return &pb.ReportMacInventoryResponse{}, nil
	}
	lsm := req.GetLsm()
	switch lsm {
	case "selinux", "apparmor", "none":
	default:
		return nil, status.Errorf(codes.InvalidArgument, "lsm must be selinux, apparmor or none")
	}
	node, err := s.nodeSvc.GetNodeByName(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to look up node: %v", err)
	}
	if node == nil {
		return nil, status.Errorf(codes.NotFound, "node not found for client_id: %s", clientID)
	}

	inv := &database.MacInventory{LSM: lsm}
	switch lsm {
	case "selinux":
		inv.Selinux = sanitizeSelinuxStatus(req.GetSelinux())
		inv.Mode = inv.Selinux.GetMode()
		inv.Booleans = sanitizeSelinuxBooleans(req.GetBooleans())
		inv.Modules = sanitizeSelinuxModules(req.GetModules())
	case "apparmor":
		inv.Apparmor = sanitizeApparmorStatus(req.GetApparmor())
		inv.Mode = "disabled"
		if inv.Apparmor.GetEnabled() {
			inv.Mode = "enabled"
		}
		inv.Profiles = sanitizeApparmorProfiles(req.GetProfiles())
	}
	inv.Denials = sanitizeMacDenials(req.GetDenials())
	if ts := req.GetDenialsSince(); ts != nil && ts.IsValid() {
		t := ts.AsTime()
		inv.DenialsSince = &t
	}
	if err := s.macRepo.ReplaceNodeInventory(ctx, node.ID, inv); err != nil {
		log.Printf("WARNING: mac inventory: failed to store inventory for node %s: %v", clientID, err)
		return nil, status.Errorf(codes.Internal, "failed to store inventory")
	}
	log.Printf("mac inventory: node %s reported %s (%s): %d booleans, %d modules, %d profiles, %d denial groups",
		clientID, lsm, inv.Mode, len(inv.Booleans), len(inv.Modules), len(inv.Profiles), len(inv.Denials))
	return &pb.ReportMacInventoryResponse{}, nil
}

func macWord(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !macWordPattern.MatchString(s) {
		return ""
	}
	return s
}

func macFlags(flags []string) []string {
	out := make([]string, 0, min(len(flags), maxMacBootFlags))
	for _, f := range flags {
		if len(out) == maxMacBootFlags {
			break
		}
		f = truncateString(strings.TrimSpace(f), maxMacWord)
		if f != "" && !strings.ContainsAny(f, "\x00\n\r") {
			out = append(out, f)
		}
	}
	return out
}

func sanitizeSelinuxStatus(st *pb.SelinuxStatus) *pb.SelinuxStatus {
	if st == nil {
		return &pb.SelinuxStatus{}
	}
	return &pb.SelinuxStatus{
		Active:                st.GetActive(),
		Mode:                  macWord(st.GetMode()),
		ConfigMode:            macWord(st.GetConfigMode()),
		PolicyType:            macWord(st.GetPolicyType()),
		PolicyVersion:         max(st.GetPolicyVersion(), 0),
		BooleanCount:          max(st.GetBooleanCount(), 0),
		ModuleCount:           max(st.GetModuleCount(), 0),
		SemanagePresent:       st.GetSemanagePresent(),
		Audit2AllowPresent:    st.GetAudit2AllowPresent(),
		SetroubleshootPresent: st.GetSetroubleshootPresent(),
		PolicyloadLocked:      st.GetPolicyloadLocked(),
		StoreRoot:             truncateString(st.GetStoreRoot(), maxMacPath),
		BootFlags:             macFlags(st.GetBootFlags()),
		RelabelPending:        st.GetRelabelPending(),
		ToolsVersion:          truncateString(st.GetToolsVersion(), maxMacWord),
	}
}

func sanitizeSelinuxBooleans(in []*pb.SelinuxBooleanInfo) []*pb.SelinuxBooleanInfo {
	out := make([]*pb.SelinuxBooleanInfo, 0, min(len(in), maxReportedMacBooleans))
	seen := make(map[string]struct{}, len(in))
	for _, b := range in {
		if len(out) == maxReportedMacBooleans {
			break
		}
		if macrules.ValidateBooleanName(b.GetName()) != nil {
			continue
		}
		if _, dup := seen[b.GetName()]; dup {
			continue
		}
		seen[b.GetName()] = struct{}{}
		out = append(out, &pb.SelinuxBooleanInfo{
			Name:        b.GetName(),
			Current:     b.GetCurrent(),
			Persistent:  b.GetPersistent(),
			Description: truncateString(b.GetDescription(), maxMacShortString),
		})
	}
	return out
}

func sanitizeSelinuxModules(in []*pb.SelinuxModuleInfo) []*pb.SelinuxModuleInfo {
	out := make([]*pb.SelinuxModuleInfo, 0, min(len(in), maxReportedMacModules))
	seen := make(map[string]struct{}, len(in))
	for _, m := range in {
		if len(out) == maxReportedMacModules {
			break
		}
		if macrules.ValidateModuleName(m.GetName()) != nil || m.GetPriority() < 0 || m.GetPriority() > 999 {
			continue
		}
		if _, dup := seen[m.GetName()]; dup {
			continue
		}
		seen[m.GetName()] = struct{}{}
		lang := macWord(m.GetLang())
		if lang != "cil" && lang != "pp" {
			lang = ""
		}
		out = append(out, &pb.SelinuxModuleInfo{Name: m.GetName(), Priority: m.GetPriority(), Enabled: m.GetEnabled(), Lang: lang})
	}
	return out
}

func sanitizeApparmorStatus(st *pb.ApparmorStatus) *pb.ApparmorStatus {
	if st == nil {
		return &pb.ApparmorStatus{}
	}
	clampSwitch := func(v int32) int32 {
		if v < -1 {
			return -1
		}
		if v > 1 {
			return 1
		}
		return v
	}
	return &pb.ApparmorStatus{
		Enabled:            st.GetEnabled(),
		InLsmList:          st.GetInLsmList(),
		ParserVersion:      truncateString(st.GetParserVersion(), maxMacWord),
		UsernsMediation:    st.GetUsernsMediation(),
		ServiceActive:      st.GetServiceActive(),
		ServiceEnabled:     st.GetServiceEnabled(),
		EnforceCount:       max(st.GetEnforceCount(), 0),
		ComplainCount:      max(st.GetComplainCount(), 0),
		UnconfinedCount:    max(st.GetUnconfinedCount(), 0),
		KillCount:          max(st.GetKillCount(), 0),
		RestrictUserns:     clampSwitch(st.GetRestrictUserns()),
		RestrictUnconfined: clampSwitch(st.GetRestrictUnconfined()),
		BootFlags:          macFlags(st.GetBootFlags()),
		ProfileFileCount:   max(st.GetProfileFileCount(), 0),
	}
}

func sanitizeApparmorProfiles(in []*pb.ApparmorProfileInfo) []*pb.ApparmorProfileInfo {
	out := make([]*pb.ApparmorProfileInfo, 0, min(len(in), maxReportedMacProfiles))
	seen := make(map[string]struct{}, len(in))
	for _, p := range in {
		if len(out) == maxReportedMacProfiles {
			break
		}
		name := p.GetName()
		if !macProfilePattern.MatchString(name) {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		file := p.GetFile()
		if file != "" && macrules.ValidateProfileFile(file) != nil {
			file = ""
		}
		managed := macWord(p.GetManagedBy())
		switch managed {
		case "vendor", "bor", "snapd", "docker", "lxd", "libvirt", "runtime":
		default:
			managed = macrules.ManagedBy(name, file)
		}
		var attachments []string
		for _, a := range p.GetAttachments() {
			if len(attachments) == maxMacAttachments {
				break
			}
			if a != "" && len(a) <= maxMacPath && !strings.ContainsAny(a, "\x00\n\r") {
				attachments = append(attachments, a)
			}
		}
		out = append(out, &pb.ApparmorProfileInfo{
			Name:                  name,
			File:                  file,
			LoadedMode:            macWord(p.GetLoadedMode()),
			FileMode:              macWord(p.GetFileMode()),
			ManagedBy:             managed,
			HasLocalInclude:       p.GetHasLocalInclude(),
			AttachedProcesses:     max(p.GetAttachedProcesses(), 0),
			UnconfinedWithProfile: p.GetUnconfinedWithProfile(),
			Attachments:           attachments,
			ForeignDisableLink:    p.GetForeignDisableLink(),
			ForeignComplainLink:   p.GetForeignComplainLink(),
			AttachmentConflict:    p.GetAttachmentConflict(),
		})
	}
	return out
}

func sanitizeMacDenials(in []*pb.MacDenialGroup) []*pb.MacDenialGroup {
	out := make([]*pb.MacDenialGroup, 0, min(len(in), maxReportedMacDenials))
	clean := func(s string, n int) string {
		s = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, s)
		return truncateString(s, n)
	}
	for _, d := range in {
		if len(out) == maxReportedMacDenials {
			break
		}
		subject := clean(d.GetSubject(), maxMacShortString)
		if subject == "" {
			continue
		}
		g := &pb.MacDenialGroup{
			Subject:    subject,
			Operation:  clean(d.GetOperation(), maxMacWord),
			Class:      clean(d.GetClass(), maxMacWord),
			Target:     clean(d.GetTarget(), maxMacPath),
			Count:      max(d.GetCount(), 0),
			Example:    clean(d.GetExample(), maxMacPath),
			Permissive: d.GetPermissive(),
			Execpath:   clean(d.GetExecpath(), maxMacPath),
		}
		if ts := d.GetLastSeen(); ts != nil && ts.IsValid() && ts.AsTime().Before(time.Now().Add(time.Hour)) {
			g.LastSeen = ts
		}
		out = append(out, g)
	}
	return out
}

// ReportMacActionResult accepts the outcome of an on-demand action and
// hands it to the request waiting for it.
func (s *PolicyServer) ReportMacActionResult(ctx context.Context, req *pb.ReportMacActionResultRequest) (*pb.ReportMacActionResultResponse, error) {
	clientID, err := requireClientIdentity(ctx, req.GetClientId())
	if err != nil {
		return nil, err
	}
	if !macActionIDPattern.MatchString(req.GetActionId()) {
		return nil, status.Errorf(codes.InvalidArgument, "action_id is malformed")
	}
	result := services.MacActionResult{
		Success: req.GetSuccess(),
		Message: truncateString(req.GetMessage(), maxMacActionMessage),
		Text:    truncateString(req.GetText(), maxMacActionText),
	}
	delivered := s.macActions != nil && s.macActions.Deliver(req.GetActionId(), clientID, result)
	log.Printf("mac action %s: node %s reported success=%v (waiting request: %v)", req.GetActionId(), clientID, req.GetSuccess(), delivered)
	return &pb.ReportMacActionResultResponse{}, nil
}
