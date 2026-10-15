# Polkit internals

Developer reference for the `Polkit` policy type: the action catalog that
feeds the editor, how the agent turns each policy into its own JavaScript
rules file, how binding priority maps to polkit's evaluation order, and how
compliance is checked. For administrator guidance see
[Polkit](../../policy-types/polkit.md).

## Files on the managed node

```text
/etc/polkit-1/rules.d/
    010-bor-a1b2c3d4.rules    one file per Polkit policy that applies to the node
    050-bor-e5f6a7b8.rules
```

`PolkitRulesPath(priority, policyID)` in `agent/internal/policy/polkit.go`
builds the name:

```go
return filepath.Join(PolkitRulesDir, fmt.Sprintf("%03d-bor-%s.rules", priority, shortID))
```

- `priority` is the policy's effective binding priority on this node
  (`Policy.priority`, the highest priority among the enabled bindings that
  reach the node), zero-padded to three digits.
- `shortID` is the policy ID with every non-alphanumeric character removed,
  cut to 8 characters (`unknown` if nothing is left). The ID comes from the
  server and is sanitized because it becomes part of a path.

## Priority: lower number wins

polkitd loads rules files in file-name order and evaluates the
`polkit.addRule` functions in that order; the first function that returns
a result decides. Because the file name starts with the binding priority,
**for Polkit a lower binding priority number is evaluated first and wins**.

This is the opposite of the merge-based types. In Dconf, Flatpak and the
other merged types the highest priority wins, because entries are merged in
ascending priority order and the last writer wins. Keep this difference in
mind when changing either side; the user documentation explains it to
administrators.

Further consequences of the naming:

- Priorities outside 0 to 999 do not produce three-digit names (`%03d`
  pads but does not truncate, and negative numbers keep their sign), so
  they no longer sort numerically.
- Two policies with the same priority are ordered by their short IDs.
- polkitd also loads files from its other rules directory
  (`/usr/share/polkit-1/rules.d`), sorted together with these by name.

`PolkitPolicy.file_prefix` in the proto is not used for the file name.
`MergePolkitPolicies` (an older single-file merge helper, still covered by
tests) carries it, but `syncAllPolkit` does not call it, and the editor
does not offer the field (see the comment in `PolkitPolicyEditor.tsx`).

## Policy content

`proto/policy/polkit.proto`:

| Message | Fields |
|---|---|
| `PolkitPolicy` | `rules`, `file_prefix` (unused, see above) |
| `PolkitRule` | `description`, `action_ids`, `action_prefixes`, `subject`, `result`, `action_conditions` |
| `PolkitSubjectFilter` | `in_group`, `negate_group`, `is_user`, `require_local`, `require_active`, `system_unit` |
| `PolkitActionCondition` | `key`, `value`, `negate` |
| `PolkitResult` | `NOT_SET`, `NO`, `YES`, `AUTH_SELF`, `AUTH_SELF_KEEP`, `AUTH_ADMIN`, `AUTH_ADMIN_KEEP` (prefixed `POLKIT_RESULT_`) |
| `PolkitActionDescription` | `action_id`, `description`, `message`, `vendor`, `default_any`, `default_inactive`, `default_active` |
| `ReportPolkitCatalogueRequest` / `Response` | `client_id`, `actions` / `success` |

The policy is `polkit_policy`, field 15 of the `typed_content` oneof in
`proto/policy/policy.proto`. The editor stores snake_case field names, and
`Polkit` is listed in `protoNameTypes` in
`server/internal/export/export.go`.

The server has no type-specific validator for Polkit content:
`validatePolicyContent` in `server/internal/services/policy.go` has no
`Polkit` case, so any content is accepted. Structural problems surface on
the agent, where `writeRuleJS` refuses a rule without action IDs or
prefixes, or with result `NOT_SET`, and the policy is reported with status
`ERROR` ("failed to generate rules file").

## Generated JavaScript

`PolkitPoliciesToJS` writes a header followed by one `polkit.addRule` call
per rule, in the order of the policy's rules:

```js
// This file is managed by Bor. Do not edit manually.
// Changes will be overwritten by policy enforcement.
// Generated: 2026-04-11T10:00:00Z
// Allow developers to mount removable drives
polkit.addRule(function(action, subject) {
  if (
    action.id === "org.freedesktop.udisks2.filesystem-mount" &&
    subject.isInGroup("developers")
  ) {
    if (action.lookup("drive.removable") !== "true") { return; }
    return polkit.Result.YES;
  }
});
```

