# Monitoring

This page explains how to watch a running Bor installation: the built-in
Prometheus metrics endpoint and every metric it exports, a scrape
configuration, alert rules built only on metrics Bor actually exposes, the
checks you can use as health probes, and where the server and agent write
their logs.

## Overview

| What | Where | Notes |
|------|-------|-------|
| Prometheus metrics | `http://127.0.0.1:9090/metrics` (default) | Separate listener, always started by `bor-server` |
| Server liveness | `GET https://bor.example.com/api/v1/version` | No authentication, does not touch the database |
| Server logs | `journalctl -u bor-server` | Syslog identifier `bor-server` |
| Agent logs | `journalctl -u bor-agent` on each endpoint | Syslog identifier `bor-agent` |
| Audit trail | **Audit Logs** page, export, or syslog forwarding | See [Audit log](../user-guide/audit-log.md) |

## The metrics endpoint

`bor-server` always starts a dedicated HTTP listener for Prometheus, separate
from the HTTPS web port (`server.enrollment_port`, 443 recommended, default
8443) used by browsers and agents. It serves:

- `/metrics` - the metrics in Prometheus / OpenMetrics text format.
- `/` - redirects to `/metrics`.

By default the listener binds to `127.0.0.1:9090`, so only processes on the
server itself can scrape it. There is no switch to turn the listener off; keep
it on the loopback address if you do not use it.

On startup the server logs which address it is using:

```text
Metrics (Prometheus) listening on http://127.0.0.1:9090/metrics
```

!!! warning "Port 9090 is also the Prometheus default"
    If Prometheus runs on the same host as `bor-server`, both will try to bind
    port 9090. A failed metrics listener does **not** stop `bor-server`: it only
    logs `Metrics server error: listen tcp 127.0.0.1:9090: bind: address already in use`
    and keeps serving the UI and agents without metrics. Move one of the two to
    another port, for example `listen_addr: "127.0.0.1:9469"`.

### Configuration

Set the options in the `metrics` section of `/etc/bor/server.yaml`, or with the
matching environment variables (for example in `/etc/bor/server.env`).
Environment variables take precedence over the YAML file.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `metrics.listen_addr` | `BOR_METRICS_ADDR` | `127.0.0.1:9090` | `host:port` the listener binds to. Use a management interface address to allow remote scraping, or `:9090` for all interfaces. |
| `metrics.bearer_token` | `BOR_METRICS_TOKEN` | empty | When set, every request must carry `Authorization: Bearer <token>`; anything else gets `401 Unauthorized`. |
| `metrics.tls_cert_file` | `BOR_METRICS_TLS_CERT_FILE` | empty | PEM certificate. Together with the key, switches the listener to HTTPS. |
| `metrics.tls_key_file` | `BOR_METRICS_TLS_KEY_FILE` | empty | PEM private key for the certificate above. |

The certificate and key must be set together. If only one of them is set the
server refuses to start with:

```text
Failed to load configuration: both BOR_METRICS_TLS_CERT_FILE and BOR_METRICS_TLS_KEY_FILE must be set, or neither
```

Example: expose the endpoint on a management network with a token and TLS.

```yaml
metrics:
  listen_addr: "192.0.2.10:9090"
  tls_cert_file: "/etc/bor/metrics.crt"
  tls_key_file: "/etc/bor/metrics.key"
```

Put the token in `/etc/bor/server.env` instead of the YAML file:

```bash
BOR_METRICS_TOKEN=3f1c...your-random-token...
```

Generate a token with `openssl rand -hex 32`. The certificate and key files
must be readable by the `bor` user. Restart the server to apply changes:

```bash
sudo systemctl restart bor-server
```

Verify from the Prometheus host:

```bash
curl -s -H "Authorization: Bearer $TOKEN" --cacert metrics-ca.crt \
  https://192.0.2.10:9090/metrics | grep '^bor_'
```

!!! note
    The metrics listener is plain HTTP unless you configure a certificate. When
    you bind it to anything other than the loopback address, set a bearer token,
    enable TLS, and restrict the port with a firewall.

## Metrics reference

