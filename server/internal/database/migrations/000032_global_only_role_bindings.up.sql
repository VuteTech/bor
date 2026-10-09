-- RBAC is global-only: scoped role bindings were never enforced (the HTTP
-- middleware always checked the global scope), so any non-global rows are
-- inert. Delete them now so they can never silently become live grants if
-- scoped enforcement is introduced later, and constrain both tables until a
-- real delegated-administration model ships.

DELETE FROM user_role_bindings WHERE scope_type <> 'global';
DELETE FROM user_group_role_bindings WHERE scope_type <> 'global';

-- A global binding never had a meaningful scope target; clear strays so the
-- CHECK below cannot fail on existing data.
UPDATE user_role_bindings SET scope_id = NULL WHERE scope_id IS NOT NULL;
UPDATE user_group_role_bindings SET scope_id = NULL WHERE scope_id IS NOT NULL;

ALTER TABLE user_role_bindings
    ADD CONSTRAINT user_role_bindings_global_only
    CHECK (scope_type = 'global' AND scope_id IS NULL);

ALTER TABLE user_group_role_bindings
    ADD CONSTRAINT user_group_role_bindings_global_only
    CHECK (scope_type = 'global' AND scope_id IS NULL);
