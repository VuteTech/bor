# Policy types

A Bor policy always has exactly one **type**. The type decides which editor
the web UI shows, which settings you can configure and what the agent
changes on the endpoint. This page lists all twelve types, explains where
each one writes on a node and what the node needs, and then describes the
behavior that all types share: how several policies of the same type are
combined, tamper protection, compliance reporting and what happens when a
policy stops applying to a node.

## Overview

The **Type** column shows the label used in the create wizard
(**Policies -> Create a policy**). The web UI groups the types into
*Browsers & mail*, *Desktop environment* and *System & security*.

| Type | What it manages | Where it writes on the endpoint | Endpoint prerequisites |
|---|---|---|---|
| [Firefox](firefox.md) | Firefox enterprise policies: updates, privacy and telemetry, security, extensions, restrictions | `/etc/firefox/policies/policies.json`; Flatpak: `/var/lib/flatpak/extension/org.mozilla.firefox.systemconfig/<arch>/stable/policies/policies.json` | Firefox (ESR recommended) from distribution packages or Flatpak |
| [Thunderbird](thunderbird.md) | Thunderbird enterprise policies: updates, security, add-ons, network | `/etc/thunderbird/policies/policies.json`; Flatpak: `/var/lib/flatpak/extension/net.thunderbird.Thunderbird.systemconfig/<arch>/stable/policies/policies.json` | Thunderbird from distribution packages or Flatpak |
| [Chrome](chrome.md) | Managed policies for Google Chrome and Chromium | `bor_managed.json` in `/etc/opt/chrome/policies/managed`, `/etc/chromium/policies/managed`, `/etc/chromium-browser/policies/managed` and the Flatpak Chromium extension directory | Google Chrome or Chromium from packages or Flatpak |
| [Microsoft Edge](edge.md) | Managed policies for Microsoft Edge on Linux | `/etc/opt/edge/policies/managed/bor_managed.json` | Microsoft Edge for Linux |
| [KDE Plasma](kconfig.md) (Kconfig) | KDE Kiosk restrictions, desktop layout lock, System Settings module lockdown, URL restrictions, a catalogue of Plasma and KDE application settings (screen lock, session, power, appearance, privacy, Dolphin, Konsole, Kate, Spectacle, Okular, KWallet, KDE PIM, ...) or any KConfig key, a deployed Konsole profile and applications hidden from menus | `/etc/bor/xdg/*` (system-wide KDE config files), `/etc/bor/xdg/groups/<group>/*` (files of policies scoped to a Unix group), `/etc/bor/share/*` (generated look-and-feel package, Plasma scripts, Konsole profile, hidden application entries), `/etc/xdg/plasma-workspace/env/00-bor.sh`, `/etc/profile.d/99-bor.sh`; optionally `/etc/kde5rc` and a Flatpak system override | KDE Plasma 5 or 6 |
| [Dconf](dconf.md) | Mandatory GSettings keys with optional locks for GNOME and other GSettings-based software | `/etc/dconf/db/<database>.d/00-bor`, `/etc/dconf/db/<database>.d/locks/bor`, `/etc/dconf/profile/user`, then `dconf update` | `dconf` command; `gsettings` for compliance checks |
| [Polkit](polkit.md) | Which users and groups may run privileged actions | `/etc/polkit-1/rules.d/<priority>-bor-<id>.rules` (one file per policy) | polkit with JavaScript rules support |
| [Firewall](firewalld.md) (firewalld) | firewalld zone services, ports, rich rules and target | `/etc/firewalld/zones/<zone>.xml`, then a firewalld reload | firewalld installed and running |
| [Flatpak apps](flatpak.md) | Flatpak remotes, required, updated or removed applications, periodic updates | `/etc/bor/flatpak/*.flatpakrepo`, `*.filter`, `*.gpg`; the system Flatpak installation | `flatpak` |
| [Package](packages.md) | Software repositories and packages to install or remove | `bor-*` repository and key files in `/etc/apt/sources.list.d`, `/etc/apt/trusted.gpg.d`, `/etc/yum.repos.d`, `/etc/pki/rpm-gpg` or `/etc/zypp/repos.d`; packages through PackageKit | PackageKit for package installs and removals |
| [Session Access](session-access.md) | Weekly time windows during which users and groups may use the desktop | systemd-logind session lock and terminate; managed block in `/etc/security/time.conf` and the PAM stack | systemd-logind (`loginctl`); `pam_time` for the PAM layer |
| [Disk encryption](disk-encryption.md) (LUKS) | LUKS2 coverage checks, TPM2 and Tang unlocking, escrowed recovery keys | LUKS2 keyslots and tokens; `/etc/dracut.conf.d/90-bor-disk-encryption.conf` and `/etc/crypttab` options in manage mode | `cryptsetup` and LUKS2 volumes; `systemd-cryptenroll`, `clevis` and dracut depending on the protectors used |