Most Bor metrics are **gauges** computed from the database at scrape time:
each scrape runs a few `COUNT ... GROUP BY` queries against PostgreSQL. The
exceptions are the four counters marked below, which count events since the
server process started and reset to zero on restart.

The endpoint also exports the standard Go runtime (`go_*`) and process
(`process_*`) metric families.

### Nodes

`bor_nodes_total` (gauge, label `status`)
:   Number of enrolled nodes by their cached connection status. The series for
    `online`, `degraded`, `offline` and `unknown` are always present (zero when
    empty). A node becomes `online` when its agent completes the initial policy
    sync on port 8444, `offline` when that stream closes, and is `unknown`
    until its agent has connected for the first time.

`bor_node_certificate_expiry_seconds` (gauge, labels `node`, `fqdn`)
:   Seconds until the node's mTLS client certificate expires. Negative values
    mean the certificate has already expired. `node` is the node name in Bor
    (the certificate common name), `fqdn` is the host name the agent reported.
    Agent certificates are issued for 90 days.

`bor_node_last_seen_seconds` (gauge, labels `node`, `fqdn`)
:   Unix timestamp of the last heartbeat. The agent sends a heartbeat each time
    it (re)connects its policy stream and when an administrator requests a
    metadata refresh, **not** periodically. A node with a healthy, long-lived
    connection can therefore have an old value. Use `bor_nodes_total` for
    liveness and this metric to see when a node last reconnected.

### Policies and bindings

`bor_policies_total` (gauge, labels `state`, `type`)
:   Number of policies by lifecycle state (`draft`, `released`, `archived`) and
    policy type (for example `Firefox`, `Dconf`, `Kconfig`, `Polkit`). Only
    combinations that exist are exported. Only `released` policies are
    delivered to agents.

`bor_policy_bindings_total` (gauge, label `state`)
:   Number of policy-to-node-group bindings, `enabled` or `disabled`. Both
    series are always present.

### Compliance

`bor_compliance_results_total` (gauge, label `status`)
:   Number of compliance results, one per (node, policy) pair, by the status
    the agent last reported. The series `compliant`, `non_compliant`, `error`
    and `unknown` are always present. Results with the status `inapplicable`
    (the policy does not apply to that endpoint, for example the target
    software is not installed) are not exported.

Fleet compliance ratio:

```text
bor_compliance_results_total{status="compliant"}
  / ignoring(status)
sum without(status) (bor_compliance_results_total)
```

### Users

`bor_users_total` (gauge, labels `source`, `enabled`)
:   Number of user accounts by authentication source (`local`, `ldap`, and
    other sources if present) and whether the account is enabled (`true` or
    `false`).

### Audit

`bor_audit_events_total` (gauge, label `action`)
:   Number of rows in the audit log, per action (for example `create`,
    `update`, `delete`, `tamper_detected`, `auth.login`, `access_denied`).
    This is a snapshot of the table, not a monotonic counter: it drops when
    the retention job purges old entries. An action with no rows has no series.

### Agent package downloads

`bor_agent_package_downloads_total` (counter, labels `format`, `arch`)
:   Agent packages downloaded from this server's `/agent/` repository.
    `format` is `deb`, `rpm`, `apk` or `arch`; `arch` is `amd64`, `arm64`,
    `ppc64le` or `unknown`. Only successful `GET` requests for package files
    are counted, not index or key files.

### Flatpak catalog

`bor_flatpak_catalog_apps_total` (gauge, label `repo`)
:   Applications indexed in the server-side Flatpak catalog
    (**Settings -> Flatpak Repositories**), per repository.

`bor_flatpak_catalog_last_success_timestamp` (gauge, label `repo`)
:   Unix timestamp of the last successful catalog refresh or upload per
    repository, `0` if the catalog was never refreshed.

`bor_flatpak_catalog_refresh_total` (counter, labels `repo`, `outcome`)
:   Catalog refresh attempts. `outcome` is `ok`, `unchanged` (the repository
    answered "not modified"), `error`, or `upload` (catalog uploaded by hand).

### Disk encryption

These metrics carry aggregate labels only, never node names or key material.

`bor_luks_volumes_total` (gauge, label `escrowed`)
:   LUKS volumes reported by agents, split by whether an active recovery key is
    escrowed on the server (`true`/`false`).

