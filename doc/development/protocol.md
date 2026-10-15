# Protocol and code generation

Server and agent talk gRPC, and every policy type is described as a
Protocol Buffers message. This page lists the `.proto` files, the services
and messages they define, how `make proto` turns them into Go and
TypeScript, how field annotations drive the generic policy editors in the
web UI, and the rules for changing a proto without breaking deployed
agents.

## Proto files

All definitions live under `proto/`:

| File | Package | Contents |
|---|---|---|
| `proto/policy/policy.proto` | `bor.policy.v1` | `PolicyService`, the `Policy` envelope, streaming, compliance, heartbeat, tamper and renewal messages |
| `proto/policy/firefox.proto` | `bor.policy.v1` | `FirefoxPolicy` |
| `proto/policy/thunderbird.proto` | `bor.policy.v1` | `ThunderbirdPolicy` |
| `proto/policy/chrome.proto` | `bor.policy.v1` | `ChromePolicy` |
| `proto/policy/edge.proto` | `bor.policy.v1` | `EdgePolicy` |
| `proto/policy/kconfig.proto` | `bor.policy.v1` | `KConfigPolicy` |
| `proto/policy/dconf.proto` | `bor.policy.v1` | `DConfPolicy`, GSettings schema catalogue messages |
| `proto/policy/polkit.proto` | `bor.policy.v1` | `PolkitPolicy`, polkit action catalogue messages |
| `proto/policy/firewalld.proto` | `bor.policy.v1` | `FirewalldPolicy` |
| `proto/policy/flatpak.proto` | `bor.policy.v1` | `FlatpakPolicy` |
| `proto/policy/package.proto` | `bor.policy.v1` | `PackagePolicy` (repositories and packages) |
| `proto/policy/session_access.proto` | `bor.policy.v1` | `SessionAccessPolicy` |
| `proto/policy/disk_encryption.proto` | `bor.policy.v1` | `DiskEncryptionPolicy`, LUKS inventory, escrow and task messages |
| `proto/enrollment/enrollment.proto` | `bor.enrollment.v1` | `EnrollmentService` |
| `proto/bor/ui.proto` | `bor.ui` | The `(bor.ui.field)` field option used by the UI generator |
| `proto/audit/audit.proto` | `bor.audit.v1` | `AuditEvent`, the internal canonical audit record consumed by all audit sinks |
| `proto/export/export.proto` | `bor.export.v1` | The envelope for policy and binding export and import (YAML or JSON) |

The policy-type files are imported by `policy.proto`, so the whole
`proto/policy` directory compiles as one unit. Only `policy.proto` and
`enrollment.proto` define services; `audit.proto` and `export.proto` are
message-only and never go over the wire between server and agent.

## Services

### `bor.enrollment.v1.EnrollmentService`

Served on the web port (`server.enrollment_port`, default 8443).

| RPC | Caller | Purpose |
|---|---|---|
| `Enroll(EnrollRequest) returns (EnrollResponse)` | Agent, first run | Exchange a one-time token and a CSR for a signed client certificate and the CA certificate |
| `KerberosEnroll(KerberosEnrollRequest) returns (EnrollResponse)` | Domain-joined agent | Same, authenticated with a SPNEGO token from the machine keytab instead of a token |
| `CreateEnrollmentToken(CreateEnrollmentTokenRequest) returns (CreateEnrollmentTokenResponse)` | Automation | Mint a token for a node group; requires the `x-admin-token` metadata to match `BOR_ADMIN_TOKEN` and, like every non-bootstrap enrollment RPC, a client certificate from the internal CA |

`EnrollResponse` carries `node_id`, `signed_cert_pem`, `ca_cert_pem` and
`assigned_node_group`. The web UI does not use `CreateEnrollmentToken`; it
calls the REST endpoint `POST /api/v1/node-groups/{id}/tokens`.

### `bor.policy.v1.PolicyService`

