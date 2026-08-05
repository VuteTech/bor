// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// buildAgentRepo lays out a minimal repository tree plus a secret file
// OUTSIDE the root and a symlink inside pointing at it (escape probes).
func buildAgentRepo(t *testing.T) (repoDir string) {
	t.Helper()
	parent := t.TempDir()
	repoDir = filepath.Join(parent, "agent-repo")

	secretPath := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	for dir, files := range map[string]map[string]string{
		".":            {"manifest.json": `{"version":"1.2.3","apk_version":"1.2.3","signed":true,"files":[{"path":"deb/bor-agent_1.2.3_amd64.deb","format":"deb","arch":"amd64","size":7,"sha256":"abc"}]}`},
		"deb":          {"bor-agent_1.2.3_amd64.deb": "DEBDATA", "Packages": "Package: bor-agent\n", "InRelease": "signed stuff"},
		"rpm/repodata": {"repomd.xml": "<repomd/>"},
		"apk":          {"bor-agent_1.2.3_x86_64.apk": "APKDATA"},
	} {
		if err := os.MkdirAll(filepath.Join(repoDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(repoDir, dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Symlink(secretPath, filepath.Join(repoDir, "deb", "evil-link.deb")); err != nil {
		t.Fatal(err)
	}
	return repoDir
}

func newTestAgentRepoHandler(t *testing.T, dir, caCertFile, version string) (*AgentRepoHandler, *prometheus.CounterVec) {
	t.Helper()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_downloads_total"}, []string{"format", "arch"})
	return NewAgentRepoHandler(dir, caCertFile, version, counter), counter
}

func getAgent(h *AgentRepoHandler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://bor.example"+path, http.NoBody)
	// Deliberately bypasses http.ServeMux and its path cleaning: the handler
	// must hold its own against raw traversal attempts (defense in depth).
	req.URL.Path = path
	rec := httptest.NewRecorder()
	if path == "/agent/ca.crt" {
		h.ServeCACert(rec, req)
	} else {
		h.ServeFiles(rec, req)
	}
	return rec
}

func TestAgentRepoServeFiles(t *testing.T) {
	repoDir := buildAgentRepo(t)
	h, counter := newTestAgentRepoHandler(t, repoDir, "", "1.2.3")

	t.Run("package download counts with labels", func(t *testing.T) {
		rec := getAgent(h, http.MethodGet, "/agent/deb/bor-agent_1.2.3_amd64.deb")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != "DEBDATA" {
			t.Fatalf("body = %q", got)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.debian.binary-package" {
			t.Fatalf("content-type = %q", ct)
		}
		if got := testutil.ToFloat64(counter.WithLabelValues("deb", "amd64")); got != 1 {
			t.Fatalf("download counter = %v, want 1", got)
		}
	})

	t.Run("apk arch normalized despite x86_64 underscore", func(t *testing.T) {
		rec := getAgent(h, http.MethodGet, "/agent/apk/bor-agent_1.2.3_x86_64.apk")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := testutil.ToFloat64(counter.WithLabelValues("apk", "amd64")); got != 1 {
			t.Fatalf("apk counter = %v, want 1", got)
		}
	})

	t.Run("HEAD serves but does not count", func(t *testing.T) {
		before := testutil.ToFloat64(counter.WithLabelValues("deb", "amd64"))
		rec := getAgent(h, http.MethodHead, "/agent/deb/bor-agent_1.2.3_amd64.deb")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if after := testutil.ToFloat64(counter.WithLabelValues("deb", "amd64")); after != before {
			t.Fatalf("HEAD incremented the counter: %v → %v", before, after)
		}
	})

	t.Run("metadata is served but not counted", func(t *testing.T) {
		rec := getAgent(h, http.MethodGet, "/agent/deb/InRelease")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := testutil.ToFloat64(counter.WithLabelValues("deb", "unknown")); got != 0 {
			t.Fatalf("metadata fetch was counted: %v", got)
		}
	})

	t.Run("traversal and malformed paths are rejected", func(t *testing.T) {
		for _, p := range []string{
			"/agent/../secret.txt",
			"/agent/deb/../../secret.txt",
			"/agent//etc/passwd",
			"/agent/./manifest.json",
			"/agent/deb/..%2f..%2fsecret.txt",
			"/agent/",
			"/agent/deb",
			"/agent/deb/",
			"/agent/nope.deb",
		} {
			if rec := getAgent(h, http.MethodGet, p); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", p, rec.Code)
			}
		}
	})

	// Note: "/agent/" appears in the 404 table above because this tree has no
	// index.html — the root serves one only when the assembler generated it.
	t.Run("index page served at the root when present", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>Bor agent packages</h1>"), 0o644); err != nil {
			t.Fatal(err)
		}
		hi, _ := newTestAgentRepoHandler(t, dir, "", "1.2.3")
		rec := getAgent(hi, http.MethodGet, "/agent/")
		if rec.Code != http.StatusOK {
			t.Fatalf("index status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Fatalf("index content-type = %q", ct)
		}
	})

	t.Run("symlink escape is blocked by os.Root", func(t *testing.T) {
		rec := getAgent(h, http.MethodGet, "/agent/deb/evil-link.deb")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("symlink escape served: status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("mutating methods rejected", func(t *testing.T) {
		if rec := getAgent(h, http.MethodPost, "/agent/deb/Packages"); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST = %d, want 405", rec.Code)
		}
	})
}

func TestAgentRepoGracefulAbsence(t *testing.T) {
	h, _ := newTestAgentRepoHandler(t, filepath.Join(t.TempDir(), "does-not-exist"), "", "1.2.3")

	if rec := getAgent(h, http.MethodGet, "/agent/deb/anything.deb"); rec.Code != http.StatusNotFound {
		t.Fatalf("files with absent repo = %d, want 404", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-packages", http.NoBody)
	rec := httptest.NewRecorder()
	h.ManifestAPI(rec, req)
	var resp AgentPackagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RepoAvailable || resp.Manifest != nil {
		t.Fatalf("absent repo reported available: %+v", resp)
	}
	if resp.ServerVersion != "1.2.3" {
		t.Fatalf("server_version = %q", resp.ServerVersion)
	}
}

func TestAgentRepoManifestAPI(t *testing.T) {
	repoDir := buildAgentRepo(t)

	t.Run("matching version", func(t *testing.T) {
		h, _ := newTestAgentRepoHandler(t, repoDir, "", "1.2.3")
		rec := httptest.NewRecorder()
		h.ManifestAPI(rec, httptest.NewRequest(http.MethodGet, "/api/v1/agent-packages", http.NoBody))
		var resp AgentPackagesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.RepoAvailable || !resp.VersionMatch || resp.Manifest == nil {
			t.Fatalf("unexpected response: %+v", resp)
		}
		if len(resp.Manifest.Files) != 1 || resp.Manifest.Files[0].Arch != "amd64" || !resp.Manifest.Signed {
			t.Fatalf("manifest content wrong: %+v", resp.Manifest)
		}
	})

	t.Run("version mismatch flagged", func(t *testing.T) {
		h, _ := newTestAgentRepoHandler(t, repoDir, "", "9.9.9")
		rec := httptest.NewRecorder()
		h.ManifestAPI(rec, httptest.NewRequest(http.MethodGet, "/api/v1/agent-packages", http.NoBody))
		var resp AgentPackagesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.RepoAvailable || resp.VersionMatch {
			t.Fatalf("mismatch not flagged: %+v", resp)
		}
	})

	t.Run("corrupt manifest means unavailable", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{nope"), 0o644); err != nil {
			t.Fatal(err)
		}
		h, _ := newTestAgentRepoHandler(t, dir, "", "1.2.3")
		rec := httptest.NewRecorder()
		h.ManifestAPI(rec, httptest.NewRequest(http.MethodGet, "/api/v1/agent-packages", http.NoBody))
		var resp AgentPackagesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.RepoAvailable {
			t.Fatalf("corrupt manifest reported available: %+v", resp)
		}
	})
}

func TestAgentRepoServeCACert(t *testing.T) {
	pemPath := filepath.Join(t.TempDir(), "ca.crt")
	pem := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(pemPath, []byte(pem), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("served", func(t *testing.T) {
		h, _ := newTestAgentRepoHandler(t, t.TempDir(), pemPath, "1.2.3")
		rec := getAgent(h, http.MethodGet, "/agent/ca.crt")
		if rec.Code != http.StatusOK || rec.Body.String() != pem {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/x-pem-file" {
			t.Fatalf("content-type = %q", ct)
		}
	})

	t.Run("missing file is 404", func(t *testing.T) {
		h, _ := newTestAgentRepoHandler(t, t.TempDir(), filepath.Join(t.TempDir(), "nope.crt"), "1.2.3")
		if rec := getAgent(h, http.MethodGet, "/agent/ca.crt"); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}
