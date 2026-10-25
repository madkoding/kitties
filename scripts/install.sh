#!/bin/sh
# Installs the Kitties gateway on a server: the release binary for this system, checked against the
# release's SHA256SUMS (and its Ed25519 signature), a `kitties` user, and a systemd service.
# Safe to run again: it updates the binary and the unit and keeps the configuration and the token.
#
#   curl -fsSL https://raw.githubusercontent.com/madkoding/kitties/main/scripts/install.sh | sudo sh
#   curl -fsSL .../install.sh | sudo sh -s -- --version v0.5.0 --public-url https://kitties.example.com
#
# Options (or the environment variable in brackets):
#   --version vX.Y.Z    release to install; default the latest        [KITTIES_VERSION]
#   --listen ADDR       address of the gateway; default 127.0.0.1:7488 [KITTIES_LISTEN]
#   --public-url URL    where the page is reached from (behind https)  [KITTIES_PUBLIC_URL]
#   --pubkey KEY        release signing key (base64); with it the signature is required [KITTIES_PUBKEY]
#   --no-service        install the binary only (no user, no systemd)
#   --prefix DIR        folder of the binary; default /usr/local/bin   [KITTIES_PREFIX]
#   -h, --help
# Testing: KITTIES_BASE_URL replaces the release download URL.
set -eu

REPO="${KITTIES_REPO:-madkoding/kitties}"
VERSION="${KITTIES_VERSION:-latest}"
LISTEN="${KITTIES_LISTEN:-127.0.0.1:7488}"
PUBLIC_URL="${KITTIES_PUBLIC_URL:-}"
PUBKEY="${KITTIES_PUBKEY:-}"
PREFIX="${KITTIES_PREFIX:-/usr/local/bin}"
BASE_URL="${KITTIES_BASE_URL:-}"
SERVICE=1
CONF_DIR="${KITTIES_CONF_DIR:-/etc/kitties}"
UNIT="${KITTIES_UNIT_DIR:-/etc/systemd/system}/kitties.service"
USER_NAME=kitties
DATA_DIR=/var/lib/kitties

say() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is needed and not installed"; }

while [ $# -gt 0 ]; do
    case "$1" in
        --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
        --listen) [ $# -ge 2 ] || die "--listen needs a value"; LISTEN="$2"; shift 2 ;;
        --public-url) [ $# -ge 2 ] || die "--public-url needs a value"; PUBLIC_URL="$2"; shift 2 ;;
        --pubkey) [ $# -ge 2 ] || die "--pubkey needs a value"; PUBKEY="$2"; shift 2 ;;
        --prefix) [ $# -ge 2 ] || die "--prefix needs a value"; PREFIX="$2"; shift 2 ;;
        --no-service) SERVICE=0; shift ;;
        -h|--help) sed -n '2,/^set -eu/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) die "unknown option: $1 (try --help)" ;;
    esac
done

# The service needs root; the binary alone needs only a writable --prefix.
[ "$SERVICE" -eq 0 ] || [ "$(id -u)" -eq 0 ] || die "run it as root (… | sudo sh), or use --no-service"
need curl
need uname
need sha256sum

# ---- which release file this system needs ----
os=$(uname -s | tr '[:upper:]' '[:lower:]')
[ "$os" = linux ] || die "this installer is for Linux servers; on $os see docs/INSTALL.md"
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    armv7l|armv6l|armhf|arm) arch=arm ;;
    *) die "no release for the architecture $(uname -m)" ;;
esac
asset="kitties-$os-$arch"

if [ -n "$BASE_URL" ]; then
    url="$BASE_URL"
elif [ "$VERSION" = latest ]; then
    url="https://github.com/$REPO/releases/latest/download"
else
    url="https://github.com/$REPO/releases/download/$VERSION"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# ---- download and verify ----
say "downloading $asset ($VERSION)"
curl -fsSL "$url/$asset" -o "$tmp/$asset" || die "could not download $url/$asset"
curl -fsSL "$url/SHA256SUMS" -o "$tmp/SHA256SUMS" || die "could not download $url/SHA256SUMS"

want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "SHA256SUMS does not list $asset"
got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
[ "$want" = "$got" ] || die "checksum mismatch for $asset (expected $want, got $got)"
say "checksum ok"

verify_signature() {
    # An Ed25519 public key is 32 raw bytes; wrapped in this fixed DER prefix it is what openssl reads.
    if command -v openssl >/dev/null 2>&1 && command -v base64 >/dev/null 2>&1; then
        { printf '\060\052\060\005\006\003\053\145\160\003\041\000'; printf '%s' "$PUBKEY" | base64 -d; } > "$tmp/key.der" 2>/dev/null || return 1
        base64 -d < "$tmp/SHA256SUMS.sig" > "$tmp/sig.bin" 2>/dev/null || return 1
        openssl pkeyutl -verify -pubin -inkey "$tmp/key.der" -keyform DER -rawin \
            -in "$tmp/SHA256SUMS" -sigfile "$tmp/sig.bin" >/dev/null 2>&1 && return 0
        return 1
    fi
    # No openssl: the binary's own check (it has been checked against the sums above).
    warn "openssl not found: checking the signature with the downloaded binary"
    chmod +x "$tmp/$asset"
    "$tmp/$asset" release-verify "$tmp/SHA256SUMS" "$tmp/SHA256SUMS.sig" "$PUBKEY" >/dev/null 2>&1
}

