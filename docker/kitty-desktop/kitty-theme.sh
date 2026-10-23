#!/bin/sh
# Puts IceWM in a theme and remembers it in the workspace, so a container made anew wears it:
#   kitty-theme ceres | ceres-pro | default | <any theme installed>
set -eu
: "${DISPLAY:=:99}"
name="${1:-}"
case "$name" in
'' | *[!A-Za-z0-9@._-]*)
    echo "usage: kitty-theme NAME (a folder of /usr/share/icewm/themes)" >&2
    exit 2
    ;;
esac
if [ ! -f "/usr/share/icewm/themes/$name/default.theme" ]; then
    echo "no theme called $name" >&2
    exit 2
fi
printf 'Theme="%s/default.theme"\n' "$name" >"$HOME/.icewm/theme"
mkdir -p "$HOME/work/.desktop" 2>/dev/null && echo "$name" >"$HOME/work/.desktop/theme" 2>/dev/null || true
# A running IceWM and its background take the theme at once.
if pgrep -x icewm >/dev/null 2>&1; then
    icewm --restart >/dev/null 2>&1 &
    icewmbg --restart >/dev/null 2>&1 || true
fi
echo "theme $name"
