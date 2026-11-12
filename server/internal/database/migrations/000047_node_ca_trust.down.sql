-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

ALTER TABLE nodes
    DROP COLUMN trust_bundle_sha256,
    DROP COLUMN cert_issuer_sha256;