`bor_luks_recovery_keys_overdue_total` (gauge, no labels)
:   Volumes with a pending recovery-key rotation request that the node has not
    completed yet.

`bor_luks_escrow_operations_total` (counter, labels `op`, `outcome`)
:   Recovery-key escrow operations. `op` is `escrow`, `confirm`, `release` or
    `destroy`; `outcome` is `ok` or `error`.

`bor_luks_recovery_key_reveals_total` (counter, no labels)
:   Recovery keys revealed to administrators. Each reveal is also recorded in
    the audit log.

!!! note "Missing series mean a failed query"
    If a database query fails during a scrape, the server logs a line starting
    with `metrics:` (for example `metrics: CountByStatus: ...`) and simply
    omits that metric from the response. The scrape itself still succeeds.
    Alerting on `absent(bor_nodes_total)` therefore catches a server that is up
    but cannot query its database.

## Prometheus scrape configuration

Scraping from the Bor server itself (default loopback listener):

```yaml
scrape_configs:
  - job_name: bor
    static_configs:
      - targets: ["127.0.0.1:9090"]
```

Scraping a remote listener with TLS and a bearer token:

```yaml
scrape_configs:
  - job_name: bor
    scheme: https
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/bor-metrics.token
    tls_config:
      ca_file: /etc/prometheus/bor-metrics-ca.crt
    static_configs:
      - targets: ["bor.example.com:9090"]
```

Each scrape runs several database queries, so a scrape interval of 30 to 60
seconds is plenty.

## Suggested alerts

The rules below only use metrics listed above. Adjust thresholds and `for`
durations to your fleet.

```yaml
groups:
  - name: bor
    rules:
      - alert: BorMetricsDown
        expr: up{job="bor"} == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Bor metrics endpoint is not reachable"

      - alert: BorDatabaseQueriesFailing
        expr: absent(bor_nodes_total{job="bor"})
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "bor-server returns no node metrics (database queries failing or target down)"

      - alert: BorNodeCertificateNotRenewed
        expr: bor_node_certificate_expiry_seconds < 21 * 86400
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Certificate of node {{ $labels.node }} expires in less than 21 days"
          description: >
            The agent renews its certificate when the bor-agent service starts
            and the certificate is within 30 days of expiry. Restart bor-agent
            on {{ $labels.fqdn }} while the certificate is still valid.

      - alert: BorNodeCertificateExpired
        expr: bor_node_certificate_expiry_seconds < 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Certificate of node {{ $labels.node }} has expired"
          description: >
            The agent can no longer connect or renew. Re-enroll the node.

      - alert: BorNodesOffline
        expr: bor_nodes_total{status="offline"} > 0
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} Bor node(s) offline"

      - alert: BorComplianceFailures
        expr: bor_compliance_results_total{status=~"non_compliant|error"} > 0
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} compliance result(s) in state {{ $labels.status }}"

      - alert: BorTamperDetected
        expr: delta(bor_audit_events_total{action="tamper_detected"}[1h]) > 0
        labels:
          severity: warning
        annotations:
          summary: "A Bor-managed file was modified outside Bor"
          description: "Check Audit Logs for tamper_detected events."

      - alert: BorFlatpakCatalogStale
        expr: time() - bor_flatpak_catalog_last_success_timestamp > 3 * 86400
        for: 1h
        labels:
          severity: info
        annotations:
          summary: "Flatpak catalog {{ $labels.repo }} not refreshed for 3 days"

      - alert: BorLuksRotationOverdue
        expr: bor_luks_recovery_keys_overdue_total > 0
        for: 1d
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} LUKS volume(s) have not completed a requested key rotation"

      - alert: BorLuksVolumesWithoutEscrow
        expr: bor_luks_volumes_total{escrowed="false"} > 0
        for: 1d
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} LUKS volume(s) have no escrowed recovery key"

      - alert: BorLuksEscrowErrors
        expr: increase(bor_luks_escrow_operations_total{outcome="error"}[1h]) > 0
        labels:
          severity: warning
        annotations:
          summary: "Recovery-key escrow operation {{ $labels.op }} failed"
```

Notes on these rules:

