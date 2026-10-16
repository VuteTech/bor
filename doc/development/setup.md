# Development environment

This page walks through setting up a local development environment: the
tools you need, a PostgreSQL database in a container, running the server
from source as a normal user, working on the web UI, and pointing an agent
at your development server.

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Go | the version in `server/go.mod` (`go 1.26.0`) or newer | Server and agent |
| Node.js and npm | 22 (the version CI uses) | Web UI build, TypeScript protobuf types |
| `protoc` | Protocol Buffers compiler | `make proto` |
| podman and podman-compose | any recent | Development PostgreSQL (`make dev`) |
| `make`, `git` | any | Build orchestration |
| nfpm | optional | Building packages (`make packages`) |
| golangci-lint | latest release | `make lint` (see [Testing and linting](testing.md#golangci-lint)) |

CI selects the Go toolchain with `go-version-file: server/go.mod`, so the
`go` line in `server/go.mod` is the authoritative minimum.

## Clone and install the tool chain

```bash
git clone https://github.com/VuteTech/Bor.git
cd Bor
make install-deps
```

`make install-deps` runs `go install` for `protoc-gen-go`,
`protoc-gen-go-grpc`, `golangci-lint` and `nfpm` (all `@latest`), installs the
in-tree `protoc-gen-bor-ui` plugin from `server/cmd/protoc-gen-bor-ui`, and
runs `npm install` in `server/web/frontend` (which provides
`protoc-gen-ts_proto`). It does not install `protoc` itself; install it from
your distribution (for example the `protobuf-compiler` or `protobuf` package).

Make sure `$(go env GOPATH)/bin` is on your `PATH`, because `make proto`
calls `protoc-gen-bor-ui` through `which`.

## Configure the environment

The server is configured from an optional YAML file plus environment
variables, and environment variables always win
(`server/internal/config/config.go`). The YAML path defaults to
`/etc/bor/server.yaml` (override with `BOR_CONFIG`); a missing file is
silently ignored, so a development setup can rely on environment variables
alone.

Copy the template:

```bash
cp .env.example .env
```

The variables you will most likely touch:

| Variable | Default in `.env.example` | Purpose |
|---|---|---|
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` | `localhost`, `5432`, `bor`, `bor`, `bor` | PostgreSQL connection; matches the compose service |
| `DB_SSLMODE` | `disable` | Use `require` for a remote database |
| `BOR_ENROLLMENT_PORT` | `8443` | Web UI, REST API and enrollment gRPC |
| `BOR_POLICY_PORT` | `8444` | mTLS policy stream for agents |
| `BOR_HOSTNAMES` | unset | Extra SANs for the auto-generated TLS certificate (it always contains `localhost`, `127.0.0.1` and `::1`) |
| `JWT_SECRET` | `change-me-in-production` | The placeholder makes the server generate a random secret at startup, so sessions do not survive a restart. Set 32 or more random bytes to keep sessions across restarts |
| `BOR_ADMIN_PASSWORD` | unset | Initial password for the `admin` user, used only when the database has no users |
| `BOR_TLS_AUTOGEN_DIR`, `BOR_CA_AUTOGEN_DIR` | `/var/lib/bor/pki/ui`, `/var/lib/bor/pki/ca` | Where the auto-generated server certificate and internal CA are stored |
| `LDAP_ENABLED` | `false` | LDAP integration, see [LDAP and Active Directory](../configuration/ldap.md) |
| `BOR_DEV_MODE` | unset | Relaxes some checks (for example allows `LDAP_TLS_SKIP_VERIFY=true`). Never use in production |

The full list, with comments, is in `.env.example`. The YAML equivalents are
documented in [Server configuration](../configuration/server.md).

## Start PostgreSQL

```bash
make dev
```

This runs `podman-compose up -d postgres`, which starts the `postgres`
service from `podman-compose.yml` (image `postgres:17.6-alpine3.22`, user,
password and database all `bor`, published on `localhost:5432`, data in the
named volume `postgres_data`).

!!! tip "Port 5432 already in use"
    If another PostgreSQL already listens on 5432, run a throwaway container
    on a different port and point the server at it:

    ```bash
    podman run -d --name bor-dev-pg -p 15432:5432 \
      -e POSTGRES_USER=bor -e POSTGRES_PASSWORD=bor -e POSTGRES_DB=bor \
      docker.io/library/postgres:17.6-alpine3.22
    ```

    Then set `DB_PORT=15432` in `.env`.

You do not need to run migrations by hand. The SQL files in
`server/internal/database/migrations/` are embedded into the binary and
applied in order when the server starts.

## Run the server

`make run-server` runs `go run ./cmd/server` in `server/` with
`GOFIPS140=v1.0.0`. It does **not** load `.env`, and neither does the
server binary. Export the file into your shell first:

```bash
set -a; source .env; set +a
make run-server
```

Values that contain shell metacharacters (for example LDAP filters with `&`
or parentheses) must be quoted in `.env` for `source` to read them correctly.

### Running as a normal user

The default PKI directories under `/var/lib/bor` are owned by root on most
systems, and when no `BOR_ADMIN_PASSWORD` is set the server writes the
generated admin password to `/var/lib/bor/initial_admin_password`. To run
without root, point the PKI directories at a writable location and set the
admin password explicitly:

```bash
set -a; source .env; set +a
export BOR_TLS_AUTOGEN_DIR="$HOME/.local/share/bor-dev/pki/ui"
export BOR_CA_AUTOGEN_DIR="$HOME/.local/share/bor-dev/pki/ca"
export BOR_ADMIN_PASSWORD='a-local-dev-password'
make run-server
```

On first start the server creates the internal CA and a server certificate
in those directories, applies the migrations and creates the `admin` user.
Open `https://localhost:8443` and accept the self-signed certificate.

`BOR_ADMIN_PASSWORD` is only used while the user table is empty. To start
over, drop the database volume (`podman-compose down -v`) or the throwaway
container.

!!! note "Metrics listener"
    The server also starts a metrics listener on `127.0.0.1:9090` by default.
    If that port is taken, the listener fails to bind but the rest of the
    server keeps running; set `BOR_METRICS_ADDR` to another address to avoid
    the message.

## Work on the web UI

The web UI source is in `server/web/frontend/`. webpack writes the bundle to
`server/web/static/`, and the server embeds that directory at compile time
(`server/web/web.go`):

```go
//go:embed static/*
```

The server therefore serves whatever was in `server/web/static/` when it was
compiled. A rebuilt bundle is visible only after the server is rebuilt or
restarted with `make run-server` (which recompiles).

```bash
cd server/web/frontend
npm install
npm run dev        # webpack --mode development --watch
```

Leave `npm run dev` running, and restart `make run-server` after a change you
want to see in the browser. `npm run build` produces the production bundle;
`make frontend` runs `npm install && npm run build`.

webpack uses `babel-loader`, which strips TypeScript types without checking
them. Run `npm run typecheck` before you push; see
[Testing and linting](testing.md#frontend-checks).

Read the frontend conventions (shared components, design tokens and
accessibility rules) in [Frontend](frontend.md) before building new UI.

## Run an agent against the dev server

The agent manages system files (for example `/etc/firefox/policies/`,
`/etc/dconf/db/`, `/etc/polkit-1/rules.d/`) and runs as root. Run it in a
disposable virtual machine rather than on your workstation, so that test
policies do not change your own desktop.

1. Build the agent:

    ```bash
    make agent         # produces agent/bor-agent
    ```

2. Make the server reachable from the VM. Add the address the VM uses to the
   certificate SANs, for example `BOR_HOSTNAMES=192.0.2.10`, and restart the
   server. Open the enrollment port and the policy port (8443 and 8444 by
   default) in the host firewall.

3. Write an agent config based on `agent/config.yaml.example`:

    ```yaml
    server:
      address: "192.0.2.10"
      enrollment_port: 8443
      policy_port: 8444
      # The dev server uses a self-signed certificate. Verification is
      # skipped for enrollment only; the CA received during enrollment is
      # used for every later connection.
      insecure_skip_verify: true
    enrollment:
      data_dir: "/var/lib/bor/agent"
    ```

4. In the web UI, open **Node Groups**, choose **Generate token** in the
   group's actions menu and copy the token. Tokens are single use and expire
   after five minutes.

5. Enroll. The token is read from a file (or `BOR_ENROLLMENT_TOKEN`) so it
   does not show up in the process list:

    ```bash
    sudo ./bor-agent --config ./dev-agent.yaml --token-file ./token.txt
    ```

    After a successful enrollment the agent prints where it stored the
    certificates and exits.

6. Start it again without a token to connect to the policy stream:

    ```bash
    sudo ./bor-agent --config ./dev-agent.yaml
    ```

The agent logs to stderr. Bind a released policy to the node group and watch
the agent apply it. More detail on agent options is in
[Agent configuration](../configuration/agent.md).

## Optional compose services

`podman-compose.yml` defines two more services next to `postgres`:

- `syslog-ng`: a syslog receiver for testing audit forwarding
  (`BOR_AUDIT_SYSLOG_*`). It listens on `127.0.0.1:5514` (UDP and TCP) and
  mounts its configuration from `dev/syslog-ng/syslog-ng.conf` and its log
  directory from `dev/syslog-ng/logs`. The `dev/` directory is git-ignored,
  so provide your own syslog-ng configuration there before starting the
  service. Point the server at it with:

    ```bash
    BOR_AUDIT_SYSLOG_ENABLED=true
    BOR_AUDIT_SYSLOG_NETWORK=udp
    BOR_AUDIT_SYSLOG_ADDR=127.0.0.1:5514
    BOR_AUDIT_SYSLOG_FORMAT=cef      # or ocsf
    ```

- `server`: builds `server/Containerfile` and runs the server in a container
  with `.env` as an optional env file. Its volume list mounts Kerberos test
  files from `dev/kerberos/` (a keytab, `krb5.conf` and a CA certificate);
  remove those mounts, or provide the files, if you do not test Kerberos
  enrollment. Use `podman-compose.yml.sample` as a cleaner starting point
  for a compose-based setup.

For day-to-day work, running only `postgres` from compose and the server
with `make run-server` gives the fastest edit-compile loop.
