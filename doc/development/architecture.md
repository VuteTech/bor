# Architecture

This page describes how Bor is put together for people who work on its code:
the two binaries, the two server ports and why they are split, the trust
model between server and agents, the data model behind policies and RBAC,
and the main runtime flows (enrollment, policy streaming and certificate
renewal). Details of each side live in [Server internals](server.md),
[Agent internals](agent.md), [Protocol and code generation](protocol.md) and
[Database and migrations](database.md).

## Components

Bor has two deliverables, both written in Go and kept in one repository:

| Component | Source | Binary | Runs as |
|---|---|---|---|
| Server | `server/` (entry point `server/cmd/server`, wiring in `server/pkg/app`) | `bor-server` | unprivileged user `bor` |
| Agent | `agent/` (entry point `agent/cmd/agent`) | `bor-agent` | `root` (systemd unit `bor-agent.service`) |
| Web UI | `server/web/frontend` (PatternFly 6, React, TypeScript) | embedded into the server binary | browser |
| Protocol | `proto/` | generated Go in `server/pkg/grpc/*`, TypeScript in `server/web/frontend/src/generated/proto` | - |

The agent module depends on the server module for the generated protobuf
code (`github.com/VuteTech/Bor/server/pkg/grpc/...`) through a `replace`
directive in `agent/go.mod` (`github.com/VuteTech/Bor/server => ../server`),
so both modules must always build from the same checkout.

PostgreSQL is the only external dependency of the server. All persistent
state lives there: policies, bindings, nodes, users, RBAC, audit log,
compliance results, enrollment tokens and the policy hub revision counter.

```mermaid
--8<-- "diagrams/dev-components.mmd"
```

## The dual-port design

The server opens two TLS listeners (plus an optional metrics listener). The
split keeps browsers and unenrolled agents away from the port that serves
policy, and lets each port use the TLS settings that fit its clients.

### Web port (`server.enrollment_port`, default 8443)

One `http.Server` serves the web UI, the REST API, the agent package
repository under `/agent/` and the enrollment gRPC service. The handler in
`server/pkg/app/app.go` routes each request by protocol and content type:

```go
uiGrpcRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
        enrollGrpcSrv.ServeHTTP(w, r)
    } else {
        // Security headers + CSRF applied to HTTP only, not gRPC.
        api.SecurityHeadersMiddleware(
            api.CSRFMiddleware(mux),
        ).ServeHTTP(w, r)
    }
})
```

TLS settings on this port:

- `ClientAuth: tls.VerifyClientCertIfGiven`. Browsers and enrolling agents
  have no client certificate, so the handshake must succeed without one;
  if a certificate is presented it must chain to the internal CA.
- TLS 1.2 minimum, ECDHE with AES-GCM suites only for TLS 1.2, curve
  preference X25519, P-256, P-384.
- The certificate comes either from a static file / the auto-generated
  server certificate, or from ACME (`pki.NewACMEManager`) when ACME is
  enabled.

On the gRPC side, `RequireClientCertInterceptor` (in
`server/internal/grpc/interceptor.go`) still requires a verified client
certificate for every enrollment RPC except the two bootstrap calls
`/bor.enrollment.v1.EnrollmentService/Enroll` and
`/bor.enrollment.v1.EnrollmentService/KerberosEnroll`.

### Policy port (`server.policy_port`, default 8444)

A second `http.Server` serves only the `bor.policy.v1.PolicyService` gRPC
server. It is agent-only:

- `ClientAuth: tls.RequireAndVerifyClientCert` against the internal CA pool.
- TLS 1.3 minimum, ALPN `h2` only.
- Always presents the internal-CA-signed server certificate, even when ACME
  is active on the web port, so agents can verify it with the `ca.crt` they
  received at enrollment.
- Unary and stream interceptors check the client certificate serial against
  the `revoked_certificates` table on every call.

Both gRPC servers share `grpcHardeningOptions()`: at most 256 concurrent
streams, 8 MiB maximum message size, keepalive enforcement (minimum ping
interval 10 s) and a one-hour server keepalive.

### Metrics listener

