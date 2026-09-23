// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/escrow"
	"github.com/VuteTech/Bor/server/internal/models"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Errors surfaced by DiskEncryptionService. Handlers map them to HTTP or
// gRPC status codes.
var (
	ErrLuksVolumeNotFound  = database.ErrLuksVolumeNotFound
	ErrRecoveryKeyNotFound = database.ErrRecoveryKeyNotFound
	// ErrEscrowNotConfigured is returned when no KEK is configured (fail closed).
	ErrEscrowNotConfigured = errors.New("recovery key escrow is not configured on the server (set BOR_ESCROW_KEK_FILE)")
	// ErrNoRotationPending is returned when a key release is requested
	// without a pending rotation task.
	ErrNoRotationPending = errors.New("no rotation is pending for this volume")
	// ErrReleaseRateLimited is returned when a volume's key was already
	// released within the last 24 hours.
	ErrReleaseRateLimited = errors.New("the recovery key of this volume was already released in the last 24 hours")
	// ErrEscrowRateLimited is returned when a volume escrows too many keys.
	ErrEscrowRateLimited = errors.New("too many escrow attempts for this volume today")
	// ErrServerAssistedRotationOff is returned when the effective policy
	// disables server-assisted rotation.
	ErrServerAssistedRotationOff = errors.New("server-assisted rotation is disabled by policy for this node")
)

// DiskEncryptionValidationError is returned for invalid input (HTTP 400 /
// gRPC InvalidArgument).
type DiskEncryptionValidationError struct{ Msg string }

func (e *DiskEncryptionValidationError) Error() string { return e.Msg }

func diskEncInvalid(format string, args ...interface{}) error {
	return &DiskEncryptionValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Validation rules shared with the agent.
var (
	// recoveryKeyRE is the systemd-cryptenroll modhex recovery key format:
	// 8 dash-separated groups of 8 characters from "cbdefghijklnrtuv"
	// (256 bits of entropy). Kept in parity with the agent's generator.
	recoveryKeyRE = regexp.MustCompile(`^([cbdefghijklnrtuv]{8}-){7}[cbdefghijklnrtuv]{8}$`)
	// tangThumbprintRE is an RFC 7638 SHA-256 thumbprint: 43 base64url chars.
	tangThumbprintRE = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	// luksCipherRE covers cryptsetup cipher specs such as aes-xts-plain64.
	luksCipherRE = regexp.MustCompile(`^[a-z0-9-]{3,64}$`)
	luksUUIDRE   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	diskEncMaxMountpoints      = 32
	diskEncMaxTangServers      = 4
	diskEncMinRotationDays     = uint32(30)
	diskEncMaxRotationDays     = uint32(730)
	diskEncDefaultRotationDays = uint32(180)
)

// ValidateRecoveryKeyFormat checks the 8×8 modhex recovery key format.
func ValidateRecoveryKeyFormat(key string) bool {
	return recoveryKeyRE.MatchString(key)
}

// ValidateTangThumbprint checks an RFC 7638 S256 thumbprint (base64url).
func ValidateTangThumbprint(thp string) bool {
	return tangThumbprintRE.MatchString(thp)
}

// ValidateDiskEncryptionContent validates a DiskEncryptionPolicy JSON string
// for release.
func ValidateDiskEncryptionContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("disk encryption policy content is empty")
	}
	var pol pb.DiskEncryptionPolicy
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid disk encryption policy: %w", err)
	}

	switch pol.GetVolumeScope() {
	case pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_UNSPECIFIED,
		pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_SYSTEM,
		pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_ALL_FIXED:
		if len(pol.GetMountpoints()) > 0 {
			return fmt.Errorf("mountpoints are only allowed with the MOUNTPOINTS volume scope")
		}
	case pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_MOUNTPOINTS:
		if len(pol.GetMountpoints()) == 0 {
			return fmt.Errorf("the MOUNTPOINTS volume scope requires at least one mountpoint")
		}
	default:
		return fmt.Errorf("unknown volume_scope %d", int32(pol.GetVolumeScope()))
	}
	if len(pol.GetMountpoints()) > diskEncMaxMountpoints {
		return fmt.Errorf("at most %d mountpoints are allowed", diskEncMaxMountpoints)
	}
	seenMounts := map[string]bool{}
	for _, m := range pol.GetMountpoints() {
		if !strings.HasPrefix(m, "/") || strings.Contains(m, "..") || strings.ContainsAny(m, " \t\r\n") {
			return fmt.Errorf("invalid mountpoint %q: must be an absolute path", m)
		}
		if seenMounts[m] {
			return fmt.Errorf("duplicate mountpoint %q", m)
		}
		seenMounts[m] = true
	}

	if bits := pol.GetMinVolumeKeyBits(); bits != 0 && (bits < 128 || bits > 4096) {
		return fmt.Errorf("min_volume_key_bits must be between 128 and 4096 (got %d)", bits)
	}
	for _, c := range pol.GetAllowedCiphers() {
		if !luksCipherRE.MatchString(c) {
			return fmt.Errorf("invalid cipher spec %q", c)
		}
	}

	if err := validateTpm2Protector(pol.GetTpm2()); err != nil {
		return err
	}
	if err := validateTangProtector(pol.GetTang()); err != nil {
		return err
	}
	if rec := pol.GetRecovery(); rec != nil && rec.RotationIntervalDays != nil {
		d := rec.GetRotationIntervalDays()
		if d != 0 && (d < diskEncMinRotationDays || d > diskEncMaxRotationDays) {
			return fmt.Errorf("rotation_interval_days must be 0 (never) or between %d and %d (got %d)",
				diskEncMinRotationDays, diskEncMaxRotationDays, d)
		}
	}

	switch pol.GetPassphraseMode() {
	case pb.PassphraseMode_PASSPHRASE_MODE_UNSPECIFIED, pb.PassphraseMode_PASSPHRASE_MODE_KEEP,
		pb.PassphraseMode_PASSPHRASE_MODE_REMOVE:
	default:
		return fmt.Errorf("unknown passphrase_mode %d", int32(pol.GetPassphraseMode()))
	}
	switch pol.GetInitramfsMode() {
	case pb.InitramfsMode_INITRAMFS_MODE_UNSPECIFIED, pb.InitramfsMode_INITRAMFS_MODE_VERIFY_ONLY,
		pb.InitramfsMode_INITRAMFS_MODE_MANAGE:
	default:
		return fmt.Errorf("unknown initramfs_mode %d", int32(pol.GetInitramfsMode()))
	}

	if !pol.GetRequireEncryption() && !pol.GetRequireTpm2() && !pol.GetRequireSecureBoot() &&
		!pol.GetTpm2().GetEnabled() && !pol.GetTang().GetEnabled() && !pol.GetRecovery().GetEscrow() {
		return fmt.Errorf("policy must require encryption or enable at least one protector")
	}
	return nil
}

