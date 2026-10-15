# Testing and linting

This page lists the checks a change has to pass, how to run each one locally,
and how CI runs them. A pull request is expected to be green on all of them.

| Check | Local command | Runs in CI |
|---|---|---|
| Go tests | `make test` | yes (`test` job) |
| golangci-lint (includes gofmt and goimports) | `make lint-server`, `make lint-agent` | yes (`lint` job) |
| Frontend design-token guardrail | `make check-frontend-tokens` | yes (`lint` job) |
| Frontend typecheck | `npm run typecheck` | yes (`build-server-frontend` job) |
| Frontend build | `npm run build` | yes (`build-server-frontend` job) |
| Frontend accessibility lint | `make lint-frontend` | no, run it locally |

`make lint` runs `lint-server`, `lint-agent`, `lint-frontend` and
`check-frontend-tokens` in that order.

## Go tests

```bash
make test           # test-server and test-agent, verbose
make test-server    # cd server && go test -v ./...
make test-agent     # cd agent && go test -v ./...
```

Run a single test or package:

```bash
cd server && go test -v -run TestFunctionName ./internal/services/...
cd agent  && go test -v -run TestFunctionName ./internal/policy/...
```

Tests live next to the code (`*_test.go`), mostly in the same package so they
can exercise unexported functions; a few use an external `_test` package.
Conventions:

- Write table-driven tests for functions with more than one interesting
  case: a slice of cases with a `name`, the inputs and the expected result,
  and a `t.Run(tc.name, ...)` loop.
- Use `t.TempDir()` for anything that writes files. Never write to
  hard-coded system paths such as `/etc` from a test.
- Mock at interface boundaries (repositories, clients), not deep inside a
  call chain.
- Avoid `time.Sleep`; synchronize with channels or other primitives.

A new feature or bug fix comes with tests. Because the agent module depends
on the server module through a `replace` directive, run both test suites
when you change anything under `server/` that the agent imports (in
practice: the generated protobuf code).

### Coverage

```bash
make coverage
```

This writes `coverage.out` and an HTML report `coverage.html` in both
`server/` and `agent/`.

## golangci-lint

Each module has its own configuration (`server/.golangci.yml`,
`agent/.golangci.yml`, golangci-lint configuration format version 2). It
enables, among others, `errcheck`, `govet` (with `shadow`), `staticcheck`,
`gosec`, `gocritic`, `revive`, `unparam` and `bodyclose`, and the `gofmt` and
`goimports` formatters. `gosec` and `errcheck` are relaxed in `_test.go`
files.

```bash
make lint-server
make lint-agent
```

A Go change is complete only when both report 0 issues.

!!! warning "Use the latest golangci-lint"
    CI runs golangci-lint through `golangci/golangci-lint-action` with
    `version: latest`. A local golangci-lint that is a few releases behind
    can miss checks that CI enforces, so a clean local run means little.
    Upgrade to the latest release before you trust the result:

    ```bash
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    golangci-lint --version
    ```

    Note that `make install-deps` installs the module path shown above. Check
    with `golangci-lint --version` that the binary on your `PATH` is the
    version you just installed.

When a finding is a deliberate exception, suppress it on the line with a
`//nolint:<linter> // reason` comment that explains why, as the existing
code does. Do not disable linters globally.

## Formatting

```bash
make fmt     # go fmt ./... in server/ and agent/
```

gofmt and goimports are also enforced by golangci-lint, so unformatted code
fails the lint job.

## Frontend checks

Run these in `server/web/frontend/` after `npm install` (or `npm ci`).

### Typecheck

```bash
npm run typecheck    # tsc --noEmit
```

webpack builds the UI through `babel-loader` with
`@babel/preset-typescript`, which removes type annotations without checking
them. A type error therefore does not break `npm run build` or `npm run dev`.
`tsc --noEmit` is the only real typecheck, and CI runs it before the build.

### Accessibility lint

```bash
make lint-frontend   # npm run lint: eslint "src/**/*.{ts,tsx}"
```

`server/web/frontend/eslint.config.mjs` enables the recommended rules of
`eslint-plugin-jsx-a11y` only. It is an accessibility gate, not a general
style linter. CI does not run it yet, so run it yourself for UI changes. The
UI targets WCAG 2.2 Level AA; see [Frontend](frontend.md).

### Design-token guardrail

```bash
make check-frontend-tokens
```

`scripts/check-frontend-tokens.sh` is a plain shell script (no Node needed)
with two rules for `server/web/frontend/src`:

1. Legacy PatternFly 5 variables (`--pf-v5-*`) are banned outright in
   `.ts`, `.tsx` and `.css` files. PatternFly 6 uses `--pf-t--*` and
   `--pf-v6-*`.
2. Raw hex color literals in `.ts` and `.tsx` files (excluding
   `src/generated/`) are ratcheted against `scripts/frontend-hex-baseline.txt`.
   The count per file and value may go down but not up. `bor-theme.css`,
   where the design tokens are defined, is exempt.

If you remove hex literals, the script passes and suggests refreshing the
baseline. After an intentional, reviewed change, regenerate it:

```bash
scripts/check-frontend-tokens.sh --update
```

### Automated UI tests

The frontend has no automated test suite at present (there is no `test`
script in `package.json`). UI changes are verified by the typecheck, the
lints above, and by checking them in a browser.

## Verify UI changes in a browser

1. Start PostgreSQL and the server as described in
   [Development environment](setup.md#run-the-server), with
   `BOR_ADMIN_PASSWORD` set so you know the `admin` password.
2. Rebuild the bundle (`npm run build`, or keep `npm run dev` running) and
   restart `make run-server`. The bundle is embedded at compile time, so a
   server that was started before the rebuild keeps serving the old UI.
3. Open `https://localhost:8443`, accept the self-signed certificate and log
   in as `admin`.
4. Check the change in both light and dark themes, with the keyboard only,
   and at a narrow window width.

For scripted checks you can log in with curl. Authentication uses cookies:
`bor_session` (HttpOnly) carries the session and `bor_csrf` must be echoed in
the `X-CSRF-Token` header on state-changing requests.

```bash
curl -sk -c jar.txt -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"a-local-dev-password"}' \
  https://localhost:8443/api/v1/auth/login
```

Use a fresh database or delete the test objects you created when you are
done, so that test data and the development admin password do not linger.
