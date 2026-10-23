package connector

import (
	"strings"
	"testing"
)

var ha = Settings{"url": "http://homeassistant.local:8123/", "token": "lltok"}

func TestHomeAssistant(t *testing.T) {
	states := `[{"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen light"}},
{"entity_id":"sensor.living_temp","state":"21.5","attributes":{"friendly_name":"Living room","unit_of_measurement":"°C"}},
{"entity_id":"lock.front","state":"locked","attributes":{"friendly_name":"Front door"}}]`
	out, err, cap := run(t, "home-assistant", ha, map[string]any{}, states)
	if err != nil || !strings.Contains(out, "- sensor.living_temp | Living room | 21.5 °C") || cap.reqs[0].URL.String() != "http://homeassistant.local:8123/api/states" ||
		cap.reqs[0].Header.Get("Authorization") != "Bearer lltok" {
		t.Errorf("states = %q %v", out, err)
	}
	if out, _, _ := run(t, "home-assistant", ha, map[string]any{"domain": "light"}, states); out != "- light.kitchen | Kitchen light | on" {
		t.Errorf("by domain = %q", out)
	}
	if out, _, _ := run(t, "home-assistant", ha, map[string]any{"query": "door"}, states); out != "- lock.front | Front door | locked" {
		t.Errorf("by query = %q", out)
	}
	if out, _, _ := run(t, "home-assistant", ha, map[string]any{"query": "garage"}, states); out != "no device matches" {
		t.Errorf("none = %q", out)
	}
	if _, err, _ := run(t, "home-assistant", ha, map[string]any{}, "<html>"); err == nil {
		t.Error("an unreadable answer fails")
	}
	out, err, cap = run(t, "home-assistant", ha, map[string]any{"action": "call_service", "service": "light.turn_on", "entity_id": "light.kitchen", "data": map[string]any{"brightness_pct": 40.0}}, "[]")
	if err != nil || out != "done: light.turn_on light.kitchen" || cap.reqs[0].URL.Path != "/api/services/light/turn_on" ||
		!strings.Contains(cap.bodies[0], `"brightness_pct":40`) || !strings.Contains(cap.bodies[0], `"entity_id":"light.kitchen"`) {
		t.Errorf("call = %q %v %s %s", out, err, cap.reqs[0].URL, cap.bodies[0])
	}
	// The domain comes from the entity when the service is bare.
	if _, _, cap := run(t, "home-assistant", ha, map[string]any{"action": "call_service", "service": "lock", "entity_id": "lock.front"}, "[]"); cap.reqs[0].URL.Path != "/api/services/lock/lock" {
		t.Errorf("domain from entity = %s", cap.reqs[0].URL)
	}
	for _, args := range []map[string]any{{"action": "call_service"}, {"action": "call_service", "service": "turn_on"}} {
		if _, err, _ := run(t, "home-assistant", ha, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	if RiskOf("home-assistant", "call_service") != RiskAsk || RiskOf("home-assistant", "states") != RiskNone {
		t.Error("acting on the house asks first")
	}
}

func TestFeeds(t *testing.T) {
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>Blog</title>
<item><title>Old post</title><link>https://blog/1</link><pubDate>Mon, 20 Oct 2026 10:00:00 +0000</pubDate></item>
<item><title><![CDATA[New <b>post</b>]]></title><link>https://blog/2</link><pubDate>Wed, 22 Oct 2026 10:00:00 +0000</pubDate></item>
<item><title></title></item></channel></rss>`
	atom := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Releases</title>
<entry><title>v2.0</title><link rel="self" href="https://x/self"/><link href="https://x/v2"/><updated>2026-10-21T09:00:00Z</updated></entry></feed>`
	rdf := `<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/"><channel><title>Old school</title></channel>
<item><title>Undated</title><link>https://o/1</link></item></rdf:RDF>`
	s := Settings{"feeds": "https://blog/feed, https://x/atom\nhttps://o/rdf https://down/feed https://html/page"}
	out, err, cap := run(t, "rss", s, map[string]any{}, rss, atom, rdf, "not used", "<html")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(cap.reqs) != 5 || !strings.Contains(lines[0], "Blog | New post | https://blog/2") || !strings.Contains(lines[1], "Releases | v2.0 | https://x/v2") ||
		!strings.Contains(lines[2], "Old post") || !strings.Contains(out, "- Old school | Undated | https://o/1") {
		t.Errorf("news =\n%s", out)
	}
	if !strings.Contains(out, "could not read: https://down/feed (not a feed); https://html/page (not a feed)") {
		t.Errorf("failures = %s", out)
	}
	out, _, cap = run(t, "rss", s, map[string]any{"feed": "https://one/feed", "query": "old"}, rss)
	if len(cap.reqs) != 1 || !strings.HasPrefix(out, "- 2026-10-2") || !strings.HasSuffix(out, " | Blog | Old post | https://blog/1") || strings.Contains(out, "\n") {
		t.Errorf("one feed, filtered = %q", out)
	}
	c := &capture{code: 500}
	c.install(t)
	conn, _ := Build("rss", Settings{"feeds": "https://a"}, "")
	if out, _ := conn.Call(t.Context(), map[string]any{}); !strings.Contains(out, "could not read: https://a (HTTP 500") {
		t.Errorf("down = %q", out)
	}
	if _, err, _ := run(t, "rss", Settings{"feeds": " , "}, map[string]any{}); err == nil {
		t.Error("no feeds fails")
	}
	if _, err, _ := run(t, "rss", s, map[string]any{"action": "dance"}); err == nil {
		t.Error("an unknown action fails")
	}
}

func TestWeather(t *testing.T) {
	geo := `{"results":[{"name":"Santiago","admin1":"Región Metropolitana","country":"Chile","latitude":-33.45,"longitude":-70.66}]}`
	fc := `{"current":{"temperature_2m":18.4,"apparent_temperature":17.2,"weather_code":2,"wind_speed_10m":9,"relative_humidity_2m":40},
"daily":{"time":["2026-10-22","2026-10-23"],"weather_code":[0,61],"temperature_2m_max":[24,19],"temperature_2m_min":[9,11],"precipitation_probability_max":[0,70]}}`
	out, err, cap := run(t, "weather", Settings{"place": "Santiago"}, map[string]any{"days": "2"}, geo, fc)
	if err != nil {
		t.Fatal(err)
	}
	want := "Santiago, Región Metropolitana, Chile now: partly cloudy, 18°C (feels 17°C), wind 9 km/h, humidity 40%\n- 2026-10-22 | clear | 9 to 24°C | rain 0%\n- 2026-10-23 | light rain | 11 to 19°C | rain 70%"
	if out != want {
		t.Errorf("forecast =\n%s", out)
	}
	if !strings.Contains(cap.reqs[0].URL.String(), "name=Santiago") || !strings.Contains(cap.reqs[1].URL.String(), "latitude=-33.4500") || !strings.Contains(cap.reqs[1].URL.String(), "forecast_days=2") {
		t.Errorf("requests = %s %s", cap.reqs[0].URL, cap.reqs[1].URL)
	}
	if _, err, _ := run(t, "weather", Settings{}, map[string]any{}); err == nil {
		t.Error("no place fails")
	}
	if _, err, _ := run(t, "weather", Settings{}, map[string]any{"place": "Atlantis"}, `{}`); err == nil || !strings.Contains(err.Error(), "Atlantis") {
		t.Errorf("unknown place = %v", err)
	}
	if _, err, _ := run(t, "weather", Settings{}, map[string]any{"place": "x"}, geo, "<html>"); err == nil {
		t.Error("a broken forecast fails")
	}
	if weatherWord(42) != "code 42" || len(nonEmpty("", "a")) != 1 {
		t.Error("helpers")
	}
	c := &capture{answer: []string{geo}, code: 0}
	c.install(t)
	conn, _ := Build("weather", Settings{"place": "x"}, "")
	c.answer = []string{geo, `{"current":{}}`}
	if out, err := conn.Call(t.Context(), map[string]any{}); err != nil || !strings.Contains(out, "now: clear") {
		t.Errorf("a forecast without days = %q %v", out, err)
	}
}

func TestHomeAssistantReceivesEvents(t *testing.T) {
	k, _ := LookupKind("home-assistant")
	found := false
	for _, f := range k.Fields {
		found = found || f.Name == "inbound_secret"
	}
	if !k.Inbound || !found {
		t.Errorf("home assistant takes events: %+v", k)
	}
	if w, _ := LookupKind("weather"); w.Inbound {
		t.Error("the weather posts nothing")
	}
}
