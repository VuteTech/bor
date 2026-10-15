# Concepts

This page explains the building blocks of Bor and how they fit together:
the server and the agent, nodes and node groups, policies and their
lifecycle, policy bindings, how the agent combines several policies, and how
policy changes reach the desktops in real time. Read it once before you
install Bor; the rest of the documentation uses these terms throughout.

## Architecture overview

Bor has two components:

- **Bor server** (`bor-server`): a single Go binary that serves the web
  interface and REST API, stores everything in PostgreSQL, runs an internal
  certificate authority (CA), and streams policies to agents.
- **Bor agent** (`bor-agent`): a daemon that runs as root on every managed
  Linux desktop. It enrolls with the server once, keeps a persistent
  connection open, writes the policy files on the endpoint, reports
  compliance, and restores managed files when someone changes them locally.

```mermaid
--8<-- "diagrams/architecture-detail.mmd"
```

The server listens on two ports:

| Port | Default | Used by | Purpose |
|------|---------|---------|---------|
| Web port (enrollment / UI) | 8443, 443 recommended for production | Browsers, enrolling agents, package managers | Web UI, REST API, the enrollment gRPC service and the agent package repository. Client certificates are accepted but not required, because a new agent has no certificate yet. |
| Policy port | 8444 | Enrolled agents only | Policy streaming and certificate renewal. Every connection must present a valid, non-revoked agent certificate signed by the internal CA. |

See [Files and ports](../reference/files-and-ports.md) for the complete list.

