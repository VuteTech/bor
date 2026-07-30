-- Granular user/role permissions.
--
-- The Users and Roles admin screens were gated by the coarse `user:manage` /
-- `role:manage` permissions (all-or-nothing), unlike node_group/policy which are
-- already per-action. Add per-action permissions so those screens can be gated
-- like the rest (e.g. a read-only "user viewer" role).
--
-- To keep existing deployments working unchanged, grant the new granular
-- permissions to every role that currently holds the corresponding `:manage`
-- permission (Super Admin, Administrator, and any custom role).

INSERT INTO permissions (resource, action) VALUES
    ('user', 'view'),
    ('user', 'create'),
    ('user', 'edit'),
    ('user', 'delete'),
    ('role', 'view'),
    ('role', 'create'),
    ('role', 'edit'),
    ('role', 'delete')
ON CONFLICT (resource, action) DO NOTHING;

-- Grant granular user:* to every role that already has user:manage.
INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, p.id
FROM role_permissions rp
JOIN permissions m ON m.id = rp.permission_id AND m.resource = 'user' AND m.action = 'manage'
JOIN permissions p ON p.resource = 'user' AND p.action IN ('view', 'create', 'edit', 'delete')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant granular role:* to every role that already has role:manage.
INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, p.id
FROM role_permissions rp
JOIN permissions m ON m.id = rp.permission_id AND m.resource = 'role' AND m.action = 'manage'
JOIN permissions p ON p.resource = 'role' AND p.action IN ('view', 'create', 'edit', 'delete')
ON CONFLICT (role_id, permission_id) DO NOTHING;