func validateTpm2Protector(t *pb.Tpm2Protector) error {
	if t == nil || !t.GetEnabled() {
		return nil
	}
	switch t.GetPcrProfile() {
	case pb.TpmPcrProfile_TPM_PCR_PROFILE_UNSPECIFIED,
		pb.TpmPcrProfile_TPM_PCR_PROFILE_SECURE_BOOT,
		pb.TpmPcrProfile_TPM_PCR_PROFILE_SECURE_BOOT_SHIM:
		if len(t.GetPcrs()) > 0 {
			return fmt.Errorf("tpm2.pcrs is only allowed with the CUSTOM PCR profile")
		}
	case pb.TpmPcrProfile_TPM_PCR_PROFILE_CUSTOM:
		if len(t.GetPcrs()) == 0 {
			return fmt.Errorf("the CUSTOM PCR profile requires a non-empty tpm2.pcrs list")
		}
		seen := map[uint32]bool{}
		for _, p := range t.GetPcrs() {
			if p > 23 {
				return fmt.Errorf("invalid PCR %d: PCRs are 0..23", p)
			}
			if (p == 0 || p == 2) && !t.GetAllowFirmwarePcrs() {
				return fmt.Errorf("PCR %d changes on firmware updates; set allow_firmware_pcrs to use it", p)
			}
			if seen[p] {
				return fmt.Errorf("duplicate PCR %d", p)
			}
			seen[p] = true
		}
	default:
		return fmt.Errorf("unknown tpm2.pcr_profile %d", int32(t.GetPcrProfile()))
	}
	switch t.GetBackend() {
	case pb.TpmBackend_TPM_BACKEND_UNSPECIFIED, pb.TpmBackend_TPM_BACKEND_AUTO,
		pb.TpmBackend_TPM_BACKEND_SYSTEMD, pb.TpmBackend_TPM_BACKEND_CLEVIS:
	default:
		return fmt.Errorf("unknown tpm2.backend %d", int32(t.GetBackend()))
	}
	return nil
}

func validateTangProtector(t *pb.TangProtector) error {
	if t == nil || !t.GetEnabled() {
		return nil
	}
	n := len(t.GetServers())
	if !t.GetBorResponder() && n == 0 {
		return fmt.Errorf("the Tang protector requires at least one server")
	}
	if n > diskEncMaxTangServers {
		return fmt.Errorf("at most %d Tang servers are allowed per policy (LUKS2 metadata budget)", diskEncMaxTangServers)
	}
	if thr := t.GetThreshold(); thr > uint32(n) && n > 0 { //nolint:gosec // n <= 4
		return fmt.Errorf("tang.threshold (%d) exceeds the number of servers (%d)", thr, n)
	}
	seenURLs := map[string]bool{}
	for i, s := range t.GetServers() {
		prefix := fmt.Sprintf("tang.servers[%d]", i)
		if err := validateTangURL(s.GetUrl()); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		if seenURLs[s.GetUrl()] {
			return fmt.Errorf("%s: duplicate server URL %q", prefix, s.GetUrl())
		}
		seenURLs[s.GetUrl()] = true
		if s.GetThumbprint() == "" {
			return fmt.Errorf("%s: a confirmed signing thumbprint is required (never bind with -y)", prefix)
		}
		for _, thp := range append([]string{s.GetThumbprint()}, s.GetAcceptedThumbprints()...) {
			if !tangThumbprintRE.MatchString(thp) {
				return fmt.Errorf("%s: invalid thumbprint %q (expected 43 base64url characters, RFC 7638 S256)", prefix, thp)
			}
		}
	}
	if t.GetBorResponder() {
		return fmt.Errorf("tang.bor_responder is not available yet (Phase 2)")
	}
	return nil
}

// tangURLRE is the only shape of Tang URL accepted anywhere: http or https
// (Tang serves plain HTTP by design), a DNS name or IPv4 literal (no
// userinfo, no IPv6 literal), an optional port and an optional plain path.
// No query, no fragment. It is applied to the raw string before parsing so
// every downstream use of the value is guarded by the same check.
var tangURLRE = regexp.MustCompile(
	`^https?://[A-Za-z0-9](?:[A-Za-z0-9.-]{0,252}[A-Za-z0-9])?(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~%/-]*)?$`)