`writeRuleJS` builds the condition:

- **Action match (any of)** - `action.id === "<id>"` for each entry in
  `action_ids` and `action.id.indexOf("<prefix>") === 0` for each entry in
  `action_prefixes`. Several alternatives are wrapped in parentheses and
  joined with `||`.
- **Subject filter (all of)** - `subject.isInGroup("<group>")` (or
  `!subject.isInGroup(...)` with `negate_group`), `subject.user ===
  "<user>"`, `subject.local === true`, `subject.active === true`,
  `subject.system_unit === "<unit>"`, for each field that is set.
- The action match and the subject conditions are joined with `&&`.
- **Action conditions** - inside the `if`, each `action_conditions` entry
  becomes an early `return;` when `action.lookup(key)` does not equal
  `value` (or equals it, with `negate`). A rule that returns nothing lets
  polkitd continue with the next rule.

Strings are quoted with Go's `%q`, so quotes and backslashes in the policy
cannot break out of the JavaScript string literals.

## Enforcement on the agent

`syncAllPolkit` in `agent/cmd/agent/main.go` runs after every change to the
set of Polkit policies and when a managed rules file is tampered with:

1. **Desired paths** - `PolkitRulesPath(e.priority, e.id)` for every
   cached policy.
2. **Existing files** - `ListBorManagedPolkitFiles()` lists the `*.rules`
   files in `/etc/polkit-1/rules.d` whose content starts with
   `// This file is managed by Bor.`.
3. **Suppress** - `suppressManagedWrites(cfg, desired + existing...)` and
   `defer updateWatcher(cfg)`, so the agent's own writes and deletions do
   not trigger a restore.
4. **Remove stale files** - every managed file that is not a desired path
   is removed (`RemovePolkitRules`). This covers deleted or unbound
   policies and priority changes, which move a policy's file to a new
   name. With no Polkit policies left, every managed file is removed.
5. **Write** - for each policy, `PolkitPoliciesToJS` renders the file and
   `SyncPolkitRules` writes it through a temporary file in the same
   directory, `chmod 0644` and `rename`. polkitd notices the change itself;
   no reload is needed.
6. **Check and report** - per policy, see below.

Keeping one file per policy means each policy's compliance is checked
against its own file, deleting a policy removes exactly one file, and the
evaluation order is polkitd's own file order rather than a merge inside
Bor.

### Tamper protection

`getManagedPaths` includes every file returned by
`ListBorManagedPolkitFiles()`, and `onTamperedFile` maps any path under
`/etc/polkit-1/rules.d/` to `syncAllPolkit`, then reports the tamper event
with the processes that held the file. A managed file that is edited or
deleted is rewritten, and a stale managed file is removed again.

## Compliance

`CheckPolkitCompliance(policy, rulesPath)` reads the file back:

- File missing or unreadable: every rule is `NON_COMPLIANT`.
- File identical to a freshly generated rendering: every rule is
  `COMPLIANT`. Because the header carries a generation timestamp, this
  exact match is rare in practice, and the next check is the usual path.
- Otherwise, each rule's own `polkit.addRule` block is rendered and
  searched for in the file: found is `COMPLIANT`, missing or modified is
  `NON_COMPLIANT`.
- If the policy cannot be rendered at all, every rule is `ERROR`.

`RollupPolkitCompliance` returns `ERROR` if any rule has an error,
otherwise `NON_COMPLIANT` if any rule is non-compliant, otherwise
`COMPLIANT` (`UNKNOWN` for a policy without rules). The agent reports each
policy with one item per rule:

```text
schema_id: "polkit:<first 40 characters of the rule description, or 'rule'>"
key:       "rule_<index>"
```

The **Compliance** page recognizes the `polkit:` prefix and shows the rule
description instead of a schema ID. The check confirms the file content,
not that polkitd loaded it without errors.

## Action catalog

The editor's action ID and prefix fields offer suggestions from a catalog
of polkit actions stored on the server, in two tiers like the dconf schema
catalog.

### Built-in tier

`server/assets/polkit_builtin_actions.json`, embedded through
`server/assets/polkit.go`, is upserted into `polkit_actions` with
`source = 'builtin'` at every server startup by `seedPolkitBuiltinActions`
(`server/pkg/app/polkit_seed.go`, run in a goroutine). Regenerate it on a
reference system with polkit installed:

