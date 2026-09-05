// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Errors returned by RefreshNow / Upload.
var (
	ErrRefreshDisabled = errors.New("catalog refresh is disabled on this server (BOR_FLATPAK_CATALOG_REFRESH=false)")
	ErrCatalogDisabled = errors.New("catalog indexing is disabled for this repository")
	ErrNoAppstreamURL  = errors.New("this remote has no plain-HTTP AppStream catalog; set appstream_url or upload the catalog")
)

// AuditEmitter is the subset of services.AuditService the manager needs.
type AuditEmitter interface {
	Emit(ctx context.Context, event *auditpb.AuditEvent)
}

// Manager schedules and runs catalog refreshes. One scheduler goroutine scans
// the repository table every scanInterval and refreshes the repositories that
// are due; RefreshNow and Upload share the same per-repository mutex so a
// manual action never overlaps a scheduled one.
type Manager struct {
	repo           *database.FlatpakCatalogRepository
	fetcher        *Fetcher
	audit          AuditEmitter
	refreshTotal   *prometheus.CounterVec
	refreshEnabled bool

	startDelay   time.Duration
	scanInterval time.Duration

	mu          sync.Mutex
	locks       map[string]*sync.Mutex
	lastAttempt map[string]time.Time

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewManager builds a Manager. audit and refreshTotal may be nil.
func NewManager(repo *database.FlatpakCatalogRepository, fetcher *Fetcher, audit AuditEmitter, refreshTotal *prometheus.CounterVec, refreshEnabled bool) *Manager {
	return &Manager{
		repo:           repo,
		fetcher:        fetcher,
		audit:          audit,
		refreshTotal:   refreshTotal,
		refreshEnabled: refreshEnabled,
		startDelay:     30 * time.Second,
		scanInterval:   time.Minute,
		locks:          make(map[string]*sync.Mutex),
		lastAttempt:    make(map[string]time.Time),
	}
}

// RefreshEnabled reports whether outbound catalog fetches are allowed.
func (m *Manager) RefreshEnabled() bool { return m.refreshEnabled }

// Start launches the scheduler. Stop cancels it and waits for in-flight work.
func (m *Manager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.wg.Add(1)
	go m.loop()
}

// Stop cancels the scheduler and any running refresh and waits for them.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Manager) background() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *Manager) loop() {
	defer m.wg.Done()
	timer := time.NewTimer(m.startDelay)
	defer timer.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-timer.C:
		}
		m.runDue()
		timer.Reset(m.scanInterval)
	}
}

// runDue refreshes every enabled repository whose interval has elapsed since
// its last successful refresh and since its last attempt (so a failing remote
// is retried at its interval, not every scan).
func (m *Manager) runDue() {
	if !m.refreshEnabled {
		return
	}
	repos, err := m.repo.ListRepos(m.ctx)
	if err != nil {
		log.Printf("flatpak catalog: list repositories: %v", err)
		return
	}
	now := time.Now()
	for _, r := range repos {
		if m.ctx.Err() != nil {
			return
		}
		if !r.CatalogEnabled {
			continue
		}
		interval := time.Duration(r.RefreshIntervalS) * time.Second
		if interval < time.Hour {
			interval = time.Hour
		}
		m.mu.Lock()
		attempt, attempted := m.lastAttempt[r.ID]
		m.mu.Unlock()
		if attempted && now.Sub(attempt) < interval {
			continue
		}
		if r.LastRefreshAt != nil && now.Sub(*r.LastRefreshAt) < interval {
			continue
		}
		m.refresh(m.ctx, r)
	}
}

// RefreshNow queues an immediate refresh of one repository and returns
// without waiting for it (HTTP 202 semantics).
func (m *Manager) RefreshNow(ctx context.Context, id string) error {
	if !m.refreshEnabled {
		return ErrRefreshDisabled
	}
	repo, err := m.repo.GetRepo(ctx, id)
	if err != nil {
		return err
	}
	if !repo.CatalogEnabled {
		return ErrCatalogDisabled
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.refresh(m.background(), repo)
	}()
	return nil
}

func (m *Manager) lockFor(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	mu, ok := m.locks[id]
	if !ok {
		mu = &sync.Mutex{}
		m.locks[id] = mu
	}
	return mu
}

func (m *Manager) markAttempt(id string) {
	m.mu.Lock()
	m.lastAttempt[id] = time.Now()
	m.mu.Unlock()
}

// AppstreamURL returns the catalog URL for one arch of a repository.
func AppstreamURL(repo *models.FlatpakRepository, arch string) (string, error) {
	if repo.AppstreamURL != "" {
		return strings.ReplaceAll(repo.AppstreamURL, "{arch}", arch), nil
	}
	lower := strings.ToLower(repo.URL)
	if strings.HasPrefix(lower, "oci+") {
		return "", ErrNoAppstreamURL
	}
	return strings.TrimRight(repo.URL, "/") + "/appstream/" + arch + "/appstream.xml.gz", nil
}