// normalizeTangURL validates raw against tangURLRE, parses it and rebuilds
// the URL from the parsed components (scheme, host, path) with any trailing
// slash dropped. The rebuilt value is the only form that is stored in the
// registry or fetched by the advertisement checks, so an admin-supplied URL
// can never smuggle credentials, a query string or an unexpected shape into
// an outbound request (CodeQL go/request-forgery).
func normalizeTangURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url is required")
	}
	if len(raw) > 2048 {
		return "", fmt.Errorf("url is too long")
	}
	if !tangURLRE.MatchString(raw) {
		return "", fmt.Errorf("invalid url %q: expected http(s)://host[:port][/path] with no credentials, query or fragment", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url %q: %v", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid url %q: expected http(s)://host[:port][/path] with no credentials, query or fragment", raw)
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

func validateTangURL(u string) error {
	_, err := normalizeTangURL(u)
	return err
}

// ─── Effective policy merge ─────────────────────────────────────────────

// MergeDiskEncryptionPolicies merges bound DiskEncryption policies into one
// effective configuration (policies sorted by ascending priority; the last
// writer wins for "highest-priority" semantics). require_* and
// recovery.escrow are OR-ed; rotation_interval_days takes the smallest
// non-zero value; everything else comes from the highest-priority policy
// that sets it. Tang server lists are NOT unioned.
func MergeDiskEncryptionPolicies(policies []*pb.DiskEncryptionPolicy) *pb.DiskEncryptionPolicy {
	if len(policies) == 0 {
		return nil
	}
	out := &pb.DiskEncryptionPolicy{}
	var smallestRotation *uint32
	for _, p := range policies {
		if p == nil {
			continue
		}
		out.RequireEncryption = out.RequireEncryption || p.GetRequireEncryption()
		out.RequireTpm2 = out.RequireTpm2 || p.GetRequireTpm2()
		out.RequireSecureBoot = out.RequireSecureBoot || p.GetRequireSecureBoot()
		if p.GetVolumeScope() != pb.LuksVolumeScope_LUKS_VOLUME_SCOPE_UNSPECIFIED {
			out.VolumeScope = p.GetVolumeScope()
			out.Mountpoints = p.GetMountpoints()
		}
		if p.GetMinVolumeKeyBits() != 0 {
			out.MinVolumeKeyBits = p.GetMinVolumeKeyBits()
		}
		if len(p.GetAllowedCiphers()) > 0 {
			out.AllowedCiphers = p.GetAllowedCiphers()
		}
		if p.GetTpm2() != nil {
			out.Tpm2 = p.GetTpm2()
		}
		if p.GetTang() != nil {
			out.Tang = p.GetTang()
		}
		if rec := p.GetRecovery(); rec != nil {
			if out.Recovery == nil {
				out.Recovery = &pb.RecoveryKeyPolicy{}
			}
			out.Recovery.Escrow = out.Recovery.Escrow || rec.GetEscrow()
			if rec.RotationIntervalDays != nil {
				d := rec.GetRotationIntervalDays()
				if d != 0 && (smallestRotation == nil || d < *smallestRotation) {
					smallestRotation = &d
				}
			}
			if rec.RotateAfterReveal != nil {
				v := rec.GetRotateAfterReveal()
				out.Recovery.RotateAfterReveal = &v
			}
			if rec.ServerAssistedRotation != nil {
				v := rec.GetServerAssistedRotation()
				out.Recovery.ServerAssistedRotation = &v
			}
		}
		if p.GetPassphraseMode() != pb.PassphraseMode_PASSPHRASE_MODE_UNSPECIFIED {
			out.PassphraseMode = p.GetPassphraseMode()
		}
		if p.GetInitramfsMode() != pb.InitramfsMode_INITRAMFS_MODE_UNSPECIFIED {
			out.InitramfsMode = p.GetInitramfsMode()
		}
		if p.AdoptExisting != nil {
			v := p.GetAdoptExisting()
			out.AdoptExisting = &v
		}
	}
	if out.Recovery != nil && smallestRotation != nil {
		out.Recovery.RotationIntervalDays = smallestRotation
	}
	return out
}

// EffectiveRotationIntervalDays resolves the rotation interval of a merged
// policy: unset = 180 days, 0 = never (returns 0).
func EffectiveRotationIntervalDays(pol *pb.DiskEncryptionPolicy) uint32 {
	if pol == nil || pol.GetRecovery() == nil || !pol.GetRecovery().GetEscrow() {
		return 0
	}
	rec := pol.GetRecovery()
	if rec.RotationIntervalDays == nil {
		return diskEncDefaultRotationDays
	}
	return rec.GetRotationIntervalDays()
}

func effectiveRotateAfterReveal(pol *pb.DiskEncryptionPolicy) bool {
	if pol == nil || pol.GetRecovery() == nil {
		return true // safe default: a revealed key is replaced
	}
	if pol.GetRecovery().RotateAfterReveal == nil {
		return true
	}
	return pol.GetRecovery().GetRotateAfterReveal()
}

func effectiveServerAssistedRotation(pol *pb.DiskEncryptionPolicy) bool {
	if pol == nil || pol.GetRecovery() == nil {
		return true
	}
	if pol.GetRecovery().ServerAssistedRotation == nil {
		return true
	}
	return pol.GetRecovery().GetServerAssistedRotation()
}

// ─── Service ─────────────────────────────────────────────────────────────

// DiskEncryptionTaskSender pushes a DISK_ENCRYPTION_TASK stream event to a
// connected agent. Implemented by grpc.PolicyHub.
type DiskEncryptionTaskSender interface {
	SendDiskEncryptionTask(clientID string) bool
}

// DiskEncryptionConfig carries the escrow retention and MFA settings.
type DiskEncryptionConfig struct {
	RequireMFAForReveal  bool
	RetiredRetentionDays int
	OrphanRetentionDays  int
}

// DiskEncryptionService implements the LUKS recovery-key escrow flows, the
// volume inventory and the rotation task machinery
// .
type DiskEncryptionService struct {
	repo       *database.LuksRepository
	nodeSvc    *NodeService
	policySvc  *PolicyService
	escrowSvc  *escrow.Service
	auditSvc   *AuditService
	taskSender DiskEncryptionTaskSender
	cfg        DiskEncryptionConfig

	// Metrics (may be nil): escrow operations and reveals.
	escrowOps *prometheus.CounterVec
	reveals   prometheus.Counter

	// alertedClones dedupes luks.clone_suspected per process.
	cloneMu       sync.Mutex
	alertedClones map[string]bool

	janitorCancel context.CancelFunc
	janitorWG     sync.WaitGroup
	// lastTangCheck coordinates the 6-hourly Tang check inside the hourly
	// janitor tick.
	tangSvc       *TangServerService
	lastTangCheck time.Time
}

// NewDiskEncryptionService creates a DiskEncryptionService.
func NewDiskEncryptionService(repo *database.LuksRepository, nodeSvc *NodeService, policySvc *PolicyService,
	escrowSvc *escrow.Service, auditSvc *AuditService, cfg DiskEncryptionConfig) *DiskEncryptionService {
	if cfg.RetiredRetentionDays <= 0 {
		cfg.RetiredRetentionDays = 30
	}
	if cfg.OrphanRetentionDays <= 0 {
		cfg.OrphanRetentionDays = 90
	}
	return &DiskEncryptionService{
		repo:          repo,
		nodeSvc:       nodeSvc,
		policySvc:     policySvc,
		escrowSvc:     escrowSvc,
		auditSvc:      auditSvc,
		cfg:           cfg,
		alertedClones: map[string]bool{},
	}
}

// WithTaskSender wires the hub used to push DISK_ENCRYPTION_TASK events.
func (s *DiskEncryptionService) WithTaskSender(sender DiskEncryptionTaskSender) *DiskEncryptionService {
	s.taskSender = sender
	return s
}

// WithTangService wires the Tang registry service whose periodic checks run
// inside the janitor.
func (s *DiskEncryptionService) WithTangService(tang *TangServerService) *DiskEncryptionService {
	s.tangSvc = tang
	return s
}

// WithMetrics wires the Prometheus counters (both may be nil).
func (s *DiskEncryptionService) WithMetrics(escrowOps *prometheus.CounterVec, reveals prometheus.Counter) *DiskEncryptionService {
	s.escrowOps = escrowOps
	s.reveals = reveals
	return s
}

// countOp increments the escrow-operations counter.
func (s *DiskEncryptionService) countOp(op string, err error) {
	if s.escrowOps == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	s.escrowOps.WithLabelValues(op, outcome).Inc()
}

// EscrowConfigured reports whether a KEK is available (the editor shows a
// blocking alert when it is not).
func (s *DiskEncryptionService) EscrowConfigured() bool {
	return s.escrowSvc.Configured()
}

// effectivePolicyForNode merges the DiskEncryption policies bound to the
// node's groups. Returns nil when none is bound.
func (s *DiskEncryptionService) effectivePolicyForNode(ctx context.Context, node *models.Node) (*pb.DiskEncryptionPolicy, error) {
	if node == nil || len(node.NodeGroupIDs) == 0 {
		return nil, nil
	}
	policies, err := s.policySvc.ListPoliciesForNodeGroups(ctx, node.NodeGroupIDs)
	if err != nil {
		return nil, fmt.Errorf("list policies for node groups: %w", err)
	}
	var matched []*models.Policy
	for _, p := range policies {
		if p.Type == "DiskEncryption" {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	// Ascending priority: the merge lets later (higher-priority) policies win.
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Priority < matched[j].Priority })
	parsed := make([]*pb.DiskEncryptionPolicy, 0, len(matched))
	for _, p := range matched {
		var dp pb.DiskEncryptionPolicy
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(p.Content), &dp); err != nil {
			log.Printf("WARNING: disk_encryption: skipping unparsable policy %s: %v", p.ID, err)
			continue
		}
		parsed = append(parsed, &dp)
	}
	return MergeDiskEncryptionPolicies(parsed), nil
}

