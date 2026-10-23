#!/bin/sh
# Builds the "Haiku" icon theme from the Haiku icons in $1 (svg/ and png/16x16 32x32 64x64)
# into $2, by icons.map: freedesktop names drawn with Haiku's icons, the rest from Adwaita.
set -eu
src="$1"
out="$2"
map="$(dirname "$0")/icons.map"
dirs=""
for size in 16 32 64 scalable; do
    for ctx in places apps mimetypes devices status actions; do
        mkdir -p "$out/$size/$ctx"
    done
done
grep -v '^#' "$map" | while read -r name haiku; do
    [ -n "$name" ] || continue
    ctx="${name%%/*}"
    icon="${name#*/}"
    for size in 16 32 64; do
        cp "$src/png/${size}x${size}/$haiku.png" "$out/$size/$ctx/$icon.png"
    done
    cp "$src/svg/$haiku.svg" "$out/scalable/$ctx/$icon.svg"
done
for size in 16 32 64; do
    for ctx in places apps mimetypes devices status actions; do
        dirs="$dirs$size/$ctx,"
    done
done
for ctx in places apps mimetypes devices status actions; do
    dirs="${dirs}scalable/$ctx,"
done
{
    echo "[Icon Theme]"
    echo "Name=Haiku"
    echo "Comment=Haiku's icons (MIT, Haiku, Inc.), by freedesktop names"
    echo "Inherits=Adwaita,hicolor"
    echo "Directories=${dirs%,}"
    for size in 16 32 64; do
        for ctx in places apps mimetypes devices status actions; do
            context="$(echo "$ctx" | sed 's/places/Places/;s/apps/Applications/;s/mimetypes/MimeTypes/;s/devices/Devices/;s/status/Status/;s/actions/Actions/')"
            printf '\n[%s/%s]\nSize=%s\nContext=%s\nType=Fixed\n' "$size" "$ctx" "$size" "$context"
        done
    done
    for ctx in places apps mimetypes devices status actions; do
        context="$(echo "$ctx" | sed 's/places/Places/;s/apps/Applications/;s/mimetypes/MimeTypes/;s/devices/Devices/;s/status/Status/;s/actions/Actions/')"
        printf '\n[scalable/%s]\nSize=64\nMinSize=8\nMaxSize=512\nContext=%s\nType=Scalable\n' "$ctx" "$context"
    done
} >"$out/index.theme"
