// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/tang"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Errors surfaced by TangServerService.
var (
	ErrTangServerNotFound = database.ErrTangServerNotFound
	ErrTangServerConflict = database.ErrTangServerConflict
	// ErrTangServerInUse is returned when deleting a server referenced by a
	// DiskEncryption policy.
	ErrTangServerInUse = errors.New("this Tang server is referenced by a disk encryption policy")
)

var (
	tangServerNameRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
	tangAdvMaxBytes   = int64(64 * 1024)
	tangProbeTimeout  = 15 * time.Second
	tangMaxThumbCount = 8
)

// TangServerService manages the external Tang server registry
// (Settings -> Tang servers) and its periodic advertisement checks.
type TangServerService struct {
	repo      *database.LuksRepository
	policySvc *PolicyService
	auditSvc  *AuditService
	client    *http.Client
}

// NewTangServerService creates a TangServerService.
func NewTangServerService(repo *database.LuksRepository, policySvc *PolicyService, auditSvc *AuditService) *TangServerService {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = tangDialContext
	// Registry probes go directly to the Tang host; a proxy would both
	// bypass the address policy in tangDialContext and break access to the
	// internal networks Tang lives on.
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &TangServerService{
		repo:      repo,
		policySvc: policySvc,
		auditSvc:  auditSvc,
		client: &http.Client{
			Timeout:   tangProbeTimeout,
			Transport: tr,
			// Tang serves plain HTTP by design; never follow redirects to
			// avoid being bounced to an unexpected host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("tang advertisement endpoints must not redirect")
			},
		},
	}
}

// tangBlockedAddr refuses addresses the server must never probe: loopback,
// unspecified, link-local (which includes cloud metadata endpoints such as
// 169.254.169.254), multicast and broadcast. Private RFC 1918 / CGNAT
// ranges stay allowed - that is where Tang servers normally live.
func tangBlockedAddr(ip net.IP) error {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	reason := ""
	switch {
	case ip.IsUnspecified():
		reason = "unspecified"
	case ip.IsLoopback():
		reason = "loopback"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		reason = "link-local or multicast"
	case ip.Equal(net.IPv4bcast):
		reason = "broadcast"
	}
	if reason != "" {
		return fmt.Errorf("refusing to contact %s: %s address", ip, reason)
	}
	return nil
}

// tangDialContext resolves the host itself and connects only to addresses
// tangBlockedAddr accepts (DNS pinning: the name is resolved and checked
// here, at connect time, so it cannot be re-pointed at a blocked address
// between validation and connect).
func tangDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, rerr := net.DefaultResolver.LookupIPAddr(ctx, host)
		if rerr != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, rerr)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, ip := range ips {
		if berr := tangBlockedAddr(ip); berr != nil {
			lastErr = berr
			continue
		}
		conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no addresses for %s", host)
	}
	return nil, fmt.Errorf("dial %s: %w", host, lastErr)
}

