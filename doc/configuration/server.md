# Server configuration

This page is the complete reference for the Bor server configuration: the
YAML file `/etc/bor/server.yaml`, the environment variables that override
it, and how the two are combined. Every key is listed section by section
with its environment variable, default value and meaning.

Feature-specific pages go deeper on individual sections:

- [TLS and certificates](tls.md) for `tls`, `ca` and `acme`
- [LDAP and Active Directory](ldap.md) for `ldap`
- [Kerberos enrollment](kerberos.md) for `kerberos`
- [Multi-factor authentication](mfa.md) for `webauthn`

## How the configuration is loaded

The server builds its configuration once, at startup, in three layers:

1. **Built-in defaults** compiled into `bor-server`.
2. **The YAML file.** The path is `/etc/bor/server.yaml`, or the value of
   the `BOR_CONFIG` environment variable when it is set. A missing file is
   silently ignored (the server then runs on defaults and environment
   variables only). A file that exists but is not valid YAML stops the
   server with `failed to parse config file`.
3. **Environment variables.** A variable that is set to a non-empty value
   wins over the YAML file. An empty variable is treated as not set.

The precedence is therefore **environment > YAML > built-in defaults**.

!!! note "Restart required"
    The configuration is read only at startup. After changing
    `/etc/bor/server.yaml` or `/etc/bor/server.env`, restart the service:

    ```bash
    sudo systemctl restart bor-server
    ```

    Then check the log for the startup messages and for any configuration
    error:

    ```bash
    sudo journalctl -u bor-server -b --no-pager | tail -n 50
    ```

### Value formats