Most paths in the table can be changed in the agent configuration file
`/etc/bor/config.yaml`. The individual pages list the keys; see also
[agent configuration](../configuration/agent.md).

!!! note "Prerequisites are checked per node"
    A policy bound to a node that lacks the software it manages does not
    fail the whole node. Depending on the type, the agent still writes its
    files (the browser types do this, so the policy is ready if the
    browser is installed later) or reports the policy as **Inapplicable**
    (for example Firewall when firewalld is not installed, Flatpak apps
    without `flatpak`, Package without PackageKit, or a Dconf key whose
    schema is not installed).

## How a policy reaches a node

Only **released** policies with at least one **enabled** binding to a node
group that contains the node are delivered to that node. When a policy is
bound to several node groups that the node belongs to, the node receives it
once, and its effective priority is the highest priority of those enabled
bindings. See [policy bindings](../user-guide/bindings.md).

The agent keeps a persistent stream to the server. Whenever a policy, a
binding or a node group membership changes, the agent re-applies the whole
set of policies of every affected type, not just the one that changed. That
makes the result independent of the order in which changes arrive.

## Merging and conflicts

Several policies of the same type can apply to one node at the same time
(for example a fleet-wide baseline plus a stricter policy for a lab). The
agent always merges them into one result per type. How conflicts are
resolved differs by type, and **binding priority is not used by every
type**:

| Type | How policies are merged | Conflicting values |
|---|---|---|
| Firefox, Thunderbird | Merged into one `policies.json`. Nested settings are merged field by field; lists (extensions to install, bookmarks, cookie allow lists and so on) are concatenated. | Binding priority is **not** used. When two policies set the same single-valued setting to different values, which one wins is not defined. |
| Chrome, Microsoft Edge | Merged into one `bor_managed.json`. Objects are merged recursively, lists are concatenated. | Binding priority is **not** used. Conflicting single values: the winner is not defined. |
| KDE Plasma | All entries from all policies are written into the same KDE config files, each key once; policies scoped to Unix groups are written into per-group files that only members receive. URL restriction rules from all policies are combined and renumbered; System Settings module restrictions are combined. | The policy with the **higher** binding priority wins (ties: the lexically smaller policy ID). The losing policy reports the overridden setting as Inapplicable with the message "Overridden by higher-priority policy ...". |
| Dconf | Entries are merged by schema, path and key. | The policy with the **higher** binding priority wins. The losing policy reports the overridden key as Inapplicable with the message "Overridden by a higher-priority policy". The database name comes from the highest-priority policy that sets one. |
| Polkit | Not merged: every policy gets its own rules file. | Files are evaluated in file-name order, and the file name starts with the binding priority, so the **lower** priority number is evaluated first. The first rule that returns a result wins. |
| Firewall | Policies targeting the same zone are merged; services, ports and rich rules are combined. | The zone target comes from the policy with the **higher** binding priority. |
| Flatpak apps | Remotes merged by name, applications by ID. | **Higher** binding priority wins. *Auto update* is on if any policy enables it. |
| Package | Repositories and packages are merged by name. | **Higher** binding priority wins for the same package or repository. |
| Session Access | Rules from all policies are combined. | A user's allowed time is the union of every rule that targets them. |
| Disk encryption | `require_*` settings and recovery-key escrow are combined with OR; the shortest non-zero rotation interval applies. | Every other setting, including the full Tang server list, comes from the **higher** priority policy. |

!!! tip "Design policies so they do not overlap"
    For the four browser types the outcome of two policies
    that set the same single-valued setting differently is not
    deterministic. Split settings so that each one is owned by exactly one
    policy (for example a baseline policy for telemetry and updates and a
    separate policy for the home page), or keep conflicting variants on
    disjoint node groups.

## Tamper protection

Every file-based policy type is watched on the endpoint. The agent keeps an
inotify watch on the directories that contain its managed files. When a
local user or administrator edits, replaces or deletes a managed file, the
agent:

1. Immediately re-applies all policies of that type, restoring the file to
   the policy-defined content.
2. Records which processes still held the file open (PID, command name and
   user), if any.
3. Reports a tamper event to the server. It appears in the
   [audit log](../user-guide/audit-log.md) with the action
   `tamper_detected` and resource type `managed_file`, the file path as the
   resource ID and the node as the actor.

Watched files per type:

| Type | Watched files |
|---|---|
| Firefox, Thunderbird | The system `policies.json` and the Flatpak copy (only while at least one policy of the type applies) |
| Chrome, Microsoft Edge | Each `bor_managed.json` (only while at least one policy of the type applies) |
| KDE Plasma | Managed files under `/etc/bor/xdg`; the two `XDG_CONFIG_DIRS` hook scripts while at least one policy applies; `/etc/kde5rc` only when `kconfig.write_kde5rc` is set and module restrictions exist |
| Dconf | `00-bor` and `locks/bor` in each database directory in use |
| Polkit | Every Bor-generated file in `/etc/polkit-1/rules.d` |
| Firewall | Bor-managed zone files in the zones directory |
| Flatpak apps | The rendered remote files in `/etc/bor/flatpak` (not Flatpak's own `/var/lib/flatpak/repo/config`) |
| Package | Bor-managed `bor-*` repository and key files |
| Session Access | `/etc/security/time.conf` while a managed block is present |
| Disk encryption | The dracut drop-in and `/etc/crypttab` in manage mode |

!!! note
    Tamper protection only works while the agent is running. The files
    stay in place when the agent is stopped, so applications keep reading
    the last applied policy, and the agent re-applies everything when it
    reconnects to the server.

## Compliance reporting

After each sync the agent reports a compliance status for every policy it
applied. The [compliance page](../user-guide/compliance.md) shows one row
per node and policy. The possible states are:

| State | Meaning |
|---|---|
| Compliant | The policy is applied as intended. |
| Non-compliant | The policy is not (fully) in effect, for example a value differs or a file is missing or modified. |
| Inapplicable | The software or feature the policy manages is not present on the node. Inapplicable results are left out of the compliance score. |
| Error | The software is present but enforcement or the check failed. |
| Unknown | The agent has not reported yet. |

What "compliant" means depends on the type:

- **Firefox, Thunderbird, Chrome, Microsoft Edge**: the agent
  reports compliant with the message "Deployed" once the managed files are
  written. It does not inspect the running application. If the write fails,
  the policy is reported non-compliant with the error message.
- **KDE Plasma**: one item per setting; compliant once written, inapplicable
  when a higher-priority policy overrides it, error when a value could not
  be written. The running session is not inspected.
- **Dconf**: every key is read back with `gsettings get` and compared with
  the policy value. Per-key results are shown in the expandable row.
- **Polkit**: the rules file on disk is compared with the expected content,
  rule by rule.
- **Firewall, Flatpak apps, Package, Session Access, Disk encryption**: the
  agent checks the live state (zone configuration, installed applications
  and packages, PAM and logind tooling, LUKS headers). See each type's page.

## When a policy stops applying

A policy stops applying to a node when you delete it, archive or unpublish
it (return it to draft), disable or delete its binding, or remove the node
from the node group. The agent then re-applies the remaining policies of
that type. If none remain, it cleans up:

| Type | Cleanup when no policy of the type applies anymore |
|---|---|
| Firefox, Thunderbird | The system `policies.json` is restored from the `policies.json.bor-backup` copy taken before Bor first wrote it; if there was no file before, it is deleted. The Flatpak copy is deleted. |
| Chrome, Microsoft Edge | `bor_managed.json` is deleted from every policy directory. Other JSON files in those directories are not touched. |
| KDE Plasma | Each managed file under `/etc/bor/xdg` (and `/etc/kde5rc` when mirrored) is restored from its `.bor-backup` copy, or deleted if it did not exist before. The `XDG_CONFIG_DIRS` hook scripts stay in place. |
| Dconf | The keyfile and locks file are emptied (written without entries) and `dconf update` runs, so the keys return to their defaults or user values. |
| Polkit | The policy's rules file is deleted. |
| Firewall | Managed zone files are restored from their backups and firewalld is reloaded. |
| Flatpak apps | Remotes Bor created are removed (unless installed applications still depend on them), adopted remotes get their original settings back and the managed files are deleted. Installed applications stay. |
| Package | Bor-managed repository and key files are removed. Installed packages stay; removing a package always requires an explicit entry. |
| Session Access | The `time.conf` managed block and Bor's `pam_time` configuration are removed; logind enforcement stops. |
| Disk encryption | No protection is removed. Keyslots, enrollments and escrowed keys stay; only Bor's dracut drop-in is withdrawn. |

!!! warning "Editing a released policy"
    The configuration of a released policy is read-only. To change it you
    **Unpublish** it, which returns it to draft. Draft policies are not
    delivered, so the nodes clean up as described above until you
    **Release** the policy again. Plan changes to important policies for a
    maintenance window, or create the new version as a separate policy,
    release it, and then retire the old one.

## Desktop notifications

For Firefox, Thunderbird, Chrome, Microsoft Edge and KDE Plasma the agent
can show a desktop notification to logged-in users after a change, asking
them to restart the application or log in again. Notifications are
controlled under **Settings -> Agent Notifications** (**Enable desktop
notifications**, **Notification cooldown (seconds)** and one message per
product). For KDE Plasma the agent also asks KWin, Plasma and the screen
locker to reload their configuration when their files changed.
