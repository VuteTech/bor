# LDAP and Active Directory

Bor can authenticate web UI users against a directory: FreeIPA, Active
Directory, Samba AD or a generic LDAP server. This page explains how
directory sign-in works, how users are created in Bor on their first
login, how directory groups grant Bor roles, how to configure the TLS
connection, and how to troubleshoot problems.

Directory sign-in applies to the web UI only. Agents never use LDAP; for
domain-joined endpoints see [Kerberos enrollment](kerberos.md).

## How directory sign-in works

The sign-in page asks for the user name first. What happens next depends
on whether Bor already knows the user:

| Account in Bor | What Bor does with the password |
|----------------|---------------------------------|
| None yet, LDAP enabled | Authenticates against LDAP. On success, creates the user in Bor (source `ldap`). |
| LDAP account (created on an earlier login) | Authenticates against LDAP, then updates the e-mail address and full name from the directory. |
| Local account | Checks the local password only. LDAP is never consulted, even if a directory user has the same name. |

For an LDAP authentication Bor:

1. Connects to `host:port` (LDAPS, StartTLS, or plain).
2. Binds as the service account (`bind_dn`, `bind_password`).
3. Searches `base_dn` (whole subtree) with `user_filter`, where `%s` is
   replaced by the escaped user name.
4. If nothing is found and `upn_suffix` is set, binds directly as
   `<username><upn_suffix>` with the user's password, then searches again
   as that user. This helps in Active Directory forests where the service
   account cannot see every user.
5. Binds as the found user DN with the supplied password. A failed bind
   means a wrong password.
6. Reads the user's groups (see [Group membership](#group-membership)).
7. Synchronizes roles from `group_role_map`, then signs the user in.

Bor stores no LDAP password. A user who is disabled in Bor (**Settings ->
Users**) cannot sign in even if the directory accepts the password. LDAP
users cannot change their password in Bor; the **Change Password** tab of
**Account security** is shown only to local users.

!!! note "Second factor"
    The [MFA policy](mfa.md) applies to LDAP users too. On the very first
    login the user does not exist in Bor yet, so only the password is
    asked; if MFA is required, the user is then asked to set up a second
    factor before getting a session.

## Configure LDAP

Add an `ldap` section to `/etc/bor/server.yaml`, keep the service account
password in `/etc/bor/server.env`, and restart the server.

### Settings

