-- Revert to ON DELETE CASCADE. Rows with a NULL node_id (revocations whose node
-- was deleted) must be removed first, as they cannot satisfy NOT NULL.
DELETE FROM revoked_certificates WHERE node_id IS NULL;
ALTER TABLE revoked_certificates DROP CONSTRAINT IF EXISTS revoked_certificates_node_id_fkey;
ALTER TABLE revoked_certificates ALTER COLUMN node_id SET NOT NULL;
ALTER TABLE revoked_certificates
    ADD CONSTRAINT revoked_certificates_node_id_fkey
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE;
