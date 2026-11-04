#!/bin/sh
# Checks, inside the built image (or anywhere with jq and curl), what the shelf's islands are fed
# with: kitty-shelf-weather and kitty-shelf-media, with a stand-in curl and playerctl so that no
# network or player is needed, and that the shelf's font resolves through fontconfig.
#
#   docker run --rm --entrypoint sh -v "$PWD/docker/kitty-desktop/test:/test:ro" kitty-desktop:ci /test/shelf-feeds.sh
#
# WEATHER and MEDIA name the commands under test (default: the ones on the PATH).
set -u
WEATHER="${WEATHER:-kitty-shelf-weather}"
MEDIA="${MEDIA:-kitty-shelf-media}"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
ok() { echo "ok: $*"; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"

# A curl that answers like Open-Meteo and ipwho.is, logs the URLs it was asked for, and fails
# when told to (NOIP, NONET).
cat >"$work/bin/curl" <<'STUB'
#!/bin/sh
for a; do u="$a"; done
echo "$u" >>"$CURL_LOG"
[ -n "${NONET:-}" ] && exit 7
case "$u" in
*geocoding*) echo '{"results":[{"latitude":-33.45,"longitude":-70.66}]}' ;;
*ipwho*) [ -n "${NOIP:-}" ] && exit 22; echo '{"success":true,"latitude":40.4,"longitude":-3.7}' ;;
*forecast*) echo '{"current":{"temperature_2m":18.4,"weather_code":2},"daily":{"time":["2026-10-25","2026-10-26","2026-10-27"],"temperature_2m_max":[21.2,19.8,15.1],"weather_code":[2,61,95]}}' ;;
*) exit 22 ;;
esac
STUB
chmod +x "$work/bin/curl"
export CURL_LOG="$work/curl.log"

weather() { : >"$CURL_LOG"; PATH="$work/bin:$PATH" sh -c "exec $WEATHER" 2>/dev/null; }
shape() { # the feed's format: temperature, sky, three days
    printf '%s\n' "$1" | awk '
        NR == 1 && $0 !~ /^-?[0-9]+°$/ { bad = 1 }
        NR == 2 && $0 == "" { bad = 1 }
        NR >= 3 && $0 !~ /^[A-Za-z][A-Za-z][A-Za-z]\|-?[0-9]+°\|(sun|cloud|cloud-sun|rain|snow|storm|fog)$/ { bad = 1 }
        END { exit (NR != 5 || bad) }'
}

out="$(KITTY_WEATHER_PLACE="Santiago de Chile" weather)"
shape "$out" && ok "weather for a named city" || bad "weather for a named city: $out"
grep -q 'geocoding.*name=Santiago%20de%20Chile' "$CURL_LOG" && ok "the city is looked up" || bad "the city is not looked up"

out="$(KITTY_WEATHER_PLACE= HOME="$work" weather)"
shape "$out" && ok "weather from the address" || bad "weather from the address: $out"
grep -q 'ipwho' "$CURL_LOG" && ! grep -q geocoding "$CURL_LOG" && ok "the address is used when no city is set" || bad "the address path"

out="$(KITTY_WEATHER_PLACE= HOME="$work" NOIP=1 TZ=America/Santiago weather)"
shape "$out" && ok "weather from the time zone's city" || bad "weather from the time zone: $out"
grep -q 'geocoding.*name=Santiago' "$CURL_LOG" && ok "the time zone's city is looked up" || bad "the time zone fallback"

out="$(KITTY_WEATHER_PLACE=Madrid KITTY_WEATHER_UNITS=f weather)"
grep -q 'temperature_unit=fahrenheit' "$CURL_LOG" && ok "Fahrenheit on request" || bad "Fahrenheit"
out="$(KITTY_WEATHER_PLACE=Madrid weather)"
grep -q 'temperature_unit=celsius' "$CURL_LOG" && ok "Celsius by default" || bad "Celsius"

out="$(KITTY_WEATHER_PLACE=Madrid NONET=1 weather)"
[ -z "$out" ] && ok "no network, nothing printed (the island hides)" || bad "no network printed: $out"
out="$(KITTY_WEATHER_PLACE= HOME="$work" NONET=1 TZ= weather)"
[ -z "$out" ] && ok "no network and no place, nothing printed" || bad "no place printed: $out"

