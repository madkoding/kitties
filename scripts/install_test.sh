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

run() { KITTIES_BASE_URL="file://$rel" sh scripts/install.sh --no-service --prefix "$work/bin" "$@"; }

run --pubkey "$pub" | grep -q "signature ok" || fail "signed install"
[ "$("$work/bin/kitties" version)" = "kitties v9.9.9" ] || fail "binary not installed"
run --pubkey "$pub" | grep -q "already installed" || fail "second run is not a no-op"

other="$("$work/kitties" release-key | sed -n '2p')"
run --pubkey "$other" >/dev/null 2>&1 && fail "wrong key accepted"
run >"$work/out" 2>&1 || fail "install without a key"
grep -q "no --pubkey" "$work/out" || fail "no warning without a key"

echo tampered >> "$rel/kitties-linux-$a"
run --pubkey "$pub" >/dev/null 2>&1 && fail "tampered binary accepted"

# the unit written by the installer is the shipped one (path and user substituted)
sed -n '/^cat > "\$UNIT.new" <<UNITEOF$/,/^UNITEOF$/p' scripts/install.sh | sed '1d;$d' \
    | sed -e 's#\$USER_NAME#kitties#g' -e 's#\$CONF_DIR#/etc/kitties#g' -e 's#\$PREFIX#/usr/local/bin#g' -e 's#\$DATA_DIR#/var/lib/kitties#g' | grep -v '^#' > "$work/unit"
grep -v '^#' deploy/kitties.service | diff - "$work/unit" >/dev/null || fail "install.sh unit differs from deploy/kitties.service"
echo "ok"
