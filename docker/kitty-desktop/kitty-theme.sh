#!/bin/sh
# Puts Openbox in a theme and remembers it in the workspace, so a container made anew wears it:
#   kitty-theme ceres | ceres-pro | <any theme installed>
set -eu
: "${DISPLAY:=:99}"
name="${1:-}"
case "$name" in
'' | *[!A-Za-z0-9@._-]*)
    echo "usage: kitty-theme NAME (a folder of /usr/share/themes with an openbox-3 theme)" >&2
    exit 2
    ;;
esac
if [ ! -f "/usr/share/themes/$name/openbox-3/themerc" ]; then
    echo "no theme called $name" >&2
    exit 2
fi
sed -i "s|<name>[A-Za-z0-9@._-]*</name>\\(<!--theme-->\\)|<name>$name</name>\\1|" "$HOME/.config/openbox/rc.xml"
mkdir -p "$HOME/work/.desktop" 2>/dev/null && echo "$name" >"$HOME/work/.desktop/theme" 2>/dev/null || true
# A running Openbox takes the theme at once.
if pgrep -x openbox >/dev/null 2>&1; then
    openbox --reconfigure >/dev/null 2>&1 || true
fi
echo "theme $name"
