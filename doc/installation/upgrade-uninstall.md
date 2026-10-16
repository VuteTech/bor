# Upgrading and removing

This page explains how to upgrade the Bor server and the agents, what is
preserved during an upgrade, how server and agent versions relate, and how
to remove Bor completely, including what is left behind on the server and on
managed desktops.

## Before you upgrade

1. Read the release notes of the new version on the
   [GitHub releases](https://github.com/VuteTech/Bor/releases) page.
2. Back up the database and the PKI directory `/var/lib/bor/pki`. Database
   migrations run automatically when the new server starts and are not
   reversed by downgrading the package. See
   [Backup and restore](../operations/backup.md).
3. Upgrade the server first, then the agents.

## Upgrade the server

1. Download the new `bor-server` package and verify its checksum as
   described in [Installing the server](server.md#step-1-download-the-package).
2. Install it over the existing version:

    === "Debian / Ubuntu"

        ```bash
        sudo apt install ./bor-server_${VERSION}_amd64.deb
        ```

    === "RHEL / Fedora / Rocky / Alma"

        ```bash
        sudo dnf install ./bor-server-${VERSION}-1.x86_64.rpm
        ```

    === "openSUSE / SLES"

        ```bash
        sudo zypper install --allow-unsigned-rpm ./bor-server-${VERSION}-1.x86_64.rpm
        ```

    === "Alpine"

        ```bash
        sudo apk add --allow-untrusted ./bor-server_${VERSION}_x86_64.apk
        sudo rc-service bor-server restart   # if you created an OpenRC service
        ```

    === "Arch Linux"

        ```bash
        sudo pacman -U ./bor-server-${VERSION}-1-x86_64.pkg.tar.zst
        ```

3. Make sure the service is enabled and running the new version (see the
   warning below):

    ```bash
    sudo systemctl enable bor-server
    sudo systemctl restart bor-server
    sudo journalctl -u bor-server -n 50
    ```

!!! warning "Debian, Ubuntu and RPM-based systems: the service may be stopped and disabled"
    The package's removal script, which also runs for the old version during
    an upgrade, stops and disables the service. After upgrading on
    Debian/Ubuntu or any RPM-based distribution, check
    `systemctl is-enabled bor-server` and `systemctl is-active bor-server`,
    and run `sudo systemctl enable --now bor-server` if needed. The same
    applies to `bor-agent` (see below).

### What happens during a server upgrade

- **Configuration is preserved.** `/etc/bor/server.yaml` is a configuration
  file that the package manager never overwrites when you have modified it.
  On RPM systems a new default may be saved next to it as
  `server.yaml.rpmnew`; Debian may ask whether to keep your version (keep
  it). Compare new defaults with your file by hand.
- **The installation script** re-applies the ownership and permissions of
  `/etc/bor/server.yaml` and `/var/lib/bor/{pki,acme}`, re-applies the
  `cap_net_bind_service` capability to the new binary, reloads systemd and
  runs `systemctl try-restart bor-server` (restarts the service only if it
  is running). The JWT secret, admin password and database are not touched.
- **RPM upgrades may print a new "Initial admin credentials" banner.**
  Ignore it: the password only replaces the commented placeholder that was
  already replaced on first install, and it is only ever used when the
  database contains no users. Existing accounts are unchanged.
- **Database migrations run automatically** the first time the new version
  starts. Watch the log for `Failed to run database migrations` if the
  server does not come up.
- **The agent repository is updated together with the server.** The new
  package replaces `/usr/share/bor/agent-repo` with the agent packages of
  the same release, and the server serves them immediately. Desktops get
  the new agent on their next package upgrade.
- **Certificates and the CA are kept.** The files in `/var/lib/bor/pki` are
  created by the server, not shipped in the package, so an upgrade leaves
  them alone and enrolled agents keep working.

## Upgrade the agents

Agents installed from the server-hosted repository are upgraded with the
normal package tools of the desktop, from your own server:

=== "Debian / Ubuntu"

    ```bash
    sudo apt-get update
    sudo apt-get install --only-upgrade bor-agent
    ```

=== "RHEL / Fedora"

    ```bash
    sudo dnf upgrade bor-agent
    ```

=== "SUSE"

    ```bash
    sudo zypper refresh
    sudo zypper update bor-agent
    ```

=== "Alpine"

    Download the new package from `https://bor.example.com/agent/apk/`
    (file names in `/agent/manifest.json`) and install it:

    ```bash
    sudo apk add --allow-untrusted ./<file>.apk
    ```

=== "Arch Linux"

    Download the new package from `https://bor.example.com/agent/arch/`
    and install it:

    ```bash
    sudo pacman -U ./<file>.pkg.tar.zst
    ```

Then make sure the agent runs the new binary. The agent's installation
script does not restart the service, and on Debian/Ubuntu and RPM-based
systems the upgrade leaves it stopped and disabled:

```bash
sudo systemctl enable bor-agent
sudo systemctl restart bor-agent
```

Include both commands in whatever tool rolls out the upgrade (configuration
management, unattended-upgrades hooks and similar).

What is preserved on the desktop:

- `/etc/bor/config.yaml` (configuration file, never overwritten when
  modified).
- The enrollment data in `/var/lib/bor/agent`. Upgrading does not require
  re-enrollment.
- All files the agent manages. On restart the agent receives a full
  snapshot and rewrites them as needed.

The agent version of each node is shown in the **Agent** column on the
**Nodes** page.

### Version compatibility

- A server hosts exactly one agent version: the one built with the same
  release. The **Deploy agent** wizard shows a warning if the hosted agent
  version differs from the server version (for example because the
  repository directory was modified).
- Upgrade the server first, then roll the agents forward. Already enrolled
  agents of the previous version keep connecting while you upgrade the fleet
  gradually, but upgrade them promptly so new policy types and settings are
  enforced.
- If `apt update` fails with `NO_PUBKEY` or a signature error after a
  server upgrade, the repository signing key was rotated. Download the key
  again:

    ```bash
    curl -fsS https://bor.example.com/agent/repo-key.asc | sudo tee /etc/apt/keyrings/bor.asc >/dev/null
    ```

## Remove an agent from a desktop

The agent does **not** clean up when its service stops or its package is
removed: the policy files it wrote stay in place and stay in effect (they
are just no longer protected against changes). Let the agent remove its
policies first, then uninstall it.

1. **Withdraw all policies from the node.** On **Fleet -> Nodes**, select
   the node and use **Remove from group** to remove it from every node group.
2. **Let the agent apply the empty policy set.** On the desktop, restart the
   agent so it reconnects with its new (empty) group membership:

    ```bash
    sudo systemctl restart bor-agent
    sudo journalctl -u bor-agent -n 50
    ```

    Wait for `Received empty snapshot (no policies assigned)`. The agent
    then restores the original version of every file it replaced (it keeps
    a `.bor-backup` copy before the first write) or deletes files it
    created, and removes Bor's package repositories, polkit rules, Flatpak
    remotes and similar. What happens to installed packages, Flatpak
    applications, firewall zones and LUKS keyslots is described on the
    individual [policy type](../policy-types/index.md) pages.

3. **Remove the package.** The removal script stops and disables the
   service:

    === "Debian / Ubuntu"

        ```bash
        sudo apt purge bor-agent
        ```

    === "RHEL / Fedora"

        ```bash
        sudo dnf remove bor-agent
        ```

    === "SUSE"

        ```bash
        sudo zypper remove bor-agent
        ```

    === "Alpine"

        ```bash
        sudo apk del bor-agent
        ```

    === "Arch Linux"

        ```bash
        sudo pacman -R bor-agent
        ```

4. **Delete the remaining files**, including the agent's private key and the
   repository configuration added during installation:

    ```bash
    sudo rm -rf /var/lib/bor/agent /etc/bor/config.yaml /etc/bor/flatpak /etc/bor/xdg /etc/bor/share
    sudo rm -f /etc/profile.d/99-bor.sh /etc/xdg/plasma-workspace/env/00-bor.sh
    # repository definition, depending on the distribution:
    sudo rm -f /etc/apt/sources.list.d/bor.sources /etc/apt/keyrings/bor.asc   # Debian/Ubuntu
    sudo rm -f /etc/yum.repos.d/bor.repo                                      # RHEL/Fedora
    sudo zypper removerepo bor                                                # SUSE
    ```

    Remove `/etc/bor` if it is now empty. The CA certificate added to the
    system trust store (`bor-ca.crt`) can be removed as well; run
    `update-ca-certificates` or `update-ca-trust` afterwards.

5. **Delete the node** on **Fleet -> Nodes**. This revokes its certificate.

!!! note "If the desktop is gone already"
    If the machine is decommissioned or unreachable, simply delete the node
    in the web UI. Deleting a node revokes its certificate, so a copy of the
    machine can no longer connect.

If you skipped step 2, managed files remain on the desktop. You can remove
them by hand using the list in
[Files and ports](../reference/files-and-ports.md#managed-files-on-endpoints):
for files the agent replaced, a `.bor-backup` file next to it holds the
original content (an empty backup means no original existed).

## Remove the server

1. **Stop and remove the package.** The removal script stops and disables
   `bor-server`.

    === "Debian / Ubuntu"

        ```bash
        sudo apt remove bor-server    # keeps /etc/bor/server.yaml
        sudo apt purge bor-server     # also removes /etc/bor/server.yaml
        ```

    === "RHEL / Fedora / Rocky / Alma"

        ```bash
        sudo dnf remove bor-server
        ```

        A modified `/etc/bor/server.yaml` is saved as
        `/etc/bor/server.yaml.rpmsave`.

    === "openSUSE / SLES"

        ```bash
        sudo zypper remove bor-server
        ```

    === "Alpine"

        ```bash
        sudo rc-service bor-server stop   # if you created an OpenRC service
        sudo apk del bor-server
        ```

    === "Arch Linux"

        ```bash
        sudo pacman -R bor-server
        ```

2. **Decide what to keep.** Package removal leaves your data in place:

    | What | Where | Notes |
    |------|-------|-------|
    | Database | PostgreSQL database `bor`, role `bor` | All policies, nodes, users, audit logs, escrowed recovery keys. |
    | Internal CA and TLS certificate | `/var/lib/bor/pki` | Contains the CA **private key**. Without it, no enrolled agent can connect to a reinstalled server. |
    | ACME cache | `/var/lib/bor/acme` | Only used with ACME. |
    | Configuration | `/etc/bor/server.yaml` (unless purged), `/etc/bor/server.env` | Contains the JWT secret and possibly the database password. |
    | Service account | user and group `bor` | Created by the package. |
    | Disk-encryption key file | the path set in `disk_encryption.escrow_kek_file` | Only if you configured recovery-key escrow. |

    To reinstall later with the same data, keep the database and
    `/var/lib/bor/pki`.

3. **Purge everything** (irreversible):

    ```bash
    sudo -u postgres psql -c 'DROP DATABASE bor;'
    sudo -u postgres psql -c 'DROP ROLE bor;'
    sudo rm -rf /var/lib/bor/pki /var/lib/bor/acme
    sudo rm -f /etc/bor/server.yaml /etc/bor/server.yaml.rpmsave /etc/bor/server.env
    sudo userdel bor
    sudo groupdel bor 2>/dev/null || true
    ```

    Remove `/var/lib/bor` and `/etc/bor` if they are empty. Remove
    PostgreSQL itself only if nothing else uses it.

!!! danger
    Deleting `/var/lib/bor/pki` destroys the internal CA. Every enrolled
    agent then has to be re-enrolled against a new server. Deleting the
    database also deletes escrowed disk-encryption recovery keys. Export or
    back up anything you still need first.
