// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package metrics provides a Prometheus collector for Bor server state.
package metrics

import (
	"context"
	"log"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/prometheus/client_golang/prometheus"
)

// repos groups the repository types the collector queries.
type repos struct {
	nodes          *database.NodeRepository
	policies       *database.PolicyRepository
	policyBindings *database.PolicyBindingRepository
	auditLogs      *database.AuditLogRepository
	users          *database.UserRepository
	compliance     *database.DConfRepository
}

// BorCollector implements prometheus.Collector and emits Bor-specific metrics
// by querying the database on every scrape. All metrics are Gauges — the
// collector runs lightweight aggregation queries (COUNT … GROUP BY) so the
// overhead per scrape is minimal even for large fleets.
type BorCollector struct {
	repos repos

	// ── Node metrics ──────────────────────────────────────────────────────
	nodesTotal     *prometheus.Desc
	nodeCertExpiry *prometheus.Desc
	nodeLastSeen   *prometheus.Desc

	// ── Policy metrics ────────────────────────────────────────────────────
	policiesTotal *prometheus.Desc
	bindingsTotal *prometheus.Desc

	// ── Compliance metrics ────────────────────────────────────────────────
	complianceTotal *prometheus.Desc

	// ── User metrics ──────────────────────────────────────────────────────
	usersTotal *prometheus.Desc

	// ── Audit metrics ─────────────────────────────────────────────────────
	auditEventsTotal *prometheus.Desc
}

// NewBorCollector creates a new BorCollector wired to the given repositories.
func NewBorCollector(
	nodeRepo *database.NodeRepository,
	policyRepo *database.PolicyRepository,
	bindingRepo *database.PolicyBindingRepository,
	auditLogRepo *database.AuditLogRepository,
	userRepo *database.UserRepository,
	dconfRepo *database.DConfRepository,
) *BorCollector {
	return &BorCollector{
		repos: repos{
			nodes:          nodeRepo,
			policies:       policyRepo,
			policyBindings: bindingRepo,
			auditLogs:      auditLogRepo,
			users:          userRepo,
			compliance:     dconfRepo,
		},

		nodesTotal: prometheus.NewDesc(
			"bor_nodes_total",
			"Number of enrolled nodes, partitioned by their cached status.",
			[]string{"status"}, nil,
		),
		nodeCertExpiry: prometheus.NewDesc(
			"bor_node_certificate_expiry_seconds",
			"Seconds until the node's mTLS certificate expires. Negative values mean the certificate has already expired.",
			[]string{"node", "fqdn"}, nil,
		),
		nodeLastSeen: prometheus.NewDesc(
			"bor_node_last_seen_seconds",
			"Unix timestamp of the last heartbeat received from the node.",
			[]string{"node", "fqdn"}, nil,
		),

		policiesTotal: prometheus.NewDesc(
			"bor_policies_total",
			"Number of policies, partitioned by state and type.",
			[]string{"state", "type"}, nil,
		),
		bindingsTotal: prometheus.NewDesc(
			"bor_policy_bindings_total",
			"Number of policy bindings, partitioned by state.",
			[]string{"state"}, nil,
		),

		complianceTotal: prometheus.NewDesc(
			"bor_compliance_results_total",
			"Number of compliance results, partitioned by status.",
			[]string{"status"}, nil,
		),

		usersTotal: prometheus.NewDesc(
			"bor_users_total",
			"Number of user accounts, partitioned by source and enabled flag.",
			[]string{"source", "enabled"}, nil,
		),

		auditEventsTotal: prometheus.NewDesc(
			"bor_audit_events_total",
			"Total number of audit log entries, partitioned by action. This is a snapshot count, not a monotonic counter.",
			[]string{"action"}, nil,
		),
	}
}

// Describe sends all metric descriptors to ch.
func (c *BorCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.nodesTotal
	ch <- c.nodeCertExpiry
	ch <- c.nodeLastSeen
	ch <- c.policiesTotal
	ch <- c.bindingsTotal
	ch <- c.complianceTotal
	ch <- c.usersTotal
	ch <- c.auditEventsTotal
}

