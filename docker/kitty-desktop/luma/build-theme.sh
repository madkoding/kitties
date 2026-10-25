#!/bin/sh
# Builds the IceWM theme "luma" into $1: the flat theme file ($(dirname $0)/default.theme) and
# its window buttons, drawn with ImageMagick as Lucide-style strokes (2 units of 24, round caps).
set -eu
out="$1"
here="$(dirname "$0")"
mkdir -p "$out"
cp "$here/default.theme" "$out/default.theme"
# button NAME DRAWING: the active (A) and inactive (I) button as IceWM reads them, two frames
# stacked (at rest, pressed) of 34x34, as tall as the title row; the glyph is drawn in 22x22 and
# centred, in ink on the title bar's colour (A) and in muted (I), the pressed one on the hover fill.
button() {
    name="$1"
    draw="$2"
    for state in A:'#25282d':'#f2f3f5' I:'#202124':'#8d8f93'; do
        suffix="${state%%:*}"
        rest="${state#*:}"
        bg="${rest%%:*}"
        ink="${rest#*:}"
        convert -size 34x68 "xc:$bg" -fill '#30363d' -draw 'rectangle 0,34 33,67' -fill none -stroke "$ink" -strokewidth 1.6 \
            -draw "stroke-linecap round stroke-linejoin round translate 6,6 $draw translate 0,34 $draw" "PNG24:$out/$name$suffix.png"
    done
}
button close 'line 7,7 15,15 line 15,7 7,15'
button minimize 'line 6,11 16,11'
button maximize 'roundrectangle 6,6 16,16 2,2'
button restore 'roundrectangle 6,8 14,16 2,2 polyline 9,6 9,5 15,5 17,7 17,13 16,13'
button hide 'line 6,16 16,16'
button rollup 'polyline 7,13 11,9 15,13'
button rolldown 'polyline 7,9 11,13 15,9'
button depth 'roundrectangle 5,5 13,13 2,2 roundrectangle 9,9 17,17 2,2'
