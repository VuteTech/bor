# Bor — Enterprise Linux Desktop Policy Management

🌐 [getbor.dev](https://getbor.dev) ·
📦 [GitHub](https://github.com/VuteTech/Bor) ·
📖 [Contributing](docs/CONTRIBUTING.md)

---

## Overview

Bor is an open-source policy management system for Linux desktops. A central
server distributes configuration policies to enrolled Linux endpoints in
real-time. A lightweight Go agent daemon on each machine receives policies over
an encrypted gRPC stream and enforces them locally.

**Currently enforced policies:**

- Firefox ESR — system-wide `policies.json`
- Google Chrome / Chromium — managed JSON in `/etc/opt/chrome/` and `/etc/chromium/`
- KDE Plasma — KDE Kiosk (`kconfig` files under `/etc/xdg/`)

---

## Architecture

```text
┌─────────────────────┐       gRPC / mTLS          ┌──────────────────────┐
│     Bor Agent       │ ◄─────────────────────────► │     Bor Server       │
│  (Go daemon, root)  │   streaming policy updates  │  Go + PatternFly UI  │
└─────────────────────┘                             └──────────┬───────────┘
  • One-time token enrollment                                  │ SQL
  • mTLS client certificate auth                               ▼
  • Applies Firefox / Chrome /               ┌──────────────────────────────┐
    KDE Kiosk policies                       │        PostgreSQL            │
  • Reports compliance                       │  policies · nodes · users    │
  • Desktop notifications                   │  bindings · RBAC · audit log │
                                             └──────────────────────────────┘
```

The server exposes a single HTTPS port (`:8443`). Traffic is routed by
content-type: `application/grpc*` → gRPC handler (mTLS required),
everything else → REST API + embedded PatternFly web UI (JWT auth).

---

## Features

- **Centralised web UI** — PatternFly 5 dashboard for policies, nodes, and
  bindings
- **Real-time distribution** — gRPC server-side streaming; agents receive
  changes instantly
- **Secure by design** — mTLS certificate authentication for every agent;
  internal CA auto-generated on first run
- **Easy enrollment** — one-time token generated in the UI; agent bootstraps
  its own certificate
- **Delta sync** — monotonic revision counter with ring buffer; reconnecting
  agents receive only what changed
- **Node Groups** — many-to-many assignment of nodes to policy groups
- **RBAC** — built-in roles (Super Admin, Org Admin, Policy Editor, …) with
  granular per-resource permissions
- **Audit log** — every API action recorded with user, IP, and timestamp
- **LDAP/AD integration** — optional, enabled via environment variables
- **Linux-native packaging** — deb, rpm, apk, Arch Linux packages via nfpm

---

## Quick Start

### Prerequisites

| Component | Requirement |
| --- | --- |
| Server host | Podman / Docker + podman-compose / docker-compose |
| Agent host | Linux x86\_64 or arm64 |
| Building from source | Go 1.21+, Node.js 18+, Make |

### Run the server with Podman Compose

```bash
git clone https://github.com/VuteTech/Bor.git
cd Bor
cp .env.example .env
$EDITOR .env          # set DB_PASSWORD and JWT_SECRET at minimum
podman-compose up -d
```

The server starts on `https://localhost:8443`. On first boot it auto-generates
an internal CA and a server TLS certificate under `/var/lib/bor/pki/`.

Log in with the admin credentials you set in `.env`.

### Install the agent (from package)

Download the appropriate package for your distribution from the
[releases page](https://github.com/VuteTech/Bor/releases) and install it:

```bash
# Debian / Ubuntu
sudo dpkg -i bor-agent_<version>_amd64.deb

# Fedora / RHEL
sudo rpm -i bor-agent-<version>.x86_64.rpm

# Arch Linux
sudo pacman -U bor-agent-<version>-x86_64.pkg.tar.zst
```

### Configure and enroll the agent

1. Edit `/etc/bor/config.yaml` (installed by the package):

   ```yaml
   server:
     address: "your-server:8443"
     insecure_skip_verify: true   # set false after trusting the CA cert
   ```

2. Generate an enrollment token in the web UI (Node Groups page).

3. Enroll:

   ```bash
   sudo bor-agent --token <ENROLLMENT_TOKEN>
   ```

   The agent stores its certificate, key, and the server CA cert in
   `/var/lib/bor/agent/`. After enrollment, start it as a service:

   ```bash
   sudo systemctl enable --now bor-agent
   ```

---

## Building from Source

All build targets are in the `Makefile`. Run `make help` to list them.

### Install build dependencies

```bash
make install-deps
```

This installs `protoc-gen-go`, `protoc-gen-go-grpc`, `golangci-lint`, `nfpm`,
and the TypeScript proto plugin via `npm`. You must have `protoc` and `Node.js`
available separately.

### Build

```bash
make server      # → server/server
make agent       # → agent/bor-agent
make frontend    # → embedded into server binary (run before make server)
make proto       # regenerate Go + TypeScript from proto/ definitions
```

Build everything in the correct order:

```bash
make frontend && make server && make agent
```

### Run locally

```bash
# Start PostgreSQL
make dev

# Run the server (reads .env automatically via go run)
make run-server

# In a separate terminal — run the agent against the local server
sudo ./agent/bor-agent --config agent/config.yaml.example
```

### Packaging

Build distribution packages for all formats:

```bash
make packages                        # deb + rpm + apk + archlinux, version 0.1.0
make packages VERSION=1.2.3          # specify version
make packages VERSION=1.2.3 ARCH=arm64   # cross-arch
make packages-agent VERSION=1.2.3    # agent packages only
make packages-server VERSION=1.2.3   # server packages only
```

Packages are written to `builds/`. Requires `nfpm` (`make install-deps`).

### Container image

```bash
make docker      # builds bor-server:latest with podman
```

---

## Testing

```bash
make test            # all tests (server + agent)
make test-server     # server tests only
make test-agent      # agent tests only
make coverage        # HTML coverage reports → server/coverage.html, agent/coverage.html
```

Run a single test by name:

```bash
cd server && go test -v -run TestPolicyService ./internal/services/...
cd agent  && go test -v -run TestMergeKConfig  ./internal/policy/...
```

---

## Configuration

### Server

The server is configured entirely via environment variables. Copy
`.env.example` to `.env` for local development, or use
`/etc/bor/server.env` (installed by the package) for production.

| Variable | Default | Description |
| --- | --- | --- |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `bor` | PostgreSQL user |
| `DB_PASSWORD` | — | PostgreSQL password (**required**) |
| `DB_NAME` | `bor` | PostgreSQL database |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `BOR_ADDR` | `:8443` | HTTPS listen address |
| `JWT_SECRET` | — | JWT signing secret (**required**) |
| `BOR_CA_CERT_FILE` | auto | Path to CA certificate |
| `BOR_CA_KEY_FILE` | auto | Path to CA private key |
| `BOR_TLS_CERT_FILE` | auto | Path to server TLS certificate |
| `BOR_TLS_KEY_FILE` | auto | Path to server TLS private key |
| `LDAP_ENABLED` | `false` | Enable LDAP authentication |

If CA and TLS cert/key variables are not set, the server auto-generates a
self-signed internal CA and server certificate under `/var/lib/bor/pki/` on
first start and reuses them on subsequent starts.

### Agent

The agent reads `/etc/bor/config.yaml` by default. Pass `--config <path>` to
override. See `agent/config.yaml.example` for all options with comments.

Key settings:

```yaml
server:
  address: "bor-server.example.com:8443"
  insecure_skip_verify: false   # false after enrollment stores the CA cert

enrollment:
  data_dir: "/var/lib/bor/agent"   # cert, key, and CA stored here

firefox:
  policies_path: "/etc/firefox/policies/policies.json"

chrome:
  chrome_policies_path: "/etc/opt/chrome/policies/managed"
  chromium_policies_path: "/etc/chromium/policies/managed"

kconfig:
  config_path: "/etc/xdg"
```

---

## Project Structure

```text
Bor/
├── agent/                   Go daemon (runs as root on each managed desktop)
│   ├── cmd/agent/           Entry point
│   └── internal/
│       ├── config/          YAML config loader
│       ├── notify/          D-Bus desktop notifications
│       ├── policy/          Policy enforcement (Firefox, Chrome, KConfig)
│       ├── policyclient/    gRPC client + mTLS enrollment
│       └── sysinfo/         System fact collection (OS, FQDN, …)
├── server/                  Go backend
│   ├── cmd/server/          Entry point
│   └── internal/
│       ├── api/             REST handlers
│       ├── authz/           RBAC
│       ├── config/          Environment-variable loader
│       ├── database/        PostgreSQL repos + migrations
│       ├── grpc/            gRPC service + PolicyHub pub/sub
│       ├── models/          Data models and DTOs
│       ├── pki/             Internal CA + TLS certificate management
│       └── services/        Business logic
│   └── web/frontend/        PatternFly 5 + React 18 (TypeScript, Webpack)
├── proto/                   Protocol Buffer definitions
├── packaging/               nfpm configs, systemd units, install scripts
├── docs/                    Documentation
│   ├── CONTRIBUTING.md
│   └── ARCHITECTURE.md
├── Makefile                 All build, test, lint, and package targets
└── podman-compose.yml       Development + deployment environment
```

---

## Documentation

- [Contributing](docs/CONTRIBUTING.md) — setup, coding standards, PR process
- [Architecture](docs/ARCHITECTURE.md) — detailed design and data flows
- [Agent README](agent/README.md) — agent-specific notes
- [Server README](server/README.md) — server-specific notes

---

## Contributing

Contributions are welcome via
[GitHub pull requests](https://github.com/VuteTech/Bor/pulls) or by emailing
patches to <bor@vute.tech>.

See [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) for the full guide.

---

## Roadmap

### Implemented

- [x] Core policy management API and web UI
- [x] gRPC streaming with mTLS
- [x] Token-based agent enrollment
- [x] Firefox ESR policy enforcement
- [x] Chrome / Chromium policy enforcement
- [x] KDE Plasma KConfig (Kiosk) enforcement
- [x] RBAC with roles and permissions
- [x] Audit log
- [x] Desktop notifications on policy change
- [x] Many-to-many node group membership
- [x] deb / rpm / apk / Arch Linux packaging

### Planned

- [ ] Persistent compliance reporting (database storage)
- [ ] Prometheus metrics endpoint
- [ ] AD / FreeIPA LDAP enrollment (Kerberos)
- [ ] Additional policy types: dconf, systemd units, firewalld, polkit, packages
- [ ] Agent auto-update mechanism
- [ ] Multi-tenancy

---

## License

Copyright (C) 2026 Vute Tech LTD
Copyright (C) 2026 Bor contributors

Licensed under the [GNU Lesser General Public License v3.0](LICENSE.md).
