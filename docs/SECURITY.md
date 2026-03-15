# Security Reference

This document describes the cryptographic algorithms, standards compliance, deployment security practices, and HSM integration for the Bor Policy Management System.

---

## Table of Contents

1. [Cryptographic Algorithms](#cryptographic-algorithms)
2. [TLS Configuration](#tls-configuration)
3. [PKI and Certificate Lifecycle](#pki-and-certificate-lifecycle)
4. [Authentication](#authentication)
5. [Multi-Factor Authentication (TOTP)](#multi-factor-authentication-totp)
6. [Standards Compliance](#standards-compliance)
7. [FIPS 140-3 Build and Runtime](#fips-140-3-build-and-runtime)
8. [Deployment Security Checklist](#deployment-security-checklist)
9. [HSM Integration (PKCS#11)](#hsm-integration-pkcs11)

---

## Cryptographic Algorithms

### Asymmetric Keys

| Role | Algorithm | Key Size | Security Level | Lifetime |
|---|---|---|---|---|
| Internal CA | ECDSA P-384 | 384-bit | 192-bit | 10 years |
| Server TLS cert | ECDSA P-256 | 256-bit | 128-bit | 365 days |
| Agent client cert | ECDSA P-256 | 256-bit | 128-bit | 90 days |

**Why P-384 for the CA?** A CA key signs certificates that will be trusted for its entire lifetime. P-384 provides 192-bit security, appropriate for a 10-year root. BSI TR-02102-1 (2024) withdraws RSA-2048 approval at end of 2025; P-384 is approved by all referenced EU standards.

**Why P-256 for end-entity certs?** P-256 provides 128-bit security, sufficient for short-lived certs (90–365 days), and is marginally faster than P-384 for TLS handshakes.

All private keys are stored in **PKCS#8** format (`PRIVATE KEY` PEM header). Legacy PKCS#1 (`RSA PRIVATE KEY`) is accepted only for externally-provided CA keys, to allow migration.

### Key Storage Permissions

| File | Mode | Notes |
|---|---|---|
| `ca.key` | `0600` | CA private key; owner read/write only |
| `ui.key` | `0600` | Server TLS private key |
| `agent.key` | `0600` | Agent private key |
| `ca.crt` | `0644` | CA certificate; distributed to agents |
| `ui.crt` | `0600` | Server TLS certificate |
| `agent.crt` | `0644` | Agent client certificate |

### Hash Functions

| Usage | Algorithm | Parameters |
|---|---|---|
| Certificate serial numbers | `crypto/rand` (128-bit random integer) | — |
| Web UI password hashing | PBKDF2-SHA-256 | 600,000 iterations, 16-byte salt, 32-byte key |
| JWT signing | HMAC-SHA-256 (HS256) | Symmetric, key from `JWT_SECRET` |
| TOTP code generation | HMAC-SHA-256 or HMAC-SHA-512 | RFC 6238, 6 digits, 30-second period |
| TOTP secret encryption at rest | AES-256-GCM | Random 12-byte nonce per ciphertext; key from `BOR_MFA_SECRET` |
| TOTP backup code storage | SHA-256 | Single hash, hex-encoded; consumed on use |

**PBKDF2 parameters** follow NIST SP 800-132 and OWASP recommendations. The encoded format is:

```
$pbkdf2$sha256$600000$<base64url-salt>$<base64url-key>
```

Verification uses a constant-time byte comparison to prevent timing attacks.

### Symmetric / AEAD

TLS 1.3 cipher suites are selected automatically by the Go TLS stack. For TLS 1.2 (port 8443 only), the following suites are explicitly configured:

| Cipher Suite | Key Exchange | AEAD | MAC |
|---|---|---|---|
| `TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384` | ECDHE | AES-256-GCM | SHA-384 |
| `TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384` | ECDHE | AES-256-GCM | SHA-384 |
| `TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256` | ECDHE | AES-128-GCM | SHA-256 |
| `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256` | ECDHE | AES-128-GCM | SHA-256 |

All four use ECDHE key exchange (forward secrecy) and AEAD encryption. Static RSA key exchange and CBC-mode suites are not permitted.

### Elliptic Curves

| Curve | FIPS 140-3 | BSI TR-02102-1 | ANSSI RGS | ETSI TS 119 312 | Notes |
|---|---|---|---|---|---|
| P-256 (secp256r1) | Yes | Yes | Yes | Yes | Default for end-entity certs |
| P-384 (secp384r1) | Yes | Yes | Yes | Yes | Used for CA key |
| X25519 | No | Yes (2024) | Yes | Yes | Listed for TLS; removed at runtime when `GODEBUG=fips140=on` |

X25519 is included in curve preferences for performance in standard deployments. It is automatically removed by the Go runtime when FIPS mode is active. If strict FIPS compliance is required, set `GODEBUG=fips140=on` at runtime.

---

## TLS Configuration

Bor runs two separate HTTPS listeners with different security postures.

### Port 8443 — UI and Enrollment

Serves the admin web UI (REST) and the enrollment gRPC endpoint. Browsers and unenrolled agents connect here.

| Setting | Value |
|---|---|
| Minimum TLS version | TLS 1.2 |
| Client certificate | Optional (`VerifyClientCertIfGiven`) |
| Protocols | HTTP/2, HTTP/1.1 |
| TLS 1.2 cipher suites | ECDHE+AEAD only (see table above) |
| Curve preferences | X25519, P-256, P-384 |

The enrollment RPC (`/bor.enrollment.v1.EnrollmentService/Enroll`) is exempt from the mandatory client certificate requirement at the gRPC interceptor layer because this is the bootstrap call that exchanges a one-time token for a certificate. All other RPCs on this port require a valid, non-revoked client certificate.

### Port 8444 — Agent Policy Stream (mTLS)

Serves the policy streaming gRPC endpoint. Only enrolled agents connect here.

| Setting | Value |
|---|---|
| Minimum TLS version | TLS 1.3 |
| Client certificate | Mandatory (`RequireAndVerifyClientCert`) |
| Protocols | HTTP/2 only |
| TLS 1.2 cipher suites | Not applicable (TLS 1.3 minimum) |
| Curve preferences | X25519, P-256, P-384 |

Every RPC on port 8444 is intercepted by `RequireClientCertInterceptor`. The interceptor extracts the client certificate from the TLS peer context, reads its serial number, and checks the serial against the revocation table in the database before allowing the call to proceed.

### Certificate Revocation

Bor does not use CRL or OCSP. For a private, closed PKI, real-time per-RPC database revocation checks are architecturally superior:

- **CRL** requires periodic polling and can be up to `nextUpdate` stale.
- **OCSP** requires an external HTTP responder.
- **Bor's approach**: every gRPC call checks `SELECT COUNT(*) FROM revoked_certificates WHERE serial = $1`. Revocation is effective immediately on the next request after the record is written.

---

## PKI and Certificate Lifecycle

### Internal CA

At startup, if no CA cert/key path is configured, the server auto-generates an internal CA:

```
/var/lib/bor/pki/ca/ca.crt  (ECDSA P-384, self-signed, 10 years)
/var/lib/bor/pki/ca/ca.key  (PKCS#8, mode 0600)
```

The CA certificate is distributed to each agent during enrollment so the agent can verify the server's TLS certificate and establish mTLS.

### Server TLS Certificate

The server TLS certificate is signed by the internal CA (or self-signed if no CA is configured). It is re-generated automatically if the existing certificate was not signed by the current CA — for example, after a CA rotation.

Subject Alternative Names always include `localhost`, `127.0.0.1`, `::1`, and the system hostname. Additional SANs can be added with `BOR_HOSTNAMES`.

### Agent Certificate Lifecycle

```
1. Admin generates a one-time token (5-minute TTL, 256-bit random, single-use)
2. Agent generates an ECDSA P-256 key pair locally
3. Agent creates a CSR (CN = node name, O = "Bor Agent")
4. Agent calls Enroll RPC on port 8443 with token + CSR
5. Server verifies token, signs CSR (90-day cert, serial = 128-bit random)
6. Agent persists: agent.crt (0644), agent.key (0600), ca.crt (0644)
7. Agent connects to port 8444 using mTLS from this point on
```

**Certificate renewal** is triggered automatically when the certificate expires within 30 days. The agent generates a new key pair, submits a CSR over the existing mTLS connection, and atomically replaces the key and certificate on disk. No human intervention is required.

**Re-enrollment** is performed by running `bor-agent --token <NEW_TOKEN>`. If an existing enrollment is present, the old certificate, key, and CA cert are deleted before re-enrollment proceeds. This is the correct procedure after CA rotation or when moving a node to a different group.

---

## Authentication

### Web UI — JWT

The REST API uses signed JSON Web Tokens (HS256). Tokens are issued by the `/api/v1/auth/login` endpoint and must be presented in the `Authorization: Bearer <token>` header on all protected endpoints.

| Property | Value |
|---|---|
| Algorithm | HS256 (HMAC-SHA-256) |
| Lifetime | 24 hours |
| Claims | `user_id`, `username`, `exp`, `iat`, `sub` |
| Secret | `JWT_SECRET` environment variable |

The JWT secret must be a long, randomly generated value in production. The default `"change-me-in-production"` is rejected at startup if not overridden (see deployment checklist).

### Agent — mTLS

Agents do not use API keys or tokens after enrollment. Identity is established entirely by the mutual TLS handshake on port 8444. The gRPC interceptor extracts the certificate serial from the verified TLS peer and checks it against the revocation table on every call.

### LDAP (Optional)

LDAP authentication is enabled by setting `LDAP_ENABLED=true`. Successful LDAP authentication creates or updates a local user record. Local (PBKDF2) and LDAP users can coexist.

LDAP users are subject to TOTP MFA in the same way as local users. Bor performs the LDAP bind itself, so it is responsible for applying the second factor. The login flow for an LDAP user with MFA enabled is: username → TOTP code → LDAP password.

---

## Multi-Factor Authentication (TOTP)

Bor supports Time-based One-Time Password (TOTP) two-factor authentication for all user accounts managed by the server — both local (PBKDF2 password) and LDAP-authenticated users. TOTP is applied on top of the primary credential check regardless of where the password is verified.

The only accounts exempt from MFA are those authenticated entirely by an external identity provider that handles MFA itself — specifically future OAuth 2.0 / SAML 2.0 integrations (not yet implemented). LDAP is not such an integration: Bor verifies the LDAP bind itself, so Bor is responsible for the second factor.

### How It Works

Authentication with MFA uses a three-step state machine rather than a single username-and-password form:

```
POST /api/v1/auth/begin   { "username": "alice" }
  → { "session_token": "<5-min JWT>", "next": "totp" }   # if MFA enabled

POST /api/v1/auth/step    { "session_token": "...", "type": "totp", "credential": "123456" }
  → { "session_token": "<new 5-min JWT>", "next": "password" }

POST /api/v1/auth/step    { "session_token": "...", "type": "password", "credential": "hunter2" }
  → { "token": "<24h JWT>", "user": { ... } }            # full access granted
```

When MFA is not enabled for a user, the `begin` step returns `next: "password"` directly and the TOTP step is skipped.

Session tokens issued by `/auth/begin` and the TOTP `/auth/step` carry `session_type: "auth_session"` in their claims and expire in 5 minutes. The regular auth middleware rejects session tokens — they cannot be used to call protected API endpoints.

### TOTP Parameters

| Parameter | Value |
|---|---|
| Standard | RFC 6238 (TOTP) |
| Algorithm | SHA-256 (default) or SHA-512 (admin-configurable) |
| Digits | 6 |
| Period | 30 seconds |
| Clock skew tolerance | ±1 period (accepts codes from T-1 and T+1) |
| Library | `github.com/pquerna/otp` |

SHA-256 is the default because it is FIPS 140-3 approved and widely supported by authenticator apps (FreeOTP+, Aegis, Google Authenticator, Authy). SHA-512 is available for deployments that require the stronger hash; verify that your chosen authenticator app supports `SHA512` before enabling it.

### Secret Storage

TOTP secrets are encrypted at rest using **AES-256-GCM** before being stored in the `user_mfa` database table. The encryption key is derived from the `BOR_MFA_SECRET` environment variable (SHA-256 hash). If `BOR_MFA_SECRET` is not set, it falls back to `JWT_SECRET`.

It is strongly recommended to set a dedicated `BOR_MFA_SECRET` that is independent of the JWT signing secret:

```bash
BOR_MFA_SECRET=$(openssl rand -hex 32)
```

| Stored field | Format |
|---|---|
| `totp_secret` | AES-256-GCM ciphertext, base64-encoded (nonce prepended) |
| `backup_codes` | SHA-256 hashes of each plain code, stored as a PostgreSQL `TEXT[]` |

### Backup Codes

During MFA setup, the server generates **8 single-use backup codes**. Each code is a 10-character uppercase hex string formatted as `XXXXX-XXXXX` for readability. Codes are hashed with SHA-256 before storage and consumed on use. The plain codes are returned to the user once and are not retrievable afterwards.

Backup codes can be used in place of the TOTP code at the TOTP step of authentication. After use, the consumed code is removed from the stored list.

### Enforcing MFA for All Users

Administrators can require MFA for all local accounts globally. When enforcement is active:

- Any local user who has not set up MFA will see a mandatory setup screen immediately after login. The application is not accessible until MFA is configured.
- Users can log out from the setup screen without completing setup.
- Only future OAuth/SAML users would be exempt from this requirement. LDAP users are subject to MFA enforcement in the same way as local users.

#### Via the Admin UI

1. Log in as a Super Admin or Org Admin.
2. Go to **Settings → MFA Settings**.
3. Enable **"Require MFA for all local users"**.
4. Optionally change the TOTP algorithm (SHA-256 or SHA-512). Changing the algorithm only affects new enrolments; existing users keep their current algorithm until they re-enrol.
5. Click **Save**.

#### Via environment variable / config file

The setting is stored in the `agent_settings` table. It can also be pre-seeded via a database migration or set directly:

```sql
UPDATE agent_settings SET value = 'true' WHERE key = 'mfa_required';
UPDATE agent_settings SET value = 'SHA512' WHERE key = 'totp_algorithm';
```

### Personal MFA Setup (Users)

Individual users can enrol and manage their own TOTP from the **Account Security** dialog, accessible from the user menu in the top-right corner of the header. This menu is available to all authenticated users regardless of RBAC role.

**To enrol:**

1. Click the user menu (top-right) → **Account security**.
2. Click **Enable MFA**.
3. Scan the QR code with an authenticator app, or enter the manual secret.
4. Enter the 6-digit code from the app and click **Verify**.
5. Store the displayed backup codes in a safe place — they are shown only once.

**To disable:**

1. Click the user menu → **Account security**.
2. Click **Disable MFA** and confirm your current password.

### Emergency Recovery — Resetting a User's MFA

If an administrator has lost access to their authenticator app and no backup codes remain, MFA can be reset using the server binary's `--reset-mfa` flag. This flag connects directly to the database, deletes the user's MFA record, and exits — the server daemon is not started.

```bash
bor-server --reset-mfa <username>
```

**Example:**

```bash
# Disable MFA for user "admin"
/usr/sbin/bor-server --reset-mfa admin
```

Expected output on success:

```
2026/03/15 14:22:01 MFA disabled for user "admin" (id=3fa85f64-5717-4562-b3fc-2c963f66afa6)
```

If the username is not found:

```
2026/03/15 14:22:01 reset-mfa failed: user "alice" not found
exit status 1
```

**Security considerations for this command:**

- The command requires local access to the server host and a correctly configured environment (database credentials must be available via environment variables or `/etc/bor/server.env`).
- It should be run as the `bor` service user or `root`.
- The action is **not** recorded in the application audit log (since it bypasses the running server). Record the operation in your out-of-band change log.
- After resetting MFA, the affected user can log in with password only. If MFA enforcement is active, they will be prompted to enrol immediately on next login.

---

## Standards Compliance

### FIPS 140-3

| Component | Status | Notes |
|---|---|---|
| ECDSA P-256 / P-384 | Approved | CAVP-validated implementation via `GOFIPS140=v1.0.0` |
| AES-128-GCM / AES-256-GCM | Approved | |
| SHA-256 / SHA-384 | Approved | |
| HMAC-SHA-256 | Approved | Used for JWT signing |
| PBKDF2-SHA-256 | Approved | 600,000 iterations |
| HMAC-SHA-256 (TOTP) | Approved | RFC 6238 TOTP default algorithm |
| HMAC-SHA-512 (TOTP) | Approved | RFC 6238 TOTP optional algorithm |
| AES-256-GCM (TOTP secrets) | Approved | At-rest encryption of TOTP secrets in the database |
| X25519 | **Not approved** | Automatically removed when `GODEBUG=fips140=on` |
| RSA-2048 (external CA keys) | Approved | Accepted for compatibility; not generated by Bor |

Bor binaries are built with `GOFIPS140=v1.0.0`, which pins the Go 1.24 FIPS 140-3 validated crypto snapshot (CAVP certificate **A6650**). This ensures the binary contains the validated implementation regardless of the host Go toolchain version.

To enforce FIPS-only algorithms at runtime (removes X25519, enforces P-256/P-384 only):

```bash
GODEBUG=fips140=on /usr/sbin/bor-server
```

### BSI TR-02102-1 and TR-02102-2 (Germany)

| Requirement | Implementation |
|---|---|
| Key agreement | ECDHE only; static RSA key exchange excluded |
| Symmetric encryption | AES-GCM (AEAD) only; no CBC |
| Elliptic curves | P-256, P-384 (BSI-approved); X25519 (approved from 2024) |
| TLS version | Minimum TLS 1.2; TLS 1.3 on agent-only port |
| Hash in TLS | SHA-256 / SHA-384 |
| CA key size | P-384 (192-bit security; RSA-2048 withdrawn end of 2025) |
| Key encoding | ECDSA certs use `KeyUsageDigitalSignature` only; `KeyUsageKeyEncipherment` absent (RFC 5480) |

### ANSSI RGS / Guide de sécurité TLS (France)

| Requirement | Implementation |
|---|---|
| Approved curves | P-256 (NIST P-256 = anssi-FRP256v1 equivalent), P-384 |
| AEAD mandatory | Yes — all cipher suites use GCM |
| Forward secrecy | Yes — ECDHE on all connections |
| Minimum TLS | TLS 1.2; TLS 1.3 recommended for agent connections |

### ETSI TS 119 312 and EN 319 412 (EU eIDAS)

| Requirement | Implementation |
|---|---|
| Signature algorithms | ECDSA with P-256 (SHA-256), ECDSA with P-384 (SHA-384) |
| Key usage for ECDSA | `digitalSignature` only (no `keyEncipherment`) — compliant with RFC 5480 and EN 319 412-1 |
| CA key size | P-384 (≥192-bit security required for new CAs) |
| Certificate profiles | BasicConstraints, correct EKU (serverAuth / clientAuth), random serial numbers |

### NIS2 / ENISA Guidelines

The NIS2 Directive requires that operators of essential and important entities apply appropriate technical measures. Bor's security architecture addresses the relevant technical controls:

- **Encryption in transit**: All agent-server communication uses TLS 1.3 with mTLS.
- **Access control**: RBAC on the web UI; certificate-based identity for agents.
- **Audit logging**: All state-changing API operations are recorded in the audit log table.
- **Supply chain**: Builds use a pinned, FIPS-validated crypto module.

### NCSC Cyber Essentials / TLS Profile (UK)

| Requirement | Implementation |
|---|---|
| TLS 1.2+ | Yes (TLS 1.2 minimum on port 8443; TLS 1.3 on port 8444) |
| Forward secrecy | Yes — ECDHE on all connections |
| Authenticated encryption | Yes — GCM only |
| Weak cipher exclusion | Yes — no NULL, RC4, 3DES, CBC-mode, anonymous, export |

---

## FIPS 140-3 Build and Runtime

### Building

All standard build targets use `GOFIPS140=v1.0.0`:

```bash
make server          # Standard FIPS 140-3 build
make agent           # Standard FIPS 140-3 build
make server-pkcs11   # FIPS 140-3 + PKCS#11 HSM support (CGO required)
```

The `GOFIPS140=v1.0.0` build environment variable instructs the Go toolchain to embed the FIPS 140-3 validated crypto module snapshot identified by CAVP certificate A6650. Regardless of which Go version is installed on the build host, the resulting binary uses the validated implementation.

### Runtime Enforcement

FIPS 140-3 mode can be enforced at runtime independently of the build flag:

```bash
# Strict FIPS mode — only FIPS-approved algorithms permitted
GODEBUG=fips140=on /usr/sbin/bor-server

# Via systemd override
systemctl edit bor-server
# Add:
# [Service]
# Environment=GODEBUG=fips140=on
```

In strict FIPS mode:
- X25519 is removed from TLS curve preferences (P-256 becomes the preferred curve).
- Any call to a non-FIPS algorithm panics at runtime.

Without `GODEBUG=fips140=on`, the binary still contains the validated crypto module but does not restrict algorithm selection — X25519 may be negotiated.

---

## Deployment Security Checklist

### Before First Start

- [ ] **Change the JWT secret**: Set `JWT_SECRET` to at least 32 bytes of random data.
  ```bash
  JWT_SECRET=$(openssl rand -hex 32)
  ```

- [ ] **Set the MFA encryption secret**: Set `BOR_MFA_SECRET` to at least 32 bytes of random data, separate from the JWT secret. TOTP secrets are encrypted at rest with a key derived from this value.
  ```bash
  BOR_MFA_SECRET=$(openssl rand -hex 32)
  ```

- [ ] **Change the admin password**: Set `BOR_ADMIN_PASSWORD` to a strong password. The default `"admin"` is logged as a warning and must not be used in production.

- [ ] **Enable database TLS**: Set `DB_SSLMODE=verify-full` and provide a CA certificate for the PostgreSQL connection. The default `disable` is insecure.

- [ ] **Restrict the admin token**: Set `BOR_ADMIN_TOKEN` to a cryptographically random string (minimum 32 bytes). This token authorises enrollment RPCs.
  ```bash
  BOR_ADMIN_TOKEN=$(openssl rand -hex 32)
  ```

- [ ] **Configure hostnames**: Set `BOR_HOSTNAMES` to the FQDN(s) of the server so that agent TLS verification succeeds without `insecureSkipVerify`.

- [ ] **Protect secret files**: The server env file and YAML config must not be world-readable.
  ```bash
  chmod 640 /etc/bor/server.env
  chown root:bor /etc/bor/server.env
  chmod 640 /etc/bor/server.yaml
  ```

- [ ] **Restrict PKI directory**: The `/var/lib/bor/pki/` directory and all key files must be accessible only to the `bor` service user.
  ```bash
  chown -R bor:bor /var/lib/bor/
  chmod 700 /var/lib/bor/pki/
  ```

### Network

- [ ] **Firewall port 8444** so that only managed desktops can reach the agent policy stream. The admin UI on port 8443 should be reachable only from administrator workstations or via a VPN/bastion host.

- [ ] **Do not expose the database port** (default 5432) to the network. The server connects to PostgreSQL via localhost or a private network interface.

- [ ] **Reverse proxy consideration**: If placing a reverse proxy in front of port 8443, ensure TLS termination is not done at the proxy for enrollment traffic, or that mTLS client certificates are correctly forwarded. Port 8444 must not be reverse-proxied — it requires end-to-end TLS with client certificate verification.

### Ongoing Operations

- [ ] **Enable FIPS runtime enforcement** on production systems: `GODEBUG=fips140=on`.

- [ ] **Rotate the JWT secret** periodically. Rotation invalidates all active sessions and requires users to log in again.

- [ ] **Monitor certificate expiry**: Agent certificates are renewed automatically when within 30 days of expiry, but this requires the agent service to be running continuously. Set up alerting on the node list in the UI to catch stale nodes.

- [ ] **Rotate the CA** by generating a new CA cert/key, re-enrolling all agents, and retiring the old CA. With HSM, CA key rotation is a new key generation on the HSM followed by a new self-signed cert.

- [ ] **Audit log review**: The audit log (`/api/v1/audit-logs`) records all state-changing operations. Review it periodically or export it to a SIEM with the export endpoint (`/api/v1/audit-logs/export`).

- [ ] **Lock down agent data directory**: On each managed desktop, `/var/lib/bor/agent/` should be readable only by the `root` user (or the user running `bor-agent`).
  ```bash
  chmod 700 /var/lib/bor/agent/
  chmod 600 /var/lib/bor/agent/agent.key
  ```

---

## HSM Integration (PKCS#11)

Storing the CA private key in a Hardware Security Module (HSM) ensures that the key material never leaves the tamper-resistant hardware, even if the server filesystem is compromised. Bor supports any PKCS#11-compliant HSM.

### How It Works

When HSM configuration is present, the CA private key is generated and stored on the HSM. Bor retrieves a `crypto.Signer` handle from the HSM via the PKCS#11 interface. All signing operations (certificate issuance) are performed inside the HSM; the raw key bytes are never exposed to the process.

The CA certificate is still stored on disk as a regular PEM file. This is intentional — the certificate is public and must be distributed to agents during enrollment.

```
+-------------------+         PKCS#11 sign()        +------------------+
|   bor-server      |  <------------------------->  |  HSM / SoftHSM   |
|                   |                               |                  |
|  caCert  (disk)   |  CA private key lives here -> |  P-384 key slot  |
|  signer  (handle) |                               |                  |
+-------------------+                               +------------------+
```

### Prerequisites

**Build requirement** — PKCS#11 support requires CGO and must be explicitly enabled:

```bash
# One-time: add the dependency
cd server && go get github.com/ThalesIgnite/crypto11

# Build with PKCS#11 support
make server-pkcs11
```

**Runtime requirement** — a PKCS#11 shared library (`.so`) for your HSM must be installed on the server. Examples:

| HSM / Token | Library Path |
|---|---|
| SoftHSMv2 (software, testing) | `/usr/lib/softhsm/libsofthsm2.so` |
| Thales Luna Network HSM | `/usr/lib/libCryptoki2_64.so` |
| Yubico YubiHSM 2 | `/usr/lib/x86_64-linux-gnu/pkcs11/yubihsm_pkcs11.so` |
| nShield Connect (Entrust) | `/opt/nfast/toolkits/pkcs11/libcknfast.so` |
| AWS CloudHSM | `/opt/cloudhsm/lib/libcloudhsm_pkcs11.so` |

### Configuration

PKCS#11 configuration is loaded from environment variables and/or the `ca.pkcs11` section of `server.yaml`. **Always set the PIN via an environment variable** — never store it in a YAML file committed to version control.

#### Environment variables

```bash
BOR_CA_PKCS11_LIB=/usr/lib/softhsm/libsofthsm2.so
BOR_CA_PKCS11_TOKEN_LABEL=bor-ca
BOR_CA_PKCS11_KEY_LABEL=bor-ca-key
BOR_CA_PKCS11_PIN=<token-pin>          # env var only — not in YAML

# CA certificate is still kept on disk
BOR_CA_CERT_FILE=/var/lib/bor/pki/ca/ca.crt
```

#### server.yaml (non-secret fields only)

```yaml
ca:
  cert_file: "/var/lib/bor/pki/ca/ca.crt"
  pkcs11:
    lib: "/usr/lib/softhsm/libsofthsm2.so"
    token_label: "bor-ca"
    key_label: "bor-ca-key"
    # pin: set via BOR_CA_PKCS11_PIN — never put the PIN in this file
```

On first start with a new token, Bor automatically generates a new ECDSA P-384 key on the HSM and creates the CA certificate. On subsequent starts, it finds the existing key by label and loads the certificate from disk.

### Example: SoftHSMv2 (Development / Testing)

SoftHSMv2 is a software PKCS#11 token suitable for testing the HSM integration without physical hardware. Do not use it in production — the key material is stored on disk in the SoftHSM database and provides no hardware protection.

#### 1. Install SoftHSMv2

```bash
# Fedora / RHEL
sudo dnf install softhsm

# Debian / Ubuntu
sudo apt install softhsm2

# Arch Linux
sudo pacman -S softhsm
```

#### 2. Initialise a token

```bash
# Create a new token labelled "bor-ca"
# The SO (Security Officer) PIN is used for administrative operations;
# the user PIN is used by bor-server at runtime.
softhsm2-util --init-token --free \
  --label "bor-ca" \
  --so-pin "so-pin-change-me" \
  --pin "user-pin-change-me"
```

Verify the token was created:

```bash
softhsm2-util --show-slots
```

#### 3. Find the PKCS#11 library path

```bash
# The library is typically in one of these locations:
ls /usr/lib/softhsm/libsofthsm2.so
ls /usr/lib/x86_64-linux-gnu/softhsm/libsofthsm2.so
ls /usr/local/lib/softhsm/libsofthsm2.so

# Alternatively:
pkcs11-tool --module /usr/lib/softhsm/libsofthsm2.so --list-slots
```

#### 4. Build the server with PKCS#11 support

```bash
cd /path/to/regula/server
go get github.com/ThalesIgnite/crypto11
cd ..
make server-pkcs11
```

#### 5. Configure and start the server

Create `/etc/bor/server.env`:

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=bor
DB_PASSWORD=change-me
DB_NAME=bor
DB_SSLMODE=disable    # use verify-full in production

# Server
BOR_ENROLLMENT_PORT=8443
BOR_POLICY_PORT=8444
BOR_HOSTNAMES=bor.example.com

# Security
JWT_SECRET=$(openssl rand -hex 32)
BOR_ADMIN_PASSWORD=change-me-in-production

# PKCS#11 HSM — CA key lives in SoftHSM, cert on disk
BOR_CA_CERT_FILE=/var/lib/bor/pki/ca/ca.crt
BOR_CA_PKCS11_LIB=/usr/lib/softhsm/libsofthsm2.so
BOR_CA_PKCS11_TOKEN_LABEL=bor-ca
BOR_CA_PKCS11_KEY_LABEL=bor-ca-key
BOR_CA_PKCS11_PIN=user-pin-change-me
```

Protect the env file:

```bash
chmod 640 /etc/bor/server.env
chown root:bor /etc/bor/server.env
```

Start the server:

```bash
# Interactive test run
source /etc/bor/server.env && GODEBUG=fips140=on ./server/server

# Via systemd (env file loaded by EnvironmentFile= directive)
systemctl start bor-server
```

On first start, Bor will log:

```
pki: key "bor-ca-key" not found on HSM token "bor-ca" — generating new ECDSA P-384 CA key
Loading CA key from PKCS#11 HSM (token="bor-ca" key="bor-ca-key")
CA loaded from HSM; certificate at /var/lib/bor/pki/ca/ca.crt
```

On subsequent starts:

```
Loading CA key from PKCS#11 HSM (token="bor-ca" key="bor-ca-key")
CA loaded from HSM; certificate at /var/lib/bor/pki/ca/ca.crt
```

#### 6. Verify the key is on the token

```bash
pkcs11-tool \
  --module /usr/lib/softhsm/libsofthsm2.so \
  --token-label "bor-ca" \
  --login --pin "user-pin-change-me" \
  --list-objects
```

You should see an entry with label `bor-ca-key` of type `Private Key` and mechanism `EC`.

### HSM Migration from Software Key

If you have an existing deployment using a software CA key and want to migrate to an HSM:

1. **Back up** the existing CA key and cert before proceeding.
2. **Do not reuse the old CA key** on the HSM. The old key cannot be imported into most HSMs without the HSM manufacturer's key injection process, which requires special equipment.
3. **Generate a new CA** on the HSM (first start with HSM config creates it automatically).
4. **Re-enroll all agents** against the new CA. Generate a new enrollment token for each node group and run `bor-agent --token <NEW_TOKEN>` on each managed machine. The `--token` flag automatically removes old certificates before re-enrolling.
5. **Retire the old CA** once all agents are re-enrolled.

### HSM Operational Notes

- **Token PIN**: The PIN is loaded from the `BOR_CA_PKCS11_PIN` environment variable. It must be available to the `bor-server` process at startup. On systemd systems, use `EnvironmentFile=` pointing to a file with `0640` permissions owned by `root:bor`.

- **Key label uniqueness**: Use a descriptive, unique label (e.g. `bor-ca-key-2026`) so that future CA rotations create a distinct key object on the token. The label is used to find the key on startup; if two objects share a label the result is undefined.

- **Context lifetime**: The PKCS#11 session is opened at startup and kept alive for the server process lifetime. Do not remove or re-initialise the token while the server is running.

- **Hardware availability**: If the HSM is unavailable at startup (library not found, token not present, wrong PIN), the server will fail with a clear error. This is intentional — signing certificates with a software fallback after an HSM failure would silently reduce security.

- **SoftHSMv2 database location**: By default, SoftHSM stores its database in `~/.softhsm2/` or `/var/lib/softhsm/tokens/`. Ensure this path is included in your backup procedure, as losing it means losing the key.
