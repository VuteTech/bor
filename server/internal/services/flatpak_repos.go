// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/flatpakcatalog"
	"github.com/VuteTech/Bor/server/internal/models"
)

// Errors surfaced by FlatpakRepoService. Handlers map them to HTTP codes.
var (
	ErrFlatpakRepoBuiltin  = errors.New("built-in repositories cannot be deleted; disable the catalog instead")
	ErrFlatpakRepoNotFound = database.ErrFlatpakRepoNotFound
	ErrFlatpakRepoConflict = database.ErrFlatpakRepoConflict
	ErrFlatpakAppNotFound  = database.ErrFlatpakAppNotFound
	ErrFlatpakIconNotFound = errors.New("no icon available for this application")
)

// FlatpakRepoValidationError is returned for invalid admin input (HTTP 400).
type FlatpakRepoValidationError struct{ Msg string }

func (e *FlatpakRepoValidationError) Error() string { return e.Msg }

func flatpakInvalid(format string, args ...interface{}) error {
	return &FlatpakRepoValidationError{Msg: fmt.Sprintf(format, args...)}
}

var (
	flatpakRepoArches       = map[string]struct{}{"x86_64": {}, "aarch64": {}, "i686": {}}
	flatpakRepoNameRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	flatpakProbeMaxBytes    = int64(64 * 1024)
	flatpakIconMaxBytes     = int64(256 * 1024)
	flatpakIconTTL          = 7 * 24 * time.Hour
	flatpakRepoMinInterval  = 3600
	flatpakRepoMaxGPGKeyLen = 64 * 1024
)

// FlatpakRepoService manages the server-side Flatpak repositories and their
// indexed application catalog (Settings → Flatpak repositories).
type FlatpakRepoService struct {
	repo    *database.FlatpakCatalogRepository
	manager *flatpakcatalog.Manager
	fetcher *flatpakcatalog.Fetcher
}

// NewFlatpakRepoService creates a new FlatpakRepoService.
func NewFlatpakRepoService(repo *database.FlatpakCatalogRepository, manager *flatpakcatalog.Manager, fetcher *flatpakcatalog.Fetcher) *FlatpakRepoService {
	return &FlatpakRepoService{repo: repo, manager: manager, fetcher: fetcher}
}

// RefreshEnabled reports whether outbound catalog fetches are allowed.
func (s *FlatpakRepoService) RefreshEnabled() bool {
	return s.manager != nil && s.manager.RefreshEnabled()
}

// List returns every registered repository (without key bytes).
func (s *FlatpakRepoService) List(ctx context.Context) (*models.FlatpakRepositoryListResponse, error) {
	repos, err := s.repo.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	if repos == nil {
		repos = []*models.FlatpakRepository{}
	}
	for _, r := range repos {
		s.decorate(r)
	}
	return &models.FlatpakRepositoryListResponse{Items: repos, RefreshEnabled: s.RefreshEnabled()}, nil
}

// Get returns one repository.
func (s *FlatpakRepoService) Get(ctx context.Context, id string) (*models.FlatpakRepository, error) {
	r, err := s.repo.GetRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	s.decorate(r)
	return r, nil
}

// decorate fills derived, non-persisted fields.
func (s *FlatpakRepoService) decorate(r *models.FlatpakRepository) {
	r.HasGPGKey = len(r.GPGKey) > 0
	if len(r.Arches) == 0 {
		r.Arches = []string{"x86_64"}
	}
	if !s.RefreshEnabled() && r.LastRefreshStatus == "never" {
		r.LastRefreshStatus = "disabled"
	}
}

