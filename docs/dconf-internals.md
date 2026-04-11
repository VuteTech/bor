# dconf Internals

Developer reference for the dconf policy type. For end-user guidance see [dconf.md](dconf.md).

## System layout on the managed node

```
/etc/dconf/profile/user         # profile definition — must contain "system-db:local"
/etc/dconf/db/local.d/00-bor    # INI keyfile written by the agent
/etc/dconf/db/local.d/locks/bor # lock file — one dconf path per locked key
/etc/dconf/db/local             # compiled binary produced by `dconf update`
```

The agent writes to the `local` database by default. The database name is configurable per policy via the `db_name` field (maps to `/etc/dconf/db/<db_name>.d/`).

## Schema catalogue

### Built-in tier

`server/assets/dconf_builtin_schemas.json` is embedded in the server binary and seeded into the `dconf_schemas` table at startup with `source = 'builtin'`. It contains well-known schemas from `gsettings-desktop-schemas` and makes the policy editor usable before any agent connects.

To regenerate it, run on a reference GNOME system:

```bash
make update-dconf-schemas
```

This calls `agent/cmd/gen-dconf-schemas/main.go`, which parses the schema XML files under `/usr/share/glib-2.0/schemas/` and writes the JSON to `server/assets/dconf_builtin_schemas.json`. This is a **manual step** — it is not run during `make agent` or `make server`. Run it when `gsettings-desktop-schemas` is updated on the reference system and commit the result.

### Dynamic tier

At startup the agent calls `ScanGSettingsSchemasFrom` (`agent/internal/policy/dconf_catalogue.go`), which parses all `*.gschema.xml` files under `/usr/share/glib-2.0/schemas/`. The full metadata — key names, GVariant types, summaries, descriptions, defaults, enum values, ranges, choices — is sent to the server via the `ReportSchemaCatalogue` gRPC call. The server upserts the schemas into `dconf_schemas` and replaces the node's rows in `node_dconf_schemas`.

Dynamic schemas have `source = 'agent'`. Built-in schemas are never downgraded to `'agent'` by an agent report.

## Agent enforcement flow

`syncAllDConf` in `agent/cmd/agent/main.go` runs whenever a Dconf policy is created, updated, deleted, or on initial snapshot:

1. **Merge** — `MergeDConfPolicies` combines all active Dconf policies into one (last-writer-wins per `schema_id + path + key`). Policies are sorted ascending by binding priority before merging, so the highest-priority policy is processed last and wins on any key conflict. Priority is the `priority` field on the `Policy` proto message, set by the server to the maximum binding priority across all enabled bindings for that node.
2. **Render** — `DConfPolicyToFiles` converts the merged policy to an INI keyfile and a locks file. Section headers use slash-separated paths: `[org/gnome/desktop/screensaver]`.
3. **Write** — `SyncDConfFiles` writes `00-bor` and `bor` atomically under `/etc/dconf/db/<db_name>.d/`. Pre-existing unmanaged files are backed up. `/etc/dconf/profile/user` is checked and `system-db:<db_name>` is appended if missing.
4. **Compile** — `dconf update` is called to recompile the binary database and broadcast a D-Bus change notification.
5. **Check** — `CheckDConfCompliance` calls `gsettings get <schema> <key>` for each entry and compares to the policy value using `normalizeGVariant` (strips type prefixes so `uint16 5042` and `5042` compare equal).
6. **Report** — `RollupDConfCompliance` derives an overall status. Per-key results and the rollup are sent to the server via `ReportComplianceWithStatus`.

### GVariant type prefix handling

`gsettings get` always returns the canonical GVariant text including type prefixes (`uint16 5042`, `uint32 300`). Policy values for non-`int32` integer types must include the same prefix or `normalizeGVariant` will strip both sides before comparing. The policy editor formats values correctly; raw JSON edits must use the correct prefix.

Type prefix map used by `GVARIANT_INT_PREFIX` in the editor and `gvariantIntPrefixes` in the agent:

| GVariant type code | Prefix |
|---|---|
| `u` (uint32) | `uint32` |
| `q` (uint16) | `uint16` |
| `n` (int16) | `int16` |
| `t` (uint64) | `uint64` |
| `x` (int64) | `int64` |
| `y` (byte) | `byte` |
| `i` (int32) | *(none)* |

## Compliance result lifecycle

Compliance results are stored in `compliance_results` as a per-(node, policy) upsert. Per-key results are stored in `items_json` as a JSON array.

