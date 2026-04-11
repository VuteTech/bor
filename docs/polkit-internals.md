# Polkit Internals

Developer reference for the polkit policy type. For end-user guidance see [polkit.md](polkit.md).

## System layout on the managed node

```
/etc/polkit-1/rules.d/
    010-bor-a1b2c3d4.rules   # one file per active polkit policy
    050-bor-e5f6a7b8.rules
    ...
```

Each active polkit policy gets its own rules file. The filename is `<priority>-bor-<shortID>.rules` where `shortID` is the first 8 hex characters of the policy UUID (without dashes) and `priority` is the binding priority zero-padded to three digits. polkitd evaluates files in alphabetical filename order (ascending), so a lower priority number means the file is loaded first and wins in first-match-wins evaluation.

When a policy is deleted or its binding priority changes, the agent removes the old file and (if the policy still exists) writes a new one at the updated path.

## Action catalogue

### Built-in tier

`server/assets/polkit_builtin_actions.json` is embedded in the server binary and seeded into the `polkit_actions` table at startup with `source = 'builtin'`. It contains well-known actions discovered from a reference installation and makes the policy editor action typeahead usable before any agent connects.

To regenerate it, run on a reference Linux system:

```bash
make update-polkit-actions
```

This calls `agent/cmd/gen-polkit-actions/main.go`, which runs `pkaction --verbose`, parses its output, and writes the result to `server/assets/polkit_builtin_actions.json`. This is a **manual step** — it is not run during `make agent` or `make server`. Run it when the action set on the reference system changes (e.g. after a desktop environment upgrade) and commit the result.

### Dynamic tier

At startup the agent calls `DiscoverPolkitActions` (`agent/internal/policy/polkit_catalogue.go`), which executes `pkaction --verbose` and parses each action block (action ID, description, message, vendor, default implicit results). The result is sent to the server via the `ReportPolkitCatalogue` gRPC call. The server upserts actions into `polkit_actions` and replaces the node's rows in `node_polkit_actions`.

Dynamic actions have `source = 'agent'`. Built-in actions are never downgraded by an agent report.

The `pkaction --verbose` output format is one block per action separated by blank lines:

```
org.freedesktop.NetworkManager.wifi.enable:
  description:       Enable or disable Wi-Fi
  message:           System policy prevents enabling or disabling Wi-Fi
  vendor:            NetworkManager
  vendor_url:        https://networkmanager.dev/
  icon:
  implicit any:      auth_admin
  implicit inactive: auth_admin
  implicit active:   yes
```

## Agent enforcement flow

`syncAllPolkit` in `agent/cmd/agent/main.go` runs whenever a Polkit policy is created, updated, deleted, or on initial snapshot:

1. **Compute desired paths** — for each entry in `polkitCache`, compute `PolkitRulesPath(e.priority, e.id)`. This is the file the policy should occupy.
2. **Discover stale files** — `ListBorManagedPolkitFiles()` scans `/etc/polkit-1/rules.d/` for all files whose first line contains `// This file is managed by Bor.`. Any such file not in the desired set is stale.
3. **Suppress + defer update** — `suppressManagedWrites(cfg, allPaths...)` tells the file watcher to ignore events on all involved paths for 2 seconds (preventing Bor's own writes from triggering a tamper-restore loop). `defer updateWatcher(cfg)` refreshes the watcher's set after the sync completes.
4. **Remove stale files** — stale files from deleted policies or old priorities are removed with `RemovePolkitRules`.
5. **Write per-policy** — for each active policy, `PolkitPoliciesToJS(e.policy)` renders the rules to JS and `SyncPolkitRules(rulesPath, js)` writes atomically via temp file + `os.Rename`. polkitd hot-reloads via inotify.
6. **Check per-policy** — `CheckPolkitCompliance(e.policy, rulesPath)` reads the file back and verifies the content byte-for-byte, then per-block if the file content differs. Each policy is checked independently against its own file.
7. **Report per-policy** — each policy calls `ReportComplianceWithStatus` independently with its own compliance items.

### File watcher integration

The polkit rules directory is monitored by the `filewatcher` package. If any bor-managed file is modified or removed externally (e.g. by a local admin), `onTamperedFile` is called, which:
1. Calls `syncAllPolkit` to restore all files to the correct state.
2. Calls `client.ReportTamperEvent` to record the event on the server with the PID and username of the modifying process.

The watcher suppresses events from Bor's own atomic writes using a 2-second suppress window around each `SyncPolkitRules` call.

### Why separate files instead of one merged file

Each policy in its own file means:
- **No merge ambiguity** — compliance is checked per-policy against its own file, not attributed from a merged slice.
- **Natural polkit semantics** — polkitd's load order is controlled by the binding priority directly, without any Bor-internal sort.
- **Clean deletion** — removing a policy binding deletes exactly one file; no re-merge of remaining policies is needed (they keep their own files).
- **Priority changes** — when a binding priority changes, only the affected policy's file is renamed (old file removed, new file written at the new priority-derived path).

### Generated JavaScript format

`PolkitPoliciesToJS` emits:

```js
// This file is managed by Bor. Do not edit manually.
// Changes will be overwritten by policy enforcement.
// Generated: 2026-04-11T10:00:00Z

// Allow developers to mount USB storage
polkit.addRule(function(action, subject) {
  if (
    action.id === "org.freedesktop.udisks2.filesystem-mount" &&
    subject.isInGroup("developers")
  ) {
    return polkit.Result.YES;
  }
});

// Deny USB storage mounting for everyone else
polkit.addRule(function(action, subject) {
  if (
    action.id === "org.freedesktop.udisks2.filesystem-mount"
  ) {
    return polkit.Result.NO;
  }
});
```

Each rule function returns a `polkit.Result.*` constant or falls through (`undefined`) to allow the next rule to match.

### writeRuleJS conditions

`writeRuleJS` (`agent/internal/policy/polkit.go`) builds the `if` condition from two groups:

**Action conditions** (OR-joined, any must match):
- `action.id === "..."` — exact action ID match
- `action.id.indexOf("...") === 0` — prefix match

**Subject conditions** (AND-joined, all must hold):
- `subject.isInGroup("...")` — user is in group
- `!subject.isInGroup("...")` — user is NOT in group (negate_group)
- `subject.user === "..."` — specific username
- `subject.local === true` — local console session
- `subject.active === true` — active session
- `subject.system_unit === "..."` — calling systemd service unit

All action conditions are emitted first, then subject conditions. The groups are separated by `&&`.

## Compliance result lifecycle

Compliance results are stored in `compliance_results` as a per-(node, policy) upsert. Per-rule results are stored in `items_json` as a JSON array with elements `{ schema_id: "polkit:<desc>", key: "rule_<N>", status, message }`.

`ListComplianceResults` filters with an `EXISTS` subquery against `policy_bindings` and `node_group_members`: only rows where there is still an enabled binding connecting the policy to a group containing the node are returned. Stale rows from removed or disabled bindings are excluded automatically without any cleanup job.

## Database schema

```
polkit_actions
  action_id        TEXT PK
  description      TEXT
  message          TEXT
  vendor           TEXT
  default_any      TEXT
  default_inactive TEXT
  default_active   TEXT
  source           TEXT NOT NULL DEFAULT 'agent'   — 'builtin' | 'agent'
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()

node_polkit_actions
  node_id    UUID FK → nodes(id)                ON DELETE CASCADE
  action_id  TEXT FK → polkit_actions(action_id) ON DELETE CASCADE
  PK (node_id, action_id)

compliance_results                    (shared with dconf)
  id          UUID PK DEFAULT gen_random_uuid()
  node_id     UUID FK → nodes  ON DELETE CASCADE
  policy_id   UUID FK → policies  ON DELETE CASCADE
  status      VARCHAR
  message     TEXT
  items_json  JSONB   — []{ schema_id: "polkit:<desc>", key: "rule_N", status, message }
  reported_at TIMESTAMPTZ
  UNIQUE (node_id, policy_id)
```

Migration: `server/internal/database/migrations/000024_polkit_actions.up.sql`.

## Proto

`proto/policy/polkit.proto`:

| Message | Fields |
|---|---|
| `PolkitPolicy` | `rules`, `file_prefix` |
| `PolkitRule` | `description`, `action_ids`, `action_prefixes`, `subject`, `result` |
| `PolkitSubjectFilter` | `in_group`, `negate_group`, `is_user`, `require_local`, `require_active`, `system_unit` |
| `PolkitResult` | enum: `NOT_SET`, `NO`, `YES`, `AUTH_SELF`, `AUTH_SELF_KEEP`, `AUTH_ADMIN`, `AUTH_ADMIN_KEEP` |
| `PolkitActionDescription` | `action_id`, `description`, `message`, `vendor`, `default_any`, `default_inactive`, `default_active` |
| `ReportPolkitCatalogueRequest` | `client_id`, `actions` |
| `ReportPolkitCatalogueResponse` | `success` |

`proto/policy/policy.proto` additions:
- `Policy.typed_content` oneof field `15` — `polkit_policy`
- `ReportPolkitCatalogue` RPC on `PolicyService`

## REST API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/polkit/actions` | List all known polkit actions; optional `?node_id=<uuid>` to filter to actions reported by a specific node |

Response: JSON array of `{ action_id, description, message, vendor, default_any, default_inactive, default_active }`.

The frontend `PolkitPolicyEditor` fetches this endpoint on mount to populate the action ID and prefix typeahead dropdowns.

## File index

| Path | Purpose |
|---|---|
| `proto/policy/polkit.proto` | Polkit policy and action catalogue proto messages |
| `proto/policy/policy.proto` | `polkit_policy` oneof field, `ReportPolkitCatalogue` RPC |
| `server/assets/polkit_builtin_actions.json` | Built-in action catalogue — commit after `make update-polkit-actions` |
| `server/assets/polkit.go` | `go:embed` wrapper for the built-in catalogue |
| `server/cmd/server/polkit_seed.go` | Seeds built-in actions at server startup |
| `server/internal/database/migrations/000024_polkit_actions.up.sql` | `polkit_actions`, `node_polkit_actions` tables |
| `server/internal/database/polkit.go` | `PolkitRepository` — `UpsertAction`, `ReplaceNodeActions`, `ListActions`, `ListActionsByNode` |
| `server/internal/grpc/polkit_catalogue.go` | `ReportPolkitCatalogue` gRPC handler |
| `server/internal/grpc/server.go` | `modelToProto` — `case "Polkit"` unmarshals `PolkitPolicy` |
| `server/internal/api/polkit_actions.go` | `GET /api/v1/polkit/actions` REST handler |
| `agent/internal/policy/polkit.go` | `PolkitRulesPath`, `PolkitPoliciesToJS`, `SyncPolkitRules`, `RemovePolkitRules`, `ListBorManagedPolkitFiles`, `CheckPolkitCompliance`, `RollupPolkitCompliance` |
| `agent/internal/policy/polkit_catalogue.go` | `DiscoverPolkitActions` — runs `pkaction --verbose` and parses output |
| `agent/internal/policy/polkit_test.go` | Enforcer unit tests |
| `agent/internal/policy/polkit_catalogue_test.go` | Catalogue parser unit tests |
| `agent/internal/policyclient/client.go` | `ReportPolkitCatalogue` RPC client method |
| `agent/cmd/agent/main.go` | `polkitCache`, `syncAllPolkit`, `getManagedPaths` (polkit), `onTamperedFile` (polkit case), catalogue goroutine |
| `agent/cmd/gen-polkit-actions/main.go` | CLI tool to regenerate the built-in action catalogue |
| `server/web/frontend/src/apiClient/polkitApi.ts` | TypeScript types, `fetchPolkitActions`, content helpers, JS preview generator |
| `server/web/frontend/src/views/Policies/PolkitPolicyEditor.tsx` | Policy editor with rule cards and action typeahead |
| `server/web/frontend/src/views/Compliance/CompliancePage.tsx` | Compliance dashboard — `polkit:` prefix handling in `ItemsTable` |
