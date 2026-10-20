# Compliance reports

A compliance report is a point-in-time record of the fleet: which nodes
were in scope, which policies applied to each of them, the agent's last
compliance result for every policy, and each node's security posture
(disk encryption, pending security updates, mandatory access control).
The live [Compliance](compliance.md) page changes with every agent
report; a compliance report never changes after it is generated. Use it
to answer "were the lab machines encrypted and locked down on the 14th"
when an auditor asks, and to keep evidence for your records.

Open **Fleet -> Reports** in the sidebar. The page lists reports and
generates new ones.

!!! note "Bor Enterprise"
    Compliance reports are a Bor Enterprise feature. The community server
    contains the page and the API but keeps them switched off; a license
    that includes the `reports` feature enables them, together with report
    signing and the HTML document. Without it the **Reports** page says so
    and the report API answers 404. Reports generated while the feature was
    enabled are kept and reappear when it is enabled again.

## Permissions

| Action | Permission |
|---|---|
| List, open and download reports | `report:view` |
| Generate a report | `report:create` |
| Delete a report | `report:manage` |

A delegated administrator with a node-group-scoped role (Bor Enterprise)
sees only reports whose scope lies inside their groups and can generate
reports only over those groups; "all nodes" means all of their groups.
See [Permissions](../reference/permissions.md).

## Generate a report

Select **Generate report**, choose the scope and submit:

| Field | Meaning |
|---|---|
| **Scope** | **All nodes**, or one or more node groups. A node in several selected groups appears once. |
| **Policy types** | Limit the results to these policy types. Empty means every type. The nodes stay in scope either way, so a report limited to disk encryption still lists every node and says which ones had no result. |
| **Label** and **Notes** | Free text stored with the report, for example the audit it was made for. Both appear in the downloads. |
| **Include per-setting items** | On by default. Off stores only the per-policy result and message, without the per-setting breakdown. Switch it off when item keys would carry personal data you do not want in the evidence (for example SELinux login mappings of named users). |

The report is generated at once from the results the server holds; the
agents are not contacted. A result is included only while an enabled
binding still delivers the policy to the node, the same rule the
Compliance page applies. The report records how old every result was
and flags stale ones (see below).

Generation refuses a scope with more than `reports.max_results`
node-policy results (100000 by default); narrow the scope or raise the
limit in the [server configuration](../configuration/server.md#reports).

## What a report contains

| Section | Content |
|---|---|
| **Summary** | Nodes in scope, online, unreachable and without results; results and stale results; counts per status; the compliance score. |
| **Policies** | Every policy with a result: name, type, the policy version in force, and counts per status. |
| **Nodes** | Every node in scope: operating system, agent version, connection state and last-seen time, node groups, mandatory access control module and mode, whether the system volume is encrypted and its recovery key escrowed, pending security updates and reboot state, and counts per status. |
| **Results** | One entry per node and policy: status, the agent's message, when it was reported, its age at generation time, and the per-setting items with their benchmark control labels (when items are included). |
| **Controls** | The per-setting items aggregated by the security benchmark controls they map to (for example CIS Red Hat Enterprise Linux 9 1.3.1.5), with pass, fail, error and inapplicable counts and the number of failing nodes. Only in the downloads. |

**Score.** Compliant results divided by the results that could be
evaluated (compliant, non-compliant and error), in percent. Inapplicable
and unknown results do not count. A report with nothing to evaluate has
no score.

**Stale and unreachable.** A result older than `reports.stale_after`
(7 days by default) at generation time is marked stale; a node that was
not online and had not been seen within the same period is marked
unreachable. Both are counted in the summary and shown on the report, so
old evidence is never presented as current. Agents re-report after every
sync and whenever a managed file or unit changes, so a stable node's
results can legitimately be days old.

## Open a report

Select a report to open it. The header shows when and by whom it was
generated, the scope, and the report's integrity:

| Field | Meaning |
|---|---|
| **SHA-256** | The hash of the report's canonical JSON bundle, recorded when the report was generated and written to the audit log. |
| **Hash** | `ok` when the stored bundle still has that hash. `mismatch` means the stored data changed after generation; the server records a `report.integrity_failure` audit event when this happens. |
| **Signature** | Every report generated while the feature is licensed is signed with a certificate issued by the server's certificate authority; the status is `valid`, `invalid` (the signature does not match the stored bundle) or `unverified` (signed, but this server cannot check it, for example after the license was removed). `unsigned` marks a report generated while signing was off. |

The tabs list the policies, the nodes with their posture, and the
results with filters for status and staleness. Expand a result to see
its per-setting items.

## Download a report

| Format | Content |
|---|---|
| **JSON** | The canonical bundle (`apiVersion: bor.report.v1`), byte for byte as stored. Its SHA-256 is the hash shown in the header. Keep this file: it is the evidence, and every other format is derived from it. |
| **CSV** | Four files: `results` (one row per node and policy), `items` (one row per setting), `nodes` (one row per node with posture columns) and `controls` (one row per benchmark control). Cells that start with a formula character are prefixed with an apostrophe so spreadsheets do not execute them. |
| **Signature** | The detached signature of the JSON bundle (a compact JWS), for signed reports. |
| **HTML** | A self-contained, printable document of the report with the hash and the signing key on every page. |

Every download is recorded in the audit log (`report.download`) with the
format, since handing out evidence is itself an auditable event.

### Verify a downloaded bundle

Compute the SHA-256 of the JSON file and compare it with the hash shown
in the report header or recorded in the `report.generate` audit event:

```bash
sha256sum compliance-report-q4-labs-20261020-1400.json
```

When the audit log is forwarded to a SIEM, the hash in the forwarded
event is an anchor outside Bor's database: a bundle that matches it is
the bundle the server generated at that time. Signed reports are verified
offline with the `bor-verify` tool and the server's CA certificate:

```bash
bor-verify -bundle compliance-report-q4-labs-20261020-1400.json \
  -signature compliance-report-q4-labs-20261020-1400.jws -ca ca.crt
```

## Retention and deletion

Reports are kept for `reports.retention_days` (1095 days, three years,
by default); an hourly job deletes older ones and records a
`report.purge` audit event with the count. The purge runs in every
edition, so old reports age out even after a license lapses. Set the
value to `0` to keep reports forever.

Deleting a report needs `report:manage` and is recorded as
`report.delete` with the report's hash. There is no undo: download the
bundle first when the evidence is still needed.

## Personal data

A report contains node names and FQDNs, policy names and the agent's
messages and per-setting keys. It never contains IP addresses, user
accounts or recovery keys. Where setting keys name people (login
mappings, session-access users), generate the report without items or
restrict who holds `report:view`, and let the retention period follow
your records policy. See [Security and compliance](../security/compliance.md).
