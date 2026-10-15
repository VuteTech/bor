# dconf internals

Developer reference for the `Dconf` policy type: the GSettings schema
catalog the editor is built on, how the agent turns policies into dconf
keyfiles and locks, how compliance is checked, and where each piece lives.
For administrator guidance see [GNOME (dconf)](../../policy-types/dconf.md).

## Files on the managed node

```text
/etc/dconf/profile/user          profile; the agent ensures "system-db:<db>" is present
/etc/dconf/db/<db>.d/00-bor      keyfile written by the agent
/etc/dconf/db/<db>.d/locks/bor   locks file: one dconf path per locked key
/etc/dconf/db/<db>                binary database compiled by "dconf update"
```

`<db>` is `local` unless a policy sets `db_name`. The value comes from the
server and becomes a path component, so `SyncDConfFiles` rejects anything
that is not a plain identifier (`isValidPathIdentifier`).

If `/etc/dconf/profile/user` does not exist, the agent creates it with
`user-db:user` followed by `system-db:<db>`. If it exists, the agent
appends `system-db:<db>` only when that line is missing and keeps the rest
of the file. The write is atomic (temporary file and rename).

## Policy content

`proto/policy/dconf.proto`:

```proto
message DConfEntry {
  string schema_id = 1;  // e.g. "org.gnome.desktop.screensaver"
  string path      = 2;  // dconf path override; empty for non-relocatable schemas
  string key       = 3;  // e.g. "lock-enabled"
  string value     = 4;  // GVariant text format: "true", "'Adwaita'", "uint32 300"
  bool   lock      = 5;  // add this key to the locks file (user cannot override)
}

message DConfPolicy {
  repeated DConfEntry entries = 1;
  string db_name = 2;
}
```

The editor stores the original snake_case field names (`schema_id`,
`db_name`); `Dconf` is listed in `protoNameTypes` in
`server/internal/export/export.go` so exported policies keep that form.

Server-side validation (`ValidateDConfPolicy` in
`server/internal/services/dconf.go`) requires at least one entry and a
non-empty `schema_id`, `key` and `value` on every entry. It does not check
values against the schema; the editor does that from the catalog, and the
agent's compliance check catches the rest.

## Schema catalog

The editor offers schemas, keys, types, defaults, ranges and enum choices
from a catalog stored on the server. The catalog has two tiers.

### Built-in tier

`server/assets/dconf_builtin_schemas.json` is embedded in the server
binary (`server/assets/dconf.go`, `go:embed`). At every startup,
`seedDConfBuiltinSchemas` (`server/pkg/app/dconf_seed.go`) runs in a
goroutine and upserts each schema into `dconf_schemas` with
`source = 'builtin'`. This makes the editor usable before any agent has
connected.

Regenerate the file on a reference GNOME system:

```bash
make update-dconf-schemas
# or, from copied *.gschema.xml files:
make update-dconf-schemas SCHEMAS_DIR=/path/to/schemas
```

The target runs `agent/cmd/gen-dconf-schemas`, which parses the schema XML
in `/usr/share/glib-2.0/schemas` (or `SCHEMAS_DIR`) and writes the JSON.
It is a manual step, not part of `make server` or `make agent`; commit the
updated JSON.

### Agent tier

On the first stream connection after startup, the agent scans
`/usr/share/glib-2.0/schemas/*.gschema.xml` in the background
(`ScanGSettingsSchemas` in `agent/internal/policy/dconf_catalogue.go`).
The scan is best-effort: files that fail to parse are skipped, and a
missing directory yields an empty catalog. The agent then:

1. keeps the set of installed schema IDs in `dconfSchemaIndex` for its own
   compliance checks, and
2. sends the full metadata (keys, GVariant types, summaries, descriptions,
   defaults, enum values, ranges, choices) and the detected GNOME version
   to the server with the `ReportSchemaCatalogue` RPC.

The server handler (`server/internal/grpc/dconf_catalogue.go`) upserts
every schema with `source = 'agent'` and replaces the node's rows in
`node_dconf_schemas`. The upsert never downgrades a built-in row:

