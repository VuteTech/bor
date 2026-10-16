-- SPDX-License-Identifier: AGPL-3.0-only
-- Copyright (C) 2026 Vute Tech LTD

-- Global themes (Plasma look-and-feel packages) installed on each node,
-- with display names, so the editor can offer them by name instead of
-- asking for the package id.
CREATE TABLE node_lookandfeel_packages (
    node_id     UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    package_id  VARCHAR(255) NOT NULL,
    name        VARCHAR(255) NOT NULL DEFAULT '',
    PRIMARY KEY (node_id, package_id)
);
