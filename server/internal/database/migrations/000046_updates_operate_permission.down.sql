-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE resource = 'updates' AND action = 'operate');
DELETE FROM permissions WHERE resource = 'updates' AND action = 'operate';
