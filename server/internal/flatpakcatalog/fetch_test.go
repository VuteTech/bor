// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// testFetcher returns a Fetcher whose transport trusts the httptest TLS server
// and rewrites every host to it, so allowlist logic sees the real hostnames.
func testFetcher(t *testing.T, srv *httptest.Server) *Fetcher {
	t.Helper()
	f := NewFetcher(1)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	f.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Host = strings.TrimPrefix(srv.URL, "https://")
		return tr.RoundTrip(req)
	})
	return f
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenConditional(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repo/appstream/x86_64/appstream.xml.gz":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Fri, 04 Sep 2026 23:13:26 GMT")
			_, _ = w.Write([]byte("payload"))
		case "/redirect":
			http.Redirect(w, r, "https://evil.example/x", http.StatusFound)
		case "/redirect-http":
			http.Redirect(w, r, "http://mirror.example/x", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	f := testFetcher(t, srv)
	ctx := context.Background()

	res, err := f.OpenConditional(ctx, "https://mirror.example/repo/appstream/x86_64/appstream.xml.gz", "", "")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(body) != "payload" || res.ETag != `"v1"` || res.LastModified == "" || res.NotModified {
		t.Errorf("unexpected result: %q %+v", body, res)
	}

	res, err = f.OpenConditional(ctx, "https://mirror.example/repo/appstream/x86_64/appstream.xml.gz", `"v1"`, "")
	if err != nil || !res.NotModified {
		t.Fatalf("conditional fetch: %v %+v", err, res)
	}

	if _, err := f.OpenConditional(ctx, "https://mirror.example/redirect", "", ""); err == nil {
		t.Error("redirect to another host must be refused")
	}
	if _, err := f.OpenConditional(ctx, "https://mirror.example/redirect-http", "", ""); err == nil {
		t.Error("redirect to http must be refused")
	}
	if _, err := f.OpenConditional(ctx, "http://mirror.example/repo/x", "", ""); err == nil {
		t.Error("plain http must be refused")
	}
	if _, err := f.OpenConditional(ctx, "https://mirror.example/missing", "", ""); err == nil {
		t.Error("404 must be an error")
	}
}

func TestGetSmall_SizeCap(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	f := testFetcher(t, srv)

	data, ct, err := f.GetSmall(context.Background(), "https://mirror.example/icon.png", 200)
	if err != nil || len(data) != 100 || ct != "image/png" {
		t.Fatalf("GetSmall: %v %d %q", err, len(data), ct)
	}
	if _, _, err := f.GetSmall(context.Background(), "https://mirror.example/icon.png", 50); !errors.Is(err, ErrTooLarge) {
		t.Errorf("expected ErrTooLarge, got %v", err)
	}
	if _, _, err := f.GetSmall(context.Background(), "https://mirror.example/missing", 50); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAppstreamURL(t *testing.T) {
	repo := &models.FlatpakRepository{URL: "https://dl.flathub.org/repo/"}
	if u, err := AppstreamURL(repo, "x86_64"); err != nil || u != "https://dl.flathub.org/repo/appstream/x86_64/appstream.xml.gz" {
		t.Errorf("derived url = %q %v", u, err)
	}
	repo.AppstreamURL = "https://mirror.example/as/{arch}/appstream.xml.gz"
	if u, _ := AppstreamURL(repo, "aarch64"); u != "https://mirror.example/as/aarch64/appstream.xml.gz" {
		t.Errorf("override url = %q", u)
	}
	oci := &models.FlatpakRepository{URL: "oci+https://registry.fedoraproject.org"}
	if _, err := AppstreamURL(oci, "x86_64"); !errors.Is(err, ErrNoAppstreamURL) {
		t.Errorf("oci without override: %v", err)
	}
}
