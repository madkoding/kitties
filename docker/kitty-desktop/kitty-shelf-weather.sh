#!/bin/sh
# What the shelf's weather island shows, from Open-Meteo (no key): the temperature, the sky, then
# three lines "day|temperature|glyph" (glyph: sun, cloud, cloud-sun, rain, snow, storm, fog).
# The place is KITTY_WEATHER_PLACE, or the first line of ~/.config/kitties/weather-place (a city
# name); with neither, where the kitty's address is. Prints nothing when a service does not
# answer (the island hides). Units: Celsius, or Fahrenheit with KITTY_WEATHER_UNITS=f.
set -u
# WMO weather codes: a glyph and a word for each.
glyph() {
    case "$1" in
    0) echo sun ;;
    1 | 2) echo cloud-sun ;;
    3) echo cloud ;;
    45 | 48) echo fog ;;
    71 | 73 | 75 | 77 | 85 | 86) echo snow ;;
    95 | 96 | 99) echo storm ;;
    *) echo rain ;;
    esac
}
sky() {
    case "$1" in
    0) echo Clear ;;
    1 | 2) echo "Partly cloudy" ;;
    3) echo Overcast ;;
    45 | 48) echo Fog ;;
    51 | 53 | 55 | 56 | 57) echo Drizzle ;;
    61 | 63 | 65 | 66 | 67 | 80 | 81 | 82) echo Rain ;;
    71 | 73 | 75 | 77 | 85 | 86) echo Snow ;;
    95 | 96 | 99) echo Thunderstorm ;;
    *) echo Rain ;;
    esac
}
get() { curl -fsS -m 10 "$1" 2>/dev/null; }

place="${KITTY_WEATHER_PLACE:-}"
[ -n "$place" ] || place="$(head -n 1 "$HOME/.config/kitties/weather-place" 2>/dev/null)"
place="$(printf '%s' "$place" | tr -cd 'A-Za-z0-9 ,._-' | sed 's/ /%20/g')"
if [ -n "$place" ]; then
    geo="$(get "https://geocoding-api.open-meteo.com/v1/search?name=${place}&count=1")" || exit 0
    loc="$(printf '%s' "$geo" | jq -r '.results[0] | "\(.latitude) \(.longitude)"' 2>/dev/null)" || exit 0
else
    geo="$(get "https://ipwho.is/")" || exit 0
    loc="$(printf '%s' "$geo" | jq -r '"\(.latitude) \(.longitude)"' 2>/dev/null)" || exit 0
fi
# shellcheck disable=SC2086
set -- $loc
[ "$#" -eq 2 ] && [ "$1" != null ] && [ "$2" != null ] || exit 0
unit=celsius
[ "${KITTY_WEATHER_UNITS:-c}" = f ] && unit=fahrenheit
json="$(get "https://api.open-meteo.com/v1/forecast?latitude=$1&longitude=$2&current=temperature_2m,weather_code&daily=weather_code,temperature_2m_max&forecast_days=3&timezone=auto&temperature_unit=${unit}")" || exit 0
row="$(printf '%s' "$json" | jq -r '"\(.current.temperature_2m | round) \(.current.weather_code)"' 2>/dev/null)" || exit 0
# shellcheck disable=SC2086
set -- $row
[ "$#" -eq 2 ] || exit 0
echo "$1°"
sky "$2"
for i in 0 1 2; do
    row="$(printf '%s' "$json" | jq -r ".daily | \"\(.time[$i]) \(.temperature_2m_max[$i] | round) \(.weather_code[$i])\"" 2>/dev/null)" || continue
    # shellcheck disable=SC2086
    set -- $row
    [ "$#" -eq 3 ] || continue
    echo "$(date -d "$1" +%a 2>/dev/null)|$2°|$(glyph "$3")"
done
exit 0
