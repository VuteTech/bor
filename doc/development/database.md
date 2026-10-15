# Database and migrations

Bor stores all server state in PostgreSQL. This page covers how the server
connects, how schema migrations are written and applied, the repository
pattern used by the rest of the code, the conventions for queries and
transactions, and how database code is tested.

## Connection

`database.New` (`server/internal/database/database.go`) opens a
`database/sql` pool with the `github.com/lib/pq` driver from the
`DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` and `DB_SSLMODE`
settings (or their `server.yaml` equivalents):

- A host that starts with `/` is a Unix socket directory. Port and password
  are omitted and `sslmode` defaults to `disable`, which suits peer
  authentication on the same machine.
- The pool is bounded to 25 open and 25 idle connections, with a 5-minute
  connection lifetime, so a burst of reconnecting agents cannot exhaust
  PostgreSQL's `max_connections`.
- The connection is pinged at start-up; failure is fatal.

For local development, `make dev` starts PostgreSQL in a container on
`localhost:5432` with the credentials from `.env.example` (`bor` / `bor`,
database `bor`). See [Development environment](setup.md).

## Migrations

### Layout and naming

Migrations live in `server/internal/database/migrations/`:

```text
000001_initial_schema.up.sql
000001_initial_schema.down.sql
000002_seed_data.up.sql
000002_seed_data.down.sql
...
000036_sso_link_approval.up.sql
000036_sso_link_approval.down.sql
```

- File names are `NNNNNN_short_name.up.sql` and `NNNNNN_short_name.down.sql`,
  with a six-digit, zero-padded number. The numbers have gaps (older
  migrations were consolidated); that is harmless.
- Each `.up.sql` has a matching `.down.sql` that reverses it.
- Start each file with a comment explaining why the change exists.

### The migration runner

Bor does not use an external migration tool. `DB.RunMigrations` runs on
every server start, before any repository is used:

1. Create `schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMP)`
   if it does not exist.
2. Read the `.up.sql` files embedded in the binary:

    ```go
    //go:embed migrations/*.up.sql
    var migrationFiles embed.FS
    ```

3. Sort the file names lexically.
4. For each file, the version is the file name without `.up.sql` (for
   example `000031_restart_safe_state`). If that version is already in
   `schema_migrations`, skip it.
5. Otherwise run the whole file and insert the version in **one
   transaction**. On error the transaction is rolled back and the server
   refuses to start.

Consequences you need to know:

- **No checksums.** The runner only compares names. Editing an applied
  migration has no effect on existing databases, and a fresh database gets
  the edited version, so the two diverge silently.
- **Renaming a file re-runs it**, because the version is the file name.
- **Ordering is by name**, so the zero padding matters.
- **Down migrations are not embedded and never run by the server.** They
  exist for developers and operators who need to roll a schema back by
  hand, for example with `psql -f`.
- **Each file is one transaction.** Do not put `BEGIN` / `COMMIT` in a
  migration, and avoid statements PostgreSQL refuses inside a transaction
  (for example `CREATE INDEX CONCURRENTLY`).
- **No cross-instance lock.** Two server instances starting against an
  empty database at the same time can race; start one instance first when
  upgrading a multi-instance setup.

!!! note "`make migrate-up` / `make migrate-down`"
    The Makefile has these targets, but they call `go run ./cmd/migrate`,
    and there is no `server/cmd/migrate` in the tree. Migrations are applied
    by starting the server.

Server extension modules keep their own migrations and apply them with
`DB.RunModuleMigrations(module, fsys, dir)`, which uses the same rules and
records versions in `ext_schema_migrations` under the module name.

### Adding a migration safely

1. Take the next free number after the highest existing one:

    ```bash
    ls server/internal/database/migrations | tail -2
    ```

2. Create both files, for example
   `000037_widget_settings.up.sql` and `000037_widget_settings.down.sql`.
3. Write the up migration so it works on a database with real data:
    - add columns as nullable or with a `DEFAULT`;
    - backfill in the same file if the code needs the data;
    - add foreign keys with an explicit `ON DELETE` behavior;
    - name indexes `idx_<table>_<columns>`.
4. Write the down migration to undo exactly that (`DROP ... IF EXISTS` in
   reverse order).
