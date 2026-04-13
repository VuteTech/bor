# Prometheus Metrics

Bor exposes a Prometheus-compatible `/metrics` endpoint on a dedicated port (default `127.0.0.1:9090`). The endpoint serves plain HTTP — it is intentionally separate from the main HTTPS port so it can be restricted to a management network without affecting agent or browser traffic.

---

## Table of Contents

1. [Configuration](#configuration)
2. [Metrics reference](#metrics-reference)
3. [Alerting examples](#alerting-examples)
4. [Grafana dashboard](#grafana-dashboard)
5. [Prometheus scrape config](#prometheus-scrape-config)
6. [Security](#security)

---

## Configuration

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BOR_METRICS_ADDR` | `127.0.0.1:9090` | `host:port` the endpoint binds to |
| `BOR_METRICS_TOKEN` | _(empty)_ | When set, requires `Authorization: Bearer <token>` on every scrape request |
| `BOR_METRICS_TLS_CERT_FILE` | _(empty)_ | Path to TLS certificate; enables HTTPS when set together with `BOR_METRICS_TLS_KEY_FILE` |
| `BOR_METRICS_TLS_KEY_FILE` | _(empty)_ | Path to TLS private key |

### YAML (`/etc/bor/server.yaml`)

```yaml
metrics:
  listen_addr: "127.0.0.1:9090"   # change host to expose on a management interface
  bearer_token: ""                 # leave empty when listener is localhost-only
  # tls_cert_file: "/etc/bor/metrics.crt"   # optional: enable HTTPS
  # tls_key_file:  "/etc/bor/metrics.key"
```

### Binding to a specific interface

```bash
# Management VLAN only
BOR_METRICS_ADDR=192.168.100.10:9090

# All interfaces (protect with a firewall rule)
BOR_METRICS_ADDR=0.0.0.0:9090
```

Environment variables always take precedence over the YAML file.

---

## Metrics reference

All Bor metrics are **Gauges** — they reflect the current state of the database at scrape time, not monotonically increasing counters. Each scrape runs a set of lightweight `COUNT … GROUP BY` queries against PostgreSQL; the typical overhead is a few milliseconds.

In addition to the Bor-specific metrics below, the endpoint also exposes the standard Go runtime (`go_*`) and process (`process_*`) metric families.

### Node metrics

#### `bor_nodes_total`

Number of enrolled nodes, partitioned by cached connection status.

| Label | Values |
|-------|--------|
| `status` | `online`, `degraded`, `offline`, `unknown` |

```
bor_nodes_total{status="online"}   1
bor_nodes_total{status="offline"}  1
bor_nodes_total{status="degraded"} 0
bor_nodes_total{status="unknown"}  0
```

**online** — agent connected and sending heartbeats within the expected interval.  
**degraded** — agent was seen recently but missed recent heartbeats.  
**offline** — no heartbeat received within the offline threshold.

---

#### `bor_node_certificate_expiry_seconds`

Seconds until each node's mTLS client certificate expires. **Negative values indicate an already-expired certificate** — the agent will be unable to reconnect after its current session drops.

| Label | Description |
|-------|-------------|
| `node` | Bor inventory name (unique, admin-assigned) |
| `fqdn` | System hostname reported by the node |

```
bor_node_certificate_expiry_seconds{node="fedora",fqdn="fedora"}        7718413
bor_node_certificate_expiry_seconds{node="fedora-atomic",fqdn="fedora"} 5891772
```

Agent certificates are valid for 90 days by default. An alert threshold of 14 days gives enough runway for a manual renewal before the agent loses connectivity.

---

#### `bor_node_last_seen_seconds`

Unix timestamp of the last heartbeat received from each node. Subtract from `time()` in PromQL to get the age of the last heartbeat in seconds.

| Label | Description |
|-------|-------------|
| `node` | Bor inventory name |
| `fqdn` | System hostname |

```
bor_node_last_seen_seconds{node="fedora",fqdn="fedora"}        1775911091
bor_node_last_seen_seconds{node="fedora-atomic",fqdn="fedora"} 1774040448
```

---

### Policy metrics

#### `bor_policies_total`

Number of policies, partitioned by lifecycle state and policy type.

| Label | Values |
|-------|--------|
| `state` | `draft`, `released` |
| `type` | `Firefox`, `Dconf`, `Kconfig`, `Polkit`, … |

```
bor_policies_total{state="released",type="Firefox"} 1
bor_policies_total{state="released",type="Dconf"}   2
bor_policies_total{state="released",type="Kconfig"}  1
bor_policies_total{state="draft",type="Dconf"}       1
```

Only `released` policies are delivered to agents. `draft` policies are work-in-progress.

---

#### `bor_policy_bindings_total`

Number of policy-to-node-group bindings, partitioned by state.

| Label | Values |
|-------|--------|
| `state` | `enabled`, `disabled` |

```
bor_policy_bindings_total{state="enabled"}  6
bor_policy_bindings_total{state="disabled"} 2
```

---

### Compliance metrics

#### `bor_compliance_results_total`

Number of compliance check results across all nodes and policies, partitioned by status. Each row represents one (node, policy, key) triple.

| Label | Values |
|-------|--------|
| `status` | `compliant`, `non_compliant`, `error`, `unknown` |

```
bor_compliance_results_total{status="compliant"}     4
bor_compliance_results_total{status="non_compliant"} 0
bor_compliance_results_total{status="error"}         0
bor_compliance_results_total{status="unknown"}       0
```

**compliant** — key present and matches the policy value.  
**non_compliant** — key present but value differs from the policy.  
**error** — schema installed but enforcement or check failed.  
**unknown** — result not yet reported by the agent.

The overall fleet compliance ratio can be derived with:
```promql
bor_compliance_results_total{status="compliant"}
  /
ignoring(status) sum without(status)(bor_compliance_results_total)
```

---

### User metrics

#### `bor_users_total`

Number of user accounts, partitioned by authentication source and enabled state.

| Label | Values |
|-------|--------|
| `source` | `local`, `ldap` |
| `enabled` | `true`, `false` |

```
bor_users_total{source="local",enabled="true"}  2
bor_users_total{source="ldap",enabled="true"}   14
bor_users_total{source="ldap",enabled="false"}  3
```

---

### Audit metrics

#### `bor_audit_events_total`

Total number of audit log entries in the database, partitioned by action. This is a **snapshot count** (Gauge), not a monotonically increasing counter — it reflects the current row count and will decrease if audit logs are purged.

| Label | Values |
|-------|--------|
| `action` | `create`, `update`, `delete`, `tamper_detected` |

```
bor_audit_events_total{action="create"}           32
bor_audit_events_total{action="update"}           121
bor_audit_events_total{action="delete"}           7
bor_audit_events_total{action="tamper_detected"}  6
```

---

## Alerting examples

Paste these into a Prometheus `rules.yml` file.

```yaml
groups:
  - name: bor
    rules:

      # Agent certificate expiring within 14 days
      - alert: BorNodeCertExpiringSoon
        expr: bor_node_certificate_expiry_seconds < 14 * 86400
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Node {{ $labels.node }} certificate expires in {{ $value | humanizeDuration }}"
          description: >
            The mTLS certificate for node {{ $labels.node }} ({{ $labels.fqdn }})
            expires in less than 14 days. The agent will lose connectivity when
            it expires. Renew via the Bor web UI or using the agent's renewal RPC.

      # Agent certificate already expired
      - alert: BorNodeCertExpired
        expr: bor_node_certificate_expiry_seconds < 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Node {{ $labels.node }} certificate has expired"
          description: >
            The mTLS certificate for {{ $labels.node }} expired
            {{ $value | humanizeDuration }} ago. The agent cannot reconnect
            until the certificate is renewed.

      # Node not seen for more than 30 minutes
      - alert: BorNodeStale
        expr: (time() - bor_node_last_seen_seconds) > 1800
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Node {{ $labels.node }} has not reported for {{ $value | humanizeDuration }}"

      # Any non-compliant or error compliance results
      - alert: BorComplianceFailure
        expr: bor_compliance_results_total{status=~"non_compliant|error"} > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} compliance failures (status={{ $labels.status }})"
          description: >
            One or more nodes have policy keys in state {{ $labels.status }}.
            Check the Compliance page in the Bor web UI for details.

      # File tamper events detected
      - alert: BorTamperDetected
        expr: increase(bor_audit_events_total{action="tamper_detected"}[1h]) > 0
        labels:
          severity: critical
        annotations:
          summary: "Managed file tampered on a Bor-enrolled node"
          description: >
            A policy-managed file was modified outside of Bor on at least one
            node in the last hour. Check the Audit Logs page for details.
```

---

## Grafana dashboard

A minimal Grafana dashboard can be built from the following panels:

| Panel | Query |
|-------|-------|
| Online nodes | `bor_nodes_total{status="online"}` |
| Offline nodes | `bor_nodes_total{status="offline"}` |
| Compliance ratio | `sum(bor_compliance_results_total{status="compliant"}) / sum(bor_compliance_results_total)` |
| Cert expiry (days) | `bor_node_certificate_expiry_seconds / 86400` |
| Released policies | `sum(bor_policies_total{state="released"})` |
| Active bindings | `bor_policy_bindings_total{state="enabled"}` |
| Tamper events (24 h) | `increase(bor_audit_events_total{action="tamper_detected"}[24h])` |

Set the **Cert expiry** panel to a time-series with a threshold at `14` days and colour it red below `0` to make expired certificates immediately visible.

---

## Prometheus scrape config

Add to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: bor
    # Plain HTTP (default — listener bound to localhost or behind a firewall)
    scheme: http
    static_configs:
      - targets:
          - "bor-server:9090"   # or the management IP if not on the same host
    # Uncomment if BOR_METRICS_TOKEN is set:
    # authorization:
    #   credentials: "your-token-here"
    scrape_interval: 30s
    scrape_timeout: 10s

  # Alternative: HTTPS (when BOR_METRICS_TLS_CERT_FILE / KEY are configured)
  # - job_name: bor-tls
  #   scheme: https
  #   tls_config:
  #     ca_file: /path/to/bor-metrics-ca.crt   # CA that signed the metrics cert
  #   static_configs:
  #     - targets: ["bor-server:9090"]
  #   authorization:
  #     credentials: "your-token-here"
```

### Quick test from the host

```bash
# No token (default dev config — listener on localhost only)
curl http://localhost:9090/metrics

# With bearer token
curl -H "Authorization: Bearer your-token" http://localhost:9090/metrics

# Filter to Bor metrics only
curl -s http://localhost:9090/metrics | grep "^bor_"
```

---

## Security

- The endpoint serves **plain HTTP** by default. Never expose it without TLS or a firewall when not on localhost.
- **Bind to localhost** (`127.0.0.1:9090`) when Prometheus runs on the same host as Bor — the default.
- **Bind to a management interface** (`192.168.100.10:9090`) when Prometheus is on a separate host. Restrict access with a firewall rule so only the Prometheus server can reach port 9090.
- **Bearer token** (`BOR_METRICS_TOKEN`) adds a layer of defence-in-depth when the listener is on a network interface. The token is checked with a constant-time comparison; choose a random string of at least 32 characters.
- **TLS** (`BOR_METRICS_TLS_CERT_FILE` + `BOR_METRICS_TLS_KEY_FILE`): when both are set, the endpoint switches to HTTPS. Recommended when the Prometheus scraper is on a different host and a VPN or firewall is not available.
- The endpoint does **not** expose any secret values, user credentials, or policy content — only aggregate counts and per-node timing information.