// Collect runs all DB queries and emits the current metric values.
// Errors from individual queries are logged but do not abort the others —
// a partial scrape is preferable to a failed one.
func (c *BorCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c.collectNodes(ctx, ch)
	c.collectPolicies(ctx, ch)
	c.collectBindings(ctx, ch)
	c.collectCompliance(ctx, ch)
	c.collectUsers(ctx, ch)
	c.collectAuditEvents(ctx, ch)
}

func (c *BorCollector) collectNodes(ctx context.Context, ch chan<- prometheus.Metric) {
	// ── Counts by status ──
	counts, err := c.repos.nodes.CountByStatus(ctx)
	if err != nil {
		log.Printf("metrics: CountByStatus: %v", err)
	} else {
		for _, status := range []string{"online", "degraded", "offline", "unknown"} {
			ch <- prometheus.MustNewConstMetric(c.nodesTotal, prometheus.GaugeValue,
				float64(counts[status]), status)
		}
	}

	// ── Per-node cert expiry and last-seen ──
	nodes, err := c.repos.nodes.ListForMetrics(ctx)
	if err != nil {
		log.Printf("metrics: ListForMetrics: %v", err)
		return
	}

	now := time.Now()
	for _, n := range nodes {
		if n.CertNotAfter != nil {
			ch <- prometheus.MustNewConstMetric(c.nodeCertExpiry, prometheus.GaugeValue,
				n.CertNotAfter.Sub(now).Seconds(), n.Name, n.FQDN)
		}
		if n.LastSeen != nil {
			ch <- prometheus.MustNewConstMetric(c.nodeLastSeen, prometheus.GaugeValue,
				float64(n.LastSeen.Unix()), n.Name, n.FQDN)
		}
	}
}

func (c *BorCollector) collectPolicies(ctx context.Context, ch chan<- prometheus.Metric) {
	rows, err := c.repos.policies.CountByStateAndType(ctx)
	if err != nil {
		log.Printf("metrics: CountByStateAndType: %v", err)
		return
	}
	for _, r := range rows {
		ch <- prometheus.MustNewConstMetric(c.policiesTotal, prometheus.GaugeValue,
			float64(r.Count), r.State, r.Type)
	}
}

func (c *BorCollector) collectBindings(ctx context.Context, ch chan<- prometheus.Metric) {
	counts, err := c.repos.policyBindings.CountByState(ctx)
	if err != nil {
		log.Printf("metrics: binding CountByState: %v", err)
		return
	}
	for _, state := range []string{"enabled", "disabled"} {
		ch <- prometheus.MustNewConstMetric(c.bindingsTotal, prometheus.GaugeValue,
			float64(counts[state]), state)
	}
}

func (c *BorCollector) collectCompliance(ctx context.Context, ch chan<- prometheus.Metric) {
	counts, err := c.repos.compliance.CountComplianceByStatus(ctx)
	if err != nil {
		log.Printf("metrics: CountComplianceByStatus: %v", err)
		return
	}
	for _, status := range []string{"compliant", "non_compliant", "error", "unknown"} {
		ch <- prometheus.MustNewConstMetric(c.complianceTotal, prometheus.GaugeValue,
			float64(counts[status]), status)
	}
}

func (c *BorCollector) collectUsers(ctx context.Context, ch chan<- prometheus.Metric) {
	rows, err := c.repos.users.CountBySourceAndEnabled(ctx)
	if err != nil {
		log.Printf("metrics: CountBySourceAndEnabled: %v", err)
		return
	}
	for _, r := range rows {
		enabled := "false"
		if r.Enabled {
			enabled = "true"
		}
		ch <- prometheus.MustNewConstMetric(c.usersTotal, prometheus.GaugeValue,
			float64(r.Count), r.Source, enabled)
	}
}

func (c *BorCollector) collectAuditEvents(ctx context.Context, ch chan<- prometheus.Metric) {
	counts, err := c.repos.auditLogs.CountByAction(ctx)
	if err != nil {
		log.Printf("metrics: CountByAction: %v", err)
		return
	}
	for action, count := range counts {
		ch <- prometheus.MustNewConstMetric(c.auditEventsTotal, prometheus.GaugeValue,
			float64(count), action)
	}
}
