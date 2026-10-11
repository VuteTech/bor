-- Federated users and server extensions.
--
-- A federated user signs in only through an external identity provider; it
-- has no usable password. federation_provider names the login connection
-- that owns the account.
ALTER TABLE users ADD COLUMN IF NOT EXISTS federation_provider TEXT NULL;

-- Links an identity at an external provider to a Bor user. The key is the
-- provider's stable identifier for the person (for OIDC the issuer and
-- "sub" claim, for SAML the IdP entity ID and persistent NameID), never an
-- email address.
CREATE TABLE user_external_identities (
    provider      TEXT NOT NULL,
    subject       TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ NULL,
    PRIMARY KEY (provider, subject)
);

CREATE INDEX idx_user_external_identities_user_id ON user_external_identities(user_id);

-- Migrations of server extension modules, tracked per module so their
-- numbering never collides with the core migrations.
CREATE TABLE ext_schema_migrations (
    module     TEXT NOT NULL,
    version    TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (module, version)
);
