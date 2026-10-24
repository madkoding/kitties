#!/bin/sh
# Kitties updating itself, with real binaries and a fake GitHub: a signed release is installed and
# restarted into (same process), one whose signature is wrong is refused, and a version that is
# restarted before it holds is gone back from and not offered again.
#   scripts/selfupdate_test.sh
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
GH=8791
GW=7599
pids=""
cleanup() {
    for p in $pids; do kill "$p" 2>/dev/null || true; done
    pkill -f "$work/rel/kitties" 2>/dev/null || true
    rm -rf "$work"
}
trap cleanup EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
api() { curl -s -H "Authorization: Bearer tok" "$@"; }
version() { curl -s "http://127.0.0.1:$GW/v1/health" | python3 -c 'import sys,json; print(json.load(sys.stdin)["version"])'; }
wait_version() { # wait_version vX.Y.Z
    for _ in $(seq 1 50); do
        [ "$(version 2>/dev/null || true)" = "$1" ] && return 0
        sleep 0.3
    done
    fail "the gateway did not come up as $1 (it is $(version 2>/dev/null || echo down))"
}

mkdir -p "$work/rel" "$work/gh" "$work/data"
cd "$here"
go build -o "$work/kitties-tool" ./cmd/kitties
keys="$("$work/kitties-tool" release-key)"
pub="$(echo "$keys" | awk 'length($0)==44 {print; exit}')"
priv="$(echo "$keys" | awk 'length($0)==88 {print; exit}')"
[ -n "$pub" ] && [ -n "$priv" ] || fail "release-key printed no keys"
build() { go build -ldflags "-X main.version=$1 -X 'github.com/madkoding/kitties/internal/selfupdate.builtinKey=$pub'" -o "$2" ./cmd/kitties; }
build v1.0.0 "$work/rel/kitties"
build v1.1.0 "$work/gh/kitties-linux-amd64"
(cd "$work/gh" && sha256sum kitties-linux-amd64 > SHA256SUMS)
KITTIES_SIGNING_KEY="$priv" "$work/kitties-tool" release-sign "$work/gh/SHA256SUMS" >/dev/null
"$work/kitties-tool" release-verify "$work/gh/SHA256SUMS" "$work/gh/SHA256SUMS.sig" "$pub" >/dev/null

python3 "$here/scripts/fake_github.py" 8791 "$work/gh" v1.1.0 &
pids="$pids $!"
cat >"$work/kitties.yaml" <<YAML
data_dir: $work/data
providers:
  p:
    kind: ollama
    model: m
default_provider: p
containers:
  auto_update: false
gateway:
  listen: 127.0.0.1:$GW
  token: tok
updates:
  gateway: notify
  api: http://127.0.0.1:$GH
YAML
start() { (cd "$work" && "$work/rel/kitties" -config kitties.yaml gateway >>"$work/gw.log" 2>&1 &) ; sleep 0.5; }
sleep 1

echo "== a release with a wrong signature is refused"
cp "$work/gh/SHA256SUMS.sig" "$work/good.sig"
python3 -c 'import base64,os; open("'"$work"'/gh/SHA256SUMS.sig","w").write(base64.b64encode(os.urandom(64)).decode()+"\n")'
start
wait_version v1.0.0
api "http://127.0.0.1:$GW/v1/update?refresh=1" | grep -q '"available":true' || fail "the new version is not seen"
api -X POST "http://127.0.0.1:$GW/v1/update/apply" | grep -q "not signed by the release key" || fail "a bad signature was not refused"
[ "$(version)" = "v1.0.0" ] || fail "the version changed after a refused update"
[ ! -e "$work/rel/kitties.prev" ] || fail "a refused update left a .prev"

echo "== a signed release is installed and restarted into, in the same process"
cp "$work/good.sig" "$work/gh/SHA256SUMS.sig"
pid="$(pgrep -f "$work/rel/kitties" | head -1)"
api -X POST "http://127.0.0.1:$GW/v1/update/apply" | grep -q '"restarting":true' || fail "the update was not accepted"
wait_version v1.1.0
[ "$(pgrep -f "$work/rel/kitties" | head -1)" = "$pid" ] || fail "the process changed: it did not restart in place"
"$work/rel/kitties.prev" version | grep -q v1.0.0 || fail "the old version was not kept"
[ -e "$work/rel/kitties.update" ] || fail "no marker of an update to confirm"

echo "== a version restarted before it holds is gone back from, and not offered again"
pkill -9 -f "$work/rel/kitties"
sleep 0.5
start
wait_version v1.0.0
"$work/rel/kitties" version | grep -q v1.0.0 || fail "the binary was not put back"
[ -e "$work/rel/kitties.failed" ] || fail "the failed version was not kept"
[ ! -e "$work/rel/kitties.update" ] || fail "the marker stays after a rollback"
api "http://127.0.0.1:$GW/v1/update?refresh=1" | grep -q '"skipped":"v1.1.0"' || fail "the failed version is offered again"
api "http://127.0.0.1:$GW/v1/update?refresh=1" | grep -q '"available":false' || fail "the failed version is available"
echo "OK: refused a bad signature, updated in place, went back from a version that did not hold"
