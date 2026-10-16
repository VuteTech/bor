-- SPDX-License-Identifier: AGPL-3.0-only
DROP TABLE IF EXISTS user_mfa;
DELETE FROM agent_settings WHERE key IN ('mfa_required', 'totp_algorithm');
