// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireFeature(t *testing.T) {
	on := false
	called := 0
	h := RequireFeature(func() bool { return on }, "not in this edition")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports", http.NoBody))
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "not in this edition") || called != 0 {
		t.Fatalf("off: status %d body %s called %d", rr.Code, rr.Body.String(), called)
	}

	on = true
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports", http.NoBody))
	if rr.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("on: status %d called %d", rr.Code, called)
	}
}
