# Kerberos Token-Free Agent Enrollment

Bor supports automatic, token-free enrollment for agents running on domain-joined Linux hosts using Kerberos (GSSAPI/SPNEGO). Domain-joined nodes authenticate with their machine keytab (`/etc/krb5.keytab`) and obtain a signed mTLS certificate without any administrator intervention — no enrollment tokens need to be generated or distributed.

This feature works with **FreeIPA** and **Active Directory** (Windows AD and Samba AD).

---

## Table of Contents

1. [How it works](#how-it-works)
2. [Prerequisites](#prerequisites)
3. [Server setup](#server-setup)
   - [FreeIPA](#server-setup--freeipa)
   - [Active Directory / Samba AD](#server-setup--active-directory--samba-ad)
4. [Agent setup](#agent-setup)
5. [Configuration reference](#configuration-reference)
6. [Node group assignment](#node-group-assignment)
7. [Security considerations](#security-considerations)
8. [Testing](#testing)
9. [Troubleshooting](#troubleshooting)
10. [Coexistence with token-based enrollment](#coexistence-with-token-based-enrollment)

---

## How it works

```
Domain-joined agent                 KDC (FreeIPA / AD)          Bor server
        │                                  │                         │
        │──── 1. AS-REQ (machine keytab) ──▶│                         │
        │◀─── 2. TGT ──────────────────────│                         │
        │                                  │                         │
        │──── 3. TGS-REQ (for Bor svc) ───▶│                         │
        │◀─── 4. Service ticket ───────────│                         │
        │                                  │                         │
        │──── 5. KerberosEnroll RPC (SPNEGO token + CSR) ───────────▶│
        │         (TLS, no client cert yet)                           │
        │                                  │──6. Validate SPNEGO ───▶│
        │                                  │   (keytab verify)       │
        │◀─── 7. Signed cert + CA cert ──────────────────────────────│
        │                                  │                         │
        │ (stores cert/key/CA on disk)      │                         │
        │                                  │                         │
        │════ 8. mTLS policy streaming ════════════════════════════▶ │
```

1. The agent reads the machine keytab (`/etc/krb5.keytab`) and obtains a Kerberos TGT from the KDC using the host principal (`host/<fqdn>@REALM`).
2. The agent requests a service ticket for the Bor service principal (e.g. `HTTP/bor.example.com@EXAMPLE.COM`).
3. The service ticket is wrapped in a SPNEGO NegTokenInit and sent alongside a fresh CSR to the Bor `KerberosEnroll` gRPC endpoint.
4. The server validates the SPNEGO token against its own service keytab. If valid, it signs the CSR with the internal CA and returns the certificate.
5. The agent stores the signed certificate and CA cert, then reconnects to the policy port using mTLS for normal operation.

This process happens automatically on the first run of the agent. No human interaction is required after domain join.

> **Key point — the Bor server does not need to be domain-joined.**
> Token validation is entirely offline: the server decrypts the AP_REQ using the key material in its service keytab, with no network call to the KDC. The keytab is exported from FreeIPA or AD and simply copied to the Bor server. The **agent** is the party that requires KDC access (to obtain its TGT and service ticket); the server is a passive verifier.

---

## Prerequisites

### Bor server

- A service keytab for the Bor service principal (see setup steps below).
- Network access from agents to the Bor server on the enrollment port (default 8443).
- **No domain membership required.** The Bor server does not need to be joined to the domain, enrolled in LDAP, or able to reach the KDC. Token validation is offline — the keytab is self-contained.

### Agent host

- The host must be **joined to the domain**:
  - FreeIPA: `ipa-client-install` completed successfully.
  - Active Directory: `realm join`, `adcli join`, or `net ads join` completed.
- `/etc/krb5.keytab` must exist and contain a host principal entry.
- `/etc/krb5.conf` must be correctly configured (set by `ipa-client-install` / SSSD / `realm`).
- Network access from the agent to the KDC on port 88 to obtain tickets.

---

## Server setup — FreeIPA

### 1. Register the Bor service

```bash
# On the FreeIPA server (as admin)
ipa service-add HTTP/bor.example.com
```

Replace `bor.example.com` with the FQDN of the host running the Bor server. The principal name `HTTP/bor.example.com` is the standard convention; use any service type you prefer (e.g. `bor/bor.example.com`), but it must match `service_principal` in the server config.

### 2. Export the service keytab

```bash
# On the Bor server host (must be FreeIPA-enrolled, run as root)
ipa-getkeytab \
    -s ipa.example.com \
    -p HTTP/bor.example.com@EXAMPLE.COM \
    -k /etc/bor/krb5.keytab

# Restrict permissions — the keytab is as sensitive as a private key
chown root:bor /etc/bor/krb5.keytab
chmod 640 /etc/bor/krb5.keytab
```

> **Tip:** If the Bor server is not itself FreeIPA-enrolled, use `ipa-getkeytab` from any enrolled host that has admin access. Copy the keytab to the Bor server securely (e.g. `scp` over an encrypted channel).

### 3. Verify the keytab

```bash
# List the principals in the keytab
klist -ekt /etc/bor/krb5.keytab

# Expected output:
# Keytab name: FILE:/etc/bor/krb5.keytab
# KVNO Timestamp           Principal
# ---- ------------------- ------------------------------------------------------
#    1 01/01/2026 00:00:00 HTTP/bor.example.com@EXAMPLE.COM (aes256-cts-hmac-sha1-96)
#    1 01/01/2026 00:00:00 HTTP/bor.example.com@EXAMPLE.COM (aes128-cts-hmac-sha1-96)
```

### 4. Configure the Bor server

```yaml
# /etc/bor/server.yaml
kerberos:
  enabled: true
  realm: "EXAMPLE.COM"
  keytab_file: "/etc/bor/krb5.keytab"
  service_principal: "HTTP/bor.example.com@EXAMPLE.COM"
  default_node_group_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
```

Get the node group UUID from the Bor web UI: **Node Groups** → **Edit** on the target group → copy the **Group ID** field.

Or via environment variables:

```bash
BOR_KERBEROS_ENABLED=true
BOR_KERBEROS_REALM=EXAMPLE.COM
BOR_KERBEROS_KEYTAB=/etc/bor/krb5.keytab
BOR_KERBEROS_PRINCIPAL=HTTP/bor.example.com@EXAMPLE.COM
BOR_KERBEROS_DEFAULT_NODE_GROUP=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

---

## Server setup — Active Directory / Samba AD

### Option A: Samba AD (Linux-based DC)

#### 1. Create a service account and keytab

```bash
# On the Samba AD DC (as root)
samba-tool user create bor-svc \
    --random-password \
    --description="Bor server service account"

# Set the Service Principal Name
samba-tool spn add HTTP/bor.example.com bor-svc

# Export the keytab for this SPN
samba-tool domain exportkeytab /etc/bor/krb5.keytab \
    --principal=HTTP/bor.example.com

chown root:bor /etc/bor/krb5.keytab
chmod 640 /etc/bor/krb5.keytab
```

#### 2. Verify the keytab

```bash
klist -ekt /etc/bor/krb5.keytab
```

### Option B: Windows Active Directory

#### 1. Create a service account using `setspn` and `ktpass`

On a Windows domain controller (PowerShell as Domain Admin):

```powershell
# Create a dedicated service account
New-ADUser `
    -Name "bor-svc" `
    -SamAccountName "bor-svc" `
    -UserPrincipalName "HTTP/bor.example.com@EXAMPLE.COM" `
    -AccountPassword (ConvertTo-SecureString "StrongRandom!" -AsPlainText -Force) `
    -PasswordNeverExpires $true `
    -CannotChangePassword $true `
    -Enabled $true

# Add the SPN
Set-ADUser bor-svc -ServicePrincipalNames @{Add="HTTP/bor.example.com"}

# Export the keytab
# AES256 is mandatory; RC4 (arcfour-hmac) is deprecated
ktpass `
    -princ HTTP/bor.example.com@EXAMPLE.COM `
    -mapuser EXAMPLE\bor-svc `
    -crypto AES256-SHA1 `
    -ptype KRB5_NT_PRINCIPAL `
    -pass StrongRandom! `
    -out C:\bor-krb5.keytab
```

Copy `bor-krb5.keytab` to the Bor server:

```bash
# On the Bor server
scp user@windc01.example.com:C:/bor-krb5.keytab /etc/bor/krb5.keytab
chown root:bor /etc/bor/krb5.keytab
chmod 640 /etc/bor/krb5.keytab
```

#### 2. Configure the Bor server

```yaml
# /etc/bor/server.yaml
kerberos:
  enabled: true
  realm: "EXAMPLE.COM"
  keytab_file: "/etc/bor/krb5.keytab"
  service_principal: "HTTP/bor.example.com@EXAMPLE.COM"
  default_node_group_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
```

---

## Agent setup

### 1. Join the host to the domain

**FreeIPA:**

```bash
# Install the FreeIPA client
dnf install -y freeipa-client        # Fedora / RHEL / Rocky

# Join the domain (creates /etc/krb5.keytab automatically)
ipa-client-install \
    --server=ipa.example.com \
    --domain=example.com \
    --realm=EXAMPLE.COM \
    --principal=admin \
    --password='admin-password' \
    --unattended
```

**Active Directory (using `realmd`):**

```bash
# Install realmd and SSSD
dnf install -y realmd sssd sssd-tools adcli samba-common-tools

# Discover the domain
realm discover EXAMPLE.COM

# Join the domain (creates /etc/krb5.keytab automatically)
realm join --user=Administrator EXAMPLE.COM

# Verify
realm list
klist -ekt /etc/krb5.keytab
```

### 2. Configure the Bor agent

```yaml
# /etc/bor/config.yaml
server:
  address: "bor.example.com"
  enrollment_port: 8443
  policy_port: 8444
  insecure_skip_verify: false  # false once the CA cert is trusted

kerberos:
  enabled: true
  keytab_file: "/etc/krb5.keytab"
  service_principal: "HTTP/bor.example.com@EXAMPLE.COM"

  # kdc: (optional) hostname or IP of the domain controller / KDC.
  # When set the agent builds its Kerberos config from this address and the
  # realm in service_principal, completely bypassing /etc/krb5.conf.
  # Recommended on Fedora/RHEL where /etc/krb5.conf may use values that the
  # agent's Kerberos library does not accept (e.g. dns_canonicalize_hostname = fallback).
  # kdc: "dc1.example.com"

  # machine_principal: (optional) the Kerberos principal of this host's
  # machine account as it appears in /etc/krb5.keytab.
  # Run "klist -kte /etc/krb5.keytab" to list all principals.
  #
  # Leave empty for FreeIPA hosts — the default "host/<hostname>" is correct.
  #
  # For Active Directory / Samba AD hosts joined with realm(1) or adcli the
  # computer account principal is "<HOSTNAME>$@REALM", NOT "host/<fqdn>".
  # The "host/*" and "HOST/*" entries in an AD keytab are Service Principal
  # Names (SPNs) attached to the account — they cannot obtain a TGT.
  # Only the "<HOSTNAME>$" account entry can perform the initial AS exchange.
  # machine_principal: "MYHOSTNAME$@EXAMPLE.COM"

enrollment:
  data_dir: "/var/lib/bor/agent"
```

### 3. First run

```bash
# Run the agent — no --token flag needed
bor-agent

# Expected log output:
# Kerberos keytab found at /etc/krb5.keytab – attempting Kerberos enrollment
# Kerberos enrollment successful: principal=host/worker01.example.com@EXAMPLE.COM node_group=linux-desktops
# Enrollment successful. Certificates stored in /var/lib/bor/agent
```

After successful enrollment, the agent stores its certificate at `/var/lib/bor/agent/` and exits with instructions to enable the systemd service.

```bash
systemctl enable --now bor-agent
```

---

## Configuration reference

### Server (`/etc/bor/server.yaml`)

| Field | Env var | Required | Description |
|-------|---------|----------|-------------|
| `kerberos.enabled` | `BOR_KERBEROS_ENABLED` | Yes | Set to `true` to activate the `KerberosEnroll` endpoint |
| `kerberos.realm` | `BOR_KERBEROS_REALM` | Yes | Kerberos realm, e.g. `EXAMPLE.COM` (must be uppercase) |
| `kerberos.keytab_file` | `BOR_KERBEROS_KEYTAB` | Yes | Absolute path to the service keytab file |
| `kerberos.service_principal` | `BOR_KERBEROS_PRINCIPAL` | Yes | Full service principal, e.g. `HTTP/bor.example.com@EXAMPLE.COM` |
| `kerberos.default_node_group_id` | `BOR_KERBEROS_DEFAULT_NODE_GROUP` | Yes | UUID of the node group for auto-enrolled agents |

### Agent (`/etc/bor/config.yaml`)

| Field | Default | Description |
|-------|---------|-------------|
| `kerberos.enabled` | `false` | Set to `true` to enable Kerberos enrollment |
| `kerberos.keytab_file` | `/etc/krb5.keytab` | Path to the machine keytab |
| `kerberos.service_principal` | _(empty)_ | Full Kerberos principal of the Bor server service |
| `kerberos.kdc` | _(empty)_ | KDC hostname/IP. When set, `/etc/krb5.conf` is bypassed entirely. Recommended on Fedora/RHEL. |
| `kerberos.machine_principal` | `host/<hostname>` | Machine account principal in the keytab. AD/Samba hosts: use `HOSTNAME$@REALM`. FreeIPA hosts: leave empty. |

---

## Node group assignment

All agents enrolled via Kerberos are placed into the node group specified by `default_node_group_id`. To use different groups for different hosts, create multiple node groups in the UI and set up automation (e.g. a post-enrollment webhook or a script that moves nodes via the REST API based on naming conventions).

Future versions of Bor may support principal-based group assignment rules (e.g. map `host/*.servers.example.com` to the "Servers" group and `host/*.workstations.example.com` to the "Workstations" group).

### Creating a node group for Kerberos agents

1. Open the Bor web UI.
2. Navigate to **Node Groups** → **Create Node Group**.
3. Enter a name (e.g. "Linux Desktops — Kerberos").
4. After creation, click **Edit** on the group — the **Group ID** field at the top shows the UUID with a copy button.
5. Set `BOR_KERBEROS_DEFAULT_NODE_GROUP` (or `kerberos.default_node_group_id` in `server.yaml`) to that UUID.

---

## Security considerations

### Keytab protection

The service keytab on the Bor server host is the primary secret. Anyone with access to the keytab can impersonate the Bor service or forge enrollment tokens.

```bash
# Minimum required permissions
chown root:bor /etc/bor/krb5.keytab
chmod 640 /etc/bor/krb5.keytab

# If the Bor server runs as a dedicated system user:
chown root:bor-server /etc/bor/krb5.keytab
chmod 640 /etc/bor/krb5.keytab
```

- Store the keytab in a directory with `700` permissions, readable only by the `bor` service user.
- Rotate the keytab periodically using `ipa-getkeytab` (FreeIPA) or `ktpass` (AD). The new keytab can be deployed without service downtime by including both the old and new KVNO entries.
- Never store the keytab in a Docker image layer, version control, or a world-readable location.

### Machine keytab on agent hosts

The machine keytab `/etc/krb5.keytab` is created by the domain join process and is owned by root:

```
-rw------- 1 root root /etc/krb5.keytab
```

The Bor agent must run as root (or with `CAP_DAC_READ_SEARCH`) to read the machine keytab. This is consistent with how other Kerberos-aware services (SSSD, NFS, SSH with GSSAPI) operate on Linux.

### Replay protection

Kerberos AP_REQ messages include an authenticator with a timestamp and nonce. The gokrb5 library enforces a clock skew window (default 5 minutes). Ensure the clocks of all domain members (KDC, Bor server, agent hosts) are synchronized using NTP/chrony.

### What Kerberos enrollment does NOT provide

- **Authorization** — any valid domain member that can obtain a ticket for the Bor service can enroll. If you need to restrict enrollment to specific hosts (e.g. only hosts in a specific OU), implement that at the AD/FreeIPA level by restricting which hosts can obtain a service ticket, or use enrollment tokens for sensitive node groups.
- **Ongoing authentication** — after enrollment, agents use mTLS certificate authentication. Kerberos is used only for the initial enrollment step.

---

## Testing

### Verify the keytab works from the Bor server host

```bash
# Obtain a ticket using the service keytab
kinit -k -t /etc/bor/krb5.keytab HTTP/bor.example.com@EXAMPLE.COM
klist

# Expected output:
# Credentials cache: API:...
# Principal: HTTP/bor.example.com@EXAMPLE.COM
# Issued                Expires               Principal
# Jan 01 00:00:00 2026  Jan 01 10:00:00 2026  krbtgt/EXAMPLE.COM@EXAMPLE.COM
```

If `kinit` succeeds, the keytab is valid and the Bor server will be able to validate agent tokens.

### Verify the machine keytab on an agent host

```bash
# List the principals in the machine keytab
klist -ekt /etc/krb5.keytab

# Test authentication
kinit -k host/worker01.example.com@EXAMPLE.COM
klist
kdestroy
```

### Test KDC connectivity

```bash
# From the agent host
nc -zv ipa.example.com 88        # Kerberos port (TCP)
nc -uv ipa.example.com 88        # Kerberos port (UDP)
```

### Run the agent in verbose mode (first enrollment)

```bash
BOR_LOG_LEVEL=debug bor-agent 2>&1 | grep -i kerberos
```

---

## Troubleshooting

### "failed to load /etc/krb5.conf"

The `/etc/krb5.conf` file is missing or malformed. On FreeIPA-enrolled hosts, it is created by `ipa-client-install`. On AD-joined hosts, it is created by `realmd` / `sssd`.

```bash
# Verify the file exists and has the correct realm
grep default_realm /etc/krb5.conf
```

### "Kerberos login failed (keytab=..., principal=host/...@REALM)"

Possible causes:

1. **Wrong principal in keytab** — the machine keytab may use a different principal name. Run `klist -ekt /etc/krb5.keytab` to list all principals.
2. **Keytab is stale** — if the host was re-joined to the domain, the old keytab entries are invalid. Re-join or run `ipa-getkeytab` / `net ads keytab create` again.
3. **Clock skew** — Kerberos requires clocks within 5 minutes. Run `chronyc tracking` and ensure the host is synchronising with the domain's NTP server.
4. **KDC unreachable** — check DNS and firewall for port 88 (TCP and UDP).

### "failed to get service ticket for HTTP/bor.example.com@EXAMPLE.COM"

1. The service principal does not exist in the KDC. Verify it was registered: `ipa service-find HTTP/bor.example.com` (FreeIPA) or `setspn -L bor-svc` (AD).
2. The realm in `service_principal` does not match the agent's configured realm. Check `/etc/krb5.conf`.
3. The Bor server's FQDN (`bor.example.com`) does not resolve in DNS. Add a DNS A record or alias.

### "AP_REQ verification failed" on the server

1. **KVNO mismatch** — the key version number in the service ticket does not match the keytab. Regenerate the keytab: `ipa-getkeytab ... -k /etc/bor/krb5.keytab`.
2. **Encryption type mismatch** — the KDC encrypted the service ticket with an etype the server keytab does not contain. Use `klist -ekt /etc/bor/krb5.keytab` to list supported etypes. The error message names the missing etype:
   - **etype 18 / 17** (AES256 / AES128) — regenerate the keytab with AES keys.
   - **etype 23** (RC4-HMAC / arcfour) — the KDC chose RC4, likely because the agent machine's keytab supports it and the service account has no AES-only restriction. Two fixes (use either):
     - **Preferred (AD/Samba AD):** set `msDS-SupportedEncryptionTypes = 24` on the `bor-svc` service account. This tells the KDC to use only AES (bit 3 = AES128, bit 4 = AES256), regardless of what the client machine supports:
       ```powershell
       # Windows AD (PowerShell)
       Set-ADUser bor-svc -KerberosEncryptionType AES128,AES256
       ```
       ```bash
       # Samba AD (samba-tool)
       samba-tool user modify bor-svc --option='msDS-SupportedEncryptionTypes=24' \
           -U Administrator --URL=ldap://dc1.example.com
       ```
     - **Workaround:** add RC4 entries to the server keytab. This is not recommended for new deployments; RC4 is deprecated in RFC 8429.
3. **Wrong principal** — the service principal in the keytab does not match `service_principal` in the server config (case-sensitive).

### "failed to load /etc/krb5.conf: invalid boolean value"

Fedora/RHEL systems set `dns_canonicalize_hostname = fallback` in `/etc/krb5.conf`. The agent automatically sanitises this; if you are running an older binary, either upgrade or change the value to `true` manually:

```bash
sudo sed -i 's/dns_canonicalize_hostname = fallback/dns_canonicalize_hostname = true/' /etc/krb5.conf
```

Alternatively, set `kerberos.kdc` in the agent config to bypass `/etc/krb5.conf` entirely.

### "KDC_ERR_C_PRINCIPAL_UNKNOWN" during AS exchange

The agent is trying to authenticate with the wrong machine principal. On AD/Samba AD, the computer account is `HOSTNAME$`, not `host/hostname`. Run `klist -kte /etc/krb5.keytab` to find the correct principal, then set `kerberos.machine_principal` in the agent config:

```yaml
kerberos:
  machine_principal: "MYHOSTNAME$@EXAMPLE.COM"
```

### "KDC_ERR_S_PRINCIPAL_UNKNOWN" during TGS exchange

The Bor service principal (`HTTP/bor.example.com`) is not registered as an SPN in the directory.

- **FreeIPA:** `ipa service-show HTTP/bor.example.com` — if missing, re-run `ipa service-add`.
- **AD/Samba AD:** `samba-tool spn list bor-svc -U Administrator --URL=ldap://dc1.example.com` — if missing, add with `samba-tool spn add HTTP/bor.example.com bor-svc ...`.

### "expected SPNEGO NegTokenInit, got NegTokenResp"

The agent sent a SPNEGO response token instead of an init token. This should not happen under normal operation. File a bug report with the full agent log.

### Agent falls back to token enrollment despite keytab existing

Check the agent log for the reason:

```
Kerberos enrollment failed: <reason> – falling back to token-based enrollment
```

The agent continues with token-based enrollment if Kerberos fails, so domain-joined hosts that are not yet enrolled or have a keytab issue will not be silently stuck.

---

## Coexistence with token-based enrollment

Kerberos and token-based enrollment can be used simultaneously in the same Bor installation. The agent tries Kerberos first (if configured) and falls back to the token if Kerberos fails or the keytab is absent. This allows:

- **Mixed environments** — domain-joined desktops use Kerberos; non-domain servers use tokens.
- **Phased rollout** — enable Kerberos on the server and agent; non-enrolled hosts will use tokens until the domain join is complete.
- **Manual override** — running `bor-agent --token <TOKEN>` always uses token enrollment, even when Kerberos is configured. This is useful for placing a node in a specific non-default group.

### Enrollment priority

```
bor-agent first run
├─ Kerberos enabled + keytab present?
│   ├─ Yes → attempt KerberosEnroll
│   │   ├─ Success → enrolled, start policy streaming
│   │   └─ Failure → log warning, continue to token enrollment
│   └─ No → continue to token enrollment
└─ --token flag provided?
    ├─ Yes → attempt token Enroll
    │   └─ Success → enrolled, start policy streaming
    └─ No → fatal error with instructions
```
