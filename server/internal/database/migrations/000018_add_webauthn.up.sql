-- SPDX-License-Identifier: LGPL-3.0-or-later
CREATE TABLE user_webauthn_credentials (
    id            UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT    NOT NULL UNIQUE,   -- base64url-encoded WebAuthn credential ID
    public_key    BYTEA   NOT NULL,           -- COSE-encoded public key bytes
    aaguid        TEXT    NOT NULL DEFAULT '',
    sign_count    BIGINT  NOT NULL DEFAULT 0,
    name          TEXT    NOT NULL DEFAULT 'Security Key',
    transports    TEXT[]  NOT NULL DEFAULT '{}',
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMP
);
CREATE INDEX idx_webauthn_creds_user_id ON user_webauthn_credentials(user_id);

CREATE TABLE webauthn_sessions (
    id           UUID      PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_type TEXT      NOT NULL,   -- 'registration' or 'authentication'
    session_data TEXT      NOT NULL,   -- JSON from go-webauthn SessionData
    expires_at   TIMESTAMP NOT NULL DEFAULT NOW() + INTERVAL '5 minutes'
);
CREATE INDEX idx_webauthn_sessions_user_id ON webauthn_sessions(user_id);