// ─── Inventory ───────────────────────────────────────────────────────────

// ProcessStateReport persists an agent inventory report, runs drift and
// clone detection, and returns the node's pending tasks.
func (s *DiskEncryptionService) ProcessStateReport(ctx context.Context, node *models.Node, req *pb.ReportDiskEncryptionStateRequest) ([]*pb.DiskEncryptionTask, error) {
	if p := req.GetPlatform(); p != nil {
		unencrypted, err := json.Marshal(unencryptedMountsJSON(req.GetUnencryptedSystemMounts()))
		if err != nil {
			unencrypted = []byte("[]")
		}
		tpmPresent := p.GetTpm2Present()
		platform := &models.NodeDiskEncryption{
			NodeID:                  node.ID,
			TPM2Present:             &tpmPresent,
			SecureBoot:              secureBootString(p.GetSecureBoot()),
			InitramfsGenerator:      p.GetInitramfsGenerator(),
			SystemdVersion:          p.GetSystemdVersion(),
			CryptsetupVersion:       p.GetCryptsetupVersion(),
			ClevisVersion:           p.GetClevisVersion(),
			UnencryptedSystemMounts: unencrypted,
		}
		if err := s.repo.UpsertNodePlatform(ctx, platform); err != nil {
			return nil, err
		}
	}

	retiredCutoff := time.Now().Add(time.Duration(s.cfg.RetiredRetentionDays) * 24 * time.Hour)

	for _, vol := range req.GetVolumes() {
		if !luksUUIDRE.MatchString(vol.GetLuksUuid()) {
			log.Printf("WARNING: disk_encryption: node %s reported invalid LUKS UUID %q; skipping", node.Name, vol.GetLuksUuid())
			continue
		}
		stateJSON, err := protojson.Marshal(vol)
		if err != nil {
			return nil, fmt.Errorf("marshal volume state: %w", err)
		}
		model := &models.LuksVolume{
			LuksUUID:      strings.ToLower(vol.GetLuksUuid()),
			MappingName:   vol.GetMappingName(),
			Mountpoints:   vol.GetMountpoints(),
			IsSystem:      vol.GetIsSystem(),
			LuksVersion:   int(vol.GetLuksVersion()),
			Cipher:        vol.GetCipher(),
			VolumeKeyBits: int(vol.GetVolumeKeyBits()),
			StateJSON:     stateJSON,
		}
		volumeID, err := s.repo.UpsertVolume(ctx, node.ID, node.Name, req.GetPlatform().GetSystemdVersion(), model)
		if err != nil {
			return nil, err
		}

		fingerprints := make([]string, 0, len(vol.GetKeyslots()))
		for _, ks := range vol.GetKeyslots() {
			if fp := ks.GetFingerprint(); fp != "" {
				fingerprints = append(fingerprints, fp)
			}
		}

		// Superseded keys whose slot is gone -> retired (with retention).
		if retired, err := s.repo.RetireSupersededKeys(ctx, volumeID, fingerprints, retiredCutoff); err != nil {
			log.Printf("WARNING: disk_encryption: retire superseded keys for volume %s: %v", volumeID, err)
		} else if len(retired) > 0 {
			log.Printf("disk_encryption: retired %d superseded key(s) for volume %s", len(retired), volumeID)
		}

		// Drift: the ACTIVE key's fingerprint no longer on disk.
		s.detectDrift(ctx, node, volumeID, model.LuksUUID, fingerprints)

		// Clones: the same LUKS UUID on more than one node.
		s.detectClone(ctx, node, model.LuksUUID)
	}

	return s.tasksForNode(ctx, node)
}

// detectDrift raises luks.drift and a DRIFT rotation task when the ACTIVE
// key's keyslot fingerprint disappeared from the volume.
func (s *DiskEncryptionService) detectDrift(ctx context.Context, node *models.Node, volumeID, luksUUID string, fingerprints []string) {
	active, err := s.repo.GetActiveKeyRow(ctx, volumeID)
	if err != nil {
		if !errors.Is(err, database.ErrRecoveryKeyNotFound) {
			log.Printf("WARNING: disk_encryption: drift check for volume %s: %v", volumeID, err)
		}
		return
	}
	if active.KeyslotFingerprint == "" || slices.Contains(fingerprints, active.KeyslotFingerprint) {
		return
	}
	log.Printf("disk_encryption: drift detected on node %s volume %s (active key %s slot missing)",
		node.Name, luksUUID, active.ID)
	if err := s.repo.SetRotationRequested(ctx, volumeID, pb.RecoveryKeyReason_RECOVERY_KEY_REASON_DRIFT.String()); err != nil {
		log.Printf("WARNING: disk_encryption: set drift rotation for volume %s: %v", volumeID, err)
	}
	s.emitAudit(ctx, node.Name, "luks.drift", true, "luks_volume", volumeID, map[string]interface{}{
		"luks_uuid": luksUUID,
		"escrow_id": active.ID,
		"detail":    "the escrowed recovery key no longer opens this volume; rotation scheduled",
	})
}

