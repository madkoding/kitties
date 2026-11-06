#!/bin/sh
# The kitty's home outlives its container: the gateway mounts a folder of the host at /home/kitty
# (its settings, ~/.local, /opt/kitty-apps) and another at ~/Shared. This seeds it from the
# image's own copy (/usr/share/kitties/skel) at every start:
#   - a file the home lacks is copied (a new home gets everything; an old one, what is new);
#   - the files the image keeps the look of the desktop in (skel.managed) are copied again
#     whenever the image changes, so an update brings its new menus, theme and dock;
#   - nothing else of the kitty's is touched.
#   kitty-home
set -eu
skel="${KITTY_SKEL:-/usr/share/kitties/skel}"
home="${HOME:-/home/kitty}"
[ -d "$skel" ] || exit 0
cp -a --update=none "$skel/." "$home/" 2>/dev/null || true
stamp="$home/.config/kitties/skel"
id="$(cat "$skel.id" 2>/dev/null || echo none)"
if [ "$(cat "$stamp" 2>/dev/null || true)" != "$id" ]; then
    if [ -f "$skel.managed" ]; then
        while IFS= read -r f; do
            case "$f" in '' | /* | *..*) continue ;; esac
            [ -e "$skel/$f" ] || continue
            mkdir -p "$(dirname "$home/$f")"
            if [ -d "$skel/$f" ]; then
                mkdir -p "$home/$f"
                cp -a "$skel/$f/." "$home/$f/"
            else
                cp -a "$skel/$f" "$home/$f"
            fi
        done <"$skel.managed"
    fi
    mkdir -p "$(dirname "$stamp")"
    echo "$id" >"$stamp"
fi
mkdir -p "$home/work" "$home/Shared" "$home/.local/bin" "$home/.local/opt"
