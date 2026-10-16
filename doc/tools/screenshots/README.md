# Documentation screenshots

`doc-shoot.js` captures every web UI screenshot used by the documentation
into `doc/images/screenshots/` (WebP, 1280x800 at 2x, light theme). File
names are stable, so re-running it before a release replaces the images in
place and the diff shows what changed.

This directory is excluded from the published site (`exclude_docs` in
`mkdocs.yml`).

## Prerequisites

- Google Chrome or Chromium on the host (`CHROME_BIN=/path/to/chrome` to
  override the default `/usr/bin/google-chrome-stable`).
- Node.js 22.12 or newer, then once in this directory: `npm install`
  (installs `puppeteer-core`).
- A **throwaway** Bor server with demo data. Never point the script at a
  real installation: screenshots are published.

## Prepare the instance

1. Start a fresh PostgreSQL and a server built from the current tree, for
   example on the default web port 8443. Set `BOR_ESCROW_KEK_FILE` (any
   32-byte file) so the disk encryption screens work.
2. Seed it with the demo kit, so no screen is empty:

    ```bash
    BOR_URL=https://localhost:8443 ADMIN_PASSWORD=... DEMO_USERS_PASSWORD=... \
      SEED_DIR=demo/seed bash demo/reset/seed.sh
    psql ... -f demo/reset/extras.sql
    ```

3. Optional clean-up for a neutral look: delete the public-demo-only `demo`
   user and `Demo Admin` role, and insert a few `luks_volumes` /
   `luks_recovery_keys` rows for the disk encryption pages (the demo kit has
   none).

## Capture

```bash
export BOR_CA_CERT=/path/to/pki/ca/ca.crt            # the server's internal CA
node doc-shoot.js admin '<admin-password>'          # all shots
SHOTS=dashboard,nodes node doc-shoot.js admin '...'  # only some
node doc-shoot.js /tmp/out admin '...'               # other output dir
```

Environment: `BOR_CA_CERT` (required: the CA certificate that signed the
server's TLS certificate, `ca.crt` in the CA autogen directory; API calls
verify the server against it), `BOR_API` (default `https://localhost:8443`,
the host name must be in the server certificate), `BOR_UI_HOST`
(host name shown in the browser, default `bor.example.com`, resolved to
127.0.0.1), `CHROME_BIN`, `SHOTS`.

What the script does:

- Copies one released demo policy per type into a temporary draft for the
  editor shots (built-in examples for Flatpak and Disk encryption) and
  deletes the drafts at the end.
- Fakes the agent package manifest and the enrollment-token response, so
  the Deploy agent dialog renders without a real repository and no token is
  issued.
- Fills in, but never submits, the recovery-key reveal form.

The run itself creates audit entries; delete them from `audit_logs` and
re-take `audit-logs` if they show up in that screenshot.

## After capturing

Look at every image before committing: no real host names, user names or
secrets. Each image used in a page needs alt text describing what it shows.
New policy types: add an entry to `EDITOR_SHOTS` in `doc-shoot.js`.
