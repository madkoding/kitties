#!/bin/sh
# Checks kitty-profile: the workspace keeps the profile when it can hold one; a workspace that
# cannot make symbolic links (a Windows or macOS folder through Docker Desktop, where Chromium
# cannot lock its profile) gives way to the local folder, which starts as a copy of what the
# workspace had; KITTY_PROFILE (the kitty's volume) wins and is filled the same way.
#
#   docker run --rm --entrypoint sh -v "$PWD/docker/kitty-desktop/test:/test:ro" kitty-desktop:ci /test/profile.sh
#   BIN=docker/kitty-desktop sh docker/kitty-desktop/test/profile.sh      (from the sources)
set -u
fail=0
bad() { echo "FAIL: $*"; fail=1; }
ok() { echo "ok: $*"; }
bin() { if [ -n "${BIN:-}" ]; then echo "$BIN/$1.sh"; else command -v "$1"; fi; }
prog="$(cd "$(dirname "$(bin kitty-profile)")" && pwd)/$(basename "$(bin kitty-profile)")"

t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
mkdir -p "$t/home/work" "$t/home/.cache" "$t/fakebin"
run() { env -u KITTY_PROFILE HOME="$t/home" KITTY_LOCAL_PROFILE="$t/local" "$@" sh "$prog"; }

p="$(run)"
[ "$p" = "$t/home/work/.browser" ] && [ -d "$p" ] && ok "a workspace that can hold it keeps it" || bad "workspace: $p"
[ -z "$(ls -A "$p")" ] && ok "no probe left behind" || bad "left: $(ls -A "$p")"

# A workspace with a profile, which cannot make links any more (the fake ln refuses all).
mkdir -p "$t/home/work/.browser/Default"
echo cookie >"$t/home/work/.browser/Default/Cookies"
ln -s x "$t/home/work/.browser/SingletonLock"
printf '#!/bin/sh\nexit 1\n' >"$t/fakebin/ln" && chmod +x "$t/fakebin/ln"
p="$(run PATH="$t/fakebin:$PATH")"
[ "$p" = "$t/local" ] && ok "no links: the local folder" || bad "no links: $p"
[ "$(cat "$t/local/Default/Cookies" 2>/dev/null)" = cookie ] && ok "it starts as a copy of the workspace's" || bad "not copied"
[ ! -L "$t/local/SingletonLock" ] && ok "without the old lock" || bad "lock copied"
[ -e "$t/home/work/.browser/Default/Cookies" ] && ok "the container's own folder is lost with it: the workspace's copy stays" || bad "the workspace's copy went without a volume"
rm -f "$t/home/work/.browser/SingletonLock"
grep -q "cannot hold the browser profile" "$t/home/.cache/chromium-launch.log" && ok "the move is logged" || bad "not logged"
[ "$(stat -c %a "$t/local")" = 700 ] && ok "the profile is the kitty's alone" || bad "mode $(stat -c %a "$t/local")"

# A profile already there is never overwritten by the workspace's.
echo newer >"$t/local/Default/Cookies"
run PATH="$t/fakebin:$PATH" >/dev/null
[ "$(cat "$t/local/Default/Cookies")" = newer ] && ok "a profile there is kept" || bad "overwritten"

# KITTY_PROFILE (the kitty's volume) wins, and a new one is filled from the workspace (whose
# copy then goes: the volume outlives the container).
p="$(env HOME="$t/home" KITTY_PROFILE="$t/volume" sh "$prog")"
[ "$p" = "$t/volume" ] && [ "$(cat "$t/volume/Default/Cookies" 2>/dev/null)" = cookie ] && ok "KITTY_PROFILE, filled from the workspace" || bad "volume: $p"
[ ! -e "$t/home/work/.browser" ] && ok "and the workspace's copy is gone" || bad "workspace copy kept"
# A leftover copy in the workspace (an older kitty-restore put one there on each update) goes too.
mkdir -p "$t/home/work/.browser/Default"
echo stale >"$t/home/work/.browser/Default/Cookies"
env HOME="$t/home" KITTY_PROFILE="$t/volume" sh "$prog" >/dev/null
[ ! -e "$t/home/work/.browser" ] && [ "$(cat "$t/volume/Default/Cookies")" = cookie ] && ok "a leftover copy goes, the volume stays" || bad "leftover kept or volume touched"
exit "$fail"
