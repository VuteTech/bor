# TLS and certificates

Bor uses TLS on both server ports and mutual TLS (client certificates) for
every agent. This page explains the certificate that browsers see on the
web UI port, the internal certificate authority (CA) that issues agent
certificates, how agent certificates are renewed and revoked, how to trust
the Bor CA on browsers and endpoints, how to keep the CA key in a hardware
security module, and how to run in FIPS 140-3 mode.

## Overview

| Port | Used by | Certificate presented by the server | Client certificate |
|------|---------|-------------------------------------|--------------------|
| Web port (`server.enrollment_port`, default 8443, 443 recommended) | Browsers, REST API, agent package downloads, agent enrollment | Depends on the [mode](#choose-a-certificate-mode-for-the-web-ui) | Optional |
| `8444` (`server.policy_port`) | Enrolled agents only | The internal server certificate (auto-generated or `tls.cert_file`), never the ACME certificate | Required, issued by the Bor CA |

TLS settings that are fixed in the server and cannot be changed:

- Web port: TLS 1.2 or 1.3. For TLS 1.2 only ECDHE key exchange with
  AES-GCM ciphers is offered (`ECDHE-ECDSA/RSA-AES256-GCM-SHA384`,
  `ECDHE-ECDSA/RSA-AES128-GCM-SHA256`). Curves: X25519, P-256, P-384.
- Port 8444: TLS 1.3 only, HTTP/2 only, client certificate required and
  verified against the Bor CA. In addition, every call checks the
  certificate's serial number against the revocation list in the database.

## The internal CA

On first start, if no CA is configured, the server creates one in
`/var/lib/bor/pki/ca` (`ca.autogen_dir`):

| Property | Value |
|----------|-------|
| Files | `ca.crt` and `ca.key` (PKCS#8), both mode `0600` |
| Key | ECDSA P-384 |
| Subject | `CN=Bor Internal CA, O=Bor` |
| Validity | 10 years from creation by default; set `ca.lifetime` (`BOR_CA_LIFETIME`) before the first start to change it |
| Constraints | CA, path length 0 (can sign only end-entity certificates) |

The CA signs two kinds of certificates:

- The auto-generated **server certificate** for both ports.
- **Agent client certificates** at enrollment and renewal.

The CA certificate is public and is published at
`https://bor.example.com/agent/ca.crt`.

!!! danger "Back up the CA"
    Every enrolled agent trusts this exact CA. If `ca.crt` and `ca.key` are
    lost or replaced, all agents fail to connect to port 8444 and must be
    re-enrolled. Include `/var/lib/bor/pki` in your backups. See [Backup and
    restore](../operations/backup.md).

### Use your own CA

To have Bor issue agent certificates from a CA you manage (for example an
intermediate CA of your organization), set both files:

```yaml
ca:
  cert_file: "/etc/bor/pki/bor-ca.crt"
  key_file: "/etc/bor/pki/bor-ca.key"
```

The key may be ECDSA or RSA, in PKCS#8 or traditional PEM form. Both files
must be readable by the `bor` user. Setting only one of the two stops the
server with `both BOR_CA_CERT_FILE and BOR_CA_KEY_FILE must be set, or
neither`. Switching CA on a running deployment has the same effect as
losing the CA: agents enrolled under the old CA must re-enroll.

At startup the server checks the CA and stops with a message that names the
cause when:

- the private key does not match the certificate,
- the certificate is not a CA certificate or lacks the certificate signing
  key usage,
- the key is below the baseline (RSA 3072 bits, ECDSA P-256),
- the certificate, or one of its issuers in the file, restricts extended
  key usage and does not allow both server and client authentication,
- a path length constraint of an issuer forbids a subordinate CA.

### Run the Bor CA under an external authority

The Bor CA can be a subordinate CA of your organization's PKI. The server
creates the CA key and a certificate signing request, your authority issues
a **CA** certificate for it, and the server installs the result. The private
key never leaves the server.

Run the commands on the server host as the `bor` user. They read the same
configuration as the server (`/etc/bor/server.yaml` and the `BOR_CA_*`
variables), so the key and certificate end up where the server looks for
them: in `ca.autogen_dir`, or at `ca.cert_file` / `ca.key_file` when you set
those.

1. Create the signing request. On a new installation, do this **before the
   first start** of the server:

    ```bash
    sudo -u bor bor-server ca csr -cn "Example Bor CA" -out /tmp/bor-ca.csr
    ```

    If the CA has no key yet, an ECDSA P-384 key is generated. The request
    asks for a CA certificate with path length 0 and the certificate signing
    key usage. Without `-cn` the request repeats the subject of the current
    CA certificate (or `CN=Bor CA, O=Bor` when there is none).

2. Have your certificate authority issue the certificate. It must meet these
   requirements:

    | Property | Requirement |
    |----------|-------------|
    | Basic constraints | `CA:TRUE`. A path length of 0 is enough: Bor issues only end-entity certificates. |
    | Key usage | Certificate signing |
    | Extended key usage | Absent, or including both server and client authentication |
    | Name constraints | Absent, or permitting every name of the server certificate (see below) |
    | Issuers | Each authority above must permit a subordinate CA (path length) |

3. Put the issued certificate and its issuers into one PEM file: the Bor CA
   certificate first, then each issuer in order, ending with the self-signed
   root. Install it and restart the server:

    ```bash
    cat bor-ca.crt issuing-ca.crt root-ca.crt > /tmp/bor-ca-chain.pem
    sudo -u bor bor-server ca import -in /tmp/bor-ca-chain.pem
    sudo systemctl restart bor-server
    ```

    The import refuses a certificate that does not belong to the CA key, a
    file that is out of order or does not end with the root, and a
    certificate that fails the checks listed under
    [Use your own CA](#use-your-own-ca).

`bor-server ca status` shows the certificate on disk, its issuers and
whether a signing request is waiting for its certificate.

While a signing request is waiting and no CA certificate is installed, the
server refuses to start. This prevents it from generating a self-signed CA
in place of the one you requested. Remove the request file (`ca.csr` next
to the CA certificate) to abandon the request.

With the CA key in an HSM, the same commands create the request for the key
on the token and need the token PIN, for example
`sudo -u bor env BOR_CA_PKCS11_PIN=... bor-server ca csr`.

You can also create the key and request with your own tools and configure
the result with `ca.cert_file` and `ca.key_file`; the same rules for the
file contents apply. A file with the signing certificate alone still works,
but the server then cannot present the chain.

How the chain is used:

- **Agent certificates** are verified against the Bor signing certificate
  only. A certificate issued by another CA under the same root is rejected,
  whether or not the file contains the root.
- **Agents** receive the Bor signing certificate at enrollment and accept
  only server certificates issued by it. See
  [What agents trust](#what-agents-trust) for the alternative.
- **The auto-generated server certificate** is served together with the
  signing certificate and the intermediates (without the root), so a
  browser that trusts your root validates the web UI.

With name constraints on the CA, the auto-generated server certificate must
fit them. It always contains `localhost`, the loopback addresses and the
system hostname besides `server.hostnames`, so a CA constrained to your
domain usually cannot issue it. The server then stops at startup; supply
the server certificate yourself with `tls.cert_file` and `tls.key_file`.

`ca.lifetime` does not apply to a CA certified by an external authority:
its validity is set by that authority.

#### Renew or replace the CA certificate

Repeat the three steps before the CA certificate expires. A certificate
that keeps the **same key and the same subject** replaces the old one
without touching the agents: certificates issued before and after verify
against either CA certificate. `bor-server ca csr` without `-cn` requests
exactly that, and `bor-server ca import` reports whether the installed
certificate qualifies. The replaced file is kept next to the new one with
the suffix `.previous`.

This also applies to a deployment that started with the self-generated CA:
requesting a certificate for its key and subject turns it into a
subordinate CA in place.

!!! warning "Limits of an in-place replacement"
    - A certificate with a different key or subject is a CA switch. Without
      a [CA rotation](#replace-the-ca-with-a-new-key), agents enrolled
      under the previous certificate must re-enroll.
    - Some authorities re-encode the subject they are given. The import
      then reports a different subject; treat it as a CA switch.
    - Agents older than the version that
      [keeps CA certificates current](#how-agents-keep-their-ca-certificates-current)
      hold the certificate they received at enrollment and stop trusting
      the server when **that** certificate expires. Upgrade them, or
      re-enroll them before it runs out.

### What agents trust

`ca.agent_trust` (`BOR_CA_AGENT_TRUST`) selects the CA certificates an agent
receives at enrollment and verifies the server against. It only matters
when the CA certificate file lists the authorities above the Bor CA.

| Value | Agents receive | Agents accept a server certificate issued by |
|-------|----------------|-----------------------------------------------|
| `signing` (default) | The Bor signing certificate | The Bor CA only |
| `chain` | The Bor signing certificate, the intermediates and the root | Any CA under that root |

With `chain`, agents keep trusting the server when the Bor signing
certificate is renewed or replaced under the same root, including after the
certificate they received at enrollment has expired.

!!! danger "`chain` widens who can impersonate the server"
    A server that agents trust delivers policies that are applied as
    `root`. With `ca.agent_trust: chain`, every certificate authority under
    your root can issue a certificate that agents accept as the Bor server:
    a certificate mis-issued anywhere in that PKI is enough to take over
    every managed machine.

    Use `chain` only when the root is dedicated to Bor, or when it is
    constrained so that no other CA under it can issue a certificate for
    the Bor server names. The server logs a warning at every start while
    `chain` is active.

The setting never changes which **agent** certificates the server accepts:
those are always verified against the Bor signing certificate alone. An
agent whose certificate was issued by a replaced signing key must
re-enroll, whatever it trusts.

Agents receive the certificates at enrollment, and enrolled agents pick up
a change of the setting with their next heartbeat (see below). The same
certificates are served at `/agent/ca.crt` for the agent installer.

### How agents keep their CA certificates current

An agent stores the CA certificates it verifies the server against in
`ca.crt` in its data directory. With every heartbeat (on connect, and
whenever the server asks for one) it reports a fingerprint of that file.
When the server hands out different certificates, it answers with them and
the agent replaces the file. The exchange runs over the mutually
authenticated policy channel, so only a server the agent already trusts can
change what it trusts.

This is how these changes reach enrolled agents without re-enrollment:

- a CA certificate renewed for the same key and subject,
- a switch of `ca.agent_trust`,
- a new CA during a [CA rotation](#replace-the-ca-with-a-new-key).

The agent uses the new file for its next connection; it also reads its own
certificate again for every connection, so a renewed certificate needs no
restart. Agents that predate this exchange report no fingerprint and keep
what they received at enrollment.

### Replace the CA with a new key

Renewing the CA certificate keeps its key. Replacing the key (after a
suspected compromise, to move to another authority, or because policy
demands a fresh key) is a CA rotation. It runs in three steps and does not
re-enroll agents:

1. **Stage.** A new CA is created next to the one in use, either
   self-signed or with a signing request for your authority. The server
   starts handing it to agents in addition to the current one. Nothing is
   signed by it yet.
2. **Activate.** The new CA becomes the server's CA and the server is
   restarted. It then issues from the new CA and presents a server
   certificate from it. Certificates issued by the replaced CA stay
   accepted, and the server asks each agent to renew its certificate, which
   the agent does at once.
3. **Retire.** When no agent holds a certificate of the replaced CA any
   more, it is removed: its certificates are no longer accepted, agents
   stop trusting it, and its key is deleted.

The server keeps the state in the directory `rotation` next to the CA
certificate (`/var/lib/bor/pki/ca/rotation` by default); it is part of the
PKI directory you back up.

An agent that was offline between step 1 and step 2 never learned the new
CA: it can no longer verify the server and must re-enroll. An agent that
was offline between step 2 and step 3 still holds a certificate of the
retired CA and must re-enroll as well. Wait for the agents before each of
these steps.

Guided CA rotation, with the count of agents that are ready for each step
and a safeguard against cutting agents off, is part of **Bor Enterprise**
(**Settings** -> **Certificate authority**). Rotation needs the CA key in a
file; a key in a PKCS#11 token is not supported.

## Choose a certificate mode for the web UI

The web UI port can use one of three certificate sources. Exactly one is
active:

| Mode | When it is active | Browsers | Agents at enrollment |
|------|-------------------|----------|----------------------|
| 1. Auto-generated | `tls.cert_file`, `tls.key_file` empty and `acme.enabled: false` (default) | Warning until the Bor CA is trusted | Need the Bor CA in their trust store, or `insecure_skip_verify: true` |
| 2. Your own certificate | `tls.cert_file` and `tls.key_file` set | Trusted if your CA is trusted | Need your CA in their trust store |
| 3. ACME | `acme.enabled: true` | Trusted (public CA) | Trusted, no extra step |

### Mode 1: auto-generated certificate (default)

The server creates `ui.crt` and `ui.key` in `/var/lib/bor/pki/ui`
(`tls.autogen_dir`), signed by the internal CA:

| Property | Value |
|----------|-------|
| Key | ECDSA P-256 |
| Subject | `CN=Bor UI, O=Bor` |
| Validity | 365 days |
| Usage | TLS server authentication |

At every start the server checks the existing certificate and replaces it
when it is not signed by the current CA, has expired, or is missing a
required name. Therefore:

- Adding a host name to `server.hostnames` and restarting is enough to get
  a certificate with the new name.
- An expired auto-generated certificate is renewed at the next restart.
  Because it is valid for a year, restart the service at least once a year
  (package upgrades do this).

This certificate is also the one served on the policy port, so it must
contain the name agents use in `server.address`.

### Mode 2: your own certificate

Use a certificate from your organization's PKI:

```yaml
tls:
  cert_file: "/etc/bor/tls/bor.example.com.crt"   # leaf first, then intermediates
  key_file: "/etc/bor/tls/bor.example.com.key"
```

```bash
sudo chown root:bor /etc/bor/tls/bor.example.com.key
sudo chmod 0640 /etc/bor/tls/bor.example.com.key
sudo systemctl restart bor-server
```

!!! warning "The policy port serves the same certificate"
    With `tls.cert_file` set, the server presents this certificate on
    **both** ports. Agents verify the policy port only against the Bor CA
    certificate they received at enrollment. A certificate issued by
    another CA therefore makes every agent fail on port 8444.

    Use this mode only together with [your own CA](#use-your-own-ca), so
    that `tls.cert_file` is issued by the same CA that is configured in
    `ca.cert_file`. If you only want a browser-trusted certificate on the
    web UI, use ACME (mode 3) instead: ACME changes only the web port.

Bor does not reload the files while running. After replacing the
certificate, restart the service.

### Mode 3: ACME (Let's Encrypt)

The server obtains and renews a publicly trusted certificate for the web
UI port by itself, from Let's Encrypt or any other RFC 8555 CA.

Requirements:

- A DNS name (for example `bor.example.com`) that resolves to the server.
- The CA must reach the server on TCP port 80 (HTTP-01 challenge), or on
  port 443 if the web port is 443 or forwarded from 443 (TLS-ALPN-01).
  With the recommended web port 443, the server offers TLS-ALPN-01 on the
  web port automatically; the HTTP-01 listener on port 80 is started as
  well.
- `tls.cert_file` and `tls.key_file` must be empty.

1. Configure ACME in `/etc/bor/server.yaml`:

    ```yaml
    acme:
      enabled: true
      domains:
        - "bor.example.com"
      email: "admin@example.com"
      # directory_url: "https://acme-staging-v02.api.letsencrypt.org/directory"
    ```

2. Open port 80 in the firewall:

    ```bash
    sudo firewall-cmd --permanent --add-service=http && sudo firewall-cmd --reload   # firewalld
    sudo ufw allow 80/tcp                                                          # ufw
    ```

3. Restart and watch the log:

    ```bash
    sudo systemctl restart bor-server
    sudo journalctl -u bor-server -f
    ```

    You should see `ACME certificate provisioning enabled` and
    `ACME HTTP-01 challenge listener on :80`.

4. Open `https://bor.example.com` in a browser. The first connection
    triggers the certificate order; it can take a few seconds.

How it works:

- The challenge listener is plain HTTP on `acme.http_port` (default 80).
  The service unit grants `CAP_NET_BIND_SERVICE`, so the `bor` user can
  bind to port 80.
- The account key and certificates are stored in `/var/lib/bor/acme`.
  Renewal is automatic, about 30 days before expiry, without a restart.
- Clients that connect without a server name (SNI), for example by IP
  address, receive the internal auto-generated certificate.
- The policy port keeps using the internal certificate, so
  `server.hostnames` still matters for agents.

ACME settings:

| YAML key | Environment variable | Default |
|----------|----------------------|---------|
| `acme.enabled` | `BOR_ACME_ENABLED` | `false` |
| `acme.domains` | `BOR_ACME_DOMAINS` | none (required) |
| `acme.email` | `BOR_ACME_EMAIL` | none (required) |
| `acme.directory_url` | `BOR_ACME_DIRECTORY` | Let's Encrypt production |
| `acme.cache_dir` | `BOR_ACME_CACHE_DIR` | `/var/lib/bor/acme` |
| `acme.http_port` | `BOR_ACME_HTTP_PORT` | `80` |

Other ACME CAs are used by setting `directory_url`, for example
`https://acme.zerossl.com/v2/DV90` (ZeroSSL),
`https://api.buypass.com/acme/directory` (Buypass) or the ACME directory of
an internal CA such as step-ca.

!!! tip "Test against the staging CA first"
    Let's Encrypt limits how many certificates you can request. Use
    `https://acme-staging-v02.api.letsencrypt.org/directory` until the
    setup works, then remove `directory_url` and delete the staging data:
    `sudo rm -rf /var/lib/bor/acme/*` before restarting.

If port 80 is served by a reverse proxy, set `acme.http_port` to an
unprivileged port such as `8080` and forward
`/.well-known/acme-challenge/` from the proxy to `127.0.0.1:8080`.

Startup errors you may see:

| Message | Fix |
|---------|-----|
| `BOR_ACME_ENABLED and BOR_TLS_CERT_FILE/BOR_TLS_KEY_FILE are mutually exclusive` | Remove `tls.cert_file`/`tls.key_file` (and the matching environment variables). |
| `BOR_ACME_ENABLED requires BOR_ACME_DOMAINS to be set` | Add `acme.domains`. |
| `BOR_ACME_ENABLED requires BOR_ACME_EMAIL to be set` | Add `acme.email`. |

## Hostnames and Subject Alternative Names

The auto-generated server certificate always contains:

- DNS names: `localhost` and the system host name (output of `hostname`)
- IP addresses: `127.0.0.1` and `::1`

Add every other name or address that browsers or agents use:

```yaml
server:
  hostnames:
    - "bor.example.com"
    - "bor"
    - "192.0.2.10"
    - "2001:db8::10"
```

Entries that parse as IP addresses become IP SANs; everything else becomes
a DNS SAN. The environment variable `BOR_HOSTNAMES` takes a comma-separated
list and replaces the YAML list. Restart the server; the certificate is
re-issued automatically.

Check the names in the current certificate:

```bash
openssl s_client -connect bor.example.com:8444 </dev/null 2>/dev/null \
  | openssl x509 -noout -ext subjectAltName
```

!!! warning
    If the name in an agent's `server.address` is missing from this list,
    the agent enrolls (enrollment uses the web port) but then fails to connect
    to port 8444 with a certificate name error.

## Agent certificate lifecycle

### Issuance

At enrollment the agent generates its own ECDSA P-256 key pair and sends
only a certificate signing request. The server:

- rejects keys weaker than RSA 3072, ECDSA P-256 or Ed25519,
- replaces the requested subject with `CN=<node name>`,
- issues a certificate valid for **90 days**, for client authentication
  only, with a random 128-bit serial number,
- records the serial number on the node.

The agent stores the key, certificate and CA certificate in
`/var/lib/bor/agent` (see [Agent configuration](agent.md#enrollment)).

### Renewal

The agent checks its certificate when it starts and every 12 hours while
it runs. When the certificate expires within **30 days**, the agent
generates a new key pair, requests a new certificate over mutual TLS on
port 8444, and replaces the files on disk. No token and no administrator
action are needed. If renewal fails, the agent logs `certificate renewal
failed` and tries again at the next check.

The agent uses the new certificate from its next connection on. The server
closes every agent connection after at most 24 hours (open streams get a
minute to finish), so a renewed certificate is in use within a day, and an
expired certificate cannot keep an old connection open.

Until the agent connects with the new certificate, the previous one stays
valid, so an agent that lost the server's answer can renew again. The
first connection with the new certificate revokes the previous one
(reason `superseded by renewal`). A certificate that was already replaced
cannot be renewed.

If a certificate expires anyway (for example the machine was switched off
for months), the agent can no longer connect and must be re-enrolled with a
new token.

### Revocation

Bor keeps its own revocation list in the database instead of CRLs or OCSP.
Every call on the policy port, and every authenticated call on the
enrollment port, checks it. An open policy stream checks it again every
minute and closes when its certificate is revoked or has expired. A
certificate is revoked when:

- you click **Revoke Certificate** on a node in **Nodes**, or
- you **Decommission** (delete) a node: its certificates are revoked before
  the record is removed, or
- the agent connects with a renewed certificate: the one it replaced is
  revoked.

Revoking or decommissioning a node revokes both its current certificate
and, shortly after a renewal, the previous one. Renewal never clears a
revocation.

A revoked agent must be re-enrolled with a new token. See
[Nodes](../user-guide/nodes.md).

## Trust the Bor CA

With the auto-generated certificate, browsers show a warning and agents
cannot verify the server until the Bor CA is trusted. Download the CA
certificate once and check its fingerprint against the server:

```bash
curl -fsSk https://bor.example.com/agent/ca.crt -o bor-ca.crt
openssl x509 -in bor-ca.crt -noout -subject -fingerprint -sha256
# Compare with the server:
sudo openssl x509 -in /var/lib/bor/pki/ca/ca.crt -noout -fingerprint -sha256
```

`-k` is needed for this first download only, because nothing can be
verified yet. Comparing the fingerprint closes that gap.

### Trust the Bor CA on endpoints

Add the CA to the system trust store of each endpoint before enrolling, and
set `server.insecure_skip_verify: false` in `/etc/bor/config.yaml`:

=== "Debian / Ubuntu"

    ```bash
    sudo cp bor-ca.crt /usr/local/share/ca-certificates/bor-ca.crt
    sudo update-ca-certificates
    ```

=== "Fedora / RHEL"

    ```bash
    sudo cp bor-ca.crt /etc/pki/ca-trust/source/anchors/bor-ca.crt
    sudo update-ca-trust
    ```

=== "openSUSE"

    ```bash
    sudo cp bor-ca.crt /etc/pki/trust/anchors/bor-ca.crt
    sudo update-ca-certificates
    ```

=== "Arch Linux"

    ```bash
    sudo cp bor-ca.crt /etc/ca-certificates/trust-source/anchors/bor-ca.crt
    sudo update-ca-trust
    ```

=== "Alpine"

    ```bash
    sudo cp bor-ca.crt /usr/local/share/ca-certificates/bor-ca.crt
    sudo update-ca-certificates
    ```

This also lets `apt`, `dnf` and `zypper` use the agent package repository
at `https://bor.example.com/agent/` without disabling verification.
Verify:

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://bor.example.com/agent/ca.crt
```

A `200` without a certificate error means the CA is trusted.

### Trust the Bor CA in browsers

- **Firefox** has its own certificate store: **Settings -> Privacy &
  Security -> Certificates -> View Certificates -> Authorities -> Import**,
  then tick **Trust this CA to identify websites**.
- **Chrome, Chromium and Edge on Linux** use the NSS database of the user.
  Import it under **Settings -> Privacy and security -> Security -> Manage
  certificates**, or from a shell:

    ```bash
    certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n "Bor CA" -i bor-ca.crt
    ```

- **Windows and macOS** administrator workstations: import `bor-ca.crt`
  into the system's trusted root store with the usual tools.

## Store the CA key in an HSM (PKCS#11)

The CA private key can live in a hardware security module or any PKCS#11
token instead of `ca.key`. All signing then happens inside the HSM; only
the public CA certificate is written to disk.

!!! warning "Not available in the distribution packages"
    PKCS#11 support is compiled in only when the server is built with the
    `pkcs11` build tag (CGO). The `bor-server` packages on GitHub releases
    and getbor.dev are built without it. With a package build, configuring
    `ca.pkcs11` stops the server with `PKCS#11 HSM support is not compiled
    in`. You need a server binary built with PKCS#11 support to use this
    feature.

Configuration (with a PKCS#11-enabled binary):

```yaml
ca:
  autogen_dir: "/var/lib/bor/pki/ca"   # ca.crt is stored here
  pkcs11:
    lib: "/usr/lib64/pkcs11/libsofthsm2.so"
    token_label: "bor-ca"
    key_label: "bor-ca-key"
```

```bash
# /etc/bor/server.env
BOR_CA_PKCS11_PIN=123456
```

Behavior at startup:

- If no key with `key_label` exists on the token, the server generates an
  ECDSA P-384 key on the token and a new CA certificate valid for
  `ca.lifetime` (10 years by default).
- If the key exists but the certificate file is missing, a new CA
  certificate is created for that key.
- Otherwise both are loaded.

Leave `ca.cert_file` and `ca.key_file` empty in this mode: the CA
certificate is kept at `<ca.autogen_dir>/ca.crt`, and setting `cert_file`
without `key_file` is rejected by the configuration check.

## FIPS 140-3 mode

The `bor-server` and `bor-agent` binaries are built against the Go FIPS
140-3 cryptographic module. By default they also allow non-FIPS algorithms
where those are preferred (X25519 key exchange is offered first). To
enforce FIPS-approved algorithms only, set `GODEBUG=fips140=on` in the
service environment with a systemd drop-in:

```bash
sudo systemctl edit bor-server
```

```ini
[Service]
Environment=GODEBUG=fips140=on
```

```bash
sudo systemctl restart bor-server
```

Repeat with `sudo systemctl edit bor-agent` on endpoints. In this mode
X25519 is no longer negotiated and P-256 or P-384 is used instead. All
certificates Bor generates (ECDSA P-256/P-384) and the TLS ciphers it
offers are FIPS-approved, so no other change is needed. Certificates you
supply yourself (`tls.cert_file`, `ca.cert_file`, LDAP CA) must also use
approved algorithms.

Verify that the setting is active:

```bash
systemctl show bor-server -p Environment
```
