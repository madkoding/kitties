#!/bin/sh
# Checks what outlives a kitty's container: kitty-home seeds a home of the host and renews only
# the desktop's own files when the image changes, kitty-share copies into ~/Shared within its
# quota, kitty-import puts an export (or an older home) back, and kitty-packages remembers the
# Debian packages installed beyond the image's (with a stand-in apt-mark).
#
#   docker run --rm --entrypoint sh -v "$PWD/docker/kitty-desktop/test:/test:ro" kitty-desktop:ci /test/home.sh
#   BIN=docker/kitty-desktop sh docker/kitty-desktop/test/home.sh      (from the sources)
set -u
fail=0
bad() { echo "FAIL: $*"; fail=1; }
ok() { echo "ok: $*"; }
bin() { if [ -n "${BIN:-}" ]; then echo "$BIN/$1.sh"; else command -v "$1"; fi; }

t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT

# --- kitty-home ------------------------------------------------------------------------------
skel="$t/skel"
mkdir -p "$skel/.config/lumen" "$skel/.config/other"
echo menu-v1 >"$skel/.config/lumen/menu.xml"
echo theirs-v1 >"$skel/.config/other/conf"
echo bashrc-v1 >"$skel/.bashrc"
printf '.config/lumen\n../evil\n' >"$skel.managed"
echo id1 >"$skel.id"
home="$t/home"
mkdir -p "$home"
HOME="$home" KITTY_SKEL="$skel" sh "$(bin kitty-home)"
[ "$(cat "$home/.config/lumen/menu.xml")" = menu-v1 ] && [ -f "$home/.bashrc" ] && ok "an empty home is seeded" || bad "seed"
[ -d "$home/Shared" ] && [ -d "$home/.local/opt" ] && [ -d "$home/work" ] && ok "Shared, work and ~/.local/opt exist" || bad "folders"
# The kitty changes its files; the image changes.
echo mine >"$home/.bashrc"
echo mine >"$home/.config/other/conf"
echo mine >"$home/.config/lumen/menu.xml"
echo own >"$home/.config/lumen/extra"
HOME="$home" KITTY_SKEL="$skel" sh "$(bin kitty-home)"
[ "$(cat "$home/.config/lumen/menu.xml")" = mine ] && ok "the same image keeps every change" || bad "same image overwrote"
echo menu-v2 >"$skel/.config/lumen/menu.xml"
echo bashrc-v2 >"$skel/.bashrc"
echo new >"$skel/.new"
echo id2 >"$skel.id"
HOME="$home" KITTY_SKEL="$skel" sh "$(bin kitty-home)"
[ "$(cat "$home/.config/lumen/menu.xml")" = menu-v2 ] && ok "a new image renews the desktop's own files" || bad "managed not renewed"
[ "$(cat "$home/.bashrc")" = mine ] && [ "$(cat "$home/.config/other/conf")" = mine ] && ok "the kitty's own files stay" || bad "own files lost"
[ -f "$home/.config/lumen/extra" ] && [ -f "$home/.new" ] && ok "new files arrive, extra ones stay" || bad "merge"
[ ! -e "$t/evil" ] && ok "a managed path cannot leave the home" || bad "escape"
HOME="$home" KITTY_SKEL="$t/none" sh "$(bin kitty-home)" && ok "no skel: nothing to do" || bad "no skel"

# --- kitty-share -----------------------------------------------------------------------------
echo report >"$t/report.md"
out="$(KITTY_SHARED="$home/Shared" sh "$(bin kitty-share)" "$t/report.md")"
[ -f "$home/Shared/report.md" ] && [ "$out" = "shared report.md" ] && ok "kitty-share copies" || bad "share: $out"
KITTY_SHARED="$home/Shared" sh "$(bin kitty-share)" -d drafts "$t/report.md" >/dev/null
[ -f "$home/Shared/drafts/report.md" ] && ok "kitty-share -d" || bad "share -d"
KITTY_SHARED="$home/Shared" sh "$(bin kitty-share)" -d ../x "$t/report.md" 2>/dev/null && bad "a folder outside" || ok "a folder outside is refused"
KITTY_SHARED="$home/Shared" sh "$(bin kitty-share)" "$t/nope" 2>/dev/null && bad "missing file" || ok "a missing file is refused"
KITTY_SHARED="$home/Shared" sh "$(bin kitty-share)" 2>/dev/null && bad "no files" || ok "no files: usage"
head -c 2097152 /dev/zero >"$t/big"
KITTY_SHARED="$home/Shared" KITTY_SHARED_QUOTA_MB=1 sh "$(bin kitty-share)" "$t/big" 2>/dev/null && bad "over quota" || ok "over the quota is refused"
list="$(KITTY_SHARED="$home/Shared" KITTY_SHARED_QUOTA_MB=5 sh "$(bin kitty-share)" --list)"
echo "$list" | grep -qx report.md && echo "$list" | grep -q "of 5 MB" && ok "kitty-share --list" || bad "list: $list"

# --- kitty-import (export format), with the home at /home/kitty only inside the image ---------
if [ -z "${BIN:-}" ]; then
    mkdir -p "$t/exp/home/.config/app" "$t/exp/work" "$t/exp/shared"
    echo cfg >"$t/exp/home/.config/app/x"
    echo w >"$t/exp/work/notes"
    echo s >"$t/exp/shared/pic.txt"
    tar -czf "$t/exp.tgz" -C "$t/exp" home work shared
    kitty-import <"$t/exp.tgz" >/dev/null
    [ -f /home/kitty/.config/app/x ] && [ -f /home/kitty/work/notes ] && [ -f /home/kitty/Shared/pic.txt ] && ok "kitty-import" || bad "import"
    mkdir -p "$t/old/.oldapp"
    echo o >"$t/old/.oldapp/c"
    tar -cf "$t/old.tar" -C "$t/old" .
    kitty-import --home <"$t/old.tar" >/dev/null
    [ -f /home/kitty/.oldapp/c ] && ok "kitty-import --home" || bad "import --home"
fi

# --- kitty-packages --------------------------------------------------------------------------
mkdir -p "$t/bin" "$t/kh/work"
printf '#!/bin/sh\nprintf "xterm\\ngimp\\nvlc\\n"\n' >"$t/bin/apt-mark"
chmod +x "$t/bin/apt-mark"
printf 'xterm\n' >"$t/base"
PATH="$t/bin:$PATH" KITTY_BASE_PACKAGES="$t/base" KITTY_PACKAGES="$t/kh/work/.desktop/packages" sh "$(bin kitty-packages)"
PATH="$t/bin:$PATH" KITTY_BASE_PACKAGES="$t/base" KITTY_PACKAGES="$t/kh/work/.desktop/packages" sh "$(bin kitty-packages)"
[ "$(cat "$t/kh/work/.desktop/packages" | tr '\n' ' ')" = "gimp vlc " ] && ok "kitty-packages remembers what the image lacks, once" || bad "packages: $(cat "$t/kh/work/.desktop/packages")"
PATH="$t/bin:$PATH" KITTY_BASE_PACKAGES="$t/nobase" KITTY_PACKAGES="$t/x/y/packages" sh "$(bin kitty-packages)" && [ ! -e "$t/x" ] && ok "no base list: nothing" || bad "no base"

exit "$fail"
