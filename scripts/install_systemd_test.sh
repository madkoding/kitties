#!/bin/sh
# scripts/install.sh for real: a fake signed release is installed as a systemd service on this
# machine (needs root and systemd, so it is for CI runners and throwaway VMs), the service answers,
# `kitties gateway start|stop|restart|status` drive it, and running the installer again keeps the token.
# ufw and caddy are stubs: what the installer asks of them is checked, not their effect.
#   sudo scripts/install_systemd_test.sh
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"
[ "$(id -u)" -eq 0 ] || { echo "needs root: sudo $0" >&2; exit 1; }
[ -d /run/systemd/system ] || { echo "skipped: no systemd here"; exit 0; }
work="$(mktemp -d)"
fail() { echo "FAIL: $*" >&2; journalctl -u kitties --no-pager -n 40 >&2 || true; exit 1; }
cleanup() {
    systemctl disable --now kitties >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/kitties.service /usr/local/bin/kitties
    systemctl daemon-reload || true
    userdel kitties >/dev/null 2>&1 || true
    rm -rf /etc/kitties /var/lib/kitties "$work"
}
trap cleanup EXIT

cd "$here"
go build -ldflags "-X main.version=v9.9.9" -o "$work/kitties" ./cmd/kitties
case "$(uname -m)" in x86_64) a=amd64 ;; aarch64) a=arm64 ;; *) a=arm ;; esac
rel="$work/rel"; mkdir -p "$rel"
cp "$work/kitties" "$rel/kitties-linux-$a"
(cd "$rel" && sha256sum kitties-* > SHA256SUMS)
keys="$("$work/kitties" release-key)"
pub="$(echo "$keys" | sed -n '2p')"
priv="$(echo "$keys" | sed -n '5p')"
KITTIES_SIGNING_KEY="$priv" "$work/kitties" release-sign "$rel/SHA256SUMS" >/dev/null

stub="$work/stub"; mkdir -p "$stub" "$work/caddy"
printf '#!/bin/sh\n[ "$1" = status ] && echo "Status: active"\n[ "$1" = allow ] && echo "$*" >> %s/ufw.log\nexit 0\n' "$work" > "$stub/ufw"
printf '#!/bin/sh\nexit 0\n' > "$stub/caddy"
chmod +x "$stub/ufw" "$stub/caddy"
printf 'import conf.d/*\n' > "$work/caddy/Caddyfile"

run_install() {
    PATH="$stub:$PATH" KITTIES_CADDY_DIR="$work/caddy" KITTIES_LANG=en KITTIES_BASE_URL="file://$rel" \
        sh scripts/install.sh --yes --pubkey "$pub" --listen 127.0.0.1:7499 \
        --public-url https://k.example.com --caddy --open-firewall
}
run_install > "$work/out" 2>&1 || { cat "$work/out" >&2; fail "installer"; }
grep -q "Kitties v9.9.9 is running" "$work/out" || fail "no summary"
systemctl is-active --quiet kitties || fail "service not active"
curl -fsS -o /dev/null http://127.0.0.1:7499/ || fail "the gateway does not answer"
[ "$(stat -c %a /etc/kitties/kitties.env)" = 600 ] || fail "kitties.env is not 0600"
token="$(sed -n 's/^KITTIES_TOKEN=//p' /etc/kitties/kitties.env)"
[ -n "$token" ] && grep -q "$token" "$work/out" || fail "token not shown"
grep -q "allow 7499/tcp" "$work/ufw.log" || fail "firewall not asked to open the port"
grep -q "k.example.com" "$work/caddy/conf.d/kitties.caddy" && grep -q "127.0.0.1:7499" "$work/caddy/conf.d/kitties.caddy" || fail "caddy site"

/usr/local/bin/kitties gateway stop >/dev/null || fail "gateway stop"
systemctl is-active --quiet kitties && fail "still active after stop"
/usr/local/bin/kitties gateway status >/dev/null 2>&1 && fail "status of a stopped service must fail"
/usr/local/bin/kitties gateway start >/dev/null || fail "gateway start"
sleep 1; systemctl is-active --quiet kitties || fail "not active after start"
/usr/local/bin/kitties gateway restart >/dev/null || fail "gateway restart"
sleep 1; /usr/local/bin/kitties gateway status >/dev/null || fail "status of a running service"

run_install > "$work/out2" 2>&1 || { cat "$work/out2" >&2; fail "second run"; }
grep -q "already installed" "$work/out2" || fail "second run is not a no-op"
[ "$(sed -n 's/^KITTIES_TOKEN=//p' /etc/kitties/kitties.env)" = "$token" ] || fail "the token changed"
systemctl is-active --quiet kitties || fail "not active after the second run"
echo "ok"
