#!/bin/sh
# Checks kitty-nolan: the machine of a kitty that works on its own reaches no local network (the
# rules are in, a private address is refused at once) and its sudo is cut down to kitty-apt.
#
#   docker run --rm --cap-add NET_ADMIN --user root --entrypoint sh \
#     -v "$PWD/docker/kitty-desktop/test:/test:ro" kitty-desktop:ci /test/nolan.sh
set -u
fail=0
bad() { echo "FAIL: $*"; fail=1; }
ok() { echo "ok: $*"; }

/usr/local/sbin/kitty-nolan || bad "kitty-nolan exited $?"
rules="$(iptables -S KITTY-NOLAN 2>/dev/null || iptables-legacy -S KITTY-NOLAN 2>/dev/null)"
for net in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 169.254.0.0/16; do
    echo "$rules" | grep -q -- "-d $net .*REJECT" && ok "$net is refused" || bad "no rule for $net"
done
# A refused address fails at once (REJECT), not after a timeout.
start=$(date +%s)
if curl -s -m 5 -o /dev/null http://192.168.1.1/; then bad "192.168.1.1 answered"; else ok "192.168.1.1 is not reached"; fi
[ $(($(date +%s) - start)) -lt 4 ] && ok "refused at once" || bad "it waited for a timeout"
grep -qx 'kitty ALL=(root) NOPASSWD: /usr/local/sbin/kitty-apt' /etc/sudoers.d/kitty && ok "sudo is only kitty-apt" || bad "sudoers: $(cat /etc/sudoers.d/kitty)"
# Twice is harmless (a restart runs it again on the same container).
/usr/local/sbin/kitty-nolan >/dev/null || bad "a second run failed"
n="$(iptables -S OUTPUT 2>/dev/null | grep -c KITTY-NOLAN || true)"
[ "${n:-0}" -le 1 ] && ok "the chain is hooked once" || bad "hooked $n times"
exit $fail
