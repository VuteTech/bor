#!/usr/bin/env bash
# assemble-agent-repo.sh — build the on-disk agent package repository that the
# Bor server serves at /agent/* (docs/agent-package-downloads-plan.md).
#
# Takes the bor-agent packages nfpm already produced (all formats, all
# architectures) and assembles a static tree with real distribution-repo
# metadata, generated once here at build time — the server serves plain files
# and never runs repo tooling or holds signing keys:
#
#   OUT_DIR/
#   ├── manifest.json          machine-readable index (UI + startup check)
#   ├── repo-key.asc           public signing key (only when signing)
#   ├── deb/                   flat apt repo: *.deb + Packages(.gz) + Release
#   │                          + InRelease/Release.gpg (when signing)
#   ├── rpm/                   dnf/zypper repo: *.rpm + repodata/
#   │                          + repodata/repomd.xml.asc (when signing)
#   ├── apk/                   direct-download .apk files (no index — Alpine
#   │                          repo signing is abuild-specific; see plan §4.4)
#   └── arch/                  direct-download pacman packages
#
# Environment:
#   VERSION       (required) package version, e.g. 0.9.0 or 0.0.0.gitabc1234
#   APK_VERSION   Alpine package version (numeric-only); default: VERSION
#   PACKAGES_DIR  directory containing the built packages;   default: builds
#   OUT_DIR       output tree;                    default: builds/agent-repo
#   SIGN_KEY_ID   GPG key id/fingerprint in the current keyring. Empty = the
#                 repos are assembled unsigned (manifest records signed:false;
#                 CI signs whenever the release key secret is available).
#   SIGN_KEY_PASSPHRASE
#                 Passphrase of the signing key, when it has one. Enables
#                 non-interactive loopback pinentry — without it a protected
#                 key fails in CI with "Inappropriate ioctl for device"
#                 (gpg trying to prompt on a nonexistent TTY).
#
# Requires: apt-ftparchive (apt-utils), createrepo_c, jq, gzip, sha256sum,
# and gpg when SIGN_KEY_ID is set. CI installs these; locally the tooling is
# Debian-flavoured — run inside a debian container if your distro lacks it.
set -euo pipefail

: "${VERSION:?VERSION is required}"
APK_VERSION="${APK_VERSION:-$VERSION}"
PACKAGES_DIR="${PACKAGES_DIR:-builds}"
OUT_DIR="${OUT_DIR:-builds/agent-repo}"
SIGN_KEY_ID="${SIGN_KEY_ID:-}"

for tool in jq gzip sha256sum apt-ftparchive createrepo_c; do
  command -v "$tool" >/dev/null || {
    echo "FATAL: '$tool' not found. Install apt-utils/createrepo-c/jq (Debian-family tooling; use a debian container on other distros)." >&2
    exit 1
  }
done
if [ -n "$SIGN_KEY_ID" ]; then
  command -v gpg >/dev/null || { echo "FATAL: SIGN_KEY_ID set but gpg not found" >&2; exit 1; }
fi

log() { printf '[agent-repo] %s\n' "$*"; }

# Fresh output tree. builds/agent-repo/.gitkeep is tracked in git (it keeps
# the Containerfile COPY valid on checkouts that never ran this script) — it
# is restored at the end.
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"/{deb,rpm,apk,arch}

# ── Collect the bor-agent packages ───────────────────────────────────────────
# Version-scoped globs (nfpm's naming per packager): stale artifacts from
# earlier local builds and the bor-server*/bor-agent-repo* packages must
# never end up in the tree. CI workspaces are clean; local ones often aren't.
collect() { # glob, destdir
  find "$PACKAGES_DIR" -maxdepth 1 -name "$1" -print0 | xargs -0 -r cp -t "$OUT_DIR/$2"
}
collect "bor-agent_${VERSION}_*.deb" deb
collect "bor-agent-${VERSION}-*.rpm" rpm
collect "bor-agent_${APK_VERSION}_*.apk" apk
collect "bor-agent-${VERSION}-*.pkg.tar.zst" arch

