# Files and ports

This page lists every file, directory, port, systemd unit and user that the
Bor server and agent use, including the files the agent manages on desktops
for each policy type. Paths are the defaults; where a setting can change a
path, the setting is named.

## Server

### Files and directories

| Path | Owner / mode | Purpose | Setting |
|------|--------------|---------|---------|
| `/usr/bin/bor-server` | root, `0755` | Server binary. | - |
| `/etc/bor/server.yaml` | `root:bor`, `0640` | Main configuration. Contains the JWT secret, possibly the database password and the initial admin password. Preserved on upgrade. | `BOR_CONFIG` (environment) |
| `/etc/bor/server.env` | root, `0600` recommended | Optional environment file read by the systemd unit. Environment variables override `server.yaml`. Not created by the package. | - |
| `/lib/systemd/system/bor-server.service` | root, `0644` | systemd unit. | - |
| `/var/lib/bor/pki/` | `bor:bor`, `0750` | PKI root directory. | - |
| `/var/lib/bor/pki/ca/ca.crt` | `bor` | Internal CA certificate (ECDSA P-384, valid 10 years). Also served at `/agent/ca.crt`. | `ca.autogen_dir`, `ca.cert_file` |
| `/var/lib/bor/pki/ca/ca.key` | `bor` | Internal CA private key. Signs agent certificates. Protect and back up. | `ca.autogen_dir`, `ca.key_file` |
| `/var/lib/bor/pki/ui/ui.crt`, `ui.key` | `bor` | Auto-generated server TLS certificate (ECDSA P-256, valid 1 year, signed by the internal CA). Used on both ports unless you configure your own certificate. | `tls.autogen_dir`, `tls.cert_file`, `tls.key_file` |
| `/var/lib/bor/acme/` | `bor:bor`, `0750` | ACME account and certificate cache. | `acme.cache_dir` |
| `/var/lib/bor/initial_admin_password` | `bor`, `0600` | Only written when no users exist and no initial admin password is configured. Delete after use. | - |
| `/usr/share/bor/agent-repo/` | root | Agent packages, apt/dnf repositories and `manifest.json`, served at `/agent/`. Replaced on every server upgrade. | `agent_repo.dir` |
| `/etc/bor/escrow.kek` (example) | `bor`, `0600` | Key-encryption key for disk-encryption recovery-key escrow. Only if configured. | `disk_encryption.escrow_kek_file` |
| `/etc/bor/krb5.keytab` (example) | `bor`, readable | Service keytab for Kerberos agent enrollment. Only if configured. | `kerberos.keytab_file` |

The packaged systemd unit runs with `ProtectSystem=strict` and allows writes
only to `/var/lib/bor/pki` and `/var/lib/bor/acme`.

### Database

All other state lives in the PostgreSQL database (default database and role
`bor`): policies, bindings, nodes, node groups, users, roles, settings,
compliance results, audit logs, enrollment tokens, the policy revision
counter and escrowed disk-encryption recovery keys.

### Ports

