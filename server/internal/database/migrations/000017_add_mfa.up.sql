-- SPDX-License-Identifier: LGPL-3.0-or-later
CREATE TABLE user_mfa (
    user_id        UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    totp_secret    TEXT NOT NULL,
    totp_algorithm VARCHAR(10) NOT NULL DEFAULT 'SHA256',
    totp_enabled   BOOLEAN NOT NULL DEFAULT false,
    backup_codes   TEXT[] NOT NULL DEFAULT '{}',
    created_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW()
);

INSERT INTO agent_settings (key, value) VALUES
    ('mfa_required', 'false'),
    ('totp_algorithm', 'SHA256')
ON CONFLICT (key) DO NOTHING;
