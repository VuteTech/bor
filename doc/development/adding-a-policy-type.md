# Adding a policy type

A policy type touches every layer of Bor: the protobuf schema, the server's
validation and gRPC conversion, the web UI's registry and editor, the
agent's cache and enforcement code, tamper protection, tests and the user
documentation. There is no plugin mechanism; each layer has a `switch` or a
map keyed by the type's wire name, and a new type adds one entry to each.

This page is the checklist. It follows how the existing types are wired,
using **Session Access** (`SessionAccess`, one rule set compiled on the
node) and **Flatpak** (`Flatpak`, file-based with a long-running sync) as
the reference implementations. The placeholder type in the examples is
`Widget`.

```mermaid
--8<-- "diagrams/dev-policy-type-touchpoints.mmd"
```

## Before you start

Decide three things, because they shape the rest of the work:

The wire name
:   The string stored in `Policy.type`, in PascalCase without spaces
    (`SessionAccess`, `DiskEncryption`). It is the key in every `switch`
    below and it never changes once released.

How several policies combine
:   Every type receives all bound policies of that type at once and must
    produce one result. The existing types either merge by binding priority
    (Dconf, Flatpak), write one file per policy (Polkit, Firewalld), or
    compile a union (Session Access). The agent receives each policy's
    effective binding priority in `Policy.priority` (the highest priority
    among the enabled bindings that reach the node).

