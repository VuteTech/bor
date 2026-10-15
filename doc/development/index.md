# Development

This section is for people who work on Bor itself: Go developers on the
server and agent, frontend developers on the web UI, and packagers. It covers
how the repository is laid out, how to set up a development environment, how
the code is built, tested and released, and how the main components work
inside.

If you only want to deploy and operate Bor, start with
[Installation](../installation/requirements.md) instead.

## Where to start

| Page | What it covers |
|---|---|
| [Development environment](setup.md) | Prerequisites, local PostgreSQL, running the server, frontend watch mode, running an agent against a dev server |
| [Building and packaging](building.md) | Makefile targets, FIPS 140-3 build flags, nfpm packages, the agent package repository, the container image, the CI pipeline |
| [Architecture](architecture.md) | How the server, agent and web UI fit together |
| [Protocol and code generation](protocol.md) | The protobuf definitions in `proto/` and the generated Go and TypeScript code |
| [Server internals](server.md) | Packages under `server/internal`, request flow, the policy hub |
| [Agent internals](agent.md) | Enrollment, the policy stream, enforcement and the file watcher |
| [Database and migrations](database.md) | PostgreSQL schema and the embedded SQL migrations |
| [Frontend](frontend.md) | The PatternFly web UI in `server/web/frontend` |
| [Adding a policy type](adding-a-policy-type.md) | End-to-end checklist for a new policy type |
| [Editions and extensions](editions.md) | The public extension points in `server/pkg/app`, `server/pkg/edition` and `server/pkg/ext` |
| [Testing and linting](testing.md) | Go tests, golangci-lint, frontend typecheck and guardrails |
| [Internals: dconf](internals/dconf.md), [Polkit](internals/polkit.md) | Deep dives into two policy types |
| [Writing documentation](documentation.md) | How this documentation site is built |
| [Contributing](contributing.md) | Workflow, commit messages, coding conventions, license |

## Repository layout

Bor is a single Git repository with two Go modules, a React frontend that is
compiled into the server binary, and shared protobuf definitions.

| Path | Contents |
|---|---|
| `server/` | Go module `github.com/VuteTech/Bor/server`: the server binary, REST API, gRPC services, internal CA, database layer and the web UI |
| `server/cmd/server/` | Server entry point. `main.go` only sets the version and edition and calls `app.Run` in `server/pkg/app` |
| `server/cmd/protoc-gen-bor-ui/` | A protoc plugin that generates policy editor metadata for the web UI |
| `server/internal/` | Server packages: `api`, `services`, `database`, `models`, `grpc`, `pki`, `config`, `authz`, `audit`, `escrow`, `export`, `exthost`, `flatpakcatalog`, `metrics`, `tang` |
| `server/pkg/` | Importable packages: `app` (server wiring), `edition`, `ext`, and the generated protobuf code under `grpc/` |
| `server/web/frontend/` | PatternFly web UI source (TypeScript, webpack) |
| `server/web/static/` | Built web UI bundle, embedded into the server binary |
| `server/assets/` | Built-in dconf schema and polkit action catalogs (JSON, embedded) |
| `agent/` | Go module `github.com/VuteTech/Bor/agent`: the `bor-agent` daemon |
| `agent/cmd/agent/` | Agent entry point: enrollment, policy stream, reconnect loop, file watcher hooks |
| `agent/cmd/gen-dconf-schemas/`, `agent/cmd/gen-polkit-actions/` | Generators for the catalogs in `server/assets/` |
| `agent/internal/` | Agent packages: `config`, `policyclient`, `policy`, `filewatcher`, `luks`, `logind`, `sessionaccess`, `notify`, `procinfo`, `sysinfo` |
| `proto/` | Protobuf definitions: `policy/` (policy service and one file per policy type), `enrollment/`, `audit/`, `export/`, `bor/ui.proto` (UI field options) |
| `packaging/` | nfpm configurations (`nfpm-server.yaml`, `nfpm-agent.yaml`), install/remove scripts and systemd units |
| `scripts/` | Build helpers: `assemble-agent-repo.sh`, `check-frontend-tokens.sh` and its baseline file |
| `doc/` | This documentation site (MkDocs). Diagram sources live in `doc/diagrams/` |
| `.github/workflows/` | The CI pipeline (`build-packages.yml`) |
| `podman-compose.yml` | Local development stack (PostgreSQL, a syslog receiver, the server container) |
| `.env.example` | Template for the local server environment |

The `dev/` directory is listed in `.gitignore`. It is the place for local,
uncommitted development helpers; `podman-compose.yml` expects some files
there (see [Development environment](setup.md#optional-compose-services)).

### Two Go modules

The server and the agent are separate Go modules, each with its own `go.mod`,
`go.sum` and `.golangci.yml`. The agent imports the generated protobuf
packages from the server module, and resolves that module from the working
tree with a `replace` directive in `agent/go.mod`:

```text
replace github.com/VuteTech/Bor/server => ../server
```

A change to a `.proto` file or to anything else the agent imports from
`server/` therefore affects both modules at once. Build and test both before
you open a pull request.

## Components

```mermaid
--8<-- "diagrams/architecture-overview.mmd"
```

- **Server** (`server/`): the web UI, REST API and enrollment gRPC service on
  the enrollment port (`server.enrollment_port`, default 8443), and the
  mTLS-only policy stream and certificate renewal service on the policy port
  (`server.policy_port`, default 8444). It stores state in PostgreSQL and
  signs agent certificates with its internal CA. See
  [Server internals](server.md).
- **Agent** (`agent/`): a root daemon on each managed desktop. It enrolls
  once, then keeps a gRPC stream open to receive policy snapshots and
  deltas, writes the managed files, watches them for tampering and reports
  compliance. See [Agent internals](agent.md).
- **Web UI** (`server/web/frontend/`): a single-page app served by the
  server. See [Frontend](frontend.md).
- **Protocol** (`proto/`): shared message and service definitions. See
  [Protocol and code generation](protocol.md).

## Tech stack

| Area | Technology |
|---|---|
| Server and agent | Go (`go 1.25.0` in both `go.mod` files), built with `GOFIPS140=v1.0.0` |
| Agent and server RPC | gRPC with Protocol Buffers (proto3), mTLS for agents |
| Database | PostgreSQL (the dev stack uses `postgres:17.6-alpine3.22`) |
| Web UI | React with TypeScript and PatternFly 6 (`@patternfly/react-core`, `@patternfly/react-table`, `@patternfly/react-icons`) |
| Frontend build | webpack 5 with `babel-loader` (type stripping only); `tsc --noEmit` for type checking; ESLint with `eslint-plugin-jsx-a11y` |
| TypeScript protobuf types | `ts-proto` plus the in-tree `protoc-gen-bor-ui` plugin |
| Packaging | nfpm (deb, rpm, apk, Arch Linux) for amd64, arm64 and ppc64le |
| Container image | UBI 10 base images, `server/Containerfile` |
| Documentation | MkDocs with the Material theme, Mermaid diagrams |

Check `server/web/frontend/package.json` for the exact frontend dependency
versions; at the time of writing it pins React 19.
