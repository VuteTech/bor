-- A user group can be owned by a server extension (for example SCIM
-- provisioning from an identity provider). The owner manages its name and
-- membership; Bor still manages which roles the group carries.
ALTER TABLE user_groups ADD COLUMN managed_by TEXT NULL;
