DROP TABLE IF EXISTS ext_schema_migrations;
DROP TABLE IF EXISTS user_external_identities;
ALTER TABLE users DROP COLUMN IF EXISTS federation_provider;
