#!/bin/bash
# Makes the page's typeface: Allo, the typeface of Espalier's Default theme,
# from the font Espalier bundles, which is Allo at its roundest. It keeps the
# weight and optical size axes over the ranges the page uses (weight 400 to
# 750, optical size 12 to 32) and only the characters it uses (Latin,
# punctuation and a few symbols).
#
#   scripts/font.sh
#
# Needs a checkout of Espalier (ESPALIER, default ~/Developer/Espalier) and
# fonttools with WOFF2 support (brew install fonttools). Writes
# web/static/fonts/allo.woff2.

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
espalier="${ESPALIER:-$HOME/Developer/Espalier}"
source="$espalier/Packages/EspalierKit/Sources/EspalierUI/Resources/Fonts/allo/AlloVariable-Round.ttf"
out="$root/web/static/fonts/allo.woff2"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for tool in fonttools pyftsubset; do
  command -v "$tool" >/dev/null || { echo "error: $tool isn't installed (brew install fonttools)" >&2; exit 1; }
done
[[ -f "$source" ]] || { echo "error: no Espalier checkout at $espalier (set ESPALIER)" >&2; exit 1; }

mkdir -p "$(dirname "$out")"
fonttools varLib.instancer "$source" wght=400:750 opsz=12:32 -q -o "$work/allo.ttf"
pyftsubset "$work/allo.ttf" \
  --output-file="$out" \
  --flavor=woff2 \
  --unicodes="U+0020-007E,U+00A0-00FF,U+0131,U+0152-0153,U+02C6,U+02DA,U+02DC,U+2000-206F,U+2190-2193,U+21A9,U+21E7,U+2212,U+2318,U+2713,U+FEFF,U+FFFD" \
  --layout-features+=tnum,case \
  --no-hinting \
  --desubroutinize

echo "Wrote web/static/fonts/allo.woff2 ($(du -k "$out" | cut -f1) KB)"
