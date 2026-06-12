-- Per-user token invalidation cut-off. Access/refresh tokens whose "issued at"
-- (iat) predates this timestamp are rejected, providing server-side revocation
-- on logout, account disable, password change, and deletion.
-- NULL means "no revocation cut-off" so existing tokens remain valid until the
-- first bump.
ALTER TABLE users ADD COLUMN IF NOT EXISTS tokens_valid_after TIMESTAMPTZ;