- `BorNodeCertificateNotRenewed`: the agent checks its certificate only when
  the `bor-agent` service starts. An agent that runs for months without a
  restart (or reboot) is not renewed until it restarts, so an alert well inside
  the 30-day renewal window tells you which endpoints need a restart.
- `BorTamperDetected` uses `delta` because `bor_audit_events_total` is a gauge.
  The very first tamper event creates the series and is not seen by `delta`;
  check the **Audit Logs** page as well.
- `BorFlatpakCatalogStale` only makes sense when catalog refresh is enabled
  (`flatpak_catalog.refresh_enabled`, on by default). Each repository's refresh
  interval is configured per repository; adjust the threshold to match.
- Do not alert on `bor_node_last_seen_seconds` age: as explained above, it only
  changes when an agent reconnects.

## Health checks

Bor has no dedicated health or readiness endpoint. Use these instead:

| Check | Command | What it proves |
|-------|---------|----------------|
| HTTPS listener (web port) | `curl -fsS --cacert ca.crt https://bor.example.com/api/v1/version` | The UI/enrollment listener accepts TLS and HTTP. Returns `{"version":"..."}` without authentication and without touching the database. |
| Agent listener (8444) | `openssl s_client -connect bor.example.com:8444 -CAfile ca.crt </dev/null` | The mTLS port is reachable and presents a certificate signed by the Bor CA. |
| Database | `absent(bor_nodes_total)` in Prometheus, or the `metrics:` lines in the server log | The server can query PostgreSQL. |
| Service state | `systemctl is-active bor-server` | systemd sees the process running. |

Download `ca.crt` once from `https://bor.example.com/agent/ca.crt` (when
the server ships the agent repository), or copy it from
`/var/lib/bor/pki/ca/ca.crt` on the server. If the UI port uses an ACME or
other publicly trusted certificate, omit `--cacert` for the web port check; port
8444 always uses the certificate signed by the internal CA.

!!! tip
    Because `/api/v1/version` does not query the database, combine it with the
    database check above in any load balancer or uptime monitor that should
    detect a broken database connection.

## Logs

Both services log to the systemd journal.

### Server

```bash
sudo journalctl -u bor-server -f              # follow
sudo journalctl -u bor-server -b              # since last boot
sudo journalctl -u bor-server --since "1 hour ago"
sudo journalctl -u bor-server -p warning      # warnings and errors only
```

Useful lines to look for:

| Log line | Meaning |
|----------|---------|
| `Bor Policy Management Server <version> (community edition)` | Startup |
| `HTTPS + enrollment gRPC listening on :<port>` | UI and enrollment port (`server.enrollment_port`) is up |
| `Agent policy gRPC (mTLS) listening on :8444` | Agent port is up |
| `Applied migration: <version>` | A database schema migration ran (after upgrades) |
| `Client <node> entering watch mode at revision <n>` | An agent finished its initial sync and is streaming |
| `LDAP authentication failed for user <name>: <reason>` | Failed LDAP login, with the LDAP error |
| `Audit log retention: purged <n> records older than <d> days` | Retention job removed old audit entries |
| `WARNING: JWT_SECRET not set` | No JWT secret configured; sessions do not survive restarts |

### Agent

On each endpoint:

```bash
sudo journalctl -u bor-agent -f
sudo systemctl status bor-agent
```

| Log line | Meaning |
|----------|---------|
| `Server enrollment: <host>:<port>  policy: <host>:8444` | Addresses read from `/etc/bor/config.yaml` |
| `Agent is enrolled ... using mTLS credentials` | Certificates found in `/var/lib/bor/agent` |
| `Connecting to policy stream (last_known_revision=<n>)...` | Connecting to port 8444 |
| `Policy update: type=<type> id=<id> name=<name> version=<v>` | A policy was received |
| `Received empty snapshot (no policies assigned)` | Connected, but no released policy is bound to the node's groups |
| `Policy stream disconnected: <error>` | Connection lost; the agent retries with backoff up to 60 seconds |
| `Certificate expires within 30 days, renewing...` | Automatic renewal at service start |
| `Tamper protection: restoring <path>` | A managed file was changed locally and is being restored |

See [Troubleshooting](troubleshooting.md) for what to do when these messages
point to a problem.
