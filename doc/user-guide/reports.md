# Compliance reports

A compliance report is a point-in-time record of the fleet: which nodes
were in scope, which policies applied to each of them, the agent's last
compliance result for every policy, and each node's security posture
(disk encryption, pending security updates, mandatory access control).
The live [Compliance](compliance.md) page changes with every agent
report; a compliance report never changes after it is generated. Use it
to answer "were the lab machines encrypted and locked down on the 14th"
when an auditor asks, and to keep evidence for your records.

Open **Fleet -> Reports** in the sidebar. The page shows the fleet's
compliance trend, lists reports and generates new ones; with the
corresponding permissions it also has the **Schedules** and **Waivers**
tabs.

!!! note "Bor Enterprise"
    Compliance reports are a Bor Enterprise feature: the Bor Enterprise
    server generates, stores and serves them while its license includes the
    `reports` feature. The community server has no reports; its
    **Reports** page says so. Moving from the community server to Bor
    Enterprise of the same version needs no migration step: install the
    Bor Enterprise server and its license, and it creates the report tables
    on its first start. Reports stay in the database when the license
    lapses and reappear when it is renewed.

## Permissions

| Action | Permission |
|---|---|
| List, open and download reports; see the trend | `report:view` |
| Generate a report | `report:create` |
| Delete, pin and unpin reports; manage schedules | `report:manage` (global) |
| Sign a report off; accept and revoke waivers | `report:attest` (global) |

A delegated administrator with a node-group-scoped role sees only reports
whose scope lies inside their groups and can generate reports only over
those groups; "all nodes" means all of their groups. The fleet trend,
schedules, waivers and sign-off need the permission globally. See
[Permissions](../reference/permissions.md).

## Generate a report

Select **Generate report**, choose the scope and submit:

| Field | Meaning |
|---|---|
| **Scope** | **All nodes**, or one or more node groups. A node in several selected groups appears once. |
| **Policy types** | Limit the results to these policy types. Empty means every type. The nodes stay in scope either way, so a report limited to disk encryption still lists every node and says which ones had no result. |
| **Label** and **Notes** | Free text stored with the report, for example the audit it was made for. Both appear in the downloads. |
| **Include per-setting items** | On by default. Off stores only the per-policy result and message, without the per-setting breakdown. Switch it off when item keys would carry personal data you do not want in the evidence (for example SELinux login mappings of named users). |
| **Collect fresh evidence first** | Off by default. The server asks every online agent in scope to re-apply its policies and report again, waits until each of them has reported every result in scope anew (up to `reports.refresh_wait`, one minute by default), then takes the snapshot. The report records when the request went out, how long the server waited and how many agents answered in time; agents that did not answer keep their last result, which counts as stale if it is old. |

Without fresh evidence the report is generated at once from the results
the server holds. A result is included only while an enabled binding
still delivers the policy to the node, the same rule the Compliance page
applies. The report records how old every result was and flags stale
ones (see below).

