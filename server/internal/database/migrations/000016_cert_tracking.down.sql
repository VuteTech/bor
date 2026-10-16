-- SPDX-License-Identifier: AGPL-3.0-only
DROP TABLE IF EXISTS revoked_certificates;
ALTER TABLE nodes DROP COLUMN IF EXISTS cert_serial;
ALTER TABLE nodes DROP COLUMN IF EXISTS cert_not_after;