Served on the policy port (`server.policy_port`, default 8444), mTLS only.
The caller's identity is always the verified certificate CN; a `client_id`
field that disagrees with it is rejected with `PermissionDenied`.

| RPC | Purpose |
|---|---|
| `SubscribePolicyUpdates` (server stream) | Long-lived policy stream: snapshot or delta on connect, then pushes |
| `ReportCompliance` | Compliance result for one policy on this node |
| `GetAgentConfig` | Server-side agent settings: desktop notification switch, cooldown and messages, LUKS inventory reporting |
| `Heartbeat` | Node metadata (`NodeInfo`: FQDN, IP, OS name and version, desktop environments, agent version, machine ID) |
| `ReportTamperEvent` | A managed file was changed externally and restored |
| `RenewCertificate` | Sign a new CSR for the calling node, authenticated with its current certificate |
| `ReportSchemaCatalogue` | GSettings schemas installed on the node (dconf editor data) |
| `ReportPolkitCatalogue` | Polkit actions installed on the node (polkit editor data) |
| `ReportDiskEncryptionState` | LUKS volume inventory and platform facts; the response piggybacks pending tasks |
| `GetDiskEncryptionTasks` | Pending disk-encryption tasks for the node |
| `EscrowRecoveryKey` | Store a new recovery key before its keyslot exists |
| `ConfirmRecoveryKey` | Promote a pending key to active once the keyslot is verified |
| `BeginRecoveryKeyRotation` | Release the active key as the unlock credential for a pending rotation |
| `GetPolicy`, `ListPolicies` | Unary policy lookup; the current agent uses the stream instead |

## Messages

### `Policy`

The envelope delivered to agents:

| Field | Meaning |
|---|---|
| `id`, `name`, `description`, `type`, `version` | Policy metadata; `type` is the policy type string (`Firefox`, `Kconfig`, `Dconf`, `Firewalld`, ...) |
| `content` (5) | The stored JSON content, kept for REST compatibility |
| `typed_content` (oneof, 10-13, 15-22) | The same content parsed into the type's message (`firefox_policy`, `kconfig_policy`, ..., `disk_encryption_policy`) |
| `enabled` | `true` when the policy state is `released` |
| `priority` (14) | Highest priority among the enabled bindings that deliver this policy to the node |

The server fills `typed_content` in `modelToProto`
(`server/internal/grpc/server.go`) by parsing `content` with `protojson`.
Agents work from the typed message.

### `PolicyUpdate`

One message on the policy stream:

| Field | Meaning |
|---|---|
| `type` | `UpdateType` enum, see below |
| `policy` | The policy, or empty for an empty snapshot and for commands |
| `revision` | Server revision after this event; the agent sends the last value back as `last_known_revision` on reconnect |
| `snapshot_complete` | `true` on the last message of a snapshot batch |

`UpdateType` values:

| Value | Meaning |
|---|---|
| `SNAPSHOT` (4) | One entry of a full snapshot. The agent stages entries and swaps its cache when `snapshot_complete` arrives, so a snapshot is applied atomically |
| `CREATED` (1), `UPDATED` (2) | Delta: the policy was added or changed |
| `DELETED` (3) | Delta: the policy no longer applies |
| `METADATA_REQUEST` (5) | Command: collect metadata and send a `Heartbeat` |
| `DISK_ENCRYPTION_TASK` (6) | Command: call `GetDiskEncryptionTasks` and run the tasks |
| `UNKNOWN` (0) | Zero value, never sent |

Inside the server a `SNAPSHOT` event with no policy is a resync signal
(`IsResyncSignal` in `server/internal/grpc/policy_hub.go`); it is never
forwarded as is, the stream handler turns it into a real snapshot.

### Compliance

`ReportComplianceRequest` carries `policy_id`, a free-text `message`,
`reported_at`, a `status` and optional `items`:

