// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package metrics

import "github.com/prometheus/client_golang/prometheus"

// NewLuksEscrowOperations returns the counter for recovery-key escrow
// operations. Register it on the metrics server and pass it to the
// disk-encryption service. Aggregate labels only, no node identifiers.
func NewLuksEscrowOperations() *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bor_luks_escrow_operations_total",
			Help: "LUKS recovery-key escrow operations by operation (escrow, confirm, release, destroy) and outcome (ok, error).",
		},
		[]string{"op", "outcome"},
	)
}

// NewLuksReveals returns the counter for recovery-key reveals to humans.
func NewLuksReveals() prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bor_luks_recovery_key_reveals_total",
		Help: "LUKS recovery keys revealed to administrators.",
	})
}
