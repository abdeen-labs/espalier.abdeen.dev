#!/bin/bash
# Makes the page's images from Espalier itself: the window screenshots from
# Espalier's Showcase captures (History and Changes in Classic, History in
# Frost, Islands, and Pop, each light and dark), the app icon from its
# Icon Composer file, and the favicons and sharing image from those.
#
#   scripts/images.sh
#
# Needs a checkout of Espalier (ESPALIER, default ~/Developer/Espalier),
# Xcode, ImageMagick, and cwebp. Writes web/static/images and
# web/root/{favicon.ico,apple-touch-icon.png}.

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
espalier="${ESPALIER:-$HOME/Developer/Espalier}"
ictool="/Applications/Xcode.app/Contents/Applications/Icon Composer.app/Contents/Executables/ictool"
out="$root/web/static/images"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for tool in magick cwebp; do
  command -v "$tool" >/dev/null || { echo "error: $tool isn't installed (brew install imagemagick webp)" >&2; exit 1; }
done
[[ -x "$ictool" ]] || { echo "error: no Icon Composer in /Applications/Xcode.app" >&2; exit 1; }
[[ -x "$espalier/scripts/capture.sh" ]] || { echo "error: no Espalier checkout at $espalier (set ESPALIER)" >&2; exit 1; }

rm -rf "$out"
mkdir -p "$out"

# webp <png> <name>: the screenshot at 1440 and 2880 pixels wide.
webp() {
  for width in 1440 2880; do
    cwebp -quiet -q 82 -m 6 -sharp_yuv -resize "$width" 0 "$1" -o "$out/$2-$width.webp"
  done
}

# Classic's looks first: History and Changes, and a close-up of the graph.
looks=(classic frost-iris islands-lagoon pop-magenta)
for look in "${looks[@]}"; do
  "$espalier/scripts/capture.sh" --look "$look" --out "$work/$look" ShowcaseCaptures >/dev/null
  theme="${look%%-*}"
  for appearance in light dark; do
    shots="$work/$look/$look/Showcase"
    webp "$shots/history-$appearance.png" "$theme-$appearance"
    if [[ "$look" == classic ]]; then
      webp "$shots/changes-$appearance.png" "changes-$appearance"
      # The commit list and its graph, from the toolbar's row down.
      magick "$shots/history-$appearance.png" -crop 840x1070+482+110 +repage "$work/graph-$appearance.png"
      for width in 420 840; do
        cwebp -quiet -q 84 -m 6 -sharp_yuv -resize "$width" 0 "$work/graph-$appearance.png" -o "$out/graph-$appearance-$width.webp"
      done
    fi
  done
done

# The icon, light and dark, and what's made from it.
for rendition in Default Dark; do
  "$ictool" "$espalier/Espalier/AppIcon.icon" --export-image --output-file "$work/icon-$rendition.png" \
    --platform macOS --rendition "$rendition" --width 1024 --height 1024 --scale 1 >/dev/null
done
for size in 128 256 512; do
  cwebp -quiet -q 90 -alpha_q 100 -resize "$size" "$size" "$work/icon-Default.png" -o "$out/icon-light-$size.webp"
  cwebp -quiet -q 90 -alpha_q 100 -resize "$size" "$size" "$work/icon-Dark.png" -o "$out/icon-dark-$size.webp"
done
magick "$work/icon-Default.png" -resize 64x64 -strip "$out/icon-64.png"
magick "$work/icon-Default.png" -resize 180x180 -strip "$root/web/root/apple-touch-icon.png"
magick "$work/icon-Default.png" -define icon:auto-resize=48,32,16 -strip "$root/web/root/favicon.ico"

# The sharing image: the icon and the name over History, 1200 by 630, in SF
# Pro where it's installed.
bold=/Library/Fonts/SF-Pro-Display-Bold.otf
regular=/Library/Fonts/SF-Pro-Display-Regular.otf
[[ -f "$bold" && -f "$regular" ]] || bold=/System/Library/Fonts/Helvetica.ttc regular=/System/Library/Fonts/Helvetica.ttc
magick -size 1200x630 xc:'#110c19' \
  \( "$work/classic/classic/Showcase/history-dark.png" -resize 1040x \) -geometry +80+250 -composite \
  \( "$work/icon-Dark.png" -resize 132x132 \) -geometry +80+64 -composite \
  -font "$bold" -pointsize 64 -fill '#f9f7fe' -annotate +238+138 'Espalier' \
  -font "$regular" -pointsize 28 -fill '#beb6ce' -annotate +240+184 'A native Git client for the Mac' \
  -quality 86 -sampling-factor 4:2:0 -strip "$out/og.jpg"

echo "Wrote $(ls "$out" | wc -l | tr -d ' ') images to web/static/images"