```proto
enum ComplianceStatus {
  COMPLIANCE_STATUS_UNKNOWN       = 0;
  COMPLIANCE_STATUS_COMPLIANT     = 1;
  COMPLIANCE_STATUS_NON_COMPLIANT = 2;
  // INAPPLICABLE: schema / feature not installed on this node; excluded from score.
  COMPLIANCE_STATUS_INAPPLICABLE  = 3;
  // ERROR: feature is installed but enforcement or check failed.
  COMPLIANCE_STATUS_ERROR         = 4;
}
```

`items` is a list of `ComplianceItemResult` (`schema_id`, `key`, `status`,
`message`). It was introduced for dconf keys and is also used by polkit,
package and Flatpak results to report per-rule or per-package detail.

The `bool compliant` field (3) is deprecated but still populated by the
agent for older servers. When `status` is `UNKNOWN`, the server falls back
to `compliant` (`complianceStatusToString`). The result is stored as
`compliant`, `non_compliant`, `inapplicable` or `error` in
`compliance_results`.

## Code generation

`make proto` runs three targets. Generated files are committed, so a normal
build does not need `protoc`; run the targets after editing any `.proto`
file and commit the output with the change.

| Target | Tool | Output |
|---|---|---|
| `proto-go` | `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` | `server/pkg/grpc/borui` (from `ui.proto`), `server/pkg/grpc/policy` (all of `proto/policy`, with gRPC stubs), `server/pkg/grpc/audit`, `server/pkg/grpc/export` |
| `proto-ts` | `protoc` + `ts-proto` (`node_modules/.bin/protoc-gen-ts_proto`) | `server/web/frontend/src/generated/proto/<name>.ts`, type definitions only |
| `proto-ui` | `protoc` + `protoc-gen-bor-ui` | `server/web/frontend/src/generated/proto/<name>_ui.ts` and `policy_ui_types.ts` |

`make install-deps` installs `protoc-gen-go`, `protoc-gen-go-grpc`, builds
and installs `protoc-gen-bor-ui` from `server/cmd/protoc-gen-bor-ui`, and
runs `npm install` in the frontend for `ts-proto`. `protoc` itself must
come from your distribution.

!!! warning "Enrollment stubs"
    `make proto` does not compile `proto/enrollment/enrollment.proto`. The
    committed `server/pkg/grpc/enrollment/*.pb.go` files were generated
    separately. If you change `enrollment.proto`, regenerate them by hand
    with the same `protoc-gen-go` / `protoc-gen-go-grpc` options as
    `proto-go`, for example:

    ```bash
    protoc --go_out=server/pkg/grpc/enrollment --go_opt=paths=source_relative \
        --go-grpc_out=server/pkg/grpc/enrollment --go-grpc_opt=paths=source_relative \
        -I proto/enrollment proto/enrollment/enrollment.proto
    ```

The TypeScript types are generated with
`onlyTypes=true,snakeToCamel=false,outputServices=false`: plain interfaces
whose property names are the proto field names, no runtime codec and no
service clients. The browser never speaks gRPC; it talks to the REST API.

### UI metadata: `protoc-gen-bor-ui`

`proto/bor/ui.proto` extends `google.protobuf.FieldOptions` with a
`FieldUi` option (field number 50000):

| `FieldUi` field | Effect |
|---|---|
| `group` | Section in the tree editor. Fields without an annotation, or message fields without a group, are skipped |
| `label` | Display name; when empty, derived from the field name by splitting camelCase / PascalCase words |
| `description` | Help text; when empty, the field's leading proto comment is used |
| `chrome_only` | Marks Chrome settings that do nothing in Chromium builds |
| `int_options` | Allowed integer values with labels; the field becomes `integer-enum` |
| `string_options` | Allowed string values; the field becomes `string-enum` |

An annotated field looks like this (`proto/policy/chrome.proto`):

```proto
optional string HomepageLocation = 1 [json_name = "HomepageLocation", (bor.ui.field) = {group: "Homepage & Startup"}];
```