```sql
source = CASE WHEN dconf_schemas.source = 'builtin' THEN 'builtin' ELSE EXCLUDED.source END
```

### REST API

| Method | Path | Permission | Description |
|---|---|---|---|
| `GET` | `/api/v1/dconf/schemas` | `policy:view` | All known schemas; `?node_id=<uuid>` limits the list to schemas reported by that node |

The handler is `DConfHandler.ListSchemas` in
`server/internal/api/dconf_schemas.go`; the frontend client is
`server/web/frontend/src/apiClient/dconfApi.ts`, used by
`views/Policies/DConfPolicyEditor.tsx`.

## Enforcement on the agent

`syncAllDConf` in `agent/cmd/agent/main.go` runs after every change to the
set of Dconf policies (snapshot, create, update, delete) and when a managed
file is tampered with:

1. **Order** - the cached policies are sorted ascending by binding
   priority, so the highest-priority policy comes last.
2. **Merge** - `MergeDConfPolicies` combines all entries; a later entry
   with the same `(schema_id, path, key)` replaces an earlier one, so the
   highest priority wins. The database name is the last non-empty
   `db_name` in that order, otherwise `local`.
3. **Render** - `DConfPolicyToFiles` groups entries into sections named
   after the dconf path (the `path` override with slashes trimmed, or the
   schema ID with dots replaced by slashes, for example
   `[org/gnome/desktop/screensaver]`) and writes `key=value` lines. Every
   entry with `lock: true` adds `/<path>/<key>` to the locks file.
4. **Suppress** - `suppressManagedWrites` is called for the keyfile and
   locks file, with `defer updateWatcher(cfg)`.
5. **Write** - `SyncDConfFiles` creates the directories, backs up the
   original keyfile and locks file once (`BackupOriginal` keeps the first
   backup and writes an empty sentinel when there was no original), writes
   `00-bor` (with a "managed by Bor" header) and `locks/bor` atomically,
   ensures the profile line, and runs `dconf update`, which recompiles the
   database and notifies running sessions.
6. **Check and report** - see below.

With no Dconf policies left, the sync still runs: it writes an empty
keyfile and locks file for `local` and recompiles, which removes every
Bor-managed value.

The file watcher covers `00-bor` and `locks/bor` for every database name
in use (`getManagedPaths`); any change under `/etc/dconf/` that the watcher
reports triggers `syncAllDConf` from `onTamperedFile`.

## Compliance

`CheckDConfCompliance` checks each merged entry:

| Situation | Item status |
|---|---|
| Schema not in the agent's `dconfSchemaIndex` | `INAPPLICABLE` ("schema not installed on this node") |
| `gsettings get <schema> <key>` fails | `ERROR` |
| Current value equals the policy value | `COMPLIANT` |
| Otherwise | `NON_COMPLIANT` ("expected ..., got ...") |

`RollupDConfCompliance` derives the overall status: all items inapplicable
gives `INAPPLICABLE`, any error gives `ERROR`, any non-compliant item gives
`NON_COMPLIANT`, otherwise `COMPLIANT`.

Each policy is then reported separately with
`ReportComplianceWithStatus`. Items whose key the policy sets but whose
value lost the merge to a higher-priority policy are reported as
`INAPPLICABLE` with the message "Overridden by a higher-priority policy;
applied value: ...", and the policy's status is rolled up from its own
items (`rollupProtoItems`). Without this, every losing policy would look
compliant or non-compliant depending only on the winner.

### GVariant integer prefixes

`gsettings get` prints values in canonical GVariant text, which includes a
type prefix for every integer type except `int32`: `uint32 300`,
`uint16 5042`. Before comparing, `NormalizeGVariant` strips a leading
`uint16`, `uint32`, `uint64`, `int16`, `int32`, `int64`, `byte` or
`handle` word from both sides, so `uint16 5042` and `5042` compare equal.

The values still have to be written with the right prefix for `dconf
update` to store the right type. The editor does this through
`GVARIANT_INT_PREFIX` in `DConfPolicyEditor.tsx`:

| GVariant type code | Prefix written by the editor |
|---|---|
| `u` (uint32) | `uint32` |
| `q` (uint16) | `uint16` |
| `n` (int16) | `int16` |
| `t` (uint64) | `uint64` |
| `x` (int64) | `int64` |
| `y` (byte) | `byte` |
| `i` (int32) | none |

Content written by hand (or imported) must follow the same rule.

## Compliance storage

Compliance results of all policy types share one table, created for dconf:

```text
compliance_results
  id          UUID PK DEFAULT gen_random_uuid()
  node_id     UUID -> nodes(id)    ON DELETE CASCADE
  policy_id   UUID -> policies(id) ON DELETE CASCADE
  status      VARCHAR(20) DEFAULT 'unknown'
  message     TEXT
  items_json  JSONB   -- [{schema_id, key, status, message}, ...]
  reported_at TIMESTAMPTZ
  UNIQUE (node_id, policy_id)
```

Each report upserts the row for `(node_id, policy_id)`. The list queries in
`server/internal/database/dconf.go` only return rows for which an
**enabled** binding still connects the policy to a node group that contains
the node (an `EXISTS` subquery over `policy_bindings` and
`node_group_members`), so results of removed or disabled bindings disappear
without a cleanup job. `GET /api/v1/compliance` (`compliance:view`)
serves them to the **Compliance** page, which shows the per-key items.

## Database schema

```text
dconf_schemas
  schema_id    VARCHAR(255) PRIMARY KEY
  path         VARCHAR(255)          -- NULL for relocatable schemas
  relocatable  BOOLEAN
  keys_json    JSONB                 -- []GSettingsKey, protojson
  source       VARCHAR(10)           -- 'builtin' | 'agent'
  updated_at   TIMESTAMPTZ

node_dconf_schemas
  node_id    UUID -> nodes(id)                  ON DELETE CASCADE
  schema_id  VARCHAR(255) -> dconf_schemas      ON DELETE CASCADE
  PRIMARY KEY (node_id, schema_id)
```

Migrations: `000021_dconf_schema_catalogue` (both catalog tables),
`000022_compliance_results`, `000023_compliance_items` (`items_json`).

## File index

| Path | Purpose |
|---|---|
| `proto/policy/dconf.proto` | `DConfEntry`, `DConfPolicy`, `GSettingsSchema`, `GSettingsKey`, `GSettingsEnumValue`, `ReportSchemaCatalogueRequest` |
| `proto/policy/policy.proto` | `dconf_policy` (field 13 of `typed_content`), `ReportSchemaCatalogue` RPC, `ComplianceStatus`, `ComplianceItemResult` |
| `server/assets/dconf_builtin_schemas.json`, `server/assets/dconf.go` | built-in catalog and its `go:embed` wrapper |
| `server/pkg/app/dconf_seed.go` | seeds the built-in catalog at startup |
| `server/internal/database/dconf.go` | `DConfRepository`: catalog and compliance queries |
| `server/internal/grpc/dconf_catalogue.go` | `ReportSchemaCatalogue` handler |
| `server/internal/services/dconf.go` | `ValidateDConfPolicy`, `ParseDConfPolicyContent` |
| `server/internal/api/dconf_schemas.go` | `GET /api/v1/dconf/schemas`, `ComplianceHandler` for `GET /api/v1/compliance` |
| `agent/internal/policy/dconf.go` | merge, render, sync, compliance check, rollup |
| `agent/internal/policy/dconf_catalogue.go` | GSettings schema XML scanner |
| `agent/internal/policy/dconf_test.go` | unit tests |
| `agent/cmd/agent/main.go` | `dconfCache`, `syncAllDConf`, schema scan on first connect, watcher hooks |
| `agent/cmd/gen-dconf-schemas/main.go` | regenerates the built-in catalog |
| `server/web/frontend/src/apiClient/dconfApi.ts` | API client and types |
| `server/web/frontend/src/views/Policies/DConfPolicyEditor.tsx` | policy editor |
| `server/web/frontend/src/views/Compliance/CompliancePage.tsx` | compliance view with per-key rows |
