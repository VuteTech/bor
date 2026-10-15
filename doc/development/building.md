# Building and packaging

This page describes how Bor is built: the Makefile targets, the FIPS 140-3
build settings, the distribution packages produced with nfpm, the agent
package repository that ships inside the server package, the container
image, and the CI pipeline that ties them together.

All commands run from the repository root.

## Makefile targets

### Binaries and code generation

| Target | What it does | Output |
|---|---|---|
| `make server` | Builds the server with `GOFIPS140=v1.0.0`, `GOARCH=$(ARCH)` and `-ldflags "-X main.Version=$(VERSION)"` | `server/server` |
| `make server-pkcs11` | Same as `server`, plus `CGO_ENABLED=1` and `-tags pkcs11` for a PKCS#11 HSM-backed CA key. Needs a C compiler | `server/server` |
| `make agent` | Builds the agent with the same FIPS and version flags | `agent/bor-agent` |
| `make frontend` | `npm install && npm run build` in `server/web/frontend` | `server/web/static/` |
| `make proto` | Runs `proto-go`, `proto-ts` and `proto-ui` | see below |
| `make proto-go` | Go code for `proto/bor/ui.proto`, `proto/policy/*.proto`, `proto/audit/audit.proto` and `proto/export/export.proto` | `server/pkg/grpc/{borui,policy,audit,export}/` |
| `make proto-ts` | TypeScript types for `proto/policy/*.proto` via `ts-proto` (`onlyTypes=true`, `snakeToCamel=false`) | `server/web/frontend/src/generated/proto/` |
| `make proto-ui` | Policy editor metadata via the in-tree `protoc-gen-bor-ui` plugin | `server/web/frontend/src/generated/proto/*_ui.ts` |
| `make update-dconf-schemas` | Regenerates the built-in dconf schema catalog from local GSettings schemas (`SCHEMAS_DIR` overrides the source) | `server/assets/dconf_builtin_schemas.json` |
| `make update-polkit-actions` | Regenerates the built-in polkit action catalog from the local system | `server/assets/polkit_builtin_actions.json` |

The generated Go and TypeScript files and the built web UI bundle in
`server/web/static/` are committed. Regenerate and commit them together with
the source change that requires it. The server embeds `server/web/static/` at
compile time, so run `make frontend` before `make server` when the UI has
changed.

### Packages and release artifacts

