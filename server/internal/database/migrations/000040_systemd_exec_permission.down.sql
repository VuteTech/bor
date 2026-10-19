DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE resource = 'systemd' AND action = 'exec');
DELETE FROM permissions WHERE resource = 'systemd' AND action = 'exec';