// Create registers a repository. When a .flatpakrepo URL is given and the
// URL or key are missing, the file is fetched server-side and its fields fill
// the gaps.
func (s *FlatpakRepoService) Create(ctx context.Context, req *models.FlatpakRepositoryRequest, createdBy string) (*models.FlatpakRepository, error) {
	if req == nil {
		return nil, flatpakInvalid("request body is required")
	}
	if req.FlatpakrepoURL != "" && (req.URL == "" || req.GPGKeyData == "") {
		info, err := s.Probe(ctx, req.FlatpakrepoURL)
		if err != nil {
			return nil, err
		}
		if req.Name == "" {
			req.Name = info.Name
		}
		if req.URL == "" {
			req.URL = info.URL
		}
		if req.Title == "" {
			req.Title = info.Title
		}
		if req.Homepage == "" {
			req.Homepage = info.Homepage
		}
		if req.Comment == "" {
			req.Comment = info.Comment
		}
		if req.Description == "" {
			req.Description = info.Description
		}
		if req.IconURL == "" {
			req.IconURL = info.IconURL
		}
		if req.CollectionID == "" {
			req.CollectionID = info.CollectionID
		}
		if req.DefaultBranch == "" {
			req.DefaultBranch = info.DefaultBranch
		}
		if req.Subset == "" {
			req.Subset = info.Subset
		}
		if req.GPGKeyData == "" {
			req.GPGKeyData = info.GPGKeyData
		}
	}
	repo := &models.FlatpakRepository{CreatedBy: createdBy}
	if err := applyFlatpakRepoRequest(repo, req, true); err != nil {
		return nil, err
	}
	if err := s.repo.CreateRepo(ctx, repo); err != nil {
		return nil, err
	}
	return s.Get(ctx, repo.ID)
}

