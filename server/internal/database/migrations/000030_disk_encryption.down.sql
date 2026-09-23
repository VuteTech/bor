-- Remove the disk encryption tables and permissions. Their role_permissions
-- rows are removed automatically (ON DELETE CASCADE).
DROP TABLE IF EXISTS luks_recovery_keys;
DROP TABLE IF EXISTS tang_servers;
DROP TABLE IF EXISTS node_disk_encryption;
DROP TABLE IF EXISTS luks_volumes;
DELETE FROM permissions WHERE resource IN ('disk_encryption', 'tang_server');
