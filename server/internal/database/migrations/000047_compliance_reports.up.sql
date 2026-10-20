-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- Compliance reports: immutable point-in-time snapshots of the fleet's
-- compliance results and security posture. The canonical JSON bundle in
-- compliance_report_bundles is the evidence (hashed, optionally signed by
-- a server extension); the other tables are copies of its rows for
-- paging, filtering and aggregation. Node and policy ids are copies, not
-- foreign keys: a report outlives the objects it describes.
CREATE TABLE compliance_reports (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    generated_at         TIMESTAMPTZ NOT NULL,
    trigger              VARCHAR(16) NOT NULL,          -- manual | scheduled | daily_summary | api
    kind                 VARCHAR(8)  NOT NULL DEFAULT 'full', -- full | summary
    generated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    generated_by         VARCHAR(255) NOT NULL DEFAULT '',
    schedule_id          UUID,
    label                VARCHAR(255) NOT NULL DEFAULT '',
    notes                TEXT NOT NULL DEFAULT '',
    scope_json           JSONB NOT NULL,
    scope_all            BOOLEAN NOT NULL DEFAULT FALSE,
    scope_group_ids      UUID[] NOT NULL DEFAULT '{}',
    node_count           INTEGER NOT NULL DEFAULT 0,
    nodes_online         INTEGER NOT NULL DEFAULT 0,
    nodes_unreachable    INTEGER NOT NULL DEFAULT 0,
    result_count         INTEGER NOT NULL DEFAULT 0,
    results_stale        INTEGER NOT NULL DEFAULT 0,
    count_compliant      INTEGER NOT NULL DEFAULT 0,
    count_non_compliant  INTEGER NOT NULL DEFAULT 0,
    count_error          INTEGER NOT NULL DEFAULT 0,
    count_inapplicable   INTEGER NOT NULL DEFAULT 0,
    count_unknown        INTEGER NOT NULL DEFAULT 0,
    score_pct            NUMERIC(5,2),
    bundle_sha256        BYTEA,
    bundle_bytes         INTEGER,
    signature_format     VARCHAR(16),
    signature_value      TEXT,
    signature_key_id     VARCHAR(128),
    pinned               BOOLEAN NOT NULL DEFAULT FALSE,
    pinned_by            VARCHAR(255),
    pinned_at            TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_compliance_reports_generated ON compliance_reports (generated_at DESC);
CREATE INDEX idx_compliance_reports_kind ON compliance_reports (kind, generated_at DESC);
CREATE INDEX idx_compliance_reports_scope_groups ON compliance_reports USING GIN (scope_group_ids);

CREATE TABLE compliance_report_bundles (
    report_id UUID PRIMARY KEY REFERENCES compliance_reports(id) ON DELETE CASCADE,
    gzip_json BYTEA NOT NULL
);

CREATE TABLE compliance_report_nodes (
    report_id           UUID NOT NULL REFERENCES compliance_reports(id) ON DELETE CASCADE,
    node_id             UUID NOT NULL,
    node_name           VARCHAR(255) NOT NULL,
    os_name             VARCHAR(100),
    os_version          VARCHAR(100),
    agent_version       VARCHAR(50),
    status              VARCHAR(20) NOT NULL,
    last_seen           TIMESTAMPTZ,
    unreachable         BOOLEAN NOT NULL DEFAULT FALSE,
    groups              TEXT[] NOT NULL DEFAULT '{}',
    lsm                 VARCHAR(16),
    lsm_mode            VARCHAR(16),
    encrypted           BOOLEAN,
    escrowed            BOOLEAN,
    pending_security    INTEGER,
    reboot_required     BOOLEAN,
    count_compliant     INTEGER NOT NULL DEFAULT 0,
    count_non_compliant INTEGER NOT NULL DEFAULT 0,
    count_error         INTEGER NOT NULL DEFAULT 0,
    count_inapplicable  INTEGER NOT NULL DEFAULT 0,
    count_unknown       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (report_id, node_id)
);

CREATE TABLE compliance_report_results (
    report_id      UUID NOT NULL REFERENCES compliance_reports(id) ON DELETE CASCADE,
    node_id        UUID NOT NULL,
    node_name      VARCHAR(255) NOT NULL,
    policy_id      UUID NOT NULL,
    policy_name    VARCHAR(255) NOT NULL,
    policy_type    VARCHAR(50) NOT NULL,
    policy_version INTEGER NOT NULL DEFAULT 1,
    status         VARCHAR(20) NOT NULL,
    message        TEXT,
    reported_at    TIMESTAMPTZ,
    stale          BOOLEAN NOT NULL DEFAULT FALSE,
    item_total     INTEGER NOT NULL DEFAULT 0,
    item_failed    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (report_id, node_id, policy_id)
);
CREATE INDEX idx_compliance_report_results_status ON compliance_report_results (report_id, status);
CREATE INDEX idx_compliance_report_results_policy ON compliance_report_results (report_id, policy_id);

CREATE TABLE compliance_report_controls (
    report_id     UUID NOT NULL REFERENCES compliance_reports(id) ON DELETE CASCADE,
    framework     VARCHAR(128) NOT NULL,
    control_id    VARCHAR(32) NOT NULL,
    title         VARCHAR(255) NOT NULL,
    pass          INTEGER NOT NULL DEFAULT 0,
    fail          INTEGER NOT NULL DEFAULT 0,
    error         INTEGER NOT NULL DEFAULT 0,
    inapplicable  INTEGER NOT NULL DEFAULT 0,
    nodes_failing INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (report_id, framework, control_id)
);
