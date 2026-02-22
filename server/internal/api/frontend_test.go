// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrontendHandler_ServesIndexHTML(t *testing.T) {
	staticFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{
			Data: []byte("<html>test</html>"),
		},
	}

	handler := FrontendHandler(staticFS)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "<html>test</html>") {
		t.Errorf("GET / body = %q, want to contain <html>test</html>", rec.Body.String())
	}
}

func TestFrontendHandler_ServesStaticFile(t *testing.T) {
	staticFS := fstest.MapFS{
		"static/index.html":    &fstest.MapFile{Data: []byte("<html>index</html>")},
		"static/style.css":     &fstest.MapFile{Data: []byte("body{}")},
	}

	handler := FrontendHandler(staticFS)

	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /style.css status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "body{}") {
		t.Errorf("GET /style.css body = %q, want body{}", rec.Body.String())
	}
}

func TestFrontendHandler_SPAFallback(t *testing.T) {
	staticFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("<html>spa</html>")},
	}

	handler := FrontendHandler(staticFS)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /dashboard status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "<html>spa</html>") {
		t.Errorf("GET /dashboard body = %q, want to contain <html>spa</html>", rec.Body.String())
	}
}

func TestFrontendHandler_APIRoutesNotServed(t *testing.T) {
	staticFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("<html>index</html>")},
	}

	handler := FrontendHandler(staticFS)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/something", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/v1/something status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