| Type | YAML | Environment variable |
|------|------|----------------------|
| Boolean | `true` / `false` | `true`, `1`, `yes` or `false`, `0`, `no`. Any other value is ignored and the YAML or default value is kept. |
| Integer | `8444` | `8444`. A non-numeric value stops the server with `invalid <VARIABLE>`. |
| Duration | `"1h"`, `"30m"`, `"24h"` | Same syntax (Go duration: `s`, `m`, `h`). |
| List | YAML sequence | Comma-separated. Whitespace around each item is trimmed. The environment value **replaces** the whole YAML list. |
| Map | YAML mapping | See [`ldap.group_role_map`](#ldap). |

### Files used by the package

| Path | Purpose |
|------|---------|
| `/etc/bor/server.yaml` | Main configuration. Owned `root:bor`, mode `0640`, so the `bor` service user can read it but not change it. Preserved on upgrade. |
| `/etc/bor/server.env` | Optional environment file read by `bor-server.service` (`EnvironmentFile=-/etc/bor/server.env`). Not created by the package. |
| `/var/lib/bor/pki` | Auto-generated internal CA and web UI certificate. |
| `/var/lib/bor/acme` | ACME account key and certificates. |
| `/usr/share/bor/agent-repo` | Agent package repository served at `/agent/`. |

On a fresh install the package's post-install script replaces the
placeholder `jwt_secret` with a random value, writes a random
`security.admin_password` into `server.yaml` (and prints it once on the
console), and points `database.host` at the local PostgreSQL socket.

### Keeping secrets in `/etc/bor/server.env`

Every secret in this reference has an environment variable. Putting
secrets in `/etc/bor/server.env` instead of `server.yaml` keeps them out of
the main file, which you may want to keep in version control or share for
troubleshooting.

1. Create the file, readable only by root (systemd reads it as root before
   starting the service as `bor`):

    ```bash
    sudo install -m 0600 -o root -g root /dev/null /etc/bor/server.env
    ```

2. Add the secrets, one `NAME=value` per line:

    ```bash
    sudo tee /etc/bor/server.env >/dev/null <<'EOF'
    DB_PASSWORD=change-me
    LDAP_BIND_PASSWORD=change-me-too
    BOR_METRICS_TOKEN=a-long-random-string
    EOF
    ```

3. Remove the same keys from `server.yaml` (or leave them empty) and
   restart the service.

!!! warning "Commands run outside systemd do not read server.env"
    `EnvironmentFile` is applied only by systemd. When you run `bor-server`
    by hand, for example `bor-server --reset-mfa`, the variables in
    `/etc/bor/server.env` are not loaded unless you export them yourself.
    See [Reset MFA for a locked-out user](mfa.md#reset-mfa-for-a-locked-out-user).

## server

Network listeners. See [Files and ports](../reference/files-and-ports.md)
for firewall planning.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `server.address` | `BOR_ADDRESS` | `""` (all interfaces) | IP address or hostname to bind the listeners to, without a port. Empty binds both ports on all interfaces. Also used as the bind address of the ACME HTTP-01 listener. |
| `server.enrollment_port` | `BOR_ENROLLMENT_PORT` | `8443` | The web port: web UI, REST API, agent package repository (`/agent/`) and the enrollment gRPC service. TLS 1.2 or newer; a client certificate is optional. Production installations should set `443`; see [Choose the web port](../installation/server.md#step-6-choose-the-web-port). Agents must use the same value in their `server.enrollment_port`. |
| `server.policy_port` | `BOR_POLICY_PORT` | `8444` | Port for agent policy streaming and certificate renewal. TLS 1.3 only, client certificate mandatory. Only enrolled agents connect here. |
| `server.hostnames` | `BOR_HOSTNAMES` | none | Extra DNS names and IP addresses to put in the Subject Alternative Names of the auto-generated server certificate. `localhost`, `127.0.0.1`, `::1` and the system hostname are always included. Add the FQDN that browsers and agents use. See [TLS](tls.md#hostnames-and-subject-alternative-names). |

```yaml
server:
  address: ""
  enrollment_port: 443
  policy_port: 8444
  hostnames:
    - "bor.example.com"
    - "192.0.2.10"
```

!!! tip
    Agents verify the certificate on the policy port against the host name
    in their own `server.address`. If agents connect with
    `bor.example.com`, that name must be in `server.hostnames` (unless it is
    already the system hostname), even when you use ACME for the web UI.

## database

Connection to PostgreSQL. The server runs its schema migrations
automatically at every start.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `database.host` | `DB_HOST` | `localhost` | Host name, IP address, or Unix socket directory (for example `/var/run/postgresql`). The packaged `server.yaml` sets the socket directory. |
| `database.port` | `DB_PORT` | `5432` | TCP port, also used to build the socket file name. |
| `database.user` | `DB_USER` | `bor` | Database role. |
| `database.password` | `DB_PASSWORD` | `bor` | Password of the role. Not needed with peer authentication over the local socket. Prefer `DB_PASSWORD` in `server.env` for TCP connections. |
| `database.name` | `DB_NAME` | `bor` | Database name. |
| `database.sslmode` | `DB_SSLMODE` | `require` | PostgreSQL `sslmode`. The packaged `server.yaml` sets `disable`, which is correct for the local Unix socket. Use `require` (or `verify-full`) for a remote server over TCP. |

```yaml
# Local PostgreSQL over the Unix socket (peer authentication, package default)
database:
  host: "/var/run/postgresql"
  port: 5432
  user: "bor"
  name: "bor"
  sslmode: "disable"
```

```yaml
# Remote PostgreSQL over TCP (put the password in /etc/bor/server.env as DB_PASSWORD)
database:
  host: "db.example.com"
  port: 5432
  user: "bor"
  name: "bor"
  sslmode: "require"
```

!!! note
    With peer authentication, PostgreSQL maps the operating system user to
    the database role of the same name. The service runs as `bor`, so the
    role must be called `bor`. This is also why maintenance commands such
    as `bor-server --reset-mfa` must run as the `bor` user.

## security

Web UI sessions and bootstrap credentials.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `security.jwt_secret` | `JWT_SECRET` | `change-me-in-production` (placeholder) | Secret that signs web UI session tokens, refresh tokens and short-lived login tokens. Must be at least 32 characters. If the placeholder is still in use, the server generates a random secret at each start and logs a warning: all sessions are lost on every restart. A value shorter than 32 characters stops the server. The package generates a random value on install. Changing it signs out every user. |
| `security.jwt_lifetime` | `BOR_JWT_LIFETIME` | `1h` | Lifetime of the access token. |
| `security.refresh_lifetime` | `BOR_REFRESH_LIFETIME` | `24h` | Lifetime of the refresh token, kept in an HTTP-only cookie. A user must sign in again after this time. |
| `security.admin_token` | `BOR_ADMIN_TOKEN` | `""` | Static token that authorizes the `CreateEnrollmentToken` gRPC call (header `x-admin-token`) on the enrollment port, for scripted token creation. Empty disables that call. Enrollment tokens created in the web UI do not need it. |
| `security.admin_password` | `BOR_ADMIN_PASSWORD` | `""` | Password for the initial `admin` account. Used only when the user table is empty, that is on the very first start. The package writes a random value here at install time. Once you have signed in and changed the password, remove the value from the file. |

```yaml
security:
  jwt_secret: "6f0c...64-hex-characters..."
  jwt_lifetime: "1h"
  refresh_lifetime: "24h"
  admin_token: ""
```

To generate a new secret:

```bash
openssl rand -hex 32
```

## tls

Certificate for the web UI and enrollment port. See [TLS and
certificates](tls.md) for the three modes and how to choose between them.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `tls.cert_file` | `BOR_TLS_CERT_FILE` | `""` | PEM certificate (chain) to use instead of the auto-generated one. Must be set together with `key_file`. Cannot be combined with ACME. |
| `tls.key_file` | `BOR_TLS_KEY_FILE` | `""` | PEM private key for `cert_file`. |
| `tls.autogen_dir` | `BOR_TLS_AUTOGEN_DIR` | `/var/lib/bor/pki/ui` | Directory for the auto-generated certificate (`ui.crt`, `ui.key`). |

Setting only one of `cert_file` and `key_file` stops the server with
`both BOR_TLS_CERT_FILE and BOR_TLS_KEY_FILE must be set, or neither`.

## ca

The internal certificate authority that signs agent client certificates
and the auto-generated server certificate.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `ca.cert_file` | `BOR_CA_CERT_FILE` | `""` | PEM CA certificate to use instead of the auto-generated CA. Must be set together with `key_file`. |
| `ca.key_file` | `BOR_CA_KEY_FILE` | `""` | PEM private key of the CA (PKCS#8 or traditional ECDSA/RSA). |
| `ca.autogen_dir` | `BOR_CA_AUTOGEN_DIR` | `/var/lib/bor/pki/ca` | Directory for the auto-generated CA (`ca.crt`, `ca.key`). With PKCS#11, the CA certificate is stored here as `ca.crt`. |
| `ca.pkcs11.lib` | `BOR_CA_PKCS11_LIB` | `""` | Path to the PKCS#11 module (`.so`) of the HSM. |
| `ca.pkcs11.token_label` | `BOR_CA_PKCS11_TOKEN_LABEL` | `""` | Label of the HSM token. |
| `ca.pkcs11.key_label` | `BOR_CA_PKCS11_KEY_LABEL` | `""` | Label of the CA private key on the token. |
| `ca.pkcs11.pin` | `BOR_CA_PKCS11_PIN` | `""` | Token user PIN. Set it in `server.env`, not in YAML. |

PKCS#11 is active when `lib`, `token_label` and `key_label` are all set. It
needs a server binary built with PKCS#11 support, which the distribution
packages are not. See [Store the CA key in an HSM](tls.md#store-the-ca-key-in-an-hsm-pkcs11).

## acme

Automatic, publicly trusted certificate for the web UI port from Let's
Encrypt or any other RFC 8555 certificate authority. See
[TLS](tls.md#mode-3-acme-lets-encrypt).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `acme.enabled` | `BOR_ACME_ENABLED` | `false` | Turn on ACME for the web UI port. Cannot be combined with `tls.cert_file`/`tls.key_file`. |
| `acme.domains` | `BOR_ACME_DOMAINS` | none | Domain names the server may request certificates for. Required when enabled. |
| `acme.email` | `BOR_ACME_EMAIL` | `""` | Contact address registered with the CA. Required when enabled. |
| `acme.directory_url` | `BOR_ACME_DIRECTORY` | Let's Encrypt production | ACME directory URL of the CA. |
| `acme.cache_dir` | `BOR_ACME_CACHE_DIR` | `/var/lib/bor/acme` | Account key and issued certificates. |
| `acme.http_port` | `BOR_ACME_HTTP_PORT` | `80` | Port of the plain-HTTP listener that answers HTTP-01 challenges. |

## ldap

Sign-in to the web UI with directory accounts. See [LDAP and Active
Directory](ldap.md) for complete examples and troubleshooting.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `ldap.enabled` | `LDAP_ENABLED` | `false` | Turn on LDAP sign-in. |
| `ldap.host` | `LDAP_HOST` | `localhost` | Directory server host name. Also used to verify its TLS certificate. |
| `ldap.port` | `LDAP_PORT` | `389` | Port. Use `636` for LDAPS. |
| `ldap.use_tls` | `LDAP_USE_TLS` | `false` | LDAPS: TLS from the first byte. Wins over `start_tls`. |
| `ldap.start_tls` | `LDAP_START_TLS` | `false` | Upgrade a plain connection with StartTLS. |
| `ldap.tls_ca_file` | `LDAP_TLS_CA_FILE` | `""` | PEM CA certificate to verify the directory server. Empty uses the system trust store. |
| `ldap.tls_skip_verify` | `LDAP_TLS_SKIP_VERIFY` | `false` | Disable certificate verification. Testing only: the server refuses to start with it unless `BOR_ALLOW_INSECURE_LDAP=true` is also set in the environment. |
| `ldap.bind_dn` | `LDAP_BIND_DN` | `""` | Service account used to search for users. |
| `ldap.bind_password` | `LDAP_BIND_PASSWORD` | `""` | Service account password. Keep it in `server.env`. |
| `ldap.base_dn` | `LDAP_BASE_DN` | `""` | Subtree searched for users. |
| `ldap.user_filter` | `LDAP_USER_FILTER` | `(uid=%s)` | User search filter; `%s` is the escaped login name. |
| `ldap.upn_suffix` | `LDAP_UPN_SUFFIX` | `""` | Active Directory UPN suffix (for example `@EXAMPLE.COM`) for a direct bind when the search finds nothing. |
| `ldap.attr_username` | `LDAP_ATTR_USERNAME` | `uid` | Attribute holding the login name. |
| `ldap.attr_email` | `LDAP_ATTR_EMAIL` | `mail` | Attribute holding the e-mail address. |
| `ldap.attr_full_name` | `LDAP_ATTR_FULL_NAME` | `cn` | Attribute holding the display name. |
| `ldap.attr_member_of` | `LDAP_ATTR_MEMBER_OF` | `memberOf` | User attribute listing group DNs. When set, it is used instead of a group search. |
| `ldap.group_base_dn` | `LDAP_GROUP_BASE_DN` | `""` (uses `base_dn`) | Subtree for the group search. |
| `ldap.group_filter` | `LDAP_GROUP_FILTER` | `""` | Group search filter; `%s` is the escaped user DN. Used only when `attr_member_of` is empty. |
| `ldap.group_member_attr` | `LDAP_GROUP_MEMBER_ATTR` | `member` | Group attribute that lists members. |
| `ldap.page_size` | `LDAP_PAGE_SIZE` | `500` | Paged search size (RFC 2696). `0` disables paging. |
| `ldap.group_role_map` | `BOR_LDAP_GROUP_ROLE_MAP` | none | Map of LDAP group common names to Bor role names. The environment form is `Group A=Role,Group B=Role` and replaces the YAML map. |

## kerberos

Token-free agent enrollment for domain-joined hosts. See [Kerberos
enrollment](kerberos.md).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `kerberos.enabled` | `BOR_KERBEROS_ENABLED` | `false` | Turn on the `KerberosEnroll` endpoint. |
| `kerberos.realm` | `BOR_KERBEROS_REALM` | `""` | Kerberos realm, for example `EXAMPLE.COM`. Shown in the startup log. |
| `kerberos.keytab_file` | `BOR_KERBEROS_KEYTAB` | `""` | Service keytab. Loaded at startup; the server does not start if it cannot be read. |
| `kerberos.service_principal` | `BOR_KERBEROS_PRINCIPAL` | `""` | Service principal, for example `HTTP/bor.example.com@EXAMPLE.COM`. |
| `kerberos.default_node_group_id` | `BOR_KERBEROS_DEFAULT_NODE_GROUP` | `""` | ID of the node group that Kerberos-enrolled agents join. Enrollment is refused while it is empty. |

## webauthn

Security keys and passkeys (FIDO2) as a second factor for the web UI. See
[Multi-factor authentication](mfa.md#configure-webauthn-on-the-server).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `webauthn.rpid` | `BOR_WEBAUTHN_RPID` | `""` | Relying party ID: the host name users type in the browser, without scheme or port, for example `bor.example.com`. Empty disables security keys. |
| `webauthn.origins` | `BOR_WEBAUTHN_ORIGINS` | `https://<rpid>` | Exact origins the browser reports, including a port other than 443, for example `https://bor.example.com` (web port 443) or `https://bor.example.com:8443` (default port). |
| `webauthn.display_name` | `BOR_WEBAUTHN_DISPLAY_NAME` | `Bor Policy Manager` | Name shown by the browser and authenticator. |

## metrics

Prometheus endpoint on a separate listener. It is always started. See
[Monitoring](../operations/monitoring.md).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `metrics.listen_addr` | `BOR_METRICS_ADDR` | `127.0.0.1:9090` | `host:port` of the metrics listener, path `/metrics`. Use `:9090` for all interfaces. |
| `metrics.bearer_token` | `BOR_METRICS_TOKEN` | `""` | When set, scrapes must send `Authorization: Bearer <token>`. |
| `metrics.tls_cert_file` | `BOR_METRICS_TLS_CERT_FILE` | `""` | PEM certificate. With `tls_key_file` it turns the listener into HTTPS. |
| `metrics.tls_key_file` | `BOR_METRICS_TLS_KEY_FILE` | `""` | PEM private key. Set both or neither. |

!!! warning
    Without `tls_cert_file`/`tls_key_file` the metrics listener speaks plain
    HTTP. Keep it on `127.0.0.1`, or protect it with a firewall and a bearer
    token when a remote Prometheus scrapes it.

## audit

Audit log retention and forwarding to a SIEM. See [Audit
log](../user-guide/audit-log.md).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `audit.retention_days` | `BOR_AUDIT_RETENTION_DAYS` | `365` | Audit records older than this are purged. `0` keeps them forever. |
| (none) | `BOR_AUDIT_ANONYMIZE_IPS` | `false` | Store client IP addresses truncated to /24 (IPv4) or /48 (IPv6). Environment variable only: an `anonymize_ips` key in `server.yaml` is ignored. |
| `audit.syslog.enabled` | `BOR_AUDIT_SYSLOG_ENABLED` | `false` | Also send every audit event to a syslog receiver. |
| `audit.syslog.network` | `BOR_AUDIT_SYSLOG_NETWORK` | `udp` | `udp`, `tcp` or `tcp+tls`. |
| `audit.syslog.addr` | `BOR_AUDIT_SYSLOG_ADDR` | `localhost:514` | `host:port` of the receiver. |
| `audit.syslog.format` | `BOR_AUDIT_SYSLOG_FORMAT` | `cef` | `cef` (ArcSight Common Event Format) or `ocsf` (JSON). |
| `audit.syslog.facility` | `BOR_AUDIT_SYSLOG_FACILITY` | `16` (local0) | Syslog facility code, 0 to 23. |
| `audit.syslog.tls_ca` | `BOR_AUDIT_SYSLOG_TLS_CA` | `""` | PEM CA certificate to verify the receiver with `tcp+tls`. |

```yaml
audit:
  retention_days: 365
  syslog:
    enabled: true
    network: "tcp+tls"
    addr: "siem.example.com:6514"
    format: "ocsf"
    facility: 16
    tls_ca: "/etc/bor/siem-ca.crt"
```

## ui

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `ui.privacy_policy_url` | `BOR_PRIVACY_POLICY_URL` | `""` | When set, a privacy policy link is shown in the sidebar footer of the web UI. |

## agent_repo

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `agent_repo.dir` | `BOR_AGENT_REPO_DIR` | `/usr/share/bor/agent-repo` | Directory with the signed agent package repository that the server publishes on its web port at `https://<server>/agent/` and that the agent deployment dialog uses. Installed by the `bor-server` package. When the directory is missing or has no `manifest.json`, package downloads under `/agent/` return 404. The CA certificate at `/agent/ca.crt` is served either way. |

## flatpak_catalog

The server downloads the AppStream catalog of each Flatpak repository
registered under **Settings -> Flatpak Repositories**, so that the policy
editor can search applications by name.

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `flatpak_catalog.refresh_enabled` | `BOR_FLATPAK_CATALOG_REFRESH` | `true` | Allow outbound catalog downloads. Set `false` on air-gapped servers; catalogs can still be uploaded by hand in the UI. |
| `flatpak_catalog.max_download_mb` | `BOR_FLATPAK_CATALOG_MAX_DOWNLOAD_MB` | `64` | Maximum size of one catalog download, in MB. Must be positive. |
| `flatpak_catalog.allow_private_networks` | `BOR_FLATPAK_CATALOG_ALLOW_PRIVATE_NETWORKS` | `false` | Allow catalog downloads from RFC 1918 and CGNAT addresses (internal mirrors). Loopback, link-local and multicast addresses are always refused. |

## disk_encryption

Escrow of LUKS recovery keys reported by agents. See [Disk
encryption](../policy-types/disk-encryption.md).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `disk_encryption.escrow_kek_file` | `BOR_ESCROW_KEK_FILE` | `""` | Key-encryption key file: exactly 32 random bytes, raw or base64. It must not be group- or world-readable, or the server refuses to start. Without it, escrow is disabled (fails closed) and the policy editor shows a warning. |
| `disk_encryption.escrow_kek_id` | `BOR_ESCROW_KEK_ID` | `file-1` | Label stored with each record wrapped by the current key. Change it when you rotate the key. |
| `disk_encryption.escrow_kek_previous_files` | `BOR_ESCROW_KEK_PREVIOUS_FILES` | none | Old keys kept readable during rotation, as `id=path` entries. A background job re-wraps records under the current key. |
| `disk_encryption.require_mfa_for_reveal` | `BOR_ESCROW_REQUIRE_MFA` | `true` | Revealing a recovery key requires the user's authenticator (TOTP) code in addition to the password. See [Step-up authentication](mfa.md#step-up-authentication). |
| `disk_encryption.retired_retention_days` | `BOR_ESCROW_RETIRED_RETENTION_DAYS` | `30` | Days a replaced recovery key stays revealable before it is destroyed. |
| `disk_encryption.orphan_retention_days` | `BOR_ESCROW_ORPHAN_RETENTION_DAYS` | `90` | Days the keys of a deleted node are kept. |
| `disk_encryption.escrow_pkcs11_key_label` | `BOR_ESCROW_PKCS11_KEY_LABEL` | `""` | Reserved. HSM-held escrow keys are not implemented yet; setting it only logs a warning. |

Create a key file that the `bor` service user can read and nobody else:

```bash
sudo sh -c 'head -c 32 /dev/urandom > /etc/bor/escrow.kek'
sudo chown bor:bor /etc/bor/escrow.kek
sudo chmod 0600 /etc/bor/escrow.kek
```

!!! danger "Back up the key-encryption key"
    Escrowed recovery keys cannot be decrypted without this file. Back it
    up separately from the database backup. See [Backup and
    restore](../operations/backup.md).

## reports

Compliance reports: point-in-time snapshots of the fleet's compliance
results and posture. See [Compliance reports](../user-guide/reports.md).

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `reports.retention_days` | `BOR_REPORT_RETENTION_DAYS` | `1095` | Days a report is kept. An hourly job deletes older unpinned reports and records a `report.purge` audit event. `0` keeps reports forever. |
| `reports.stale_after` | `BOR_REPORT_STALE_AFTER` | `168h` | Age after which a compliance result counts as stale and a node that is not online counts as unreachable in a report. A Go duration such as `72h`. |
| `reports.max_results` | `BOR_REPORT_MAX_RESULTS` | `100000` | A report with more node-policy results than this is refused; narrow its scope. |
| `reports.generate_timeout` | `BOR_REPORT_GENERATE_TIMEOUT` | `120s` | Time budget of one generation. |

Compliance reports are a Bor Enterprise feature: the community server
keeps the page and the API switched off and answers 404 on the report
API. The retention purge runs in every edition. Reports are stored in
PostgreSQL and are part of the normal database backup.

## Systemd options that are not configuration keys

Some behavior is controlled through the service environment rather than
through `server.yaml`. Use a drop-in so package upgrades keep it:

```bash
sudo systemctl edit bor-server
```

```ini
[Service]
Environment=GODEBUG=fips140=on
```

| Variable | Effect |
|----------|--------|
| `GODEBUG=fips140=on` | Enforce FIPS 140-3 approved algorithms at runtime. See [TLS](tls.md#fips-140-3-mode). |
| `GODEBUG=x509negativeserial=1` | Accept directory server certificates with negative serial numbers (old Samba AD certificates). See [LDAP](ldap.md#troubleshooting). |

Combine several `GODEBUG` settings with commas, for example
`Environment=GODEBUG=fips140=on,x509negativeserial=1`.

## Complete example

```yaml
server:
  address: ""
  enrollment_port: 443
  policy_port: 8444
  hostnames:
    - "bor.example.com"

database:
  host: "/var/run/postgresql"
  port: 5432
  user: "bor"
  name: "bor"
  sslmode: "disable"

security:
  jwt_secret: "replace-with-output-of-openssl-rand-hex-32"
  jwt_lifetime: "1h"
  refresh_lifetime: "24h"

tls:
  autogen_dir: "/var/lib/bor/pki/ui"

ca:
  autogen_dir: "/var/lib/bor/pki/ca"

ldap:
  enabled: false

webauthn:
  rpid: "bor.example.com"
  origins:
    - "https://bor.example.com"

metrics:
  listen_addr: "127.0.0.1:9090"

audit:
  retention_days: 365

disk_encryption:
  escrow_kek_file: "/etc/bor/escrow.kek"
reports:
  retention_days: 1095
  stale_after: 168h
```
