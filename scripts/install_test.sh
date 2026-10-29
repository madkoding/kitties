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
ask() { printf '%b' "$1" > "$work/answers"; KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/answers" sh scripts/install.sh --dry-run > "$work/ask.out" 2>&1 || true; }
ask '\n\n\ny\n'; grep -q "LISTEN=127.0.0.1:7488 PUBLIC_URL=$" "$work/ask.out" || fail "defaults are not local-only"
ask '9000\n3\ny\nhttps://k.example.com\ny\n'; grep -q "LISTEN=0.0.0.0:9000 PUBLIC_URL=https://k.example.com" "$work/ask.out" || fail "0.0.0.0 answers"
ask '\n3\nn\n1\n\ny\n'; grep -q "LISTEN=127.0.0.1:7488" "$work/ask.out" || fail "declining 0.0.0.0 must fall back"
ask '\n3\n'; grep -q "EVERY network interface" "$work/ask.out" || fail "no 0.0.0.0 warning"
ask '\n4\n10.0.0.5:8080\n\ny\n'; grep -q "LISTEN=10.0.0.5:8080" "$work/ask.out" || fail "custom address"
ask '\n\n\nn\n'; grep -q "cancelled" "$work/ask.out" || fail "no cancel on a refused confirmation"
# flags skip the question they answer, --yes asks nothing
KITTIES_LANG=en KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/nonexistent" sh scripts/install.sh --dry-run --yes --listen 192.168.1.5:7000 > "$work/ask.out" 2>&1; grep -q "LISTEN=192.168.1.5:7000" "$work/ask.out" || fail "--yes --listen"
KITTIES_LANG=es KITTIES_CONF_DIR="$work/none" KITTIES_TTY="$work/nonexistent" sh scripts/install.sh --dry-run > "$work/ask.out" 2>&1; grep -q "dry run" "$work/ask.out" || fail "no terminal: must not ask"

# the unit written by the installer is the shipped one (path and user substituted)
sed -n '/^cat > "\$UNIT.new" <<UNITEOF$/,/^UNITEOF$/p' scripts/install.sh | sed '1d;$d' \
    | sed -e 's#\$USER_NAME#kitties#g' -e 's#\$CONF_DIR#/etc/kitties#g' -e 's#\$PREFIX#/usr/local/bin#g' -e 's#\$DATA_DIR#/var/lib/kitties#g' | grep -v '^#' > "$work/unit"
grep -v '^#' deploy/kitties.service | diff - "$work/unit" >/dev/null || fail "install.sh unit differs from deploy/kitties.service"
echo "ok"
