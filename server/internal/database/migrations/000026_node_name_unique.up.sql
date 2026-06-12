-- The node name (Common Name) is the authoritative mTLS identity used to
-- authorize policy streaming, compliance reporting, and certificate renewal.
-- It must therefore be unique: a non-unique name lets one agent's certificate
-- resolve to another agent's node record. Enforce uniqueness at the database
-- level (defense in depth; the enrollment path also rejects duplicates).
--
-- NOTE: if an existing deployment already contains duplicate node names, this
-- migration will fail. Remove or rename the duplicates before upgrading.
DROP INDEX IF EXISTS idx_nodes_name;
CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
