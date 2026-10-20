-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- Mandatory access control (SELinux / AppArmor) inventory.

-- The active LSM and its mode, reported with every heartbeat, so the
-- nodes list can show and filter by it before any policy exists.
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS lsm VARCHAR(16);
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS lsm_mode VARCHAR(16);
CREATE INDEX IF NOT EXISTS idx_nodes_lsm ON nodes (lsm);

-- One row per node: the status block of the last inventory report and the
-- denial summary of its window. Replaced on every report.
CREATE TABLE node_mac_status (
    node_id       UUID         PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    lsm           VARCHAR(16)  NOT NULL,
    mode          VARCHAR(16)  NOT NULL DEFAULT '',
    info_json     JSONB        NOT NULL,
    denials_json  JSONB        NOT NULL DEFAULT '[]',
    denials_since TIMESTAMPTZ,
    reported_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- One row per (node, lsm, kind, name): SELinux booleans and modules,
-- AppArmor profiles. info_json is the protojson form of the item's
-- message. The editors read a union over nodes or one node.
CREATE TABLE node_mac_items (
    node_id     UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    lsm         VARCHAR(16)  NOT NULL,
    kind        VARCHAR(16)  NOT NULL,
    name        VARCHAR(255) NOT NULL,
    info_json   JSONB        NOT NULL,
    reported_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (node_id, lsm, kind, name)
);
CREATE INDEX idx_node_mac_items_lookup ON node_mac_items (lsm, kind, name);
