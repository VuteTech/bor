-- Restart-safe server state: pending enrollment tokens and the PolicyHub
-- revision high-water mark move from process memory into the database.

-- Pending enrollment tokens. Only the SHA-256 hash of the token is stored;
-- the plaintext token is returned once to the caller and never persisted
-- (data minimization). Rows are deleted on consume (single use) and pruned
-- once expired.
CREATE TABLE enrollment_tokens (
    token_hash CHAR(64) PRIMARY KEY,
    node_group_id UUID NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_enrollment_tokens_expires_at ON enrollment_tokens(expires_at);

-- Single-row high-water mark for the PolicyHub revision counter. Persisting
-- it keeps revision numbers monotonic across server restarts, so a
-- reconnecting agent can never be served a delta computed against a revision
-- number that was reused for different events after a restart.
CREATE TABLE policy_hub_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    revision BIGINT NOT NULL DEFAULT 0
);

INSERT INTO policy_hub_state (id, revision) VALUES (1, 0);
