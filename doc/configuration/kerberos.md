# Kerberos enrollment

On hosts joined to FreeIPA or Active Directory (including Samba AD), the
Bor agent can enroll without an enrollment token: it proves its identity
with the machine's Kerberos keytab instead. This page explains how this
works, how to create the service principal and keytab for the Bor server,
how to configure the server and the agents, and how to troubleshoot.

Kerberos is used only for the one-time enrollment. Afterwards the agent
uses its client certificate like any other agent. Web UI sign-in with
directory accounts is a separate feature, see [LDAP](ldap.md).

## How it works

1. The agent reads the machine keytab (`/etc/krb5.keytab`) and gets a
   ticket-granting ticket from the KDC as the host's machine principal.
2. It requests a service ticket for the Bor service principal, for example
   `HTTP/bor.example.com@EXAMPLE.COM`.
3. It sends the ticket (wrapped in SPNEGO) together with a certificate
   signing request to the `KerberosEnroll` call on the server's web port
   (`server.enrollment_port`, 443 recommended, default 8443).
4. The Bor server decrypts and validates the ticket with its own service
   keytab. It does not contact the KDC and does not need to be joined to
   the domain.
5. The server signs the certificate, creates the node in the configured
   default node group and returns the certificate and CA certificate.

Because the server only needs the keytab, the Bor server itself can stay
outside the domain. The agent hosts must reach the KDC on port 88 (TCP and
UDP).

!!! warning "Who can enroll"
    The server accepts any client whose service ticket for the Bor principal
    it can decrypt with its keytab. It does not restrict enrollment to
    particular hosts, groups or realms. Every account that can obtain a
    ticket for the Bor service principal from your KDC can therefore enroll
    a node, and all of them land in the same default node group. Keep the
    default node group free of sensitive policy assignments, or restrict
    ticket issuance for the service principal in the directory. Review new
    nodes in **Nodes** and decommission unexpected ones.

## Prerequisites

Server side:

- A service principal for Bor in the realm, and a keytab with its keys.
- A node group for Kerberos-enrolled agents.

Agent side:

- The host is joined to the domain (`ipa-client-install`, `realm join`,
  `adcli join`), so `/etc/krb5.keytab` exists.
- `/etc/krb5.conf` knows the realm, or you set `kerberos.kdc` in the agent
  configuration.
- Clocks are synchronized with the KDC (Kerberos tolerates 5 minutes of
  skew).
- The agent runs as root, which it does by default, so it can read
  `/etc/krb5.keytab`.

## Step 1: Create the service principal and keytab

Use a principal whose host part is the name agents use to reach Bor, for
example `HTTP/bor.example.com`. The examples assume realm `EXAMPLE.COM`.

=== "FreeIPA"

    On any host enrolled in FreeIPA, as an IPA administrator:

    ```bash
    kinit admin
    # The service needs a host entry for bor.example.com. If the Bor server is
    # not enrolled in IPA, create one first:
    ipa host-add bor.example.com --force
    ipa service-add HTTP/bor.example.com
    ipa-getkeytab -s ipa.example.com \
        -p HTTP/bor.example.com@EXAMPLE.COM \
        -k /tmp/bor.keytab
    ```

    `ipa-getkeytab` creates new keys every time it runs; existing keytabs
    for the principal stop working. Run it once and copy the file.