// refresh downloads and indexes every arch of one repository.
func (m *Manager) refresh(ctx context.Context, repo *models.FlatpakRepository) {
	mu := m.lockFor(repo.ID)
	mu.Lock()
	defer mu.Unlock()
	m.markAttempt(repo.ID)

	if err := m.repo.UpdateRefresh(ctx, repo.ID, database.FlatpakRefreshUpdate{Status: "running"}); err != nil {
		log.Printf("flatpak catalog: %s: mark running: %v", repo.Name, err)
	}

	arches := repo.Arches
	if len(arches) == 0 {
		arches = []string{"x86_64"}
	}
	singleArch := len(arches) == 1
	var (
		total     int
		changed   bool
		unchanged int
		etag      = repo.LastRefreshETag
		modified  = repo.LastRefreshModified
	)
	for _, arch := range arches {
		if err := ctx.Err(); err != nil {
			m.fail(ctx, repo, arches, fmt.Errorf("cancelled: %w", err))
			return
		}
		u, err := AppstreamURL(repo, arch)
		if err != nil {
			m.fail(ctx, repo, arches, err)
			return
		}
		condETag, condModified := "", ""
		if singleArch {
			condETag, condModified = repo.LastRefreshETag, repo.LastRefreshModified
		}
		res, err := m.fetcher.OpenConditional(ctx, u, condETag, condModified)
		if err != nil {
			m.fail(ctx, repo, arches, err)
			return
		}
		if res.NotModified {
			unchanged++
			continue
		}
		entries, err := ParseAppStreamGzip(res.Body, m.fetcher.MaxDecompressedBytes())
		_ = res.Body.Close()
		if err != nil {
			m.fail(ctx, repo, arches, fmt.Errorf("%s: %w", arch, err))
			return
		}
		n, err := m.repo.ReplaceCatalog(ctx, repo.ID, arch, entries)
		if err != nil {
			m.fail(ctx, repo, arches, err)
			return
		}
		total = n
		changed = true
		etag, modified = res.ETag, res.LastModified
	}

	now := time.Now()
	upd := database.FlatpakRefreshUpdate{Status: "ok", At: &now}
	if singleArch {
		upd.ETag, upd.Modified = &etag, &modified
	}
	if err := m.repo.UpdateRefresh(ctx, repo.ID, upd); err != nil {
		log.Printf("flatpak catalog: %s: record success: %v", repo.Name, err)
	}
	outcome := "ok"
	if !changed {
		outcome = "unchanged"
		total = repo.AppCount
	}
	m.count(repo.Name, outcome)
	m.emitAudit(ctx, repo, "flatpakrepo.refresh", true, map[string]interface{}{
		"status": outcome, "apps": total, "arches": arches, "unchanged_arches": unchanged,
	})
	log.Printf("flatpak catalog: %s refreshed (%s, %d apps)", repo.Name, outcome, total)
}

func (m *Manager) fail(ctx context.Context, repo *models.FlatpakRepository, arches []string, cause error) {
	msg := cause.Error()
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	if err := m.repo.UpdateRefresh(ctx, repo.ID, database.FlatpakRefreshUpdate{Status: "error", Error: msg}); err != nil {
		log.Printf("flatpak catalog: %s: record failure: %v", repo.Name, err)
	}
	m.count(repo.Name, "error")
	m.emitAudit(ctx, repo, "flatpakrepo.refresh", false, map[string]interface{}{
		"status": "error", "error": msg, "arches": arches,
	})
	log.Printf("flatpak catalog: %s refresh failed: %v", repo.Name, cause)
}

// Upload indexes an admin-provided appstream.xml.gz for one arch (air-gapped
// deployments). It runs synchronously under the repository's mutex.
func (m *Manager) Upload(ctx context.Context, repoID, arch string, r io.Reader) (int, error) {
	repo, err := m.repo.GetRepo(ctx, repoID)
	if err != nil {
		return 0, err
	}
	mu := m.lockFor(repo.ID)
	mu.Lock()
	defer mu.Unlock()

	entries, err := ParseAppStreamGzip(r, m.fetcher.MaxDecompressedBytes())
	if err != nil {
		m.count(repo.Name, "error")
		return 0, err
	}
	n, err := m.repo.ReplaceCatalog(ctx, repo.ID, arch, entries)
	if err != nil {
		m.count(repo.Name, "error")
		return 0, err
	}
	now := time.Now()
	if err := m.repo.UpdateRefresh(ctx, repo.ID, database.FlatpakRefreshUpdate{Status: "ok", At: &now}); err != nil {
		log.Printf("flatpak catalog: %s: record upload: %v", repo.Name, err)
	}
	m.count(repo.Name, "upload")
	log.Printf("flatpak catalog: %s catalog uploaded (%s, %d apps)", repo.Name, arch, n)
	return n, nil
}

func (m *Manager) count(repoName, outcome string) {
	if m.refreshTotal != nil {
		m.refreshTotal.WithLabelValues(repoName, outcome).Inc()
	}
}

func (m *Manager) emitAudit(ctx context.Context, repo *models.FlatpakRepository, action string, success bool, details map[string]interface{}) {
	if m.audit == nil {
		return
	}
	outcome := auditpb.Outcome_OUTCOME_SUCCESS
	if !success {
		outcome = auditpb.Outcome_OUTCOME_FAILURE
	}
	body, _ := json.Marshal(details)
	m.audit.Emit(ctx, &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      &auditpb.Actor{Username: "flatpak-catalog:" + repo.Name},
		Action:     action,
		Resource:   &auditpb.Resource{Type: "flatpak-repos", Id: repo.ID},
		Outcome:    outcome,
		Payload: &auditpb.AuditEvent_HttpChange{
			HttpChange: &auditpb.HttpPayload{BodyJson: string(body)},
		},
	})
}