Whether it writes files
:   A type that writes files to disk **must** integrate with the file
    watcher (see [Step 8](#step-8-tamper-protection-file-based-types)).
    That is not optional.

## Step 1: the protobuf message

Create `proto/policy/widget.proto` in package `bor.policy.v1`:

```proto
syntax = "proto3";

package bor.policy.v1;

option go_package = "github.com/VuteTech/Bor/server/pkg/grpc/policy;policy";

// WidgetPolicy ... (document every field: the comments become editor help)
message WidgetPolicy {
  repeated WidgetRule rules = 1;
}
```

Then add it to `proto/policy/policy.proto`:

1. `import "widget.proto";` next to the other imports.
2. A new member of the `typed_content` oneof in `message Policy`, with the
   next free field number (the last one in use is
   `SystemdPolicy systemd_policy = 23`):

    ```proto
    oneof typed_content {
      ...
      SystemdPolicy        systemd_policy         = 23;
      WidgetPolicy         widget_policy          = 24;
    }
    ```

Field numbers are permanent. If you reserve numbers for later, say so in a
comment, as `SessionAccessRule` does for fields 7 to 15.

### UI annotations (tree-editor types only)

Types whose settings are a long, flat list of keys (Firefox, Thunderbird,
Chrome, Edge) annotate each field with `(bor.ui.field)` from
`proto/bor/ui.proto`, and the generic tree editor renders them:

```proto
import "ui.proto";

message WidgetPolicy {
  optional bool DisableThing = 1 [json_name = "DisableThing", (bor.ui.field) = {group: "General"}];
}
```

`FieldUi` carries `group` (the tree section, required on top-level fields),
`label` (otherwise derived from the field name), `description` (otherwise
the field's leading comment), `int_options` or `string_options` for enums,
and `chrome_only`. Rule- or entry-based types do not need annotations.

### Generate the code

```bash
make install-deps   # once: protoc-gen-go, protoc-gen-go-grpc, protoc-gen-bor-ui
cd server/web/frontend && npm install && cd -   # ts-proto for proto-ts
make proto
```

`make proto` runs three targets and the output of all three is committed:

| Target | Output |
|---|---|
| `proto-go` | `server/pkg/grpc/policy/widget.pb.go` and the updated `policy.pb.go` |
| `proto-ts` | `server/web/frontend/src/generated/proto/widget.ts` (message types, `ts-proto` with `onlyTypes=true`) |
| `proto-ui` | `server/web/frontend/src/generated/proto/widget_ui.ts` (`WIDGET_ALL_POLICIES`, empty unless annotated) |

The agent module imports the server module through a `replace` directive
(`github.com/VuteTech/Bor/server => ../server`), so the agent sees the new
Go types without further steps.

## Step 2: content storage and server validation

Policy content is stored as a JSON string in `Policy.content` (the
`policies.content` column) and is the protojson encoding of the type's
message. The web UI writes it, the REST API stores it, and the server
parses it into the typed message for the agent.

Add `server/internal/services/widget.go` with the validator:

```go
// ValidateWidgetContent validates widget policy JSON content.
func ValidateWidgetContent(content string) error {
	if strings.TrimSpace(content) == "" || content == "{}" {
		return fmt.Errorf("widget policy content is empty")
	}
	var pol pb.WidgetPolicy
	if err := protojson.Unmarshal([]byte(content), &pol); err != nil {
		return fmt.Errorf("invalid widget policy: %w", err)
	}
	if len(pol.GetRules()) == 0 {
		return fmt.Errorf("widget policy needs at least one rule")
	}
	// Check every value the agent will use: lengths, ranges, names that
	// become file names or command arguments, duplicates, limits.
	return nil
}
```

and register it in `validatePolicyContent()` in
`server/internal/services/policy.go`:

```go
case "Widget":
	return ValidateWidgetContent(content)
```

Rules for the validator:

- The server never accepts an empty policy; require at least one setting.
- Validate everything that reaches the endpoint. Values that end up in a
  path, a file name, a shell-free command argument or a generated script
  must be checked here **and** again on the agent (the agent treats server
  data as untrusted too, see `isValidPathIdentifier` in the dconf code).
- Return errors with context; the message is shown to the administrator.

`validatePolicyContent()` is also used by policy import
(`ValidatePolicyContentByType`), so the same rules apply to imported
policies. Unknown types are accepted for forward compatibility, which is
why forgetting this step silently disables validation.

## Step 3: server registration

Two more `switch` statements on the server know the types:

`server/internal/grpc/server.go`, `modelToProto()`
:   Converts the stored JSON into `typed_content` for the policy stream.
    Without this case the agent receives the policy with no typed content.

    ```go
    case "Widget":
    	var wPol pb.WidgetPolicy
    	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(p.Content), &wPol); err != nil {
    		log.Printf("WARNING: failed to unmarshal Widget typed_content for policy %s: %v", p.ID, err)
    	} else {
    		pol.TypedContent = &pb.Policy_WidgetPolicy{WidgetPolicy: &wPol}
    	}
    ```

`server/internal/export/export.go`, `newContentMessage()`
:   Export and import round-trip content through the message; import
    rejects types this function does not know.

    ```go
    case "Widget":
    	return &pb.WidgetPolicy{}
    ```

    If the editor stores original snake_case field names instead of the
    protojson default (camelCase or `json_name`), also add the type to
    `protoNameTypes` in the same file, as Polkit and Dconf do. Both
    spellings parse; the map only keeps exported documents identical to
    what the editor writes.

Permissions need no change: all policy types share the `policy` resource.

## Step 4: the web UI registry

Add the type to `server/web/frontend/src/policyTypes/registry.tsx`:

1. Add `"Widget"` to the `PolicyTypeId` union.
2. Add an entry to `POLICY_TYPES`. Every field is used somewhere:

    ```tsx
    Widget: {
      id: "Widget",
      label: "Widgets",                 // shown everywhere instead of the id
      technicalName: "widget",          // optional subtitle when it differs from the label
      category: "system",               // "browsers" | "desktop" | "system"
      tagline: "One line for the wizard tile",
      description: "Two or three sentences for the wizard description panel.",
      manages: ["/etc/widget/bor.conf"], // what the agent writes or controls
      appliesTo: ["Nodes with widgetd installed"],
      examples: ["...", "...", "..."],
      docsHref: `${DOCS_BASE}/widget.md`,
      Icon: CubesIcon,
      defaultContent: () => pretty({ rules: [] }),
      validateContent: requireNonEmptyList("Widget", "rules", "Add at least one rule before saving"),
    },
    ```

3. Add `"Widget"` to `POLICY_TYPE_ORDER` at the place it should appear
   within its category.

The wizard tile and description panel, the policies list **Type** column
and filter, the dashboard and the editor all read the registry, so these
steps cover the create wizard's tile and description. `docsHref` points to
`doc/policy-types/widget.md` on GitHub through `DOCS_BASE`; the description
panel shows a documentation link only when it is set, so set it once the
page from [Step 10](#step-10-user-documentation) exists.

`validateContent` mirrors the server's structural checks so the user sees
the problem before a round trip; the helpers `requireAnyKey` and
`requireNonEmptyList` cover the common cases, and `validateSessionAccess`
shows a type-specific one. The server stays authoritative.

For the icon, use a PatternFly icon or add a flat single-color mark under
`src/icons/policyTypes/` and record its source and licence in that
directory's `README.md`.

## Step 5: the editor

Add a `case` to `PolicyContentEditor`
(`server/web/frontend/src/views/Policies/editors/PolicyContentEditor.tsx`).
That component is used by both the policy editor page and the create
wizard, so one case covers both.

Generic tree editor
:   For an annotated, flat key list, reuse `TreePolicyEditor` with the
    generated field list:

    ```tsx
    case "Widget":
      return (
        <TreePolicyEditor
          fields={WIDGET_ALL_POLICIES}
          productLabel="Widget"
          idPrefix="wg"
          ariaLabel="Widget policy settings"
          {...rest}
        />
      );
    ```

    Also add the type to `TREE_TYPES` in
    `views/Policies/wizard/WizardHelpDrawer.tsx` so the wizard shows the
    tree-specific hint.

Custom editor
:   For rules, entries or anything with structure, write
    `views/Policies/WidgetPolicyEditor.tsx` (see
    `SessionAccessPolicyEditor.tsx` or `FlatpakPolicyEditor.tsx`):

    ```tsx
    case "Widget":
      return <WidgetPolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
    ```

    The editor parses `contentRaw`, pushes every change up through
    `onChange` as a JSON string, honors `isDisabled` (only draft policies are
    editable), and reports unsaveable intermediate states through
    `onValidityChange` if it has any. Build it from the shared primitives:
    `ListEditor` for repeatable rows, `PolicyTreePanel` for a tree,
    `LiveAlert` for errors, and follow the accessibility rules in
    [Frontend](frontend.md#accessibility).

Then add a branch to `buildSettingsRows()` in
`views/Policies/policySummary.ts`. It renders the read-only settings table
on the policy editor and on the wizard's **Review** step; without it the
type shows no summary.

Rebuild and typecheck the frontend, and commit the rebuilt bundle in
`server/web/static/` with the source (see [Frontend](frontend.md#building)).

## Step 6: agent enforcement package

Put the enforcement logic in `agent/internal/policy/widget.go`, separate
from the streaming code so it can be unit tested. Most types expose the
same shape of API (compare `polkit.go`, `dconf.go`, `flatpak.go`):

| Function | Purpose |
|---|---|
| `MergeWidgetPolicies(...)` or a compile step | combine all bound policies, honoring priority |
| `WidgetToFiles(...)` / render | produce the bytes to write, deterministically |
| `SyncWidgetFiles(...)` | write atomically (`WriteFileAtomically`), back up unmanaged originals (`BackupOriginal`), run any reload command |
| `ListBorManagedWidgetFiles()` | find files Bor wrote, so stale ones can be removed |
| `CheckWidgetCompliance(...)` | read back the real state and compare it with the policy |
| `RollupWidgetCompliance(...)` | derive one status and message from the per-item results |

Conventions the existing types follow:

- Write a "managed by Bor" header into generated files where the format
  allows comments, and use it (or a `bor-` file name prefix) to recognize
  Bor's files.
- Never trust server values as path components; validate them again.
- If the software the type manages is missing on the node, report
  `COMPLIANCE_STATUS_INAPPLICABLE` instead of failing.
- If the agent needs configurable paths, add them to
  `agent/internal/config/config.go` with defaults and document them in
  `agent/config.yaml.example`.

## Step 7: agent wiring in `main.go`

`agent/cmd/agent/main.go` holds the per-type caches and the stream
handler. Add, following the Session Access and Flatpak code:

1. **Typed field on `PolicyInfo`** in
   `agent/internal/policyclient/client.go`, filled from the oneof:

    ```go
    WidgetPolicy *pb.WidgetPolicy // populated from typed_content for Widget type
    ...
    if wp := p.GetWidgetPolicy(); wp != nil {
    	pi.WidgetPolicy = wp
    }
    ```

2. **Cache and snapshot staging** variables:

    ```go
    type widgetCacheEntry struct {
    	id       string
    	name     string
    	priority int32
    	policy   *pb.WidgetPolicy
    }

    var widgetCache = make(map[string]widgetCacheEntry)
    var widgetSnapshotStaging map[string]widgetCacheEntry
    ```

    If the cache is touched from more than one goroutine (a timer, a
    watchdog, an async sync), guard it with a mutex as
    `sessionAccessMu` does.

3. **`handlePolicyUpdate()`**, in four places:
    - the empty-snapshot branch: reset the cache and staging and call the
      sync, so a node with no policies left cleans up;
    - the snapshot accumulation `switch`: add the policy to the staging map;
    - the snapshot-complete block: swap staging into the cache and call
      the sync;
    - the `CREATED`/`UPDATED` `switch` and the `DELETED` `else if` chain:
      update or delete the cache entry and call the sync.

    A type missing from the accumulation `switch` falls into `default`,
    which reports "unsupported policy type" as non-compliant.

4. **The sync function**, `syncAllWidget(ctx, client, cfg)`. It always
   recomputes the result from the whole cache, never from the single
   policy that changed, so the outcome does not depend on event order:

    ```go
    func syncAllWidget(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
    	// 1. Compute desired paths and discover existing managed files.
    	desired := ...
    	existing, _ := policy.ListBorManagedWidgetFiles()

    	// 2. Suppress the watcher for everything this sync touches.
    	suppressManagedWrites(cfg, append(desired, existing...)...)
    	defer updateWatcher(cfg)

    	// 3. Remove stale files. With an empty cache this removes everything:
    	//    that is how unbinding cleans up.
    	// 4. Write new or changed files.
    	// 5. Check compliance and report it for every policy in the cache.
    }
    ```

    If the work can take long (package installs, downloads, disk
    operations), do not block the stream goroutine: wrap it as
    `triggerFlatpakSync()` and `triggerPackageSync()` do, with a mutex that
    serializes runs and a `go trigger...()` call at every call site.

### Cleanup when a policy no longer applies

Policy changes (edits, state changes such as release or archive,
deprecation) and binding changes (enabling, disabling, a new priority,
deleting an enabled binding) reach the agent as a resync: the
server publishes a resync signal (`PolicyHub.PublishResync`, wired in
`server/pkg/app/app.go`) and sends the agents of the affected node groups a
fresh snapshot. A full snapshot is also sent when an agent reconnects too
far behind.
A policy that no longer applies is simply absent from the snapshot, so the
snapshot-complete swap leaves it out of the cache and `syncAllWidget` must
undo its effects. Test the empty-cache path explicitly: it must remove
every managed file and revert any other change (services, PAM, remotes)
the type made.

### Compliance reporting

Report through `client.ReportComplianceWithStatus(ctx, policyID, status,
message, items)` once per policy after every sync, including after a
tamper restore. `status` is one of `COMPLIANCE_STATUS_COMPLIANT`,
`NON_COMPLIANT`, `INAPPLICABLE`, `ERROR` (or `UNKNOWN`). `items` is an
optional list of `ComplianceItemResult{SchemaId, Key, Status, Message}`
shown as expandable per-item rows on the **Compliance** page. When several
policies are merged, report each policy against its own intended values;
Dconf marks keys overridden by a higher-priority policy as inapplicable
with an explanatory message. The helper `rollupProtoItems()` derives a
policy's overall status from its items.

### User notifications

If users must restart an application or log in again for the change to
take effect, schedule a desktop notification after a sync that changed
something. The browser and KDE types use a `notify.Notifier` with a
`notify.Config`, filled from `GetAgentConfig` on every connect:

```go
if syncAllFirefox(ctx, client, cfg) {
	firefoxNotifier.ScheduleNotification(firefoxNotifyConfig, map[string]bool{"policies.json": true})
}
```

A per-type message needs a new field in `AgentConfig`
(`proto/policy/policy.proto`), a new key in the agent settings
(`server/internal/database/settings.go`, `server/internal/services/settings.go`,
`server/internal/models/models.go`), `GetAgentConfig` in
`server/internal/grpc/server.go`, and a field on the **Agent
Notifications** settings tab (`views/Settings/AgentNotificationsTab.tsx`).
Types that apply without user action need none of this.

## Step 8: tamper protection (file-based types)

Every type that writes files must let the file watcher detect and undo
external changes; otherwise a local administrator can override the policy
by editing the file. Wire four hooks in `agent/cmd/agent/main.go`:

`getManagedPaths(cfg)`
:   Return the absolute paths this type currently manages. Return paths
    only while the cache is non-empty or the files exist, so nothing is
    watched after cleanup:

    ```go
    if len(widgetCache) > 0 {
    	if files, err := policy.ListBorManagedWidgetFiles(); err == nil {
    		paths = append(paths, files...)
    	}
    }
    ```

`suppressManagedWrites(cfg, extraPaths...)`
:   Call at the top of `syncAllWidget` with every path about to be written
    or removed (desired and stale). It silences watcher events on those
    paths for two seconds so the agent's own writes do not start a restore
    loop.

`defer updateWatcher(cfg)`
:   Call right after `suppressManagedWrites`. It refreshes the watched set
    from `getManagedPaths` after the sync, so new files are watched and
    removed files are not.

`onTamperedFile`
:   Add a `case` that recognizes this type's paths and re-runs the sync:

    ```go
    case strings.HasPrefix(path, policy.WidgetDir+string(filepath.Separator)):
    	syncAllWidget(ctx, client, cfg)
    ```

    `onTamperedFile` then reports the event to the server with the
    processes holding the file. The re-run sync reports compliance again.

Watch only the files Bor renders. Flatpak, for example, watches its
`/etc/bor/flatpak/` definitions but deliberately not
`/var/lib/flatpak/repo/config`, which Flatpak itself rewrites.

## Step 9: tests

Server
:   `server/internal/services/widget_test.go`: table-driven tests for
    `ValidateWidgetContent` with named valid and invalid cases, as in
    `session_access_test.go`. Cover empty content, malformed JSON, every
    limit and every value the agent will use as a path or argument.

Agent
:   `agent/internal/policy/widget_test.go`: table-driven tests for merge
    (priority order, overrides), rendering (exact bytes), stale-file
    detection, compliance checks and rollup. Make paths injectable (a
    directory argument or a `...From(dir)` variant, as
    `ScanGSettingsSchemasFrom` does) so tests run in `t.TempDir()` without
    root.

Run `make test` and `make lint`; both must pass with no issues. See
[Testing and linting](testing.md).

## Step 10: user documentation

1. Write `doc/policy-types/widget.md`: what the type manages, the editor,
   what the agent writes on the node, prerequisites, how merging works,
   how to verify and troubleshoot. Follow
   [Writing documentation](documentation.md).
2. Add the page to the **Policy types** section of the `nav` in
   `mkdocs.yml` and a row to the overview table in
   `doc/policy-types/index.md` (including the "Where it writes on the
   endpoint" and "Endpoint prerequisites" columns).
3. Set `docsHref` in the registry (Step 4).
4. Add the new paths to `doc/reference/files-and-ports.md` if the type
   writes files.

### Demo data and screenshots

- Add a realistic example policy to the demo kit in `demo/seed/` as
  `policy-NN-<name>.json` (`name`, `type`, `state`, `description` and
  `content` as a JSON object), and bind it in `bindings.json` if it should
  be active. The documentation screenshots are taken from this data.
- Add the type to `EDITOR_SHOTS` in `doc/tools/screenshots/doc-shoot.js`:

    ```js
    const EDITOR_SHOTS = {
      ...
      Widget: "policy-editor-widget",
    };
    ```

    The script copies a released policy of the type into a temporary draft
    so the editor renders editable; if the demo kit has none, add a
    built-in example the way `flatpakExample()` does. Then capture only the
    new image and embed it in the type's page:

    ```bash
    cd doc/tools/screenshots
    SHOTS=policy-editor-widget node doc-shoot.js admin '<password>'
    ```

## Summary: file -> what to add

| File | What to add |
|---|---|
| `proto/policy/widget.proto` | `WidgetPolicy` and sub-messages; `(bor.ui.field)` annotations for tree types |
| `proto/policy/policy.proto` | `import "widget.proto"`; `widget_policy` in the `typed_content` oneof; `AgentConfig` field if it notifies users |
| `server/pkg/grpc/policy/`, `server/web/frontend/src/generated/proto/` | regenerated by `make proto` (committed) |
| `server/internal/services/widget.go` | `ValidateWidgetContent` |
| `server/internal/services/policy.go` | `case "Widget"` in `validatePolicyContent()` |
| `server/internal/grpc/server.go` | `case "Widget"` in `modelToProto()` |
| `server/internal/export/export.go` | `case "Widget"` in `newContentMessage()`; `protoNameTypes` if snake_case |
| `server/internal/services/widget_test.go` | table-driven validator tests |
| `server/web/frontend/src/policyTypes/registry.tsx` | `PolicyTypeId`, `POLICY_TYPES` entry (wizard copy, `docsHref`, icon, default content, validation), `POLICY_TYPE_ORDER` |
| `server/web/frontend/src/views/Policies/editors/PolicyContentEditor.tsx` | `case "Widget"` (tree editor or custom editor) |
| `server/web/frontend/src/views/Policies/WidgetPolicyEditor.tsx` | custom editor, if not tree-based |
| `server/web/frontend/src/views/Policies/wizard/WizardHelpDrawer.tsx` | `TREE_TYPES` entry, tree editors only |
| `server/web/frontend/src/views/Policies/policySummary.ts` | `buildSettingsRows()` branch |
| `server/web/static/` | rebuilt bundle (committed) |
| `agent/internal/policy/widget.go` | merge, render, sync, list managed files, compliance check, rollup |
| `agent/internal/policy/widget_test.go` | table-driven enforcement tests |
| `agent/internal/policyclient/client.go` | `PolicyInfo.WidgetPolicy` and the oneof extraction |
| `agent/internal/config/config.go`, `agent/config.yaml.example` | configurable paths, if any |
| `agent/cmd/agent/main.go` | cache and staging, `handlePolicyUpdate` cases (empty snapshot, accumulate, swap, update, delete), `syncAllWidget`, the four file watcher hooks, notifications if needed |
| `doc/policy-types/widget.md`, `doc/policy-types/index.md`, `mkdocs.yml` | user documentation page, overview row, nav entry |
| `demo/seed/policy-NN-*.json` | demo example policy |
| `doc/tools/screenshots/doc-shoot.js` | `EDITOR_SHOTS` entry |