The plugin (`server/cmd/protoc-gen-bor-ui/main.go`) handles every message
whose name ends in `Policy`. For each `.proto` file containing such a
message it writes `<basename>_ui.ts` exporting one array per message, named
from the message (`ChromePolicy` -> `CHROME_ALL_POLICIES`). Each entry is a
`PolicyFieldDef` with `key` (the field's JSON name), `label`, `group`,
`description`, a `type` inferred from the proto type (`boolean`, `string`,
`integer`, `string-enum`, `integer-enum`, `list`, `json` for
`google.protobuf.Value`, `object` with `subFields` for nested messages) and
the options above.

The web UI imports `FIREFOX_ALL_POLICIES`, `THUNDERBIRD_ALL_POLICIES`,
`CHROME_ALL_POLICIES` and `EDGE_ALL_POLICIES` in
`src/views/Policies/editors/PolicyContentEditor.tsx` (the generic tree
editor) and `src/views/Policies/policySummary.ts`. These four types are
the ones annotated with `(bor.ui.field)`. The other policy types
(KConfig, dconf, polkit, Firewalld, Flatpak, packages, session access,
disk encryption) have hand-written editors and do not use the annotations.

Adding an annotated setting to one of the four browser types is therefore a
proto-only change: add the field with its annotation, run `make proto`, and
the setting appears in the editor.

## Stored policy content

`policies.content` is a JSON document in the **protojson** mapping of the
type's message. The server parses it with `protojson.Unmarshal`, mostly
with `DiscardUnknown: true`, both to validate content on release and to
fill `typed_content` for agents.

Things to keep in mind:

- protojson accepts both the JSON name and the original proto field name
  on input. The JSON name defaults to lowerCamelCase (`schema_id` ->
  `schemaId`). Newer hand-written editors (Firewalld, Flatpak, session
  access, disk encryption) emit camelCase keys and enum values as their
  string names.
- The browser policy types set `json_name` explicitly to the upstream
  policy name (`HomepageLocation`, `DisableAppUpdate`), so the stored JSON
  matches the vendor's own policy format.
- Because unknown fields are discarded, content written by a newer UI with
  fields an older server does not know loses those fields silently when the
  server converts it for agents.
- When you rename a field, its JSON name changes too, and existing stored
  content using the old name stops parsing (or is dropped). Set `json_name`
  to the old name, or write a data migration.

## Changing protos safely

Agents in the field are upgraded independently of the server, so every
change must be wire compatible in both directions.

- **Never change or reuse a field number.** The number is the wire
  identity. Changing a field's type is also a breaking change.
- **Removing a field:** delete it and add `reserved` for both the number and
  the name, as in `proto/policy/kconfig.proto`:

    ```proto
    reserved 1;
    reserved "entries";
    ```

- **Adding a field:** use a new number. Old agents ignore it; new agents
  see the zero value from old servers, so the zero value must mean "not
  set" or "keep the old behavior". Use `optional` where the difference
  between unset and zero matters (the browser policy messages use
  `optional` for most settings).
- **Enums:** keep `0` as an unknown or default value, add new values at the
  end, and handle unknown values on the receiving side. See how the server
  maps `ComplianceStatus` back to the legacy `compliant` bool.
- **Deprecating:** keep the field and populate it as long as supported
  peers read it, with a comment saying what replaces it (`compliant` in
  `ReportComplianceRequest` is the example).
- **New policy type:** add the message in its own file under
  `proto/policy`, import it from `policy.proto` and add it to the
  `typed_content` oneof with a new number. The full checklist is in
  [Adding a policy type](adding-a-policy-type.md).
- **Package names** carry the major version (`bor.policy.v1`). A change that
  cannot be made compatibly needs a new package, not an edit in place.

After any change, run `make proto`, then `make test` and `make lint`, and
check the frontend with `npm run typecheck` in `server/web/frontend`.
