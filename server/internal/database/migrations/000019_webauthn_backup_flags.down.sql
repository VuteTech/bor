-- SPDX-License-Identifier: LGPL-3.0-or-later
ALTER TABLE user_webauthn_credentials
    DROP COLUMN IF EXISTS backup_eligible,
    DROP COLUMN IF EXISTS backup_state;