Generation refuses a scope with more than `reports.max_results`
node-policy results (100000 by default); narrow the scope or raise the
limit in the [server configuration](../configuration/server.md#reports).

## What a report contains

| Section | Content |
|---|---|
| **Summary** | Nodes in scope, online, unreachable and without results; results, stale results and waived results; counts per status; the compliance score. |
| **Policies** | Every policy with a result: name, type, the policy version in force, and counts per status. |
| **Nodes** | Every node in scope: operating system, agent version, connection state and last-seen time, node groups, mandatory access control module and mode, whether the system volume is encrypted and its recovery key escrowed, pending security updates and reboot state, and counts per status. |
| **Results** | One entry per node and policy: status, the agent's message, when it was reported, its age at generation time, the framework controls it maps to, the waiver that covers it, and the per-setting items with their controls (when items are included). |
| **Controls** | Results and items aggregated by the framework controls they map to (see [Frameworks](#frameworks)), with pass, fail, error and inapplicable counts and the number of failing nodes. |
| **Waivers** | The waivers in force at generation time that cover at least one failure in the report. |
| **Collection** | For fresh-evidence reports: when the agents were asked, how long the server waited and how many answered. |

**Score.** Compliant results divided by the results that could be
evaluated (compliant, non-compliant and error), in percent. Inapplicable
and unknown results do not count. A report with nothing to evaluate has
no score. Waived results stay failures in the score: a waiver accepts a
risk, it does not remove it.

**Stale and unreachable.** A result older than `reports.stale_after`
(7 days by default) at generation time is marked stale; a node that was
not online and had not been seen within the same period is marked
unreachable. Both are counted in the summary and shown on the report, so
old evidence is never presented as current. Agents re-report after every
sync and whenever a managed file or unit changes, so a stable node's
results can legitimately be days old; generate with fresh evidence when
that matters.

## Frameworks

Results and items map to the controls of these frameworks:

| Framework | What maps to it |
|---|---|
| ISO/IEC 27001:2022 Annex A | Disk encryption (A.8.24, A.8.1), software updates (A.8.8), mandatory access control, units, dconf and KDE settings (A.8.9), packages, repositories and Flatpak (A.8.19), the host firewall (A.8.20), privilege rules (A.8.2), session access (A.5.15, A.8.5), browsers (A.8.23, A.8.1), screen locking (A.7.7), removable media (A.7.10, A.8.7), audit and logging services (A.8.15, A.8.16). |
| NIS2 Directive Art. 21(2) | The same measures under (b) incident handling, (e) vulnerability handling, (g) basic cyber hygiene, (h) cryptography and encryption and (i) access control. |
| CIS Red Hat Enterprise Linux 9 Benchmark v2.0.0 | On nodes that report Red Hat Enterprise Linux 9: updates, SELinux, services and client packages that must not be in use, GNOME screen lock and removable media, repositories, the firewall, rsyslog and auditd. |
| CIS Ubuntu Linux 24.04 LTS Benchmark v1.0.0 | On nodes that report Ubuntu 24.04: updates, AppArmor, services and client packages that must not be in use, GNOME screen lock and removable media, repositories, rsyslog and auditd. |

A CIS control is counted only where the result is evidence for it: a
compliant `telnet` package result supports "Ensure telnet client is not
installed" only when the policy removes the package, a unit result
supports "Ensure ... services are not in use" only when the policy masks
the unit or disables and stops it, and "Ensure the SELinux mode is
enforcing" only when the policy asks for enforcing mode. Controls for
which no Bor setting provides evidence are not listed.

Each report records the version of the mapping it was built with
(`server.frameworks_version` in the bundle) and keeps its controls when
the mapping changes later.

## Open a report

Select a report to open it. The header shows when and by whom it was
generated, the scope, whether the report is pinned and signed off, and
the report's integrity:

| Field | Meaning |
|---|---|
| **SHA-256** | The hash of the report's canonical JSON bundle, recorded when the report was generated and written to the audit log. |
| **Hash** | `ok` when the stored bundle still has that hash. `mismatch` means the stored data changed after generation; the server records a `report.integrity_failure` audit event when this happens. |
| **Signature** | Every report generated while the feature is licensed is signed with a certificate issued by the server's certificate authority; the status is `valid`, `invalid` (the signature does not match the stored bundle) or `unverified` (signed, but this server cannot check it, for example after the license was removed). `unsigned` marks a report generated while signing was off. |

The tabs:

| Tab | Content |
|---|---|
| **Overview** | The notes and the policies with their counts. |
| **Controls** | The framework controls, one framework at a time. Expand a control to list the results and settings behind it. |
| **Nodes** | The nodes with their posture. |
| **Results** | The results, with filters for status and staleness. Expand a result to see its per-setting items; waived results and settings are marked. With `report:attest`, **Accept exception** records a waiver for a failing result or one of its settings. |
| **Compare** | What changed since an earlier report (the latest earlier report over the same scope by default): new failures, fixed results, results that still fail, and nodes and policies added to or removed from the scope. |
| **Sign-off** | The report's attestations; see [Sign-off](#sign-off). |

## Download a report

| Format | Content |
|---|---|
| **JSON** | The canonical bundle (`apiVersion: bor.report.v1`), byte for byte as stored. Its SHA-256 is the hash shown in the header. Keep this file: it is the evidence, and every other format is derived from it. |
| **CSV** | Four files: `results` (one row per node and policy), `items` (one row per setting), `nodes` (one row per node with posture columns) and `controls` (one row per framework control). Cells that start with a formula character are prefixed with an apostrophe so spreadsheets do not execute them. |
| **Signature** | The detached signature of the JSON bundle (a compact JWS), for signed reports. |
| **HTML** | A self-contained, printable document of the report with the hash and the signing key on every page. |
| **PDF** | An archival document (PDF/A-2b, fonts embedded) with the summary, framework controls, failures, policies, nodes, waivers and notes; the bundle's hash and the signer are in the footer of every page. The PDF is signed with the report signing key (a CMS signature PDF readers display) and, when `reports.timestamp_url` is set, time-stamped by that time-stamping authority. It is rendered on its first download and kept, so every later download returns the same file. |
| **OCSF** | One OCSF 1.3.0 Compliance Finding (class 2003) per setting, or per result without settings, as NDJSON, for import into a SIEM or data lake. |
| **Attestations** | The report's sign-offs: each signed statement with its signature, for offline verification. |

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
the bundle the server generated at that time. Signed reports and their
sign-offs are verified offline with the `bor-verify` tool and the
server's CA certificate:

```bash
bor-verify -bundle compliance-report-q4-labs-20261020-1400.json \
  -signature compliance-report-q4-labs-20261020-1400.jws \
  -attestations compliance-report-q4-labs-20261020-1400.attestations.json \
  -ca ca.crt
```

The tool prints the report's summary, the signer and the outcome of
every check, and exits with 0 when all of them pass.

## Compliance trend

Once a day the server stores a summary of the fleet and of every node
group: the counts and the score only, no evidence bundle. The summaries
feed the **Fleet compliance score** chart above the report list and the
**Compliance trend** card on the dashboard (last 30 days; hover or use
the arrow keys for the values of a day, or show the chart as a table).
Daily summaries are hidden from the report list unless **Show daily
summaries** is on. Switch them off with `reports.daily_summary: false`.

## Schedules

The **Schedules** tab (`report:manage`) generates reports on a fixed
cadence: daily, weekly on a weekday, or monthly on a day of the month
(1-28, or the last day), at a time of day in a time zone. Runs follow
daylight saving time in that zone. A schedule has the same scope options
as a manual report, including fresh evidence, and a label template:
`{name}`, `{date}` and `{datetime}` are replaced with the schedule's name
and the run's local date and time.

The server checks for due schedules every minute. A run missed while the
server was down runs once when it starts again, unless it is older than
`reports.catch_up_window` (24 hours by default); older runs
are recorded as skipped. **Run now** generates the schedule's report
immediately. The list shows the next run and the outcome of the last one
with a link to its report. Several servers sharing one database never
run the same schedule twice.

Scheduled reports are generated by `schedule:<name>`, signed and kept
like manual reports.

### Delivery

After each run a schedule can deliver the report:

- **Webhook.** An HTTPS `POST` with a JSON body
  (`event: report.generated`, the schedule, the report's summary, hash and
  formats, and links to the report and its downloads when
  `reports.public_url` is set). With a signing secret the request carries
  `X-Bor-Timestamp` and
  `X-Bor-Signature: sha256=<HMAC-SHA256 of "<timestamp>.<body>">`; verify
  it before trusting the payload. `X-Bor-Delivery` identifies the delivery
  and stays the same when it is retried, so a receiver can discard
  duplicates. Redirects are not followed.
- **Email.** One message to all recipients with the summary and the
  selected formats attached (PDF, HTML, JSON, CSV results, signature; at
  most 20 MB in total), sent through the relay configured under
  `reports.smtp` (or the `BOR_REPORT_SMTP_*` variables) over TLS only:
  implicit TLS, or STARTTLS, which the relay must offer.

A run puts its deliveries in a queue and finishes; a background worker
sends them. A delivery that fails is retried after 1, 5 and 15 minutes,
1 hour and 3 hours (six attempts in all); a refusal that cannot change
(an HTTP 4xx answer other than 429, an SMTP 5xx reply) ends it at once.
The queue is stored in the database, so deliveries survive a restart, and
several servers sharing one database never send one twice. The schedule
list shows the state of the last run's deliveries: **Queued**,
**Retrying** (with the last error), **Delivered** or **Delivery failed**.

**Settings -> Compliance reports** (`settings:manage`) shows the mail and
webhook settings in force, where they were set, and the queue: deliveries
waiting, sent and failed in the last 24 hours, and the recent failures
with a **Retry** action. **Send test email** sends a message to your own
account's email address through the relay and shows the relay's answer.
Outcomes are recorded in the audit log (`report.delivered`,
`report.delivery_failed`, `report.delivery_retry`, `report.test_email`);
attachments are recorded as downloads by the schedule.

## Waivers

A waiver records that a failure is a known, accepted risk until a date:
for one policy, on one node or on every node, for the whole result or for
one setting (a key ending in `*` matches a prefix). Accept one from a
report's **Results** tab; the **Waivers** tab lists active waivers (or all
of them, including expired and revoked ones) and revokes them. Creating
and revoking a waiver needs `report:attest`, requires a reason and an
expiry at most `reports.waiver_max_days` (365) days away, and
is audit-logged (`report_waiver.create`, `report_waiver.revoke`).

Reports generated while a waiver is in force record it and mark the
results and settings it covers as waived; their status and the score do
not change. Revoking or expiring a waiver never changes reports already
generated.

## Sign-off

The **Sign-off** tab records attestations: a person other than the one
who generated the report approves or rejects it, with a comment (required
for a rejection). A report is **Approved** once
`reports.required_approvals` (1 by default) people approved it
and nobody rejected it, and **Rejected** as soon as anyone rejects it.
Signing off needs `report:attest`; each person signs a report off once,
and only reports whose stored bundle still matches its hash can be signed
off.

Each attestation is a statement that names the report and its bundle's
hash, the decision, the comment, the person and the time, signed by the
server with the report signing key. The signature proves that the server
recorded this decision for the signed-in user; it is not the person's own
signature. Download the attestations and verify them with `bor-verify`
(see above). Every sign-off is audit-logged (`report.attest`).

## Pinning (legal hold)

**Pin** keeps a report until it is unpinned: a pinned report cannot be
deleted and the retention purge skips it. Pinning and unpinning need
`report:manage` and are audit-logged (`report.pin`, `report.unpin`).

## Findings in the SIEM

When the audit log is forwarded in OCSF format
(`audit.syslog.format: ocsf`), every change of a compliance result is
also forwarded as an OCSF Compliance Finding as the agent reports it:
**Create** for a node's first result of a policy, **Update** when its
status changes and **Close** when it becomes compliant, with the
framework controls of the result and its failing settings. Switch this
off with `reports.ocsf_findings: false`. CEF forwarding does
not carry findings.

## Monitoring

The server's Prometheus endpoint exports the number of stored reports,
the time of the newest report per trigger, the fleet compliance score,
schedule states and overdue schedules, the delivery queue and the expiry
of the signing certificate, all named `bor_compliance_*`. See
[Monitoring](../operations/monitoring.md#compliance-reports-bor-enterprise)
for the list and suggested alerts.

## Retention and deletion

Reports are kept for `reports.retention_days` (1095 days, three years,
by default); an hourly job deletes older unpinned reports and records a
`report.purge` audit event with the count. The purge keeps running when
the license lapses, so old reports still age out. Set the value to `0`
to keep reports forever. Deleting a report also deletes its
stored PDF and sign-offs.

Deleting a report needs `report:manage` and is recorded as
`report.delete` with the report's hash; pinned reports must be unpinned
first. There is no undo: download the bundle first when the evidence is
still needed.

## Personal data

A report contains node names and FQDNs, policy names and the agent's
messages and per-setting keys, and the user names of the people who
generated it, accepted waivers or signed it off. It never contains IP
addresses or recovery keys. Where setting keys name people (login
mappings, session-access users), generate the report without items or
restrict who holds `report:view`, and let the retention period follow
your records policy. See [Security and compliance](../security/compliance.md).
