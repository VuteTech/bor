# Editions and extensions

The Bor server can be built into other distributions without patching it.
The community binary is a thin `main` package around a reusable server
package, and two public packages define what a distribution may change:
which optional features are on (`server/pkg/edition`) and which extra
modules run inside the server (`server/pkg/ext`). This page describes those
extension points as they exist in this repository.

!!! note "Licensing of distributions"
    The server is licensed under AGPL-3.0-only. Extension modules and
    editions are compiled into the same binary as the server, so a
    distribution built on these packages is a combined work covered by the
    AGPL: its source code, including the modules, must be available under
    AGPL-compatible terms to everyone who receives the binary or uses the
    server over a network. Bor Enterprise is built on the same extension
    points and is distributed by Vute Tech LTD under a commercial license.
    For a commercial license for your own distribution, contact
    <bor@vute.tech>.

```mermaid
--8<-- "diagrams/dev-extension-host.mmd"
```

| Package | Role |
|---|---|
| `server/pkg/app` | The whole server: `app.Run(app.Options)` and `app.ResetMFA(username)` |
| `server/pkg/edition` | The `Edition` interface, the `Community` edition and the feature flags |
| `server/pkg/ext` | The extension API: `Module`, `Host` and the types a module uses |
| `server/internal/exthost` | The server-side implementation of `Host` (not importable from outside the module) |

## The server entry point: `pkg/app`

`server/cmd/server/main.go` is the complete community entry point:

```go
func main() {
	resetMFAUser := flag.String("reset-mfa", "", "Disable MFA for the given username and exit")
	flag.Parse()

	if *resetMFAUser != "" {
		if err := app.ResetMFA(*resetMFAUser); err != nil {
			log.Fatalf("reset-mfa failed: %v", err)
		}
		return
	}

	app.Run(app.Options{Version: Version, Edition: edition.Community{}})
}
```

`app.Run` loads the configuration, prepares the database, PKI and
services, serves the web UI, REST API and both gRPC listeners, and blocks
until `SIGINT` or `SIGTERM`, then shuts down gracefully. Startup failures
are fatal. Its options:

```go
type Options struct {
	// Version is reported in logs, the UI and /api/v1/version.
	Version string
	// Edition selects the optional features the server runs with; nil means
	// the community edition (no optional features).
	Edition edition.Edition
	// Extensions are optional modules (login methods, routes, storage)
	// initialized at startup; see pkg/ext. The community edition has none.
	Extensions []ext.Module
}
```

An empty `Version` becomes `dev`. The community build sets it with
`-ldflags "-X main.Version=x.y.z"`.

## Editions: `pkg/edition`

```go
type Edition interface {
	// Name is a short, human-readable edition name such as "community".
	Name() string
	// Enabled reports whether the feature is available right now.
	Enabled(f Feature) bool
}
```

`edition.Community{}` returns `"community"` and enables nothing.
`Enabled` is consulted per request, not once at startup, so an edition can
switch a feature on or off at run time (for example when a license
expires) without a restart.

### Features

`Feature` is a string type. The known features are listed in
`edition.AllFeatures`; there are currently two:

`NodeGroupScopedRBAC` (`"node_group_scoped_rbac"`)
:   Role bindings scoped to a single node group (delegated
    administration). When it is off, only global role bindings can be
    created, and existing node-group-scoped bindings grant nothing. The
    server passes the check into the authorizer (`authz.New`) and into the
    auth, user-group and role-binding services, all wired in
    `server/pkg/app/app.go`:

    ```go
    nodeGroupScopes := func() bool { return ed.Enabled(edition.NodeGroupScopedRBAC) }
    ```

`ComplianceReports` (`"reports"`)
:   Point-in-time compliance reports. They are implemented entirely by the
    enterprise `reports` extension module (storage, generation, API under
    `/api/v1/ext/reports/`, signing, documents, schedules); the community
    server contains only the web UI pages, which show the **Reports** page
    while the edition enables the feature (the feature list of
    `/api/v1/auth/me` drives it) and a Bor Enterprise notice otherwise.

To add a feature, declare a new `Feature` constant, append it to
`AllFeatures`, and gate the behavior on `ed.Enabled(...)` at request time.

### How the web UI learns the edition

`GET /api/v1/auth/me` returns the edition name and the features that are
enabled at that moment, next to the user's permissions:

```json
{
  "id": "...",
  "username": "admin",
  "permissions": ["policy:view", "..."],
  "permission_scopes": {"...": "..."},
  "edition": "community",
  "features": []
}
```

