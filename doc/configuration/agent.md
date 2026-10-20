# Agent configuration

This page is the complete reference for the Bor agent configuration file
`/etc/bor/config.yaml`. It explains how the agent finds the server, how it
verifies the server during and after enrollment, where it keeps its
credentials, and where each policy type writes its files.

## How the configuration is loaded

- The agent reads `/etc/bor/config.yaml` at startup. Use `--config <path>`
  to read another file. Unlike the server, the agent **requires** the file:
  a missing or unreadable file stops it with `failed to read config file`.
- Every key is optional. A key that is absent keeps its built-in default.
  The defaults are listed below.
- The agent has no environment variables for configuration. The only
  environment variable it reads is `BOR_ENROLLMENT_TOKEN` (see
  [Agent installation](../installation/agent.md)).
- Changes take effect after a restart:

    ```bash
    sudo systemctl restart bor-agent
    ```

The `bor-agent` package installs the file with mode `0600` (root only) and
marks it as a configuration file, so upgrades never overwrite your edits.

## server

How the agent reaches the Bor server.

| Key | Default | Description |
|-----|---------|-------------|
| `server.address` | `localhost` | Host name or IP address of the Bor server, without a port. Used for both ports. The host name must match the server certificate (see [Verification](#how-the-agent-verifies-the-server)). |
| `server.enrollment_port` | `8443` | The server's web port, used only for enrollment and re-enrollment. Must match the server's `server.enrollment_port`; set `443` when the server uses the recommended production port. |
| `server.policy_port` | `8444` | Port for the policy stream and certificate renewal. Must match the server's `server.policy_port`. |
| `server.insecure_skip_verify` | `false` | Skip verification of the server certificate **during enrollment only**. The packaged `config.yaml` sets this to `true`; see below. |
| `server.ca_cert` | `""` | Accepted in the file but currently not used by the agent. Trust the server's CA through the system trust store instead. |

```yaml
server:
  address: "bor.example.com"
  enrollment_port: 443
  policy_port: 8444
  insecure_skip_verify: false
```

### How the agent verifies the server

The agent connects to the server in two phases, and each phase verifies
the server differently:

1. **Enrollment (web port, `enrollment_port`).** The agent has no credentials yet. With
   `insecure_skip_verify: false` it checks the server certificate against
   the **operating system trust store** and the name in `server.address`.
   This succeeds when the web UI uses a publicly trusted (ACME) certificate,
   or when you have added the Bor CA to the trust store of the endpoint.
   With `insecure_skip_verify: true` the agent accepts any certificate for
   this one connection and logs
   `WARNING: server certificate verification disabled for enrollment`.
2. **Normal operation (port 8444).** Enrollment returns the server's CA
   certificate, which the agent stores as `ca.crt` in `data_dir`. From then
   on the agent always verifies the policy port against that CA, whatever
   `insecure_skip_verify` says. It never skips verification after
   enrollment.

`insecure_skip_verify: true` is convenient for a first test, but someone
able to intercept the enrollment connection could hand the agent a
different CA. For production, add the Bor CA to the endpoint's trust store
and set `insecure_skip_verify: false`. The deployment dialog in the web UI
generates exactly these commands; the manual equivalent is in
[TLS: Trust the Bor CA on endpoints](tls.md#trust-the-bor-ca-on-endpoints).

!!! note "The shipped default"
    The `config.yaml` installed by the package contains
    `insecure_skip_verify: true` so that an enrollment against a server with
    the auto-generated certificate works out of the box. The built-in
    default when the key is missing is `false`.

## agent

| Key | Default | Description |
|-----|---------|-------------|
| `agent.client_id` | the host name | Name of this node in Bor. It is sent at enrollment and becomes the node name and the Common Name of the agent certificate. Empty means the output of `hostname`. |

Set `client_id` when host names are not unique or not meaningful, for
example on cloned virtual machines. The value is used only at enrollment;
changing it afterwards does not rename an enrolled node. To change the name,
re-enroll the agent.

## enrollment

| Key | Default | Description |
|-----|---------|-------------|
| `enrollment.data_dir` | `/var/lib/bor/agent` | Directory for the agent's credentials. |

After enrollment `data_dir` contains:

| File | Mode | Content |
|------|------|---------|
| `agent.key` | `0600` | The agent's private key (ECDSA P-256), generated on the endpoint. It never leaves the machine. |
| `agent.crt` | `0644` | Client certificate signed by the Bor CA, valid 90 days. |
| `ca.crt` | `0644` | The Bor CA certificate, used to verify the policy port. |

The agent considers itself enrolled when all three files exist. Running
`bor-agent --token-file <file>` on an enrolled node deletes the three files
and enrolls again. Other agent state, such as `flatpak-state.json`,
`systemd-state.json` and `luks-state.json`, also lives in
`/var/lib/bor/agent` by default.

!!! warning
    Back up or protect `agent.key` like any private key. Anyone who copies
    the three files can impersonate the node until its certificate is
    revoked. See [Nodes](../user-guide/nodes.md) for revoking a certificate.

## kerberos

Token-free enrollment for hosts joined to FreeIPA or Active Directory.
Complete setup steps are in [Kerberos enrollment](kerberos.md).

| Key | Default | Description |
|-----|---------|-------------|
| `kerberos.enabled` | `false` | Try Kerberos enrollment when the agent is not enrolled. |
| `kerberos.keytab_file` | `/etc/krb5.keytab` | Machine keytab. If the file does not exist, Kerberos is skipped. |
| `kerberos.service_principal` | `""` | Bor service principal, for example `HTTP/bor.example.com@EXAMPLE.COM`. Required; Kerberos is skipped while it is empty. Its realm is the realm the agent uses. |
| `kerberos.kdc` | `""` | KDC host name or IP. When set, the agent ignores `/etc/krb5.conf` and builds its Kerberos configuration from this KDC and the realm of `service_principal`. |
| `kerberos.machine_principal` | `host/<hostname>` | Principal of this host in the keytab. Active Directory hosts usually need `HOSTNAME$`. A realm suffix (`@REALM`) is accepted and ignored. |

## firefox

| Key | Default | Description |
|-----|---------|-------------|
| `firefox.policies_path` | `/etc/firefox/policies/policies.json` | Policy file for Firefox installed from distribution packages. |
| `firefox.flatpak_policies_path` | `/var/lib/flatpak/extension/org.mozilla.firefox.systemconfig/<arch>/stable/policies/policies.json` | Policy file for the Firefox Flatpak. Empty disables it. |

## thunderbird

| Key | Default | Description |
|-----|---------|-------------|
| `thunderbird.policies_path` | `/etc/thunderbird/policies/policies.json` | Policy file for Thunderbird installed from distribution packages. |
| `thunderbird.flatpak_policies_path` | `/var/lib/flatpak/extension/net.thunderbird.Thunderbird.systemconfig/<arch>/stable/policies/policies.json` | Policy file for the Thunderbird Flatpak. Empty disables it. |

## chrome

The agent writes `bor_managed.json` into each directory that is set.

| Key | Default | Description |
|-----|---------|-------------|
| `chrome.chrome_policies_path` | `/etc/opt/chrome/policies/managed` | Google Chrome, all channels. |
| `chrome.chromium_policies_path` | `/etc/chromium/policies/managed` | Chromium built from upstream (for example Arch Linux, Fedora). |
| `chrome.chromium_browser_policies_path` | `/etc/chromium-browser/policies/managed` | Chromium from the Debian/Ubuntu `chromium-browser` package. |
| `chrome.flatpak_chromium_policies_path` | `/var/lib/flatpak/extension/org.chromium.Chromium.Extension.system-policies/<arch>/1/policies/managed` | Chromium Flatpak. |

Set a path to `""` to stop writing to that location.

## edge

| Key | Default | Description |
|-----|---------|-------------|
| `edge.edge_policies_path` | `/etc/opt/edge/policies/managed` | Directory for `bor_managed.json`. Covers all Microsoft Edge channels. Empty disables Edge policies. |

## kconfig

| Key | Default | Description |
|-----|---------|-------------|
| `kconfig.config_path` | `/etc/bor/xdg` | Base directory for KDE configuration files written by KConfig policies. The agent puts it first in `XDG_CONFIG_DIRS` through `/etc/xdg/plasma-workspace/env/zz-bor.sh` and `/etc/profile.d/99-bor.sh`. |
| `kconfig.data_path` | `/etc/bor/share` | Directory for generated data files: the look-and-feel package (wallpaper, panel layout) and Plasma update scripts of the Desktop Layout feature. Put first in `XDG_DATA_DIRS` by the same hook scripts. The whole directory is managed by the agent. |
| `kconfig.write_kde5rc` | `false` | Also mirror System Settings module restrictions into `/etc/kde5rc`. Plasma 5 and 6 read them from `kdeglobals`; enable only for very old KDE Frameworks releases. |
| `kconfig.probe_effective` | `false` | After each sync, read the written settings back with `kreadconfig6`/`kreadconfig5` in a logged-in user's session and report user overrides of non-enforced defaults as non-compliant; also checks that the Plasma session has the managed directory in `XDG_CONFIG_DIRS`. Read-only; skipped without a graphical session. |
| `kconfig.flatpak_override` | `true` | While KDE Plasma policies apply and `flatpak` is installed, keep a system-wide Flatpak override that exposes the config directory to sandboxed applications and prepends it to their `XDG_CONFIG_DIRS`, so KDE applications from Flathub read the managed settings. Removed when no policy remains. |

## firewalld

| Key | Default | Description |
|-----|---------|-------------|
| `firewalld.zones_dir` | `/etc/firewalld/zones` | Directory where managed zone XML files are written before firewalld is reloaded. |

## systemd

| Key | Default | Description |
|-----|---------|-------------|
| `systemd.system_dir` | `/etc/systemd/system` | Directory where the `<unit>.d/90-bor.conf` drop-ins and the `bor-<task>.service` / `.timer` units of Services & timers policies are written. Tamper-protected. |
| `systemd.preset_dir` | `/etc/systemd/system-preset` | Directory of the optional `10-bor.preset` file. Tamper-protected. |
| `systemd.scripts_dir` | `/etc/bor/systemd` | Scripts of Bor-deployed tasks. Belongs to Bor; every file in it is managed and tamper-protected. |
| `systemd.state_path` | `/var/lib/bor/agent/systemd-state.json` | Record of each managed unit's original enablement and activity, restored when the unit leaves every policy. |
| `systemd.job_timeout` | `90s` | Maximum wait for a start, stop or restart job. |
| `systemd.reconcile_interval` | `10m` | Interval of the pass that re-asserts the enablement and activity of every managed unit while a policy applies; `0` disables it. |
| `systemd.watch_signals` | `true` | Follow systemd's D-Bus signals so a managed unit changed by hand is reconciled within seconds; the periodic pass remains the backstop. |
| `systemd.allow_tasks` | `true` | Deploy the scheduled tasks of bound policies (scripts that run as root). `false` reports every task as an error and removes task files deployed earlier. |
| `systemd.analyze_binary` | `systemd-analyze` | Executable used to validate calendar expressions before a timer drop-in is written and to verify task unit files, looked up in `PATH` unless absolute. Also computes the sandboxing exposure of every loaded service for the unit inventory (`systemd-analyze security`). |
| `systemd.user_dir` | `/etc/systemd/user` | Directory where the drop-ins and task units of policy entries scoped to **All users** are written and where their enablement symlinks are created, as `systemctl --global` does. Tamper-protected. |
| `systemd.user_preset_dir` | `/etc/systemd/user-preset` | Directory of the optional `10-bor.preset` file for user managers. Tamper-protected. |
| `systemd.allow_actions` | `true` | Run the start, stop and restart actions requested from the node details of the console (holders of `systemd:operate`). `false` answers every action with an error. |

## selinux

| Key | Default | Description |
|-----|---------|-------------|
| `selinux.config_path` | `/etc/selinux/config` | Configuration file whose `SELINUX=` line SELinux policies rewrite. Tamper-protected while a policy applies. |
| `selinux.modules_dir` | `/etc/bor/selinux` | CIL sources of the modules Bor installs. Tamper-protected. |
| `selinux.module_priority` | `450` | `semodule -X` priority of Bor modules, apart from the distribution's (100) and the administrator's (400). |
| `selinux.state_path` | `/var/lib/bor/agent/selinux-state.json` | Record of every original value (mode, booleans, module flags, login mappings), restored when it leaves every policy. |
| `selinux.reconcile_interval` | `5m` | Interval of the pass that re-asserts the mode, booleans and modules while a policy applies; `0` disables it. |
| `selinux.allow_mode_change` | `true` | Let policies switch between enforcing and permissive. |
| `selinux.allow_custom_modules` | `true` | Install Bor modules (custom modules, permissive domains, file contexts). |
| `selinux.follow_audit` | `true` | Follow the audit records (`MAC_STATUS`, `MAC_CONFIG_CHANGE`, `MAC_POLICY_LOAD`) so a manual change is reconciled within seconds. |
| `selinux.tool_timeout` | `5m` | Maximum run time of one `semodule` or `setsebool` call. |

## apparmor

| Key | Default | Description |
|-----|---------|-------------|
| `apparmor.profiles_dir` | `/etc/apparmor.d` | Profile directory: Bor profiles, local override blocks, the home tunable and the `disable/` and `force-complain/` links live under it. Tamper-protected. |
| `apparmor.parser_binary` | `apparmor_parser` | Parser executable, looked up in `PATH`, `/usr/sbin` and `/sbin` unless absolute. |
| `apparmor.sysctl_path` | `/etc/sysctl.d/zz-bor-apparmor.conf` | sysctl file for the user namespace switches. Tamper-protected. |
| `apparmor.state_path` | `/var/lib/bor/agent/apparmor-state.json` | Record of every original state, restored when it leaves every policy. |
| `apparmor.backup_dir` | `/var/lib/bor/agent/apparmor-backups` | Backups of vendor profile files whose mode flags a policy edited. |
| `apparmor.reconcile_interval` | `5m` | Interval of the pass that re-asserts links, loaded modes and sysctls; `0` disables it. |
| `apparmor.allow_custom_profiles` | `true` | Write Bor profiles and local overrides. |
| `apparmor.allow_vendor_flag_edits` | `false` | Let policies strip the complain flag inside vendor profile files (package conffiles). |
| `apparmor.follow_audit` | `true` | Follow the kernel's AppArmor status records so a manual profile change is reconciled within seconds. |
| `apparmor.tool_timeout` | `2m` | Maximum run time of one `apparmor_parser` call. |

## mac

Settings shared by the SELinux and AppArmor types.

| Key | Default | Description |
|-----|---------|-------------|
| `mac.inventory_interval` | `1h` | How often the node re-sends its mandatory access control inventory (status, booleans or profiles, denial summary) while connected. |
| `mac.denial_window` | `24h` | How far back the denial summary looks in the journal. |
| `mac.collect_denials` | `true` | Read the journal's audit records for the denial summary. Records are aggregated and redacted on the node. |
| `mac.allow_actions` | `true` | Answer proposal requests (`audit2allow`, AppArmor rules from denials) from the node page, for holders of `mac:operate`. |

## updates

| Key | Default | Description |
|-----|---------|-------------|
| `updates.state_path` | `/var/lib/bor/agent/updates-state.json` | Record of every original state (engine configuration, timer state, locks, the pending reboot), restored when the node leaves every policy. |
| `updates.backup_dir` | `/var/lib/bor/agent/updates-backups` | Backups of the engine configuration files Bor replaced. |
| `updates.reconcile_interval` | `10m` | Interval of the pass that re-asserts the timer and the configuration; `0` disables it. |
| `updates.inventory_interval` | `1h` | How often the node re-sends its update state (engine, pending updates, reboot) while connected. |
| `updates.reboot_check_interval` | `1m` | How often the reboot rule runs while a policy applies. |
| `updates.tool_timeout` | `10m` | Maximum run time of one engine or package manager call. |
| `updates.allow_install` | `true` | Let policies install the engine package. |
| `updates.allow_reboot` | `true` | Let policies reboot the node. With `false` the node reports pending reboots but never restarts on its own. |

## flatpak

| Key | Default | Description |
|-----|---------|-------------|
| `flatpak.binary` | `flatpak` | Flatpak executable, looked up in `PATH` unless absolute. |
| `flatpak.state_dir` | `/etc/bor/flatpak` | Remote definitions rendered by Bor (`.flatpakrepo`, `.filter`, `.gpg`). Tamper-protected. |
| `flatpak.state_file` | `/var/lib/bor/agent/flatpak-state.json` | Record of remotes Bor created or adopted, used to undo changes when a policy is unbound. |
| `flatpak.proxy_url` | `""` | Optional HTTP(S) proxy for Flatpak downloads, for example `http://proxy.example.com:3128`. |

## disk_encryption

| Key | Default | Description |
|-----|---------|-------------|
| `disk_encryption.state_file` | `/var/lib/bor/agent/luks-state.json` | Crash-recovery state for key rotation. Never contains keys. |
| `disk_encryption.bootstrap_dir` | `/etc/bor/luks-bootstrap` | One-time provisioning credentials (`<luks-uuid>.key`, mode `0600`) placed by the installer; consumed and wiped when the volume is adopted. |
| `disk_encryption.run_dir` | `/run/bor/luks` | Root-only work directory on tmpfs. |
| `disk_encryption.cryptsetup` | `cryptsetup` | `cryptsetup` binary. |
| `disk_encryption.cryptenroll` | `systemd-cryptenroll` | `systemd-cryptenroll` binary. |
| `disk_encryption.clevis` | `clevis` | `clevis` binary. |

## Policy types without configuration keys

The remaining policy types write to fixed system locations and have no
settings in `config.yaml`:

| Policy type | Location |
|-------------|----------|
| Dconf | `/etc/dconf/db/*.d` and `/etc/dconf/profile/user` |
| Polkit | `/etc/polkit-1/rules.d` |
| Session access | `/etc/security/time.conf` |
| Packages | Repository and key files named `bor-*` in the package manager's configuration directory |

## Flatpak paths on non-x86 systems

The built-in defaults for the three Flatpak paths use the architecture of
the machine (`x86_64`, `aarch64`, `ppc64le`). The `config.yaml` shipped in
the package, however, spells out the **x86_64** paths for
`firefox.flatpak_policies_path` and `chrome.flatpak_chromium_policies_path`.
On arm64 and ppc64le endpoints, either correct the architecture in those
two lines or delete them so the built-in default applies:

```bash
sudo sed -i 's|/x86_64/|/aarch64/|' /etc/bor/config.yaml   # arm64 example
sudo systemctl restart bor-agent
```

## Complete example

```yaml
server:
  address: "bor.example.com"
  enrollment_port: 443
  policy_port: 8444
  insecure_skip_verify: false

agent:
  client_id: ""

enrollment:
  data_dir: "/var/lib/bor/agent"

firefox:
  policies_path: "/etc/firefox/policies/policies.json"

chrome:
  chrome_policies_path: "/etc/opt/chrome/policies/managed"
  chromium_policies_path: "/etc/chromium/policies/managed"
  chromium_browser_policies_path: "/etc/chromium-browser/policies/managed"

flatpak:
  proxy_url: ""
```

## Verify the configuration

The first lines the agent logs show the addresses and name it uses:

```bash
sudo journalctl -u bor-agent -b --no-pager | head -n 20
```

```text
Bor Agent starting
Server enrollment: bor.example.com:443  policy: bor.example.com:8444
Client ID: ws-042
Agent is enrolled - using mTLS credentials
```
