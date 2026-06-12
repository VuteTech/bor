DROP INDEX IF EXISTS idx_nodes_name;
CREATE INDEX idx_nodes_name ON nodes(name);
