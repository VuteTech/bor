// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewServer_ExtensionCollectors(t *testing.T) {
	core := prometheus.NewGauge(prometheus.GaugeOpts{Name: "bor_core_test", Help: "Core."})
	module := prometheus.NewGauge(prometheus.GaugeOpts{Name: "bor_module_test", Help: "Module."})
	module.Set(7)
	// A module collector that clashes with a core metric is left out; it
	// must not stop the server.
	clash := prometheus.NewGauge(prometheus.GaugeOpts{Name: "bor_core_test", Help: "Clash."})

	srv := NewServer("127.0.0.1:0", "", []prometheus.Collector{module, clash}, core)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{"bor_core_test 0", "bor_module_test 7"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics output lacks %q", want)
		}
	}
	if strings.Contains(string(body), "Clash.") {
		t.Error("the clashing collector was registered")
	}
}
