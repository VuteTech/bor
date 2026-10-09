ALTER TABLE policies DROP COLUMN IF EXISTS created_by_user_id;

DROP INDEX IF EXISTS idx_user_group_role_bindings_scope;

ALTER TABLE user_group_role_bindings DROP CONSTRAINT IF EXISTS user_group_role_bindings_scope_node_group_fk;
ALTER TABLE user_role_bindings DROP CONSTRAINT IF EXISTS user_role_bindings_scope_node_group_fk;
ALTER TABLE user_group_role_bindings DROP CONSTRAINT IF EXISTS user_group_role_bindings_scope_valid;
ALTER TABLE user_role_bindings DROP CONSTRAINT IF EXISTS user_role_bindings_scope_valid;

-- Returning to the global-only model: scoped bindings cannot be expressed
-- there, so they are removed before the global-only constraints come back.
DELETE FROM user_role_bindings WHERE scope_type <> 'global';
DELETE FROM user_group_role_bindings WHERE scope_type <> 'global';

ALTER TABLE user_role_bindings
    ADD CONSTRAINT user_role_bindings_global_only
    CHECK (scope_type = 'global' AND scope_id IS NULL);

ALTER TABLE user_group_role_bindings
    ADD CONSTRAINT user_group_role_bindings_global_only
    CHECK (scope_type = 'global' AND scope_id IS NULL);