// Update changes the editable fields of a repository. An empty gpg_key_data
// keeps the stored key; clear_gpg_key removes it.
func (s *FlatpakRepoService) Update(ctx context.Context, id string, req *models.FlatpakRepositoryRequest) (*models.FlatpakRepository, error) {
	if req == nil {
		return nil, flatpakInvalid("request body is required")
	}
	repo, err := s.repo.GetRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	if repo.Builtin && req.Name != "" && req.Name != repo.Name {
		return nil, flatpakInvalid("the name of a built-in repository cannot be changed")
	}
	if err := applyFlatpakRepoRequest(repo, req, false); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateRepo(ctx, repo); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// applyFlatpakRepoRequest validates req and copies it onto repo.
func applyFlatpakRepoRequest(repo *models.FlatpakRepository, req *models.FlatpakRepositoryRequest, create bool) error {
	name := strings.TrimSpace(req.Name)
	if create || name != "" {
		if !flatpakRepoNameRE.MatchString(name) || strings.Contains(name, "..") {
			return flatpakInvalid("invalid repository name %q: must match [A-Za-z0-9][A-Za-z0-9._-]* and be at most 64 characters", name)
		}
		repo.Name = name
	}
	u := strings.TrimSpace(req.URL)
	if create || u != "" {
		if err := validateFlatpakRemoteURL(u); err != nil {
			return flatpakInvalid("%v", err)
		}
		repo.URL = u
	}
	if req.FlatpakrepoURL != "" {
		if _, err := flatpakcatalog.ValidateHTTPSURL(req.FlatpakrepoURL); err != nil {
			return flatpakInvalid("flatpakrepo_url: %v", err)
		}
	}
	repo.FlatpakrepoURL = strings.TrimSpace(req.FlatpakrepoURL)
	repo.Title = strings.TrimSpace(req.Title)
	repo.Homepage = strings.TrimSpace(req.Homepage)
	repo.Comment = strings.TrimSpace(req.Comment)
	repo.Description = strings.TrimSpace(req.Description)
	repo.IconURL = strings.TrimSpace(req.IconURL)
	if len(repo.Title) > 512 || len(repo.Comment) > 512 || len(repo.Description) > 4096 || len(repo.Homepage) > 2048 || len(repo.IconURL) > 2048 {
		return flatpakInvalid("title, comment, description, homepage or icon_url is too long")
	}
	if id := strings.TrimSpace(req.GPGKeyID); id != "" && !gpgFingerprintRe.MatchString(id) {
		return flatpakInvalid("gpg_key_id must be a 40-character hex fingerprint")
	}
	repo.GPGKeyID = strings.TrimSpace(req.GPGKeyID)
	switch {
	case req.ClearGPGKey:
		repo.GPGKey = nil
	case req.GPGKeyData != "":
		key, err := decodeFlatpakBase64(req.GPGKeyData)
		if err != nil {
			return flatpakInvalid("gpg_key_data is not valid base64")
		}
		if len(key) > flatpakRepoMaxGPGKeyLen {
			return flatpakInvalid("gpg_key_data exceeds %d bytes", flatpakRepoMaxGPGKeyLen)
		}
		repo.GPGKey = key
	}
	if c := strings.TrimSpace(req.CollectionID); c != "" && !flatpakCollectionRE.MatchString(c) {
		return flatpakInvalid("invalid collection_id %q", c)
	}
	repo.CollectionID = strings.TrimSpace(req.CollectionID)
	if b := strings.TrimSpace(req.DefaultBranch); b != "" && !flatpakBranchRE.MatchString(b) {
		return flatpakInvalid("invalid default_branch %q", b)
	}
	repo.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	if sub := strings.TrimSpace(req.Subset); sub != "" {
		if _, known := flatpakKnownSubsets[sub]; !known && !flatpakSubsetRE.MatchString(sub) {
			return flatpakInvalid("invalid subset %q", sub)
		}
	}
	repo.Subset = strings.TrimSpace(req.Subset)
	if a := strings.TrimSpace(req.AppstreamURL); a != "" {
		if _, err := flatpakcatalog.ValidateHTTPSURL(strings.ReplaceAll(a, "{arch}", "x86_64")); err != nil {
			return flatpakInvalid("appstream_url: %v", err)
		}
	}
	repo.AppstreamURL = strings.TrimSpace(req.AppstreamURL)

	arches := make([]string, 0, len(req.Arches))
	seen := map[string]bool{}
	for _, a := range req.Arches {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		if _, ok := flatpakRepoArches[a]; !ok {
			return flatpakInvalid("unsupported architecture %q (expected x86_64, aarch64 or i686)", a)
		}
		seen[a] = true
		arches = append(arches, a)
	}
	if len(arches) == 0 {
		arches = []string{"x86_64"}
	}
	repo.Arches = arches

	repo.CatalogEnabled = req.CatalogEnabled
	interval := req.RefreshIntervalS
	if interval == 0 {
		interval = 86400
	}
	if interval < flatpakRepoMinInterval {
		return flatpakInvalid("refresh_interval_s must be at least %d seconds", flatpakRepoMinInterval)
	}
	repo.RefreshIntervalS = interval
	return nil
}

func decodeFlatpakBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

// Delete removes a repository and its catalog. Built-in rows are protected.
func (s *FlatpakRepoService) Delete(ctx context.Context, id string) error {
	repo, err := s.repo.GetRepo(ctx, id)
	if err != nil {
		return err
	}
	if repo.Builtin {
		return ErrFlatpakRepoBuiltin
	}
	return s.repo.DeleteRepo(ctx, id)
}

// Refresh queues an immediate catalog refresh.
func (s *FlatpakRepoService) Refresh(ctx context.Context, id string) error {
	if s.manager == nil {
		return flatpakcatalog.ErrRefreshDisabled
	}
	return s.manager.RefreshNow(ctx, id)
}

// Upload indexes an admin-provided appstream.xml.gz for one architecture.
func (s *FlatpakRepoService) Upload(ctx context.Context, id, arch string, r io.Reader) (int, error) {
	if s.manager == nil {
		return 0, flatpakInvalid("catalog service unavailable")
	}
	arch = strings.TrimSpace(arch)
	if arch == "" {
		arch = "x86_64"
	}
	if _, ok := flatpakRepoArches[arch]; !ok {
		return 0, flatpakInvalid("unsupported architecture %q", arch)
	}
	return s.manager.Upload(ctx, id, arch, r)
}

// Probe fetches and parses a .flatpakrepo file.
func (s *FlatpakRepoService) Probe(ctx context.Context, flatpakrepoURL string) (*models.FlatpakRemoteInfo, error) {
	u, err := flatpakcatalog.ValidateHTTPSURL(flatpakrepoURL)
	if err != nil {
		return nil, flatpakInvalid("%v", err)
	}
	data, _, err := s.fetcher.GetSmall(ctx, u.String(), flatpakProbeMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", u.Redacted(), err)
	}
	rf, err := flatpakcatalog.ParseFlatpakrepo(data)
	if err != nil {
		return nil, flatpakInvalid("%s: %v", u.Redacted(), err)
	}
	info := &models.FlatpakRemoteInfo{
		Name:          flatpakNameFromURL(u),
		Title:         rf.Title,
		URL:           rf.URL,
		Homepage:      rf.Homepage,
		Comment:       rf.Comment,
		Description:   rf.Description,
		IconURL:       rf.Icon,
		CollectionID:  rf.CollectionID,
		DefaultBranch: rf.DefaultBranch,
		Subset:        rf.Subset,
	}
	if len(rf.GPGKey) > 0 {
		info.GPGKeyData = base64.StdEncoding.EncodeToString(rf.GPGKey)
	} else {
		info.Warning = "This .flatpakrepo file carries no GPG key. Add the remote's public key manually or disable verification explicitly."
	}
	if info.URL == "" {
		return nil, flatpakInvalid("%s: the file has no Url= entry", u.Redacted())
	}
	if err := validateFlatpakRemoteURL(info.URL); err != nil {
		return nil, flatpakInvalid("%s: %v", u.Redacted(), err)
	}
	return info, nil
}

// flatpakNameFromURL derives a remote name from the .flatpakrepo file name
// ("flathub-beta.flatpakrepo" → "flathub-beta").
func flatpakNameFromURL(u *url.URL) string {
	base := strings.TrimSuffix(path.Base(u.Path), ".flatpakrepo")
	base = strings.ToLower(base)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), ".-_")
	if name == "" || !flatpakRepoNameRE.MatchString(name) {
		return "remote"
	}
	return name
}