// detectClone raises luks.clone_suspected once per process per UUID when
// several nodes report the same LUKS UUID (shared volume key).
func (s *DiskEncryptionService) detectClone(ctx context.Context, node *models.Node, luksUUID string) {
	n, err := s.repo.CountDistinctNodesForUUID(ctx, luksUUID)
	if err != nil {
		log.Printf("WARNING: disk_encryption: clone check for %s: %v", luksUUID, err)
		return
	}
	if n < 2 {
		return
	}
	s.cloneMu.Lock()
	alerted := s.alertedClones[luksUUID]
	s.alertedClones[luksUUID] = true
	s.cloneMu.Unlock()
	if alerted {
		return
	}
	s.emitAudit(ctx, node.Name, "luks.clone_suspected", false, "luks_volume", luksUUID, map[string]interface{}{
		"luks_uuid": luksUUID,
		"nodes":     n,
		"detail":    "several nodes report the same LUKS UUID: cloned images share one volume key",
	})
}

// tasksForNode collects the node's pending disk-encryption tasks.
func (s *DiskEncryptionService) tasksForNode(ctx context.Context, node *models.Node) ([]*pb.DiskEncryptionTask, error) {
	volumes, err := s.repo.ListVolumesByNode(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	var tasks []*pb.DiskEncryptionTask
	for _, v := range volumes {
		if v.RotationRequestedAt == nil {
			continue
		}
		reason := pb.RecoveryKeyReason_RECOVERY_KEY_REASON_UNSPECIFIED
		if r, ok := pb.RecoveryKeyReason_value[v.RotationReason]; ok {
			reason = pb.RecoveryKeyReason(r)
		}
		tasks = append(tasks, &pb.DiskEncryptionTask{
			Kind:        pb.DiskEncryptionTask_ROTATE_RECOVERY_KEY,
			LuksUuid:    v.LuksUUID,
			Reason:      reason,
			RequestedAt: timestamppb.New(*v.RotationRequestedAt),
		})
	}
	return tasks, nil
}

// GetTasks returns the node's pending tasks.
func (s *DiskEncryptionService) GetTasks(ctx context.Context, node *models.Node) ([]*pb.DiskEncryptionTask, error) {
	return s.tasksForNode(ctx, node)
}

// ─── Escrow flows (agent-facing) ─────────────────────────────────────────

// EscrowKey stores a new PENDING recovery key for one of the node's volumes.
func (s *DiskEncryptionService) EscrowKey(ctx context.Context, node *models.Node, req *pb.EscrowRecoveryKeyRequest) (err error) {
	defer func() { s.countOp("escrow", err) }()
	if !s.escrowSvc.Configured() {
		return ErrEscrowNotConfigured
	}
	if !ValidateRecoveryKeyFormat(req.GetRecoveryKey()) {
		return diskEncInvalid("recovery key does not match the 8×8 modhex format")
	}
	escrowID, err := uuid.Parse(req.GetEscrowId())
	if err != nil {
		return diskEncInvalid("escrow_id must be a UUID")
	}
	volume, err := s.repo.GetVolumeByNodeAndUUID(ctx, node.ID, strings.ToLower(req.GetLuksUuid()))
	if err != nil {
		return err
	}
	// Rate limit: at most 10 escrowed keys per volume per day.
	n, err := s.repo.CountKeysCreatedSince(ctx, volume.ID, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if n >= 10 {
		s.emitAudit(ctx, node.Name, "recoverykey.escrow", false, "luks_volume", volume.ID, map[string]interface{}{
			"luks_uuid": volume.LuksUUID, "error": "rate limit exceeded",
		})
		return ErrEscrowRateLimited
	}

	keyBytes := []byte(req.GetRecoveryKey())
	wrappedDEK, ciphertext, kekID, err := s.escrowSvc.Seal(keyBytes, volume.ID, volume.LuksUUID, escrowID.String())
	zeroBytes(keyBytes)
	if err != nil {
		return fmt.Errorf("seal recovery key: %w", err)
	}
	reason := req.GetReason().String()
	if err := s.repo.InsertPendingKey(ctx, escrowID.String(), volume.ID, reason, kekID, wrappedDEK, ciphertext); err != nil {
		return err
	}
	s.emitAudit(ctx, node.Name, "recoverykey.escrow", true, "luks_volume", volume.ID, map[string]interface{}{
		"luks_uuid": volume.LuksUUID,
		"escrow_id": escrowID.String(),
		"reason":    reason,
	})
	return nil
}

// ConfirmKey promotes a PENDING key to ACTIVE after the agent verified the
// keyslot, and completes any pending rotation for the volume.
func (s *DiskEncryptionService) ConfirmKey(ctx context.Context, node *models.Node, req *pb.ConfirmRecoveryKeyRequest) (err error) {
	defer func() { s.countOp("confirm", err) }()
	volume, err := s.repo.GetVolumeByNodeAndUUID(ctx, node.ID, strings.ToLower(req.GetLuksUuid()))
	if err != nil {
		return err
	}
	if req.GetKeyslotFingerprint() == "" {
		return diskEncInvalid("keyslot_fingerprint is required")
	}
	if err := s.repo.ConfirmKey(ctx, req.GetEscrowId(), volume.ID, int(req.GetKeyslot()), req.GetKeyslotFingerprint()); err != nil {
		return err
	}
	// The rotation (if one was pending) is complete once the new key is ACTIVE.
	if err := s.repo.ClearRotationRequested(ctx, volume.ID); err != nil {
		log.Printf("WARNING: disk_encryption: clear rotation for volume %s: %v", volume.ID, err)
	}
	s.emitAudit(ctx, node.Name, "recoverykey.confirm", true, "luks_volume", volume.ID, map[string]interface{}{
		"luks_uuid": volume.LuksUUID,
		"escrow_id": req.GetEscrowId(),
		"keyslot":   req.GetKeyslot(),
	})
	return nil
}

// BeginRotation releases the ACTIVE key to the owning node as the unlock
// credential for a pending rotation. Guard rails: a rotation task
// must exist, the effective policy must allow it, and at most one release
// per volume per 24 h.
func (s *DiskEncryptionService) BeginRotation(ctx context.Context, node *models.Node, luksUUID string) (rotationID, recoveryKey string, completeBy time.Time, err error) {
	defer func() { s.countOp("release", err) }()
	if !s.escrowSvc.Configured() {
		return "", "", time.Time{}, ErrEscrowNotConfigured
	}
	volume, err := s.repo.GetVolumeByNodeAndUUID(ctx, node.ID, strings.ToLower(luksUUID))
	if err != nil {
		return "", "", time.Time{}, err
	}
	if volume.RotationRequestedAt == nil {
		return "", "", time.Time{}, ErrNoRotationPending
	}
	pol, err := s.effectivePolicyForNode(ctx, node)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if !effectiveServerAssistedRotation(pol) {
		return "", "", time.Time{}, ErrServerAssistedRotationOff
	}
	row, err := s.repo.GetActiveKeyRow(ctx, volume.ID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if row.ReleasedAt != nil && time.Since(*row.ReleasedAt) < 24*time.Hour {
		s.emitAudit(ctx, node.Name, "recoverykey.release", false, "luks_volume", volume.ID, map[string]interface{}{
			"luks_uuid": volume.LuksUUID, "escrow_id": row.ID, "error": "release rate limit exceeded",
		})
		return "", "", time.Time{}, ErrReleaseRateLimited
	}
	keyBytes, err := s.escrowSvc.Open(row.WrappedDEK, row.Ciphertext, row.KEKID, row.VolumeID, row.VolumeUUID, row.ID)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("open recovery key: %w", err)
	}
	rotationID = uuid.NewString()
	if err := s.repo.MarkReleased(ctx, row.ID, rotationID); err != nil {
		zeroBytes(keyBytes)
		return "", "", time.Time{}, err
	}
	s.emitAudit(ctx, node.Name, "recoverykey.release", true, "luks_volume", volume.ID, map[string]interface{}{
		"luks_uuid":   volume.LuksUUID,
		"escrow_id":   row.ID,
		"rotation_id": rotationID,
	})
	key := string(keyBytes)
	zeroBytes(keyBytes)
	return rotationID, key, time.Now().Add(15 * time.Minute), nil
}

// ─── Admin flows (REST-facing) ───────────────────────────────────────────

// Summary returns the fleet StatCard counts.
func (s *DiskEncryptionService) Summary(ctx context.Context) (*models.DiskEncryptionSummary, error) {
	sum, err := s.repo.Summary(ctx)
	if err != nil {
		return nil, err
	}
	sum.EscrowConfigured = s.escrowSvc.Configured()
	return sum, nil
}

// ListVolumes runs the paginated fleet-directory query.
func (s *DiskEncryptionService) ListVolumes(ctx context.Context, search, nodeID string, page, perPage int) (*models.LuksVolumeListResponse, error) {
	if len(search) > 200 {
		return nil, diskEncInvalid("search term is too long")
	}
	page, perPage = models.ClampPagination(page, perPage)
	items, total, err := s.repo.SearchVolumes(ctx, search, nodeID, page, perPage)
	if err != nil {
		return nil, err
	}
	totalPages := 0
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	return &models.LuksVolumeListResponse{
		Items: items, Total: total, Page: page, PerPage: perPage, TotalPages: totalPages,
	}, nil
}

// GetVolumeDetail returns one volume with its key history (metadata only).
func (s *DiskEncryptionService) GetVolumeDetail(ctx context.Context, volumeID string) (*models.LuksVolumeDetailResponse, error) {
	volume, err := s.repo.GetVolume(ctx, volumeID)
	if err != nil {
		return nil, err
	}
	keys, err := s.repo.ListKeysByVolume(ctx, volumeID)
	if err != nil {
		return nil, err
	}
	return &models.LuksVolumeDetailResponse{Volume: volume, Keys: keys}, nil
}

// NodeDetail returns a node's platform facts and volumes for the drawer.
func (s *DiskEncryptionService) NodeDetail(ctx context.Context, nodeID string) (*models.NodeDiskEncryptionResponse, error) {
	platform, err := s.repo.GetNodePlatform(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	volumes, err := s.repo.ListVolumesByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return &models.NodeDiskEncryptionResponse{Platform: platform, Volumes: volumes}, nil
}

// Reveal decrypts an escrowed key for an authorised human. RBAC and step-up
// are enforced by the HTTP handler; the handler also emits the audit events
// so denied step-ups are captured.
func (s *DiskEncryptionService) Reveal(ctx context.Context, actor, volumeID string, req *models.RevealRecoveryKeyRequest) (*models.RevealRecoveryKeyResponse, error) {
	if !s.escrowSvc.Configured() {
		return nil, ErrEscrowNotConfigured
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, diskEncInvalid("a reason is required to reveal a recovery key")
	}
	volume, err := s.repo.GetVolume(ctx, volumeID)
	if err != nil {
		return nil, err
	}
	var row *database.RecoveryKeyRow
	if req.EscrowID != "" {
		row, err = s.repo.GetKeyRowByID(ctx, req.EscrowID)
		if err == nil && row.VolumeID != volumeID {
			err = database.ErrRecoveryKeyNotFound
		}
	} else {
		row, err = s.repo.GetActiveKeyRow(ctx, volumeID)
	}
	if err != nil {
		return nil, err
	}
	if len(row.Ciphertext) == 0 {
		return nil, diskEncInvalid("this key has been destroyed (crypto-shredded) and can no longer be revealed")
	}
	keyBytes, err := s.escrowSvc.Open(row.WrappedDEK, row.Ciphertext, row.KEKID, row.VolumeID, row.VolumeUUID, row.ID)
	if err != nil {
		return nil, fmt.Errorf("open recovery key: %w", err)
	}
	if err := s.repo.RecordReveal(ctx, row.ID, actor); err != nil {
		log.Printf("WARNING: disk_encryption: record reveal: %v", err)
	}
	if s.reveals != nil {
		s.reveals.Inc()
	}

	rotationPending := false
	if row.Status == models.RecoveryKeyStatusActive {
		pol := s.policyForVolumeNode(ctx, volume)
		if effectiveRotateAfterReveal(pol) {
			if err := s.repo.SetRotationRequested(ctx, volumeID, pb.RecoveryKeyReason_RECOVERY_KEY_REASON_AFTER_REVEAL.String()); err != nil {
				log.Printf("WARNING: disk_encryption: schedule rotation after reveal: %v", err)
			} else {
				rotationPending = true
				s.pushTask(volume.NodeName)
			}
		}
	}

	resp := &models.RevealRecoveryKeyResponse{
		RecoveryKey:     string(keyBytes),
		EscrowID:        row.ID,
		Keyslot:         row.Keyslot,
		CreatedAt:       row.CreatedAt,
		RotationPending: rotationPending,
	}
	zeroBytes(keyBytes)
	return resp, nil
}

// RequestRotation records an admin "Rotate now" action and notifies the
// agent when it is connected.
func (s *DiskEncryptionService) RequestRotation(ctx context.Context, actor, volumeID string) error {
	volume, err := s.repo.GetVolume(ctx, volumeID)
	if err != nil {
		return err
	}
	if err := s.repo.SetRotationRequested(ctx, volumeID, pb.RecoveryKeyReason_RECOVERY_KEY_REASON_ADMIN_REQUEST.String()); err != nil {
		return err
	}
	s.emitAudit(ctx, actor, "recoverykey.rotate_request", true, "luks_volume", volumeID, map[string]interface{}{
		"luks_uuid": volume.LuksUUID,
		"node":      volume.NodeName,
		"reason":    pb.RecoveryKeyReason_RECOVERY_KEY_REASON_ADMIN_REQUEST.String(),
	})
	s.pushTask(volume.NodeName)
	return nil
}

// policyForVolumeNode resolves the effective policy for the node owning a
// volume; best effort (nil on error or orphaned volume).
func (s *DiskEncryptionService) policyForVolumeNode(ctx context.Context, volume *models.LuksVolume) *pb.DiskEncryptionPolicy {
	if volume.NodeID == nil {
		return nil
	}
	node, err := s.nodeSvc.GetNode(ctx, *volume.NodeID)
	if err != nil || node == nil {
		return nil
	}
	pol, err := s.effectivePolicyForNode(ctx, node)
	if err != nil {
		return nil
	}
	return pol
}

// pushTask sends a DISK_ENCRYPTION_TASK stream event to a connected agent.
func (s *DiskEncryptionService) pushTask(clientID string) {
	if s.taskSender == nil || clientID == "" {
		return
	}
	if !s.taskSender.SendDiskEncryptionTask(clientID) {
		log.Printf("disk_encryption: node %s not connected; task will be delivered on the next sync", clientID)
	}
}

// EmitRevealAudit records a reveal outcome. The reveal route bypasses the
// generic audit middleware, so the handler calls this explicitly.
func (s *DiskEncryptionService) EmitRevealAudit(ctx context.Context, actor, actorID, volumeID, srcIP string, success bool, details map[string]interface{}) {
	action := "recoverykey.reveal"
	if !success {
		action = "recoverykey.reveal_denied"
	}
	outcome := auditpb.Outcome_OUTCOME_SUCCESS
	if !success {
		outcome = auditpb.Outcome_OUTCOME_FAILURE
	}
	body, _ := json.Marshal(details)
	s.auditSvc.Emit(ctx, &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      &auditpb.Actor{Username: actor, UserId: actorID},
		Action:     action,
		Resource:   &auditpb.Resource{Type: "luks_volume", Id: volumeID},
		Outcome:    outcome,
		SrcIp:      srcIP,
		Payload: &auditpb.AuditEvent_HttpChange{
			HttpChange: &auditpb.HttpPayload{BodyJson: string(body)},
		},
	})
}

// ─── Janitor ──────────────────────────────────────────────────────

// StartJanitor launches the hourly janitor goroutine.
func (s *DiskEncryptionService) StartJanitor(ctx context.Context) {
	jctx, cancel := context.WithCancel(ctx)
	s.janitorCancel = cancel
	s.janitorWG.Add(1)
	go func() {
		defer s.janitorWG.Done()
		// First pass shortly after startup, then hourly.
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-jctx.Done():
				return
			case <-timer.C:
			}
			s.JanitorPass(jctx)
			timer.Reset(time.Hour)
		}
	}()
}

