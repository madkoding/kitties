#!/bin/sh
# What the shelf's weather island shows, from wttr.in: the temperature, the sky, then three lines
# "day|temperature|glyph" (glyph: sun, cloud, cloud-sun, rain, snow, storm, fog). The place is
# KITTY_WEATHER_PLACE (a city name) or, empty, wherever the kitty's address is. Prints nothing
# when the service does not answer (the island hides). Units: Celsius, or Fahrenheit with
# KITTY_WEATHER_UNITS=f.
set -u
glyph() {
    case "$1" in
    113) echo sun ;;
    116) echo cloud-sun ;;
    119 | 122) echo cloud ;;
    143 | 248 | 260) echo fog ;;
    200 | 386 | 389 | 392 | 395) echo storm ;;
    179 | 182 | 185 | 227 | 230 | 317 | 320 | 323 | 326 | 329 | 332 | 335 | 338 | 350 | 362 | 365 | 368 | 371 | 374 | 377) echo snow ;;
    *) echo rain ;;
    esac
}
place="$(printf '%s' "${KITTY_WEATHER_PLACE:-}" | tr -cd 'A-Za-z0-9 ,._-' | tr ' ' '+')"
json="$(curl -fsS -m 10 "https://wttr.in/${place}?format=j1" 2>/dev/null)" || exit 0
if [ "${KITTY_WEATHER_UNITS:-c}" = f ]; then
    t=temp_F
    hi=maxtempF
else
    t=temp_C
    hi=maxtempC
fi
now="$(printf '%s' "$json" | jq -r ".current_condition[0] | \"\(.$t)°\n\(.weatherDesc[0].value)\"" 2>/dev/null)" || exit 0
[ -n "$now" ] || exit 0
echo "$now"
for i in 0 1 2; do
    row="$(printf '%s' "$json" | jq -r ".weather[$i] | \"\(.date) \(.$hi) \(.hourly[4].weatherCode)\"" 2>/dev/null)" || continue
    # shellcheck disable=SC2086
    set -- $row
    [ "$#" -eq 3 ] || continue
    echo "$(date -d "$1" +%a 2>/dev/null)|$2°|$(glyph "$3")"
done
exit 0
