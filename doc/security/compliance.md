# Security and compliance

This page is the security architecture and compliance reference for Bor. It
is written for auditors, security officers and procurement teams who need to
know how Bor protects data, which cryptography it uses, and how it maps to
FIPS 140-3, BSI TR-02102, SOG-IS, eIDAS, NIS2, the EU Cybersecurity Act and
GDPR. It describes the behavior of the community edition as shipped in the
distribution packages.

Everything on this page was checked against the Bor source code. Operational
procedures (protecting secrets, enforcing MFA, firewall rules, syslog
forwarding, file permissions) are in the [Hardening guide](hardening.md); this
page links to it instead of repeating them.

!!! note "Certification status"
    Bor itself holds **no** formal certification: no FIPS 140-3 module
    certificate of its own, no Common Criteria / EUCC certificate, no eIDAS
    qualified status. Bor is **designed to align with** the standards listed
    here. Where an external component holds a validation (the Go
    Cryptographic Module), this page says so and says exactly what it covers.

## Security model and trust boundaries

Bor has two components: `bor-server` (web UI, REST API, gRPC services,
internal certificate authority, PostgreSQL storage) and `bor-agent` (a root
daemon on each managed Linux endpoint). The server never connects to
endpoints; agents always connect to the server.

| Boundary | Direction | Protocol | Authentication | Notes |
|---|---|---|---|---|
| Browser -> server | Inbound to the web port (`server.enrollment_port`, default 8443; 443 recommended in production) | HTTPS (TLS 1.2+), HTTP/2 or HTTP/1.1 | Username + password (local or LDAP), optional TOTP / WebAuthn second factor; session in an HS256 JWT cookie | REST API and the static web UI. CSRF protection, security headers, rate-limited login endpoints. |
| Agent -> server, enrollment | Inbound to the web port | gRPC over TLS (agent requires TLS 1.3) | One-time enrollment token, or a Kerberos (SPNEGO) ticket | The agent sends a certificate signing request (CSR); the server returns a signed client certificate and the CA certificate. |
| Agent -> server, policy stream | Inbound to the policy port (`server.policy_port`, default 8444) | gRPC over TLS 1.3, HTTP/2 only | Mutual TLS: client certificate issued by the Bor CA, checked against the revocation list | Long-lived stream for policy delivery, compliance reports, tamper reports, certificate renewal, disk-encryption escrow. |
| Server -> PostgreSQL | Outbound | Local Unix socket (default) or TCP with libpq `sslmode` | Peer authentication (socket) or password | Default package configuration uses the local socket with `sslmode: disable`. |
| Server -> LDAP / Active Directory | Outbound | LDAPS or StartTLS (TLS 1.2+), or plain LDAP if configured | Service account bind, then user bind | See [LDAP](../configuration/ldap.md). |
| Server -> Kerberos | None at run time | - | Service keytab on disk | The server validates tickets offline with its keytab; it does not contact the KDC. |
| Server -> Tang servers | Outbound | HTTPS (TLS 1.2+) or HTTP | Advertisement signature (ES512) | Only to fetch and verify advertisements of registered Tang servers. |
| Server -> ACME CA | Outbound, plus inbound HTTP-01 / TLS-ALPN-01 challenges | HTTPS; challenge listener on `acme.http_port` | ACME account key | Only with `acme.enabled`. |
| Server -> Flatpak remotes | Outbound | HTTPS (TLS 1.2+) | - | Catalog refresh; private network destinations blocked unless `flatpak_catalog.allow_private_networks` is set. |
| Server -> syslog / SIEM | Outbound | UDP, TCP, or TCP+TLS (TLS 1.2+) | Server certificate of the receiver (TLS) | Optional real-time audit forwarding. |
| Prometheus -> server | Inbound to the metrics listener (default `127.0.0.1:9090`) | HTTP, or HTTPS with `metrics.tls_cert_file`/`tls_key_file` | Optional bearer token | Not exposed by default. |

!!! note "The web UI and enrollment share one port"
    The web UI, the REST API and the enrollment gRPC service are served by a
    single listener on `server.enrollment_port`; the server routes each
    request by its `Content-Type`. They cannot be split onto separate ports,
    and `server.address` binds both the web port and the policy port to the
    same address. Endpoints therefore need the web port for enrollment and
    for installing `bor-agent` from the server's `/agent/` repository, in
    addition to the policy port.

Key design properties:

- **Agent identity is a certificate.** After enrollment an agent never uses
  passwords or API keys. The certificate's common name is the node name,
  assigned by the server (the name requested in the CSR is discarded), and
  node names are unique.
- **The agent key never leaves the endpoint.** The agent generates its key
  pair locally and sends only a CSR.
- **Server compromise does not unlock disks at boot.** The server is not in
  the boot path; escrowed recovery keys are envelope-encrypted under a key
  encryption key (KEK) kept outside the database.
