#!/bin/sh
# Installs the Kitties gateway on a server: the release binary for this system, checked against the
# release's SHA256SUMS (and its Ed25519 signature), a `kitties` user, and a systemd service.
# Safe to run again: it updates the binary and the unit and keeps the configuration and the token.
#
# Run from a terminal it explains each step and asks where to expose the gateway (only this machine,
# the local network, or every interface) and what it needs; with flags, env vars, --yes or no
# terminal it asks nothing and uses the defaults.
#
#   curl -fsSL https://raw.githubusercontent.com/madkoding/kitties/main/scripts/install.sh | sudo sh
#   curl -fsSL .../install.sh | sudo sh -s -- --yes --listen 0.0.0.0:7488
#   curl -fsSL .../install.sh | sudo sh -s -- --version v0.5.0 --public-url https://kitties.example.com
#
# Options (or the environment variable in brackets):
#   --version vX.Y.Z    release to install; default the latest        [KITTIES_VERSION]
#   --listen ADDR       address of the gateway; default 127.0.0.1:7488 [KITTIES_LISTEN]
#   --public-url URL    where the page is reached from (behind https)  [KITTIES_PUBLIC_URL]
#   --pubkey KEY        release signing key (base64); with it the signature is required [KITTIES_PUBKEY]
#   --no-service        install the binary only (no user, no systemd)
#   --prefix DIR        folder of the binary; default /usr/local/bin   [KITTIES_PREFIX]
#   -y, --yes           ask nothing: use the defaults and the options given [KITTIES_YES=1]
#   --lang en|es        language of the messages; default from $LANG   [KITTIES_LANG]
#   --dry-run           ask the questions, print the choices and stop
#   -h, --help
# Testing: KITTIES_BASE_URL replaces the release download URL, KITTIES_TTY the file the answers are read from.
set -eu

REPO="${KITTIES_REPO:-madkoding/kitties}"
VERSION="${KITTIES_VERSION:-latest}"
LISTEN="${KITTIES_LISTEN:-127.0.0.1:7488}"
LISTEN_GIVEN=0; [ -z "${KITTIES_LISTEN:-}" ] || LISTEN_GIVEN=1
PUBLIC_URL="${KITTIES_PUBLIC_URL:-}"
PUBLIC_URL_GIVEN=0; [ -z "${KITTIES_PUBLIC_URL:-}" ] || PUBLIC_URL_GIVEN=1
PUBKEY="${KITTIES_PUBKEY:-}"
PREFIX="${KITTIES_PREFIX:-/usr/local/bin}"
BASE_URL="${KITTIES_BASE_URL:-}"
ASSUME_YES=0; [ -z "${KITTIES_YES:-}" ] || [ "${KITTIES_YES:-}" = 0 ] || ASSUME_YES=1
LANG_UI="${KITTIES_LANG:-}"
SERVICE=1
DRY_RUN=0
TTY="${KITTIES_TTY:-/dev/tty}"
CONF_DIR="${KITTIES_CONF_DIR:-/etc/kitties}"
UNIT="${KITTIES_UNIT_DIR:-/etc/systemd/system}/kitties.service"
USER_NAME=kitties
DATA_DIR=/var/lib/kitties
DEFAULT_PORT=7488

