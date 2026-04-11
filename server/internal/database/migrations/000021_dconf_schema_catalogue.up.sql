-- SPDX-License-Identifier: LGPL-3.0-or-later
-- Copyright (C) 2026 Vute Tech LTD

-- Deduplicated GSettings schema definitions reported by any agent.
-- source = 'builtin' for schemas seeded from the server's built-in set;
-- source = 'agent'   for schemas reported dynamically.
CREATE TABLE dconf_schemas (
    schema_id    VARCHAR(255) PRIMARY KEY,
    path         VARCHAR(255),
    relocatable  BOOLEAN      NOT NULL DEFAULT FALSE,
    keys_json    JSONB        NOT NULL,
    source       VARCHAR(10)  NOT NULL DEFAULT 'agent',
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Tracks which schemas are present on which nodes (for INAPPLICABLE reporting).
CREATE TABLE node_dconf_schemas (
    node_id   UUID         NOT NULL REFERENCES nodes(id)          ON DELETE CASCADE,
    schema_id VARCHAR(255) NOT NULL REFERENCES dconf_schemas(schema_id) ON DELETE CASCADE,
    PRIMARY KEY (node_id, schema_id)
);
CREATE INDEX ON node_dconf_schemas (schema_id);
