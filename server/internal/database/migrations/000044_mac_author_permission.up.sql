-- SELinux and AppArmor policies can carry administrator-authored policy
-- text: custom modules and profiles, file contexts, login mappings,
-- permissive domains and local overrides. Such text changes what the
-- kernel enforces on every node of a group, so creating, saving, importing
-- or releasing a policy that carries it requires mac:author on top of the
-- usual policy permissions. Like systemd:exec it is granted to Super Admin
-- only; every other role needs an explicit grant.
INSERT INTO permissions (resource, action) VALUES ('mac', 'author')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'mac' AND p.action = 'author'
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
