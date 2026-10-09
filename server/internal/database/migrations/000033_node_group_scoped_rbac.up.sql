-- Delegated administration: role bindings may be scoped to a node group.
--
-- A binding is either global (scope_id NULL) or scoped to exactly one node
-- group. Deleting a node group deletes the role bindings scoped to it, so a
-- removed group can never leave a dangling grant behind. Which permissions a
-- scoped binding actually grants is decided by the authorizer: only the
-- node-group-scopable resources (node, node_group, policy, binding,
-- compliance); everything else requires a global binding.

ALTER TABLE user_role_bindings DROP CONSTRAINT IF EXISTS user_role_bindings_global_only;
ALTER TABLE user_group_role_bindings DROP CONSTRAINT IF EXISTS user_group_role_bindings_global_only;

ALTER TABLE user_role_bindings
    ADD CONSTRAINT user_role_bindings_scope_valid
    CHECK ((scope_type = 'global' AND scope_id IS NULL)
        OR (scope_type = 'node_group' AND scope_id IS NOT NULL));

ALTER TABLE user_group_role_bindings
    ADD CONSTRAINT user_group_role_bindings_scope_valid
    CHECK ((scope_type = 'global' AND scope_id IS NULL)
        OR (scope_type = 'node_group' AND scope_id IS NOT NULL));

ALTER TABLE user_role_bindings
    ADD CONSTRAINT user_role_bindings_scope_node_group_fk
    FOREIGN KEY (scope_id) REFERENCES node_groups(id) ON DELETE CASCADE;

ALTER TABLE user_group_role_bindings
    ADD CONSTRAINT user_group_role_bindings_scope_node_group_fk
    FOREIGN KEY (scope_id) REFERENCES node_groups(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_user_group_role_bindings_scope
    ON user_group_role_bindings(scope_type, scope_id);

-- Policy ownership for delegated administrators. A scoped administrator's
-- draft is not bound to any node group yet, so its visibility cannot be
-- derived from bindings; the creating user owns it until it is bound.
-- created_by (the username) stays for display; ownership checks use the ID
-- so a later account with a reused username cannot inherit drafts.
ALTER TABLE policies
    ADD COLUMN created_by_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL;

-- Backfill ownership for existing policies whose creator account still exists.
UPDATE policies p
   SET created_by_user_id = u.id
  FROM users u
 WHERE u.username = p.created_by
   AND p.created_by_user_id IS NULL;
