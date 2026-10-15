# Server internals

This page walks through the Bor server code: what happens at start-up, how
the packages are divided, the conventions every REST handler follows, how
authorization is evaluated, how the policy hub fans changes out to agents,
and where metrics and audit events go. For the bigger picture see
[Architecture](architecture.md).

## Entry point and start-up sequence

`server/cmd/server/main.go` is deliberately small. It parses the
`--reset-mfa <username>` flag (handled by `app.ResetMFA`) and otherwise
calls:

```go
app.Run(app.Options{Version: Version, Edition: edition.Community{}})
```

`app.Run` in `server/pkg/app/app.go` does all the wiring, in this order:

1. **Edition.** `opts.Edition` (default `edition.Community{}`) decides
   optional features. `nodeGroupScopes` is a function evaluated per request,
   so an edition can switch features at run time.
2. **Configuration.** `config.Load()` reads the YAML file named by
   `BOR_CONFIG` (default `/etc/bor/server.yaml`, a missing file is ignored)
   and lets environment variables override each value
   (`server/internal/config/config.go`).
3. **Internal CA.** Either from a PKCS#11 HSM (`pki.EnsureCAWithHSM`), from
   `BOR_CA_CERT_FILE` / `BOR_CA_KEY_FILE`, or auto-generated into the
   autogen directory (`pki.EnsureCA`). The CA is created first because it
   signs the server certificate.
4. **Server TLS certificate.** If no certificate files are configured,
   `pki.EnsureServerCert` creates one signed by the internal CA for the
   configured hostnames.
5. **Database.** `database.New` opens the pool (25 connections maximum) and
   pings PostgreSQL; `db.RunMigrations()` applies pending migrations. Any
   failure is fatal. See [Database and migrations](database.md).
6. **Repositories.** One repository per table group
   (`database.NewPolicyRepository(db)` and so on). Two seeders start in the
   background: the built-in GSettings schemas
   (`server/assets/dconf_builtin_schemas.json`) and polkit actions
   (`server/assets/polkit_builtin_actions.json`).
7. **Identity services.** LDAP (when enabled), MFA (TOTP), WebAuthn (when
   `BOR_WEBAUTHN_RPID` is set) and `AuthService`.
8. **Domain services.** Policy, node, node group, user group, policy binding,
   enrollment, audit and settings services.
9. **Audit sinks.** The database sink is always registered; the syslog sink
   when syslog forwarding is enabled. If a retention period is set
   (`BOR_AUDIT_RETENTION_DAYS`), a goroutine
   purges old audit rows once at start-up and then every 24 hours.
10. **Authorizer.** `authz.New(...)`.
11. **Default administrator.** `EnsureDefaultAdmin` creates the `admin` user
    when the users table is empty, using `BOR_ADMIN_PASSWORD` or a generated
    password written to `/var/lib/bor/initial_admin_password` (mode 0600).
12. **Policy hub.** `grpcserver.NewPersistentPolicyHub` loads the revision
    from `policy_hub_state`.
13. **REST handlers and hooks.** Handlers are created and the policy and
    binding handlers get `OnPolicyChange` / `OnBindingChange` callbacks that
    publish resync signals to the hub.
14. **Feature services.** Agent package repository (`/agent/`), Flatpak
    catalog manager (started immediately), recovery-key escrow (when a KEK
    file is configured), Tang servers and the disk-encryption service, whose
    janitor runs two minutes after start and then hourly.
15. **HTTP routes.** Public, authenticated and permission-gated routes are
    registered on one `http.ServeMux`; extension modules mount theirs via
    `exthost.Init` under `/api/v1/ext/<module>/`; the embedded frontend is
    served on `/`.
16. **Certificates for the web port.** Static, or ACME with an HTTP-01
    challenge listener and TLS-ALPN-01.
