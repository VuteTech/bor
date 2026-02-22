-- ═══════════════════════════════════════════════
-- Permissions
-- ═══════════════════════════════════════════════
INSERT INTO permissions (resource, action) VALUES
    ('policy',     'create'),
    ('policy',     'edit'),
    ('policy',     'delete'),
    ('policy',     'release'),
    ('policy',     'view'),
    ('binding',    'create'),
    ('binding',    'toggle'),
    ('binding',    'view'),
    ('user',       'manage'),
    ('role',       'manage'),
    ('compliance', 'view'),
    ('node',       'view'),
    ('node',       'create'),
    ('node',       'edit'),
    ('node',       'delete'),
    ('node_group', 'view'),
    ('node_group', 'create'),
    ('node_group', 'edit'),
    ('node_group', 'delete'),
    ('user_group', 'view'),
    ('user_group', 'create'),
    ('user_group', 'edit'),
    ('user_group', 'delete'),
    ('audit_log',  'view'),
    ('audit_log',  'export'),
    ('settings',   'manage');

-- ═══════════════════════════════════════════════
-- Roles
-- ═══════════════════════════════════════════════
INSERT INTO roles (name, description) VALUES
    ('Super Admin',       'Global administrator with all permissions'),
    ('Org Admin',         'Organization-level administrator'),
    ('Policy Editor',     'Can create and edit policies'),
    ('Policy Reviewer',   'Can review and release policies'),
    ('Compliance Viewer', 'Can view compliance data'),
    ('Auditor',           'Read-only access to all resources');

-- ═══════════════════════════════════════════════
-- Role-Permission Assignments
-- ═══════════════════════════════════════════════

-- Super Admin: all permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.name = 'Super Admin';

-- Org Admin
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.name = 'Org Admin'
  AND (p.resource, p.action) IN (
    ('policy',     'create'), ('policy',     'edit'),   ('policy',     'delete'),
    ('policy',     'release'),('policy',     'view'),
    ('binding',    'create'), ('binding',    'toggle'), ('binding',    'view'),
    ('user',       'manage'), ('compliance', 'view'),
    ('node',       'view'),   ('node',       'create'), ('node',       'edit'),   ('node',       'delete'),
    ('node_group', 'view'),   ('node_group', 'create'), ('node_group', 'edit'),   ('node_group', 'delete'),
    ('user_group', 'view'),   ('user_group', 'create'), ('user_group', 'edit'),   ('user_group', 'delete'),
    ('audit_log',  'view'),   ('audit_log',  'export'),
    ('settings',   'manage')
  );

-- Policy Editor
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.name = 'Policy Editor'
  AND (p.resource, p.action) IN (
    ('policy', 'create'), ('policy', 'edit'), ('policy', 'view'),
    ('binding', 'view')
  );

-- Policy Reviewer
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.name = 'Policy Reviewer'
  AND (p.resource, p.action) IN (
    ('policy', 'view'), ('policy', 'release'),
    ('binding', 'view')
  );

-- Compliance Viewer
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.name = 'Compliance Viewer'
  AND (p.resource, p.action) IN (
    ('compliance', 'view'), ('policy', 'view')
  );

-- Auditor: all :view actions + audit_log:export
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.name = 'Auditor'
  AND (p.action = 'view' OR (p.resource = 'audit_log' AND p.action = 'export'));

-- ═══════════════════════════════════════════════
-- Agent Settings
-- ═══════════════════════════════════════════════
INSERT INTO agent_settings (key, value) VALUES
    ('notify_users',           'true'),
    ('notify_cooldown',        '300'),
    ('notify_message',         'Desktop policies have been updated. Please log out and log back in for all changes to take effect.'),
    ('notify_message_firefox', 'Firefox policies have been updated. Please restart Firefox for all changes to take effect.'),
    ('notify_message_chrome',  'Chrome/Chromium policies have been updated. Please restart your browser for all changes to take effect.');
