# Bor documentation

Bor is an open-source policy management system for Linux desktops. A central
**server** stores configuration policies and delivers them in real time to
enrolled Linux endpoints. A lightweight **agent** on each endpoint receives the
policies over an encrypted gRPC stream, enforces them locally, protects the
managed files against tampering and reports compliance back to the server.

This is the official community documentation. It covers installing Bor from
distribution packages, configuring the server and the agent, and managing
policies through the web interface.

## What Bor manages

| Area | Policy type |
|---|---|
| Web browsers | [Firefox](policy-types/firefox.md), [Chrome and Chromium](policy-types/chrome.md), [Microsoft Edge](policy-types/edge.md) |
| Mail | [Thunderbird](policy-types/thunderbird.md) |
| Desktop environments | [KDE Plasma (KConfig / Kiosk)](policy-types/kconfig.md), [GNOME (dconf)](policy-types/dconf.md) |
| System privileges | [Polkit](policy-types/polkit.md), [Session access](policy-types/session-access.md) |
| Network | [Firewalld](policy-types/firewalld.md) |
| Software | [Flatpak](policy-types/flatpak.md), [Packages and repositories](policy-types/packages.md) |
| Data protection | [Disk encryption (LUKS2)](policy-types/disk-encryption.md) |

## How it fits together

```mermaid
--8<-- "diagrams/architecture-overview.mmd"
```

1. An administrator creates **policies** in the web UI and releases them.
2. Policies are **bound** to **node groups**.
3. Each enrolled **node** (an endpoint running the agent) belongs to one or
   more node groups and receives every released policy bound to them.
4. Changes reach connected agents immediately; agents that were offline
   catch up when they reconnect.

Read [Concepts](getting-started/concepts.md) for a full explanation.

## Where to start

- **New to Bor?** Follow the [Quick start](getting-started/quickstart.md) to
  get a server and a first managed desktop running.
- **Planning a deployment?** Check the [Requirements](installation/requirements.md),
  then [install the server](installation/server.md) and
  [deploy agents](installation/agent.md).
- **Managing policies?** See the [User guide](user-guide/web-ui.md) and the
  [Policy types](policy-types/index.md) reference.
- **Running Bor in production?** Read [Security hardening](security/hardening.md),
  [Backup and restore](operations/backup.md) and [Monitoring](operations/monitoring.md).

## License and community

Bor is licensed under the GNU Lesser General Public License v3.0 or later.
The source code, issue tracker and releases are on
[GitHub](https://github.com/VuteTech/Bor). The project website is
[getbor.dev](https://getbor.dev).