| YAML key | Environment variable | Default | Description |
|----------|----------------------|---------|-------------|
| `enabled` | `LDAP_ENABLED` | `false` | Turn on LDAP sign-in. |
| `host` | `LDAP_HOST` | `localhost` | Directory server host name. Must match its TLS certificate. |
| `port` | `LDAP_PORT` | `389` | `636` for LDAPS, `389` for StartTLS or plain, `3269`/`3268` for the AD global catalog. |
| `use_tls` | `LDAP_USE_TLS` | `false` | LDAPS. Wins over `start_tls` when both are set. |
| `start_tls` | `LDAP_START_TLS` | `false` | StartTLS on a plain connection. |
| `tls_ca_file` | `LDAP_TLS_CA_FILE` | `""` | PEM file with the CA that issued the directory server certificate. Empty uses the system trust store. |
| `tls_skip_verify` | `LDAP_TLS_SKIP_VERIFY` | `false` | Disable certificate verification. Testing only; the server refuses to start with it unless the environment variable `BOR_ALLOW_INSECURE_LDAP=true` is also set. |
| `bind_dn` | `LDAP_BIND_DN` | `""` | Service account DN. |
| `bind_password` | `LDAP_BIND_PASSWORD` | `""` | Service account password. |
| `base_dn` | `LDAP_BASE_DN` | `""` | Subtree searched for users. |
| `user_filter` | `LDAP_USER_FILTER` | `(uid=%s)` | User search filter. |
| `upn_suffix` | `LDAP_UPN_SUFFIX` | `""` | UPN suffix for the fallback bind, for example `@EXAMPLE.COM`. |
| `attr_username` | `LDAP_ATTR_USERNAME` | `uid` | Login name attribute. |
| `attr_email` | `LDAP_ATTR_EMAIL` | `mail` | E-mail attribute. |
| `attr_full_name` | `LDAP_ATTR_FULL_NAME` | `cn` | Display name attribute. |
| `attr_member_of` | `LDAP_ATTR_MEMBER_OF` | `memberOf` | User attribute listing group DNs. |
| `group_base_dn` | `LDAP_GROUP_BASE_DN` | `""` | Subtree for the group search; empty uses `base_dn`. |
| `group_filter` | `LDAP_GROUP_FILTER` | `""` | Group search filter, `%s` is the user DN. |
| `group_member_attr` | `LDAP_GROUP_MEMBER_ATTR` | `member` | Member attribute of group objects. |
| `page_size` | `LDAP_PAGE_SIZE` | `500` | Paged search size; `0` disables paging. |
| `group_role_map` | `BOR_LDAP_GROUP_ROLE_MAP` | none | Group-to-role mapping, see [Map groups to roles](#map-groups-to-roles). |

### Example: FreeIPA

1. Create a service account on the FreeIPA server. A system account under
   `cn=sysaccounts` is a good choice because it cannot log in to hosts:

    ```bash
    kinit admin
    ldapmodify -Y GSSAPI <<'EOF'
    dn: uid=bor-svc,cn=sysaccounts,cn=etc,dc=example,dc=com
    changetype: add
    objectclass: account
    objectclass: simplesecurityobject
    uid: bor-svc
    userPassword: change-me
    passwordExpirationTime: 20380119031407Z
    nsIdleTimeout: 0
    EOF
    ```

    FreeIPA lets authenticated accounts read `uid`, `mail`, `cn` and
    `memberOf` by default; no extra permission is needed.

2. Copy the IPA CA certificate to the Bor server. On an IPA-enrolled host
   it is `/etc/ipa/ca.crt`; otherwise download it:

    ```bash
    sudo curl -fsS -o /etc/bor/ipa-ca.crt http://ipa.example.com/ipa/config/ca.crt
    ```

3. Configure Bor:

    ```yaml
    ldap:
      enabled: true
      host: "ipa.example.com"
      port: 636
      use_tls: true
      tls_ca_file: "/etc/bor/ipa-ca.crt"
      bind_dn: "uid=bor-svc,cn=sysaccounts,cn=etc,dc=example,dc=com"
      base_dn: "cn=users,cn=accounts,dc=example,dc=com"
      user_filter: "(uid=%s)"
      attr_username: "uid"
      attr_email: "mail"
      attr_full_name: "cn"
      attr_member_of: "memberOf"
      group_role_map:
        "bor-admins": "Super Admin"
        "bor-editors": "Policy Editor"
    ```

    ```bash
    # /etc/bor/server.env
    LDAP_BIND_PASSWORD=change-me
    ```

### Example: Active Directory

1. Create a service account, for example `bor-svc` in `CN=Users`, with a
   long random password that does not expire. Authenticated users can read
   `sAMAccountName`, `mail`, `displayName` and `memberOf` by default.

2. LDAPS on a domain controller needs a certificate on the DC (normally
   issued by AD Certificate Services). Export the issuing CA certificate in
   Base-64 (PEM) format and copy it to the Bor server as
   `/etc/bor/ad-ca.crt`.

3. Configure Bor:

    ```yaml
    ldap:
      enabled: true
      host: "dc01.example.com"
      port: 636
      use_tls: true
      tls_ca_file: "/etc/bor/ad-ca.crt"
      bind_dn: "CN=bor-svc,CN=Users,DC=example,DC=com"
      base_dn: "DC=example,DC=com"
      user_filter: "(sAMAccountName=%s)"
      upn_suffix: "@example.com"
      attr_username: "sAMAccountName"
      attr_email: "mail"
      attr_full_name: "displayName"
      attr_member_of: "memberOf"
      page_size: 1000
      group_role_map:
        "Bor Admins": "Super Admin"
        "Desktop Team": "Policy Editor"
    ```

    ```bash
    # /etc/bor/server.env
    LDAP_BIND_PASSWORD=change-me
    ```

For a multi-domain forest, point `host` at a global catalog server, use
port `3269` with `use_tls: true`, and set `base_dn` to the forest root.

### Apply and verify

```bash
sudo chown root:bor /etc/bor/ipa-ca.crt   # or ad-ca.crt
sudo chmod 0644 /etc/bor/ipa-ca.crt
sudo systemctl restart bor-server
sudo journalctl -u bor-server -b | grep -i ldap
```

At startup the server logs `LDAP authentication enabled` with the host,
port and TLS mode, followed by one line per entry of `group_role_map`. Then
sign in to the web UI with a directory account. The user appears under
**Settings -> Users**.

## TLS options

| Mode | Settings | Notes |
|------|----------|-------|
| LDAPS | `port: 636`, `use_tls: true` | Recommended. |
| StartTLS | `port: 389`, `start_tls: true` | Plain connection upgraded before the bind. |
| Plain | neither | Passwords cross the network unencrypted. Use only on a trusted loopback or for tests. |

The connection uses TLS 1.2 or newer and checks the certificate against
`host`:

- With `tls_ca_file`, the certificate chain must lead to that CA. If the
  server certificate has no Subject Alternative Names at all (typical for
  the self-generated certificate of Samba AD), Bor accepts a Common Name
  that equals `host`.
- Without `tls_ca_file`, the system trust store and standard host name
  checks apply.
- `tls_skip_verify: true` disables all checks. Anyone on the network path
  could then capture user passwords; do not use it in production. As a
  safeguard the server stops at startup with `LDAP_TLS_SKIP_VERIFY=true
  requires BOR_DEV_MODE=true or BOR_ALLOW_INSECURE_LDAP=true` unless you
  also put `BOR_ALLOW_INSECURE_LDAP=true` in `/etc/bor/server.env`.

## Group membership

Bor needs a user's groups only for `group_role_map`. It reads them in one
of two ways:

1. **`memberOf` attribute** (default). With `attr_member_of: "memberOf"`
   Bor reads the attribute from the user entry. Active Directory and
   FreeIPA provide it; OpenLDAP needs the `memberof` overlay.
2. **Group search.** For directories without `memberOf`, set
   `attr_member_of` to an empty string and configure `group_filter`.
   `%s` is replaced by the user's DN:

    ```yaml
    ldap:
      attr_member_of: ""
      group_base_dn: "ou=groups,dc=example,dc=com"
      group_filter: "(&(objectClass=groupOfNames)(member=%s))"
    ```

    The `cn` of every matching group is used.

!!! warning
    `group_filter` is ignored while `attr_member_of` is non-empty, and the
    default is `memberOf`. Set `attr_member_of: ""` in `server.yaml`
    explicitly; an empty `LDAP_ATTR_MEMBER_OF` environment variable does not
    override the YAML value or the default.

Group names are compared by their common name (CN): a `memberOf` value of
`CN=Desktop Team,OU=Groups,DC=example,DC=com` becomes `Desktop Team`. The
comparison is exact and case-sensitive. Nested group membership is not
expanded by Bor; use the directory's own nesting support in `memberOf` if
you need it (FreeIPA includes indirect memberships, Active Directory does
not).

## Map groups to roles

`group_role_map` grants Bor roles from directory groups. Keys are group
CNs, values are Bor role names as shown under **Settings -> Roles**: the
built-in roles are `Super Admin`, `Org Admin`, `Policy Editor`,
`Policy Reviewer`, `Compliance Viewer` and `Auditor`, and custom roles work
the same way.

```yaml
ldap:
  group_role_map:
    "Bor Admins": "Super Admin"
    "Desktop Team": "Policy Editor"
    "Security Office": "Auditor"
```

As an environment variable (replaces the whole YAML map):

```bash
BOR_LDAP_GROUP_ROLE_MAP="Bor Admins=Super Admin,Desktop Team=Policy Editor"
```

The map is applied at **every LDAP sign-in**:

- A user in a mapped group who lacks the role gets it.
- A user who holds a mapped role but is no longer in any group mapped to
  it loses it.
- Roles that do not appear in the map, for example roles an administrator
  assigned by hand in **Settings -> Users**, are never touched.
- A role name that does not exist in Bor is skipped and logged.

Changes in the directory take effect at the user's next sign-in, not
immediately. To remove access at once, disable the user in **Settings ->
Users**.

!!! note "Node-group-scoped mappings"
    The syntax `"Group": "Role@Node Group"` grants a role for one node group
    only. Node-group-scoped roles are not enabled in the community edition;
    such entries create no role binding there.

## Test with ldapsearch

Run the same search Bor runs, from the Bor server, with the `openldap`
client tools:

```bash
LDAPTLS_CACERT=/etc/bor/ad-ca.crt ldapsearch -LLL -x \
  -H ldaps://dc01.example.com:636 \
  -D "CN=bor-svc,CN=Users,DC=example,DC=com" -W \
  -b "DC=example,DC=com" \
  "(sAMAccountName=alice)" sAMAccountName mail displayName memberOf
```

If this returns the user with the expected attributes and groups, Bor can
use the same settings.

## Troubleshooting

The server logs the reason for every failed LDAP sign-in; the browser only
shows `invalid username or password`.

```bash
sudo journalctl -u bor-server | grep "LDAP authentication failed"
```

| Log message | Cause and fix |
|-------------|---------------|
| `failed to connect to LDAP` | Host or port unreachable, or TLS handshake failed. Test with `nc -zv dc01.example.com 636`. |
| `failed to bind with service account` | Wrong `bind_dn` or password. Check `LDAP_BIND_PASSWORD` in `server.env`. Code 8 `strongerAuthRequired` means the server refuses binds without TLS: use LDAPS or StartTLS. |
| `user not found in LDAP` | `base_dn` does not contain the user or `user_filter` uses the wrong attribute (`uid` vs `sAMAccountName`). |
| `invalid LDAP credentials` | The user's password is wrong, or the account is locked, expired or must change its password. |
| `user not found and UPN bind failed` | The UPN fallback bind failed. Check `upn_suffix` and the password. |
| `certificate chain verification failed` | `tls_ca_file` is not the CA that issued the server certificate. Inspect with `openssl s_client -connect dc01.example.com:636 -showcerts`. |
| `certificate CN "..." does not match host "..."` | `host` must be the name in the certificate. |
| `no valid certificates found in ...` | `tls_ca_file` is not a PEM certificate. |
| `negative serial number` | Old Samba AD certificates. Set `GODEBUG=x509negativeserial=1` in a systemd drop-in (see [Server configuration](server.md#systemd-options-that-are-not-configuration-keys)) or replace the certificate. |
| `StartTLS failed` | The server does not offer StartTLS on that port. |

Other symptoms:

- **Roles are not assigned.** Check that the startup log lists your
  mappings (`LDAP group "..." ... Bor role "..."`), compare the group CN letter by letter with the
  `memberOf` output of `ldapsearch`, and look for `syncLDAPRoles: role ...
  not found` in the log.
- **A directory user cannot sign in although `ldapsearch` works.** A local
  Bor user with the same name exists. Local accounts always take
  precedence; rename or delete the local account.
- **Sign-in is slow.** Use `attr_member_of` instead of a group search, and
  make sure the directory host name resolves quickly.
