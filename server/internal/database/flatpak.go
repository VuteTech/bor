// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/lib/pq"
)

// Sentinel errors returned by FlatpakCatalogRepository.
var (
	ErrFlatpakRepoNotFound = errors.New("flatpak repository not found")
	ErrFlatpakRepoConflict = errors.New("a flatpak repository with that name already exists")
	ErrFlatpakAppNotFound  = errors.New("application not found in the flatpak catalog")
)

// FlatpakCatalogRepository handles flatpak_repositories, flatpak_catalog_apps
// and flatpak_catalog_icons.
type FlatpakCatalogRepository struct {
	db *DB
}

// NewFlatpakCatalogRepository creates a new FlatpakCatalogRepository.
func NewFlatpakCatalogRepository(db *DB) *FlatpakCatalogRepository {
	return &FlatpakCatalogRepository{db: db}
}

// FlatpakRefreshUpdate describes which refresh bookkeeping columns to update.
// Nil pointer fields are left untouched.
type FlatpakRefreshUpdate struct {
	Status   string
	Error    string
	At       *time.Time // last successful refresh
	ETag     *string
	Modified *string
}

// FlatpakIcon is a cached catalog icon. An empty Mime marks a negative cache
// entry (the remote had no icon for the app).
type FlatpakIcon struct {
	Mime      string
	Data      []byte
	FetchedAt time.Time
}

const flatpakRepoColumns = `id, name, title, url, flatpakrepo_url, homepage, comment, description, icon_url,
	gpg_key, gpg_key_id, collection_id, default_branch, subset, appstream_url, arches, catalog_enabled,
	refresh_interval_s, builtin, last_refresh_at, last_refresh_status, last_refresh_error,
	last_refresh_etag, last_refresh_modified, app_count, created_by, created_at, updated_at`

type flatpakRowScanner interface {
	Scan(dest ...interface{}) error
}

func scanFlatpakRepo(s flatpakRowScanner) (*models.FlatpakRepository, error) {
	var (
		r       models.FlatpakRepository
		arches  string
		lastAt  sql.NullTime
		gpgKey  []byte
		created time.Time
		updated time.Time
	)
	if err := s.Scan(
		&r.ID, &r.Name, &r.Title, &r.URL, &r.FlatpakrepoURL, &r.Homepage, &r.Comment, &r.Description, &r.IconURL,
		&gpgKey, &r.GPGKeyID, &r.CollectionID, &r.DefaultBranch, &r.Subset, &r.AppstreamURL, &arches, &r.CatalogEnabled,
		&r.RefreshIntervalS, &r.Builtin, &lastAt, &r.LastRefreshStatus, &r.LastRefreshError,
		&r.LastRefreshETag, &r.LastRefreshModified, &r.AppCount, &r.CreatedBy, &created, &updated,
	); err != nil {
		return nil, err
	}
	r.GPGKey = gpgKey
	r.HasGPGKey = len(gpgKey) > 0
	r.Arches = splitFlatpakList(arches, ",")
	if lastAt.Valid {
		t := lastAt.Time
		r.LastRefreshAt = &t
	}
	r.CreatedAt = created
	r.UpdatedAt = updated
	return &r, nil
}

