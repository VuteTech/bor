-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- Persistent per-(node, policy) compliance result.
CREATE TABLE compliance_results (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id     UUID        NOT NULL REFERENCES nodes(id)    ON DELETE CASCADE,
    policy_id   UUID        NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    status      VARCHAR(20) NOT NULL DEFAULT 'unknown',
    message     TEXT,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (node_id, policy_id)
);
CREATE INDEX ON compliance_results (policy_id);
CREATE INDEX ON compliance_results (node_id);
