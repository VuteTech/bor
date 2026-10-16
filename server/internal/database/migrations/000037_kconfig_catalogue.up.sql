-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- KDE settings catalogue: curated built-in entries seeded by the server
-- (source = 'builtin') and KConfigXT schema entries reported by agents
-- (source = 'kcfg'). entry_json is the protojson form of KConfigCatalogueEntry.
CREATE TABLE kconfig_catalogue_entries (
    id          VARCHAR(512) PRIMARY KEY,
    source      VARCHAR(10)  NOT NULL DEFAULT 'kcfg',
    entry_json  JSONB        NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Which catalogue entries each node reported (per-node view in the editor).
CREATE TABLE node_kconfig_entries (
    node_id   UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    entry_id  VARCHAR(512) NOT NULL REFERENCES kconfig_catalogue_entries(id) ON DELETE CASCADE,
    PRIMARY KEY (node_id, entry_id)
);
CREATE INDEX ON node_kconfig_entries (entry_id);

-- System Settings modules installed on each node, with display names.
CREATE TABLE node_kcm_modules (
    node_id    UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    module_id  VARCHAR(255) NOT NULL,
    name       VARCHAR(255) NOT NULL DEFAULT '',
    PRIMARY KEY (node_id, module_id)
);
