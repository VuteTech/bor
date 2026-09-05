// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package metrics

import "github.com/prometheus/client_golang/prometheus"

// NewFlatpakCatalogRefreshes builds the counter for Flatpak catalog refresh
// attempts (scheduled, manual and uploads). Labels are the repository name
// and the outcome (ok, unchanged, error, upload). Register it with the
// metrics server via NewServer's collectors parameter.
func NewFlatpakCatalogRefreshes() *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bor_flatpak_catalog_refresh_total",
			Help: "Flatpak catalog refresh attempts by repository and outcome (ok, unchanged, error, upload).",
		},
		[]string{"repo", "outcome"},
	)
}
