#!/bin/sh
# Installs programs from Debian and remembers them, so a container made anew installs them
# again: kitty-install gimp vlc. (A plain `sudo apt install` is remembered too.)
#   kitty-install --remove gimp     uninstalls it and forgets it
set -eu
remove=""
if [ "${1:-}" = "--remove" ]; then
    remove=1
    shift
fi
if [ "$#" -eq 0 ]; then
    echo "usage: kitty-install [--remove] package..." >&2
    exit 2
fi
list="$HOME/work/.desktop/packages"
mkdir -p "$(dirname "$list")"
touch "$list"
if [ -n "$remove" ]; then
    sudo -n /usr/local/sbin/kitty-apt --remove "$@"
    for p in "$@"; do
        grep -vx -- "$p" "$list" >"$list.new" || true
        mv "$list.new" "$list"
    done
    echo "removed $*"
    exit 0
fi
sudo -n /usr/local/sbin/kitty-apt "$@"
for p in "$@"; do
    grep -qx -- "$p" "$list" || echo "$p" >>"$list"
done
echo "installed $*"