`ListComplianceResults` filters with an `EXISTS` subquery against `policy_bindings` and `node_group_members`: only rows where there is still an enabled binding connecting the policy to a group that contains the node are returned. Stale rows from removed or disabled bindings are excluded automatically without any cleanup job.

## Database schema

```
dconf_schemas
  schema_id   PK VARCHAR
  path        VARCHAR (NULL for relocatable schemas)
  relocatable BOOLEAN
  keys_json   JSONB      — []GSettingsKey in protojson encoding
  source      VARCHAR    — 'builtin' | 'agent'
  updated_at  TIMESTAMPTZ

node_dconf_schemas
  node_id     UUID FK → nodes  ON DELETE CASCADE
  schema_id   VARCHAR FK → dconf_schemas  ON DELETE CASCADE
  PK (node_id, schema_id)

compliance_results
  id          UUID PK DEFAULT gen_random_uuid()
  node_id     UUID FK → nodes  ON DELETE CASCADE
  policy_id   UUID FK → policies  ON DELETE CASCADE
  status      VARCHAR    — 'compliant' | 'non_compliant' | 'inapplicable' | 'error' | 'unknown'
  message     TEXT
  items_json  JSONB      — []{ schema_id, key, status, message }
  reported_at TIMESTAMPTZ
  UNIQUE (node_id, policy_id)
```

Migrations: `000021` (dconf_schemas + node_dconf_schemas), `000022` (compliance_results), `000023` (items_json column).

## Proto

`proto/policy/dconf.proto` — `DConfEntry`, `DConfPolicy`, `GSettingsKey`, `GSettingsSchema`, `GSettingsEnumValue`, `ReportSchemaCatalogueRequest/Response`.

`proto/policy/policy.proto` additions:
- `ComplianceItemResult` message (schema_id, key, status, message)
- `ComplianceStatus` enum (UNKNOWN, COMPLIANT, NON_COMPLIANT, INAPPLICABLE, ERROR)
- `ReportComplianceRequest.items` field 7 — per-key results
- `ReportSchemaCatalogue` RPC on `PolicyService`
- `Policy.typed_content` oneof field 13 — `dconf_policy`

## File index

| Path | Purpose |
|---|---|
| `proto/policy/dconf.proto` | DConf policy and schema catalogue proto messages |
| `proto/policy/policy.proto` | ComplianceStatus, ComplianceItemResult, ReportSchemaCatalogue RPC |
| `server/assets/dconf_builtin_schemas.json` | Built-in schema catalogue — commit after `make update-dconf-schemas` |
| `server/assets/dconf.go` | `go:embed` wrapper for the built-in catalogue |
| `server/cmd/server/dconf_seed.go` | Seeds built-in schemas at server startup |
| `server/internal/database/dconf.go` | DConfRepository — schema catalogue and compliance DB operations |
| `server/internal/database/migrations/000021_*` | `dconf_schemas`, `node_dconf_schemas` tables |
| `server/internal/database/migrations/000022_*` | `compliance_results` table |
| `server/internal/database/migrations/000023_*` | `items_json` column on `compliance_results` |
| `server/internal/grpc/dconf_catalogue.go` | `ReportSchemaCatalogue` gRPC handler |
| `server/internal/grpc/server.go` | `ReportCompliance` handler — marshals per-item results |
| `server/internal/services/dconf.go` | `ValidateDConfPolicy`, `ParseDConfPolicyContent` |
| `server/internal/api/dconf_schemas.go` | `GET /api/v1/dconf/schemas`, `GET /api/v1/compliance` |
| `agent/internal/policy/dconf.go` | Merge, keyfile render, sync, compliance check, rollup |
| `agent/internal/policy/dconf_catalogue.go` | GSettings XML schema scanner |
| `agent/internal/policy/dconf_test.go` | Enforcer and scanner unit tests |
| `agent/internal/policyclient/client.go` | `ReportSchemaCatalogue`, `ReportComplianceWithStatus` RPCs |
| `agent/cmd/agent/main.go` | dconf cache, `syncAllDConf`, startup schema scan |
| `agent/cmd/gen-dconf-schemas/main.go` | CLI tool to regenerate the built-in catalogue |
| `server/web/frontend/src/apiClient/dconfApi.ts` | TypeScript types and API client |
| `server/web/frontend/src/views/Policies/DConfPolicyEditor.tsx` | Policy editor component |
| `server/web/frontend/src/views/Compliance/CompliancePage.tsx` | Compliance dashboard with per-key expandable rows |