# L "english" "español": the message in the language of the user.
L() { if [ "$LANG_UI" = es ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
say() { printf '==> %s\n' "$*"; }
step() { printf '\n[%s/%s] %s\n' "$1" "$TOTAL_STEPS" "$2"; }
note() { printf '    %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 $(L 'is needed and not installed' 'es necesario y no está instalado')"; }

while [ $# -gt 0 ]; do
    case "$1" in
        --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
        --listen) [ $# -ge 2 ] || die "--listen needs a value"; LISTEN="$2"; LISTEN_GIVEN=1; shift 2 ;;
        --public-url) [ $# -ge 2 ] || die "--public-url needs a value"; PUBLIC_URL="$2"; PUBLIC_URL_GIVEN=1; shift 2 ;;
        --pubkey) [ $# -ge 2 ] || die "--pubkey needs a value"; PUBKEY="$2"; shift 2 ;;
        --prefix) [ $# -ge 2 ] || die "--prefix needs a value"; PREFIX="$2"; shift 2 ;;
        --lang) [ $# -ge 2 ] || die "--lang needs a value"; LANG_UI="$2"; shift 2 ;;
        -y|--yes) ASSUME_YES=1; shift ;;
        --no-service) SERVICE=0; shift ;;
        --dry-run) DRY_RUN=1; shift ;;
        -h|--help) sed -n '2,/^set -eu/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) die "unknown option: $1 (try --help)" ;;
    esac
done

if [ -z "$LANG_UI" ]; then
    case "${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}" in es*) LANG_UI=es ;; *) LANG_UI=en ;; esac
fi
[ "$LANG_UI" = es ] || LANG_UI=en

# ---- asking: only from a terminal (also under `curl | sh`, where stdin is the script) ----
NO_ANSWER=0
INTERACTIVE=0
if [ "$ASSUME_YES" -eq 0 ] && ( : <"$TTY" ) 2>/dev/null; then INTERACTIVE=1; exec 3<"$TTY"; fi

# ask "prompt" -> $REPLY_TEXT (empty on Enter or when the terminal goes away)
ask() {
    printf '    %s' "$1" >&2
    REPLY_TEXT=
    IFS= read -r REPLY_TEXT <&3 || { REPLY_TEXT=; NO_ANSWER=1; }
}
# confirm "prompt" default(y|n)
confirm() {
    if [ "$2" = y ]; then hint='[Y/n]'; else hint='[y/N]'; fi
    ask "$1 $hint "
    case "$REPLY_TEXT" in
        [yYsS]*) return 0 ;;
        [nN]*) return 1 ;;
        *) [ "$2" = y ] ;;
    esac
}
valid_port() { case "$1" in ''|*[!0-9]*) return 1 ;; esac; [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
# the first private IPv4 address of this machine (what the LAN sees)
lan_ip() {
    ip=
    if command -v hostname >/dev/null 2>&1; then
        for a in $(hostname -I 2>/dev/null); do
            case "$a" in *:*) continue ;; esac
            ip="$a"; break
        done
    fi
    if [ -z "$ip" ] && command -v ip >/dev/null 2>&1; then
        ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit }}')
    fi
    printf '%s' "$ip"
}