if curl -fsSL "$url/SHA256SUMS.sig" -o "$tmp/SHA256SUMS.sig" 2>/dev/null; then
    if [ -n "$PUBKEY" ]; then
        verify_signature || die "the signature of SHA256SUMS is not the release key's: not installing"
        say "signature ok"
    else
        warn "release is signed but no --pubkey was given: only the checksum was checked"
    fi
elif [ -n "$PUBKEY" ]; then
    die "a --pubkey was given but this release has no SHA256SUMS.sig"
else
    warn "this release is unsigned: only the checksum was checked"
fi

# ---- install the binary ----
chmod 0755 "$tmp/$asset"
new_version=$("$tmp/$asset" version 2>/dev/null | awk '{print $2}') || new_version=
[ -n "$new_version" ] || die "the downloaded binary does not run on this system"
old_version=
if [ -x "$PREFIX/kitties" ]; then
    old_version=$("$PREFIX/kitties" version 2>/dev/null | awk '{print $2}') || old_version=
fi

changed=0
if [ "$old_version" = "$new_version" ] && cmp -s "$tmp/$asset" "$PREFIX/kitties"; then
    say "kitties $new_version is already installed"
else
    mkdir -p "$PREFIX"
    install -m 0755 "$tmp/$asset" "$PREFIX/kitties.new"
    mv -f "$PREFIX/kitties.new" "$PREFIX/kitties"
    changed=1
    say "installed kitties ${old_version:+$old_version -> }$new_version in $PREFIX"
fi

if [ "$SERVICE" -eq 0 ]; then
    say "done (binary only). Run: kitties gateway"
    exit 0
fi

# ---- user, configuration, systemd ----
command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] || die "systemd not found; use --no-service and see docs/DEPLOY.md"

if ! id "$USER_NAME" >/dev/null 2>&1; then
    useradd --system --create-home --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$USER_NAME"
    say "created the user $USER_NAME"
fi
if getent group docker >/dev/null 2>&1; then
    usermod -aG docker "$USER_NAME"
else
    warn "no docker group: the kitties will run brain-only until Docker is installed (then run this again)"
fi

install -d -m 0750 -o "$USER_NAME" -g "$USER_NAME" "$CONF_DIR"
env_file="$CONF_DIR/kitties.env"
token=
if [ ! -f "$env_file" ]; then
    if command -v openssl >/dev/null 2>&1; then
        token=$(openssl rand -hex 24)
    else
        token=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
    fi
    umask 077
    {
        echo "# Read by the systemd unit. Keep it mode 0600."
        echo "KITTIES_LISTEN=$LISTEN"
        [ -z "$PUBLIC_URL" ] || echo "KITTIES_PUBLIC_URL=$PUBLIC_URL"
        echo "KITTIES_TOKEN=$token"
        echo "OPENAI_API_KEY="
        echo "ANTHROPIC_API_KEY="
        echo "GEMINI_API_KEY="
    } > "$env_file"
    chown "$USER_NAME:$USER_NAME" "$env_file"
    chmod 0600 "$env_file"
    say "wrote $env_file"
else
    say "kept $env_file"
fi

cat > "$UNIT.new" <<UNITEOF
[Unit]
Description=Kitties gateway
After=network-online.target docker.service
Wants=network-online.target

[Service]
User=$USER_NAME
Group=$USER_NAME
SupplementaryGroups=docker
EnvironmentFile=$CONF_DIR/kitties.env
ExecStart=$PREFIX/kitties gateway
Restart=on-failure
RestartSec=5

# What the gateway needs: its own state, and the network. Nothing else.
StateDirectory=kitties
Environment=KITTIES_DATA_DIR=$DATA_DIR
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
UNITEOF
if ! cmp -s "$UNIT.new" "$UNIT" 2>/dev/null; then
    mv -f "$UNIT.new" "$UNIT"
    systemctl daemon-reload
    changed=1
else
    rm -f "$UNIT.new"
fi

systemctl enable kitties >/dev/null 2>&1
if systemctl is-active --quiet kitties; then
    [ "$changed" -eq 0 ] || { systemctl restart kitties; say "restarted the service"; }
else
    systemctl start kitties
    say "started the service"
fi

echo
say "Kitties $new_version is running."
echo "    logs:    journalctl -u kitties -f"
echo "    config:  $env_file"
if [ -n "$token" ]; then
    echo "    token:   $token   (the password of the page; also in $env_file)"
fi
case "$LISTEN" in
    127.0.0.1:*|localhost:*)
        echo "    It listens on $LISTEN only. From your computer: ssh -L 7488:$LISTEN user@this-server,"
        echo "    then open http://127.0.0.1:7488, or put nginx/Caddy with https in front (docs/DEPLOY.md)." ;;
    *) echo "    Open http://<this-server>:${LISTEN##*:}. Put https in front before using it over the internet." ;;
esac