17. **Kerberos.** `KerberosService` when Kerberos enrollment is enabled.
18. **gRPC servers.** An enrollment server and a policy server, each with
    client-certificate and revocation interceptors.
19. **Listeners.** The web port, the policy port and the metrics listener
    start in goroutines.
20. **Shutdown.** On `SIGINT` or `SIGTERM`: stop the Flatpak manager and the
    disk-encryption janitor, gracefully stop both gRPC servers, then shut
    down the HTTP servers with a 10-second timeout.

Any error before the listeners start ends the process with `log.Fatalf`.

## Package layout

| Package | Responsibility |
|---|---|
| `server/cmd/server` | Community entry point |
| `server/cmd/protoc-gen-bor-ui` | protoc plugin for UI metadata, see [Protocol](protocol.md#ui-metadata-protoc-gen-bor-ui) |
| `server/pkg/app` | Start-up and wiring (`Run`, `ResetMFA`), plus dconf and polkit seeders |
| `server/pkg/edition` | `Edition` interface and feature switches; `edition.Community` enables none |
| `server/pkg/ext` | Extension API for optional modules (login methods, routes, storage) |
| `server/pkg/grpc/*` | Generated protobuf and gRPC code |
| `server/internal/config` | Configuration from YAML and environment |
| `server/internal/api` | REST handlers and HTTP middleware |
| `server/internal/services` | Business logic, one service per domain |
| `server/internal/database` | PostgreSQL repositories and migrations |
| `server/internal/models` | Data models and request / response DTOs |
| `server/internal/grpc` | `PolicyServer`, `EnrollmentServer`, interceptors, `PolicyHub` |
| `server/internal/authz` | Grant computation and scopes |
| `server/internal/pki` | Internal CA, CSR signing, ACME, PKCS#11 |
| `server/internal/audit` | Audit sink interface, database and syslog sinks, CEF and OCSF formatters |
| `server/internal/metrics` | Prometheus collector and metrics server |
| `server/internal/escrow` | Envelope encryption of LUKS recovery keys under a KEK |
| `server/internal/export` | Policy and binding export / import |
| `server/internal/exthost` | Host side of `pkg/ext`: mounts modules, login method registry |
| `server/internal/flatpakcatalog` | Flatpak remote and AppStream catalog fetching |
| `server/internal/tang` | Tang server client |
| `server/web` | `web.StaticFiles`, the embedded frontend build |

The layering is strict: `api` and `grpc` call `services`, `services` call
`database` repositories, and nothing below `api` knows about HTTP.

## REST conventions

All REST endpoints live under `/api/v1/` on the web port.

### Middleware chain

Every HTTP (non-gRPC) request passes through, outermost first:

1. `SecurityHeadersMiddleware`: Content-Security-Policy,
   `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, HSTS,
   `Referrer-Policy`, `Permissions-Policy`.
2. `CSRFMiddleware`: double-submit check on `POST`, `PUT`, `PATCH` and
   `DELETE` under `/api/`. The `X-CSRF-Token` header must equal the
   `bor_csrf` cookie. Public login endpoints and prefixes registered with
   `api.ExemptFromCSRF` are exempt.
3. Per route: `AuthMiddleware`, then a permission gate, then (for
   state-changing routes) `AuditMiddleware`, then the handler.

A typical protected route:

```go
policyPerms := gate.RequireScopedMethod([]api.MethodPermission{
    {Method: http.MethodGet, Resource: "policy", Action: "view"},
    {Method: http.MethodPost, Resource: "policy", Action: "create"},
    {Method: http.MethodPut, Resource: "policy", Action: "edit"},
    {Method: http.MethodDelete, Resource: "policy", Action: "delete"},
})
mux.Handle("/api/v1/policies/all/", authMiddleware(policyPerms(auditMw(http.HandlerFunc(policyHandler.ServeHTTP)))))
```

Routing is deny-by-default: a route without a gate is reachable only if it
is registered as public (version, public config, login steps, logout,
refresh, agent package downloads) or is a self-service `/api/v1/users/me/...`
or `/api/v1/auth/me` route that needs only authentication. Login endpoints
are rate limited to 10 requests per minute per client IP.

### Sessions

- `AuthMiddleware` takes the JWT from the `bor_session` httpOnly cookie, or
  from `Authorization: Bearer` for API clients, validates it and stores the
  claims in the request context (`api.GetUserFromContext`).
- JWTs are HS256 signed with `JWT_SECRET`. Access tokens default to 1 hour
  (`BOR_JWT_LIFETIME`), refresh tokens (`bor_refresh` cookie) to 24 hours
  (`BOR_REFRESH_LIFETIME`).
- On login the server also sets `bor_csrf`, a non-httpOnly cookie
  (`Secure`, `SameSite=Strict`) that the frontend copies into the
  `X-CSRF-Token` header.

### Errors

Errors are JSON objects with one `error` key and an appropriate status:

```json
{"error":"policy not found"}
```

Use `http.Error(w, `{"error":"..."}`, status)` for constant messages or
`writeJSONError` (in `permission_gate.go`) for formatted ones. Do not leak
internal errors (database messages, file paths) to clients; log them with
`log.Printf` and return a generic message. Successful responses are JSON
with `Content-Type: application/json`.

## RBAC

Authorization is implemented in `server/internal/authz/authz.go` and
enforced by `PermissionGate` in `server/internal/api/permission_gate.go`.

- A permission is `resource:action` (`authz.PermKey`). Roles and their
  permissions come from the `roles`, `permissions` and `role_permissions`
  tables; the built-in roles are seeded by migrations.
- `ComputeGrants` collects the user's effective role bindings (direct
  bindings plus bindings of user groups the user is a member of) and
  produces `Grants`: for each permission, a `Scope` that is global, a set of
  node groups, or empty.
- Node-group-scoped bindings grant only scopable resources (`node`,
  `node_group`, `policy`, `binding`, `compliance`), and only when the
  edition enables `edition.NodeGroupScopedRBAC`. In the community edition
  they grant nothing; only global bindings count. Unknown scope types fail
  closed.
- `gate.Require` / `gate.RequireMethod` admit only global grants. This is
  the default for every route.
- `gate.RequireScoped` / `gate.RequireScopedMethod` also admit scoped
  grants, and may only wrap handlers that check scope per object (they call
  `requestScope(r, resource, action).Allows(groupID)`).
- The gate stores the grants in the request context so handlers do not hit
  the database again. Denied state-changing requests are audited as
  `access_denied` events.

Server-side guards protect against privilege escalation (for example,
`Grants.CanDelegate` checks that a caller only hands out permissions they
hold themselves).

## PolicyHub

`server/internal/grpc/policy_hub.go` is the in-process publish / subscribe
hub between REST handlers and agent streams.

- **Revision counter.** A monotonic `int64`, incremented on every publish.
  `NewPersistentPolicyHub` seeds it from the single-row `policy_hub_state`
  table, and every publish writes the new value back with
  `GREATEST(old, new)` before fanning out. Without this, a restart would
  reuse revision numbers and a reconnecting agent could be served a delta
  that silently skips events.
- **Event log.** `defaultEventLogSize = 1000`. When the log grows past the
  limit, the oldest half is dropped. The log itself is in memory only, so
  after a restart every agent that is behind gets a full snapshot.
- **Delta sync.** `EventsSince(rev)` returns the events after `rev`, or
  `nil` if `rev` is older than the oldest retained event (compacted). The
  stream handler falls back to a snapshot in that case, and also when the
  delta contains a resync signal.
- **Broadcast.** `Subscribe(ctx, clientID)` returns a buffered channel per
  stream. Publishing never blocks: if a subscriber's channel is full, the
  event is dropped for that subscriber and logged.
- **Resync signals.** `PublishResync(groupIDs...)` publishes a `SNAPSHOT`
  event without a policy. Each stream checks whether its node's groups
  overlap the affected groups (empty means all) and, if so, sends a full
  snapshot built from the database. This is what the policy and binding
  REST handlers call.
- **Targeted commands.** `SendMetadataRefreshRequest(clientID)` and
  `SendDiskEncryptionTask(clientID)` send a `METADATA_REQUEST` or
  `DISK_ENCRYPTION_TASK` to one connected agent without bumping the
  revision.

The hub is per process. Running several server instances behind a load
balancer would need a shared bus; today each instance only notifies the
agents connected to it.

## Metrics

`server/internal/metrics` exposes Prometheus metrics on
`metrics.listen_addr` (`BOR_METRICS_ADDR`, default `127.0.0.1:9090`) at
`/metrics`, in OpenMetrics format, with optional bearer token
(`BOR_METRICS_TOKEN`) and optional TLS (`BOR_METRICS_TLS_CERT_FILE`,
`BOR_METRICS_TLS_KEY_FILE`).

`BorCollector` (`collector.go`) queries the repositories on each scrape and
exports gauges such as `bor_nodes_total{status}`,
`bor_node_last_seen_seconds`, `bor_node_certificate_expiry_seconds`,
`bor_policies_total`, `bor_policy_bindings_total`, `bor_users_total`,
`bor_compliance_results_total`, `bor_audit_events_total` and the Flatpak and
LUKS families. Counters that are incremented in code
(`bor_agent_package_downloads_total`, `bor_flatpak_catalog_refresh_total`,
`bor_luks_escrow_operations_total`, `bor_luks_recovery_key_reveals_total`)
are registered on the same registry, together with the Go runtime and
process collectors.

To add a metric, prefer a `prometheus.Desc` in `BorCollector` for values
that can be read from the database, and a dedicated counter type (see
`downloads.go`) for events. Keep label cardinality bounded.

## Audit logging pipeline

The canonical audit record is `bor.audit.v1.AuditEvent`
(`proto/audit/audit.proto`): time, actor, action, resource, outcome, source
IP and a typed payload (HTTP change, tamper event, ...).

```text
REST AuditMiddleware ----\
PermissionGate denials ---+--> AuditService.Emit --> DatabaseSink --> audit_logs
gRPC handlers (tamper) ---+                     \--> SyslogSink --> RFC 5424 syslog
services, extensions ----/                                           (CEF or OCSF body)
```

- `AuditMiddleware` (`server/internal/api/audit_middleware.go`) records
  successful (2xx) `POST`, `PUT`, `PATCH` and `DELETE` requests. It reads
  the JSON body and replaces values whose keys look sensitive (passwords,
  secrets, tokens and similar) with `[REDACTED]` before storing it.
- Source IPs are truncated (/24 for IPv4, /48 for IPv6) when
  `BOR_AUDIT_ANONYMIZE_IPS` is set.
- `AuditService.Emit` hands the event to every registered sink. Sinks must
  not block and must not return errors to the caller.
- `DatabaseSink` flattens the event into `models.AuditLog` and writes
  `audit_logs`.
- `SyslogSink` (`server/internal/audit/syslog.go`) queues events in a
  buffered channel and sends them over UDP, TCP or TLS with RFC 5424
  framing. The body is CEF (`FormatCEF`) or OCSF JSON (`FormatOCSF`). If the
  queue is full, events are dropped with a warning, so a slow SIEM never
  stalls requests.
- The audit log can be exported as CSV or JSON from the REST API
  (`AuditService.ExportCSV`, `ExportJSON`) and purged by the retention job.

When you add a state-changing route, wrap it in `auditMw`. When you add an
event that does not come from an HTTP request (for example a gRPC call),
build an `AuditEvent` and call `auditSvc.Emit`. Never put secrets, private
keys or TOTP seeds in an audit payload.