| Port | Bind address | Protocol | Clients | Setting |
|------|--------------|----------|---------|---------|
| 8443 (default); 443 recommended for production | all interfaces | HTTPS (TLS 1.2+, HTTP/2) and gRPC; with ACME also the TLS-ALPN-01 challenge (only useful on 443) | Browsers, REST API clients, enrolling agents, package managers | `server.enrollment_port` / `BOR_ENROLLMENT_PORT`. See [Choose the web port](../installation/server.md#step-6-choose-the-web-port). |
| 8444 | all interfaces | gRPC over TLS 1.3 with mandatory client certificates | Enrolled agents | `server.policy_port` / `BOR_POLICY_PORT` |
| 9090 | `127.0.0.1` | HTTP (HTTPS if a certificate is set) | Prometheus | `metrics.listen_addr` / `BOR_METRICS_ADDR` |
| 80 | all interfaces | HTTP | ACME CA (HTTP-01 challenge) | `acme.http_port`, only when `acme.enabled` |

`server.address` (`BOR_ADDRESS`) restricts the UI and policy ports to one
address; empty means all interfaces.

Outbound connections depend on configuration: PostgreSQL (5432 when remote),
LDAP (389 / 636), syslog audit forwarding, the ACME CA, and HTTPS downloads
of Flatpak repository catalogs.

### Service and user

| Item | Value |
|------|-------|
| systemd unit | `bor-server.service` |
| Runs as | user `bor`, group `bor` (system account, home `/var/lib/bor`, no login shell) |
| Capabilities | `CAP_NET_BIND_SERVICE` only |
| Log | journald, identifier `bor-server` |
| Depends on | `postgresql.service` (ordering and `Wants=`) |

## Agent

### Files and directories

| Path | Owner / mode | Purpose | Setting |
|------|--------------|---------|---------|
| `/usr/bin/bor-agent` | root, `0755` | Agent binary. | - |
| `/etc/bor/config.yaml` | root, `0600` | Agent configuration. Preserved on upgrade. | `--config` |
| `/lib/systemd/system/bor-agent.service` | root, `0644` | systemd unit. | - |
| `/var/lib/bor/agent/` | root, `0750` | Enrollment data directory. | `enrollment.data_dir` |
| `/var/lib/bor/agent/agent.crt` | root, `0644` | Agent client certificate (valid 90 days, renewed at startup within 30 days of expiry). | - |
| `/var/lib/bor/agent/agent.key` | root, `0600` | Agent private key. | - |
| `/var/lib/bor/agent/ca.crt` | root, `0644` | Server CA certificate received during enrollment; used to verify the policy port. | - |
| `/var/lib/bor/agent/flatpak-state.json` | root | Remotes Bor created or adopted, used to undo changes. | `flatpak.state_file` |
| `/var/lib/bor/agent/luks-state.json` | root | Disk-encryption rotation progress (never contains keys). | `disk_encryption.state_file` |
| `/etc/bor/luks-bootstrap/` | root | One-time LUKS provisioning credentials placed by an installer; wiped after adoption. | `disk_encryption.bootstrap_dir` |
| `/run/bor/luks/` | root, `0700` | Work directory and lock file for disk-encryption operations. | `disk_encryption.run_dir` |
| `/etc/bor/enroll-token` | root, `0600` | Temporary token file used by the deploy script; deleted after enrollment. | `--token-file` |

### Service and user

| Item | Value |
|------|-------|
| systemd unit | `bor-agent.service` |
| Runs as | root |
| Restart | `on-failure`, after 30 seconds; stop timeout 120 seconds |
| Log | journald, identifier `bor-agent` |

### Network

The agent only makes outbound connections:

| Destination | Purpose | Setting |
|-------------|---------|---------|
| Server web port (default 8443, 443 recommended) | Enrollment (token or Kerberos), agent package repository | `server.address`, `server.enrollment_port` (must equal the server's) |
| Server port 8444 | Policy stream, compliance reports, certificate renewal | `server.address`, `server.policy_port` |
| KDC (88) | Kerberos enrollment only | `kerberos.kdc` or `/etc/krb5.conf` |

Package and Flatpak policies make the endpoint's package tools contact the
repositories defined in those policies.

## Managed files on endpoints

These are the files the agent writes for each policy type. "Watched" means
the file watcher restores the file immediately when it is modified or
deleted outside Bor, and reports a tamper event.

When the agent replaces an existing file, it first saves the original as
`<file>.bor-backup` (an empty backup means no original existed). When the
last policy of that type is removed from the node, the original is restored
or the file deleted.

| Policy type | Path | Watched | Setting |
|-------------|------|---------|---------|
| Firefox | `/etc/firefox/policies/policies.json` | Yes | `firefox.policies_path` |
| Firefox (Flatpak) | `/var/lib/flatpak/extension/org.mozilla.firefox.systemconfig/<arch>/stable/policies/policies.json` | Yes | `firefox.flatpak_policies_path` |
| Thunderbird | `/etc/thunderbird/policies/policies.json` | Yes | `thunderbird.policies_path` |
| Thunderbird (Flatpak) | `/var/lib/flatpak/extension/net.thunderbird.Thunderbird.systemconfig/<arch>/stable/policies/policies.json` | Yes | `thunderbird.flatpak_policies_path` |
| Chrome | `/etc/opt/chrome/policies/managed/bor_managed.json` | Yes | `chrome.chrome_policies_path` |
| Chromium | `/etc/chromium/policies/managed/bor_managed.json` | Yes | `chrome.chromium_policies_path` |
| Chromium (Debian/Ubuntu) | `/etc/chromium-browser/policies/managed/bor_managed.json` | Yes | `chrome.chromium_browser_policies_path` |
| Chromium (Flatpak) | `/var/lib/flatpak/extension/org.chromium.Chromium.Extension.system-policies/<arch>/1/policies/managed/bor_managed.json` | Yes | `chrome.flatpak_chromium_policies_path` |
| Microsoft Edge | `/etc/opt/edge/policies/managed/bor_managed.json` | Yes | `edge.edge_policies_path` |
| KDE Plasma (KConfig) | Configuration files under `/etc/bor/xdg/` | Yes | `kconfig.config_path` |
| KDE Plasma (KConfig) | Group-scoped configuration files under `/etc/bor/xdg/groups/<group>/` | Yes | `kconfig.config_path` |
| KDE Plasma (KConfig) | Generated look-and-feel packages, Plasma update scripts, Konsole profiles (`konsole/`) and hidden application entries (`applications/`) under `/etc/bor/share/` (first in `XDG_DATA_DIRS`); group-scoped ones under `/etc/bor/share/groups/<group>/` | Yes | `kconfig.data_path` |
| KDE Plasma (KConfig) | Flatpak system override exposing `/etc/bor/xdg` to sandboxed applications (`flatpak override --system`) | No | `kconfig.flatpak_override` |
| KDE Plasma (KConfig) | `/etc/xdg/plasma-workspace/env/zz-bor.sh` and `/etc/profile.d/99-bor.sh` (put `/etc/bor/xdg` first in `XDG_CONFIG_DIRS` and `/etc/bor/share` in `XDG_DATA_DIRS`) | Yes (while a policy applies) | - |
| KDE Plasma (KConfig) | `/etc/kde5rc` (only with `kconfig.write_kde5rc` and KCM restrictions) | Yes | `kconfig.write_kde5rc` |
| Dconf | `/etc/dconf/db/<db>.d/00-bor` and `/etc/dconf/db/<db>.d/locks/bor` (`<db>` defaults to `local`) | Yes | policy setting |
| Dconf | `/etc/dconf/profile/user` (ensures a `system-db:<db>` line) | No | - |
| Polkit | `/etc/polkit-1/rules.d/<priority>-bor-<id>.rules` | Yes | - |
| Firewall | Zone files `/etc/firewalld/zones/<zone>.xml` | Yes | `firewalld.zones_dir` |
| Services & timers | Drop-ins `/etc/systemd/system/<unit>.d/90-bor.conf` (one per unit with overrides) | Yes | `systemd.system_dir` |
| Services & timers | Task units `/etc/systemd/system/bor-<task>.service` and `bor-<task>.timer` | Yes | `systemd.system_dir` |
| Services & timers | Task scripts `/etc/bor/systemd/<task>.sh` (the directory belongs to Bor) | Yes | `systemd.scripts_dir` |
| Services & timers | `/etc/systemd/system-preset/10-bor.preset` (optional, policies with the preset switch) | Yes | `systemd.preset_dir` |
| Services & timers | `/var/lib/bor/agent/systemd-state.json` (original unit states, restored when a unit leaves every policy) | Yes | `systemd.state_path` |
| SELinux | The `SELINUX=` line of `/etc/selinux/config` | Yes | `selinux.config_path` |
| SELinux | CIL sources `/etc/bor/selinux/bor-*.cil` of the modules Bor installs at semodule priority 450 | Yes | `selinux.modules_dir`, `selinux.module_priority` |
| SELinux | `/var/lib/bor/agent/selinux-state.json` (original mode, boolean values, module flags and login mappings, restored when they leave every policy) | Yes | `selinux.state_path` |
| AppArmor | Symlinks `/etc/apparmor.d/disable/<file>` and `/etc/apparmor.d/force-complain/<file>`, profiles `/etc/apparmor.d/bor-<name>`, the Bor block in `/etc/apparmor.d/local/<file>`, `/etc/apparmor.d/tunables/home.d/bor`; with vendor flag edits, the complain flag of a vendor profile file | Yes | `apparmor.profiles_dir` |
| AppArmor | `/etc/sysctl.d/zz-bor-apparmor.conf` (user namespace switches) | Yes | `apparmor.sysctl_path` |
| AppArmor | `/var/lib/bor/agent/apparmor-state.json` and backups of edited vendor files under `/var/lib/bor/agent/apparmor-backups/` | Yes | `apparmor.state_path`, `apparmor.backup_dir` |
| Services & timers | Drop-ins `/etc/systemd/user/<unit>.d/90-bor.conf`, task units `/etc/systemd/user/bor-<task>.service` and `.timer`, and the enablement symlinks and masks of entries scoped to all users | Yes (files Bor writes) | `systemd.user_dir` |
| Services & timers | `/etc/systemd/user-preset/10-bor.preset` (optional, policies with the preset switch and units scoped to all users) | Yes | `systemd.user_preset_dir` |
| Package (apt) | `/etc/apt/sources.list.d/bor-<id>.sources`, `/etc/apt/trusted.gpg.d/bor-<id>.gpg` | Yes | - |
| Package (dnf) | `/etc/yum.repos.d/bor-<id>.repo`, `/etc/pki/rpm-gpg/bor-<id>.gpg` | Yes | - |
| Package (zypper) | `/etc/zypp/repos.d/bor-<id>.repo`, `/etc/pki/rpm-gpg/bor-<id>.gpg` | Yes | - |
| Flatpak apps | `/etc/bor/flatpak/*.flatpakrepo`, `*.filter`, `*.gpg` | Yes | `flatpak.state_dir` |
| Session Access | Bor-managed block in `/etc/security/time.conf` | Yes | - |
| Session Access (Debian/Ubuntu) | `/usr/share/pam-configs/bor-session-access` (enabled with `pam-auth-update`) | No | - |
| Disk encryption | `/etc/dracut.conf.d/90-bor-disk-encryption.conf`, `/etc/crypttab` (Bor's options) | Yes | - |

`<arch>` is the Flatpak architecture name (`x86_64`, `aarch64`, ...). Set
a Chrome/Chromium path or a Flatpak browser path to `""` in
`/etc/bor/config.yaml` to stop writing that variant. Flatpak's own `/var/lib/flatpak/repo/config` is changed through the
`flatpak` command and is deliberately not watched.
