#!/usr/bin/env bash
# Subset web fonts for self-hosted bundling.
#
# This is an OPTIONAL build-time step. V1.x ships with a system-font
# fallback chain that works on macOS / Windows / modern Linux distros
# (PingFang SC, Microsoft YaHei UI, Noto Sans CJK SC, etc.). When the
# bundle is regenerated and committed under web/public/fonts/, the
# matching @font-face declarations in web/src/styles/fonts.css take
# precedence and pin the rendering to identical glyphs everywhere.
#
# Requirements:
#   pip install --user fonttools[woff,unicode] brotli
#
# Usage:
#   ./scripts/subset-fonts.sh /path/to/sources web/public/fonts
#
# The /path/to/sources directory must contain:
#   NotoSerifSC-Medium.otf
#   NotoSansSC-Regular.otf
#   NotoSansSC-Medium.otf
#   NotoSansSC-SemiBold.otf
#   Inter-Regular.ttf
#   Inter-Medium.ttf
#   Inter-SemiBold.ttf
#   JetBrainsMono-Regular.ttf
#
set -euo pipefail
SRC="${1:-/tmp/houfeng-fonts}"
DST="${2:-web/public/fonts}"
UNICODES="U+0020-007E,U+00A0-00FF,U+2010-201F,U+2025-2027,U+2030,U+2032-2033,U+2039-203A,U+203E,U+3000-303F,U+FF00-FFEF,U+4E00-9FFF"

if ! command -v pyftsubset >/dev/null 2>&1; then
  echo "pyftsubset not found. Install with:"
  echo "  pip install --user fonttools[woff,unicode] brotli"
  exit 1
fi

mkdir -p "$DST"

subset() {
  local in="$1" out="$2"
  pyftsubset "$in" \
    --unicodes="$UNICODES" \
    --layout-features='*' \
    --no-hinting \
    --desubroutinize \
    --output-file="$out" \
    --flavor=woff2
}

subset "$SRC/NotoSerifSC-Medium.otf"   "$DST/source-han-serif-sc-500.woff2"
subset "$SRC/NotoSansSC-Regular.otf"   "$DST/source-han-sans-sc-400.woff2"
subset "$SRC/NotoSansSC-Medium.otf"    "$DST/source-han-sans-sc-500.woff2"
subset "$SRC/NotoSansSC-SemiBold.otf"  "$DST/source-han-sans-sc-600.woff2"
subset "$SRC/Inter-Regular.ttf"        "$DST/inter-400.woff2"
subset "$SRC/Inter-Medium.ttf"         "$DST/inter-500.woff2"
subset "$SRC/Inter-SemiBold.ttf"       "$DST/inter-600.woff2"
subset "$SRC/JetBrainsMono-Regular.ttf" "$DST/jetbrains-mono-400.woff2"

ls -lh "$DST"
echo "Done. Commit web/public/fonts/ to enable bundled fonts."