```bash
make update-polkit-actions
```

This runs `agent/cmd/gen-polkit-actions`, which calls `pkaction --verbose`
and writes the JSON. It is a manual step; commit the result.

### Agent tier

On the first stream connection after startup (retried on later
connections until it succeeds), the agent runs `DiscoverPolkitActions`
(`agent/internal/policy/polkit_catalogue.go`), which executes
`pkaction --verbose` and parses one block per action:

```text
org.freedesktop.NetworkManager.network.enable:
  description:       Enable or disable a network
  message:           System policy prevents enabling or disabling networks
  vendor:            NetworkManager
  vendor_url:        http://networkmanager.freedesktop.org/
  icon:
  implicit any:      auth_admin
  implicit inactive: auth_admin
  implicit active:   auth_admin
```

It sends the result with the `ReportPolkitCatalogue` RPC. The handler in
`server/internal/grpc/polkit_catalogue.go` upserts each action with
`source = 'agent'` and replaces the node's rows in `node_polkit_actions`.
As for dconf, an agent report never changes a `builtin` row's source.
Discovery is best-effort; a node without `pkaction` simply reports
nothing.

### REST API

| Method | Path | Permission | Description |
|---|---|---|---|
| `GET` | `/api/v1/polkit/actions` | `policy:view` | All known actions; `?node_id=<uuid>` limits the list to actions reported by that node |

Response: a JSON array of `{action_id, description, message, vendor,
default_any, default_inactive, default_active}`. The handler is
`PolkitHandler.ListActions` in `server/internal/api/polkit_actions.go`; the
editor loads it through `fetchPolkitActions` in
`server/web/frontend/src/apiClient/polkitApi.ts`, which also contains the
JavaScript preview generator used by the editor.

## Database schema

Migration `000024_polkit_actions`:

```text
polkit_actions
  action_id         TEXT PRIMARY KEY
  description       TEXT
  message           TEXT
  vendor            TEXT
  default_any       TEXT
  default_inactive  TEXT
  default_active    TEXT
  source            TEXT NOT NULL DEFAULT 'agent'   -- 'builtin' | 'agent'
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()

node_polkit_actions
  node_id    UUID -> nodes(id)                     ON DELETE CASCADE
  action_id  TEXT -> polkit_actions(action_id)    ON DELETE CASCADE
  PRIMARY KEY (node_id, action_id)
```

Compliance results go to the shared `compliance_results` table described
in [dconf internals](dconf.md#compliance-storage).

## File index

| Path | Purpose |
|---|---|
| `proto/policy/polkit.proto` | policy and action catalog messages |
| `proto/policy/policy.proto` | `polkit_policy` oneof field, `ReportPolkitCatalogue` RPC |
| `server/assets/polkit_builtin_actions.json`, `server/assets/polkit.go` | built-in catalog and its `go:embed` wrapper |
| `server/pkg/app/polkit_seed.go` | seeds the built-in catalog at startup |
| `server/internal/database/polkit.go` | `PolkitRepository`: `UpsertAction`, `ReplaceNodeActions`, `ListActions`, `ListActionsByNode` |
| `server/internal/grpc/polkit_catalogue.go` | `ReportPolkitCatalogue` handler |
| `server/internal/grpc/server.go` | `modelToProto`: `case "Polkit"` |
| `server/internal/api/polkit_actions.go` | `GET /api/v1/polkit/actions` |
| `agent/internal/policy/polkit.go` | `PolkitRulesPath`, `PolkitPoliciesToJS`, `SyncPolkitRules`, `RemovePolkitRules`, `ListBorManagedPolkitFiles`, `CheckPolkitCompliance`, `RollupPolkitCompliance` |
| `agent/internal/policy/polkit_catalogue.go` | `DiscoverPolkitActions` |
| `agent/internal/policy/polkit_test.go`, `polkit_catalogue_test.go` | unit tests |
| `agent/cmd/agent/main.go` | `polkitCache`, `syncAllPolkit`, catalog report, watcher hooks |
| `agent/cmd/gen-polkit-actions/main.go` | regenerates the built-in catalog |
| `server/web/frontend/src/apiClient/polkitApi.ts` | API client, content helpers, JavaScript preview |
| `server/web/frontend/src/views/Policies/PolkitPolicyEditor.tsx` | policy editor |
| `server/web/frontend/src/views/Compliance/CompliancePage.tsx` | compliance view (`polkit:` items) |