| Target | What it does |
|---|---|
| `make packages` | `packages-agent` followed by `packages-server` |
| `make packages-agent` | Builds the agent, then deb, rpm, apk and Arch Linux packages from `packaging/nfpm-agent.yaml` into `builds/` |
| `make packages-server` | Builds the server and the frontend, then the four package formats from `packaging/nfpm-server.yaml` into `builds/` |
| `make agent-repo` | Assembles the static agent package repository from the agent packages in `builds/` into `builds/agent-repo/` (see [Agent package repository](#agent-package-repository)) |
| `make docker` | `podman build -t bor-server:latest -f server/Containerfile .` |

### Housekeeping

| Target | What it does |
|---|---|
| `make clean` | Removes `server/server`, `agent/bor-agent`, the generated `*.pb.go` files in `server/pkg/grpc/policy` and `server/pkg/grpc/borui`, `server/web/frontend/src/generated/proto/` and `builds/` |
| `make fmt` | `go fmt ./...` in both modules |
| `make coverage` | Coverage profiles and HTML reports, see [Testing and linting](testing.md#coverage) |
| `make test`, `make lint`, `make check-frontend-tokens` | See [Testing and linting](testing.md) |

!!! warning
    `make clean` deletes committed generated code. Run `make proto` afterwards
    (or restore the files with `git checkout`) before building again.

## Versions and architectures

Two variables control every build:

`VERSION`
:   Embedded with `-X main.Version=$(VERSION)` into both binaries
    (`var Version = "dev"` in `server/cmd/server/main.go` and
    `agent/cmd/agent/main.go`) and used as the package version. The default
    is `0.0.0.git<short-sha>`, because dpkg requires the version to start
    with a digit.

`ARCH`
:   Passed as `GOARCH` and as the nfpm architecture. Default `amd64`. CI
    builds `amd64`, `arm64` and `ppc64le`.

`APK_VERSION` is derived from `VERSION` with the `.git...` suffix removed,
because Alpine requires purely numeric version components.

```bash
make packages VERSION=1.2.3 ARCH=arm64
```

Cross-compiling works for the regular targets because the Go code builds
without cgo. `make server-pkcs11` enables cgo, so building it for another
architecture also needs a C cross-compiler for that target.

## FIPS 140-3 builds

Every Makefile Go build sets `GOFIPS140=v1.0.0`. This selects the frozen,
validated snapshot of the Go Cryptographic Module (CAVP certificate A6650)
instead of the toolchain's current crypto code, so the binary carries the
validated implementation regardless of the host Go version. `make run-server`
uses the same setting.

The setting does not restrict algorithms by itself. Approved-only mode is
enabled at runtime:

```bash
GODEBUG=fips140=on ./server/server
```

See [Security and compliance](../security/compliance.md#fips-140-3) for what
approved mode changes and how to enable it on installed systems.

## Distribution packages (nfpm)

The packages are described by two nfpm configurations:

| File | Package | Main contents |
|---|---|---|
| `packaging/nfpm-server.yaml` | `bor-server` | `/usr/bin/bor-server`, `/etc/bor/server.yaml` (from `server/server.yaml.example`, `config|noreplace`, mode 0640), `bor-server.service`, `/var/lib/bor/pki`, `/var/lib/bor/acme`, and the agent repository tree at `/usr/share/bor/agent-repo`. Depends on `postgresql` (`postgresql-server` for rpm) |
| `packaging/nfpm-agent.yaml` | `bor-agent` | `/usr/bin/bor-agent`, `/etc/bor/config.yaml` (from `agent/config.yaml.example`, `config|noreplace`, mode 0600), `bor-agent.service`, `/var/lib/bor/agent`. Recommends `cryptsetup`; suggests `clevis-luks`, `clevis-dracut`, `tpm2-tools` |

Install and removal hooks live in `packaging/scripts/`
(`postinstall-server.sh`, `preremove-server.sh`, `postinstall-agent.sh`,
`preremove-agent.sh`) and the systemd units in `packaging/systemd/`. What the
server post-install script does on a fresh install is described in
[Installing the server](../installation/server.md).

nfpm reads `VERSION` and `ARCH` from the environment; the Makefile passes
them. CI pins nfpm to `v2.46.3` because newer releases need a newer Go
toolchain than the project uses; `make install-deps` installs `@latest`.

## Agent package repository

Every Bor server serves the agent packages at `/agent/` on the web port,
including signed apt and dnf repositories. The tree is generated once at
build time by `scripts/assemble-agent-repo.sh`; the server only serves static
files and never holds a signing key.

```bash
make packages-agent ARCH=amd64     # repeat per architecture
make agent-repo SIGN_KEY_ID=<gpg key id>
make packages-server               # now embeds builds/agent-repo
```

The script collects the `bor-agent` packages for `VERSION` (and
`APK_VERSION`) from `PACKAGES_DIR` (default `builds`) and writes `OUT_DIR`
(default `builds/agent-repo`):

| Path | Contents |
|---|---|
| `manifest.json` | Index of all files with checksums and a `signed` flag, read by the web UI and checked at server startup |
| `deb/` | Flat apt repository: packages, `Packages(.gz)`, `Release`, plus `InRelease` and `Release.gpg` when signed |
| `rpm/` | dnf/zypper repository with `repodata/`, plus `repodata/repomd.xml.asc` when signed |
| `apk/`, `arch/` | Direct downloads (no repository index) |
| `repo-key.asc` | Public signing key, only when signed |

Environment variables:

`SIGN_KEY_ID`
:   GPG key id or fingerprint in the current keyring. Empty means the tree is
    assembled unsigned and `manifest.json` records `signed: false`. Unsigned
    trees are for development only.

`SIGN_KEY_PASSPHRASE`
:   Passphrase for a protected key. The script feeds it to gpg through
    loopback pinentry and a temporary 0600 file, never on the command line.

The script needs `apt-ftparchive` (apt-utils), `createrepo_c`, `jq`, `gzip`
and `sha256sum`, plus `gpg` when signing. This is Debian-family tooling; on
other distributions run the script inside a Debian container.

Order matters: run `make agent-repo` before `make packages-server`.
Otherwise the server package carries only the tracked placeholder
`builds/agent-repo/.gitkeep`, and the server keeps the agent download feature
switched off.

In CI the release key comes from the repository secrets
`BOR_RELEASE_GPG_KEY` (the armored private key, imported into the runner's
keyring) and `BOR_RELEASE_GPG_PASSPHRASE` (exported as
`SIGN_KEY_PASSPHRASE`). When the key secret is not available, as on forks and
pull requests from forks, the job emits a warning and assembles an unsigned
tree.

## Container image

`server/Containerfile` is a multi-stage build on Red Hat UBI 10 images:

1. `proto-go-builder` (UBI go-toolset): installs `protoc` and generates the
   Go bindings for `proto/policy/*.proto`.
2. `proto-ts-builder` (UBI Node.js 24 minimal): generates the TypeScript
   types with `ts-proto`.
3. `frontend-builder`: `npm ci` and `npm run build`.
4. `builder`: builds the server with `CGO_ENABLED=0`, `GOARCH=${TARGETARCH}`
   and `-ldflags="-s -w -X main.Version=${VERSION}"`.
5. Runtime stage (UBI 10): copies the binary to `/usr/local/bin/server` and
   `builds/agent-repo/` to `/usr/share/bor/agent-repo/`, and runs as UID
   1001.

The build stages run on the build platform and cross-compile, so
multi-platform builds do not need emulation for the Go and Node steps. Unlike
the Makefile targets, the Containerfile build does not set `GOFIPS140`.

Build locally with `make docker`, or pass a version:

```bash
podman build -t bor-server:dev --build-arg VERSION=dev -f server/Containerfile .
```

Populate `builds/agent-repo/` first if the image should serve agent packages.

## CI pipeline

CI is a single workflow, `.github/workflows/build-packages.yml` (name `CI`).
It runs on every push to any branch, on pull requests, and when a GitHub
release is published.

```mermaid
--8<-- "diagrams/dev-release-pipeline.mmd"
```

| Job | Needs | What it does |
|---|---|---|
| `test` | | `go test ./...` in `server/` and `agent/` |
| `lint` | | `make check-frontend-tokens`, then golangci-lint (`version: latest`) in `server/` and `agent/` |
| `build-server-backend` | `test`, `lint` | `go build ./...` in `server/` |
| `build-agent` | `test`, `lint` | `go build ./...` in `agent/` |
| `build-server-frontend` | `test`, `lint` | Node 22, `npm ci`, `npm run typecheck`, `npm run build` |
| `build-agent-packages` | the three build jobs | Matrix over `amd64`, `arm64`, `ppc64le`: `make packages-agent` |
| `assemble-agent-repo` | `build-agent-packages` | Downloads all agent packages, imports the release key if present, `make agent-repo` |
| `build-server-packages` | `assemble-agent-repo` | Matrix over the three architectures: `make packages-server` with the assembled repository |
| `build-image` | `assemble-agent-repo` | Multi-platform image (`linux/amd64`, `linux/arm64`, `linux/ppc64le`) from `server/Containerfile` |

The Go version comes from `server/go.mod` (`go-version-file`).

### Versions in CI

- On a published release, `VERSION` is the tag name without a leading `v`,
  and `APK_VERSION` is the same.
- Otherwise `VERSION` is `0.0.0.git<short-sha>` and `APK_VERSION` is
  `0.0.0`. The container image is tagged with the short SHA.

### Artifacts and publishing

| Event | Packages | Agent repository | Container image |
|---|---|---|---|
| Pull request, feature branch push | Workflow artifacts (`agent-packages-*`, `server-packages-*`, kept 30 days) | Workflow artifact `agent-repo-<version>` (30 days), unsigned unless the key secret is available | OCI archive artifact `container-<version>` (7 days); load with `podman load -i bor-server.tar` |
| Push to `master` | Workflow artifacts | Workflow artifact, signed when the key secret is configured | Pushed to `ghcr.io/vutetech/bor` with tag `latest` |
| Release published | Workflow artifacts and uploaded to the GitHub release | Workflow artifact, signed when the key secret is configured | Pushed to GHCR with `latest`, `<major>.<minor>.<patch>` and `<major>.<minor>` tags |

Dependency updates for both Go modules, the frontend and the GitHub Actions
are proposed weekly by Dependabot (`.github/dependabot.yml`).
