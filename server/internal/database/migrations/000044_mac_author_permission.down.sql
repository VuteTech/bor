DELETE FROM role_permissions
  WHERE permission_id IN (SELECT id FROM permissions WHERE resource = 'mac' AND action = 'author');
DELETE FROM permissions WHERE resource = 'mac' AND action = 'author';
