# LDAP / Active Directory Authentication

Bor supports LDAP-based authentication for the web UI, allowing users from a FreeIPA or Active Directory (Windows AD / Samba AD) directory to log in with their existing domain credentials. The local database is used as a fallback when LDAP is unavailable or disabled.

LDAP authentication is optional. When disabled, only the built-in Bor accounts (default admin, any locally created users) are available.

---

## Table of Contents

1. [Overview](#overview)
2. [Configuration reference](#configuration-reference)
3. [FreeIPA setup](#freeipa-setup)
4. [Active Directory setup](#active-directory-setup)
5. [StartTLS vs LDAPS](#starttls-vs-ldaps)
6. [Group membership](#group-membership)
7. [Role mapping](#role-mapping)
8. [User attribute mapping](#user-attribute-mapping)
9. [Testing the connection](#testing-the-connection)
10. [Troubleshooting](#troubleshooting)

---

## Overview

The authentication flow when LDAP is enabled:

1. User submits username + password via the Bor web UI login page.
2. Bor binds to the LDAP server using the service account (`bind_dn` / `bind_password`).
3. Bor searches for the user entry using `user_filter`.
4. If no entry is found and `upn_suffix` is set, Bor attempts a direct UPN bind (`username@upn_suffix`). This covers AD environments with restrictive ACLs on the service account.
5. Bor binds as the found user DN to verify the supplied password.
6. On success, group memberships are resolved (via `memberOf` attribute or a separate group search).
7. A JWT session token is issued to the browser.

---

## Configuration reference

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LDAP_ENABLED` | `false` | Set to `true` to activate LDAP authentication |
| `LDAP_HOST` | `localhost` | LDAP server hostname or IP |
| `LDAP_PORT` | `389` | LDAP server port (use `636` for LDAPS) |
| `LDAP_USE_TLS` | `false` | Use LDAPS (TLS from connection start) |
| `LDAP_START_TLS` | `false` | Upgrade plain LDAP connection to TLS with STARTTLS |
| `LDAP_TLS_CA_FILE` | _(empty)_ | Path to PEM CA certificate for server verification; empty = system pool |
| `LDAP_TLS_SKIP_VERIFY` | `false` | Disable all TLS certificate verification (dev/testing only) |
| `LDAP_BIND_DN` | _(empty)_ | Service account DN used for user search |
| `LDAP_BIND_PASSWORD` | _(empty)_ | Service account password |
| `LDAP_BASE_DN` | _(empty)_ | Base DN for user searches |
| `LDAP_USER_FILTER` | `(uid=%s)` | Search filter; `%s` is replaced with the escaped username |
| `LDAP_UPN_SUFFIX` | _(empty)_ | AD UPN suffix for fallback bind, e.g. `@EXAMPLE.COM` |
| `LDAP_ATTR_USERNAME` | `uid` | LDAP attribute for the username |
| `LDAP_ATTR_EMAIL` | `mail` | LDAP attribute for email address |
| `LDAP_ATTR_FULL_NAME` | `cn` | LDAP attribute for display name |
| `LDAP_GROUP_BASE_DN` | _(same as base_dn)_ | Base DN for group membership searches |
| `LDAP_GROUP_FILTER` | _(empty)_ | Filter to find groups a user belongs to; `%s` = user DN |
| `LDAP_GROUP_MEMBER_ATTR` | `member` | Group attribute listing member DNs |
| `LDAP_ATTR_MEMBER_OF` | `memberOf` | User attribute listing group DNs (preferred, skips group search) |
| `LDAP_PAGE_SIZE` | `500` | LDAP results paging (RFC 2696); `0` = disabled |
| `BOR_LDAP_GROUP_ROLE_MAP` | _(empty)_ | Map LDAP group CNs to Bor roles; format: `"Group CN=Role Name,Another Group=Another Role"` |

### YAML (`/etc/bor/server.yaml`)

```yaml
ldap:
  enabled: true
  host: "ldap.example.com"
  port: 636
  use_tls: true
  tls_ca_file: "/etc/ssl/certs/example-ca.crt"
  tls_skip_verify: false     # true to disable cert verification (dev only)
  bind_dn: "uid=bor-svc,cn=users,cn=accounts,dc=example,dc=com"
  bind_password: ""          # prefer LDAP_BIND_PASSWORD env var
  base_dn: "dc=example,dc=com"
  user_filter: "(uid=%s)"
  attr_username: "uid"
  attr_email: "mail"
  attr_full_name: "cn"
  attr_member_of: "memberOf"
  page_size: 500
  # Optional: map LDAP group CNs to Bor role names
  # group_role_map:
  #   "Domain Admins": "Super Admin"
  #   "IT Staff": "Org Admin"
```

> **Security note:** Never store `bind_password` in `server.yaml` if the file is world-readable. Use the `LDAP_BIND_PASSWORD` environment variable instead and restrict the `.env` file to the `bor` service account (`chmod 600`).

---

## FreeIPA setup

### 1. Create a service account

Log in to the FreeIPA server as an admin:

```bash
# Create a dedicated service account for Bor
ipa user-add bor-svc \
    --first="Bor" \
    --last="Service" \
    --password

# Optionally lock the account so it cannot be used for interactive login
ipa user-mod bor-svc --setattr=nsAccountLock=TRUE
```

### 2. Grant read access

FreeIPA allows all authenticated users to read the directory by default. The service account only needs the ability to bind and search. No additional ACL changes are typically required.

If the FreeIPA installation has custom ACLs, ensure the service account has **read** access to:

- `uid`, `mail`, `cn` attributes on user entries
- `memberOf` attribute on user entries (populated automatically by 389 DS)

### 3. Configure Bor

```yaml
# /etc/bor/server.yaml
ldap:
  enabled: true
  host: "ipa.example.com"
  port: 636
  use_tls: true
  tls_ca_file: "/etc/ipa/ca.crt"   # installed by ipa-client-install
  bind_dn: "uid=bor-svc,cn=users,cn=accounts,dc=example,dc=com"
  bind_password: ""                  # set via LDAP_BIND_PASSWORD
  base_dn: "dc=example,dc=com"
  user_filter: "(uid=%s)"
  attr_username: "uid"
  attr_email: "mail"
  attr_full_name: "cn"
  attr_member_of: "memberOf"
  page_size: 500
```

Or with environment variables:

```bash
LDAP_ENABLED=true
LDAP_HOST=ipa.example.com
LDAP_PORT=636
LDAP_USE_TLS=true
LDAP_TLS_CA_FILE=/etc/ipa/ca.crt
LDAP_BIND_DN=uid=bor-svc,cn=users,cn=accounts,dc=example,dc=com
LDAP_BIND_PASSWORD=<service-account-password>
LDAP_BASE_DN=dc=example,dc=com
LDAP_USER_FILTER=(uid=%s)
LDAP_ATTR_USERNAME=uid
LDAP_ATTR_EMAIL=mail
LDAP_ATTR_FULL_NAME=cn
LDAP_ATTR_MEMBER_OF=memberOf
```

### 4. FreeIPA default ports

| Port | Protocol | Purpose |
|------|----------|---------|
| 389  | LDAP (plain or StartTLS) | Standard LDAP |
| 636  | LDAPS | LDAP over TLS (recommended) |

---

## Active Directory setup

### 1. Create a service account

Using Active Directory Users and Computers (ADUC) or PowerShell:

```powershell
# PowerShell — create service account
New-ADUser `
    -Name "bor-svc" `
    -SamAccountName "bor-svc" `
    -UserPrincipalName "bor-svc@EXAMPLE.COM" `
    -AccountPassword (ConvertTo-SecureString "StrongPassword!" -AsPlainText -Force) `
    -PasswordNeverExpires $true `
    -CannotChangePassword $true `
    -Enabled $true

# Deny interactive logon (optional, security hardening)
# Apply "Deny log on locally" GPO to the account's OU
```

### 2. Grant read access

By default, all authenticated domain users can read the Active Directory directory. No additional permissions are required for a basic setup.

If the AD environment has custom ACLs or a hardened schema, grant the service account **Read** on:

- The user container (`CN=Users,DC=example,DC=com`) or the relevant OU
- Attributes: `sAMAccountName`, `mail`, `displayName`, `memberOf`

### 3. Configure Bor

```yaml
# /etc/bor/server.yaml
ldap:
  enabled: true
  host: "dc01.example.com"
  port: 636
  use_tls: true
  tls_ca_file: "/etc/ssl/certs/example-ca.crt"   # export from AD CS
  # tls_skip_verify: true                         # Samba AD: use if CA cert unavailable
  bind_dn: "CN=bor-svc,CN=Users,DC=example,DC=com"
  bind_password: ""                                # set via LDAP_BIND_PASSWORD
  base_dn: "DC=example,DC=com"
  user_filter: "(sAMAccountName=%s)"
  upn_suffix: "@EXAMPLE.COM"         # enables UPN fallback bind
  attr_username: "sAMAccountName"
  attr_email: "mail"
  attr_full_name: "displayName"
  attr_member_of: "memberOf"
  page_size: 1000
```

Or with environment variables:

```bash
LDAP_ENABLED=true
LDAP_HOST=dc01.example.com
LDAP_PORT=636
LDAP_USE_TLS=true
LDAP_TLS_CA_FILE=/etc/ssl/certs/example-ca.crt
LDAP_BIND_DN=CN=bor-svc,CN=Users,DC=example,DC=com
LDAP_BIND_PASSWORD=<service-account-password>
LDAP_BASE_DN=DC=example,DC=com
LDAP_USER_FILTER=(sAMAccountName=%s)
LDAP_UPN_SUFFIX=@EXAMPLE.COM
LDAP_ATTR_USERNAME=sAMAccountName
LDAP_ATTR_EMAIL=mail
LDAP_ATTR_FULL_NAME=displayName
LDAP_ATTR_MEMBER_OF=memberOf
LDAP_PAGE_SIZE=1000
```

### 4. Exporting the AD CA certificate

To verify the Active Directory domain controller's LDAPS certificate, export the CA from the AD Certificate Services (AD CS):

```bash
# On a Linux host joined to the domain (using sssd or winbind)
# The CA is usually available at:
/etc/pki/ca-trust/source/anchors/<domain>-CA.crt

# Or retrieve it via LDAP (unauthenticated):
ldapsearch -LLL -H ldap://dc01.example.com \
    -x -b "" -s base "(objectClass=*)" cACertificate \
    | grep cACertificate | awk '{print $2}' \
    | base64 -d > /etc/ssl/certs/ad-ca.der
openssl x509 -inform DER -in /etc/ssl/certs/ad-ca.der \
    -out /etc/ssl/certs/ad-ca.crt
```

#### Samba AD certificate notes

Samba AD generates self-signed TLS certificates automatically. These certificates have two quirks that affect Go-based applications:

1. **No SANs** — the hostname is in the Common Name field only. Bor handles this automatically when `tls_ca_file` is set.
2. **Negative serial numbers** — rejected by Go's x509 parser. Set `GODEBUG=x509negativeserial=1` on the Bor server process.

The Samba CA certificate is stored on the DC at `/var/lib/samba/private/tls/ca.pem`. Copy it to the Bor server and set `tls_ca_file` to its path. If the CA cert is unavailable, set `tls_skip_verify: true` (or `LDAP_TLS_SKIP_VERIFY=true`) as a workaround.

```bash
# Extract the server cert from the TLS handshake (not ideal — prefer the CA)
openssl s_client -connect dc01.example.com:636 -showcerts </dev/null 2>/dev/null \
    | openssl x509 > /etc/bor/dc-ca.crt
```

### 5. AD default ports

| Port | Protocol | Purpose |
|------|----------|---------|
| 389  | LDAP (plain or StartTLS) | Standard LDAP |
| 636  | LDAPS | LDAP over TLS (recommended) |
| 3268 | Global Catalog (plain) | Multi-domain forest searches |
| 3269 | Global Catalog (TLS) | Multi-domain forest LDAPS |

For multi-domain forests, use port 3268/3269 (Global Catalog) with `base_dn` set to the forest root DN (e.g. `DC=corp,DC=com`) so users from child domains can authenticate.

---

## StartTLS vs LDAPS

Both modes encrypt the LDAP connection; the difference is when TLS is established.

| Mode | Port | Config flag | How it works |
|------|------|-------------|--------------|
| **LDAPS** | 636 | `use_tls: true` | TLS from the first byte; faster handshake |
| **StartTLS** | 389 | `start_tls: true` | Plain connection upgraded to TLS via `STARTTLS` command |
| **Plain LDAP** | 389 | _(neither)_ | No encryption — **not recommended for production** |

When both `use_tls` and `start_tls` are `true`, `use_tls` takes precedence.

### Choosing the right mode

- **FreeIPA**: use LDAPS (port 636) with `/etc/ipa/ca.crt`. StartTLS also works.
- **Active Directory**: use LDAPS (port 636). AD CS must be installed for LDAPS to be available; without AD CS, only plain LDAP on port 389 is offered.
- **Legacy OpenLDAP**: use StartTLS (port 389) if LDAPS is not configured.

---

## Group membership

Bor resolves group membership on every LDAP login and uses it to automatically grant or revoke Bor roles (see [Role mapping](#role-mapping) below). Two resolution strategies are supported:

### Strategy 1: `memberOf` attribute (preferred)

Requires: AD, FreeIPA (≥ 4.0), or OpenLDAP with the `memberOf` overlay enabled.

When `attr_member_of` is set (default: `memberOf`), Bor reads this multi-valued attribute from the user entry. This is a single LDAP round-trip and is the most efficient approach.

```yaml
ldap:
  attr_member_of: "memberOf"  # returns group DNs
```

### Strategy 2: Separate group search

For directories that do not populate `memberOf`, Bor can search for groups directly. Set `group_filter` to a filter with `%s` as the user DN placeholder:

```yaml
ldap:
  group_base_dn: "ou=groups,dc=example,dc=com"  # restrict search scope (optional)
  group_filter: "(&(objectClass=groupOfNames)(member=%s))"
  group_member_attr: "member"
```

Common filters:

| Directory | Filter |
|-----------|--------|
| FreeIPA | `(&(objectClass=groupofnames)(member=%s))` |
| Active Directory | `(&(objectClass=group)(member=%s))` |
| OpenLDAP (groupOfNames) | `(&(objectClass=groupOfNames)(member=%s))` |
| OpenLDAP (posixGroup) | `(&(objectClass=posixGroup)(memberUid=%s))` — note: uses UID not DN |

---

## Role mapping

Bor can automatically grant and revoke roles based on LDAP group membership. The mapping is evaluated on every successful login — users gain roles when they join a mapped group and lose them when they leave.

Only roles listed in the mapping are managed automatically. Role bindings created manually in the Bor web UI are never touched.

### Configuration

**Environment variable** — comma-separated `LDAP Group CN=Bor Role Name` pairs:

```bash
BOR_LDAP_GROUP_ROLE_MAP="Domain Admins=Super Admin,IT Staff=Org Admin"
```

**YAML** (`/etc/bor/server.yaml`):

```yaml
ldap:
  group_role_map:
    "Domain Admins": "Super Admin"
    "IT Staff":      "Org Admin"
    "Auditors":      "Auditor"
```

### Available Bor roles

| Role name | Permissions |
|-----------|-------------|
| `Super Admin` | All permissions |
| `Org Admin` | All except role management |
| `Policy Editor` | Create and edit policies |
| `Policy Reviewer` | View and release policies |
| `Compliance Viewer` | View compliance data |
| `Auditor` | Read-only access + audit log export |

### How it works

1. The user authenticates successfully via LDAP.
2. Bor retrieves the user's LDAP group CNs (via `attr_member_of` or group search).
3. For each role in `group_role_map`, Bor checks whether the user is currently in the mapped group.
   - If yes and the user does not already hold the role → role binding is created.
   - If no and the user currently holds the role → role binding is deleted.
4. Role bindings for roles **not** in the map are left unchanged.

> **Note:** Group CNs are matched exactly as returned by LDAP (e.g. `"Domain Admins"`, not the full DN). The `cnFromDN` function automatically extracts the CN from a full DN, so `memberOf`-style values work without additional configuration.

---

## User attribute mapping

### FreeIPA defaults

| Bor field | LDAP attribute | Notes |
|-----------|----------------|-------|
| `attr_username` | `uid` | Login name |
| `attr_email` | `mail` | Email address |
| `attr_full_name` | `cn` | Common name / display name |
| `attr_member_of` | `memberOf` | Populated by 389 DS memberOf plugin |

### Active Directory defaults

| Bor field | LDAP attribute | Notes |
|-----------|----------------|-------|
| `attr_username` | `sAMAccountName` | Windows logon name (pre-Win2000) |
| `attr_email` | `mail` | Email address |
| `attr_full_name` | `displayName` | Full display name |
| `attr_member_of` | `memberOf` | Populated by AD automatically |

---

## Testing the connection

Use `ldapsearch` from the server running Bor to validate the configuration before enabling it in production.

### Test service account bind (FreeIPA)

```bash
ldapsearch -LLL \
    -H ldaps://ipa.example.com:636 \
    -D "uid=bor-svc,cn=users,cn=accounts,dc=example,dc=com" \
    -w "service-account-password" \
    -b "dc=example,dc=com" \
    "(uid=alice)" uid mail cn memberOf
```

### Test service account bind (Active Directory)

```bash
ldapsearch -LLL \
    -H ldaps://dc01.example.com:636 \
    -D "CN=bor-svc,CN=Users,DC=example,DC=com" \
    -w "service-account-password" \
    -b "DC=example,DC=com" \
    "(sAMAccountName=alice)" sAMAccountName mail displayName memberOf
```

### Test with a specific user

Replace the filter with the user you want to test. If `ldapsearch` returns the entry, Bor will be able to find and authenticate that user.

### Verify LDAPS certificate

```bash
openssl s_client -connect dc01.example.com:636 -showcerts </dev/null 2>&1 \
    | openssl x509 -noout -subject -issuer -dates
```

---

## Troubleshooting

### Login fails with "user not found"

1. Verify the service account bind succeeds: run the `ldapsearch` command above manually.
2. Check that `user_filter` matches the username format used at login. For AD, ensure `(sAMAccountName=%s)` is set, not `(uid=%s)`.
3. Verify `base_dn` covers the OU where the user lives.
4. For AD with strict ACLs, set `upn_suffix` (e.g. `@EXAMPLE.COM`) to enable the UPN fallback bind.

### "Invalid LDAP credentials" despite correct password

1. The user exists in LDAP but the bind as that user failed.
2. Check that the user's account is not locked or expired in the directory.
3. For AD: ensure the password meets the domain password policy and has not expired.
4. Verify the user DN is constructed correctly — use `ldapsearch` to confirm `entry.DN`.

### Certificate verification fails (LDAPS / StartTLS)

```
x509: certificate signed by unknown authority
```

1. Export the CA certificate from the directory server and set `tls_ca_file` to its path.
2. Ensure the CA cert file is PEM-encoded (starts with `-----BEGIN CERTIFICATE-----`).
3. Verify the certificate chain: `openssl verify -CAfile /path/to/ca.crt /path/to/server.crt`

### "certificate relies on legacy Common Name field, use SANs instead"

Samba AD auto-generated TLS certificates use only the CN field for the hostname instead of Subject Alternative Names (SANs). Bor handles this automatically when a custom CA file is provided — it verifies the certificate chain against the trusted CA and falls back to CN-based hostname matching.

If this error persists (e.g. when using the system cert pool without `tls_ca_file`), set `LDAP_TLS_SKIP_VERIFY=true` as a workaround, or replace the Samba-generated certificate with one that includes proper SANs.

### "x509: negative serial number"

Samba AD auto-generated certificates may use negative serial numbers, which Go rejects. Set the `GODEBUG=x509negativeserial=1` environment variable on the Bor server process to allow these certificates. This is set automatically in the development `podman-compose.yml`.

Alternatively, replace the Samba-generated certificate with one that uses a positive serial number, or use `LDAP_TLS_SKIP_VERIFY=true`.

### "strongerAuthRequired" (LDAP Result Code 8)

Samba AD rejects plain-text LDAP binds on port 389. Switch to LDAPS:

```bash
LDAP_PORT=636
LDAP_USE_TLS=true
```

### "no valid certificates found in <path>"

The CA certificate file cannot be parsed. Common causes:

1. The file contains the server/host certificate instead of the CA certificate. Check with `openssl x509 -in <path> -noout -text | grep "CA:"` — it should show `CA:TRUE`.
2. The certificate has a negative serial number (see above).
3. The file is not PEM-encoded or is corrupted.

### Connection times out

1. Confirm the LDAP port is open from the Bor server: `nc -zv ldap.example.com 636`
2. Check firewall rules between the Bor server and the LDAP/DC host.
3. For AD, port 636 requires AD CS to be installed and the DC to have an LDAPS certificate.

### "size limit exceeded" errors

Increase `page_size` or, for AD, ensure paging is enabled (it is by default). If the issue persists, narrow `base_dn` to a specific OU to reduce result sets.

### High latency on every login

1. Enable `attr_member_of` to resolve group membership in a single round-trip instead of a separate group search.
2. Ensure DNS resolves the LDAP hostname quickly — use an IP address in `host` if DNS is unreliable.
3. Tune `page_size` (default 500) to match the directory's max page size setting.
