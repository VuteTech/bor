# Contributing

Contributions to Bor are welcome: bug reports, fixes, new policy types,
tests and documentation. This page
describes how to submit a change, what reviewers look for, and the coding
and compliance rules every change has to follow.

## Ways to contribute

- **GitHub pull requests** against
  [VuteTech/Bor](https://github.com/VuteTech/Bor) (preferred).
- **Patches by email** to <bor@vute.tech>, if you prefer not to use GitHub.
- **Issues** in the [GitHub issue tracker](https://github.com/VuteTech/Bor/issues)
  for bugs and feature requests. For a bug, include steps to reproduce,
  expected and actual behavior, the distribution and the server and agent
  versions, and relevant log output (`journalctl -u bor-agent`,
  `journalctl -u bor-server`). For a feature request, describe the use case
  before proposing a solution.

Security problems are reported privately; see
[Reporting security vulnerabilities](#reporting-security-vulnerabilities).

Contributions made with the help of AI coding assistants are welcome under
the rules in [AI-assisted contributions](ai-assisted-contributions.md): you
must understand and test everything you submit, and disclose tool use in
the pull request.

## Pull request workflow

Bor uses the fork and branch workflow against `master`.

1. Fork the repository on GitHub and clone your fork:

    ```bash
    git clone https://github.com/<your-username>/Bor.git
    cd Bor
    git remote add upstream https://github.com/VuteTech/Bor.git
    ```

2. Create a feature branch from the current `master`:

    ```bash
    git fetch upstream
    git checkout -b feat/short-description upstream/master
    ```

3. Make the change, with tests, following the conventions below.

4. Verify locally (see [Testing and linting](testing.md)):

    ```bash
    make fmt
    make lint
    make test
    make server agent
    cd server/web/frontend && npm run typecheck   # for UI changes
    ```

5. Commit with a [Conventional Commits](#commit-messages) message, push to
   your fork and open a pull request against `VuteTech/Bor:master`.

6. In the pull request description, explain what problem the change solves,
   how you tested it, any breaking changes or migration notes, and reference
   related issues (`Closes #42`).

Keep each pull request to a single concern. Split unrelated changes into
separate pull requests. Reviewers may ask you to rebase, squash or split a
pull request before it is merged.

### Email patches

Format patches with `git format-patch`, with a cover letter for a series:

```bash
git format-patch -1 HEAD                          # single commit
git format-patch --cover-letter upstream/master   # series
```

Use the same commit message conventions, and add a sign-off line
(`git commit --signoff`) to confirm that you have the right to submit the
change under the project license.

## Commit messages

Commits follow the [Conventional Commits](https://www.conventionalcommits.org/)
specification:

```text
<type>(<scope>): <short summary>

[optional body: explain why, not only what]

[optional footer, e.g. Closes #42]
```

Types
:   `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`

Scopes
:   `server`, `agent`, `frontend`, `proto`, `packaging`, `ci`, `docs`.
    A narrower scope that names the feature (for example `auth`) is also
    used in the history.

Write the summary in the imperative mood ("add", "fix", not "added") and keep
it under 72 characters. Examples:

```text
feat(agent): add KDE Plasma desktop notification on policy update
fix(server): correct node_count query after junction table migration
docs: add development environment guide
test(server): add table-driven tests for policy binding service
chore(packaging): add nfpm configs for deb/rpm/apk/arch packages
```

## Code review

Reviewers look at:

- **Correctness and tests.** New behavior and bug fixes come with tests; all
  CI jobs pass.
- **Conventions.** `gofmt`, golangci-lint with 0 issues, `npm run typecheck`
  for UI changes, and the rules on this page.
- **Security.** Changes to authentication, authorization, the PKI,
  enrollment and policy enforcement get extra scrutiny. Expect questions
  about failure modes and input validation.
- **Scope.** One concern per pull request, no unrelated reformatting.
- **Completeness.** Database changes include a migration, protobuf changes
  include the regenerated code, file-based policy types are wired into the
  agent's file watcher (see [Adding a policy type](adding-a-policy-type.md)),
  and user-visible changes update the documentation.

Maintainers may close pull requests that are abandoned or out of scope.

## Coding conventions

### Go

- Follow standard Go project layout and
  [Effective Go](https://go.dev/doc/effective_go).
- Wrap errors with context: `fmt.Errorf("failed to load policy %s: %w", id, err)`.
  Do not swallow errors silently.
- Pass `context.Context` as the first argument to functions that do I/O or
  can be cancelled. Avoid global state; pass dependencies explicitly.
- Prefer the standard library and existing dependencies. Discuss a
  significant new dependency in an issue first.
- Exported identifiers have godoc comments.
- Write table-driven tests.

### gRPC

- Return proper status codes from `google.golang.org/grpc/status` and
  `codes` (for example `codes.InvalidArgument`, `codes.PermissionDenied`,
  `codes.NotFound`), not generic errors.
- Clients use timeouts and retries; the agent reconnects with capped
  exponential backoff.

### Database

- Every schema change is a new pair of migration files in
  `server/internal/database/migrations/` (`NNNNNN_name.up.sql` and
  `.down.sql`). Never edit a migration that has been released.
- Use parameterized queries (placeholders), never string concatenation.
- Wrap multi-step operations in a transaction.

See [Database and migrations](database.md).

### Frontend

- PatternFly 6, React function components with hooks, explicit TypeScript
  types (avoid `any`).
- Use the shared components and design tokens described in
  [Frontend](frontend.md) before writing new ones. Colors come from
  `--pf-t--*` tokens, never raw hex.
- Keep WCAG 2.2 Level AA: labels on icon-only buttons, `aria-label` on
  spinners, live regions for status messages, no color-only information,
  visible focus rings.

### Text style

These rules apply to every file in the repository: code, comments, strings,
documentation and commit messages.

- Do not use the Unicode right arrow or the em and en dash characters. Write
  `->` for arrows and a plain `-` for dashes and ranges.
- Development-time planning and working documents stay out of the
  repository. Never reference them (by path or section) from code, comments,
  documentation or commit messages.

## Security and compliance rules

Bor targets FIPS 140-3, BSI TR-02102, SOG-IS, eIDAS, NIS2, the EU
Cybersecurity Act and GDPR. Do not introduce dependencies, algorithms or
patterns that would break these targets. In practice:

| Area | Rule |
|---|---|
| TLS | Minimum TLS 1.2, prefer 1.3. AEAD cipher suites only (AES-GCM, ChaCha20-Poly1305). No CBC suites |
| Hashing | SHA-256 or SHA-512. No MD5, no SHA-1 (except where a legacy protocol requires it, opt-in and logged) |
| Keys | RSA 3072 bits or more, ECDSA P-256 or P-384, Ed25519. AES-256 preferred, AES-128 minimum |
| Algorithms | Only FIPS-approved primitives from the Go standard library crypto packages. No RC4, Blowfish, 3DES, non-NIST curves or custom crypto |
| Passwords | bcrypt with cost 12 or more, or Argon2id. Never store plaintext or reversible passwords |
| Tokens | Every JWT has an expiry; HMAC-SHA256 at minimum |
| Audit | Authentication events, privilege changes and policy changes are logged with timestamp, actor, action and target |
| Secrets in logs | Never log passwords, TOTP secrets, private keys, enrollment tokens or recovery keys. Redact before storage |
| Personal data | Collect only what is needed; audit data with personal data must respect retention and erasure settings |
| Input | Validate input at every boundary (REST, gRPC, files read by the agent) |

The full cryptographic inventory and standards mapping are in
[Security and compliance](../security/compliance.md).

## Reporting security vulnerabilities

Do not open a public issue for a security vulnerability. Report it privately
to <bor@vute.tech> with:

- a description of the vulnerability and its impact,
- steps to reproduce or a proof of concept,
- any mitigations you know of.

The project acknowledges reports within 48 hours and aims to ship a fix
within 90 days of the initial report, coordinating disclosure with the
reporter.

## License

Bor is licensed under the GNU Lesser General Public License v3.0 or later
(`LGPL-3.0-or-later`, see `LICENSE.md`). By contributing, you agree that your
contribution is licensed under the same terms.

Go, TypeScript and JavaScript source files start with an SPDX license
header:

```go
// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors
```

Add this header to every new Go, TypeScript and JavaScript file.
