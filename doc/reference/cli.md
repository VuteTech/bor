# Command line

This page documents the command-line interface of `bor-server` and
`bor-agent`, including the `bor-agent luks` subcommands, the environment
variables that affect them, and the `systemctl` and `journalctl` commands
you use to operate both services.

Both programs accept flags with one or two leading dashes (`-config` and
`--config` are equivalent) and print their usage with `-h` or `--help`.

## bor-server

```text
bor-server [--reset-mfa USERNAME]
```

Without flags, `bor-server` starts the server: it reads the configuration,
prepares the internal CA and TLS certificate, connects to PostgreSQL, runs
pending database migrations and starts listening. Normally systemd starts it
through `bor-server.service`; you do not run it by hand.

| Flag | Description |
|------|-------------|
| `--reset-mfa USERNAME` | Delete the TOTP (authenticator app) enrollment of the given user, including its backup codes, then exit. Use it when a user, typically the last administrator, has lost their authenticator. A running server is not affected; the user can then log in without a TOTP code and enroll again. Security keys (WebAuthn) are not removed. |

### Reset a user's MFA

`--reset-mfa` connects to the database with the same configuration as the
server, so run it as the `bor` user (needed for the default Unix socket peer
authentication):

```bash
sudo -u bor bor-server --reset-mfa alice
```

The command prints `MFA disabled for user "alice" (id=...)` on success.

If you keep settings such as `DB_PASSWORD` in `/etc/bor/server.env`, load
that file first, because only the systemd unit reads it automatically:

```bash
sudo sh -c 'set -a; . /etc/bor/server.env; set +a; exec runuser -u bor -- /usr/bin/bor-server --reset-mfa alice'
```

!!! warning
    `--reset-mfa` works directly on the database and bypasses the web UI,
    so it is not recorded in the audit log. Note the action in your own
    change records. See
    [Multi-factor authentication](../configuration/mfa.md).

### Environment variables

The server reads `/etc/bor/server.yaml`. Environment variables override the
values from the file. The most important ones:

| Variable | Effect |
|----------|--------|
| `BOR_CONFIG` | Path of the YAML configuration file. Default `/etc/bor/server.yaml`. A missing file is ignored and built-in defaults apply. |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE` | Database connection (`database.*`). |
| `BOR_ADDRESS`, `BOR_ENROLLMENT_PORT`, `BOR_POLICY_PORT` | Listen address and ports (`server.*`). |
| `BOR_HOSTNAMES` | Comma-separated extra names for the auto-generated certificate (`server.hostnames`). |
| `JWT_SECRET` | Session signing secret (`security.jwt_secret`), at least 32 characters. |
| `BOR_ADMIN_PASSWORD` | Initial admin password (`security.admin_password`), used only when no users exist. |
| `BOR_METRICS_ADDR` | Metrics listen address (`metrics.listen_addr`). |
| `LDAP_BIND_PASSWORD` | LDAP bind password (`ldap.bind_password`). |
| `BOR_CA_PKCS11_PIN` | PIN for a CA key stored in an HSM. |

The complete list is in [Server configuration](../configuration/server.md).
With systemd, put variables in `/etc/bor/server.env` (one `NAME=value` per
line, mode `0600`).

## bor-agent

```text
bor-agent [--config PATH] [--token-file PATH | --token TOKEN]
bor-agent [--config PATH] luks status
bor-agent [--config PATH] luks adopt [--device DEVICE]
```

The agent must run as root.

| Flag | Default | Description |
|------|---------|-------------|
| `--config PATH` | `/etc/bor/config.yaml` | Agent configuration file. Must come before a subcommand such as `luks`. |
| `--token-file PATH` | - | File containing a one-time enrollment token (one line; surrounding whitespace is ignored). The preferred way to pass a token. |
| `--token TOKEN` | - | Enrollment token on the command line. Deprecated: the token is visible in the process list and the agent logs a warning. |

### Run modes

The agent decides what to do from its enrollment state:

1. **Not enrolled, token given** (or Kerberos configured): enrolls, stores
   the certificate in `enrollment.data_dir`, prints the commands to enable
   the service, and exits with status 0. It does not start streaming
   policies; start the service afterwards.
2. **Already enrolled, token given**: deletes the existing certificate, key
   and CA certificate and enrolls again (re-enrollment). The server rejects
   the request if a node with the same name still exists; delete the old
   node in the web UI first.
3. **Not enrolled, no token, no usable Kerberos**: exits with an error
   (`Agent is not enrolled and no enrollment token was provided.`).
4. **Enrolled, no token**: runs as the daemon. It renews its certificate if
   it expires within 30 days, connects to the policy port and keeps
   running until it receives `SIGTERM` or `SIGINT`.

Example enrollment:

```bash
sudo install -m 600 /dev/null /etc/bor/enroll-token
printf '%s' 'PASTE-THE-TOKEN-HERE' | sudo tee /etc/bor/enroll-token >/dev/null
sudo bor-agent --token-file /etc/bor/enroll-token
sudo rm -f /etc/bor/enroll-token
sudo systemctl enable --now bor-agent
```

See [Installing the agent](../installation/agent.md#enroll-the-agent).

### Environment variables

| Variable | Effect |
|----------|--------|
| `BOR_ENROLLMENT_TOKEN` | Enrollment token. Used when `--token-file` is not given; takes precedence over `--token`. |

The token sources are checked in this order: `--token-file`,
`BOR_ENROLLMENT_TOKEN`, `--token`.

### bor-agent luks status

```bash
sudo bor-agent luks status
```

Prints the disk-encryption view of this machine without changing anything:

- a platform line: TPM2 present or absent, Secure Boot state, initramfs
  generator, and the `cryptsetup` and `clevis` versions;
- the tool that manages TPM enrollment, if it is managed outside Bor;
- for every dm-crypt volume: mapping name, LUKS UUID, device, mount points,
  whether it is a system volume, cipher, volume key size, free space in the
  LUKS2 header, and each keyslot with its kind (for example passphrase,
  TPM2 with its PCRs, Clevis with its Tang URLs, or a Bor recovery key with
  its escrow ID).

Use it to check what the agent sees before you assign a Disk encryption
policy, and to verify the result afterwards.

### bor-agent luks adopt

```bash
sudo bor-agent luks adopt [--device DEVICE]
```

Lets Bor take over recovery-key management for LUKS2 volumes for which it
has no other credential (no bootstrap key, no TPM2 or Tang protector it can
use). The command:

1. Requires an enrolled agent and connects to the server with the agent's
   certificate.
2. Takes the same lock as the running daemon, so two disk-encryption
   operations never run at once.
3. Asks once for the current LUKS passphrase through
   `systemd-ask-password` (120-second timeout). The passphrase never appears
   on a command line.
4. For each selected volume, tests the passphrase, then adds a new recovery
   key, escrows it on the Bor server and reports
   `<name>: recovery key escrowed on the Bor server`. Volumes the passphrase
   does not open are skipped.
5. Leaves the user's passphrase in place.

| Option | Description |
|--------|-------------|
| `--device DEVICE` | Adopt only the volume that matches this device path, LUKS UUID or mapping name. Without it, every LUKS2 volume is tried. |

The daemon reports the new state to the server on its next sync. See
[Disk encryption](../policy-types/disk-encryption.md) for the full workflow.

## Managing the services

### systemctl

| Task | Command |
|------|---------|
| Enable at boot and start now | `sudo systemctl enable --now bor-server` / `bor-agent` |
| Restart (for example after editing the configuration) | `sudo systemctl restart bor-server` / `bor-agent` |
| Stop | `sudo systemctl stop bor-server` / `bor-agent` |
| Show status and the last log lines | `systemctl status bor-server` / `bor-agent` |
| Check whether it is enabled | `systemctl is-enabled bor-server` / `bor-agent` |

Configuration changes in `/etc/bor/server.yaml` or `/etc/bor/config.yaml`
take effect only after a restart. Restarting the agent also makes it
reconnect and receive a full policy snapshot, which picks up node group
membership changes.

### journalctl

Both services log to the journal under their own identifier.

| Task | Command |
|------|---------|
| Follow the log | `sudo journalctl -u bor-server -f` |
| Last 100 lines | `sudo journalctl -u bor-agent -n 100` |
| Since the last boot | `sudo journalctl -u bor-agent -b` |
| A time range | `sudo journalctl -u bor-server --since "2026-10-01 08:00" --until "2026-10-01 12:00"` |
| Search for tamper restores on a desktop | `sudo journalctl -u bor-agent \| grep "Tamper protection"` |

Useful agent log messages:

| Message | Meaning |
|---------|---------|
| `Connecting to policy stream (last_known_revision=N)...` | The agent is (re)connecting to the policy port. |
| `Received empty snapshot (no policies assigned)` | The node has no enabled policies; managed files are restored. |
| `Policy update: type=... id=... name=... version=...` | A policy was received. |
| `Policy stream disconnected: ... reconnecting in ...` | Connection lost; the agent retries with backoff up to 60 seconds. |
| `Tamper protection: restoring <path>` | A managed file was changed locally and is being restored. |
| `Certificate expires within 30 days, renewing...` | Automatic certificate renewal at startup. |
