-- SPDX-License-Identifier: LGPL-3.0-or-later
DROP TABLE IF EXISTS user_mfa;
DELETE FROM agent_settings WHERE key IN ('mfa_required', 'totp_algorithm');
