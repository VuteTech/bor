# Bor — Enterprise Linux Desktop Policy Management

🌐 [getbor.dev](https://getbor.dev) ·
📦 [GitHub](https://github.com/VuteTech/Bor) ·
📖 [Contributing](docs/CONTRIBUTING.md) ·
🔒 [Security](docs/SECURITY.md)

---

## Overview

Bor is an open-source policy management system for Linux desktops. A central
server distributes configuration policies to enrolled Linux endpoints in
real-time. A lightweight Go agent daemon on each machine receives policies over
an encrypted gRPC stream and enforces them locally.

**Currently enforced policies:**

- Firefox ESR — system-wide `policies.json` (RPM/DEB and Flatpak)
- Google Chrome / Chromium — managed JSON in `/etc/opt/chrome/` and `/etc/chromium/` (including Flatpak)
- KDE Plasma — KDE Kiosk (`kconfig` files under `/etc/xdg/`, KCM module restrictions)

---

## Architecture

```text
┌─────────────────────┐    gRPC stream / mTLS     ┌──────────────────────┐
│     Bor Agent       │ ◄────── port 8444 ────────► │     Bor Server       │
│  (Go daemon, root)  │                             │  Go + PatternFly UI  │
└─────────────────────┘    gRPC enrollment / TLS   └──────────┬───────────┘
  • One-time token enrollment ◄── port 8443 ──►               │ SQL
  • mTLS client certificate auth                               ▼
  • Applies Firefox / Chrome /               ┌──────────────────────────────┐
    KDE Kiosk policies                       │        PostgreSQL            │
  • Reports compliance                       │  policies · nodes · users    │
  • Desktop notifications                   │  bindings · RBAC · audit log │
                                             └──────────────────────────────┘
```

The server runs two HTTPS listeners with different security postures:

| Port | Purpose | TLS | Client cert |
|---|---|---|---|
| **8443** | Admin UI (REST) + enrollment gRPC | TLS 1.2+ | Optional |
| **8444** | Agent policy stream + cert renewal | TLS 1.3, mTLS | Mandatory |

REST and enrollment gRPC share port 8443, routed by Content-Type:
`application/grpc*` → gRPC handler, everything else → REST API + embedded
PatternFly web UI.

---

## Features

- **Centralised web UI** — PatternFly 5 dashboard for policies, nodes, and bindings
- **Real-time distribution** — gRPC server-side streaming; agents receive changes instantly
- **Secure by design** — mTLS certificate authentication for every agent; internal CA auto-generated on first run; FIPS 140-3 validated crypto module
- **Easy enrollment** — one-time token generated in the UI; agent bootstraps its own certificate
- **Automatic renewal** — agent certificates (90-day) are renewed automatically before expiry; no operator action required
- **Delta sync** — monotonic revision counter with ring buffer; reconnecting agents receive only what changed
- **Node Groups** — many-to-many assignment of nodes to policy groups
- **RBAC** — built-in roles (Super Admin, Org Admin, Policy Editor, …) with granular per-resource permissions
- **Audit log** — every API action recorded with user, IP, and timestamp
- **Tamper protection** — file watcher detects and restores managed files that are modified outside Bor
- **LDAP/AD integration** — optional, enabled via environment variables
- **HSM support** — optional PKCS#11 integration stores the CA private key in a hardware security module
- **Linux-native packaging** — deb, rpm, apk, Arch Linux packages via nfpm

---

## Quick Start

### Prerequisites

| Component | Requirement |
|---|---|
| Server host | Podman / Docker + podman-compose / docker-compose |
| Agent host | Linux x86\_64 or arm64 |
| Building from source | Go 1.24+, Node.js 18+, Make |

### Run the server with Podman Compose

```bash
git clone https://github.com/VuteTech/Bor.git
cd Bor
cp .env.example .env
$EDITOR .env   # set DB_PASSWORD, JWT_SECRET, BOR_ADMIN_PASSWORD at minimum
podman-compose up -d
```

The server starts on `https://localhost:8443` (UI + enrollment) and
`https://localhost:8444` (agent mTLS). On first boot it auto-generates an
internal CA (ECDSA P-384) and a server TLS certificate (ECDSA P-256) under
`/var/lib/bor/pki/`.

Log in at `https://localhost:8443` with the admin credentials set in `.env`.

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
     address: "your-server"
     enrollment_port: 8443
     policy_port: 8444
     insecure_skip_verify: true   # set false after deploying a trusted cert
   ```

2. Generate an enrollment token in the web UI (Node Groups page).

3. Enroll the agent:

   ```bash
   sudo bor-agent --token <ENROLLMENT_TOKEN>
   ```

   The agent generates an ECDSA P-256 key pair, sends a CSR to the server,
   and stores the signed certificate, private key, and CA cert in
   `/var/lib/bor/agent/`. After enrollment, start it as a service:

   ```bash
   sudo systemctl enable --now bor-agent
   ```

4. To re-enroll (e.g. after a CA rotation or to move a node to a different
   group), pass a new token — the old certificates are removed automatically
   before re-enrollment begins:

   ```bash
   sudo bor-agent --token <NEW_TOKEN>
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

All Go builds use `GOFIPS140=v1.0.0`, which embeds the FIPS 140-3 validated
crypto module snapshot (CAVP certificate A6650) regardless of the host
toolchain.

```bash
make server      # → server/server       (FIPS 140-3)
make agent       # → agent/bor-agent     (FIPS 140-3)
make frontend    # → embedded into the server binary (run before make server)
make proto       # regenerate Go + TypeScript from proto/ definitions
```

Build everything in the correct order:

```bash
make frontend && make server && make agent
```

### PKCS#11 HSM build

To store the CA private key in a hardware security module, build with the
`pkcs11` tag. This requires CGO and the `crypto11` dependency:

```bash
cd server && go get github.com/ThalesIgnite/crypto11
cd ..
make server-pkcs11   # → server/server  (FIPS 140-3 + PKCS#11)
```

See [docs/SECURITY.md](docs/SECURITY.md#hsm-integration-pkcs11) for the full
configuration guide and a SoftHSMv2 example.

### FIPS 140-3 runtime enforcement

The build flag embeds the validated module. To additionally restrict the
running process to FIPS-approved algorithms only (removes X25519, enforces
P-256/P-384):

```bash
GODEBUG=fips140=on ./server/server
```

Add `Environment=GODEBUG=fips140=on` to the systemd unit override for
production deployments.

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
make packages                          # deb + rpm + apk + archlinux, version 0.1.0
make packages VERSION=1.2.3            # specify version
make packages VERSION=1.2.3 ARCH=arm64 # cross-arch
make packages-agent VERSION=1.2.3      # agent packages only
make packages-server VERSION=1.2.3     # server packages only
```

Packages are written to `builds/`. Requires `nfpm` (`make install-deps`).

### Container image

```bash
make docker   # builds bor-server:latest with podman
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

The server reads configuration from a YAML file (default `/etc/bor/server.yaml`,
override with `BOR_CONFIG`) and environment variables. Environment variables
always take precedence. Copy `.env.example` to `.env` for local development,
or use `/etc/bor/server.env` for production (loaded by the systemd unit).

A full annotated YAML reference is in `server/server.yaml.example`.

#### Required

| Variable | Description |
|---|---|
| `DB_PASSWORD` | PostgreSQL password |
| `JWT_SECRET` | JWT signing secret — use at least 32 random bytes |
| `BOR_ADMIN_PASSWORD` | Initial admin password (applied only when no users exist) |

#### Server

| Variable | Default | Description |
|---|---|---|
| `BOR_ADDRESS` | *(all interfaces)* | Server hostname or IP (no port) |
| `BOR_ENROLLMENT_PORT` | `8443` | UI + enrollment gRPC listen port |
| `BOR_POLICY_PORT` | `8444` | Agent mTLS policy stream listen port |
| `BOR_HOSTNAMES` | — | Comma-separated extra SANs for the auto-generated TLS cert |

#### Database

| Variable | Default | Description |
|---|---|---|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `bor` | PostgreSQL user |
| `DB_NAME` | `bor` | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode (`disable`, `require`, `verify-full`) |

#### PKI

If the CA and TLS variables are not set, the server auto-generates an internal
CA (ECDSA P-384, 10 years) and a server TLS certificate (ECDSA P-256, 365 days)
under `/var/lib/bor/pki/` and reuses them on subsequent starts.

| Variable | Default | Description |
|---|---|---|
| `BOR_CA_CERT_FILE` | auto | Path to CA certificate PEM |
| `BOR_CA_KEY_FILE` | auto | Path to CA private key PEM (unused when PKCS#11 is set) |
| `BOR_CA_AUTOGEN_DIR` | `/var/lib/bor/pki/ca` | Directory for auto-generated CA files |
| `BOR_TLS_CERT_FILE` | auto | Path to server TLS certificate PEM |
| `BOR_TLS_KEY_FILE` | auto | Path to server TLS private key PEM |
| `BOR_TLS_AUTOGEN_DIR` | `/var/lib/bor/pki/ui` | Directory for auto-generated TLS cert |

#### PKCS#11 HSM (optional)

Requires the `server-pkcs11` binary. The PIN must come from an environment
variable — never store it in a YAML file.

| Variable | Description |
|---|---|
| `BOR_CA_PKCS11_LIB` | Path to PKCS#11 shared library (`.so`) |
| `BOR_CA_PKCS11_TOKEN_LABEL` | HSM token label |
| `BOR_CA_PKCS11_KEY_LABEL` | CA private key label on the token |
| `BOR_CA_PKCS11_PIN` | Token PIN |

#### Security

| Variable | Default | Description |
|---|---|---|
| `BOR_ADMIN_TOKEN` | — | Static token for gRPC enrollment calls (optional) |
| `LDAP_ENABLED` | `false` | Enable LDAP authentication |
| `LDAP_HOST` | `localhost` | LDAP server hostname |
| `LDAP_PORT` | `389` | LDAP server port |
| `LDAP_USE_TLS` | `false` | Use TLS for the LDAP connection |
| `LDAP_BIND_DN` | — | LDAP bind distinguished name |
| `LDAP_BIND_PASSWORD` | — | LDAP bind password |
| `LDAP_BASE_DN` | — | LDAP search base DN |

### Agent

The agent reads `/etc/bor/config.yaml` by default. Pass `--config <path>` to
override. A full annotated reference is in `agent/config.yaml.example`.

```yaml
server:
  address: "bor-server.example.com"
  enrollment_port: 8443   # UI + enrollment gRPC
  policy_port: 8444       # mTLS policy stream
  insecure_skip_verify: false   # true only for self-signed certs during setup

agent:
  client_id: ""   # defaults to system hostname

enrollment:
  data_dir: "/var/lib/bor/agent"   # cert, key, and CA cert stored here

firefox:
  policies_path: "/etc/firefox/policies/policies.json"
  flatpak_policies_path: "/var/lib/flatpak/extension/org.mozilla.firefox.systemconfig/x86_64/stable/policies/policies.json"

chrome:
  chrome_policies_path: "/etc/opt/chrome/policies/managed"
  chromium_policies_path: "/etc/chromium/policies/managed"
  chromium_browser_policies_path: "/etc/chromium-browser/policies/managed"
  flatpak_chromium_policies_path: ""   # set to enable Flatpak Chromium

kconfig:
  config_path: "/etc/xdg"   # KDE Kiosk overlay directory
```

---

## Documentation

- [Security](docs/SECURITY.md) — cryptographic algorithms, FIPS 140-3 compliance, EU standards, deployment checklist, HSM integration
- [Architecture](docs/ARCHITECTURE.md) — detailed design and data flows
- [Contributing](docs/CONTRIBUTING.md) — setup, coding standards, PR process

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
- [x] Dual-port architecture (8443 UI/enrollment, 8444 agent mTLS)
- [x] gRPC streaming with mTLS (TLS 1.3, mandatory client cert)
- [x] Token-based agent enrollment (one-time, 5-minute TTL)
- [x] Automatic certificate renewal (ECDSA P-256, 90-day lifetime)
- [x] FIPS 140-3 validated builds (`GOFIPS140=v1.0.0`, CAVP A6650)
- [x] PKCS#11 HSM support for CA private key (`-tags pkcs11`)
- [x] Firefox ESR policy enforcement (RPM/DEB + Flatpak)
- [x] Chrome / Chromium policy enforcement (including Flatpak)
- [x] KDE Plasma KConfig (Kiosk) enforcement + KCM module restrictions
- [x] Tamper protection (file watcher restores managed files)
- [x] RBAC with roles and permissions
- [x] Audit log
- [x] Desktop notifications on policy change
- [x] Many-to-many node group membership
- [x] LDAP / Active Directory authentication
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
