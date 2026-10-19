-- Services & timers policies can deploy scheduled tasks (a script run as
-- root on every node of a group) and raw Exec* directives. Saving, importing
-- or releasing such a policy requires systemd:exec on top of the usual
-- policy permissions. Like disk_encryption:reveal it is granted to Super
-- Admin only; every other role needs an explicit grant.
INSERT INTO permissions (resource, action) VALUES ('systemd', 'exec')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'systemd' AND p.action = 'exec'
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
