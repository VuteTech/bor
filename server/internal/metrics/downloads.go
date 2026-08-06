// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package metrics

import "github.com/prometheus/client_golang/prometheus"

// NewAgentPackageDownloads builds the counter for agent package downloads
// served from /agent/*. Labels are aggregate only (package format and
// architecture — never client identifiers), so the metric is GDPR-neutral.
// Register it with the metrics server via NewServer's collectors parameter.
func NewAgentPackageDownloads() *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bor_agent_package_downloads_total",
			Help: "Agent packages downloaded from this server's /agent/ endpoints, by package format and architecture.",
		},
		[]string{"format", "arch"},
	)
}
