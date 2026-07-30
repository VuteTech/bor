-- Remove the granular user/role permissions. Their role_permissions rows are
-- removed automatically (role_permissions.permission_id ON DELETE CASCADE).
DELETE FROM permissions
WHERE resource IN ('user', 'role')
  AND action IN ('view', 'create', 'edit', 'delete');
