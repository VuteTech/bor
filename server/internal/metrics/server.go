// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package metrics

import (
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewServer builds a plain-HTTP server that exposes /metrics.
//
// addr is the listen address in host:port form (e.g. "127.0.0.1:9090").
// When bearerToken is non-empty every request must carry
// "Authorization: Bearer <token>"; otherwise no authentication is applied.
//
// The returned *http.Server is not started — call ListenAndServe in a goroutine.
func NewServer(addr, bearerToken string, collector prometheus.Collector) *http.Server {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	metricsHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	var handler http.Handler = metricsHandler
	if bearerToken != "" {
		handler = bearerTokenMiddleware(bearerToken, metricsHandler)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Redirect bare root to /metrics for convenience.
		http.Redirect(w, r, "/metrics", http.StatusFound)
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}

// bearerTokenMiddleware rejects requests that do not carry a valid Bearer token.
func bearerTokenMiddleware(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.EqualFold(auth[:min(len(auth), 7)], "bearer ") || auth != expected {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// min returns the smaller of a and b (backport for Go < 1.21).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
