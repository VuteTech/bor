# Requirements

This page lists what you need before installing Bor: supported platforms and
package formats, the database, network ports and firewall rules, DNS and
certificate naming, and the software each policy type expects on the managed
desktops.

## Supported platforms and packages

Bor is installed only from distribution packages. Both the server
(`bor-server`) and the agent (`bor-agent`) are published for three CPU
architectures in four package formats:

| Package format | Distributions | amd64 | arm64 | ppc64le |
|----------------|---------------|-------|-------|---------|
| `.deb` | Debian, Ubuntu and derivatives | `amd64` | `arm64` | `ppc64el` |
| `.rpm` | RHEL, Fedora, Rocky Linux, AlmaLinux, openSUSE, SLES | `x86_64` | `aarch64` | `ppc64le` |
| `.apk` | Alpine Linux | `x86_64` | `aarch64` | `ppc64le` |
| `.pkg.tar.zst` | Arch Linux | `x86_64` | `aarch64` | `ppc64le` |

The table shows the architecture name each format uses in its file names.

Packages are available from:

- [GitHub releases](https://github.com/VuteTech/Bor/releases)
- [getbor.dev/download](https://getbor.dev/download/)

Agents are normally installed from the package repository that every Bor
server hosts itself at `https://<server>/agent/` on its web port, so managed desktops
need no internet access. See [Installing the agent](agent.md).

!!! note "Service manager"
    Both packages ship systemd units (`bor-server.service`,
    `bor-agent.service`) and no OpenRC scripts. On Alpine Linux you have to
    provide your own service definition; see
    [Installing the server](server.md) for an example.

## Server requirements

### PostgreSQL

The server stores all data in PostgreSQL. The packages declare a dependency
on PostgreSQL (`postgresql` on deb, apk and Arch; `postgresql-server` on
rpm), so a local database server is installed with Bor. You can also use a
remote PostgreSQL server.

The database schema uses the built-in `gen_random_uuid()` function, which
requires PostgreSQL 13 or later. The PostgreSQL versions shipped by currently
supported releases of the distributions above meet this.

The server needs one database and one role, both named `bor` by default. The
package creates them automatically when a local PostgreSQL is running at
install time. Database migrations run automatically every time the server
starts.

### Privileges and service account

The server runs as the unprivileged system user `bor`, created by the
package. The systemd unit grants only `CAP_NET_BIND_SERVICE`, which is needed
if you configure ACME (port 80) or move the web UI to port 443 as
recommended for production.

## Agent requirements

- A systemd-based Linux desktop (the agent uses `systemd-logind` /
  `loginctl` for user sessions and ships a systemd unit).
- Root privileges: the agent runs as root because it writes system-wide
  policy files.
- Network access to the server on its web port (TCP 443 recommended,
  default 8443; enrollment and package repository) and TCP 8444 (policy
  stream).
- `notify-send` (package `libnotify-bin` on Debian/Ubuntu, `libnotify` on
  most other distributions) if you want desktop notifications.

### Software needed per policy type

The agent probes the endpoint at startup and reports a policy as
**Inapplicable** or **Error** when the software it needs is missing. The
agent log explains which tool was not found.

| Policy type | Needs on the endpoint |
|-------------|-----------------------|
| Firefox | Firefox (reads `/etc/firefox/policies/policies.json`). Flatpak Firefox is supported through the Flatpak extension directory. |
| Thunderbird | Thunderbird (reads `/etc/thunderbird/policies/policies.json`), or Flatpak Thunderbird. |
| Chrome | Google Chrome or Chromium (deb, upstream or Flatpak build). |
| Microsoft Edge | Microsoft Edge. |
| KDE Plasma (KConfig) | KDE Plasma. Settings are written under `/etc/bor/xdg` and activated through `/etc/xdg/plasma-workspace/env/zz-bor.sh` and `/etc/profile.d/99-bor.sh`, so users must log in again. |
| Dconf | GNOME / dconf with the `dconf` command (runs `dconf update`). |
| Polkit | polkit (`polkitd`); `pkaction` is used to discover available actions. |
| Firewall | firewalld installed and running (`firewall-cmd`). Without it the policy is Inapplicable. |
| Session Access | `loginctl` (systemd-logind). Login-time enforcement uses `pam_time` through `pam-auth-update` (Debian/Ubuntu) or authselect (Fedora/RHEL). |
| Package | PackageKit on the system D-Bus, and one of `apt`, `dnf`/`yum` or `zypper`. Other package managers make the policy Inapplicable. |
| Flatpak apps | `flatpak` installed. Without it the policy is Inapplicable. |
| Disk encryption | `cryptsetup` (a recommended dependency of `bor-agent`). TPM2 protectors need `systemd-cryptenroll` and `tpm2-tools`; Tang protectors need `clevis-luks` and `clevis-dracut`. `bor-agent luks adopt` uses `systemd-ask-password`. |

See the individual [policy type](../policy-types/index.md) pages for details.

## Network ports

| Port | Protocol | Direction | Required | Purpose |
|------|----------|-----------|----------|---------|
| 8443 (default), 443 recommended | TCP (HTTPS, HTTP/2) | Browsers, agents -> server | Yes | Web port: web UI, REST API, agent enrollment, agent package repository (`/agent/`). Setting: `server.enrollment_port`. Production installations should use 443; see [Choose the web port](server.md#step-6-choose-the-web-port). |
| 8444 | TCP (gRPC over mutual TLS) | Agents -> server | Yes | Policy streaming and certificate renewal. Setting: `server.policy_port`. |
| 5432 | TCP | Server -> PostgreSQL | Only for a remote database | Database connection. A local database is reached over the Unix socket. |
| 9090 | TCP (HTTP) | Monitoring -> server | Optional | Prometheus metrics. Listens on `127.0.0.1:9090` by default, so it is not reachable from other hosts unless you change `metrics.listen_addr`. |
| 80 | TCP (HTTP) | ACME CA -> server | Only with ACME | HTTP-01 challenge listener when `acme.enabled` is true (`acme.http_port`). |

All connections are initiated by the agent; the server never connects to
endpoints. Desktops do not need any inbound port opened.

Depending on the features you enable, the server also makes outbound
connections: to your LDAP server, to a syslog/SIEM receiver for audit
forwarding, to the ACME CA, and to Flatpak repositories to download their
application catalogs (can be turned off with
`flatpak_catalog.refresh_enabled: false`).

### Firewall rules on the server

The examples open the recommended web port 443 (the `https` service in
firewalld). If you keep the default, open 8443 instead.

=== "firewalld (RHEL, Fedora, openSUSE)"

    ```bash
    sudo firewall-cmd --permanent --add-service=https --add-port=8444/tcp
    sudo firewall-cmd --reload
    ```

=== "ufw (Ubuntu, Debian)"

    ```bash
    sudo ufw allow 443/tcp
    sudo ufw allow 8444/tcp
    ```

Restrict the source addresses if you can. For example, allow 8444 only from
your desktop networks:

=== "firewalld"

    ```bash
    sudo firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="192.0.2.0/24" port port="8444" protocol="tcp" accept'
    sudo firewall-cmd --reload
    ```

=== "ufw"

    ```bash
    sudo ufw allow from 192.0.2.0/24 to any port 8444 proto tcp
    ```

## DNS and host names

Agents verify the server's TLS certificate by host name, so plan the name
agents will use before you enroll them:

1. Choose one stable DNS name for the server, for example
   `bor.example.com`, and make sure it resolves from every desktop.
2. Unless you provide your own certificate, the server generates a TLS
   certificate signed by its internal CA. That certificate always contains
   `localhost`, `127.0.0.1`, `::1` and the system host name of the server.
   Add the DNS name (and any aliases or IP addresses agents will use) under
   `server.hostnames` in `/etc/bor/server.yaml`:

    ```yaml
    server:
      hostnames:
        - "bor.example.com"
        - "192.0.2.10"
    ```

3. Restart the server. On startup it checks the existing certificate and
   regenerates it when a listed name is missing.
4. Configure the same name as `server.address` in each agent's
   `/etc/bor/config.yaml`. The deploy wizard does this for you using the
   address you used to open the web UI.

!!! warning
    The policy port (8444) always presents the server certificate, and
    agents verify it against the internal CA they received during
    enrollment. If an agent connects using a name that is not in the
    certificate, the policy stream fails with a TLS verification error.
    Changing the server's DNS name later means adding the new name to
    `server.hostnames` and updating `server.address` on every agent.

The node name of each agent is its host name (or `agent.client_id` if set).
Node names must be unique across your fleet, so make sure desktops do not
share host names.