=== "Active Directory"

    In PowerShell on a domain controller, as a domain administrator, create
    a service account, register the SPN and export a keytab with AES keys:

    ```powershell
    New-ADUser -Name "bor-svc" -SamAccountName "bor-svc" `
        -AccountPassword (Read-Host -AsSecureString "Password") `
        -PasswordNeverExpires $true -CannotChangePassword $true -Enabled $true
    Set-ADUser bor-svc -KerberosEncryptionType AES128,AES256
    setspn -S HTTP/bor.example.com EXAMPLE\bor-svc
    ktpass -princ HTTP/bor.example.com@EXAMPLE.COM -mapuser EXAMPLE\bor-svc `
        -crypto AES256-SHA1 -ptype KRB5_NT_PRINCIPAL -pass * -out C:\bor.keytab
    ```

    `ktpass` sets the account password to the one you enter; use the same
    password you gave `New-ADUser`, or enter a new one there.

=== "Samba AD"

    On a Samba AD domain controller, as root:

    ```bash
    samba-tool user create bor-svc --random-password \
        --description="Bor server service account"
    samba-tool spn add HTTP/bor.example.com bor-svc
    samba-tool user modify bor-svc --option='msDS-SupportedEncryptionTypes=24'   # AES only
    samba-tool domain exportkeytab /tmp/bor.keytab --principal=HTTP/bor.example.com
    ```

Copy the keytab securely to the Bor server, then restrict it to the `bor`
service user:

```bash
sudo install -o root -g bor -m 0640 bor.keytab /etc/bor/krb5.keytab
sudo klist -kte /etc/bor/krb5.keytab
```

`klist` should list `HTTP/bor.example.com@EXAMPLE.COM` with AES encryption
types (`aes256-cts-hmac-sha1-96`, `aes128-cts-hmac-sha1-96`). Delete the
temporary copies.

!!! danger
    The keytab is a secret. Anyone who has it can decrypt service tickets
    for Bor and impersonate the service. Keep it out of backups that are not
    encrypted, and never make it world-readable.

## Step 2: Choose the node group

All Kerberos-enrolled agents join one node group. Create a dedicated one,
for example "Domain desktops", in **Node Groups**, then open it with
**Edit**. The dialog shows the **Group ID** with a copy button; you need
this ID for the server configuration.

Later you can move nodes to other groups in the web UI. See [Node
groups](../user-guide/node-groups.md).

## Step 3: Configure the server

Add to `/etc/bor/server.yaml`:

```yaml
kerberos:
  enabled: true
  realm: "EXAMPLE.COM"
  keytab_file: "/etc/bor/krb5.keytab"
  service_principal: "HTTP/bor.example.com@EXAMPLE.COM"
  default_node_group_id: "3f1c2a9e-0000-4000-8000-000000000000"
```

| YAML key | Environment variable | Description |
|----------|----------------------|-------------|
| `kerberos.enabled` | `BOR_KERBEROS_ENABLED` | Turn on the `KerberosEnroll` call. While it is off, agents get `Kerberos enrollment is not enabled on this server`. |
| `kerberos.realm` | `BOR_KERBEROS_REALM` | Realm, for reference in the startup log. Tickets are validated with the keytab. |
| `kerberos.keytab_file` | `BOR_KERBEROS_KEYTAB` | Service keytab. Read at startup; if it cannot be loaded, the server stops with `Failed to initialize Kerberos service`. |
| `kerberos.service_principal` | `BOR_KERBEROS_PRINCIPAL` | Service principal, shown in the startup log. Agents must use the same value. |
| `kerberos.default_node_group_id` | `BOR_KERBEROS_DEFAULT_NODE_GROUP` | Group ID from step 2. Without it, every Kerberos enrollment is refused. |

Restart and check the log:

```bash
sudo systemctl restart bor-server
sudo journalctl -u bor-server -b | grep -i kerberos
```

```text
Kerberos enrollment enabled (realm=EXAMPLE.COM principal=HTTP/bor.example.com@EXAMPLE.COM default_group=3f1c2a9e-...)
```

The `bor` user must be able to read the keytab. If you changed the unit's
sandboxing, make sure `/etc/bor` is still readable.

## Step 4: Configure the agents

On each domain-joined endpoint, install the `bor-agent` package (see
[Agent installation](../installation/agent.md)) and edit
`/etc/bor/config.yaml`:

```yaml
server:
  address: "bor.example.com"
  enrollment_port: 443     # same as the server's server.enrollment_port
  policy_port: 8444
  insecure_skip_verify: false

kerberos:
  enabled: true
  keytab_file: "/etc/krb5.keytab"
  service_principal: "HTTP/bor.example.com@EXAMPLE.COM"
  # kdc: "dc01.example.com"
  # machine_principal: "WS042$"
