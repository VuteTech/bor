# Troubleshooting

This page collects common problems with a Bor server and its agents. Each
section lists symptoms, their causes and fixes, and the commands to diagnose
them. Messages in `code` are quoted from the Bor server, the agent, or the
libraries they use, so you can search for them in the journal.

## First steps

Most problems show up in one of the two journals:

```bash
# on the server
sudo systemctl status bor-server
sudo journalctl -u bor-server -b --no-pager | tail -n 100

# on an endpoint
sudo systemctl status bor-agent
sudo journalctl -u bor-agent -b --no-pager | tail -n 100
```

Check which ports are listening on the server (the web port is 443 when
set as recommended, 8443 by default):

```bash
sudo ss -tlnp | grep -E ':(443|8443|8444|9090)\b'
```

And test connectivity from an endpoint (download the CA first from
`https://bor.example.com/agent/ca.crt`, or use
`/var/lib/bor/agent/ca.crt` on an enrolled endpoint):

```bash
curl -sS --cacert ca.crt https://bor.example.com/api/v1/version
openssl s_client -connect bor.example.com:8444 -CAfile ca.crt </dev/null 2>/dev/null \
  | grep -E 'Verify return code|subject='
```

`/api/v1/version` answers `{"version":"..."}` without authentication. A
`Verify return code: 0 (ok)` on port 8444 means the port is reachable and its
certificate chains to the Bor CA.

## Server does not start

`bor-server` logs a line starting with `Failed to` and exits; systemd restarts
it every 10 seconds.

