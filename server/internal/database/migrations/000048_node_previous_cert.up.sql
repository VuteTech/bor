-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- The certificate a node held before its latest renewal. It stays valid
-- until the node first connects with the renewed certificate, so a node
-- that lost the renewal response can still retry; it is then revoked.
-- Empty when there is no such certificate.
ALTER TABLE nodes
    ADD COLUMN previous_cert_serial TEXT NOT NULL DEFAULT '';