// CatalogRepos returns the policy-editor view of every repository, including
// the public GPG key so it can be copied into policies.
func (s *FlatpakRepoService) CatalogRepos(ctx context.Context) ([]models.FlatpakCatalogRepo, error) {
	repos, err := s.repo.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.FlatpakCatalogRepo, 0, len(repos))
	for _, r := range repos {
		s.decorate(r)
		item := models.FlatpakCatalogRepo{
			ID: r.ID, Name: r.Name, Title: r.Title, URL: r.URL, Homepage: r.Homepage, Comment: r.Comment,
			Subset: r.Subset, CollectionID: r.CollectionID, DefaultBranch: r.DefaultBranch,
			AppCount: r.AppCount, CatalogEnabled: r.CatalogEnabled, LastRefreshStatus: r.LastRefreshStatus,
		}
		if len(r.GPGKey) > 0 {
			item.GPGKeyData = base64.StdEncoding.EncodeToString(r.GPGKey)
		}
		out = append(out, item)
	}
	return out, nil
}

// SearchApps runs a paginated catalog search.
func (s *FlatpakRepoService) SearchApps(ctx context.Context, req *models.FlatpakCatalogSearchRequest) (*models.FlatpakCatalogSearchResponse, error) {
	if req == nil {
		req = &models.FlatpakCatalogSearchRequest{}
	}
	if len(req.Search) > 200 {
		return nil, flatpakInvalid("search term is too long")
	}
	if req.Repo != "" && !flatpakRepoNameRE.MatchString(req.Repo) {
		return nil, flatpakInvalid("invalid repository name")
	}
	if req.Arch != "" {
		if _, ok := flatpakRepoArches[req.Arch]; !ok {
			return nil, flatpakInvalid("unsupported architecture %q", req.Arch)
		}
	}
	page, perPage := models.ClampPagination(req.Page, req.PerPage)
	req.Page, req.PerPage = page, perPage
	items, total, err := s.repo.SearchApps(ctx, req)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []*models.FlatpakCatalogApp{}
	}
	totalPages := 0
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	return &models.FlatpakCatalogSearchResponse{Items: items, Total: total, Page: page, PerPage: perPage, TotalPages: totalPages}, nil
}

