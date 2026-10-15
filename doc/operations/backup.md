# Backup and restore

This page lists every piece of state a Bor server keeps, explains what you lose
if each one is gone, and walks through backing up, restoring, and moving the
server to a new host. It also covers the small amount of state on each
endpoint and what re-enrolling an agent does.

## What to back up

| State | Default location | Contains | If it is lost |
|-------|------------------|----------|---------------|
| PostgreSQL database | database `bor` | Policies, node groups, bindings, nodes and their certificate serials, revocations, users, roles, MFA enrollments, settings, compliance results, audit log, Flatpak catalog, LUKS inventory and wrapped recovery keys | Everything you configured. Start over. |
| Server configuration | `/etc/bor/server.yaml` | All settings, including `jwt_secret` and possibly the database and LDAP passwords | Sessions break, and TOTP enrollments may become unusable (see below) |
| Secrets file | `/etc/bor/server.env` (optional) | Environment overrides, typically secrets | Same as above for any secret stored only here |
| Internal CA and server certificate | `/var/lib/bor/pki` (`ca/ca.crt`, `ca/ca.key`, `ui/ui.crt`, `ui/ui.key`) | The CA that signed every agent certificate | **Every agent must be re-enrolled** |
| Recovery-key escrow KEK | the file named by `disk_encryption.escrow_kek_file`, plus any `escrow_kek_previous_files` | The key that encrypts escrowed LUKS recovery keys | **Escrowed recovery keys are permanently unrecoverable** |
| ACME state | `/var/lib/bor/acme` | ACME account key and issued certificates | The server requests new certificates (subject to the CA's rate limits) |
| Other files you configured | for example the Kerberos keytab, `tls.cert_file`/`tls.key_file`, `ldap.tls_ca_file`, metrics certificates | Whatever you pointed the configuration at | Depends on the file |

Not needed in a backup:

- `/usr/bin/bor-server`, the systemd unit, and the agent package repository in
  `/usr/share/bor/agent-repo`: reinstall the `bor-server` package instead.
- `/var/lib/bor/initial_admin_password`: only written when the server had to
  generate a first admin password; delete it after first login.

!!! danger "The escrow KEK lives outside the database"
    If you use the Disk Encryption policy type with recovery-key escrow, the
    recovery keys in the database are encrypted with the key-encryption key
    (KEK) in `escrow_kek_file`. A database backup without that file is useless
    for recovery: nobody, including Bor, can decrypt the escrowed keys. Back
    the KEK up separately from the database, to a protected location (for
    example an offline copy in a safe), and keep every previous KEK listed in
    `escrow_kek_previous_files` for as long as records may still use it. See
    [Disk encryption](../policy-types/disk-encryption.md).

!!! warning "jwt_secret also protects TOTP secrets"
    TOTP secrets are stored encrypted with a key derived from the
    `BOR_MFA_SECRET` environment variable, or from `jwt_secret` when
    `BOR_MFA_SECRET` is not set. If you restore the database with a different
    secret, existing authenticator-app enrollments can no longer be verified
    and affected users need their MFA reset with `bor-server --reset-mfa`.
    Always back up `server.yaml` and `server.env` together with the database.

### Why the internal CA matters

Unless you configured your own CA with `ca.cert_file` and `ca.key_file`, the
server generates an internal CA on first start in
`/var/lib/bor/pki/ca` (ECDSA P-384, valid 10 years) and uses it to:

- sign every agent's client certificate during enrollment, and
- sign the server certificate in `/var/lib/bor/pki/ui` that agents verify on
  port 8444.

Each agent stores a copy of that CA certificate in `/var/lib/bor/agent/ca.crt`.
If the server starts without the CA files it silently creates a **new** CA.
From then on the server rejects the existing agent certificates, and the agents
reject the server certificate. The only fix is to re-enroll every agent (see
[Re-enrolling an agent](#re-enrolling-an-agent)). Treat `ca.key` like any other
private key: back it up encrypted and restrict access to the backup.

If you configured your own CA with `ca.cert_file` and `ca.key_file`, back up
those two files instead; the same rule applies.

## Back up the server

The examples assume the default local PostgreSQL with peer authentication, as
set up by the package. Run them on the Bor server.

1. Create a protected backup directory:

    ```bash
    sudo install -d -m 0700 /var/backups/bor
    ```

2. Dump the database in PostgreSQL's custom format. `pg_dump` takes a
   consistent snapshot, so the server can keep running:

    ```bash
    sudo -u postgres pg_dump --format=custom bor \
      | sudo tee /var/backups/bor/bor-$(date +%F).dump >/dev/null
    ```

    For a remote database, run `pg_dump` with the connection details from
    `server.yaml`:

    ```bash
    pg_dump --host=db.example.com --username=bor --format=custom \
      --file=/var/backups/bor/bor-$(date +%F).dump bor
    ```

3. Archive the configuration and key material:

    ```bash
    sudo tar --create --gzip --preserve-permissions \
      --file=/var/backups/bor/bor-files-$(date +%F).tar.gz \
      /etc/bor /var/lib/bor/pki /var/lib/bor/acme
    ```

    `/etc/bor` includes `server.yaml`, `server.env` if present, and the escrow
    KEK if you placed it there (the example configuration uses
    `/etc/bor/escrow.kek`). Add any other file paths your configuration
    references.

4. Copy the backup off the host. The archive contains private keys and
   secrets, so encrypt it in transit and at rest.

5. Verify that the dump is readable:

    ```bash
    sudo -u postgres pg_restore --list /var/backups/bor/bor-$(date +%F).dump | head
    ```

Schedule steps 2 to 4 with a systemd timer or cron. The files under
`/var/lib/bor/pki` change rarely (the server certificate is regenerated at the
first server start after it expires, or when its host names change), but the database changes constantly.

## Restore the server

Restore to the same or a newer `bor-server` version than the one that made the
backup. On startup the server applies any database migrations the restored
schema is missing.

1. Install the `bor-server` package (see [Server installation](../installation/server.md))
   and PostgreSQL, but do not start `bor-server` yet. If it is running, stop it:

    ```bash
    sudo systemctl stop bor-server
    ```

2. Restore the files, keeping ownership and modes:

    ```bash
    sudo tar --extract --gzip --preserve-permissions --same-owner \
      --file=bor-files-2026-10-15.tar.gz --directory=/
    ```

    Check the result: `/etc/bor/server.yaml` must be `root:bor` mode `0640`,
    `/var/lib/bor/pki` and `/var/lib/bor/acme` owned by `bor`, and the escrow
    KEK owned by `bor` with mode `0600` (the server refuses to load a KEK file
    that is group- or world-readable).

3. Recreate the database and load the dump. On a fresh host the package may
   already have created an empty `bor` database; drop it first:

    ```bash
    sudo -u postgres psql -c "CREATE ROLE bor LOGIN" 2>/dev/null || true
    sudo -u postgres dropdb --if-exists bor
    sudo -u postgres createdb --owner=bor bor
    sudo -u postgres pg_restore --dbname=bor --no-owner --role=bor \
      bor-2026-10-15.dump
    ```

    Give the `bor` role a password again if it connects over TCP.

4. Start the server and watch the log:

    ```bash
    sudo systemctl start bor-server
    sudo journalctl -u bor-server -f
    ```

    Confirm the server loaded the restored CA rather than creating a new one:
    the fingerprint must match the `ca.crt` an existing agent holds.

    ```bash
    sudo openssl x509 -in /var/lib/bor/pki/ca/ca.crt -noout -fingerprint -sha256
    # on an endpoint:
    sudo openssl x509 -in /var/lib/bor/agent/ca.crt -noout -fingerprint -sha256
    ```

5. Verify:

    - Log in to the web UI with an existing account.
    - On **Nodes**, agents change to online as they reconnect. Agents retry with
      a backoff of up to 60 seconds, so give them a minute or two.
    - On an endpoint, `sudo journalctl -u bor-agent` shows
      `Connecting to policy stream` followed by policy updates.

Agents resynchronize by themselves: when an agent reconnects with a policy
revision the restored server does not know, the server sends it a full
snapshot of its policies.

## Move the server to a new host

Moving is a backup on the old host followed by a restore on the new one. The
part that needs planning is the name agents use to reach the server.

Each agent connects to `server.address` from its own `/etc/bor/config.yaml`
and verifies the server certificate on port 8444 against its stored CA
certificate **and** that address. Two things must therefore stay valid:

- **The CA.** Restore `/var/lib/bor/pki` on the new host. With the same CA,
  existing agent certificates keep working.
- **The name.** The agents' `server.address` must resolve to the new host and
  be listed in the server certificate.

The server certificate in `/var/lib/bor/pki/ui` always contains `localhost`,
`127.0.0.1`, `::1`, the machine's host name, and every entry in
`server.hostnames`. On startup the server checks the existing certificate, and
if it was not signed by the current CA, has expired, or lacks any of these
names, it generates a new one signed by the same CA. Agents accept the new
certificate because they trust the CA, not the individual certificate.

Recommended procedure:

1. Use a stable DNS name (for example `bor.example.com`) in all agent
   configurations rather than the machine host name or an IP address.
2. On the new host, add that name, and any other names or IP addresses agents
   use, to `server.hostnames` in `/etc/bor/server.yaml`:

    ```yaml
    server:
      hostnames:
        - "bor.example.com"
        - "192.0.2.20"
    ```

3. Stop `bor-server` on the old host, take the backup, and restore it on the
   new host as described above.
4. Point the DNS name at the new host.
5. Start `bor-server` on the new host and confirm agents reconnect.

If agents use a name or address you cannot move, either add it to
`server.hostnames` and make it resolve to the new host, or change
`server.address` in `/etc/bor/config.yaml` on every agent and restart
`bor-agent`.

Also update anything else that contains the old address:

- Package repository definitions created by the **Deploy agent** wizard
  (`/etc/apt/sources.list.d/bor.sources` or `bor.list`,
  `/etc/yum.repos.d/bor.repo`, the zypper repository `bor`).
- The browser bookmark, `webauthn.rpid`/`webauthn.origins` (security keys are
  bound to the RP ID, so changing it invalidates registered keys), and ACME
  `domains`.
- Firewall rules for the web port (443, or the default 8443) and 8444.

## Agent-side state

Each endpoint keeps a little state in `/var/lib/bor/agent`:

| File | Purpose |
|------|---------|
| `agent.key` (mode `0600`) | The agent's private key, generated on the endpoint |
| `agent.crt` | The client certificate signed by the server CA (valid 90 days) |
| `ca.crt` | The server's CA certificate, received during enrollment |
| `flatpak-state.json` | Which Flatpak remotes Bor created or adopted |
| `luks-state.json` | Crash-recovery state for LUKS key rotation (never holds keys) |

You do not need to back this up. If it is lost, or the endpoint is
reinstalled, re-enroll the agent. Policies are stored on the server and are
re-applied automatically after enrollment.

### Re-enrolling an agent

Re-enrollment gives an endpoint a new key and certificate. You need it after
the endpoint's state was lost, after the server CA was replaced, after a
certificate expired or was revoked, or to move a node into a different group.

1. In the web UI, open **Nodes**, select the node and click **Decommission**
   (type the node name to confirm). This revokes its certificate and deletes
   the node record. This step is required: the server refuses to enroll a
   second node with a name that already exists, and the attempt would use up
   the token.
2. Open **Node Groups**, choose **Generate token** for the target group, and
   copy the token. It is valid for 5 minutes and can be used once.
3. On the endpoint, store the token in a root-only file and enroll:

    ```bash
    sudo install -m 600 /dev/null /etc/bor/enroll-token
    sudoedit /etc/bor/enroll-token        # paste the token
    sudo bor-agent --token-file /etc/bor/enroll-token
    sudo rm -f /etc/bor/enroll-token
    sudo systemctl restart bor-agent
    ```

    When a token is supplied and the agent is already enrolled, the agent first
    deletes `agent.crt`, `agent.key` and `ca.crt`, then enrolls from scratch and
    exits after printing the next steps.

Decommissioning deletes the node's group memberships and compliance results
together with the node record. The re-enrolled node joins the group the token
was generated for; add it to any other groups again on the **Nodes** page.
Compliance results reappear as the agent applies its policies.
See [Agent installation](../installation/agent.md) for the full enrollment
procedure, including trusting the server CA.