// StopJanitor stops the janitor goroutine and waits for it.
func (s *DiskEncryptionService) StopJanitor() {
	if s.janitorCancel != nil {
		s.janitorCancel()
	}
	s.janitorWG.Wait()
}

// JanitorPass runs one janitor iteration. Exported for tests.
func (s *DiskEncryptionService) JanitorPass(ctx context.Context) {
	now := time.Now()

	// Volumes whose node was deleted become orphans (retention clock starts).
	if n, err := s.repo.MarkOrphanedVolumes(ctx); err != nil {
		log.Printf("WARNING: disk_encryption janitor: mark orphans: %v", err)
	} else if n > 0 {
		log.Printf("disk_encryption janitor: %d volume(s) orphaned; keys kept for %d days", n, s.cfg.OrphanRetentionDays)
	}

	// PENDING keys that never became a slot expire after 24 h.
	if ids, err := s.repo.ExpirePendingKeys(ctx, now.Add(-24*time.Hour)); err != nil {
		log.Printf("WARNING: disk_encryption janitor: expire pending: %v", err)
	} else {
		for _, id := range ids {
			s.emitAudit(ctx, "janitor", "recoverykey.destroy", true, "recovery_key", id, map[string]interface{}{
				"detail": "pending escrow expired without a confirmed keyslot",
			})
		}
	}

	// Unconfirmed server-assisted releases (possible key exposure).
	if rows, err := s.repo.ListUnconfirmedReleases(ctx, now.Add(-15*time.Minute)); err != nil {
		log.Printf("WARNING: disk_encryption janitor: unconfirmed releases: %v", err)
	} else {
		for _, row := range rows {
			s.emitAudit(ctx, "janitor", "recoverykey.release_unconfirmed", false, "luks_volume", row.VolumeID, map[string]interface{}{
				"escrow_id": row.ID,
				"luks_uuid": row.VolumeUUID,
				"detail":    "a released recovery key was not rotated within 15 minutes",
			})
		}
	}

	// Scheduled rotations from each node's effective interval.
	s.scheduleRotations(ctx, now)

	// Crypto-shred retired keys past retention and orphans past retention.
	orphanCutoff := now.Add(-time.Duration(s.cfg.OrphanRetentionDays) * 24 * time.Hour)
	if ids, err := s.repo.DestroyExpiredKeys(ctx, now, orphanCutoff); err != nil {
		log.Printf("WARNING: disk_encryption janitor: destroy keys: %v", err)
	} else {
		for _, id := range ids {
			s.countOp("destroy", nil)
			s.emitAudit(ctx, "janitor", "recoverykey.destroy", true, "recovery_key", id, map[string]interface{}{
				"detail": "retention elapsed; ciphertext and wrapped DEK deleted",
			})
		}
	}

	// Rewrap records whose KEK is not current.
	s.rewrapKeys(ctx)

	// Tang advertisement checks every 6 hours.
	if s.tangSvc != nil && now.Sub(s.lastTangCheck) >= 6*time.Hour {
		s.lastTangCheck = now
		s.tangSvc.CheckAll(ctx)
	}
}