// List returns every registered Tang server with per-thumbprint bound-volume
// counts (the rotation fleet view).
func (s *TangServerService) List(ctx context.Context) (*models.TangServerListResponse, error) {
	servers, err := s.repo.ListTangServers(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountBoundVolumesByThumbprint(ctx)
	if err != nil {
		log.Printf("WARNING: tang: bound-volume counts: %v", err)
		counts = map[string]int{}
	}
	for _, srv := range servers {
		srv.BoundVolumes = map[string]int{}
		for _, thp := range srv.TrustedThumbprints {
			if n, ok := counts[thp]; ok {
				srv.BoundVolumes[thp] = n
			}
		}
		for _, thp := range srv.AdvertisedThumbprints {
			if n, ok := counts[thp]; ok {
				srv.BoundVolumes[thp] = n
			}
		}
	}
	return &models.TangServerListResponse{Items: servers}, nil
}

// Get returns one Tang server.
func (s *TangServerService) Get(ctx context.Context, id string) (*models.TangServer, error) {
	return s.repo.GetTangServer(ctx, id)
}

// Create registers a Tang server. The trusted thumbprints must have been
// confirmed by the admin against `tang-show-keys` output (the probe endpoint
// shows what the server advertises).
func (s *TangServerService) Create(ctx context.Context, req *models.TangServerRequest, createdBy string) (*models.TangServer, error) {
	srv := &models.TangServer{CreatedBy: createdBy, LastCheckStatus: "never"}
	if err := applyTangServerRequest(srv, req); err != nil {
		return nil, err
	}
	if err := s.repo.CreateTangServer(ctx, srv); err != nil {
		return nil, err
	}
	// Best effort: run the first check right away so the row shows a status.
	s.checkServer(ctx, srv)
	return s.repo.GetTangServer(ctx, srv.ID)
}

// Update changes a Tang server's name, URL or trusted thumbprints
// (confirming a newly advertised key = adding it in front of the list).
func (s *TangServerService) Update(ctx context.Context, id string, req *models.TangServerRequest) (*models.TangServer, error) {
	srv, err := s.repo.GetTangServer(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := applyTangServerRequest(srv, req); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateTangServer(ctx, srv); err != nil {
		return nil, err
	}
	s.checkServer(ctx, srv)
	return s.repo.GetTangServer(ctx, id)
}

func applyTangServerRequest(srv *models.TangServer, req *models.TangServerRequest) error {
	if req == nil {
		return diskEncInvalid("request body is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || !tangServerNameRE.MatchString(name) {
		return diskEncInvalid("invalid name %q: letters, digits, spaces, dots, dashes; at most 64 characters", name)
	}
	srv.Name = name
	u, err := normalizeTangURL(req.URL)
	if err != nil {
		return diskEncInvalid("%v", err)
	}
	srv.URL = u
	if len(req.TrustedThumbprints) == 0 {
		return diskEncInvalid("at least one trusted signing thumbprint is required (compare with `tang-show-keys` on the Tang host)")
	}
	if len(req.TrustedThumbprints) > tangMaxThumbCount {
		return diskEncInvalid("at most %d trusted thumbprints are allowed", tangMaxThumbCount)
	}
	seen := map[string]bool{}
	trusted := make([]string, 0, len(req.TrustedThumbprints))
	for _, thp := range req.TrustedThumbprints {
		thp = strings.TrimSpace(thp)
		if !ValidateTangThumbprint(thp) {
			return diskEncInvalid("invalid thumbprint %q (expected 43 base64url characters, RFC 7638 S256)", thp)
		}
		if seen[thp] {
			continue
		}
		seen[thp] = true
		trusted = append(trusted, thp)
	}
	srv.TrustedThumbprints = trusted
	return nil
}

// Delete removes a Tang server unless a DiskEncryption policy references it.
func (s *TangServerService) Delete(ctx context.Context, id string) error {
	srv, err := s.repo.GetTangServer(ctx, id)
	if err != nil {
		return err
	}
	inUse, err := s.referencedByPolicy(ctx, srv)
	if err != nil {
		return err
	}
	if inUse {
		return ErrTangServerInUse
	}
	return s.repo.DeleteTangServer(ctx, id)
}

// referencedByPolicy reports whether any DiskEncryption policy carries this
// server (by registry id or URL - policies are self-contained copies).
func (s *TangServerService) referencedByPolicy(ctx context.Context, srv *models.TangServer) (bool, error) {
	policies, err := s.policySvc.ListAllPolicies(ctx)
	if err != nil {
		return false, fmt.Errorf("list policies: %w", err)
	}
	for _, p := range policies {
		if p.Type != "DiskEncryption" {
			continue
		}
		var dp pb.DiskEncryptionPolicy
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(p.Content), &dp); err != nil {
			continue
		}
		for _, entry := range dp.GetTang().GetServers() {
			if entry.GetRegistryId() == srv.ID || strings.TrimRight(entry.GetUrl(), "/") == srv.URL {
				return true, nil
			}
		}
	}
	return false, nil
}

// Probe fetches a Tang advertisement and returns its thumbprints for the
// admin to confirm against `tang-show-keys`. This makes trust-on-first-use
// explicit and human-verified.
func (s *TangServerService) Probe(ctx context.Context, rawURL string) (*models.TangProbeResponse, error) {
	base, err := normalizeTangURL(rawURL)
	if err != nil {
		return nil, diskEncInvalid("%v", err)
	}
	adv, err := s.fetchAdvertisement(ctx, base)
	if err != nil {
		return nil, err
	}
	return &models.TangProbeResponse{
		URL:                 base,
		SigningThumbprints:  sortedKeys(adv.SigningKeys),
		ExchangeThumbprints: sortedKeys(adv.ExchangeKeys),
	}, nil
}

// Check re-fetches one server's advertisement, verifies it and detects new
// signing keys.
func (s *TangServerService) Check(ctx context.Context, id string) (*models.TangServer, error) {
	srv, err := s.repo.GetTangServer(ctx, id)
	if err != nil {
		return nil, err
	}
	s.checkServer(ctx, srv)
	return s.repo.GetTangServer(ctx, id)
}

// CheckAll checks every registered server (called by the janitor).
func (s *TangServerService) CheckAll(ctx context.Context) {
	servers, err := s.repo.ListTangServers(ctx)
	if err != nil {
		log.Printf("WARNING: tang: list servers for check: %v", err)
		return
	}
	for _, srv := range servers {
		s.checkServer(ctx, srv)
	}
}

// checkServer fetches, verifies and persists one server's advertisement
// state. Status: ok (advertisement signed by a trusted key, no unknown
// signing keys), new_key (an unconfirmed signing key is advertised) or
// error.
func (s *TangServerService) checkServer(ctx context.Context, srv *models.TangServer) {
	adv, err := s.fetchAdvertisement(ctx, srv.URL)
	if err != nil {
		if uerr := s.repo.UpdateTangCheck(ctx, srv.ID, "error", err.Error(), srv.AdvertisedThumbprints, nil); uerr != nil {
			log.Printf("WARNING: tang: store check error for %s: %v", srv.Name, uerr)
		}
		return
	}

	advertised := sortedKeys(adv.SigningKeys)

	// The advertisement is trusted when a signature by a trusted key
	// verified. Otherwise the server may have rotated past every key the
	// admin ever confirmed (or this is a different host).
	trustedSigner := false
	for _, thp := range adv.VerifiedBy {
		if slices.Contains(srv.TrustedThumbprints, thp) {
			trustedSigner = true
			break
		}
	}

	status := "ok"
	checkErr := ""
	var advJSON []byte
	switch {
	case !trustedSigner:
		status = "new_key"
		checkErr = "the advertisement is not signed by any trusted key; confirm the new thumbprint(s)"
	default:
		for _, thp := range advertised {
			if !slices.Contains(srv.TrustedThumbprints, thp) {
				status = "new_key"
				checkErr = "the server advertises a signing key that has not been confirmed yet"
				break
			}
		}
		// Persist the verified advertisement only when a trusted key signed it.
		advJSON = adv.Raw
	}

	prevStatus := srv.LastCheckStatus
	if err := s.repo.UpdateTangCheck(ctx, srv.ID, status, checkErr, advertised, advJSON); err != nil {
		log.Printf("WARNING: tang: store check result for %s: %v", srv.Name, err)
		return
	}
	if status != prevStatus {
		s.emitCheckAudit(ctx, srv, status, checkErr, advertised)
	}
}

func (s *TangServerService) emitCheckAudit(ctx context.Context, srv *models.TangServer, status, checkErr string, advertised []string) {
	if s.auditSvc == nil {
		return
	}
	outcome := auditpb.Outcome_OUTCOME_SUCCESS
	if status == "error" {
		outcome = auditpb.Outcome_OUTCOME_FAILURE
	}
	body, _ := json.Marshal(map[string]interface{}{
		"status":     status,
		"error":      checkErr,
		"advertised": advertised,
	})
	s.auditSvc.Emit(ctx, &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      &auditpb.Actor{Username: "tang-check:" + srv.Name},
		Action:     "tangserver.check",
		Resource:   &auditpb.Resource{Type: "tang_server", Id: srv.ID, Name: srv.Name},
		Outcome:    outcome,
		Payload: &auditpb.AuditEvent_HttpChange{
			HttpChange: &auditpb.HttpPayload{BodyJson: string(body)},
		},
	})
}

// fetchAdvertisement GETs <url>/adv with a size cap and parses/verifies it.
// baseURL is re-normalized here so the request is always built from the
// validated, rebuilt form regardless of the caller (registry row or probe
// input), and the dialer refuses blocked addresses on top of that.
func (s *TangServerService) fetchAdvertisement(ctx context.Context, baseURL string) (*tang.Advertisement, error) {
	base, err := normalizeTangURL(baseURL)
	if err != nil {
		return nil, diskEncInvalid("%v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/adv", http.NoBody)
	if err != nil {
		return nil, diskEncInvalid("invalid URL: %v", err)
	}
	req.Header.Set("Accept", "application/jose+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s/adv: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s/adv: unexpected status %s", base, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, tangAdvMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s/adv: %w", base, err)
	}
	if int64(len(body)) > tangAdvMaxBytes {
		return nil, fmt.Errorf("advertisement from %s exceeds %d bytes", base, tangAdvMaxBytes)
	}
	adv, err := tang.ParseAdvertisement(body)
	if err != nil {
		return nil, err
	}
	return adv, nil
}

func sortedKeys(m map[string]*tang.JWK) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
