-- Polkit action catalogue reported by agents.
-- Mirrors the dconf_schemas / node_dconf_schemas pattern.

CREATE TABLE polkit_actions (
  action_id        TEXT PRIMARY KEY,
  description      TEXT,
  message          TEXT,
  vendor           TEXT,
  default_any      TEXT,
  default_inactive TEXT,
  default_active   TEXT,
  source           TEXT        NOT NULL DEFAULT 'agent',
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Which polkit actions are available on each node.
CREATE TABLE node_polkit_actions (
  node_id   UUID REFERENCES nodes(id) ON DELETE CASCADE,
  action_id TEXT REFERENCES polkit_actions(action_id) ON DELETE CASCADE,
  PRIMARY KEY (node_id, action_id)
);

CREATE INDEX node_polkit_actions_node_idx ON node_polkit_actions(node_id);
