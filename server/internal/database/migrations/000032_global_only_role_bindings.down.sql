-- The deleted non-global rows were inert and cannot be restored.
ALTER TABLE user_role_bindings DROP CONSTRAINT IF EXISTS user_role_bindings_global_only;
ALTER TABLE user_group_role_bindings DROP CONSTRAINT IF EXISTS user_group_role_bindings_global_only;