`features` is always an array (empty in the community edition), filled
from `edition.EnabledFeatures(ed)` in `AllFeatures` order. The web UI
stores it in `apiClient/permissions.ts` and checks it with
`hasFeature(FEATURE_NODE_GROUP_SCOPED_RBAC)`. With the feature off, the
scope picker and scope column of role bindings are hidden unless scoped
bindings already exist; those stay visible and removable, labeled
"(inactive in this edition)" (`components/RoleScopeFields.tsx`).

## Extensions: `pkg/ext`

An extension is a Go value that implements `ext.Module`:

```go
type Module interface {
	// Name is a short, stable identifier ([a-z0-9-]+). It scopes the
	// module's routes (/api/v1/ext/<name>/...) and migrations.
	Name() string
	// Init is called once at startup, after the core services exist and
	// before the server accepts requests. An error aborts startup.
	Init(ctx context.Context, host Host) error
}
```

`app.Run` passes `Options.Extensions` to `exthost.Init`, which checks each
name (`^[a-z0-9][a-z0-9-]{0,31}$`, unique across modules), creates a `Host`
bound to that name and calls `Init` in order. Everything a module mounts,
stores or logs goes through its `Host`, so the server keeps control of
routing, authorization, sessions and auditing.

### What a module can register

Routes
:   `HandlePublic(pattern, handler)` mounts an unauthenticated handler,
    for example an identity provider callback; the handler authenticates
    requests itself and the route is exempt from the CSRF check.
    `HandleAdmin(pattern, resource, action, handler)` mounts a handler
    behind the session middleware and a global `resource:action`
    permission check (for example `settings`, `manage`); state-changing
    requests are recorded by the audit middleware. Every pattern must be a
    plain path under `/api/v1/ext/<name>/`; a pattern outside the prefix
    or a duplicate is returned as an error.

Login methods
:   `Logins().Register(ext.LoginMethod{ID, DisplayName, StartURL, Enabled})`
    adds a "Sign in with ..." button to the login page. The methods are
    served to the login page by `GET /api/v1/config` (`login_methods`);
    `Enabled` is evaluated on every page load, and `StartURL` must be a
    route of the module.

Account claims and other login hooks
:   For federated accounts the module owns
    (`User.FederationProvider`):

    - `ClaimAccounts(provider, startURL)`: when such a user enters their
      username, the login page goes to `startURL(username)` instead of
      asking for a password.
    - `ClaimStepUp(provider, startURL)`: before a privileged action, the
      web UI opens `startURL(username, purpose)` in a popup instead of
      asking for a password the user does not have; the module calls
      `Identity().CompleteStepUp(...)` and redirects to
      `ext.StepUpDoneURL(result)`.
    - `ClaimLogout(provider, logoutURL)`: after such a user logs out of
      Bor, the browser goes to `logoutURL` so the module can end the
      identity provider session (single logout); the route calls
      `Identity().ConsumeLogoutTicket(...)`.

Settings panels
:   `RegisterSettingsPanel(ext.SettingsPanel{ID, Title, DataURL})` adds a
    tab to **Settings** for users with `settings:manage`. The web UI lists
    the panels from `GET /api/v1/ext-panels` and renders each one with the
    generic `ExtensionPanelTab` (`views/Settings/ExtensionPanelTab.tsx`).
    `DataURL` must be a `HandleAdmin` route of the module that returns an
    `ext.Panel` as JSON: sections with a status, read-only fields (with an
    optional copy button), tables and actions. An action is a button that
    sends `Method` to a module URL and reloads the panel; a `Danger`
    action requires typing its `ConfirmPhrase`, and `Result: "secret"`
    shows a returned token once with a copy control.

Storage
:   `DB()` returns the server's `*sql.DB` for the module's own tables.
    `RunMigrations(fsys, dir)` applies the module's `*.up.sql` files in name
    order, once each, each in its own transaction. They are tracked per
    module in `ext_schema_migrations`, so their numbering is independent
    of the server's.

Users, groups and sessions
:   `Identity()` manages federated accounts: find, create, update, rename,
    link (`LinkFederatedIdentity`, `LinkAccountByEmail`), enable or disable,
    revoke sessions, delete (for erasure requests); module-owned user groups
    (`CreateManagedGroup` and related methods, owned per module); group
    membership sync; and `StartSession`, which signs a user in exactly like
    the built-in login (session, refresh and CSRF cookies plus an
    `auth.login` audit event) and applies Bor's own second factor according
    to `AuthContext.LocalMFA`. Account changes made through `Identity()`
    are audited with the module as actor, or the actor set with
    `ext.WithActor(ctx, ...)`.

