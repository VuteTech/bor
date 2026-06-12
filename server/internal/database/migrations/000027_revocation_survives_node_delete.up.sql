-- Certificate revocations are checked by serial number on every agent RPC.
-- Previously revoked_certificates.node_id used ON DELETE CASCADE, so deleting a
-- node also deleted its revocation entry — leaving the (still cryptographically
-- valid) certificate able to reconnect if the node name were reused. Make the
-- revocation survive node deletion by nulling node_id instead of cascading.
ALTER TABLE revoked_certificates DROP CONSTRAINT IF EXISTS revoked_certificates_node_id_fkey;
ALTER TABLE revoked_certificates ALTER COLUMN node_id DROP NOT NULL;
ALTER TABLE revoked_certificates
    ADD CONSTRAINT revoked_certificates_node_id_fkey
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE SET NULL;