!!! note "Port numbers in this documentation"
    The web port is set by `server.enrollment_port`. Its default is 8443,
    but production installations should use 443 (see
    [Choose the web port](../installation/server.md#step-6-choose-the-web-port)).
    Examples in this documentation therefore assume port 443 and write
    URLs without a port, for example `https://bor.example.com/agent/`,
    and agent configuration with `enrollment_port: 443`. If your server
    still uses the default, add `:8443` to every URL
    (`https://bor.example.com:8443/agent/`) and use
    `enrollment_port: 8443` on the agents. The [Quick start](quickstart.md)
    keeps the default 8443.

## Nodes

A **node** is one enrolled agent, normally one desktop or laptop. The node
is created on the server the moment the agent enrolls. Its name is the
agent's `client_id`, which defaults to the machine's hostname. The node name
is also the Common Name of the agent's client certificate and must be unique:
the server refuses to enroll a second agent with a name that already exists.

The agent reports metadata (FQDN, IP address, operating system, desktop
environment, agent version) so you can see what is in your fleet on the
**Nodes** page. A node is shown as online while its policy stream is open
and offline when the stream closes.

## Node groups

A **node group** is a named set of nodes. Groups are the unit you assign
policies to: you never bind a policy to an individual node. A node can be a
member of several groups and receives the policies of all of them.

Every enrollment token is generated for one node group, so an agent lands in
the right group the moment it enrolls. You can change memberships later on
the **Nodes** page.

!!! note
    The agent reads its group memberships when its policy stream connects.
    After you add a node to a group or remove it from one, the change takes
    effect the next time the agent reconnects (for example after
    `sudo systemctl restart bor-agent`) or the next time a policy or binding
    of one of its groups changes.

## Policies

A **policy** is a named, typed piece of configuration, for example a set of
Firefox settings or a list of Flatpak applications. Each policy has exactly
one policy type. The available types are Firefox, Thunderbird, Chrome
(Chrome and Chromium), Microsoft Edge, KDE Plasma (KConfig), Dconf (GNOME),
Polkit, Firewall (firewalld), Session Access, Package, Flatpak apps and Disk
encryption. See [Policy types](../policy-types/index.md) for what each one
manages.

### Policy lifecycle

Policies move through three states:

```mermaid
--8<-- "diagrams/policy-lifecycle.mmd"
```

| State | Editable | Delivered to agents | Notes |
|-------|----------|---------------------|-------|
| Draft | Yes | No | Every new policy starts here. |
| Released | No | Yes, through enabled bindings | Releasing validates the content; invalid content cannot be released. |
| Archived | No | No | Kept for reference. |

Rules enforced by the server:

- Only draft policies can be edited.
- Only released policies are delivered to agents. A draft or archived
  policy never reaches a node, even when a binding exists.
- A released policy can be unpublished (back to draft) or archived only when
  none of its bindings is enabled. Disable the bindings first.
- An archived policy can be restored to draft.

A policy can additionally be marked as **deprecated**, with an optional
message and a replacement policy. Deprecation is informational: it does not
change what is delivered.

## Policy bindings

A **policy binding** connects one policy to one node group. Bindings are
many-to-many: a policy can be bound to many groups and a group can have many
policies. Each binding has:

- **State**: enabled or disabled. New bindings are created disabled, so
  nothing is delivered until you deliberately enable them. A binding can only
  be enabled while its policy is released.
- **Priority**: an integer (default 0). When the same policy reaches a node
  through several bindings, the highest priority among its enabled bindings
  is used.

A node receives every released policy that has at least one enabled binding
to any of its groups.

## How several policies of the same type are combined

A node often receives more than one policy of the same type. The agent always
merges them into one result per type, but the merge rules differ by type:

| Policy types | How the agent combines them |
|--------------|-----------------------------|
| Firefox, Thunderbird, Chrome, Microsoft Edge | The policies are merged into one managed file. Settings that only one policy sets are combined; list settings are appended. When two policies set the same single-value setting, the result is not ordered by priority, so avoid binding conflicting values of the same setting to one node. |
| KDE Plasma (KConfig) | Entries from all policies are merged into the managed configuration files. |
| Dconf | Policies are applied in ascending priority order, so for the same key the policy with the higher binding priority wins. Keys that lost are reported as overridden in compliance. |
| Package, Flatpak apps, Firewall | Merged in priority order; where two policies disagree, the higher-priority policy wins (for example the package state and version, or a Flatpak remote of the same name). |
| Polkit | Each policy becomes its own rules file whose name starts with the zero-padded priority (`000-bor-...rules`). polkitd evaluates files in name order and the first matching rule wins, so a lower priority number is evaluated first. |

The policy type pages describe the details for each type.

## Revisions, streaming and delta sync

The server keeps a monotonic **revision** counter (stored in the database, so
it survives restarts). Every policy or binding change bumps it and is pushed
immediately to the agents it affects.

1. On connect, the agent sends the last revision it has applied.
2. If the agent is new or restarted, the server sends a full **snapshot** of
   every policy the node should have.
3. If the agent was only briefly disconnected, the server sends the missed
   changes (created, updated, deleted policies) as a delta, or a full
   snapshot when the delta is no longer available.
4. The stream then stays open and the server pushes further changes as they
   happen. There is no polling interval.

If the connection drops, the agent reconnects with exponential backoff that
starts at one second and is capped at 60 seconds.

When the set of policies for a type becomes empty (for example you disable
the last binding), the agent removes what it wrote: managed files are
restored from the backup it took before the first write, or deleted if no
original existed.

## Compliance reporting

After applying a policy, the agent reports a compliance result for it.
Results use these states:

| Status | Meaning |
|--------|---------|
| Compliant | The policy is applied as specified. |
| Non-Compliant | The endpoint does not match the policy. |
| Inapplicable | The policy cannot apply to this node, for example a Firewall policy on a node without firewalld, or a Package policy on a node without a supported package manager. |
| Error | The agent tried to apply the policy and failed. The message explains why. |
| Unknown | No result has been reported yet. |

Results are visible on the **Compliance** page and per node. See
[Compliance](../user-guide/compliance.md).

## Tamper protection

For every file-based policy type, the agent watches the files it manages.
When a local user or process modifies or deletes one of them, the agent:

1. Rewrites the file to the Bor-managed state immediately.
2. Re-reports compliance for the affected policies.
3. Sends a tamper event to the server, including the processes (PID, command
   and user) that had the file open, if any. Tamper events appear in the
   **Audit Logs** with the action `tamper_detected`.

Watched files include the browser policy files, KConfig files under
`/etc/bor/xdg`, dconf keyfiles and locks, polkit rules, Bor package
repository files, firewalld zone files, Flatpak remote definitions under
`/etc/bor/flatpak`, the pam_time block in `/etc/security/time.conf` and the
disk-encryption dracut drop-in and `/etc/crypttab`. The complete list is in
[Files and ports](../reference/files-and-ports.md#managed-files-on-endpoints).

## Desktop notifications

Some changes only take effect after the user restarts an application or logs
in again. When enabled, the agent shows a desktop notification to logged-in
users after a Firefox, Thunderbird, Chrome/Chromium, Edge, KDE Plasma or
Package policy change. Notifications use `notify-send` in the user's session
and are rate-limited by a cooldown. You configure them, including the
message per policy type, in **Settings -> Agent Notifications**; see
[Settings](../user-guide/settings.md).

## Users, roles and access control

Access to the web interface is controlled by role-based access control
(RBAC). Users are local accounts or come from LDAP / Active Directory. Roles
are sets of permissions; the built-in roles are **Super Admin**, **Org
Admin**, **Policy Editor**, **Policy Reviewer**, **Compliance Viewer** and
**Auditor**. Roles are granted to users directly or through user groups. In
the community edition all role bindings apply to the whole installation.

See [Users, groups and roles](../user-guide/users-and-roles.md) and the
[permissions reference](../reference/permissions.md).

## Security model in brief

- Agents authenticate with a client certificate issued by the server's
  internal CA during enrollment. Certificates are valid for 90 days and the
  agent renews its certificate at startup when it expires within 30 days.
- Enrollment tokens are single-use and expire after 5 minutes.
- Deleting a node revokes its certificate; a revoked agent can no longer
  connect to the policy port.
- Every administrative change, login and tamper event is recorded in the
  audit log.