Audit and secrets
:   `Audit().Record(ctx, r, &ext.Event{...})` writes an audit event (never
    put secrets in `Details`). `Secrets()` returns an AES-256-GCM box whose
    key is derived per module with HKDF-SHA256 from the server's JWT
    secret, for encrypting small values such as client secrets at rest.
    `RequestUser(r)` returns the signed-in user inside a `HandleAdmin`
    route.

Permissions for user routes
:   `HandleUser(pattern, h)` mounts a handler for any signed-in user; the
    handler checks each operation with `Authorize(w, r, resource, action,
    scoped)`, which returns where the user holds the permission (globally,
    or the node groups of a scoped role binding) and answers 403 with an
    audited denial otherwise. `RegisterScopableResource(resource)` lets
    node-group-scoped role bindings grant the module's permissions; the
    module must then confine every operation to the returned groups
    (`DenyOutOfScope` refuses and audits an object outside them).

Agents, compliance results and certificates
:   `Agents().Resync(nodeName)` asks a connected agent to re-apply its
    policies and report compliance again.
    `RegisterComplianceListener(l)` tells the module about every
    compliance result an agent reports, with the previous status, the
    items and the node's operating system; the listener runs on the
    agent's request and must return quickly. `SIEM()` forwards a
    pre-formatted message with the audit log's syslog forwarding (and
    reports its format, `ocsf` or `cef`, or none). `PKI()` gives access to
    the server's certificate authority: `CACertificatePEM()` and
    `IssueCertificate(ctx, ext.CertificateRequest{...})`, which today
    issues document-signing certificates (extended key usage
    id-kp-documentSigning, no TLS usages) for a public key the module
    generated and keeps itself.

Metrics
:   `RegisterMetrics(collector)`, called from `Init`, adds a Prometheus
    collector to the server's `/metrics` endpoint. Metric names start with
    `bor_`; a collector the registry rejects (an invalid or clashing
    metric) is logged and left out without stopping the server.

The community edition registers no modules and enables no features, so
`/api/v1/ext-panels` returns an empty list, the login page shows no extra
buttons and no compliance report routes exist.

## Building a distribution

A distribution is a separate Go module with its own `main` package that
imports `github.com/VuteTech/Bor/server/pkg/app`, supplies its own
`Edition` and modules, and calls `app.Run`:

```go
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/pkg/app"
	"github.com/VuteTech/Bor/server/pkg/edition"
	"github.com/VuteTech/Bor/server/pkg/ext"
)

var Version = "dev"

// myEdition enables delegated administration.
type myEdition struct{}

func (myEdition) Name() string { return "example" }
func (myEdition) Enabled(f edition.Feature) bool {
	return f == edition.NodeGroupScopedRBAC
}

// hello is a minimal module with one admin route.
type hello struct{}

func (hello) Name() string { return "hello" }
func (hello) Init(_ context.Context, h ext.Host) error {
	return h.HandleAdmin("/api/v1/ext/hello/ping", "settings", "manage",
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
}

func main() {
	resetMFAUser := flag.String("reset-mfa", "", "Disable MFA for the given username and exit")
	flag.Parse()
	if *resetMFAUser != "" {
		if err := app.ResetMFA(*resetMFAUser); err != nil {
			log.Fatalf("reset-mfa failed: %v", err)
		}
		return
	}
	app.Run(app.Options{
		Version:    Version,
		Edition:    myEdition{},
		Extensions: []ext.Module{hello{}},
	})
}
```

Points to keep in mind:

- Only `pkg/app`, `pkg/edition`, `pkg/ext` and the generated
  `pkg/grpc/...` packages are importable; everything under
  `server/internal/` is private to the server module by Go's rules.
- Keep the `--reset-mfa` flag (or an equivalent) so administrators keep
  the recovery path documented for the community server.
- The web UI is embedded in `pkg/app` through `server/web`, so a
  distribution ships the same frontend; extensions add UI only through
  settings panels and login methods.
- Build with the same flags as the community server (see
  [Building and packaging](building.md)), in particular
  `GOFIPS140=v1.0.0`, and set the version with
  `-ldflags "-X main.Version=..."`.
