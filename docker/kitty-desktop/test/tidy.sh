#!/bin/sh
# Checks kitty-tidy: logs past their cap are trimmed in place to their tail, small ones and other
# files are left, and --boot deletes what an earlier start left in /tmp but not the display's files.
#
#   docker run --rm --entrypoint sh -v "$PWD/docker/kitty-desktop/test:/test:ro" kitty-desktop:ci /test/tidy.sh
#   BIN=docker/kitty-desktop sh docker/kitty-desktop/test/tidy.sh      (from the sources)
set -u
fail=0
bad() { echo "FAIL: $*"; fail=1; }
ok() { echo "ok: $*"; }
bin() { if [ -n "${BIN:-}" ]; then echo "$BIN/$1.sh"; else command -v "$1"; fi; }

t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
mkdir -p "$t/tmp/.X11-unix" "$t/tmp/old-dir" "$t/cache"
head -c 20480 /dev/zero >"$t/tmp/voxl.log"
printf 'tail-end' >>"$t/tmp/voxl.log"
printf 'small' >"$t/tmp/picom.log"
head -c 20480 /dev/zero >"$t/cache/chromium-launch.log"
head -c 20480 /dev/zero >"$t/tmp/download.bin"
touch "$t/tmp/.X99-lock" "$t/tmp/old-file" "$t/tmp/new-file"
touch -d '10 days ago' "$t/tmp/old-file" "$t/tmp/old-dir" "$t/tmp/.X99-lock" "$t/tmp/.X11-unix"
run() { KITTY_TMP="$t/tmp" KITTY_CACHE="$t/cache" KITTY_LOG_MAX_KB=10 KITTY_LOG_KEEP_KB=2 sh "$(bin kitty-tidy)" "$@"; }

run
[ "$(wc -c <"$t/tmp/voxl.log")" -eq 2048 ] && [ "$(tail -c 8 "$t/tmp/voxl.log")" = tail-end ] && ok "a big log keeps its tail" || bad "voxl.log $(wc -c <"$t/tmp/voxl.log")"
[ "$(cat "$t/tmp/picom.log")" = small ] && ok "a small log is left" || bad "picom.log"
[ "$(wc -c <"$t/cache/chromium-launch.log")" -eq 2048 ] && ok "logs in ~/.cache too" || bad "cache log"
[ "$(wc -c <"$t/tmp/download.bin")" -eq 20480 ] && ok "other files are not logs" || bad "download.bin"
[ -e "$t/tmp/old-file" ] && ok "without --boot nothing is deleted" || bad "deleted without --boot"
[ ! -e "$t/tmp/voxl.log.tidy" ] && ok "no scratch file left" || bad "scratch left"

run --boot
[ ! -e "$t/tmp/old-file" ] && [ ! -e "$t/tmp/old-dir" ] && ok "--boot deletes what is old" || bad "old left"
[ -e "$t/tmp/new-file" ] && [ -e "$t/tmp/download.bin" ] && ok "--boot keeps what is recent" || bad "recent deleted"
[ -e "$t/tmp/.X99-lock" ] && [ -d "$t/tmp/.X11-unix" ] && ok "the display's files stay" || bad "display files deleted"
exit "$fail"
