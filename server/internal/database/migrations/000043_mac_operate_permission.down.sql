DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE resource = 'mac' AND action = 'operate');
DELETE FROM permissions WHERE resource = 'mac' AND action = 'operate';
