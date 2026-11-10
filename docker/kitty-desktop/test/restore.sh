#!/bin/sh
# Checks kitty-restore: the browser's profile in a backup goes to where the profile lives (the
# kitty's volume), only when that one is empty, and never into the workspace (a folder of the
# host); the rest goes into the home without overwriting what is there.
#
#   docker run --rm --entrypoint sh -v "$PWD/docker/kitty-desktop/test:/test:ro" kitty-desktop:ci /test/restore.sh
#   BIN=docker/kitty-desktop sh docker/kitty-desktop/test/restore.sh      (from the sources)
set -u
fail=0
bad() { echo "FAIL: $*"; fail=1; }
ok() { echo "ok: $*"; }
bin() { if [ -n "${BIN:-}" ]; then echo "$BIN/$1.sh"; else command -v "$1"; fi; }
restore="$(cd "$(dirname "$(bin kitty-restore)")" && pwd)/$(basename "$(bin kitty-restore)")"
profile="$(cd "$(dirname "$(bin kitty-profile)")" && pwd)/$(basename "$(bin kitty-profile)")"

t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
mkdir -p "$t/fakebin" "$t/src/work/.browser/Default" "$t/src/.ssh" "$t/home/work" "$t/home/.cache" "$t/home/.ssh"
printf '#!/bin/sh\nexec sh %s "$@"\n' "$profile" >"$t/fakebin/kitty-profile" && chmod +x "$t/fakebin/kitty-profile"
echo old-cookie >"$t/src/work/.browser/Default/Cookies"
echo key >"$t/src/.ssh/id_new"
echo backup >"$t/src/.ssh/id_kept"
echo mine >"$t/home/.ssh/id_kept"
tar -czf "$t/b.tgz" -C "$t/src" .
run() { env HOME="$t/home" KITTY_HOME="$t/home" KITTY_PROFILE="$t/volume" PATH="$t/fakebin:$PATH" sh "$restore" <"$t/b.tgz"; }

run >/dev/null || bad "restore failed"
[ "$(cat "$t/volume/Default/Cookies" 2>/dev/null)" = old-cookie ] && ok "an empty volume gets the backup's profile" || bad "volume not filled"
[ ! -e "$t/home/work/.browser" ] && ok "nothing of the browser in the workspace" || bad "the profile landed in the workspace"
[ "$(cat "$t/home/.ssh/id_new" 2>/dev/null)" = key ] && ok "the rest goes into the home" || bad "home not restored"
[ "$(cat "$t/home/.ssh/id_kept")" = mine ] && ok "a file there is kept" || bad "overwritten"

echo newer >"$t/volume/Default/Cookies"
run >/dev/null || bad "second restore failed"
[ "$(cat "$t/volume/Default/Cookies")" = newer ] && ok "a profile in use is never overwritten by a backup" || bad "volume overwritten"
[ ! -e "$t/home/work/.browser" ] && ok "and the workspace stays clean" || bad "the profile landed in the workspace"
[ -z "$(ls /dev/shm 2>/dev/null | grep kitty-restore)" ] && ok "nothing left in memory" || bad "temp left"
exit "$fail"
