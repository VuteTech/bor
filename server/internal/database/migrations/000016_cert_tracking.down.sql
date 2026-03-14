-- SPDX-License-Identifier: LGPL-3.0-or-later
DROP TABLE IF EXISTS revoked_certificates;
ALTER TABLE nodes DROP COLUMN IF EXISTS cert_serial;
ALTER TABLE nodes DROP COLUMN IF EXISTS cert_not_after;