func splitFlatpakList(s, sep string) []string {
	out := []string{}
	for _, part := range strings.Split(s, sep) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinFlatpakList(items []string, sep string) string {
	var parts []string
	for _, it := range items {
		if p := strings.TrimSpace(it); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, sep)
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// ListRepos returns every registered repository, built-in first.
func (r *FlatpakCatalogRepository) ListRepos(ctx context.Context) ([]*models.FlatpakRepository, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+flatpakRepoColumns+` FROM flatpak_repositories ORDER BY builtin DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("flatpak: list repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	repos := []*models.FlatpakRepository{}
	for rows.Next() {
		repo, err := scanFlatpakRepo(rows)
		if err != nil {
			return nil, fmt.Errorf("flatpak: scan repository: %w", err)
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

// GetRepo returns one repository by ID.
func (r *FlatpakCatalogRepository) GetRepo(ctx context.Context, id string) (*models.FlatpakRepository, error) {
	repo, err := scanFlatpakRepo(r.db.QueryRowContext(ctx,
		`SELECT `+flatpakRepoColumns+` FROM flatpak_repositories WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFlatpakRepoNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("flatpak: get repository: %w", err)
	}
	return repo, nil
}

// GetRepoByName returns one repository by its remote name.
func (r *FlatpakCatalogRepository) GetRepoByName(ctx context.Context, name string) (*models.FlatpakRepository, error) {
	repo, err := scanFlatpakRepo(r.db.QueryRowContext(ctx,
		`SELECT `+flatpakRepoColumns+` FROM flatpak_repositories WHERE name = $1`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFlatpakRepoNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("flatpak: get repository by name: %w", err)
	}
	return repo, nil
}

// CreateRepo inserts a repository and fills in ID and timestamps.
func (r *FlatpakCatalogRepository) CreateRepo(ctx context.Context, repo *models.FlatpakRepository) error {
	if repo.LastRefreshStatus == "" {
		repo.LastRefreshStatus = "never"
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO flatpak_repositories
		  (name, title, url, flatpakrepo_url, homepage, comment, description, icon_url, gpg_key, gpg_key_id,
		   collection_id, default_branch, subset, appstream_url, arches, catalog_enabled, refresh_interval_s,
		   builtin, last_refresh_status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		RETURNING id, created_at, updated_at`,
		repo.Name, repo.Title, repo.URL, repo.FlatpakrepoURL, repo.Homepage, repo.Comment, repo.Description, repo.IconURL,
		nullableBytes(repo.GPGKey), repo.GPGKeyID, repo.CollectionID, repo.DefaultBranch, repo.Subset, repo.AppstreamURL,
		joinFlatpakList(repo.Arches, ","), repo.CatalogEnabled, repo.RefreshIntervalS, repo.Builtin,
		repo.LastRefreshStatus, repo.CreatedBy,
	).Scan(&repo.ID, &repo.CreatedAt, &repo.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrFlatpakRepoConflict
	}
	if err != nil {
		return fmt.Errorf("flatpak: create repository: %w", err)
	}
	repo.HasGPGKey = len(repo.GPGKey) > 0
	return nil
}

// UpdateRepo updates the editable fields of a repository.
func (r *FlatpakCatalogRepository) UpdateRepo(ctx context.Context, repo *models.FlatpakRepository) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE flatpak_repositories SET
		  name = $1, title = $2, url = $3, flatpakrepo_url = $4, homepage = $5, comment = $6, description = $7,
		  icon_url = $8, gpg_key = $9, gpg_key_id = $10, collection_id = $11, default_branch = $12, subset = $13,
		  appstream_url = $14, arches = $15, catalog_enabled = $16, refresh_interval_s = $17, updated_at = NOW()
		WHERE id = $18`,
		repo.Name, repo.Title, repo.URL, repo.FlatpakrepoURL, repo.Homepage, repo.Comment, repo.Description,
		repo.IconURL, nullableBytes(repo.GPGKey), repo.GPGKeyID, repo.CollectionID, repo.DefaultBranch, repo.Subset,
		repo.AppstreamURL, joinFlatpakList(repo.Arches, ","), repo.CatalogEnabled, repo.RefreshIntervalS, repo.ID,
	)
	if isUniqueViolation(err) {
		return ErrFlatpakRepoConflict
	}
	if err != nil {
		return fmt.Errorf("flatpak: update repository: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFlatpakRepoNotFound
	}
	return nil
}

// DeleteRepo removes a repository and, by cascade, its catalog rows.
func (r *FlatpakCatalogRepository) DeleteRepo(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM flatpak_repositories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("flatpak: delete repository: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFlatpakRepoNotFound
	}
	return nil
}

// UpdateRefresh records refresh bookkeeping for a repository.
func (r *FlatpakCatalogRepository) UpdateRefresh(ctx context.Context, id string, u FlatpakRefreshUpdate) error {
	sets := []string{"last_refresh_status = $1", "last_refresh_error = $2"}
	args := []interface{}{u.Status, u.Error}
	if u.At != nil {
		args = append(args, *u.At)
		sets = append(sets, fmt.Sprintf("last_refresh_at = $%d", len(args)))
	}
	if u.ETag != nil {
		args = append(args, *u.ETag)
		sets = append(sets, fmt.Sprintf("last_refresh_etag = $%d", len(args)))
	}
	if u.Modified != nil {
		args = append(args, *u.Modified)
		sets = append(sets, fmt.Sprintf("last_refresh_modified = $%d", len(args)))
	}
	args = append(args, id)
	query := fmt.Sprintf(`UPDATE flatpak_repositories SET %s WHERE id = $%d`, strings.Join(sets, ", "), len(args))
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("flatpak: update refresh status: %w", err)
	}
	return nil
}

// ReplaceCatalog upserts the parsed entries for one (repository, arch) and
// removes rows of that arch that were not part of this run. It returns the
// repository's new application count (distinct desktop + console apps).
func (r *FlatpakCatalogRepository) ReplaceCatalog(ctx context.Context, repoID, arch string, entries []models.FlatpakCatalogEntry) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("flatpak: begin catalog transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	started := time.Now()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO flatpak_catalog_apps
		  (repo_id, app_id, arch, branch, ref, kind, name, summary, description, developer, project_license,
		   homepage, categories, keywords, runtime, latest_version, latest_release_at, verified, icon_file,
		   content_rating, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (repo_id, app_id, arch, branch) DO UPDATE SET
		  ref = EXCLUDED.ref, kind = EXCLUDED.kind, name = EXCLUDED.name, summary = EXCLUDED.summary,
		  description = EXCLUDED.description, developer = EXCLUDED.developer, project_license = EXCLUDED.project_license,
		  homepage = EXCLUDED.homepage, categories = EXCLUDED.categories, keywords = EXCLUDED.keywords,
		  runtime = EXCLUDED.runtime, latest_version = EXCLUDED.latest_version, latest_release_at = EXCLUDED.latest_release_at,
		  verified = EXCLUDED.verified, icon_file = EXCLUDED.icon_file, content_rating = EXCLUDED.content_rating,
		  updated_at = EXCLUDED.updated_at`)
	if err != nil {
		return 0, fmt.Errorf("flatpak: prepare catalog upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	seen := make(map[string]struct{}, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.Arch != arch {
			continue
		}
		key := e.AppID + "|" + e.Branch
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		var releaseAt interface{}
		if e.LatestReleaseAt != nil {
			releaseAt = *e.LatestReleaseAt
		}
		if _, err := stmt.ExecContext(ctx,
			repoID, e.AppID, e.Arch, e.Branch, e.Ref, e.Kind, e.Name, e.Summary, e.Description, e.Developer,
			e.ProjectLicense, e.Homepage, joinFlatpakList(e.Categories, ";"), joinFlatpakList(e.Keywords, " "),
			e.Runtime, e.LatestVersion, releaseAt, e.Verified, e.IconFile, e.ContentRating, started,
		); err != nil {
			return 0, fmt.Errorf("flatpak: upsert %s: %w", e.AppID, err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM flatpak_catalog_apps WHERE repo_id = $1 AND arch = $2 AND updated_at < $3`,
		repoID, arch, started); err != nil {
		return 0, fmt.Errorf("flatpak: prune stale catalog rows: %w", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT app_id) FROM flatpak_catalog_apps
		WHERE repo_id = $1 AND kind IN ('desktop-application', 'console-application')`, repoID).Scan(&count); err != nil {
		return 0, fmt.Errorf("flatpak: count catalog apps: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE flatpak_repositories SET app_count = $1 WHERE id = $2`, count, repoID); err != nil {
		return 0, fmt.Errorf("flatpak: update app count: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("flatpak: commit catalog: %w", err)
	}
	return count, nil
}

// flatpakDefaultKinds is the kind filter applied when a search names none.
var flatpakDefaultKinds = []string{"desktop-application", "console-application"}

// buildFlatpakTsQuery turns free text into a prefix-matching tsquery for the
// 'simple' dictionary. Only [a-z0-9] runs survive, so the result is always a
// syntactically valid query and never contains operators from user input.
func buildFlatpakTsQuery(search string) string {
	var terms []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			terms = append(terms, cur.String()+":*")
			cur.Reset()
		}
	}
	for _, c := range strings.ToLower(search) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			cur.WriteRune(c)
		default:
			flush()
		}
	}
	flush()
	if len(terms) > 8 {
		terms = terms[:8]
	}
	return strings.Join(terms, " & ")
}

func escapeFlatpakLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// buildFlatpakCatalogFilter returns the WHERE clause, its bind args and the
// rank expression for a catalog search. Every value is a bind parameter.
func buildFlatpakCatalogFilter(req *models.FlatpakCatalogSearchRequest) (where string, args []interface{}, rank string) {
	conds := []string{"r.catalog_enabled = true"}
	if req.Repo != "" {
		args = append(args, req.Repo)
		conds = append(conds, fmt.Sprintf("r.name = $%d", len(args)))
	}
	kinds := req.Kinds
	if len(kinds) == 0 {
		kinds = flatpakDefaultKinds
	}
	args = append(args, pq.Array(kinds))
	conds = append(conds, fmt.Sprintf("a.kind = ANY($%d)", len(args)))
	if req.Verified != nil {
		args = append(args, *req.Verified)
		conds = append(conds, fmt.Sprintf("a.verified = $%d", len(args)))
	}
	if req.Arch != "" {
		args = append(args, req.Arch)
		conds = append(conds, fmt.Sprintf("a.arch = $%d", len(args)))
	}
	rank = "0"
	if s := strings.TrimSpace(req.Search); s != "" {
		args = append(args, "%"+escapeFlatpakLike(s)+"%")
		likeIdx := len(args)
		if tsq := buildFlatpakTsQuery(s); tsq != "" {
			args = append(args, tsq)
			tsIdx := len(args)
			conds = append(conds, fmt.Sprintf("(a.search_tsv @@ to_tsquery('simple', $%d) OR a.app_id ILIKE $%d)", tsIdx, likeIdx))
			rank = fmt.Sprintf("ts_rank(a.search_tsv, to_tsquery('simple', $%d))", tsIdx)
		} else {
			conds = append(conds, fmt.Sprintf("a.app_id ILIKE $%d", likeIdx))
		}
	}
	return "WHERE " + strings.Join(conds, " AND "), args, rank
}

