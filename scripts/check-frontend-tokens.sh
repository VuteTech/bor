#!/usr/bin/env bash
#
# Frontend design-token guardrail — see docs/frontend-ux-improvement-plan.md.
#
# 1. Hard-bans legacy `--pf-v5-*` CSS variables anywhere under the frontend
#    src. PatternFly 6 uses `--pf-t--*` / `--pf-v6-*` tokens; a v5 variable is a
#    regression that silently fails to resolve.
#
# 2. Ratchets raw hex colour literals in component code (*.ts / *.tsx, excluding
#    the generated/ protobuf output). The multiset of {file, hex-value} must not
#    grow beyond the committed baseline (scripts/frontend-hex-baseline.txt). Raw
#    hex belongs only in the design-token source `bor-theme.css`, which is
#    intentionally exempt — that is where tokens are defined from hex.
#
# The ratchet is keyed on {file, value} counts (not line numbers), so it is
# stable against unrelated edits but still catches any newly introduced hex.
#
# Usage:
#   scripts/check-frontend-tokens.sh            verify (CI mode; non-zero on regressions)
#   scripts/check-frontend-tokens.sh --update   regenerate the hex baseline after an
#                                               intentional, reviewed change
#
set -eu

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SRC="server/web/frontend/src"
BASELINE="scripts/frontend-hex-baseline.txt"

# Normalised {file<TAB>hex} multiset with per-pair counts, sorted.
collect_hex() {
  grep -rnoE '#[0-9a-fA-F]{3,8}\b' "$SRC" --include='*.ts' --include='*.tsx' 2>/dev/null \
    | grep -v "$SRC/generated/" \
    | sed -E 's/:[0-9]+:/\t/' \
    | sort \
    | uniq -c \
    | sed -E 's/^[[:space:]]+//'
}

status=0

# ── 1. Legacy PatternFly v5 variables (hard ban, must stay at zero) ──────────
pf5="$(grep -rnE '\-\-pf-v5-' "$SRC" --include='*.ts' --include='*.tsx' --include='*.css' 2>/dev/null || true)"
if [ -n "$pf5" ]; then
  echo "✗ Legacy --pf-v5-* variables found (use --pf-t--* / --pf-v6-* tokens):"
  echo "$pf5" | sed 's/^/    /'
  status=1
else
  echo "✓ No --pf-v5-* variables under $SRC."
fi

# ── 2. Raw hex ratchet in component code ─────────────────────────────────────
current="$(collect_hex)"

if [ "${1:-}" = "--update" ]; then
  printf '%s\n' "$current" > "$BASELINE"
  echo "✓ Wrote hex baseline ($(printf '%s\n' "$current" | grep -c . ) file/value entries) to $BASELINE."
  exit "$status"
fi

if [ ! -f "$BASELINE" ]; then
  echo "✗ Missing hex baseline $BASELINE. Generate it with: scripts/check-frontend-tokens.sh --update"
  exit 1
fi

added="$(comm -13 <(sort "$BASELINE") <(printf '%s\n' "$current" | sort) || true)"
removed="$(comm -23 <(sort "$BASELINE") <(printf '%s\n' "$current" | sort) || true)"

if [ -n "$added" ]; then
  echo "✗ New raw hex colour literal(s) in component code. Prefer a --pf-t--* token;"
  echo "  if genuinely unavoidable, justify it in review and run --update to accept it:"
  echo "$added" | sed 's/^/    + /'
  status=1
elif [ -z "$removed" ]; then
  echo "✓ No new raw hex literals beyond the reviewed baseline."
fi

if [ -n "$removed" ]; then
  echo "ℹ Hex was removed (good) — the baseline has stale entries. Refresh with --update:"
  echo "$removed" | sed 's/^/    - /'
fi

exit "$status"