shopt -s nullglob
DEBS=("$OUT_DIR"/deb/*.deb)
RPMS=("$OUT_DIR"/rpm/*.rpm)
APKS=("$OUT_DIR"/apk/*.apk)
ARCHPKGS=("$OUT_DIR"/arch/*.pkg.tar.zst)
shopt -u nullglob

[ "${#DEBS[@]}" -gt 0 ] || { echo "FATAL: no bor-agent .deb packages found in $PACKAGES_DIR" >&2; exit 1; }
[ "${#RPMS[@]}" -gt 0 ] || { echo "FATAL: no bor-agent .rpm packages found in $PACKAGES_DIR" >&2; exit 1; }
log "collected: ${#DEBS[@]} deb, ${#RPMS[@]} rpm, ${#APKS[@]} apk, ${#ARCHPKGS[@]} arch"
if [ "${#DEBS[@]}" -lt 3 ]; then
  log "WARNING: fewer than 3 architectures present — fine for local dev, wrong for a release"
fi

# ── apt flat repository (deb/) ───────────────────────────────────────────────
# Flat repos ("deb <url> ./") are URL-agnostic and need no dists/pool layout.
log "generating apt metadata"
DEB_ARCHES=$(for f in "${DEBS[@]}"; do basename "$f" .deb | awk -F_ '{print $NF}'; done | sort -u | tr '\n' ' ')
(
  cd "$OUT_DIR/deb"
  apt-ftparchive packages . > Packages
  gzip -9 -k -n Packages
  apt-ftparchive \
    -o "APT::FTPArchive::Release::Origin=Bor" \
    -o "APT::FTPArchive::Release::Label=Bor Agent" \
    -o "APT::FTPArchive::Release::Suite=stable" \
    -o "APT::FTPArchive::Release::Architectures=${DEB_ARCHES% }" \
    -o "APT::FTPArchive::Release::Description=bor-agent ${VERSION} — served by your Bor server instance" \
    release . > Release
)

# ── dnf/zypper repository (rpm/) ─────────────────────────────────────────────
# One repo for all architectures; dnf filters by the arch recorded per RPM.
# createrepo_c defaults to SHA-256 throughout (no MD5 anywhere — compliance).
log "generating rpm metadata"
createrepo_c --quiet "$OUT_DIR/rpm"

# ── Signing (build-time only; the server never sees private keys) ────────────
SIGNED=false
if [ -n "$SIGN_KEY_ID" ]; then
  log "signing with key $SIGN_KEY_ID"
  GPG_SIGN=(gpg --batch --yes --local-user "$SIGN_KEY_ID")
  PASSFILE=""
  if [ -n "${SIGN_KEY_PASSPHRASE:-}" ]; then
    # Passphrase-protected key: loopback pinentry, passphrase via a 0600 file
    # (never argv — process listings). Requires GnuPG >= 2.1.12.
    PASSFILE=$(mktemp)
    chmod 600 "$PASSFILE"
    printf '%s' "$SIGN_KEY_PASSPHRASE" > "$PASSFILE"
    trap '[ -n "$PASSFILE" ] && rm -f "$PASSFILE"' EXIT
    GPG_SIGN+=(--pinentry-mode loopback --passphrase-file "$PASSFILE")
  fi
  # apt: InRelease (inline) + Release.gpg (detached, for older apt).
  "${GPG_SIGN[@]}" \
    --clearsign -o "$OUT_DIR/deb/InRelease" "$OUT_DIR/deb/Release"
  "${GPG_SIGN[@]}" \
    --armor --detach-sign -o "$OUT_DIR/deb/Release.gpg" "$OUT_DIR/deb/Release"
  # dnf/zypper: detached signature over repomd.xml (repo_gpgcheck=1).
  "${GPG_SIGN[@]}" \
    --armor --detach-sign -o "$OUT_DIR/rpm/repodata/repomd.xml.asc" \
    "$OUT_DIR/rpm/repodata/repomd.xml"
  # Public key, served to clients for Signed-By= / gpgkey= (no passphrase needed).
  gpg --batch --export --armor "$SIGN_KEY_ID" > "$OUT_DIR/repo-key.asc"
  [ -n "$PASSFILE" ] && rm -f "$PASSFILE" && PASSFILE=""
  SIGNED=true
else
  log "WARNING: SIGN_KEY_ID not set — assembling UNSIGNED repositories (dev only)"
fi

# ── manifest.json ────────────────────────────────────────────────────────────
# The UI's single source of truth: every file with format, normalized
# architecture (Go names), size and SHA-256.
log "generating manifest.json"
# Normalized (Go-style) architecture from a package filename. Whole-name
# pattern matching, because the packagers' own separators are ambiguous —
# apk's "x86_64" contains the underscore apk otherwise separates fields with.
file_arch() { # path, format (format unused; kept for call-site clarity)
  local base
  base=$(basename "$1")
  case "$base" in
    *x86_64*|*amd64*)        echo amd64 ;;
    *aarch64*|*arm64*)       echo arm64 ;;
    *ppc64le*|*ppc64el*)     echo ppc64le ;;
    *_all.*|*noarch*|*-any.*) echo all ;;
    *)                       echo unknown ;;
  esac
}

FILES_JSON="[]"
add_files() { # format, files...
  local format=$1 f rel size sha a
  shift
  for f in "$@"; do
    rel=${f#"$OUT_DIR/"}
    size=$(stat -c%s "$f")
    sha=$(sha256sum "$f" | cut -d' ' -f1)
    a=$(file_arch "$f" "$format")
    FILES_JSON=$(jq -c --arg p "$rel" --arg fo "$format" --arg a "$a" \
      --argjson s "$size" --arg h "$sha" \
      '. + [{path:$p, format:$fo, arch:$a, size:$s, sha256:$h}]' <<<"$FILES_JSON")
  done
}
add_files deb "${DEBS[@]}"
add_files rpm "${RPMS[@]}"
[ "${#APKS[@]}" -gt 0 ] && add_files apk "${APKS[@]}"
[ "${#ARCHPKGS[@]}" -gt 0 ] && add_files arch "${ARCHPKGS[@]}"

jq -n \
  --arg version "$VERSION" \
  --arg apk_version "$APK_VERSION" \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson signed "$SIGNED" \
  --argjson files "$FILES_JSON" \
  '{
    version: $version,
    apk_version: $apk_version,
    generated_at: $generated_at,
    signed: $signed,
    channels: {
      deb:  {repo: true},
      rpm:  {repo: true},
      apk:  {repo: false},
      arch: {repo: false}
    },
    files: ($files | sort_by(.path))
  }' > "$OUT_DIR/manifest.json"

# ── Static instructions page (served at /agent/) ────────────────────────────
# Self-contained; the instance URL is filled in client-side, so the page is
# identical for every deployment. The full wizard lives in the web UI.
log "generating index.html"
SIGNED_TEXT="signed"
[ "$SIGNED" = true ] || SIGNED_TEXT="UNSIGNED (development build)"
cat > "$OUT_DIR/index.html" <<EOF
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Bor agent packages</title>
<style>
  body { font-family: system-ui, sans-serif; max-width: 46rem; margin: 2rem auto; padding: 0 1rem; line-height: 1.5; }
  pre { background: #f4f4f4; padding: 0.75rem; overflow-x: auto; border-radius: 4px; }
  code { font-size: 0.9em; }
  @media (prefers-color-scheme: dark) { body { background: #151515; color: #e0e0e0; } pre { background: #242424; } a { color: #95d5b2; } }
</style>
</head>
<body>
<h1>Bor agent packages</h1>
<p>bor-agent <strong>${VERSION}</strong> — repository ${SIGNED_TEXT}, served by
this Bor instance. The full deploy wizard (platform picker, enrollment
tokens) is in the web UI: <em>Nodes &rarr; Deploy agent</em>.</p>
<p>Files &amp; checksums: <a href="manifest.json">manifest.json</a> ·
Signing key: <a href="repo-key.asc">repo-key.asc</a> ·
Instance CA: <a href="ca.crt">ca.crt</a></p>
<h2>Debian / Ubuntu</h2>
<pre><code>sudo install -d -m 755 /etc/apt/keyrings
curl -fsS <span class="origin">ORIGIN</span>/agent/repo-key.asc | sudo tee /etc/apt/keyrings/bor.asc &gt;/dev/null
printf 'Types: deb\nURIs: <span class="origin">ORIGIN</span>/agent/deb/\nSuites: ./\nSigned-By: /etc/apt/keyrings/bor.asc\n' | sudo tee /etc/apt/sources.list.d/bor.sources &gt;/dev/null
sudo apt-get update &amp;&amp; sudo apt-get install -y bor-agent</code></pre>
<h2>RHEL / Fedora</h2>
<pre><code>cat &lt;&lt;REPO | sudo tee /etc/yum.repos.d/bor.repo &gt;/dev/null
[bor]
name=Bor Agent
baseurl=<span class="origin">ORIGIN</span>/agent/rpm/
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=<span class="origin">ORIGIN</span>/agent/repo-key.asc
REPO
sudo dnf install -y bor-agent</code></pre>
<p>SUSE uses the same rpm repository via <code>zypper addrepo</code>; Alpine
and Arch packages are direct downloads — paths in
<a href="manifest.json">manifest.json</a>. If this instance uses its
auto-generated certificate, trust <a href="ca.crt">ca.crt</a> first. Details:
<code>docs/agent-packages.md</code> in the Bor repository.</p>
<script>
  for (const el of document.querySelectorAll(".origin")) {
    el.textContent = window.location.origin;
  }
</script>
</body>
</html>
EOF

# Restore the tracked placeholder (see the note at the top of this section).
touch "$OUT_DIR/.gitkeep"

log "done: $(jq -r '.files | length' "$OUT_DIR/manifest.json") files, signed=$SIGNED → $OUT_DIR"
