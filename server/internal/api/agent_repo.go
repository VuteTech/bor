// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// AgentRepoHandler serves the static agent package repository assembled at
// release build time (scripts/assemble-agent-repo.sh) from the directory
// configured via agent_repo.dir / BOR_AGENT_REPO_DIR:
//
//	GET /agent/{path}            public — package files and repo metadata
//	GET /agent/ca.crt            public — the internal CA certificate (PEM)
//	GET /api/v1/agent-packages   authenticated — manifest for the deploy wizard
//
// The download endpoints are public by design: apt/dnf on managed nodes
// cannot authenticate, and the payload is exactly what the GitHub release
// already publishes. Files are opened through an os.Root, which makes path
// traversal and symlink escapes structurally impossible; directories are
// never listed. When the directory is absent, or present without a
// manifest.json (the tracked placeholder in source builds), every endpoint
// reports the feature as off.
type AgentRepoHandler struct {
	fsys          fs.FS // nil when the repo directory was absent at startup
	caCertFile    string
	serverVersion string
	downloads     *prometheus.CounterVec
}

// agentRepoManifestMaxSize caps manifest.json reads; the real file is a few
// kilobytes, anything beyond this is corrupt or hostile.
const agentRepoManifestMaxSize = 4 << 20

// AgentRepoManifest mirrors the manifest.json emitted by
// scripts/assemble-agent-repo.sh.
type AgentRepoManifest struct {
	Version     string          `json:"version"`
	APKVersion  string          `json:"apk_version"`
	GeneratedAt string          `json:"generated_at"`
	Signed      bool            `json:"signed"`
	Channels    json.RawMessage `json:"channels"`
	Files       []AgentRepoFile `json:"files"`
}

