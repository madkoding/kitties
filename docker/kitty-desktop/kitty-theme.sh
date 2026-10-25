#!/bin/sh
# Puts IceWM in a theme and remembers it in the workspace, so a container made anew wears it:
#   kitty-theme ceres | ceres-pro | luma | default | <any theme installed>
# kitty-theme --boot puts the desktop in the theme remembered in the workspace (the entrypoint).
set -eu
: "${DISPLAY:=:99}"
boot=0
if [ "${1:-}" = "--boot" ]; then
    boot=1
    name="$(head -n 1 "$HOME/work/.desktop/theme" 2>/dev/null | tr -cd 'A-Za-z0-9@._-' || true)"
    [ -n "$name" ] || exit 0
else
    name="${1:-}"
fi
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
# The luma theme brings its own type (Figtree) and icons (Lucide); every other one has Kitties'
# (Sansation, Haiku). The same swap in IceWM, GTK 2 and 3 and the desktop's icons and font.
if [ "$name" = "luma" ]; then
    font="Figtree"
    icons="Luma"
else
    font="Sansation"
    icons="Haiku"
fi
sed -i -e "s/IconThemes=\"[A-Za-z]*:/IconThemes=\"$icons:/" "$HOME/.icewm/preferences" 2>/dev/null || true
sed -i -e "s/Sansation\|Figtree/$font/g" "$HOME/.icewm/prefoverride" 2>/dev/null || true
sed -i -e "s/Sansation\|Figtree/$font/g" -e "s/^\(gtk-icon-theme-name=\"\?\)[A-Za-z]*/\1$icons/" \
    "$HOME/.config/gtk-3.0/settings.ini" "$HOME/.gtkrc-2.0" "$HOME/.config/pcmanfm/default/desktop-items-0.conf" 2>/dev/null || true
if [ "$boot" = 1 ]; then
    exit 0
fi
mkdir -p "$HOME/work/.desktop" 2>/dev/null && echo "$name" >"$HOME/work/.desktop/theme" 2>/dev/null || true
# A running IceWM and its background take the theme at once.
if pgrep -x icewm >/dev/null 2>&1; then
    icewm --restart >/dev/null 2>&1 &
    icewmbg --restart >/dev/null 2>&1 || true
fi
echo "theme $name"