`metrics.NewServer` serves Prometheus `/metrics` on `metrics.listen_addr`
(default `127.0.0.1:9090`), plain HTTP unless a TLS certificate and key are
configured, optionally behind a bearer token. See
[Server internals](server.md#metrics).

## Trust model

| Actor | Authenticates with | Identity used by the server |
|---|---|---|
| Browser user | Password (local or LDAP), optional TOTP or WebAuthn; then a JWT in the `bor_session` httpOnly cookie | user ID in the JWT claims |
| REST API client | `Authorization: Bearer <JWT>` | user ID in the JWT claims |
| Enrolling agent | One-time enrollment token, or a Kerberos SPNEGO token from the machine keytab | node group bound to the token (or the configured Kerberos default group) |
| Enrolled agent | mTLS client certificate signed by the internal CA | certificate Common Name = node name |

Key properties:

- **The internal CA is the root of agent trust.** It is created on first
  start (`pki.EnsureCA`) or loaded from files or a PKCS#11 token. Agent
  certificates are ECDSA P-256, valid for 90 days.
- **The certificate CN is authoritative.** `SignCSR` overwrites whatever CN
  the CSR carries with the server-assigned node name. Every policy RPC calls
  `requireClientIdentity`, which rejects requests whose `client_id` field
  does not match the CN.
- **Enrollment tokens** are single use and expire after 5 minutes
  (`enrollmentTokenTTL` in `server/internal/services/enrollment.go`). Only
  their SHA-256 hash is stored.
- **Revocation** is enforced by the server, not by CRLs: deleting a node or
  revoking its certificate adds the serial to `revoked_certificates`, and
  the interceptors refuse that serial on both ports.
- **The agent trusts the server** at enrollment time through the system
  trust store (or not at all if `server.insecure_skip_verify` is set, which
  is meant for test setups only). After enrollment it pins the `ca.crt`
  returned by the server and never skips verification.

## Data model

The schema is created by the migrations in
`server/internal/database/migrations`. The tables most code touches:

| Table | Purpose |
|---|---|
| `policies` | Policy artifacts: `name`, `type` (for example `Firefox`, `Dconf`, `Firewalld`), `content` (JSON), `version`, `state`, deprecation fields |
| `node_groups` | Logical groups of nodes; the unit that policies are bound to |
| `nodes` | Enrolled agents: name (unique, equals certificate CN), status, metadata from heartbeats, certificate serial and expiry |
| `node_group_members` | Many-to-many membership of nodes in node groups |
| `policy_bindings` | Policy to node group, with `state` (`enabled` / `disabled`) and `priority` |
| `users`, `user_groups`, `user_group_members` | Local and directory users, and user groups |
| `roles`, `permissions`, `role_permissions` | RBAC roles and their `resource:action` permissions |
| `user_role_bindings`, `user_group_role_bindings` | Role assignments to users and to user groups, global or (where the edition allows) node-group scoped |
| `compliance_results` | Latest compliance status per node and policy, with per-item JSON |
| `audit_logs` | Audit trail written by the database audit sink |
| `enrollment_tokens` | Hashes of pending enrollment tokens |
| `revoked_certificates` | Revoked agent certificate serials |
| `policy_hub_state` | Single row holding the policy hub revision high-water mark |

### Policy lifecycle

A policy has an artifact state (`server/internal/models/models.go`):
`draft`, `released`, `archived`. Transitions are validated in
`validateStateTransition` (`server/internal/services/policy.go`):

- `draft` -> `released` (content is validated for the policy type first)
- `released` -> `archived`
- `released` or `archived` -> `draft` (refused while enabled bindings exist)

A policy can additionally be marked deprecated, with a message and an
optional replacement policy.

Enforcement is controlled separately by the binding. A new binding always
starts `disabled`. An agent receives a policy only when the policy is
`released` **and** at least one `enabled` binding links it to a node group
the node is a member of (`PolicyService.ListPoliciesForNodeGroups`). When
several bindings apply, the policy is delivered with the highest binding
priority (`Policy.priority` in the protocol), which the agent uses to order
merges.

### RBAC

Permissions are `resource:action` pairs (for example `policy:view`,
`binding:create`). Roles group permissions; role bindings give a role to a
user or to a user group. A binding has a scope: global, or one node group.
In the community edition node-group-scoped bindings grant nothing
(`edition.NodeGroupScopedRBAC` is off); see [Server internals](server.md#rbac).

## Main flows

### Enrollment

```mermaid
--8<-- "diagrams/dev-enrollment.mmd"
```

1. An administrator generates a token for a node group
   (`POST /api/v1/node-groups/{id}/tokens`). The server stores the hash and
   returns the token once.
2. On the node, `bor-agent --token-file FILE` (or `BOR_ENROLLMENT_TOKEN`)
   generates an ECDSA P-256 key and a CSR, connects to the web port and calls
   `Enroll`.
3. The server consumes the token, signs the CSR with the internal CA (CN set
   to the node name), creates the node record and its group membership, and
   stores the certificate serial and expiry.
4. The agent writes `agent.key`, `agent.crt` and `ca.crt` to
   `enrollment.data_dir` (default `/var/lib/bor/agent`), prints the next
   steps and exits. The systemd service is started separately.

Domain-joined hosts can use `KerberosEnroll` instead: the agent sends a
SPNEGO token built from `/etc/krb5.keytab`, and the server places the node
in the configured default node group. If Kerberos enrollment fails, the
agent falls back to token enrollment.

### Policy streaming

```mermaid
--8<-- "diagrams/dev-policy-stream.mmd"
```

The agent keeps one server-streaming RPC, `SubscribePolicyUpdates`, open to
the policy port:

1. The agent sends `last_known_revision` (0 after a restart, since the agent
   keeps it in memory only).
2. The server decides the initial sync in
   `PolicyServer.SubscribePolicyUpdates` (`server/internal/grpc/server.go`):
    - revision 0, or higher than the server's: full snapshot;
    - lower than the server's: `PolicyHub.EventsSince`; if the events were
      compacted away, or any of them is a resync signal, a full snapshot is
      sent instead; otherwise the events are replayed;
    - equal: nothing to send.
3. A snapshot is one `SNAPSHOT` message per policy, the last one carrying
   `snapshot_complete = true`. An empty snapshot is a single message with no
   policy and `snapshot_complete = true`.
4. The node is marked `online`, and the stream enters watch mode on a hub
   subscription. It is marked `offline` when the stream ends.
5. When an administrator changes a policy (update, state change,
   deprecation) or a binding (state or priority change, deletion of an
   enabled binding), the REST handler calls `PolicyHub.PublishResync` with
   the affected node group IDs. Every stream whose node belongs to one of
   those groups sends a fresh full snapshot. The agent compares the new
   snapshot with its cache and re-applies what changed.
6. After applying, the agent reports compliance per policy with
   `ReportCompliance`.
7. If the stream breaks, the agent reconnects with exponential backoff
   (1 s doubling, capped at 60 s) and sends the last revision it saw.

!!! note "Snapshots are the common case"
    The protocol defines `CREATED`, `UPDATED` and `DELETED` delta events,
    and the agent handles them, but the server's REST handlers currently
    publish only resync signals. In practice every change reaches agents as
    a scoped full snapshot.

!!! note "Group membership changes"
    The streaming handler reads the node's group memberships when the stream
    opens. Adding a node to, or removing it from, a node group does not
    publish a resync, so the new policy set reaches the agent on its next
    reconnect.

The server also pushes two targeted, non-policy commands over the same
stream: `METADATA_REQUEST` (the agent answers with a `Heartbeat`) and
`DISK_ENCRYPTION_TASK` (the agent fetches its pending disk-encryption
tasks). These do not change the agent's last known revision.

### Certificate renewal

```mermaid
--8<-- "diagrams/dev-cert-renewal.mmd"
```

At start-up, after the enrollment check, the agent calls
`policyclient.CertExpiringSoon` with a 30-day threshold. If the certificate
is close to expiry it generates a new key and CSR with the same CN and calls
`RenewCertificate` on the policy port, authenticated with the current
certificate. The server looks up the node by the verified CN, signs a new
90-day certificate and updates the node's certificate serial and expiry.
The agent then writes the new key and certificate and connects as usual.

The check runs once per agent start, so a daemon that is never restarted
for longer than the certificate lifetime does not renew. Any restart of
the service (for example a reboot) runs the check again.

### Compliance and tamper reports

Besides the stream, the agent makes unary calls on the policy port:

- `ReportCompliance` after each sync, one call per policy, with a
  four-state status and optional per-item results. Stored in
  `compliance_results`.
- `Heartbeat` on every connect and on `METADATA_REQUEST`, carrying OS,
  desktop environments, agent version and machine ID.
- `ReportTamperEvent` when the file watcher restores a modified managed
  file, with the processes that had the file open.
- `ReportSchemaCatalogue` and `ReportPolkitCatalogue` once, with the
  GSettings schemas and polkit actions installed on the node, which feed
  the dconf and polkit editors in the UI.
- The disk-encryption RPCs (`ReportDiskEncryptionState`,
  `GetDiskEncryptionTasks`, `EscrowRecoveryKey`, `ConfirmRecoveryKey`,
  `BeginRecoveryKeyRotation`).
