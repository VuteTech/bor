-- SPDX-License-Identifier: LGPL-3.0-or-later
-- Add certificate tracking fields to nodes
ALTER TABLE nodes
    ADD COLUMN cert_serial TEXT,
    ADD COLUMN cert_not_after TIMESTAMP;

CREATE INDEX idx_nodes_cert_not_after ON nodes(cert_not_after);

-- Revoked certificates (serial-number blocklist)
CREATE TABLE revoked_certificates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    serial TEXT NOT NULL,
    revoked_at TIMESTAMP NOT NULL DEFAULT NOW(),
    reason TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_revoked_certs_serial ON revoked_certificates(serial);
CREATE INDEX idx_revoked_certs_node_id ON revoked_certificates(node_id);
