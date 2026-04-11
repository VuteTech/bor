# Polkit Policies

Bor can enforce [PolicyKit](https://www.freedesktop.org/software/polkit/docs/latest/) (polkit) rules as mandatory system policies on Linux desktops. Polkit is the standard mechanism that determines whether an unprivileged process is allowed to perform a privileged action (mounting a USB drive, managing network connections, installing packages, etc.).

## What is polkit?

Polkit provides a fine-grained privilege escalation framework. Applications that need elevated access call polkit with an **action ID** (a reverse-domain string such as `org.freedesktop.NetworkManager.wifi.enable`). Polkit evaluates a set of JavaScript rules files under `/etc/polkit-1/rules.d/` in filename order and returns a result: **Allow**, **Deny**, or **Authenticate** (as self or as administrator).

Rules are evaluated top-to-bottom, first-match-wins. Bor writes a single managed rules file whose ordering reflects the binding priorities you configure.

## Compliance states

| State | Meaning |
|---|---|
| `COMPLIANT` | The managed rules file exists and contains the expected content |
| `NON_COMPLIANT` | The rules file is missing or one or more rule blocks have been modified |
| `ERROR` | The rule JavaScript could not be generated (invalid policy content) |
| `UNKNOWN` | The agent has not yet reported compliance for this policy |

## Creating a polkit policy

1. Go to **Policies** and create a new policy with type **Polkit**.
2. Click **Add Rule** for each polkit rule to enforce:
   - **Description** — a human-readable label rendered as a JavaScript comment in the rules file.
   - **Exact Action IDs** — the action must match one of these IDs exactly (typeahead from the catalogue).
   - **Action Prefixes** — the action ID must start with one of these strings; useful for covering a whole subsystem (e.g. all NetworkManager actions via `org.freedesktop.NetworkManager.`).
   - **Subject filter** — optionally restrict the rule to users in (or not in) a specific Unix group, a specific username, local console sessions, or active sessions.
   - **Result** — what polkit returns when the rule matches (see table below).
3. Release the policy and create an enabled binding to a node group. The **binding priority** controls the file name and evaluation order (see below).

At least one **Exact Action ID** or **Action Prefix** must be set per rule.

### Result values

| Result | Polkit constant | Effect |
|---|---|---|
| Deny | `NO` | Action is denied unconditionally |
| Allow | `YES` | Action is allowed unconditionally |
| Ask user to authenticate | `AUTH_SELF` | User must authenticate with their own password |
| Ask user to authenticate (cached) | `AUTH_SELF_KEEP` | Same, but the authentication is cached for the session |
| Ask administrator to authenticate | `AUTH_ADMIN` | An administrator must authenticate |
| Ask administrator to authenticate (cached) | `AUTH_ADMIN_KEEP` | Same, but cached |

### Subject filter fields

| Field | Description |
|---|---|
| **Group** | Matches users who are (or are not) a member of the specified Unix group |
| **Specific user** | Matches one named user only |
| **Require local console session** | Rule applies only when the session is attached to a local console (not SSH or remote display) |
| **Require active session** | Rule applies only when the session is the currently active one on the seat |
| **System unit** | Matches a specific systemd service unit (`subject.system_unit`) — use for daemon-to-daemon calls |

When multiple conditions are set they are combined with `&&` (all must hold). Leaving all fields blank matches every subject.

## Example policies

### 1 — Allow all users to enable and disable Wi-Fi

Members of the `users` group may toggle Wi-Fi without any password prompt.

| Field | Value |
|---|---|
| Description | Allow users to enable/disable Wi-Fi |
| Exact Action IDs | `org.freedesktop.NetworkManager.wifi.enable` |
| Action Prefixes | *(none)* |
| Group | In group `users` |
| Result | Allow |

### 2 — Require admin auth for software installation (PackageKit)

All PackageKit install/remove/update actions require an administrator password, for all users.

| Field | Value |
|---|---|
| Description | Require admin auth for package management |
| Exact Action IDs | *(none)* |
| Action Prefixes | `org.freedesktop.packagekit.` |
| Group | Everyone |
| Result | Ask administrator to authenticate (cached) |

### 3 — Block USB storage mounting for non-developers

Members of the `developers` group may mount USB storage without a password. Everyone else is denied.

Two rules are needed — the allow rule must have a **higher binding priority** so it is written first in the rules file and is evaluated first (first-match-wins):

**Rule A — priority 100** (high):

| Field | Value |
|---|---|
| Description | Allow developers to mount USB storage |
| Exact Action IDs | `org.freedesktop.udisks2.filesystem-mount` |
| Group | In group `developers` |
| Result | Allow |

**Rule B — priority 10** (low):

| Field | Value |
|---|---|
| Description | Deny USB storage mounting for everyone else |
| Exact Action IDs | `org.freedesktop.udisks2.filesystem-mount` |
| Group | Everyone |
| Result | Deny |

### 4 — Restrict shutdown and reboot to local active sessions only

Users connected via SSH or a remote display may not shut down or reboot the machine.

| Field | Value |
|---|---|
| Description | Restrict power actions to local active sessions |
| Action Prefixes | `org.freedesktop.login1.` |
| Require local console session | ✓ |
| Require active session | ✓ |
| Result | Allow |

Add a second rule with the same prefix, no subject filter, and **Result = Deny** at a lower priority to block remote sessions.

### 5 — Allow a service to call a privileged D-Bus method

A custom system service (`com.example.daemon.service`) needs to call a privileged action. Identify it by its systemd unit name.

| Field | Value |
|---|---|
| Description | Allow example daemon to perform privileged action |
| Exact Action IDs | `com.example.privileged-action` |
| System unit | `com.example.daemon.service` |
| Result | Allow |

## Priority and file ordering

Each polkit policy gets its own rules file under `/etc/polkit-1/rules.d/`. The filename is derived from the **binding priority**: `<priority>-bor-<id>.rules` (priority is zero-padded to three digits).

polkitd evaluates rules files in **ascending filename order**, so a lower priority number means the file is loaded first and its rules win in first-match-wins evaluation:

| Binding priority | Filename | Evaluation order | Authority |
|---|---|---|---|
| 10 | `010-bor-<id>.rules` | First | Highest — wins over all others |
| 50 | `050-bor-<id>.rules` | Middle | Standard |
| 90 | `090-bor-<id>.rules` | Last | Lowest — falls through if a lower number matched first |

This is the opposite of dconf (where a higher priority number wins). For polkit, set a **lower priority number** for the policies that should take precedence.

When a policy's binding priority changes, the agent automatically removes the old file and writes a new one with the updated filename.

When two policies have the same binding priority their files appear side-by-side in alphabetical order by policy ID — avoid assigning the same priority to policies with overlapping action IDs.

## Compliance dashboard

The **Compliance** page shows one row per (node, policy) pair. Expand a row to see per-rule compliance:

| Column | Description |
|---|---|
| Key | The rule description from the policy |
| Status | `COMPLIANT` / `NON_COMPLIANT` / `ERROR` with colour coding |
| Details | For non-compliant rules: path of the expected rules file |

## Operational notes

- **polkitd hot-reload** — polkitd monitors `/etc/polkit-1/rules.d/` via inotify and picks up changes immediately. No explicit reload command is needed after the agent writes a rules file.
- **Tamper protection** — the agent monitors all its managed rules files via inotify. If a file is modified or deleted by a local administrator, the agent detects the change within milliseconds, restores the file to the policy-enforced content, and reports the tamper event to the server.
- **File naming** — each policy gets a file named `<priority>-bor-<shortID>.rules` (e.g. `050-bor-a1b2c3d4.rules`). The priority is zero-padded to three digits. When a binding priority changes, the agent removes the old file and writes the new one automatically.
- **Third-party rules files** — set the binding priority above the numeric values used by distribution-provided files (typically `49-`) to prevent Bor rules from being shadowed, or below `99-` to let local administrator overrides take effect.
- **Action catalogue freshness** — the catalogue seeded into the server comes from `server/assets/polkit_builtin_actions.json`. The agent also reports actions discovered at runtime via `pkaction --verbose`. Run `make update-polkit-actions` on a reference system and commit the result to refresh the built-in catalogue.
- **Flatpak and sandboxed apps** — Flatpak apps run inside a sandbox and typically do not trigger polkit actions directly. Host polkit rules do not affect in-sandbox behaviour.
- **SELinux on RHEL / Fedora** — writing to `/etc/polkit-1/rules.d/` from a root system service may require a custom SELinux policy module if the agent is confined.
- **Empty policy** — a released polkit policy with zero rules does nothing and produces an empty rules file. At least one rule is required before the policy editor allows saving.