// scheduleRotations pushes ROTATE tasks for keys older than the node's
// effective rotation interval.
func (s *DiskEncryptionService) scheduleRotations(ctx context.Context, now time.Time) {
	candidates, err := s.repo.ListRotationCandidates(ctx)
	if err != nil {
		log.Printf("WARNING: disk_encryption janitor: rotation candidates: %v", err)
		return
	}
	// Effective policies are per node; cache within the pass.
	nodePolicies := map[string]*pb.DiskEncryptionPolicy{}
	nodeNames := map[string]string{}
	for _, c := range candidates {
		if c.NodeID == nil || c.ConfirmedAt == nil {
			continue
		}
		pol, ok := nodePolicies[*c.NodeID]
		if !ok {
			node, err := s.nodeSvc.GetNode(ctx, *c.NodeID)
			if err != nil || node == nil {
				continue
			}
			pol, err = s.effectivePolicyForNode(ctx, node)
			if err != nil {
				log.Printf("WARNING: disk_encryption janitor: effective policy for node %s: %v", node.Name, err)
				continue
			}
			nodePolicies[*c.NodeID] = pol
			nodeNames[*c.NodeID] = node.Name
		}
		interval := EffectiveRotationIntervalDays(pol)
		if interval == 0 {
			continue
		}
		if now.Sub(*c.ConfirmedAt) < time.Duration(interval)*24*time.Hour {
			continue
		}
		if err := s.repo.SetRotationRequested(ctx, c.VolumeID, pb.RecoveryKeyReason_RECOVERY_KEY_REASON_SCHEDULED.String()); err != nil {
			log.Printf("WARNING: disk_encryption janitor: schedule rotation for volume %s: %v", c.VolumeID, err)
			continue
		}
		s.emitAudit(ctx, "janitor", "recoverykey.rotate_request", true, "luks_volume", c.VolumeID, map[string]interface{}{
			"luks_uuid":    c.VolumeUUID,
			"reason":       pb.RecoveryKeyReason_RECOVERY_KEY_REASON_SCHEDULED.String(),
			"key_age_days": int(now.Sub(*c.ConfirmedAt).Hours() / 24),
		})
		s.pushTask(nodeNames[*c.NodeID])
	}
}

