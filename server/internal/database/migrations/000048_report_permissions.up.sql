-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- Compliance reports: report:view opens and downloads reports,
-- report:create generates them, report:manage deletes them. Viewing goes
-- to every built-in role that can view compliance; generating evidence is
-- an administrator's and an auditor's job; deleting evidence is reserved
-- for Super Admin.
INSERT INTO permissions (resource, action) VALUES
  ('report', 'view'),
  ('report', 'create'),
  ('report', 'manage')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'report' AND p.action = 'view'
  WHERE r.name IN ('Super Admin', 'Org Admin', 'Compliance Viewer', 'Auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'report' AND p.action = 'create'
  WHERE r.name IN ('Super Admin', 'Org Admin', 'Auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'report' AND p.action = 'manage'
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
