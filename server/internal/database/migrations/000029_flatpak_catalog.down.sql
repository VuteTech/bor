-- Remove the Flatpak catalog tables and the flatpak_repo permissions. Their
-- role_permissions rows are removed automatically (ON DELETE CASCADE).
DROP TABLE IF EXISTS flatpak_catalog_icons;
DROP TABLE IF EXISTS flatpak_catalog_apps;
DROP TABLE IF EXISTS flatpak_repositories;
DELETE FROM permissions WHERE resource = 'flatpak_repo';
