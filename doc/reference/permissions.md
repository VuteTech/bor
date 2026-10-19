# Permissions

This page lists every permission Bor knows, what it allows, and which
built-in role holds it. Permissions are `resource:action` pairs. They are
granted through roles; see [Users and roles](../user-guide/users-and-roles.md)
for how roles are assigned.

In the community edition every role assignment is global, so a permission
always applies to all objects of its resource type.

## Permission list

### Policies

| Permission | Allows |
|---|---|
| `policy:view` | List and open policies, export policies, and load the reference data the policy editors use (dconf schemas, polkit actions, Flatpak catalog, repository look-ups). |
| `policy:create` | Create policies, including with the create-policy wizard, and import policy bundles. |
| `policy:edit` | Edit draft policies and change the lifecycle state of a policy: **Release**, **Unpublish**, **Archive**, **Restore to draft**. |
| `policy:delete` | Delete policies (and their disabled bindings). |
| `policy:release` | Seeded and assignable, but not currently checked by the server. Lifecycle changes are authorized with `policy:edit`. |

### Policy bindings

| Permission | Allows |
|---|---|
| `binding:view` | List policy bindings. |
| `binding:create` | Create bindings. |
| `binding:toggle` | Enable, disable and edit bindings (state and priority), and delete bindings. |

### Nodes and node groups

| Permission | Allows |
|---|---|
| `node:view` | List and open nodes, export the node list, and use the **Deploy agent** dialog. |
| `node:create` | Add nodes to node groups, request a metadata refresh (**Update metadata**), and revoke a node's certificate. |
| `node:edit` | Edit node notes. |
| `node:delete` | Decommission nodes (deletes the node and revokes its certificate) and remove nodes from node groups. |
| `node_group:view` | List and open node groups. |
| `node_group:create` | Create node groups and generate enrollment tokens. |
| `node_group:edit` | Rename node groups and change their description. |
| `node_group:delete` | Delete node groups. |

### Compliance and audit

| Permission | Allows |
|---|---|
| `compliance:view` | View the **Compliance** page. |
| `audit_log:view` | View the **Audit Logs** page. |
| `audit_log:export` | Export the audit log as CSV or JSON. |

### Users, user groups and roles

| Permission | Allows |
|---|---|
| `user:view` | List users. |
| `user:create` | Create local users. |
| `user:edit` | Edit users: profile, enable and disable, set a local user's password. |
| `user:delete` | Delete users. |
| `user:manage` | Assign roles to users and remove role assignments. |
| `user_group:view` | List user groups, their members and role assignments. |
| `user_group:create` | Create user groups, add members, and add role assignments to a group. |
| `user_group:edit` | Change a user group's name and description. |
| `user_group:delete` | Delete user groups, remove members, and remove role assignments from a group. |
| `role:view` | List roles and the permission catalog. |
| `role:create` | Create custom roles. |
| `role:edit` | Edit custom roles, including their permissions. |
| `role:delete` | Delete custom roles. |
| `role:manage` | Legacy permission kept for compatibility; superseded by the four `role:*` permissions above and not checked by any page. |