// rewrapKeys migrates records to the current KEK in bounded batches.
func (s *DiskEncryptionService) rewrapKeys(ctx context.Context) {
	if !s.escrowSvc.Configured() {
		return
	}
	rows, err := s.repo.ListKeysForRewrap(ctx, s.escrowSvc.KEKID(), 100)
	if err != nil {
		log.Printf("WARNING: disk_encryption janitor: list keys for rewrap: %v", err)
		return
	}
	for _, row := range rows {
		wrapped, ct, kekID, err := s.escrowSvc.Rewrap(row.WrappedDEK, row.Ciphertext, row.KEKID, row.VolumeID, row.VolumeUUID, row.ID)
		if err != nil {
			log.Printf("WARNING: disk_encryption janitor: rewrap key %s: %v", row.ID, err)
			continue
		}
		if err := s.repo.UpdateKeyWrapping(ctx, row.ID, kekID, wrapped, ct); err != nil {
			log.Printf("WARNING: disk_encryption janitor: store rewrapped key %s: %v", row.ID, err)
		}
	}
	if len(rows) > 0 {
		log.Printf("disk_encryption janitor: rewrapped %d key(s) to KEK %q", len(rows), s.escrowSvc.KEKID())
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (s *DiskEncryptionService) emitAudit(ctx context.Context, actor, action string, success bool, resourceType, resourceID string, details map[string]interface{}) {
	if s.auditSvc == nil {
		return
	}
	outcome := auditpb.Outcome_OUTCOME_SUCCESS
	if !success {
		outcome = auditpb.Outcome_OUTCOME_FAILURE
	}
	body, _ := json.Marshal(details)
	s.auditSvc.Emit(ctx, &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      &auditpb.Actor{Username: actor},
		Action:     action,
		Resource:   &auditpb.Resource{Type: resourceType, Id: resourceID},
		Outcome:    outcome,
		Payload: &auditpb.AuditEvent_HttpChange{
			HttpChange: &auditpb.HttpPayload{BodyJson: string(body)},
		},
	})
}

func secureBootString(s pb.SecureBootState) string {
	switch s {
	case pb.SecureBootState_SECURE_BOOT_STATE_ENABLED:
		return "enabled"
	case pb.SecureBootState_SECURE_BOOT_STATE_DISABLED:
		return "disabled"
	case pb.SecureBootState_SECURE_BOOT_STATE_SETUP_MODE:
		return "setup_mode"
	case pb.SecureBootState_SECURE_BOOT_STATE_LEGACY_BIOS:
		return "legacy_bios"
	default:
		return "unknown"
	}
}

type unencryptedMountJSON struct {
	Mountpoint string `json:"mountpoint"`
	Source     string `json:"source"`
	Fstype     string `json:"fstype"`
}

func unencryptedMountsJSON(mounts []*pb.UnencryptedMount) []unencryptedMountJSON {
	out := make([]unencryptedMountJSON, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, unencryptedMountJSON{
			Mountpoint: m.GetMountpoint(),
			Source:     m.GetSource(),
			Fstype:     m.GetFstype(),
		})
	}
	return out
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
