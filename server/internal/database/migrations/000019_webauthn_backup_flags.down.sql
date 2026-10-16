-- SPDX-License-Identifier: AGPL-3.0-only
ALTER TABLE user_webauthn_credentials
    DROP COLUMN IF EXISTS backup_eligible,
    DROP COLUMN IF EXISTS backup_state;
