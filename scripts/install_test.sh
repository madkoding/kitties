#!/bin/sh
# scripts/install.sh against a fake release (files, no network): a signed release is installed,
# running it again changes nothing, a tampered binary or a wrong key is refused, and the unit it
# writes is the one in deploy/kitties.service.
#   scripts/install_test.sh
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

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

run() { KITTIES_LANG=en KITTIES_BASE_URL="file://$rel" sh scripts/install.sh --no-service --prefix "$work/bin" "$@"; }

run --pubkey "$pub" > "$work/out" 2>&1; grep -q "signature ok" "$work/out" || fail "signed install"
[ "$("$work/bin/kitties" version)" = "kitties v9.9.9" ] || fail "binary not installed"
run --pubkey "$pub" > "$work/out" 2>&1; grep -q "already installed" "$work/out" || fail "second run is not a no-op"

other="$("$work/kitties" release-key | sed -n '2p')"
run --pubkey "$other" >/dev/null 2>&1 && fail "wrong key accepted"
run >"$work/out" 2>&1 || fail "install without a key"
grep -q "no --pubkey" "$work/out" || fail "no warning without a key"

echo tampered >> "$rel/kitties-linux-$a"
run --pubkey "$pub" >/dev/null 2>&1 && fail "tampered binary accepted"

# questions: answers come from a file (KITTIES_TTY); nothing is installed (--dry-run)
mkdir -p "$work/stub" "$work/caddy"
printf '#!/bin/sh\n[ "$1" = status ] && echo "Status: ${FAKE_UFW:-inactive}"\nexit 0\n' > "$work/stub/ufw"
printf '#!/bin/sh\nexit 0\n' > "$work/stub/caddy"
chmod +x "$work/stub/ufw" "$work/stub/caddy"
ask() { printf '%b' "$1" > "$work/answers"; PATH="$work/stub:$PATH" KITTIES_CADDY_DIR="$work/caddy" KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/answers" sh scripts/install.sh --dry-run --lang "${LANG_T:-en}" > "$work/ask.out" 2>&1 || true; }
ask '\n\n\ny\n'; grep -q "LISTEN=127.0.0.1:7488 PUBLIC_URL= " "$work/ask.out" || fail "defaults are not local-only"
ask '9000\n3\ny\nhttps://k.example.com\ny\n'; grep -q "LISTEN=0.0.0.0:9000 PUBLIC_URL=https://k.example.com" "$work/ask.out" || fail "0.0.0.0 answers"
ask '\n3\nn\n1\n\ny\n'; grep -q "LISTEN=127.0.0.1:7488" "$work/ask.out" || fail "declining 0.0.0.0 must fall back"
ask '\n3\n'; grep -q "EVERY network interface" "$work/ask.out" || fail "no 0.0.0.0 warning"
ask '\n4\n\ny\n'; grep -q "choose 1, 2 or 3" "$work/ask.out" || fail "there are only three options"
ask '\n\n\n\nn\n'; grep -q "cancelled" "$work/ask.out" || fail "no cancel on a refused confirmation"
# firewall: only asked when ufw is active and the gateway leaves the machine; 0.0.0.0 defaults to no
export FAKE_UFW=active
ask '\n3\ny\n\ny\n4\ny\n'; grep -q "FIREWALL=1" "$work/ask.out" || fail "firewall answer"
ask '\n3\ny\n\n\n4\ny\n'; grep -q "FIREWALL=0" "$work/ask.out" || fail "0.0.0.0 must default to not opening the firewall"
ask '\n\n\n4\ny\n'; grep -q "FIREWALL=0" "$work/ask.out" && ! grep -q "ufw is active" "$work/ask.out" || fail "loopback must not ask about the firewall"
FAKE_UFW=inactive
ask '\n3\ny\n\n4\ny\n'; ! grep -q "ufw is active" "$work/ask.out" || fail "asked about an inactive firewall"
# https with Caddy: asked only when Caddy imports conf.d; the key is hidden and never printed
ask '\n\nhttps://k.example.com\n4\ny\n'; ! grep -q "Caddy is installed" "$work/ask.out" || fail "Caddy asked without conf.d import"
printf 'import conf.d/*\n' > "$work/caddy/Caddyfile"
ask '\n\nhttps://k.example.com\ny\n1\nsk-secret123\ny\n'
grep -q "CADDY=1 KEY=ANTHROPIC_API_KEY" "$work/ask.out" || fail "caddy and key answers"
! grep -q "sk-secret123" "$work/ask.out" || fail "the key was printed"
ask '\n\n\n1\nbad key"\ny\n'; grep -q "KEY=$" "$work/ask.out" || fail "a key with spaces/quotes must be refused"
LANG_T=es ask '\n\n\n4\ny\n'; grep -q "Instalador de Kitties" "$work/ask.out" || fail "Spanish messages"
KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" sh scripts/install.sh --dry-run --yes --open-firewall --caddy --public-url https://k.example.com > "$work/ask.out" 2>&1; grep -q "FIREWALL=1 CADDY=1" "$work/ask.out" || fail "--open-firewall --caddy"
# flags skip the question they answer, --yes asks nothing
KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/nonexistent" sh scripts/install.sh --dry-run --yes --listen 192.168.1.5:7000 > "$work/ask.out" 2>&1; grep -q "LISTEN=192.168.1.5:7000" "$work/ask.out" || fail "--yes --listen"
for m in "local 127.0.0.1:7000" "wan 0.0.0.0:7000"; do
    KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/nonexistent" sh scripts/install.sh --dry-run --yes --listen 127.0.0.1:7000 --expose "${m%% *}" > "$work/ask.out" 2>&1; grep -q "LISTEN=${m##* }" "$work/ask.out" || fail "--expose ${m%% *}"
done
grep -q "EVERY network interface" "$work/ask.out" || fail "--expose wan must warn"
KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/nonexistent" sh scripts/install.sh --dry-run --yes --expose moon > "$work/ask.out" 2>&1 && fail "--expose moon must fail"
KITTIES_LANG=es KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/nonexistent" sh scripts/install.sh --dry-run > "$work/ask.out" 2>&1; grep -q "dry run" "$work/ask.out" || fail "no terminal: must not ask"

# the unit written by the installer is the shipped one (path and user substituted)
sed -n '/^cat > "\$UNIT.new" <<UNITEOF$/,/^UNITEOF$/p' scripts/install.sh | sed '1d;$d' \
    | sed -e 's#\$USER_NAME#kitties#g' -e 's#\$CONF_DIR#/etc/kitties#g' -e 's#\$PREFIX#/usr/local/bin#g' -e 's#\$DATA_DIR#/var/lib/kitties#g' | grep -v '^#' > "$work/unit"
grep -v '^#' deploy/kitties.service | diff - "$work/unit" >/dev/null || fail "install.sh unit differs from deploy/kitties.service"
echo "ok"