```

| Key | Default | Description |
|-----|---------|-------------|
| `kerberos.enabled` | `false` | Try Kerberos enrollment while the agent is not enrolled. |
| `kerberos.keytab_file` | `/etc/krb5.keytab` | Machine keytab. If the file is missing, Kerberos is skipped. |
| `kerberos.service_principal` | `""` | Must equal the server's principal. Its realm is the realm the agent authenticates in. |
| `kerberos.kdc` | `""` | KDC host name or IP. When set, `/etc/krb5.conf` is not read. |
| `kerberos.machine_principal` | `host/<hostname>` | Principal of this machine in the keytab. |

Which machine principal to use:

- **FreeIPA**: the default `host/<hostname>` is correct when `hostname`
  prints the fully qualified name (`ws042.example.com`). If it prints only
  the short name, set `machine_principal: "host/ws042.example.com"`.
- **Active Directory and Samba AD**: the keytab contains `host/...`
  entries, but those are service names that cannot request a
  ticket-granting ticket. Use the computer account, `WS042$` (upper case,
  with the dollar sign). List the entries with `sudo klist -kte
  /etc/krb5.keytab`.

The endpoint must trust the Bor server certificate on the web port when
`insecure_skip_verify` is `false`, exactly as for token enrollment. See
[TLS: Trust the Bor CA on endpoints](tls.md#trust-the-bor-ca-on-endpoints).

## Step 5: Enroll

Run the agent once in the foreground. It enrolls, prints the next steps and
exits:

```bash
sudo bor-agent
```

```text
Kerberos keytab found at /etc/krb5.keytab - attempting Kerberos enrollment
Kerberos enrollment successful: principal=host/ws042.example.com@EXAMPLE.COM node_group=3f1c2a9e-...

Enrollment successful. Certificates stored in /var/lib/bor/agent
```

Then start the service:

```bash
sudo systemctl enable --now bor-agent
```

The node appears in **Nodes**, in the default node group, named after the
agent's `agent.client_id` (by default the host name).

For mass rollout, push the same `config.yaml` with your configuration
management tool and run `bor-agent` once followed by `systemctl enable
--now bor-agent`.

## Order of enrollment methods

When the agent starts and is not enrolled:

1. If `kerberos.enabled` is `true`, `service_principal` is set and the
   keytab file exists, it tries Kerberos enrollment.
2. If that fails (or is skipped), it falls back to token enrollment with
   the token from `--token-file`, `BOR_ENROLLMENT_TOKEN` or `--token`.
3. Without a token, it stops with `Agent is not enrolled and no
   enrollment token was provided`.

Kerberos is attempted first even when you pass a token. To enroll a
Kerberos-enabled host into a different node group with a token, set
`kerberos.enabled: false` before running `bor-agent --token-file ...`, or
simply move the node to the right group in the web UI after Kerberos
enrollment.

## Troubleshooting

Agent errors appear in the output of `sudo bor-agent` as `Kerberos
enrollment failed: <reason> - falling back to token-based enrollment`.
Server errors appear in `journalctl -u bor-server`.

| Message | Cause and fix |
|---------|---------------|
| `Kerberos enrollment configured but keytab not found at ...` | The host is not joined, or `keytab_file` points to the wrong path. |
| `failed to load /etc/krb5.conf` | The file is missing or uses syntax the agent cannot parse. Set `kerberos.kdc` to bypass it. |
| `cannot determine Kerberos realm` | `service_principal` has no `@REALM` and `/etc/krb5.conf` has no `default_realm`. Use the full principal. |
| `kerberos login failed (keytab=..., principal=...)` | Wrong `machine_principal` (on AD use `HOSTNAME$`), stale keytab after a re-join, clock skew, or the KDC is unreachable on port 88. Test with `sudo kinit -k 'WS042$'` or `sudo kinit -k host/ws042.example.com`. |
| `failed to get service ticket for HTTP/...` | The service principal does not exist in the directory, or its name does not match. FreeIPA: `ipa service-show HTTP/bor.example.com`. AD: `setspn -Q HTTP/bor.example.com`. |
| `Kerberos authentication failed: AP_REQ verification failed` (server) | The server keytab does not match the ticket: keys regenerated after the keytab was exported (KVNO mismatch), or the KDC used an encryption type that is not in the keytab (for example RC4). Export a fresh keytab and restrict the service account to AES. |
| `kerberos_default_node_group not configured` | Set `kerberos.default_node_group_id` on the server. |
| `Kerberos enrollment is not enabled on this server` | `kerberos.enabled` is false on the server, or the server was not restarted. |
| `enrolled but failed to create node record` | The default node group ID does not exist. Copy it again from **Node Groups -> Edit**. |
| TLS or certificate errors | The endpoint does not trust the server certificate on the web port. See [TLS](tls.md#trust-the-bor-ca-on-endpoints). |

Useful checks on the endpoint:

```bash
sudo klist -kte /etc/krb5.keytab                       # principals in the machine keytab
sudo kinit -k 'WS042$' && klist && kdestroy            # AD: can the machine get a TGT?
kvno HTTP/bor.example.com@EXAMPLE.COM                  # after kinit: does the service principal exist?
chronyc tracking                                       # clock offset
```
