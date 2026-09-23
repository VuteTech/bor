-- Disk encryption (LUKS2) policy type: volume inventory, escrowed recovery
-- keys and the external Tang server registry.
-- See docs/disk-encryption.md.

-- Per-node LUKS volume inventory; also the anchor for escrowed keys.
-- node_id is ON DELETE SET NULL: decommissioned disks may still need
-- recovery, so keys outlive node deletion (unlike AD/BitLocker).
CREATE TABLE luks_volumes (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id               UUID REFERENCES nodes(id) ON DELETE SET NULL,
  node_name             TEXT NOT NULL,                                  -- snapshot for orphans + help-desk search
  machine_id            TEXT NOT NULL DEFAULT '',
  luks_uuid             UUID NOT NULL,
  mapping_name          TEXT NOT NULL DEFAULT '',
  mountpoints           TEXT[] NOT NULL DEFAULT '{}',
  is_system             BOOLEAN NOT NULL DEFAULT false,
  luks_version          SMALLINT NOT NULL,
  cipher                TEXT NOT NULL DEFAULT '',
  volume_key_bits       INT NOT NULL DEFAULT 0,
  state_json            JSONB NOT NULL,                                 -- last LuksVolumeState (protojson), no secrets
  rotation_requested_at TIMESTAMPTZ,
  rotation_reason       TEXT,
  last_reported_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  orphaned_at           TIMESTAMPTZ,                                    -- set when node_id becomes NULL
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX ux_luks_volumes_node_uuid ON luks_volumes (node_id, luks_uuid) WHERE node_id IS NOT NULL;
CREATE INDEX idx_luks_volumes_uuid ON luks_volumes (luks_uuid);            -- help-desk search, clone detection
CREATE INDEX idx_luks_volumes_node_name ON luks_volumes (lower(node_name));

-- Platform facts, one row per node.
CREATE TABLE node_disk_encryption (
  node_id                   UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  tpm2_present              BOOLEAN,
  secure_boot               TEXT NOT NULL DEFAULT 'unknown',
  initramfs_generator       TEXT NOT NULL DEFAULT 'unknown',
  systemd_version           TEXT NOT NULL DEFAULT '',
  cryptsetup_version        TEXT NOT NULL DEFAULT '',
  clevis_version            TEXT NOT NULL DEFAULT '',
  unencrypted_system_mounts JSONB NOT NULL DEFAULT '[]',
  reported_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Escrowed recovery keys, envelope-encrypted: the key is sealed with a
-- per-record DEK (AES-256-GCM), the DEK is wrapped by the KEK configured
-- outside the database (BOR_ESCROW_KEK_FILE / PKCS#11). Destroying a key
-- NULLs kek_id, wrapped_dek and ciphertext (crypto-shredding).
CREATE TABLE luks_recovery_keys (
  id                   UUID PRIMARY KEY,                -- escrow_id chosen by the agent (idempotency)
  volume_id            UUID NOT NULL REFERENCES luks_volumes(id) ON DELETE CASCADE,
  status               TEXT NOT NULL CHECK (status IN ('pending','active','superseded','retired','expired','destroyed')),
  reason               TEXT NOT NULL,
  kek_id               TEXT,
  wrapped_dek          BYTEA,
  ciphertext           BYTEA,                           -- nonce || ct || tag; AAD binds volume + escrow id + kek id
  keyslot              INT,
  keyslot_fingerprint  TEXT,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  confirmed_at         TIMESTAMPTZ,
  retired_at           TIMESTAMPTZ,
  destroy_after        TIMESTAMPTZ,
  reveal_count         INT NOT NULL DEFAULT 0,
  last_revealed_at     TIMESTAMPTZ,
  last_revealed_by     TEXT,
  released_at          TIMESTAMPTZ,                     -- server-assisted rotation
  release_rotation_id  UUID
);
CREATE UNIQUE INDEX ux_luks_recovery_keys_one_active ON luks_recovery_keys (volume_id) WHERE status = 'active';
CREATE INDEX idx_luks_recovery_keys_janitor ON luks_recovery_keys (status, destroy_after);

-- External Tang servers (Settings -> Tang servers).
CREATE TABLE tang_servers (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name                   TEXT NOT NULL UNIQUE,
  url                    TEXT NOT NULL UNIQUE,
  trusted_thumbprints    TEXT[] NOT NULL,               -- confirmed by an admin; [0] = preferred
  advertised_thumbprints TEXT[] NOT NULL DEFAULT '{}',  -- what the server advertises now (may be unconfirmed)
  advertisement          JSONB,                         -- last JWS that verified against a trusted thumbprint
  last_check_at          TIMESTAMPTZ,
  last_check_status      TEXT NOT NULL DEFAULT 'never', -- never | ok | new_key | error
  last_check_error       TEXT,
  created_by             TEXT NOT NULL DEFAULT '',
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- RBAC (000028/000029 idiom).
INSERT INTO permissions (resource, action) VALUES
  ('disk_encryption','view'), ('disk_encryption','reveal'), ('disk_encryption','rotate'),
  ('tang_server','view'), ('tang_server','create'), ('tang_server','edit'), ('tang_server','delete')
ON CONFLICT (resource, action) DO NOTHING;

-- disk_encryption:view -> every role that can already see nodes.
INSERT INTO role_permissions (role_id, permission_id)
  SELECT DISTINCT rp.role_id, p.id FROM role_permissions rp
  JOIN permissions m ON m.id = rp.permission_id AND m.resource = 'node' AND m.action = 'view'
  JOIN permissions p ON p.resource = 'disk_encryption' AND p.action = 'view'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- tang_server:* -> roles holding settings:manage.
INSERT INTO role_permissions (role_id, permission_id)
  SELECT DISTINCT rp.role_id, p.id FROM role_permissions rp
  JOIN permissions m ON m.id = rp.permission_id AND m.resource = 'settings' AND m.action = 'manage'
  JOIN permissions p ON p.resource = 'tang_server'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- reveal / rotate -> Super Admin only; everyone else needs an explicit grant.
-- (Super Admin receives permissions only at seed time, so select it by name.)
INSERT INTO role_permissions (role_id, permission_id)
  SELECT r.id, p.id FROM roles r
  JOIN permissions p ON p.resource = 'disk_encryption' AND p.action IN ('reveal','rotate')
  WHERE r.name = 'Super Admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;