| Symptom (server log) | Cause | Fix |
|----------------------|-------|-----|
| `Failed to connect to database: failed to ping database: ... Peer authentication failed for user "bor"` | Connecting over the local socket as an OS user that is not `bor`, or the PostgreSQL role is missing | Run the service as `bor` (the unit does), create the role and database (`sudo -u postgres psql -c "CREATE ROLE bor LOGIN"`, `sudo -u postgres createdb -O bor bor`) |
| `... password authentication failed for user "bor"` | Wrong `database.password` / `DB_PASSWORD` for a TCP connection | Fix the password in `server.yaml` or `server.env` |
| `... dial unix /var/run/postgresql/.s.PGSQL.5432: connect: no such file or directory` | PostgreSQL is not running, or its socket is in a different directory | `sudo systemctl enable --now postgresql`; set `database.host` to the real socket directory (for example `/tmp` on some systems) |
| `... connect: connection refused` | PostgreSQL not listening on that host/port | Check `database.host` and `database.port`, and PostgreSQL's `listen_addresses` |
| `... SSL is not enabled on the server` | `sslmode` defaults to `require` for TCP, but PostgreSQL has no TLS | Enable TLS in PostgreSQL (recommended) or set `database.sslmode: "disable"` for a local socket or trusted link |
| `... database "bor" does not exist` | The package could not create it because PostgreSQL was not running at install time | `sudo -u postgres createdb -O bor bor` |
| `Failed to run database migrations: ...` | The `bor` role lacks privileges on the database, or the schema was modified by hand | Make `bor` the database owner; restore from backup if the schema is damaged |
| `Failed to load configuration: failed to parse config file /etc/bor/server.yaml: yaml: line N: ...` | YAML syntax error (often tabs or wrong indentation) | Fix the line; YAML needs spaces |
| `Failed to load configuration: both BOR_TLS_CERT_FILE and BOR_TLS_KEY_FILE must be set, or neither` (similar messages exist for the CA and metrics certificate pairs) | Only one of a certificate/key pair is configured | Set both or neither |
| `Failed to load configuration: BOR_ACME_ENABLED and BOR_TLS_CERT_FILE/BOR_TLS_KEY_FILE are mutually exclusive` | ACME and your own certificate both configured | Choose one |
| `Failed to load configuration: BOR_ACME_ENABLED requires BOR_ACME_DOMAINS to be set ...` or `... requires BOR_ACME_EMAIL to be set` | Incomplete ACME settings | Add `acme.domains` and `acme.email` |
| `JWT_SECRET is too short (N bytes); minimum 32 bytes required` | `jwt_secret` / `JWT_SECRET` shorter than 32 characters | Use `openssl rand -hex 32` |
| `LDAP_TLS_SKIP_VERIFY=true requires BOR_DEV_MODE=true or BOR_ALLOW_INSECURE_LDAP=true` | `ldap.tls_skip_verify` is on | Configure `ldap.tls_ca_file` and turn skip-verify off |
| `Failed to load escrow KEK: escrow: KEK file <path> is group- or world-readable (mode 0640); chmod it to 0600` | KEK file permissions too open | `sudo chown bor /etc/bor/escrow.kek && sudo chmod 0600 /etc/bor/escrow.kek` |
| `Failed to load escrow KEK: escrow: KEK file <path> must hold exactly 32 random bytes, raw or base64 ...` | Wrong KEK content | Recreate it, for example `openssl rand 32` (raw) or `openssl rand -base64 32` |
| `Failed to generate internal CA: failed to create CA autogen dir ...` or `Failed to ensure server certificate: ...` with `permission denied` / `read-only file system` | `ca.autogen_dir` or `tls.autogen_dir` moved outside `/var/lib/bor/pki`; the unit only allows writes to `/var/lib/bor/pki` and `/var/lib/bor/acme` | Keep the defaults, or add the directory with a `ReadWritePaths=` drop-in (see [Security hardening](../security/hardening.md#built-in-service-hardening)) |
| `Failed to load TLS certificate: ...` | Your `tls.cert_file`/`tls.key_file` is unreadable by `bor`, not PEM, or the key does not match | Check ownership (`bor` must read both) and that they belong together |
| `UI server error: listen tcp :443: bind: address already in use` | Another process, often a web server such as Apache httpd or nginx, uses the web port (or 8444 for `Agent server error: ...`) | `sudo ss -tlnp 'sport = :443'` (use the port from the message) to find it; stop it or change `server.enrollment_port`/`server.policy_port` |
| `UI server error: listen tcp ...: bind: permission denied` | Port below 1024 without the capability, for example when started by hand | Start through systemd, which grants `CAP_NET_BIND_SERVICE` |

!!! warning "Unreadable server.yaml looks like a database problem"
    If the `bor` user cannot read `/etc/bor/server.yaml`, the server does not
    complain about the file. It starts with built-in defaults (`localhost`,
    user and password `bor`, `sslmode: require`) and then fails with a
    database error. Check with
    `sudo -u bor cat /etc/bor/server.yaml >/dev/null && echo readable`, and
    restore `root:bor` mode `0640`.

The metrics listener is the exception: if its port is taken, the server logs
`Metrics server error: listen tcp 127.0.0.1:9090: bind: address already in use`
and keeps running without metrics. Prometheus uses port 9090 by default, so
change `metrics.listen_addr` when both run on one host.

To test configuration changes interactively, stop the service and run the
binary in the foreground as the service user:

```bash
sudo systemctl stop bor-server
sudo -u bor bor-server
```

Variables from `/etc/bor/server.env` are not loaded this way; export them
first if you use that file.

## Login problems

Rejected sign-ins are logged by the server with the real reason, while the
browser only sees a generic message:

```bash
sudo journalctl -u bor-server --since "15 min ago" \
  | grep -E 'AuthBegin failed|AuthStep failed|LDAP authentication failed'
```

| Symptom | Cause | Fix |
|---------|-------|-----|
| `AuthStep failed: invalid username or password` for a local user | Wrong password | Reset it as an administrator in **Settings -> Users** |
| `AuthStep failed: user account is disabled` | The account is disabled | Enable it in **Settings -> Users** |
| An LDAP user cannot log in, and there is no `LDAP authentication failed` line | A **local** account with the same username exists; local accounts are checked first and LDAP is not tried | Delete or rename the local account |
| `LDAP authentication failed for user <name>: failed to connect to LDAP: ...` | Wrong `ldap.host`/`ldap.port`, firewall, or TLS failure | Test with `openssl s_client -connect ldap.example.com:636` from the server |
| `... failed to bind with service account: ...` | Wrong `bind_dn` or `LDAP_BIND_PASSWORD` | Test the bind with `ldapwhoami` or `ldapsearch -D` |
| `... user not found in LDAP` | `base_dn` or `user_filter` does not match the user (for example `(uid=%s)` against Active Directory) | Use `(sAMAccountName=%s)` for AD; check `base_dn` |
| `... invalid LDAP credentials` | Wrong user password | User error, or the account is locked in the directory |
| `... certificate chain verification failed: ...` or `... StartTLS failed: ...` | The directory's certificate is not trusted | Set `ldap.tls_ca_file` to the issuing CA |
| `... hostname verification failed: ...` | `ldap.host` does not match a name in the directory's certificate | Use the host name from the certificate |
| HTTP 429, `rate limit exceeded, try again later` | More than 10 authentication requests per minute from one IP address. Behind a reverse proxy, all users share the proxy's address | Wait a minute; avoid proxies in front of the web port |
| Everyone is signed out after every server restart; log shows `WARNING: JWT_SECRET not set` | No `jwt_secret` configured, so a random one is generated per start | Set `jwt_secret` in `server.yaml` or `JWT_SECRET` in `server.env` |
| Sign-in refused with "Two-factor authentication is required, but your account has none. Contact your administrator to set it up." | MFA is required and **Set up MFA while signing in** is set to **Not allowed** | Allow self-enrollment temporarily, or have the user enroll before enforcement |
| No account can log in after the database was recreated; log shows `Warning: failed to ensure default admin: could not persist the generated initial admin password to /var/lib/bor/initial_admin_password ...` | The database is empty, no `admin_password` is configured, and the generated password could not be written | Set `BOR_ADMIN_PASSWORD` in `/etc/bor/server.env` (or `security.admin_password`), restart, log in as `admin`, then remove it again |

See [LDAP](../configuration/ldap.md) for the LDAP settings.

### MFA lockout

If a user (including the last administrator) has lost their authenticator and
backup codes, remove their MFA enrollment on the server host:

```bash
sudo -u bor bor-server --reset-mfa <username>
```

The command reads `/etc/bor/server.yaml`, connects to the database, deletes the
user's MFA record and exits; the running service is not affected. On success
it prints `MFA disabled for user "<username>" (id=...)`; for an unknown user
it fails with `reset-mfa failed: user "<username>" not found`. Run it as `bor`
so local peer authentication to PostgreSQL works, and export any variables
from `server.env` first if your database settings are there.

The user can then sign in with their password. If MFA is required for all
users, they are asked to enroll again (when self-enrollment is allowed). The
reset is not recorded in the Bor audit log; note it in your change log.

If **every** user's TOTP codes stopped working at the same time, the secret
that encrypts TOTP data changed: `BOR_MFA_SECRET`, or `jwt_secret` when
`BOR_MFA_SECRET` is not set. Restore the previous value from your backup
rather than resetting everyone.

## Browser certificate warnings

| Symptom | Cause | Fix |
|---------|-------|-----|
| "Your connection is not private" / `SEC_ERROR_UNKNOWN_ISSUER`; the certificate is issued by `Bor Internal CA` | By default the UI certificate is signed by Bor's internal CA, which browsers do not know | Import the CA (`https://bor.example.com/agent/ca.crt` or `/var/lib/bor/pki/ca/ca.crt`) into the browser or OS trust store; or use your own certificate or ACME |
| Name mismatch (`SSL_ERROR_BAD_CERT_DOMAIN`, `NET::ERR_CERT_COMMON_NAME_INVALID`) | You browse to a name or IP address that is not in the certificate | Add it to `server.hostnames` and restart; the server regenerates its certificate with the new names |
| Certificate expired | The generated certificate is valid for one year and is only renewed when the server starts | Restart `bor-server` |
| Security keys (WebAuthn) cannot be registered or used | The address in the browser does not match `webauthn.rpid` / `webauthn.origins` | Browse to the configured name over HTTPS |

Inspect the certificate the server presents:

```bash
openssl s_client -connect bor.example.com:443 -servername bor.example.com </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -issuer -dates -ext subjectAltName
```

See [TLS and certificates](../configuration/tls.md).

## Agent enrollment fails

Enrollment runs once, in the foreground, and prints the error before exiting:

```bash
sudo bor-agent --token-file /etc/bor/enroll-token
```

| Symptom (agent output) | Cause | Fix |
|------------------------|-------|-----|
| `Agent is not enrolled and no enrollment token was provided.` | The service was started before enrollment, or no token was passed | Enroll first with `--token-file` or `BOR_ENROLLMENT_TOKEN` |
| `Enrollment failed: enrollment RPC failed: rpc error: code = Unauthenticated desc = enrollment failed: enrollment token expired` | The token is older than 5 minutes | Generate a new one (**Node Groups** -> **Generate token**) and use it right away |
| `... desc = enrollment failed: invalid enrollment token` | Token mistyped, already used (tokens are single-use, even by a failed attempt), or from another server | Generate a new token |
| `... code = Internal desc = enrolled but failed to create node record: a node named "<name>" already exists; delete it before re-enrolling` | A node with this name (the host name, or `agent.client_id`) is already registered; the token is now used up | Decommission the old node on **Nodes**, or set a unique `agent.client_id`; then use a new token |
| `... x509: certificate signed by unknown authority` | The server uses the internal CA and the endpoint does not trust it | Install the CA into the system trust store (the **Deploy agent** wizard shows the commands), or set `server.insecure_skip_verify: true` in `/etc/bor/config.yaml` for enrollment only |
| `... x509: certificate is valid for <names>, not <address>` | `server.address` in the agent configuration is not in the server certificate | Use a listed name, or add the address to `server.hostnames` on the server and restart it |
| `... x509: certificate has expired or is not yet valid` | The endpoint's clock is wrong (or the server certificate really expired) | Fix time synchronization (`timedatectl`), then retry |
| `... connection refused` or `i/o timeout` | Wrong address or port, server down, or a firewall blocks the web port. A common case: the server uses 443 but the agent still has the default `enrollment_port: 8443` (or the reverse) | Check `server.address` and `server.enrollment_port` in `/etc/bor/config.yaml` (it must equal the server's `server.enrollment_port`); test with `curl` as shown above |
| A TLS handshake error such as `tls: certificate required` | `enrollment_port` points at the mTLS port 8444 | Enrollment goes to the web port (`enrollment_port`, for example 443), policies to 8444 (`policy_port`) |
| `Failed to load configuration: failed to read config file /etc/bor/config.yaml: ...` | Missing or unreadable configuration | Reinstall the package or restore the file; run as root |

After a successful enrollment the agent prints
`Enrollment successful. Certificates stored in /var/lib/bor/agent` and exits.
Start the service with `sudo systemctl enable --now bor-agent`.

!!! tip "Clock skew"
    Certificates are checked against the local clock on both sides. Make sure
    the server and all endpoints synchronize time (chrony or
    systemd-timesyncd). `timedatectl` shows `System clock synchronized: yes`
    when they do.

## Agent does not receive policies

Work through these checks in order.

1. **Is the agent running and connected?** `sudo journalctl -u bor-agent -f`
   should show `Connecting to policy stream (last_known_revision=...)`
   followed by `Policy update: ...` lines or
   `Received empty snapshot (no policies assigned)`. On the server, a line
   `Client <node> entering watch mode at revision <n>` confirms the stream.
2. **Is port 8444 reachable?** Repeated `Policy stream disconnected:` lines
   with `connection refused` or `i/o timeout` mean the port is blocked or the
   server is down. Open TCP 8444 from the endpoint network. Port 8444 must not
   go through a TLS-terminating proxy.
3. **Is the certificate accepted?** Look for these in the agent log:

    | Message | Cause | Fix |
    |---------|-------|-----|
    | `certificate has been revoked` | Revoked or decommissioned on the server | Decommission (if not done) and re-enroll |
    | `node not found for client_id: <name>` | The node record was deleted | Re-enroll |
    | `x509: certificate signed by unknown authority` | The server CA changed (new CA after `/var/lib/bor/pki` was lost) | Restore the old CA from backup, or re-enroll every agent |
    | `certificate is valid for ..., not ...` | `server.address` is not in the server certificate | Add it to `server.hostnames` on the server |
    | `remote error: tls: ...` handshake errors, and `sudo openssl x509 -in /var/lib/bor/agent/agent.crt -noout -enddate` shows a past date | The agent certificate expired | Re-enroll; see [Monitoring](monitoring.md) for expiry alerts |
    | An `Unimplemented` error such as `unknown service bor.policy.v1.PolicyService` | `policy_port` points at the web port (443 or 8443) | Set `policy_port: 8444` |

4. **Is the policy released?** Only policies in the **released** state are
   sent. A draft is never delivered. Release it on **Policies**.
5. **Is it bound to a group, and is the binding enabled?** On
   **Policy Bindings**, the policy needs a binding to a node group with the
   state **Enabled**.
6. **Is the node in that group?** Open the node on **Nodes** and check its
   groups. A node joins the group its enrollment token was created for; other
   memberships are added by hand.
7. **Does the endpoint support the policy type?** For example, without
   PackageKit or Flatpak the agent logs
   `Warning: flatpak not installed; Flatpak policies will be INAPPLICABLE on this node`
   and reports the policy as **Inapplicable** instead of applying it.

Changes to released policies and bindings are pushed to connected agents
immediately; there is no polling interval to wait for.

## Policy shows non-compliant

After applying a policy the agent checks the result and reports a status per
policy. Open **Compliance** and read the **Message** column for the node; it
contains the agent's explanation.

| Status | Meaning | What to do |
|--------|---------|------------|
| **Unknown** | No report yet | Wait for the agent to connect; check it is online |
| **Non-Compliant** | The agent applied the policy but the system does not match it | Read the message; check the agent log for `Error syncing ...` lines; another tool (configuration management, a package script) may be overwriting the same setting |
| **Error** | Enforcement or the check itself failed | Read the message and the agent log; typical causes are missing tools or invalid values |
| **Inapplicable** | The policy does not apply to this endpoint, for example the software it configures is not installed | Usually fine; bind the policy to a more specific group if it is noise |

To re-run enforcement on an endpoint, restart the agent:

```bash
sudo systemctl restart bor-agent
```

It reconnects, receives a full snapshot, re-applies all policies and reports
compliance again. See [Compliance](../user-guide/compliance.md).

## A managed file keeps being restored

Bor's tamper protection watches every file the agent manages (browser policy
JSON, KConfig files, dconf databases, polkit rules, firewalld zones,
repository definitions, `/etc/security/time.conf` and others). When anything
else changes or deletes one of them, the agent immediately rewrites it from the
policy, reports the event, and logs:

```text
Tamper protection: restoring /etc/firefox/policies/policies.json
Tamper protection: file held by pid=4242 comm=vim user=alice
```

The server records a `tamper_detected` event in **Audit Logs**.

This is intended: local edits to managed files never survive. To change the
setting:

1. Change the policy in Bor and release the new version, or
2. remove the endpoint from the policy's scope (disable the binding or take the
   node out of the group), so the agent stops enforcing it there.

If something on the endpoint keeps rewriting the file (configuration
management, a package post-install script, a second management tool), the two
will fight. Stop the other tool from managing that file. The `file held by`
line helps identify the process.

## "Deploy agent" is missing in the UI

The **Deploy agent** button (masthead and **Nodes** page) only appears when
the server ships an agent package repository and the signed-in user has the
`node:view` permission.

| Cause | Check | Fix |
|-------|-------|-----|
| Missing permission | Your roles on **Settings -> Users** | Grant a role with `node:view` |
| Repository not installed or empty | Server log at startup shows `Agent package repo: disabled (no usable manifest: ...)` | Reinstall `bor-server` from the official packages; they contain `/usr/share/bor/agent-repo` |
| `agent_repo.dir` / `BOR_AGENT_REPO_DIR` points elsewhere | `ls /usr/share/bor/agent-repo/manifest.json` | Remove the override or point it at a directory with `manifest.json` |

When the repository is active, the startup log shows
`Agent package repo: serving <n> files, agent version <v>, signed=true`, and
`https://bor.example.com/agent/manifest.json` is reachable. A warning
`WARNING: agent package repo version ... does not match server version ...`
means the packaged agent is from a different release than the server.

## apt / dnf / zypper errors with the Bor repository

The server publishes its agent packages at `https://bor.example.com/agent/`
(`deb/` for apt, `rpm/` for dnf and zypper).

| Symptom | Cause | Fix |
|---------|-------|-----|
| apt: certificate verification failed / dnf: `Curl error (60)` / SSL certificate problem | The endpoint does not trust the Bor CA used on the web port | Run the CA trust step from the **Deploy agent** wizard: download `/agent/ca.crt` into the system anchors and run `update-ca-certificates` or `update-ca-trust` |
| `404 Not Found` for every file | The server has no agent repository (see the previous section) | Install the official `bor-server` package |
| apt: `NO_PUBKEY` / signature errors; dnf: repomd signature errors | The repository key is missing or the repository was re-signed | Re-download `https://bor.example.com/agent/repo-key.asc` to `/etc/apt/keyrings/bor.asc` (apt) or re-import it (dnf/zypper) |
| The wizard shows "This repository is UNSIGNED (development build)" | The server package was built without a signing key | Use official release packages; unsigned repositories need `trusted=yes` / `gpgcheck=0` |
| Connection errors after the server moved | The repository definition still has the old address | Update `/etc/apt/sources.list.d/bor.sources` (or `bor.list`), `/etc/yum.repos.d/bor.repo`, or `sudo zypper modifyrepo` |
| Updates install an older or newer agent than the server | The server's packaged agent differs from the server version (wizard warns "The packaged agent version ... differs from this server ...") | Upgrade `bor-server`; it carries the matching agent packages |

Diagnose with:

```bash
curl -sS --cacert /etc/ssl/certs/ca-certificates.crt \
  https://bor.example.com/agent/manifest.json | head
sudo apt-get update -o Debug::Acquire::https=true    # Debian / Ubuntu
sudo dnf makecache --repo bor -v                      # RHEL / Fedora
```

## Collecting information for a bug report

```bash
curl -s --cacert ca.crt https://bor.example.com/api/v1/version
sudo journalctl -u bor-server --since "1 hour ago" --no-pager > bor-server.log
sudo journalctl -u bor-agent --since "1 hour ago" --no-pager > bor-agent.log
```

Review the logs and remove host names, user names and IP addresses you do not
want to share before attaching them.
