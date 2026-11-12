-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- What each node holds of the server's certificate authority, so a CA
-- replacement can wait until the agents are ready for it.
--   trust_bundle_sha256: hex SHA-256 of the CA certificate file the agent
--     verifies the server against, as it last reported. Empty when the
--     agent has not reported it (never connected, or too old to).
--   cert_issuer_sha256: hex SHA-256 of the CA certificate that issued the
--     node's current certificate. Empty until first seen.
ALTER TABLE nodes
    ADD COLUMN trust_bundle_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN cert_issuer_sha256  VARCHAR(64) NOT NULL DEFAULT '';
