-- The Mandatory access control section of the node details can ask a
-- node for a policy proposal built from its recent denials (audit2allow,
-- or AppArmor rules derived from the log). The proposal runs a tool on
-- the node and reads its audit records, so it needs mac:operate, granted
-- to Super Admin only; every other role needs an explicit grant.
INSERT INTO permissions (resource, action) VALUES ('mac', 'operate')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'mac' AND p.action = 'operate'
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