5. Never change the behavior of a migration that has been released. Fix
   mistakes with a new migration.
6. If the change adds permissions or roles, insert them in the migration
   (see `000028_granular_user_role_perms` and
   `000030_disk_encryption` for examples) so existing installations
   receive them.
7. Start the server against a scratch database and check the log line
   `Applied migration: 000037_widget_settings`. Then apply the down
   migration by hand and the up migration again to verify they are
   symmetric.
8. Run `make test-server`; `TestMigrationFilesEmbedded` checks that the
   embedded files sort in the expected order.

Example from `000031_restart_safe_state.up.sql`:

```sql
-- Single-row high-water mark for the PolicyHub revision counter. Persisting
-- it keeps revision numbers monotonic across server restarts, ...
CREATE TABLE policy_hub_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    revision BIGINT NOT NULL DEFAULT 0
);

INSERT INTO policy_hub_state (id, revision) VALUES (1, 0);
```

and its down migration:

```sql
DROP TABLE IF EXISTS policy_hub_state;
DROP TABLE IF EXISTS enrollment_tokens;
```

## Repository pattern

All SQL lives in `server/internal/database`. Each file holds one repository
type that wraps `*database.DB`:

```go
type PolicyHubStateRepository struct {
    db *DB
}

func NewPolicyHubStateRepository(db *DB) *PolicyHubStateRepository {
    return &PolicyHubStateRepository{db: db}
}
```

- Repositories are created once in `app.Run` and injected into services.
  Services never build SQL, and handlers never call repositories for
  business operations.
- Methods take a `context.Context` first and use the `...Context` variants
  (`QueryRowContext`, `ExecContext`, `QueryContext`) so request
  cancellation reaches the database.
- They return models from `server/internal/models`. A missing row is
  usually `nil, nil` (or a `found` flag), not an error; check the existing
  methods of the repository you extend and follow them.
- Errors are wrapped with context:
  `fmt.Errorf("failed to load policy hub revision: %w", err)`.

## Queries and transactions

- **Always use placeholders** (`$1`, `$2`, ...) for values. Never build SQL
  by concatenating input. `lib/pq` sends parameterized queries as prepared
  statements, so values never become SQL.
- **Dynamic `ORDER BY` and filters** go through allowlists. For example the
  node, compliance and audit-log list queries map a requested sort field to
  a fixed column expression and fall back to a default for anything
  unknown; the tests in `nodes_pagination_test.go`,
  `compliance_pagination_test.go` and `audit_logs_sort_test.go` feed
  injection strings to prove it.
- **Multi-step changes use a transaction.** The pattern used across the
  package:

    ```go
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    // ... tx.ExecContext(ctx, ...) ...

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("failed to commit: %w", err)
    }
    ```

  Examples: `dconf.go` and `polkit.go` (catalogue upserts), `roles.go`,
  `flatpak.go`, `luks.go`.
- **Make single statements atomic where possible** instead of
  read-then-write. Consuming an enrollment token is one
  `DELETE ... RETURNING`, and saving the hub revision is an upsert with
  `GREATEST`, so concurrent callers cannot race.
- **Sensitive values are stored hashed or encrypted.** Enrollment tokens are
  stored as SHA-256 hashes, LUKS recovery keys are envelope-encrypted under
  a KEK held outside the database, and local passwords use
  PBKDF2-HMAC-SHA256 with 600,000 iterations (`hashPassword` in
  `server/internal/services/auth.go`).

## Testing database code

There is no live-database test suite in the repository. The tests in
`server/internal/database` are pure unit tests that do not connect to
PostgreSQL:

- `database_test.go` checks that the migrations are embedded, sort
  correctly and include the initial schema and seed files.
- The `*_pagination_test.go`, `*_sort_test.go` and `flatpak_test.go` tests
  exercise the query builders (order-by allowlists, filter construction,
  full-text query escaping) and assert on the generated SQL fragments.

Logic above the repositories is tested in `server/internal/services` and
`server/internal/api` with in-memory fakes of the narrow interfaces the
services depend on. When you add a query builder, add table-driven tests in
the same style, including hostile input. For schema changes, verify by
hand against a real PostgreSQL as described in
[Adding a migration safely](#adding-a-migration-safely), and see
[Testing and linting](testing.md) for the rest of the test setup.