// GetApp returns the catalog detail of one application.
func (s *FlatpakRepoService) GetApp(ctx context.Context, repoName, appID string) (*models.FlatpakCatalogAppDetail, error) {
	if !flatpakRepoNameRE.MatchString(repoName) || !flatpakAppIDRE.MatchString(appID) {
		return nil, ErrFlatpakAppNotFound
	}
	return s.repo.GetApp(ctx, repoName, appID)
}

// Icon returns the cached icon of an application, fetching it from the remote
// on first use (host allowlisted to the repository URL, 256 KiB cap, PNG
// only). A miss is cached negatively for the TTL.
func (s *FlatpakRepoService) Icon(ctx context.Context, repoName, appID string) (mime string, data []byte, err error) {
	if !flatpakRepoNameRE.MatchString(repoName) || !flatpakAppIDRE.MatchString(appID) {
		return "", nil, ErrFlatpakIconNotFound
	}
	repo, err := s.repo.GetRepoByName(ctx, repoName)
	if err != nil {
		return "", nil, err
	}
	if cached, cerr := s.repo.GetIcon(ctx, repo.ID, appID); cerr == nil && cached != nil && time.Since(cached.FetchedAt) < flatpakIconTTL {
		if cached.Mime == "" {
			return "", nil, ErrFlatpakIconNotFound
		}
		return cached.Mime, cached.Data, nil
	}
	arch, iconFile, err := s.repo.GetAppIconFile(ctx, repo.ID, appID)
	if err != nil {
		if errors.Is(err, database.ErrFlatpakAppNotFound) {
			_ = s.repo.PutIcon(ctx, repo.ID, appID, "", nil)
			return "", nil, ErrFlatpakIconNotFound
		}
		return "", nil, err
	}
	if !s.RefreshEnabled() {
		return "", nil, ErrFlatpakIconNotFound
	}
	iconURL, err := flatpakIconURL(repo, arch, iconFile)
	if err != nil {
		return "", nil, ErrFlatpakIconNotFound
	}
	body, ctype, err := s.fetcher.GetSmall(ctx, iconURL, flatpakIconMaxBytes)
	if err != nil {
		if errors.Is(err, flatpakcatalog.ErrNotFound) || errors.Is(err, flatpakcatalog.ErrTooLarge) {
			_ = s.repo.PutIcon(ctx, repo.ID, appID, "", nil)
		}
		return "", nil, ErrFlatpakIconNotFound
	}
	mime = sniffFlatpakIcon(body, ctype)
	if mime == "" {
		_ = s.repo.PutIcon(ctx, repo.ID, appID, "", nil)
		return "", nil, ErrFlatpakIconNotFound
	}
	_ = s.repo.PutIcon(ctx, repo.ID, appID, mime, body)
	return mime, body, nil
}

// flatpakIconURL builds <repo>/appstream/<arch>/icons/64x64/<file>. The file
// name comes from the remote's own AppStream data and is restricted to a
// single path component.
func flatpakIconURL(repo *models.FlatpakRepository, arch, iconFile string) (string, error) {
	if strings.ContainsAny(iconFile, "/\\") || iconFile == "" || iconFile == "." || iconFile == ".." {
		return "", errors.New("invalid icon file name")
	}
	if _, ok := flatpakRepoArches[arch]; !ok {
		return "", errors.New("invalid arch")
	}
	base := strings.TrimRight(repo.URL, "/")
	if strings.HasPrefix(strings.ToLower(base), "oci+") {
		return "", errors.New("OCI remotes publish no icon tree")
	}
	return base + "/appstream/" + arch + "/icons/64x64/" + url.PathEscape(iconFile), nil
}

// sniffFlatpakIcon returns "image/png" for a PNG payload and "" for anything
// else. SVG is deliberately rejected: an SVG served from the UI origin can
// carry scripts, and Flatpak remotes publish their icon trees as PNG.
func sniffFlatpakIcon(body []byte, _ string) string {
	if bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	return ""
}