ask_listen() {
    port="$DEFAULT_PORT"
    ask "$(L "Port [$DEFAULT_PORT]: " "Puerto [$DEFAULT_PORT]: ")"
    if [ -n "$REPLY_TEXT" ]; then
        if valid_port "$REPLY_TEXT"; then port="$REPLY_TEXT"; else warn "$(L "not a valid port, using $DEFAULT_PORT" "puerto no válido, uso $DEFAULT_PORT")"; fi
    fi
    ip=$(lan_ip)
    while :; do
        [ "$NO_ANSWER" -eq 0 ] || die "$(L 'no answer from the terminal, nothing was changed' 'sin respuesta de la terminal, no se cambió nada')"
        echo
        note "$(L 'Where should the gateway be reachable from?' '¿Desde dónde se podrá llegar al gateway?')"
        note "  1) $(L "Only this machine (127.0.0.1:$port)   - safest, the default" "Solo esta máquina (127.0.0.1:$port)   - lo más seguro, por defecto")"
        note "  2) $(L "Local network (${ip:-your LAN IP}:$port)   - other devices at home/office" "Red local (${ip:-IP de tu LAN}:$port)   - otros equipos de tu casa/oficina")"
        note "  3) $(L "Every interface (0.0.0.0:$port)   - also the internet if the server is public" "Todas las interfaces (0.0.0.0:$port)   - también internet si el servidor es público")"
        note "  4) $(L 'Another address' 'Otra dirección')"
        ask "$(L 'Choose 1-4 [1]: ' 'Elige 1-4 [1]: ')"
        case "$REPLY_TEXT" in
            ''|1) LISTEN="127.0.0.1:$port"; return ;;
            2)
                if [ -z "$ip" ]; then warn "$(L 'could not find the LAN address; pick another option' 'no encontré la dirección de la LAN; elige otra opción')"; continue; fi
                LISTEN="$ip:$port"; return ;;
            3)
                echo
                warn "$(L '0.0.0.0 listens on EVERY network interface.' '0.0.0.0 escucha en TODAS las interfaces de red.')"
                note "$(L 'The page and its token travel over plain http, and the token is the only password.' 'La página y su token viajan por http sin cifrar, y el token es la única contraseña.')"
                note "$(L 'Do it only behind a firewall or a VPN, or put nginx/Caddy with https in front.' 'Hazlo solo tras un firewall o VPN, o pon nginx/Caddy con https delante.')"
                if confirm "$(L 'Listen on every interface anyway?' '¿Escuchar en todas las interfaces de todos modos?')" n; then LISTEN="0.0.0.0:$port"; return; fi ;;
            4)
                ask "$(L 'Address (host:port): ' 'Dirección (host:puerto): ')"
                if valid_port "${REPLY_TEXT##*:}" && [ "${REPLY_TEXT%:*}" != "$REPLY_TEXT" ]; then LISTEN="$REPLY_TEXT"; return; fi
                warn "$(L 'expected host:port, e.g. 192.168.1.20:7488' 'se espera host:puerto, p. ej. 192.168.1.20:7488')" ;;
            *) warn "$(L 'choose 1, 2, 3 or 4' 'elige 1, 2, 3 o 4')" ;;
        esac
    done
}

ask_public_url() {
    echo
    note "$(L 'Will you reach it through a domain with https (nginx/Caddy in front)?' '¿Lo abrirás desde un dominio con https (nginx/Caddy delante)?')"
    note "$(L 'If so, type the address (https://kitties.example.com). Enter to skip.' 'Si es así, escribe la dirección (https://kitties.example.com). Enter para omitir.')"
    ask "URL: "
    case "$REPLY_TEXT" in
        '') ;;
        http://*|https://*) PUBLIC_URL="$REPLY_TEXT" ;;
        *) warn "$(L 'it must start with http:// or https://; skipped' 'debe empezar con http:// o https://; omitida')" ;;
    esac
}

case "$LISTEN" in
    *:*) valid_port "${LISTEN##*:}" || die "--listen: $(L 'expected host:port' 'se espera host:puerto')" ;;
    *) die "--listen: $(L 'expected host:port' 'se espera host:puerto')" ;;
esac

# The service needs root; the binary alone needs only a writable --prefix.
[ "$SERVICE" -eq 0 ] || [ "$DRY_RUN" -eq 1 ] || [ "$(id -u)" -eq 0 ] || die "$(L 'run it as root (… | sudo sh), or use --no-service' 'ejecútalo como root (… | sudo sh), o usa --no-service')"
need curl
need uname
need sha256sum

env_file="$CONF_DIR/kitties.env"
FIRST_INSTALL=1
[ ! -f "$env_file" ] || FIRST_INSTALL=0
TOTAL_STEPS=4; [ "$SERVICE" -eq 1 ] || TOTAL_STEPS=2