const flatpakCatalogFrom = `FROM flatpak_catalog_apps a JOIN flatpak_repositories r ON r.id = a.repo_id`

// SearchApps returns one page of catalog hits and the total number of hits.
// Without an arch filter one row per (repo, app, branch) is returned,
// preferring x86_64.
func (r *FlatpakCatalogRepository) SearchApps(ctx context.Context, req *models.FlatpakCatalogSearchRequest) ([]*models.FlatpakCatalogApp, int, error) {
	where, args, rank := buildFlatpakCatalogFilter(req)

	var total int
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT DISTINCT a.repo_id, a.app_id, a.branch %s %s) c`, flatpakCatalogFrom, where)
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("flatpak: count catalog search: %w", err)
	}

	page, perPage := models.ClampPagination(req.Page, req.PerPage)
	pageArgs := append(append([]interface{}{}, args...), perPage, (page-1)*perPage)
	query := fmt.Sprintf(`
		SELECT repo_id, repo_name, app_id, arch, branch, ref, kind, name, summary, developer, project_license,
		       homepage, categories, latest_version, latest_release_at, verified, runtime, has_icon
		FROM (
		  SELECT DISTINCT ON (a.repo_id, a.app_id, a.branch)
		    a.repo_id, r.name AS repo_name, a.app_id, a.arch, a.branch, a.ref, a.kind, a.name, a.summary,
		    a.developer, a.project_license, a.homepage, a.categories, a.latest_version, a.latest_release_at,
		    a.verified, a.runtime, (a.icon_file <> '') AS has_icon, %s AS rank
		  %s %s
		  ORDER BY a.repo_id, a.app_id, a.branch, (a.arch = 'x86_64') DESC, a.arch
		) d
		ORDER BY rank DESC, name ASC, app_id ASC, repo_name ASC
		LIMIT $%d OFFSET $%d`, rank, flatpakCatalogFrom, where, len(pageArgs)-1, len(pageArgs))

	rows, err := r.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("flatpak: catalog search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []*models.FlatpakCatalogApp{}
	for rows.Next() {
		app, err := scanFlatpakCatalogApp(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("flatpak: scan catalog hit: %w", err)
		}
		items = append(items, app)
	}
	return items, total, rows.Err()
}

func scanFlatpakCatalogApp(s flatpakRowScanner) (*models.FlatpakCatalogApp, error) {
	var (
		a          models.FlatpakCatalogApp
		categories string
		releaseAt  sql.NullTime
	)
	if err := s.Scan(&a.RepoID, &a.RepoName, &a.AppID, &a.Arch, &a.Branch, &a.Ref, &a.Kind, &a.Name, &a.Summary,
		&a.Developer, &a.ProjectLicense, &a.Homepage, &categories, &a.LatestVersion, &releaseAt, &a.Verified,
		&a.Runtime, &a.HasIcon); err != nil {
		return nil, err
	}
	a.Categories = splitFlatpakList(categories, ";")
	if releaseAt.Valid {
		t := releaseAt.Time
		a.LatestReleaseAt = &t
	}
	return &a, nil
}

// GetApp returns the detail view of one catalog app (prefers x86_64/stable)
// together with the list of branches known for it.
func (r *FlatpakCatalogRepository) GetApp(ctx context.Context, repoName, appID string) (*models.FlatpakCatalogAppDetail, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.repo_id, r.name, a.app_id, a.arch, a.branch, a.ref, a.kind, a.name, a.summary, a.developer,
		       a.project_license, a.homepage, a.categories, a.latest_version, a.latest_release_at, a.verified,
		       a.runtime, (a.icon_file <> ''), a.description, a.keywords, a.content_rating
		`+flatpakCatalogFrom+`
		WHERE r.name = $1 AND a.app_id = $2
		ORDER BY (a.arch = 'x86_64') DESC, (a.branch = 'stable') DESC, a.branch ASC, a.arch ASC`, repoName, appID)
	if err != nil {
		return nil, fmt.Errorf("flatpak: get catalog app: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var detail *models.FlatpakCatalogAppDetail
	branchSeen := map[string]bool{}
	for rows.Next() {
		var (
			a          models.FlatpakCatalogApp
			categories string
			releaseAt  sql.NullTime
			desc       string
			keywords   string
			rating     string
		)
		if err := rows.Scan(&a.RepoID, &a.RepoName, &a.AppID, &a.Arch, &a.Branch, &a.Ref, &a.Kind, &a.Name, &a.Summary,
			&a.Developer, &a.ProjectLicense, &a.Homepage, &categories, &a.LatestVersion, &releaseAt, &a.Verified,
			&a.Runtime, &a.HasIcon, &desc, &keywords, &rating); err != nil {
			return nil, fmt.Errorf("flatpak: scan catalog app: %w", err)
		}
		if detail == nil {
			a.Categories = splitFlatpakList(categories, ";")
			if releaseAt.Valid {
				t := releaseAt.Time
				a.LatestReleaseAt = &t
			}
			detail = &models.FlatpakCatalogAppDetail{
				FlatpakCatalogApp: a,
				Description:       desc,
				Keywords:          splitFlatpakList(keywords, " "),
				ContentRating:     rating,
				Branches:          []string{},
			}
		}
		if !branchSeen[a.Branch] {
			branchSeen[a.Branch] = true
			detail.Branches = append(detail.Branches, a.Branch)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, ErrFlatpakAppNotFound
	}
	return detail, nil
}

// GetAppIconFile returns the arch and cached icon file name for an app, or
// ErrFlatpakAppNotFound when the catalog has no icon for it.
func (r *FlatpakCatalogRepository) GetAppIconFile(ctx context.Context, repoID, appID string) (arch, iconFile string, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT arch, icon_file FROM flatpak_catalog_apps
		WHERE repo_id = $1 AND app_id = $2 AND icon_file <> ''
		ORDER BY (arch = 'x86_64') DESC, arch ASC LIMIT 1`, repoID, appID).Scan(&arch, &iconFile)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrFlatpakAppNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("flatpak: get icon file: %w", err)
	}
	return arch, iconFile, nil
}

// GetIcon returns the cached icon for an app, or nil when nothing is cached.
func (r *FlatpakCatalogRepository) GetIcon(ctx context.Context, repoID, appID string) (*FlatpakIcon, error) {
	var icon FlatpakIcon
	err := r.db.QueryRowContext(ctx,
		`SELECT mime, data, fetched_at FROM flatpak_catalog_icons WHERE repo_id = $1 AND app_id = $2`,
		repoID, appID).Scan(&icon.Mime, &icon.Data, &icon.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("flatpak: get icon: %w", err)
	}
	return &icon, nil
}

// PutIcon stores (or replaces) the cached icon for an app. An empty mime
// records a negative cache entry.
func (r *FlatpakCatalogRepository) PutIcon(ctx context.Context, repoID, appID, mime string, data []byte) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO flatpak_catalog_icons (repo_id, app_id, mime, data, fetched_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (repo_id, app_id) DO UPDATE SET mime = EXCLUDED.mime, data = EXCLUDED.data, fetched_at = EXCLUDED.fetched_at`,
		repoID, appID, mime, nullableBytes(data))
	if err != nil {
		return fmt.Errorf("flatpak: put icon: %w", err)
	}
	return nil
}

// ListRepoStats returns per-repository counts for the metrics collector.
func (r *FlatpakCatalogRepository) ListRepoStats(ctx context.Context) ([]models.FlatpakRepoStats, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, app_count, last_refresh_at FROM flatpak_repositories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("flatpak: list repo stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var stats []models.FlatpakRepoStats
	for rows.Next() {
		var (
			s      models.FlatpakRepoStats
			lastAt sql.NullTime
		)
		if err := rows.Scan(&s.Name, &s.AppCount, &lastAt); err != nil {
			return nil, fmt.Errorf("flatpak: scan repo stats: %w", err)
		}
		if lastAt.Valid {
			t := lastAt.Time
			s.LastSuccessAt = &t
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func nullableBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
