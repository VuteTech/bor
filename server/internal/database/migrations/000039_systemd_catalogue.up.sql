-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- systemd units reported by nodes for the Services & timers policy editor.
-- One row per (node, unit); info_json is the protojson form of
-- SystemdUnitInfo. The editor reads a union over nodes or one node.
CREATE TABLE node_systemd_units (
    node_id      UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    name         VARCHAR(255) NOT NULL,
    unit_type    VARCHAR(16)  NOT NULL DEFAULT '',
    info_json    JSONB        NOT NULL,
    reported_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (node_id, name)
);
CREATE INDEX ON node_systemd_units (name);
CREATE INDEX ON node_systemd_units (unit_type);