# ---- the questions (a first install from a terminal only) ----
if [ "$INTERACTIVE" -eq 1 ] && [ "$SERVICE" -eq 1 ]; then
    echo
    say "$(L 'Kitties installer' 'Instalador de Kitties')"
    note "$(L 'This will: 1) download and verify the gateway, 2) install it, 3) create a service user and' 'Esto va a: 1) descargar y verificar el gateway, 2) instalarlo, 3) crear un usuario de servicio y')"
    note "$(L "a config in $CONF_DIR with a random token, 4) start it with systemd." "una configuración en $CONF_DIR con un token aleatorio, 4) arrancarlo con systemd.")"
    note "$(L 'Nothing else on the system is changed. Ctrl+C cancels.' 'No se toca nada más del sistema. Ctrl+C cancela.')"
    if [ "$FIRST_INSTALL" -eq 1 ]; then
        [ "$LISTEN_GIVEN" -eq 1 ] || ask_listen
        [ "$PUBLIC_URL_GIVEN" -eq 1 ] || ask_public_url
    else
        note "$(L "Found $env_file: the settings and the token are kept (this run updates the program)." "Encontré $env_file: se conservan la configuración y el token (esto solo actualiza el programa).")"
    fi
    echo
    note "$(L 'Summary' 'Resumen')"
    note "  $(L 'release:   ' 'versión:   ')$VERSION"
    note "  $(L 'binary:    ' 'binario:   ')$PREFIX/kitties"
    [ "$FIRST_INSTALL" -eq 0 ] || note "  $(L 'listens on: ' 'escucha en: ')$LISTEN"
    [ -z "$PUBLIC_URL" ] || note "  $(L 'public URL: ' 'URL pública: ')$PUBLIC_URL"
    confirm "$(L 'Install now?' '¿Instalar ahora?')" y || die "$(L 'cancelled, nothing was changed' 'cancelado, no se cambió nada')"
elif [ "$FIRST_INSTALL" -eq 1 ] && [ "$SERVICE" -eq 1 ]; then
    case "$LISTEN" in 127.0.0.1:*|localhost:*) ;; *) warn "$(L "listening on $LISTEN: the page and its token travel over plain http; use a firewall, a VPN or https in front" "escuchando en $LISTEN: la página y su token viajan por http sin cifrar; usa firewall, VPN o https delante")" ;; esac
fi

if [ "$DRY_RUN" -eq 1 ]; then
    say "dry run: LISTEN=$LISTEN PUBLIC_URL=$PUBLIC_URL"
    exit 0
fi

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
step 1 "$(L "Downloading and verifying $asset ($VERSION)" "Descargando y verificando $asset ($VERSION)")"
curl -fsSL "$url/$asset" -o "$tmp/$asset" || die "could not download $url/$asset"
curl -fsSL "$url/SHA256SUMS" -o "$tmp/SHA256SUMS" || die "could not download $url/SHA256SUMS"

want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "SHA256SUMS does not list $asset"
got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
[ "$want" = "$got" ] || die "checksum mismatch for $asset (expected $want, got $got)"
say "checksum ok"
note "$(L 'the file is the one the release published' 'el archivo es el que publicó la release')"

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
step 2 "$(L "Installing the program in $PREFIX" "Instalando el programa en $PREFIX")"
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
    echo
    say "$(L 'done (binary only). Run: kitties gateway' 'listo (solo el binario). Ejecuta: kitties gateway')"
    exit 0
fi

# ---- user, configuration, systemd ----
step 3 "$(L "Creating the service user and the configuration" "Creando el usuario de servicio y la configuración")"
command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] || die "systemd not found; use --no-service and see docs/DEPLOY.md"

if ! id "$USER_NAME" >/dev/null 2>&1; then
    useradd --system --create-home --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$USER_NAME"
    say "$(L "created the user $USER_NAME" "usuario $USER_NAME creado")"
fi
if getent group docker >/dev/null 2>&1; then
    usermod -aG docker "$USER_NAME"
else
    warn "$(L 'no docker group: the kitties will run brain-only until Docker is installed (then run this again)' 'no hay grupo docker: los kitties funcionarán solo con cerebro hasta instalar Docker (luego ejecuta esto otra vez)')"
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
    say "$(L "wrote $env_file" "escribí $env_file")"
else
    say "$(L "kept $env_file (settings and token unchanged)" "conservé $env_file (configuración y token sin cambios)")"
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