# A playerctl that reports what PLAYER_STATUS says.
cat >"$work/bin/playerctl" <<'STUB'
#!/bin/sh
[ -n "${NOPLAYER:-}" ] && exit 1
case "$1" in
status) echo "$PLAYER_STATUS" ;;
metadata)
    case "$2" in
    xesam:title) echo "God Only Knows" ;;
    xesam:artist) echo "The Beach Boys" ;;
    mpris:artUrl) echo "file:///tmp/cover.png" ;;
    esac ;;
esac
exit 0
STUB
chmod +x "$work/bin/playerctl"
media() { PATH="$work/bin:$PATH" sh -c "exec $MEDIA" 2>/dev/null; }

for st in Playing Paused; do
    out="$(PLAYER_STATUS=$st media)"
    [ "$(printf '%s\n' "$out" | sed -n 1p)" = "$st" ] &&
        [ "$(printf '%s\n' "$out" | sed -n 2p)" = "God Only Knows" ] &&
        [ "$(printf '%s\n' "$out" | sed -n 3p)" = "The Beach Boys" ] &&
        [ "$(printf '%s\n' "$out" | sed -n 4p)" = "/tmp/cover.png" ] &&
        ok "player $st: status, title, artist and cover" || bad "player $st: $out"
done
out="$(PLAYER_STATUS=Stopped media)"
[ -z "$out" ] && ok "player stopped, nothing printed" || bad "stopped printed: $out"
out="$(NOPLAYER=1 media)"
[ -z "$out" ] && ok "no player open, nothing printed (the island hides)" || bad "no player printed: $out"

# The network island: kitty-shelf-net reads the routing table and the link state ("in-use:signal:name").
NET="${NET:-kitty-shelf-net}"
mkdir -p "$work/sys/eth0"
printf 'Iface\tDestination\tGateway\nth0\t00000000\t0100A8C0\n' | sed 's/^th0/eth0/' >"$work/route"
echo up >"$work/sys/eth0/operstate"
net() { KITTY_NET_ROUTE="$work/route" KITTY_NET_SYS="$work/sys" sh -c "exec $NET" 2>/dev/null; }
[ "$(net)" = "*:100:Kitty network (eth0)" ] && ok "network up: connected at 100%" || bad "network up: $(net)"
echo down >"$work/sys/eth0/operstate"
[ -z "$(net)" ] && ok "link down, nothing printed" || bad "link down printed: $(net)"
printf 'Iface\tDestination\tGateway\n' >"$work/route"
[ -z "$(net)" ] && ok "no default route, nothing printed" || bad "no route printed: $(net)"

# The volume island: the <level> command of the desktop's rc.xml, with a pactl that prints like PulseAudio.
RC="${RC:-/home/kitty/.config/lumen/rc.xml}"
cat >"$work/bin/pactl" <<'STUB'
#!/bin/sh
echo "Volume: front-left: 42598 /  65% / -11.23 dB,   front-right: 42598 /  65% / -11.23 dB"
STUB
chmod +x "$work/bin/pactl"
level="$(sed -n 's|.*<level>\(.*\)</level>.*|\1|p' "$RC" 2>/dev/null | sed 's/&amp;/\&/g;s/&lt;/</g;s/&gt;/>/g' | head -n1)"
if [ -n "$level" ]; then
    out="$(PATH="$work/bin:$PATH" sh -c "$level" 2>/dev/null)"
    [ "$out" = "65" ] && ok "volume level reads 65 from pactl" || bad "volume level: '$out'"
else
    bad "no <level> command in $RC"
fi

# The shelf's font is Sansation and fontconfig resolves it to Sansation, not to a fallback.
if command -v fc-match >/dev/null 2>&1; then
    fam="$(fc-match -f '%{family}' Sansation)"
    case "$fam" in
    Sansation*) ok "Sansation resolves to $fam" ;;
    *) bad "Sansation resolves to $fam" ;;
    esac
    sty="$(fc-match -f '%{style}' 'Sansation:bold')"
    case "$sty" in
    *Bold*) ok "Sansation bold resolves to $sty" ;;
    *) bad "Sansation bold resolves to $sty" ;;
    esac
    fc-list | grep -qi 'jetbrainsmono' && ok "JetBrains Mono is installed" || bad "JetBrains Mono is missing"
else
    bad "fc-match is not installed"
fi

exit "$fail"
