# Contributing to Bor

Thank you for your interest in contributing to Bor — an open-source Enterprise
Linux Desktop Policy Management System.

Contributions are welcome via **GitHub pull requests** at
<https://github.com/VuteTech/Bor> or by **emailing patches** to
<blago.petrov@vute.tech>.

---

## Table of Contents

- [Project Structure](#project-structure)
- [Development Setup](#development-setup)
- [Coding Standards](#coding-standards)
- [Testing](#testing)
- [Commit Messages](#commit-messages)
- [Pull Request Process](#pull-request-process)
- [Email Patches](#email-patches)
- [Issue Reporting](#issue-reporting)
- [Security Vulnerabilities](#security-vulnerabilities)
- [AI-Assisted Contributions](#ai-assisted-contributions)
- [License](#license)

---

## Project Structure

```text
Bor/
├── agent/                  Go daemon — enrolls with the server and enforces policies
│   ├── cmd/agent/          Entry point (main.go)
│   └── internal/
│       ├── config/         YAML config loader (/etc/bor/config.yaml)
│       ├── notify/         D-Bus desktop notifications (KDE/systemd sessions)
│       ├── policy/         Policy enforcement: Firefox, Chrome, KDE Kiosk (KConfig)
│       ├── policyclient/   gRPC client and mTLS enrollment
│       └── sysinfo/        System metadata collection
├── server/                 Go backend — REST API + gRPC on a single HTTPS port
│   ├── cmd/server/         Entry point (main.go)
│   └── internal/
│       ├── api/            REST handlers (policies, nodes, groups, …)
│       ├── authz/          RBAC authorization
│       ├── config/         Environment-variable configuration
│       ├── database/       PostgreSQL repositories and SQL migrations
│       ├── grpc/           gRPC service + PolicyHub (in-memory pub/sub)
│       ├── models/         Data models and DTOs
│       ├── pki/            Internal CA and TLS certificate management
│       └── services/       Business logic
│   └── web/frontend/       PatternFly 5 React app (TypeScript + Webpack)
├── proto/                  Protocol Buffer definitions (policy + enrollment)
├── packaging/              nfpm packaging configs and systemd units
└── docs/                   Project documentation
```

The server embeds the compiled frontend at build time via `//go:embed`. Run
`make frontend` before `make server` if you need the web UI in the binary.

---

## Development Setup

### Prerequisites

| Tool | Minimum version | Purpose |
| --- | --- | --- |
| Go | 1.21 | Server and agent |
| Node.js | 18 | Frontend build |
| PostgreSQL | 14 | Server database |
| `protoc` | 3.x | Protobuf code generation |
| `make` | any | Build orchestration |
| `nfpm` | 2.x | Package building (optional) |
| `golangci-lint` | 1.55 | Linting (optional, CI enforced) |

Install Go tooling and the TypeScript proto plugin in one step:

```bash
make install-deps
```

### Getting Started

```bash
# 1. Clone
git clone https://github.com/VuteTech/Bor.git
cd Bor

# 2. Start the development database (PostgreSQL via podman-compose)
make dev

# 3. Build all components
make server        # → server/server
make agent         # → agent/bor-agent
make frontend      # → server/web/static/ (embedded in server binary)

# 4. Copy and edit the server environment file
cp .env.example .env
$EDITOR .env

# 5. Run the server
make run-server

# 6. Copy and edit the agent config
cp agent/config.yaml.example /etc/bor/config.yaml
$EDITOR /etc/bor/config.yaml
```

Migrations run automatically on server start. No manual migration step is
needed for local development.

---

## Coding Standards

### Go

All Go code must pass `gofmt` and `go vet` without warnings before submission.
The CI pipeline runs `golangci-lint` with the configuration in
`server/.golangci.yml`.

To run locally:

```bash
make fmt          # gofmt on server + agent
make lint         # golangci-lint on server + agent
make lint-server  # lint server only
make lint-agent   # lint agent only
```

**Style rules:**

- Follow [Effective Go](https://go.dev/doc/effective_go) and the
  [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).
- Wrap errors with context using `fmt.Errorf("failed to do X: %w", err)`.
  Never swallow errors silently.
- Keep functions small and focused. Prefer composition over long functions.
- Avoid global state. Pass dependencies explicitly.
- Use `context.Context` as the first argument for any function that performs
  I/O or can be cancelled.
- Name receivers consistently within a type. Use short, lowercase names (e.g.
  `r` for a repository, `s` for a service).
- Prefer standard library packages over adding new third-party dependencies.
  Discuss significant new dependencies in a GitHub issue first.
- All exported types, functions, and methods must have a godoc comment.

**New policy type checklist** (when adding a browser or system policy type):

1. Add enforcement in `agent/internal/policy/`
2. Wire it into `agent/cmd/agent/main.go` (`handlePolicyUpdate` and `applyPolicies`)
3. Add agent config in `agent/internal/config/config.go`
4. Add a `.proto` message in `proto/policy/`
5. Add server-side validation in `server/internal/services/`
6. Update `server/internal/grpc/server.go` (`modelToProto`)
7. Update the frontend policy editor in `PolicyDetailsModal.tsx`

### TypeScript / Frontend

The frontend uses **PatternFly 5**, **React 18**, and **TypeScript**. All
components must be functional (no class components) and use hooks.

- Follow the [PatternFly design guidelines](https://www.patternfly.org/).
- Define explicit TypeScript types — avoid `any`.
- API calls go in `src/apiClient/`. Components must not call `fetch` directly.
- Keep components focused. Extract reusable logic into hooks or helpers.
- The Webpack build is the source of truth. Verify with `make frontend`.

### Protocol Buffers

Proto definitions live in `proto/`. Run `make proto` after any `.proto` change
to regenerate Go and TypeScript code. Generated files (`server/pkg/grpc/` and
`server/web/frontend/src/generated/`) are committed to the repository and must
be kept in sync with their definitions.

```bash
make proto        # regenerate Go + TypeScript from proto/
```

### SQL Migrations

Database schema changes require a new migration file in
`server/internal/database/migrations/`. Migrations are applied automatically
by the server on startup in filename order.

- Name files `NNNNNN_descriptive_name.up.sql` / `.down.sql`.
- Every `up` migration must have a corresponding `down` migration.
- Migrations must be non-destructive toward existing data where possible.
- Never modify an already-released migration — add a new one instead.

---

## Testing

All new features and bug fixes must include tests. The CI pipeline blocks
merges if tests fail.

### Running tests

```bash
make test           # all tests (server + agent)
make test-server    # server tests only
make test-agent     # agent tests only
make coverage       # HTML coverage reports → server/coverage.html, agent/coverage.html
```

To run a specific test:

```bash
cd server && go test -v -run TestFunctionName ./internal/services/...
cd agent  && go test -v -run TestFunctionName ./internal/policy/...
```

### Writing tests

**Go:**

- Use table-driven tests (`[]struct{ name, input, want }`) for functions with
  multiple cases.
- Use `t.TempDir()` for any test that writes to the filesystem — never write to
  hardcoded paths.
- Tests in `agent/internal/policy/` are whitebox (same package). Tests in
  `server/internal/services/` and `server/internal/api/` are also same-package.
- Mock at the interface boundary, not deep inside the call stack.
- Avoid `time.Sleep` in tests. Use synchronisation primitives or channels.

**Frontend:**

At present, the frontend does not have an automated test suite. Contributions
that add React Testing Library or Playwright coverage are welcome.

---

## Commit Messages

We follow the [Conventional Commits](https://www.conventionalcommits.org/)
specification:

```text
<type>(<scope>): <short summary>

[optional body — explain the why, not just the what]

[optional footer — e.g. Closes #42, Co-Authored-By: …]
```

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`

**Scopes:** `server`, `agent`, `frontend`, `proto`, `packaging`, `ci`, `docs`

**Examples:**

```text
feat(agent): add KDE Plasma desktop notification on policy update
fix(server): correct node_count query after junction table migration
docs: update contributing guide to reflect Go agent
test(server): add table-driven tests for policy binding service
chore(packaging): add nfpm configs for deb/rpm/apk/arch packages
```

Commit messages must be written in the imperative mood ("add", "fix", "update"),
not past tense ("added", "fixed", "updated"). Keep the summary line under 72
characters.

---

## Pull Request Process

Bor uses the **fork + branch** workflow against the `master` branch.

### Step by step

1. **Fork** the repository on GitHub and clone your fork:

   ```bash
   git clone https://github.com/<your-username>/Bor.git
   cd Bor
   git remote add upstream https://github.com/VuteTech/Bor.git
   ```

2. **Create a feature branch** from `master`:

   ```bash
   git fetch upstream
   git checkout -b feat/your-feature-name upstream/master
   ```

3. **Make your changes**, following the coding standards above.

4. **Verify locally** before pushing:

   ```bash
   make fmt          # format
   make lint         # lint
   make test         # tests
   make server       # verify it builds
   make agent        # verify it builds
   ```

5. **Commit** with a conventional commit message.

6. **Push** to your fork and open a Pull Request against `VuteTech/Bor:master`:

   ```bash
   git push origin feat/your-feature-name
   ```

7. **Fill in the PR description:**
   - What problem does this solve?
   - How was it tested?
   - Any breaking changes or migration notes?
   - Reference related issues (`Closes #42`).
   - Disclose AI tool usage if applicable (see [AI policy](#ai-assisted-contributions)).

### Review criteria

A PR will be reviewed for:

- Correctness and test coverage.
- Adherence to coding standards (`fmt`, `lint`, `vet` must pass).
- Security implications — especially for authentication, PKI, and policy
  enforcement paths.
- Scope: each PR should address a single concern. Split unrelated changes into
  separate PRs.

PRs may be requested to be rebased, squashed, or split before merging.
Maintainers may close PRs that are abandoned or fall out of scope.

---

## Email Patches

If you prefer not to use GitHub, patches are accepted by email at
**<bor@vute.tech>**.

Please format patches with `git format-patch` and include a cover letter for
non-trivial series:

```bash
# Single commit
git format-patch -1 HEAD

# Series of commits with a cover letter
git format-patch --cover-letter origin/master
```

Use the same commit message conventions described above. Include a sign-off
line (`git commit --signoff`) to confirm you have the right to submit the
contribution under the project license.

---

## Issue Reporting

Use the [GitHub issue tracker](https://github.com/VuteTech/Bor/issues).

When reporting a bug, include:

- A clear, minimal description of the problem.
- Steps to reproduce.
- Expected vs. actual behaviour.
- Environment: OS, distribution, agent and server versions.
- Relevant log output (`journalctl -u bor-agent`, server stderr).

For feature requests, describe the use case and the problem it solves before
proposing a solution.

---

## Security Vulnerabilities

**Do not open a public issue for security vulnerabilities.**

Report security issues privately to **<bor@vute.tech>**. Include:

- A description of the vulnerability and its impact.
- Steps to reproduce or a proof-of-concept.
- Any suggested mitigations you are aware of.

We will acknowledge receipt within 48 hours and aim to issue a fix within 90
days of the initial report, coordinating disclosure with the reporter.

---

## AI-Assisted Contributions

We recognise that AI-powered tools (such as GitHub Copilot, Claude, ChatGPT,
and others) are increasingly part of modern development workflows. We welcome
contributions that use these tools, subject to the following policy.

### What is permitted

- Using AI tools for code generation, refactoring, writing tests, and drafting
  documentation.
- Using AI to understand unfamiliar parts of the codebase or explore
  implementation approaches.

### Requirements

**Human review and understanding is mandatory.** Every line of AI-generated or
AI-assisted code must be reviewed, understood, and validated by the contributor
before submission. You must be able to explain the purpose and behaviour of any
code you submit, as if you had written it yourself.

**Disclosure is required.** If AI tools were used in a substantial way (beyond
simple autocomplete), note this in the pull request description. A brief mention
is sufficient — for example:

> *AI-assisted: used Copilot for initial test scaffolding.*
> *Claude helped draft the error-handling logic.*

**Quality standards still apply.** AI-generated code is held to the same
standards as human-written code. It must pass linting, testing, and code
review. *"The AI wrote it"* is not an acceptable justification for code that
does not meet project standards.

**No AI-generated code without comprehension.** Do not submit code you do not
understand. If you cannot explain what a function does, how it handles edge
cases, or why a particular approach was chosen, do not submit it.

**License and IP awareness.** Ensure that AI-generated output does not
introduce code that violates the project's LGPL-3.0-or-later license or
third-party intellectual property rights. Do not prompt AI tools with
proprietary code from other projects.

### What is not permitted

- Submitting AI-generated pull requests without meaningful human review.
- Using AI to generate large volumes of low-quality or superficial
  contributions.
- Relying on AI for security-critical code without thorough manual review and
  testing.

---

## License

By contributing, you agree that your contributions will be licensed under the
project's [GNU Lesser General Public License v3.0](../LICENSE.md).

All source files must carry the SPDX header:

```c
// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors
```

---

*Questions? Open a [GitHub discussion](https://github.com/VuteTech/Bor/discussions)
or reach out at <blago.petrov@vute.tech>.*
