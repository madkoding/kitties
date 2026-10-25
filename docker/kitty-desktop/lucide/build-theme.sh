#!/bin/sh
# Builds the "Lucide" icon theme from the Lucide icons in $1 (lucide-static's icons/*.svg) into $2,
# by icons.map: freedesktop names drawn with Lucide's outlines in a light ink, the rest from Adwaita.
set -eu
src="$1"
out="$2"
map="$(dirname "$0")/icons.map"
ink="#e8eaed"
ctxs="places apps mimetypes devices status actions"
for ctx in $ctxs; do
    mkdir -p "$out/scalable/$ctx"
done
grep -v '^#' "$map" | while read -r name lucide; do
    [ -n "$name" ] || continue
    sed "s/currentColor/$ink/" "$src/$lucide.svg" >"$out/scalable/${name%%/*}/${name#*/}.svg"
done
dirs=""
for ctx in $ctxs; do
    dirs="${dirs}scalable/$ctx,"
done
{
    echo "[Icon Theme]"
    echo "Name=Lucide"
    echo "Comment=Lucide's icons (ISC), by freedesktop names"
    echo "Inherits=Adwaita,hicolor"
    echo "Directories=${dirs%,}"
    for ctx in $ctxs; do
        context="$(echo "$ctx" | sed 's/places/Places/;s/apps/Applications/;s/mimetypes/MimeTypes/;s/devices/Devices/;s/status/Status/;s/actions/Actions/')"
        printf '\n[scalable/%s]\nSize=64\nMinSize=8\nMaxSize=512\nContext=%s\nType=Scalable\n' "$ctx" "$context"
    done
} >"$out/index.theme"
