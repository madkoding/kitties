#!/bin/sh
# Puts back what kitty-backup took, from a tar.gz on the standard input. A file that is already
# there stays: a workspace that kept its browser profile is newer than any backup of it.
#   kitty-restore < backup.tar.gz
#
# The backup carries the browser's profile as work/.browser. It is unpacked in memory (/dev/shm)
# and goes straight to where the profile lives (kitty-profile: the kitty's volume), and only when
# that profile is empty: it never lands in the workspace, a folder of the host, where the cookies
# and passwords of every update stayed behind before.
set -eu
home="${KITTY_HOME:-/home/kitty}"
base=/dev/shm
[ -d "$base" ] && [ -w "$base" ] || base=/tmp
tmp="$(mktemp -d "$base/kitty-restore-XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
chmod 700 "$tmp"
tar -xzf - -C "$tmp"
if [ -d "$tmp/work/.browser" ]; then
    profile="$(kitty-profile 2>/dev/null || echo "$home/work/.browser")"
    if [ ! -e "$profile/Default" ]; then
        mkdir -p "$profile"
        cp -a "$tmp/work/.browser/." "$profile/"
        chmod 700 "$profile" 2>/dev/null || true
    fi
    rm -rf "$tmp/work/.browser"
fi
cp -a --update=none "$tmp/." "$home/"
chmod 700 "$home/.ssh" 2>/dev/null || true
echo restored