- **Least privilege on the server host.** `bor-server` runs as the
  unprivileged `bor` user under a sandboxed systemd unit; see
  [Built-in service hardening](hardening.md#built-in-service-hardening).
  `bor-agent` runs as root because it writes system policy files.

## Cryptographic inventory

### TLS listeners

| Setting | Web port (UI, REST, enrollment) | Policy port (agents only) | Agent client (both ports) |
|---|---|---|---|
| Minimum version | TLS 1.2 | TLS 1.3 | TLS 1.3 |
| Maximum version | Go default (TLS 1.3) | Go default (TLS 1.3) | Go default (TLS 1.3) |
| TLS 1.2 cipher suites | `ECDHE-ECDSA-AES256-GCM-SHA384`, `ECDHE-RSA-AES256-GCM-SHA384`, `ECDHE-ECDSA-AES128-GCM-SHA256`, `ECDHE-RSA-AES128-GCM-SHA256` | not applicable | not applicable |
| TLS 1.3 cipher suites | Go defaults: `TLS_AES_128_GCM_SHA256`, `TLS_AES_256_GCM_SHA384`, `TLS_CHACHA20_POLY1305_SHA256` (not configurable in Go) | Go defaults (same) | Go defaults (same) |
| Key exchange groups | X25519, P-256, P-384 (in this order) | Go defaults | Offers X25519, P-256, P-384 only |
| ALPN | `h2`, `http/1.1` (plus `acme-tls/1` when ACME is enabled) | `h2` | `h2` (gRPC) |
| Client certificate | `VerifyClientCertIfGiven`: verified against the Bor CA if presented; required at the gRPC layer for every enrollment-service call except `Enroll` and `KerberosEnroll` | `RequireAndVerifyClientCert` against the Bor CA | Presents its client certificate after enrollment |
| Server certificate | Internal-CA-signed, your own, or ACME | Always the internal-CA-signed server certificate | Verified against the stored Bor CA certificate after enrollment |
| Package | `app` | `app` | `policyclient` |

Because the agent offers only X25519, P-256 and P-384, the policy stream
negotiates X25519 by default and P-256 in FIPS mode.

Other TLS clients in the server (`ldap`, Tang, Flatpak catalog, audit
syslog) set a minimum of TLS 1.2 and otherwise use Go defaults.

### Certificates and PKI

| Item | Algorithm / curve | Signature | Lifetime and rotation | Package |
|---|---|---|---|---|
| Internal CA (auto-generated in `ca.autogen_dir`) | ECDSA P-384 | Self-signed, ECDSA with SHA-384 | 10 years. No automatic rotation; rotating the CA means re-enrolling every agent. | `pki` |
| Internal CA in an HSM (PKCS#11, optional build) | ECDSA P-384 generated on the token | ECDSA with SHA-384 | 10 years | `pki` (`pkcs11` build tag) |
| Server certificate (`ui.crt` in `tls.autogen_dir`) | ECDSA P-256, `digitalSignature` key usage, `serverAuth` EKU | Signed by the CA (ECDSA with SHA-384) | 1 year. Regenerated at server start when it no longer verifies against the CA (including when expired) or when required host names are missing. | `pki` |
| Agent client certificate | ECDSA P-256 key generated on the endpoint, `digitalSignature` key usage, `clientAuth` EKU | Signed by the CA (ECDSA with SHA-384) | 90 days. The agent requests renewal (new key pair, new CSR over mTLS) when it starts and the certificate expires within 30 days. | `pki`, `policyclient` |
| Certificate serial numbers | 128-bit random from `crypto/rand` | - | - | `pki` |
| CSR policy | The CA rejects RSA keys below 3072 bits and ECDSA curves other than P-256, P-384, P-521; Ed25519 is accepted; the CSR signature is verified. | - | - | `pki` |
| ACME certificate (web port only) | ECDSA P-256 requested by default; RSA 2048 for clients that do not support ECDSA (behavior of the `golang.org/x/crypto/acme/autocert` library) | Issued by the ACME CA | Renewed automatically by the library | `pki` |

**Revocation.** Bor does not publish CRLs or run OCSP. Revoked certificate
serial numbers are stored in the `revoked_certificates` table. Every gRPC
call and every new stream on both ports passes an interceptor that requires a
verified client certificate (except the two enrollment calls) and rejects the
call if the serial number is revoked. **Revoke Certificate** and
**Decommission** on the **Nodes** page write that record; see
[TLS and certificates](../configuration/tls.md).

**PKCS#11.** HSM support for the CA key is compiled only with the `pkcs11`
build tag (`make server-pkcs11`, requires CGO). The release packages are built
with the plain `make server` target, so **packaged `bor-server` binaries do
not include PKCS#11 support**. See
[Store the CA key in an HSM](../configuration/tls.md#store-the-ca-key-in-an-hsm-pkcs11).

### Web authentication and sessions

| Item | Algorithm and parameters | Lifetime | Package |
|---|---|---|---|
| Local password hashing | PBKDF2-HMAC-SHA256, 600,000 iterations, 16-byte random salt, 32-byte output, constant-time comparison. Format `$pbkdf2$sha256$<iter>$<salt>$<key>`. | - | `services` |
| Password policy | Minimum 12 characters for local accounts. No composition rules, no history, no expiry. LDAP passwords are verified by the directory and follow the directory's policy. | - | `services` |
| Access token | JWT, HS256 with `jwt_secret` (minimum 32 bytes; random value generated by the package on install) | `security.jwt_lifetime`, default 1 hour | `services` |
| Refresh token | JWT, HS256, `token_type: refresh`. Not stored in the database. | `security.refresh_lifetime`, default 24 hours | `services` |
| Server-side revocation | Per-user cut-off timestamp (`tokens_valid_after`); tokens issued before it are rejected. Set on logout, password change, administrative password reset and account disable. | - | `services` |
| Sign-in session token (between login steps) | JWT, HS256, cannot be used as an access token | 5 minutes | `services` |
| MFA-enrollment-at-sign-in token | JWT, HS256 | 10 minutes | `services` |
| Step-up token | JWT, HS256, audience `bor-step-up`, bound to one purpose, single use | 5 minutes | `services` |
| Session cookie `bor_session` | `HttpOnly`, `Secure`, `SameSite=Strict`, path `/` | Access-token lifetime | `api` |
| Refresh cookie `bor_refresh` | `HttpOnly`, `Secure`, `SameSite=Strict`, path `/api/v1/auth/refresh` | Refresh-token lifetime | `api` |
| CSRF protection | Double-submit token: 128-bit random `bor_csrf` cookie (`Secure`, `SameSite=Strict`, readable by JavaScript) must match the `X-CSRF-Token` header on every `POST`/`PUT`/`PATCH`/`DELETE` under `/api/` except the public sign-in endpoints | 24 hours | `api` |
| Brute-force protection | Sign-in, MFA and WebAuthn endpoints: 10 requests per minute per client IP address (taken from the TCP connection), HTTP 429 above that | - | `api` |
| Security headers | `Content-Security-Policy` (`default-src 'self'`, `frame-ancestors 'none'`), `Strict-Transport-Security: max-age=63072000; includeSubDomains`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy` | - | `api` |

### Multi-factor authentication

| Item | Parameters | Package |
|---|---|---|
| TOTP | RFC 6238, HMAC-SHA256 (default) or HMAC-SHA512 (**Settings -> MFA Policy**), 6 digits, 30-second period, +/-1 step tolerance, 160-bit random secret | `services` |
| TOTP secret at rest | AES-256-GCM, random 96-bit nonce per secret; key derived with HKDF-SHA256 from `BOR_MFA_SECRET` (environment only), or from `jwt_secret` when that is unset | `services` |
| Backup codes | 8 codes, each 40 bits from `crypto/rand` (shown as `XXXXX-XXXXX`), stored as bcrypt hashes (library default cost 10), single use | `services` |
| WebAuthn / FIDO2 | Enabled only when `webauthn.rpid` is set. User verification `preferred`; no attestation requested (the authenticator model is not verified); credential algorithms are the `go-webauthn` library defaults (ECDSA, RSA and EdDSA COSE algorithms); signature counter stored and checked; ceremony state held server-side for 5 minutes | `services` |
| Step-up re-authentication | Required to reveal a LUKS recovery key: password plus a TOTP code when `BOR_ESCROW_REQUIRE_MFA` is true (default) | `services` |

See [Multi-factor authentication](../configuration/mfa.md) for setup.

### Enrollment

| Item | Parameters | Package |
|---|---|---|
| Enrollment token | 256 bits from `crypto/rand`, hex-encoded; only its SHA-256 hash is stored; valid 5 minutes; consumed by the first enrollment attempt whether it succeeds or not; bound to one node group | `services` |
| Admin token for the token-creation RPC | `security.admin_token`; compared in constant time; RPC disabled when empty | `grpc` |
| Kerberos enrollment | SPNEGO / Kerberos AP-REQ validated with the server's service keytab (`gokrb5` library). Bor does not restrict encryption types: they are those of the keytab and the KDC. Create the keytab with AES encryption types only. | `services` |

See [Kerberos](../configuration/kerberos.md).

### Disk encryption escrow

| Item | Parameters | Package |
|---|---|---|
| Recovery key generation (agent) | 256 bits from `crypto/rand`, encoded in the `systemd-cryptenroll` recovery-key format | `luks` (agent) |
| Recovery keyslot | Added with `cryptsetup luksAddKey`, PBKDF2-HMAC-SHA512 with 1,000 iterations (the same as `systemd-cryptenroll` for high-entropy recovery keys) | `luks` (agent) |
| Per-record data encryption key (DEK) | 256-bit random; recovery key encrypted with AES-256-GCM; AAD `bor-escrow-v1|<volume_id>|<luks_uuid>|<escrow_id>|<kek_id>` binds the ciphertext to its volume and record | `escrow` |
| Key encryption key (KEK) | 32 random bytes in a file (`disk_encryption.escrow_kek_file` / `BOR_ESCROW_KEK_FILE`), raw or base64. The server refuses to start if the file is group- or world-readable. DEKs are wrapped with AES-256-GCM, AAD `bor-escrow-dek-v1|<kek_id>`. | `escrow` |
| KEK rotation | New KEK id (`escrow_kek_id`, default `file-1`); old KEKs stay readable via `escrow_kek_previous_files`; an hourly job re-wraps records under the current KEK in batches of 100 | `escrow`, `services` |
| HSM-held KEK | **Not implemented.** `escrow_pkcs11_key_label` is accepted but only logs a warning; escrow stays disabled unless a KEK file is set. | `app` |
| Volume encryption | Bor does not format volumes. Compliance checks default to `aes-xts-plain64` with 512-bit volume keys (AES-256-XTS) and LUKS2. | `luks` (agent) |
| TPM2 binding | Through `systemd-cryptenroll`; PCR 7 by default, PCRs 7+14 with the Secure Boot + shim profile, or a custom list (never an empty list) | `luks` (agent) |
| Tang / Clevis | Bound with the `clevis` tools on the endpoint; the server verifies Tang advertisements (ES512 signatures, RFC 7638 SHA-256 key thumbprints) | `tang`, `luks` (agent) |

Details: [Disk encryption](../policy-types/disk-encryption.md).

### Supply chain and integrity

| Item | Parameters | Where |
|---|---|---|
| Agent package repository served at `/agent/` | apt `Release` signed (clearsigned `InRelease` and detached `Release.gpg`); dnf `repomd.xml` detached signature; public key served as `repo-key.asc`. Signing uses the OpenPGP release key held as a CI secret. Repository metadata uses SHA-256 (`createrepo_c` default, `apt-ftparchive`). | `scripts/assemble-agent-repo.sh`, CI |
| Individual package files | Not signed individually; integrity comes from the signed repository metadata. The `.apk` and Arch packages in the repository are direct downloads without a signed index. | CI |

### Other cryptographic uses

| Purpose | Algorithm | Package |
|---|---|---|
| All keys, tokens, salts, nonces, serial numbers | `crypto/rand` (the operating system CSPRNG; the Go Cryptographic Module DRBG in FIPS mode) | all |
| Agent tamper detection | File watcher compares managed files with the desired content and restores them; no cryptographic signature on policy files | agent `filewatcher` |
| Audit log integrity | **No cryptographic protection** (no hash chain, no signatures). See [Audit logging](#audit-logging-for-compliance). | - |

## FIPS 140-3

### How the binaries are built

All release packages of `bor-server` and `bor-agent` are built by the
`make server` and `make agent` targets with `GOFIPS140=v1.0.0`. This tells the
Go toolchain to compile in the frozen **Go Cryptographic Module v1.0.0**
instead of the toolchain's current crypto code. The project's Makefile
references CAVP algorithm certificate **A6650** for this module. The CMVP
module validation status is maintained by NIST; check the NIST CMVP database
for the current module certificate before you rely on it in an audit.

### Validated module versus approved mode

| Term | What it means for Bor |
|---|---|
| Validated module | The cryptographic code inside the binaries is the Go Cryptographic Module v1.0.0 snapshot. This is true for every packaged binary, with or without runtime settings. It does **not** make Bor a validated module. |
| Approved mode (`GODEBUG=fips140=on`) | At startup the module runs its integrity check and self-tests. `crypto/tls` then negotiates only FIPS-approved protocol versions, cipher suites, groups and signature algorithms: X25519 and ChaCha20-Poly1305 are no longer used, so key exchange falls back to P-256 or P-384 and TLS 1.3 uses AES-GCM. Random numbers come from the module's DRBG. |
| Default (no `GODEBUG`) | The module is used, but non-approved choices such as X25519 key exchange remain enabled and preferred. |

Algorithms Bor uses that are **outside** the Go Cryptographic Module or not
FIPS-approved, and therefore not covered by the module validation even in
approved mode:

- **bcrypt** (Blowfish-based) for MFA backup codes.
- **X25519** and **ChaCha20-Poly1305** in TLS when FIPS mode is off.
- **Kerberos** ticket decryption through the `gokrb5` library, which
  implements its own Kerberos encryption types.
- **WebAuthn** credential signatures accepted from security keys (library
  defaults, including RSA without a minimum key size).
- LUKS, TPM2 and Clevis operations run by `cryptsetup`, `systemd-cryptenroll`
  and `clevis` on the endpoint: their FIPS status depends on the endpoint
  distribution.

### Enable approved mode

Set `GODEBUG=fips140=on` with a systemd drop-in on the server and on every
endpoint. The step-by-step procedure, including verification, is in
[FIPS 140-3](hardening.md#fips-140-3) in the hardening guide. In short:

```ini
# sudo systemctl edit bor-server   (and: sudo systemctl edit bor-agent)
[Service]
Environment=GODEBUG=fips140=on
```

Then `sudo systemctl restart bor-server` (or `bor-agent`). Certificates you
supply yourself (`tls.cert_file`, `ca.cert_file`), the LDAP server, the
syslog receiver and the PostgreSQL server must also use approved algorithms,
or connections to them fail in approved mode.

## Standards mapping

Column "Operator action" lists what you must configure; "Gaps" lists what
Bor does not do or only partly does.

### FIPS 140-3

| Requirement | How Bor meets it | Operator action | Gaps and limitations |
|---|---|---|---|
| Use a validated cryptographic module | All binaries embed the Go Cryptographic Module v1.0.0 (`GOFIPS140=v1.0.0`) | Verify the module's CMVP status for your audit | Bor is not itself a validated module |
| Use only approved algorithms | Generated keys (ECDSA P-256/P-384), AES-GCM, SHA-256/384, HMAC-SHA256/512, PBKDF2, HKDF are approved | Set `GODEBUG=fips140=on` on server and agents | bcrypt backup codes, Kerberos (`gokrb5`) and WebAuthn credential algorithms are outside the module |
| Approved TLS configuration | ECDHE + AES-GCM only on TLS 1.2; approved mode removes X25519 and ChaCha20 | As above | Without approved mode X25519 is preferred |
| Key management | Keys generated with `crypto/rand`; private keys written with mode `0600` | Protect `/var/lib/bor/pki`; back up the CA ([Backup and restore](../operations/backup.md)) | No HSM in packaged builds; escrow KEK only file-based |

### BSI TR-02102-1 and TR-02102-2

| Requirement | How Bor meets it | Operator action | Gaps and limitations |
|---|---|---|---|
| TLS 1.2 minimum, TLS 1.3 preferred | TLS 1.2 minimum on the web port, TLS 1.3 only on the policy port and for all agent connections | Keep browsers current | TLS 1.2 still offered on the web port for compatibility |
| Forward secrecy, AEAD only | TLS 1.2 suites are ECDHE with AES-GCM; no CBC, RC4, 3DES, static RSA | None | TLS 1.3 suite order is chosen by Go and includes ChaCha20-Poly1305, which is not in the TR-02102-2 recommendation; enable FIPS mode to exclude it |
| Approved curves | P-256 and P-384 offered | To restrict key exchange to NIST curves, enable FIPS mode | X25519 is offered first by default; check the current TR-02102-2 edition for its status in your context |
| Signature key sizes | CA ECDSA P-384, end-entity ECDSA P-256; CSRs below RSA-3072 or below P-256 rejected | Supply your own certificates with RSA >= 3072 or ECDSA P-256+ | ACME may issue RSA 2048 certificates to clients without ECDSA support |
| Hashing | SHA-256 or stronger everywhere Bor chooses the hash | None | Legacy unsalted SHA-256 backup-code hashes are still accepted for migration |
| Symmetric encryption | AES-256-GCM for TOTP secrets and escrow | None | PostgreSQL data at rest is not encrypted by Bor |

### SOG-IS Agreed Cryptographic Mechanisms

| Mechanism class | Bor | Gaps and limitations |
|---|---|---|
| Block cipher and mode | AES-128/256 in GCM (TLS, at-rest secrets, escrow) | ChaCha20-Poly1305 possible in TLS 1.3 without FIPS mode |
| Digital signatures | ECDSA on NIST P-256 / P-384 | Ed25519 accepted in CSRs; RSA 2048 possible from ACME |
| Key agreement | ECDHE on P-256 / P-384 | X25519 preferred without FIPS mode |
| Hash and MAC | SHA-256, SHA-384, SHA-512, HMAC-SHA256, HMAC-SHA512 | - |
| Password-based key derivation | PBKDF2-HMAC-SHA256, 600,000 iterations | bcrypt for backup codes is not an agreed mechanism |
| Random number generation | Operating system CSPRNG via `crypto/rand` | - |

### eIDAS (EU 910/2014)

| Requirement | How Bor relates | Gaps and limitations |
|---|---|---|
| Qualified trust services | Bor is **not** a trust service. Its internal CA is a private PKI for agent authentication only. | Certificates issued by Bor are not qualified certificates and must not be used for electronic signatures or seals. |
| Algorithms for PKI operations | CA uses ECDSA P-384 with SHA-384; end-entity keys ECDSA P-256; `digitalSignature` key usage only; random 128-bit serials | No CRL/OCSP; no certificate policy or practice statement |
| Tamper-evident audit trails | Audit log in PostgreSQL plus optional real-time syslog forwarding | No hash chain or signatures; tamper evidence depends on forwarding to a write-once SIEM |
| Time stamping | Audit timestamps from the server clock | No qualified time stamps; keep the server clock synchronized (NTP) |

### NIS2 Directive, Article 21 measures

| Art. 21(2) measure | How Bor supports it | Operator action | Gaps and limitations |
|---|---|---|---|
| (b) Incident handling | Audit log of sign-ins, failures, permission denials, changes, tamper detections and recovery-key reveals; CEF / OCSF syslog export | Forward to a SIEM ([Audit log](../user-guide/audit-log.md)) | Successful requests only for generic API changes; failed changes appear only as permission denials or not at all |
| (c) Business continuity | Documented backup and restore | Follow [Backup and restore](../operations/backup.md) | No built-in high availability or automated backups |
| (d) Supply chain security | Signed agent repositories, pinned Go module versions (`go.sum`), weekly Dependabot updates for Go, npm and GitHub Actions | Verify the repository signing key on endpoints | No SBOM, no build provenance attestation, no signatures on individual packages |
| (e) Vulnerability handling and disclosure | Private disclosure channel (see [Vulnerability reporting](#vulnerability-reporting)) | Subscribe to GitHub releases | No published CVE / advisory feed |
| (h) Cryptography and encryption | See [Cryptographic inventory](#cryptographic-inventory) | Enable FIPS mode if required; use TLS for PostgreSQL and LDAP | Database at-rest encryption is the operator's responsibility |
| (i) Access control and asset management | RBAC with granular permissions; node inventory; certificate-based agent identity; revocation | Assign least-privilege roles ([Permissions](../reference/permissions.md)) | Node-group-scoped roles are not available in the community edition |
| (j) Multi-factor authentication | TOTP and WebAuthn, global enforcement, step-up for recovery-key reveals | Turn on **Require MFA for all users** ([Hardening guide](hardening.md#enforce-multi-factor-authentication)) | MFA is off by default; `bor-server --reset-mfa` bypasses the audit log |

### EU Cybersecurity Act / EUCC

| Topic | Bor | Gaps and limitations |
|---|---|---|
| Certification | None. Bor is not EUCC or Common Criteria certified. | - |
| Secure development | Source is public; CI runs unit tests and `golangci-lint` on server and agent before packages are built; dependency updates via Dependabot | No published threat model or formal security target |
| Defense in depth | Separate ports with different TLS policies; mTLS for agents; sandboxed server unit; CSRF, CSP and rate limiting on the web UI | - |
| Least privilege | Server runs as `bor` with only `CAP_NET_BIND_SERVICE`; RBAC on the UI | Agent runs as root by necessity |
| Input validation | CSR key strength and signature checks; server-assigned certificate identity; typed policy schemas | - |

### GDPR

| Principle / right | How Bor supports it | Operator action | Gaps and limitations |
|---|---|---|---|
| Data minimization (Art. 5(1)(c)) | Optional truncation of client IP addresses in audit records to /24 (IPv4) or /48 (IPv6) | Set `BOR_AUDIT_ANONYMIZE_IPS=true` in the **environment** (the `audit.anonymize_ips` YAML key is not read) | Sign-in and logout audit events always store the full IP address; node inventory stores endpoint IP addresses; tamper events store local usernames of processes |
| Storage limitation (Art. 5(1)(e)) | Audit retention, default 365 days, purged at startup and daily; retired recovery keys kept 30 days, orphaned 90 days; compliance reports kept 1095 days, purged hourly | Set `audit.retention_days` / `BOR_AUDIT_RETENTION_DAYS` and `reports.retention_days` / `BOR_REPORT_RETENTION_DAYS` to your policy | Data forwarded to a SIEM follows the SIEM's retention; downloaded report bundles follow the downloader's retention |
| Right of access / portability (Art. 15, 20) | `GET /api/v1/users/me/export` returns the signed-in user's account, MFA status and security keys as JSON | None | Audit records about the user are not included |
| Right to erasure (Art. 17) | Deleting a user removes the account, MFA data, security keys and role assignments | Delete the user; purge or pseudonymize audit entries in the database if required | Audit entries keep the username (only the user ID link is cleared); no built-in pseudonymization of audit history |
| Transparency | Privacy policy link in the UI (`ui.privacy_policy_url`) | Publish and link your privacy notice | - |
| Security of processing (Art. 32) | TLS in transit, hashed passwords, encrypted TOTP secrets and escrowed keys | Encrypt the database volume and backups; restrict database access | PostgreSQL contents are not encrypted by Bor |

## Data protection

### What is stored where

| Data | Location | Protection at rest |
|---|---|---|
| User accounts: username, email, full name, source (local / LDAP), enabled flag | PostgreSQL `users` | Plaintext (personal data) |
| Local password hashes | `users.password_hash` | PBKDF2-HMAC-SHA256 |
| TOTP secrets | `user_mfa` | AES-256-GCM |
| MFA backup codes | `user_mfa` | bcrypt hashes |
| WebAuthn credentials (public keys, AAGUID, counters, names) | `user_webauthn_credentials`, `webauthn_sessions` | Public data, plaintext |
| Roles, groups, bindings | `roles`, `user_groups`, `user_role_bindings` and related tables | Plaintext |
| Nodes: name, FQDN, machine ID, IP address, OS, desktop, agent version, certificate serial and expiry | `nodes` | Plaintext |
| Enrollment tokens | `enrollment_tokens` | SHA-256 hash only |
| Revoked certificate serials | `revoked_certificates` | Plaintext |
| Policies and bindings | `policies`, `policy_bindings` | Plaintext; delivered to every agent in the bound groups. Do not put secrets in policies. |
| Compliance results | `compliance_results` | Plaintext |
| Audit log: time, user ID, username, action, resource, details (redacted request body), IP address | `audit_logs` | Plaintext |
| LUKS volume inventory | `luks_volumes`, `node_disk_encryption` | Plaintext |
| Escrowed LUKS recovery keys | `luks_recovery_keys` | AES-256-GCM under a per-record DEK, DEK wrapped by the KEK |
| Internal CA and server key | `/var/lib/bor/pki` | PEM files, mode `0600`, owned by `bor` (or the CA key in an HSM with a custom build) |
| ACME account key and certificates | `/var/lib/bor/acme` | Files owned by `bor` |
| Configuration secrets: `jwt_secret`, `BOR_MFA_SECRET`, database password, LDAP bind password, `admin_token`, metrics token, PKCS#11 PIN | `/etc/bor/server.yaml` or `/etc/bor/server.env` | **Plaintext**, protected by file permissions (`root:bor 0640`) |
| Escrow KEK | File named by `escrow_kek_file` | Raw key, mode `0600` enforced |
| Initial admin password | `security.admin_password` in `server.yaml`, or `/var/lib/bor/initial_admin_password` | Plaintext until you remove it ([Hardening guide](hardening.md#replace-the-initial-admin-password)) |
| Agent key, certificate, CA certificate | `/var/lib/bor/agent` on the endpoint | `agent.key` mode `0600`, root-owned |

### Data in transit

All browser, agent, LDAP (when TLS is configured), Tang (HTTPS), Flatpak and
SIEM (`tcp+tls`) traffic can be protected with TLS as described above.
Plaintext options exist and are the operator's choice: LDAP without TLS,
syslog over `udp` or `tcp`, the metrics endpoint without TLS, and PostgreSQL
over TCP with `sslmode: disable`. For PostgreSQL over TCP use
`sslmode: verify-full` and provide the server's CA with the standard libpq
`PGSSLROOTCERT` environment variable in `/etc/bor/server.env`.

### What is never logged

- **Audit records**: request bodies are stored only after every field whose
  name contains `password`, `passwd`, `secret`, `token`, `credential`,
  `passphrase`, `private_key`, `privatekey`, `api_key`, `apikey`, `jwt`,
  `auth_token`, `access_token`, `refresh_token` or `recovery_key` is replaced
  by `[REDACTED]`, recursively. Recovery-key reveals bypass request-body
  capture.
- **Enrollment tokens** are stored only as SHA-256 hashes.
- **Recovery keys** are never written to the agent's state file or logs;
  only rotation progress is persisted.

!!! warning "Initial admin password"
    On a fresh install the package prints the generated admin password to the
    console and writes it to `server.yaml`. Remove it after the first login as
    described in the [Hardening guide](hardening.md#replace-the-initial-admin-password).

## Audit logging for compliance

| Aspect | Detail |
|---|---|
| Events | Successful (`2xx`) `POST`/`PUT`/`PATCH`/`DELETE` API requests; successful and failed sign-ins, logouts, MFA enrollment and removal, step-up; permission denials (`access_denied`); role removals; agent tamper detections (`tamper_detected`); policy export and import; recovery-key escrow, reveal, refused reveal, rotation and destruction (`recoverykey.*`, `luks.*`); compliance report generation, download, deletion, retention purge and integrity failures (`report.*`, with the report's SHA-256) |
| Fields | Event time, actor (user ID and username, or node for agent events), action, resource type and ID, outcome, source IP address, details (redacted request body, tamper file path and processes with PID, command and user) |
| Storage | PostgreSQL `audit_logs` |
| Viewing and export | **Audit Logs** page (`audit_log:view`); **Export CSV** / **Export JSON** (`audit_log:export`) |
| Real-time forwarding | RFC 5424 syslog over UDP, TCP or TCP+TLS (TLS 1.2+), payload in ArcSight CEF (`CEF:0`) or OCSF JSON |
| Retention | `audit.retention_days` (default 365, `0` disables purging) |
| Integrity | None built in. Anyone with write access to the database can change or delete records, and retention purges them. For tamper evidence, forward events in real time to a SIEM or write-once store. |

Not recorded in the audit log: read-only (`GET`) requests other than those
listed, failed (non-`2xx`) change requests other than permission denials, and
`bor-server --reset-mfa` (it runs outside the server process).

Usage, filters, retention and syslog configuration:
[Audit log](../user-guide/audit-log.md).

## Supply chain

| Topic | Status |
|---|---|
| Source | Public on GitHub (https://github.com/VuteTech/Bor). Licensed AGPL-3.0-only. |
| Build | GitHub Actions builds deb, rpm, apk and Arch packages for amd64, arm64 and ppc64le with `nfpm` (pinned version) after unit tests and lint pass. Go toolchain version comes from `go.mod`; Go dependencies are pinned by `go.sum`. Builds are not bit-for-bit reproducible and no build provenance is published. |
| Package signing | The agent repository each server hosts is signed with the project's OpenPGP release key when the CI secret is available; unsigned builds are flagged in the **Deploy agent** dialog, which then falls back to unsigned repository settings. Individual package files are not signed. |
| Dependency updates | Dependabot checks Go modules (server and agent), npm (frontend) and GitHub Actions weekly. GitHub Actions are referenced by version tag, not by commit hash. |
| SBOM | Not currently published. |
| Vulnerability scanning | No dedicated scanner (such as `govulncheck`) runs in CI. |

### Vulnerability reporting

Report security issues privately by email to **bor@vute.tech**, not as public
GitHub issues. Include a description, steps to reproduce and any suggested
mitigation. The project commits to acknowledge reports within 48 hours and
to aim for a fix within 90 days, coordinating disclosure with the reporter.

## Known limitations

These are properties of the current code that you should account for in a
risk assessment.

| Area | Limitation | Mitigation |
|---|---|---|
| Agent certificate renewal | The agent checks its certificate only when the service starts. An agent that runs for more than 60 days without a restart lets its certificate expire, after which the node must be re-enrolled. | Restart agents or reboot endpoints regularly; alert on `bor_node_certificate_expiry_seconds` ([Monitoring](../operations/monitoring.md)). |
| Server certificate renewal | The internally issued server certificate (1 year) is regenerated only at server start. | Restart the server at least yearly, or use your own certificate or ACME for the web port. |
| Revocation of open streams | Revocation is checked when a call or stream starts. A policy stream that is already open is not closed when its certificate is revoked. | Restart the server or wait for the agent to reconnect. |
| Renewed certificates | On renewal the previous certificate is not revoked and stays valid until it expires (up to 30 days). Revoking a node from the UI revokes only the current serial, and a successful renewal clears the node's revocation records. | After revoking a node that renewed recently, **Decommission** it instead, which deletes the node record so its other certificates can no longer renew or stream. |
| Kerberos enrollment | The node name sent by the agent is not checked against the Kerberos principal, and the principal's realm is not compared with `kerberos.realm`. Any principal that can obtain a ticket for the service can enroll a new node into the default node group. | Restrict which principals can get service tickets; review new nodes; use token enrollment where this matters. |
| Audit source IP | The audit log takes the client IP from `X-Forwarded-For` or `X-Real-IP` when present, without checking for a trusted proxy, so a client can set it. Rate limiting uses the TCP peer address and is not affected. | Treat audit IP addresses as informational, or strip these headers at a trusted reverse proxy. |
| IP anonymization coverage | `BOR_AUDIT_ANONYMIZE_IPS` does not apply to sign-in and logout events. | Account for this in your records of processing. |
| Audit log integrity | No hash chain or signatures. | Forward events to a SIEM in real time. |
| Compliance report integrity | The community edition stores each report's SHA-256 and re-checks it on every open, but does not sign reports; anyone with write access to the database can replace a bundle and its hash together. | Forward the audit log (the `report.generate` event carries the hash) to a SIEM or write-once store; keep downloaded bundles outside the server; Bor Enterprise signs reports with a certificate of the internal CA for offline verification. |
| Backup codes | Hashed with bcrypt at cost 10 (not a FIPS-approved algorithm); 40 bits of entropy per code. | Rate limiting applies to the sign-in endpoints. |
| `BOR_MFA_SECRET` | No minimum length is enforced; when unset, `jwt_secret` is used, so changing `jwt_secret` breaks existing TOTP enrollments. | Set a dedicated 32-byte random value before users enroll. |
| WebAuthn | No attestation, user verification only preferred. | Combine with a password (always required) and enforce MFA. |
| CSRF and metrics token comparison | Compared with ordinary string comparison rather than constant-time comparison. | Low risk; keep the metrics listener on loopback. |
| Rate limiting behind a proxy | All clients behind a reverse proxy share the proxy's address for rate limiting. | Prefer direct TLS to the web port. |
| PKCS#11 | Not included in packaged binaries; escrow KEK in an HSM not implemented. | Build with `make server-pkcs11` if you need an HSM for the CA. |
| Database at rest | Bor does not encrypt PostgreSQL data or backups. | Use encrypted storage and encrypted backups. |
| MFA reset | `bor-server --reset-mfa` is not written to the audit log. | Record its use in your change log. |
