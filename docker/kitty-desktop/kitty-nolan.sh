#!/bin/sh
# Run as root through sudo, once, by the entrypoint of a desktop started with KITTY_NO_LAN=1 (the
# WhatsApp kitty that works on its own): its machine reaches the internet but not the person's
# local network, the host, or the cloud's metadata. Then the kitty's sudo is cut down to kitty-apt,
# so nothing it runs can take the rules away. Needs the container's NET_ADMIN capability.
set -eu
[ "$(id -u)" = 0 ] || { echo "kitty-nolan: run as root" >&2; exit 1; }

pick() {
    for b in "$@"; do
        if command -v "$b" >/dev/null 2>&1 && "$b" -S OUTPUT >/dev/null 2>&1; then
            echo "$b"
            return 0
        fi
    done
    return 1
}

# The state, where only root writes and the gateway reads it (shown in the kitty's drawer):
# "closed" once every rule is in, "failed" otherwise.
STATE_DIR=/var/lib/kitty-nolan
mkdir -p "$STATE_DIR" && chmod 755 "$STATE_DIR"
echo failed >"$STATE_DIR/state"
chmod 644 "$STATE_DIR/state"

# Without the rules the kitty keeps its sudo cut anyway: it never gets more than with them.
lock_sudo() {
    printf 'kitty ALL=(root) NOPASSWD: /usr/local/sbin/kitty-apt\n' >/etc/sudoers.d/kitty
    chmod 440 /etc/sudoers.d/kitty
}

# A rule that fails stops the script (set -e): the kitty's sudo is cut on the way out anyway.
trap 'lock_sudo' EXIT
v4="$(pick iptables iptables-legacy)" || { lock_sudo; echo "kitty-nolan: no iptables (is NET_ADMIN given?)" >&2; exit 1; }

# The name servers of /etc/resolv.conf stay reachable on port 53 (often the router itself).
dns="$(awk '$1 == "nameserver" { print $2 }' /etc/resolv.conf 2>/dev/null || true)"

$v4 -N KITTY-NOLAN 2>/dev/null || $v4 -F KITTY-NOLAN
$v4 -A KITTY-NOLAN -o lo -j ACCEPT
$v4 -A KITTY-NOLAN -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
for ns in $dns; do
    case "$ns" in *:*) continue ;; esac
    $v4 -A KITTY-NOLAN -d "$ns" -p udp --dport 53 -j ACCEPT
    $v4 -A KITTY-NOLAN -d "$ns" -p tcp --dport 53 -j ACCEPT
done
for net in 0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.168.0.0/16 \
    192.0.0.0/24 198.18.0.0/15 224.0.0.0/4 240.0.0.0/4; do
    $v4 -A KITTY-NOLAN -d "$net" -j REJECT
done
$v4 -C OUTPUT -j KITTY-NOLAN 2>/dev/null || $v4 -I OUTPUT 1 -j KITTY-NOLAN

if v6="$(pick ip6tables ip6tables-legacy)"; then
    $v6 -N KITTY-NOLAN 2>/dev/null || $v6 -F KITTY-NOLAN
    $v6 -A KITTY-NOLAN -o lo -j ACCEPT
    $v6 -A KITTY-NOLAN -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
    for ns in $dns; do
        case "$ns" in *:*) ;; *) continue ;; esac
        $v6 -A KITTY-NOLAN -d "$ns" -p udp --dport 53 -j ACCEPT
        $v6 -A KITTY-NOLAN -d "$ns" -p tcp --dport 53 -j ACCEPT
    done
    for net in ::1/128 fc00::/7 fe80::/10 ff00::/8 ::ffff:0:0/96; do
        $v6 -A KITTY-NOLAN -d "$net" -j REJECT
    done
    $v6 -C OUTPUT -j KITTY-NOLAN 2>/dev/null || $v6 -I OUTPUT 1 -j KITTY-NOLAN
fi

lock_sudo
echo closed >"$STATE_DIR/state"
echo "kitty-nolan: local network closed"
