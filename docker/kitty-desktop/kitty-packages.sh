#!/bin/sh
# Run as root by dpkg after every install (/etc/apt/apt.conf.d/99kitties): adds every Debian
# package the kitty asked for that the image did not come with to the list in its workspace, so
# a container made anew installs it again, however it was installed (kitty-install, apt, apt-get).
# It only adds: a package leaves the list with kitty-install --remove.
set -eu
base="${KITTY_BASE_PACKAGES:-/usr/share/kitties/base-packages}"
list="${KITTY_PACKAGES:-/home/kitty/work/.desktop/packages}"
[ -f "$base" ] || exit 0
[ -d "$(dirname "$(dirname "$list")")" ] || exit 0
mkdir -p "$(dirname "$list")"
touch "$list"
now="$(mktemp)"
trap 'rm -f "$now"' EXIT
apt-mark showmanual 2>/dev/null | sort -u >"$now"
sort -u "$base" | comm -13 - "$now" | while IFS= read -r p; do
    grep -qx -- "$p" "$list" || echo "$p" >>"$list"
done
chown kitty:kitty "$(dirname "$list")" "$list" 2>/dev/null || true
