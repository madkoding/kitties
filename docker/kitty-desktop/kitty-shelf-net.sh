#!/bin/sh
# The shelf's network island: the container has no wifi, so it reports the link it does have, in the
# "in-use:signal:name" form the shelf reads from `nmcli`. 100% when a default route is up, nothing
# (the glyph stays bare) when there is none.
dev=$(awk 'NR > 1 && $2 == "00000000" { print $1; exit }' ${KITTY_NET_ROUTE:-/proc/net/route} 2>/dev/null)
[ -n "$dev" ] || exit 0
state=$(cat "${KITTY_NET_SYS:-/sys/class/net}/$dev/operstate" 2>/dev/null || echo up)
case "$state" in
    down|lowerlayerdown|notpresent) exit 0 ;;
esac
printf '*:100:Kitty network (%s)\n' "$dev"
