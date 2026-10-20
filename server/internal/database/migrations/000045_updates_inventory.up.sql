-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- Software updates inventory: what each node reports about its update
-- engine, the updates it can install and the reboot it needs.

-- One row per node, replaced on every report. info_json is the protojson
-- of the inventory without its pending list; the filter columns repeat
-- what the nodes list and the dashboard query.
CREATE TABLE node_updates_status (
    node_id          UUID         PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    engine           VARCHAR(32)  NOT NULL DEFAULT 'none',
    effective_mode   VARCHAR(16)  NOT NULL DEFAULT '',
    managed_by       VARCHAR(16)  NOT NULL DEFAULT '',
    pending_total    INTEGER      NOT NULL DEFAULT 0,
    pending_security INTEGER      NOT NULL DEFAULT 0,
    reboot_required  BOOLEAN      NOT NULL DEFAULT FALSE,
    reboot_since     TIMESTAMPTZ,
    info_json        JSONB        NOT NULL,
    reported_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_node_updates_status_security ON node_updates_status (pending_security) WHERE pending_security > 0;
CREATE INDEX idx_node_updates_status_reboot ON node_updates_status (reboot_required) WHERE reboot_required;

-- One row per pending update of a node (at most 200 per report).
CREATE TABLE node_updates_pending (
    node_id     UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind        VARCHAR(16)  NOT NULL DEFAULT 'package',
    name        VARCHAR(255) NOT NULL,
    version     VARCHAR(255) NOT NULL DEFAULT '',
    advisory    VARCHAR(128) NOT NULL DEFAULT '',
    severity    VARCHAR(16)  NOT NULL DEFAULT '',
    security    BOOLEAN      NOT NULL DEFAULT FALSE,
    cves_json   JSONB        NOT NULL DEFAULT '[]',
    reported_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (node_id, kind, name)
);
CREATE INDEX idx_node_updates_pending_security ON node_updates_pending (node_id, security);