The [privilege-escalation guard](../user-guide/users-and-roles.md#privilege-escalation-guard)
applies on top of these permissions: you can only grant permissions you
hold yourself.

### System settings

| Permission | Allows |
|---|---|
| `settings:manage` | The **Agent Notifications** and **MFA Policy** settings tabs, and settings tabs added by server extensions. |
| `flatpak_repo:view` | View the **Flatpak Repositories** tab. |
| `flatpak_repo:create` | Add Flatpak repositories. |
| `flatpak_repo:edit` | Edit Flatpak repositories. |
| `flatpak_repo:delete` | Delete Flatpak repositories (built-in ones cannot be deleted). |
| `flatpak_repo:refresh` | **Refresh now** and **Upload catalog...** for a repository. |
| `tang_server:view` | View the **Tang Servers** tab. |
| `tang_server:create` | Add Tang servers. |
| `tang_server:edit` | Edit Tang servers, confirm keys, and **Check advertisement**. |
| `tang_server:delete` | Delete Tang servers. |

### Disk encryption

| Permission | Allows |
|---|---|
| `disk_encryption:view` | The **Disk Encryption** page and the disk encryption section of the node details. |
| `disk_encryption:reveal` | Reveal an escrowed LUKS recovery key. Revealing also requires re-authentication and is always audit-logged. |
| `disk_encryption:rotate` | Request rotation of a recovery key. |

### Services & timers

| Permission | Allows |
|---|---|
| `systemd:exec` | Create, save, import or release a Services & timers policy that runs commands on nodes: one with scheduled tasks or raw `Exec*` directives. Checked on top of `policy:create` / `policy:edit`; every such save and release is audit-logged (`policy.systemd_exec`) with the task names and a SHA-256 of each script. A policy that only manages existing units needs no extra permission. |
| `systemd:operate` | Start, stop or restart a unit on one node from the **Services & timers** section of the node details. The action runs on the node at once and is audit-logged (`systemd.unit_action`) with the outcome. Separate from `systemd:exec` so that operators can be allowed to restart services without being allowed to deploy scripts. Can be delegated per node group. |

## Built-in roles

`Y` means the role holds the permission on a new installation.

| Permission | Super Admin | Org Admin | Policy Editor | Policy Reviewer | Compliance Viewer | Auditor |
|---|---|---|---|---|---|---|
| `policy:view` | Y | Y | Y | Y | Y | Y |
| `policy:create` | Y | Y | Y | | | |
| `policy:edit` | Y | Y | Y | | | |
| `policy:delete` | Y | Y | | | | |
| `policy:release` | Y | Y | | Y | | |
| `binding:view` | Y | Y | Y | Y | | Y |
| `binding:create` | Y | Y | | | | |
| `binding:toggle` | Y | Y | | | | |
| `compliance:view` | Y | Y | | | Y | Y |
| `node:view` | Y | Y | | | | Y |
| `node:create` | Y | Y | | | | |
| `node:edit` | Y | Y | | | | |
| `node:delete` | Y | Y | | | | |
| `node_group:view` | Y | Y | | | | Y |
| `node_group:create` | Y | Y | | | | |
| `node_group:edit` | Y | Y | | | | |
| `node_group:delete` | Y | Y | | | | |
| `audit_log:view` | Y | Y | | | | Y |
| `audit_log:export` | Y | Y | | | | Y |
| `user:view` | Y | Y | | | | |
| `user:create` | Y | Y | | | | |
| `user:edit` | Y | Y | | | | |
| `user:delete` | Y | Y | | | | |
| `user:manage` | Y | Y | | | | |
| `user_group:view` | Y | Y | | | | Y |
| `user_group:create` | Y | Y | | | | |
| `user_group:edit` | Y | Y | | | | |
| `user_group:delete` | Y | Y | | | | |
| `role:view` | Y | | | | | |
| `role:create` | Y | | | | | |
| `role:edit` | Y | | | | | |
| `role:delete` | Y | | | | | |
| `role:manage` | Y | | | | | |
| `settings:manage` | Y | Y | | | | |
| `flatpak_repo:view` | Y | Y | | | | |
| `flatpak_repo:create` | Y | Y | | | | |
| `flatpak_repo:edit` | Y | Y | | | | |
| `flatpak_repo:delete` | Y | Y | | | | |
| `flatpak_repo:refresh` | Y | Y | | | | |
| `tang_server:view` | Y | Y | | | | |
| `tang_server:create` | Y | Y | | | | |
| `tang_server:edit` | Y | Y | | | | |
| `tang_server:delete` | Y | Y | | | | |
| `disk_encryption:view` | Y | Y | | | | Y |
| `disk_encryption:reveal` | Y | | | | | |
| `disk_encryption:rotate` | Y | | | | | |
| `systemd:exec` | Y | | | | | |
| `systemd:operate` | Y | | | | | |

Built-in roles cannot be modified or deleted. To grant a different
combination, create a custom role. Because `policy:release` is not
checked, the **Policy Editor** role can release policies (it holds
`policy:edit`) and the **Policy Reviewer** role cannot.

## Navigation and permissions

The sidebar hides **Disk Encryption** without `disk_encryption:view`,
**Audit Logs** without `audit_log:view`, and **Settings** unless you hold
at least one of `user:view`, `role:view`, `user_group:view`,
`settings:manage` or `flatpak_repo:view`. Other pages are always listed
but show an error when you lack the permission to load their data.
