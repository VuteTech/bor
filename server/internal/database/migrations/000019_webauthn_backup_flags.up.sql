-- SPDX-License-Identifier: AGPL-3.0-only
-- Store the FIDO2 backup-eligibility flags recorded at registration time.
-- go-webauthn validates that these flags are consistent across every
-- authentication; without them stored, passkey managers (Bitwarden, 1Password,
-- etc.) and modern hardware keys always fail with "Backup Eligible flag
-- inconsistency detected during login validation".
ALTER TABLE user_webauthn_credentials
    ADD COLUMN backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN backup_state    BOOLEAN NOT NULL DEFAULT FALSE;