step 4 "$(L 'Starting the service' 'Arrancando el servicio')"
systemctl enable kitties >/dev/null 2>&1
if systemctl is-active --quiet kitties; then
    [ "$changed" -eq 0 ] || { systemctl restart kitties; say "$(L 'restarted the service' 'servicio reiniciado')"; }
else
    systemctl start kitties
    say "$(L 'started the service' 'servicio arrancado')"
fi

sleep 1
systemctl is-active --quiet kitties || die "$(L "the service did not stay up; see: journalctl -u kitties -e" "el servicio no se mantuvo activo; mira: journalctl -u kitties -e")"

# what is really configured (a kept file wins over the answers of this run)
cur_listen=$(sed -n 's/^KITTIES_LISTEN=//p' "$env_file" | tail -n 1)
[ -n "$cur_listen" ] || cur_listen="$LISTEN"
cur_public=$(sed -n 's/^KITTIES_PUBLIC_URL=//p' "$env_file" | tail -n 1)
port="${cur_listen##*:}"
host="${cur_listen%:*}"

echo
say "$(L "Kitties $new_version is running." "Kitties $new_version está funcionando.")"
echo
note "$(L 'Open it at:' 'Ábrelo en:')"
case "$host" in
    0.0.0.0|'')
        shown=0
        for a in $(hostname -I 2>/dev/null); do
            case "$a" in *:*) continue ;; esac
            note "  http://$a:$port"; shown=1
        done
        [ "$shown" -eq 1 ] || note "  http://<$(L 'this-server' 'este-servidor')>:$port" ;;
    *) note "  http://$host:$port" ;;
esac
[ -z "$cur_public" ] || note "  $cur_public"
echo
if [ -n "$token" ]; then
    note "$(L 'Token (the password of the page):' 'Token (la contraseña de la página):')"
    note "  $token"
    note "$(L "Saved in $env_file (mode 0600). Keep it secret." "Guardado en $env_file (modo 0600). Mantenlo en secreto.")"
else
    note "$(L "Token: see KITTIES_TOKEN in $env_file" "Token: mira KITTIES_TOKEN en $env_file")"
fi
echo
note "$(L 'Next steps:' 'Siguientes pasos:')"
case "$host" in
    127.0.0.1|localhost)
        note "  1. $(L "It only listens on this machine. From your computer: ssh -L $port:127.0.0.1:$port user@this-server," "Solo escucha en esta máquina. Desde tu computador: ssh -L $port:127.0.0.1:$port usuario@este-servidor,")"
        note "     $(L "then open http://127.0.0.1:$port - or put nginx/Caddy with https in front (docs/DEPLOY.md)." "y abre http://127.0.0.1:$port - o pon nginx/Caddy con https delante (docs/DEPLOY.md).")" ;;
    0.0.0.0|'')
        note "  1. $(L "Open port $port only to who needs it (firewall/VPN) and put https in front before using it over the internet." "Abre el puerto $port solo a quien lo necesite (firewall/VPN) y pon https delante antes de usarlo por internet.")" ;;
    *)
        note "  1. $(L "Reachable from your local network. Put https in front before using it over the internet." "Accesible desde tu red local. Pon https delante antes de usarlo por internet.")" ;;
esac
note "  2. $(L "Add a model key (OPENAI_API_KEY, ANTHROPIC_API_KEY or GEMINI_API_KEY) in $env_file, or in Settings on the page," "Añade una clave de modelo (OPENAI_API_KEY, ANTHROPIC_API_KEY o GEMINI_API_KEY) en $env_file, o en Ajustes de la página,")"
note "     $(L 'then: systemctl restart kitties' 'y luego: systemctl restart kitties')"
note "  3. $(L 'Logs: journalctl -u kitties -f' 'Logs: journalctl -u kitties -f')"
note "  4. $(L 'Update: run the same install line again (keeps your config and token).' 'Actualizar: ejecuta la misma línea de instalación otra vez (conserva tu configuración y token).')"
