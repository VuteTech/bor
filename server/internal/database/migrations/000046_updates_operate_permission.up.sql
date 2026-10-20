-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- The Software updates section of the node details can ask a node to
-- refresh its metadata, run its update engine once or reboot. Installing
-- updates and rebooting change the node at once, so the actions need
-- updates:operate, granted to Super Admin only; every other role needs an
-- explicit grant.
INSERT INTO permissions (resource, action) VALUES ('updates', 'operate')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'updates' AND p.action = 'operate'
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
