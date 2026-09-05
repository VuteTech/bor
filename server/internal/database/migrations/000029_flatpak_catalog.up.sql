-- Flatpak repositories indexed by the server (Settings → Flatpak repositories)
-- and the AppStream catalog parsed from them. See docs/flatpak-policy-plan.md.

CREATE TABLE flatpak_repositories (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name                  TEXT NOT NULL UNIQUE,            -- remote name suggested to policies ("flathub")
  title                 TEXT NOT NULL DEFAULT '',
  url                   TEXT NOT NULL,                   -- https://dl.flathub.org/repo/
  flatpakrepo_url       TEXT NOT NULL DEFAULT '',        -- where it was imported from (optional)
  homepage              TEXT NOT NULL DEFAULT '',
  comment               TEXT NOT NULL DEFAULT '',
  description           TEXT NOT NULL DEFAULT '',
  icon_url              TEXT NOT NULL DEFAULT '',
  gpg_key               BYTEA,                           -- public keyring (binary)
  gpg_key_id            TEXT NOT NULL DEFAULT '',        -- optional 40-hex fingerprint
  collection_id         TEXT NOT NULL DEFAULT '',
  default_branch        TEXT NOT NULL DEFAULT '',
  subset                TEXT NOT NULL DEFAULT '',        -- suggested --subset for policies
  appstream_url         TEXT NOT NULL DEFAULT '',        -- '' = <url>/appstream/<arch>/appstream.xml.gz
  arches                TEXT NOT NULL DEFAULT 'x86_64',  -- comma-separated; each indexed separately
  catalog_enabled       BOOLEAN NOT NULL DEFAULT true,
  refresh_interval_s    INT NOT NULL DEFAULT 86400 CHECK (refresh_interval_s >= 3600),
  builtin               BOOLEAN NOT NULL DEFAULT false,  -- seeded rows cannot be deleted, only disabled
  last_refresh_at       TIMESTAMPTZ,
  last_refresh_status   TEXT NOT NULL DEFAULT 'never',   -- never | ok | error | running | disabled
  last_refresh_error    TEXT NOT NULL DEFAULT '',
  last_refresh_etag     TEXT NOT NULL DEFAULT '',
  last_refresh_modified TEXT NOT NULL DEFAULT '',
  app_count             INT NOT NULL DEFAULT 0,
  created_by            TEXT NOT NULL DEFAULT '',
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per (repository, app, arch, branch) parsed from AppStream.
CREATE TABLE flatpak_catalog_apps (
  repo_id           UUID NOT NULL REFERENCES flatpak_repositories(id) ON DELETE CASCADE,
  app_id            TEXT NOT NULL,
  arch              TEXT NOT NULL,
  branch            TEXT NOT NULL,
  ref               TEXT NOT NULL,                       -- app/org.x.y/x86_64/stable
  kind              TEXT NOT NULL,                       -- desktop-application | console-application | addon | runtime | other
  name              TEXT NOT NULL DEFAULT '',
  summary           TEXT NOT NULL DEFAULT '',
  description       TEXT NOT NULL DEFAULT '',            -- tags stripped, <= 4 KiB
  developer         TEXT NOT NULL DEFAULT '',
  project_license   TEXT NOT NULL DEFAULT '',
  homepage          TEXT NOT NULL DEFAULT '',
  categories        TEXT NOT NULL DEFAULT '',            -- ';'-joined
  keywords          TEXT NOT NULL DEFAULT '',            -- ' '-joined, untranslated
  runtime           TEXT NOT NULL DEFAULT '',            -- org.gnome.Platform/x86_64/50
  latest_version    TEXT NOT NULL DEFAULT '',
  latest_release_at TIMESTAMPTZ,
  verified          BOOLEAN NOT NULL DEFAULT false,      -- flathub::verification::verified
  icon_file         TEXT NOT NULL DEFAULT '',            -- <icon type="cached"> file name
  content_rating    TEXT NOT NULL DEFAULT '',            -- oars-1.1 etc.
  extra_json        JSONB,
  search_tsv        TSVECTOR GENERATED ALWAYS AS (
                      setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
                      setweight(to_tsvector('simple', replace(app_id, '.', ' ')), 'A') ||
                      setweight(to_tsvector('simple', coalesce(summary, '')), 'B') ||
                      setweight(to_tsvector('simple', coalesce(keywords, '') || ' ' || coalesce(developer, '')), 'C')
                    ) STORED,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (repo_id, app_id, arch, branch)
);
CREATE INDEX idx_flatpak_catalog_apps_tsv  ON flatpak_catalog_apps USING GIN (search_tsv);
CREATE INDEX idx_flatpak_catalog_apps_id   ON flatpak_catalog_apps (app_id);
CREATE INDEX idx_flatpak_catalog_apps_kind ON flatpak_catalog_apps (repo_id, kind, verified);

-- Lazily fetched icons (mime '' = negative cache entry).
CREATE TABLE flatpak_catalog_icons (
  repo_id    UUID NOT NULL REFERENCES flatpak_repositories(id) ON DELETE CASCADE,
  app_id     TEXT NOT NULL,
  mime       TEXT NOT NULL DEFAULT '',
  data       BYTEA,
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (repo_id, app_id)
);

-- RBAC: new resource flatpak_repo (000028 idiom). Backfill every role that
-- already holds settings:manage so existing administrators keep working.
INSERT INTO permissions (resource, action) VALUES
    ('flatpak_repo', 'view'),
    ('flatpak_repo', 'create'),
    ('flatpak_repo', 'edit'),
    ('flatpak_repo', 'delete'),
    ('flatpak_repo', 'refresh')
ON CONFLICT (resource, action) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, p.id
FROM role_permissions rp
JOIN permissions m ON m.id = rp.permission_id AND m.resource = 'settings' AND m.action = 'manage'
JOIN permissions p ON p.resource = 'flatpak_repo'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Seed Flathub. The GPG key is the GPGKey value of
-- https://dl.flathub.org/repo/flathub.flatpakrepo (captured 2026-09-05).
INSERT INTO flatpak_repositories
  (name, title, url, flatpakrepo_url, homepage, comment, description, icon_url, gpg_key, collection_id, builtin)
VALUES
  ('flathub', 'Flathub', 'https://dl.flathub.org/repo/', 'https://dl.flathub.org/repo/flathub.flatpakrepo',
   'https://flathub.org/', 'Central repository of Flatpak applications', 'Central repository of Flatpak applications',
   'https://dl.flathub.org/repo/logo.svg', decode('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', 'base64'), 'org.flathub.Stable', true)
ON CONFLICT (name) DO NOTHING;
