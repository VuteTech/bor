-- The Services & timers section of the node details can start, stop and
-- restart units on a node on demand. Such an action runs on the node
-- immediately and is audited; it needs systemd:operate, a permission
-- separate from systemd:exec so that operators can be allowed to restart
-- services without being allowed to deploy scripts. Like systemd:exec it
-- is granted to Super Admin only; every other role needs an explicit grant.
INSERT INTO permissions (resource, action) VALUES ('systemd', 'operate')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'systemd' AND p.action = 'operate'
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