// AgentRepoFile is one downloadable package in the manifest.
type AgentRepoFile struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Arch   string `json:"arch"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// AgentPackagesResponse is returned by GET /api/v1/agent-packages.
type AgentPackagesResponse struct {
	RepoAvailable bool               `json:"repo_available"`
	ServerVersion string             `json:"server_version"`
	VersionMatch  bool               `json:"version_match"`
	Manifest      *AgentRepoManifest `json:"manifest,omitempty"`
}

// NewAgentRepoHandler opens dir as the repository root. A missing directory
// is not an error — the feature is simply off (every endpoint 404s and the
// manifest API reports repo_available=false).
func NewAgentRepoHandler(dir, caCertFile, serverVersion string, downloads *prometheus.CounterVec) *AgentRepoHandler {
	h := &AgentRepoHandler{
		caCertFile:    caCertFile,
		serverVersion: serverVersion,
		downloads:     downloads,
	}
	root, err := os.OpenRoot(dir)
	if err == nil {
		h.fsys = root.FS()
	}
	return h
}

// Manifest reads and parses manifest.json from the repository. Read fresh on
// every call so a package upgrade under a running server is picked up.
func (h *AgentRepoHandler) Manifest() (*AgentRepoManifest, error) {
	if h.fsys == nil {
		return nil, fs.ErrNotExist
	}
	f, err := h.fsys.Open("manifest.json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var m AgentRepoManifest
	if err := json.NewDecoder(io.LimitReader(f, agentRepoManifestMaxSize)).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// LogStartup emits one line describing the repository state, and a loud
// warning when the packaged agent version does not match the running server
// (reachable only when the directory was edited by hand — the packaging
// replaces both together).
func (h *AgentRepoHandler) LogStartup() {
	m, err := h.Manifest()
	if err != nil {
		log.Printf("Agent package repo: disabled (no usable manifest: %v) — /agent/* downloads are off", err)
		return
	}
	log.Printf("Agent package repo: serving %d files, agent version %s, signed=%v", len(m.Files), m.Version, m.Signed)
	if m.Version != h.serverVersion {
		log.Printf("WARNING: agent package repo version %q does not match server version %q — the /agent/* downloads offer a different agent release", m.Version, h.serverVersion)
	}
}

// ServeFiles handles GET/HEAD /agent/{path} — the static repository tree.
func (h *AgentRepoHandler) ServeFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.fsys == nil {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/agent/")
	if name == "" {
		// The repository root serves the static instructions page (generated
		// by the assemble script); 404 below when the tree has none.
		name = "index.html"
	}
	// fs.ValidPath rejects ".", "..", absolute paths and any ".." element —
	// combined with the os.Root-backed FS, escapes are impossible even via
	// symlinks planted inside the tree.
	if !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(h.fsys, name)
	if err != nil || info.IsDir() {
		// No directory listings, no distinction between missing and denied.
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", agentRepoContentType(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// statusRecorder is shared with the audit middleware (audit_middleware.go).
	rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
	http.ServeFileFS(rec, r, h.fsys, name)

	if r.Method == http.MethodGet && rec.statusCode < 300 {
		if format, arch, ok := agentRepoPackageLabels(name); ok && h.downloads != nil {
			h.downloads.WithLabelValues(format, arch).Inc()
		}
	}
}

// ServeCACert handles GET/HEAD /agent/ca.crt — the internal CA certificate
// (public material only), so package managers and enroll scripts can trust
// the server's TLS endpoint when it uses the auto-generated certificate.
func (h *AgentRepoHandler) ServeCACert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	pem, err := os.ReadFile(h.caCertFile)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(pem)
}

// ManifestAPI handles GET /api/v1/agent-packages (authenticated) — what the
// deploy wizard renders.
func (h *AgentRepoHandler) ManifestAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	resp := AgentPackagesResponse{ServerVersion: h.serverVersion}
	if m, err := h.Manifest(); err == nil {
		resp.RepoAvailable = true
		resp.VersionMatch = m.Version == h.serverVersion
		resp.Manifest = m
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode agent-packages response: %v", err)
	}
}

// agentRepoContentType maps repository files to stable content types.
// http.ServeContent honours a pre-set Content-Type and never sniffs.
func agentRepoContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".deb"):
		return "application/vnd.debian.binary-package"
	case strings.HasSuffix(name, ".rpm"):
		return "application/x-rpm"
	case strings.HasSuffix(name, ".zst"):
		return "application/zstd"
	case strings.HasSuffix(name, ".apk"), strings.HasSuffix(name, ".bz2"):
		return "application/octet-stream"
	case strings.HasSuffix(name, ".gz"):
		return "application/gzip"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".asc"), strings.HasSuffix(name, ".gpg"):
		return "application/pgp-signature"
	case strings.HasSuffix(name, ".xml"):
		return "application/xml"
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	default:
		// Packages, Release, InRelease and friends.
		return "text/plain; charset=utf-8"
	}
}

// agentRepoPackageLabels reports the metrics labels for a served file, and
// whether it is a package download at all (metadata fetches are not counted).
func agentRepoPackageLabels(name string) (format, arch string, ok bool) {
	format, _, found := strings.Cut(name, "/")
	switch format {
	case "deb", "rpm", "apk", "arch":
	default:
		return "", "", false
	}
	if !found {
		return "", "", false
	}
	base := path.Base(name)
	isPackage := strings.HasSuffix(base, ".deb") || strings.HasSuffix(base, ".rpm") ||
		strings.HasSuffix(base, ".apk") || strings.HasSuffix(base, ".pkg.tar.zst")
	if !isPackage {
		return "", "", false
	}
	// Mirror of the assembler's whole-name matching: the packagers' own
	// separators are ambiguous (apk's "x86_64" contains apk's field separator).
	switch {
	case strings.Contains(base, "x86_64"), strings.Contains(base, "amd64"):
		arch = "amd64"
	case strings.Contains(base, "aarch64"), strings.Contains(base, "arm64"):
		arch = "arm64"
	case strings.Contains(base, "ppc64le"), strings.Contains(base, "ppc64el"):
		arch = "ppc64le"
	default:
		arch = "unknown"
	}
	return format, arch, true
}
