package connector

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HOME AND THE WORLD OUTSIDE.
//
// What an assistant is asked about every day besides work: the house (lights, heating, doors
// through Home Assistant), the news a person follows (RSS and Atom feeds) and the weather.

// ── Home Assistant ─────────────────────────────────────────────────────────────────────────

func makeHomeAssistant(s Settings) Connector {
	root := strings.TrimRight(s["url"], "/") + "/api"
	auth := bearer(s["token"])
	return newAPI("home-assistant", s, map[string]actionFn{
		"states": func(s Settings, a map[string]any) (request, error) {
			query, domain := str(a, "query", "name"), strings.TrimSuffix(str(a, "domain"), ".")
			return request{method: "GET", url: root + "/states", headers: auth, follow: func(_ context.Context, raw []byte, _ map[string]string) (string, error) {
				var states []struct {
					EntityID   string `json:"entity_id"`
					State      string `json:"state"`
					Attributes struct {
						Name string `json:"friendly_name"`
						Unit string `json:"unit_of_measurement"`
					} `json:"attributes"`
				}
				if err := json.Unmarshal(raw, &states); err != nil {
					return "", errors.New("home assistant answered something unreadable")
				}
				var out []string
				for _, st := range states {
					if domain != "" && !strings.HasPrefix(st.EntityID, domain+".") {
						continue
					}
					if !matches(query, st.EntityID, st.Attributes.Name) {
						continue
					}
					value := st.State
					if st.Attributes.Unit != "" {
						value += " " + st.Attributes.Unit
					}
					out = append(out, "- "+st.EntityID+" | "+st.Attributes.Name+" | "+value)
				}
				sort.Strings(out)
				return capLines(out, 60, "no device matches"), nil
			}}, nil
		},
		"call_service": func(s Settings, a map[string]any) (request, error) {
			service, err := need(a, "service")
			if err != nil {
				return request{}, err
			}
			entity := str(a, "entity_id", "entity")
			domain := str(a, "domain")
			if d, svc, ok := strings.Cut(service, "."); ok {
				domain, service = d, svc
			}
			if domain == "" && entity != "" {
				domain, _, _ = strings.Cut(entity, ".")
			}
			if domain == "" {
				return request{}, errors.New("domain is missing (light, switch, climate…), or write the service as light.turn_on")
			}
			body := map[string]any{}
			if data, ok := a["data"].(map[string]any); ok {
				for k, v := range data {
					body[k] = v
				}
			}
			if entity != "" {
				body["entity_id"] = entity
			}
			return request{method: "POST", url: root + "/services/" + url.PathEscape(domain) + "/" + url.PathEscape(service), headers: auth, body: body,
				follow: func(context.Context, []byte, map[string]string) (string, error) {
					return "done: " + domain + "." + service + " " + entity, nil
				}}, nil
		},
	}, "states")
}

// ── news feeds ─────────────────────────────────────────────────────────────────────────────

// feedDoc holds both an RSS channel and an Atom feed: encoding/xml fills whichever is there.
type feedDoc struct {
	Title   string `xml:"title"`
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
			Date    string `xml:"date"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Updated   string `xml:"updated"`
		Published string `xml:"published"`
	} `xml:"entry"`
	// RSS 1.0 (RDF) puts the items next to the channel.
	Items []struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
		Date  string `xml:"date"`
	} `xml:"item"`
}

type feedItem struct {
	when         time.Time
	source, line string
}

var feedLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02T15:04:05Z07:00", "2006-01-02"}

func feedTime(v string) time.Time {
	v = strings.TrimSpace(v)
	for _, l := range feedLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

type feedsConnector struct{ s Settings }

func makeFeeds(s Settings) Connector { return &feedsConnector{s: s} }

func (c *feedsConnector) Describe() string {
	k, _ := LookupKind("rss")
	return k.Help + " " + actionsHelp(k)
}

func (c *feedsConnector) Call(ctx context.Context, args map[string]any) (string, error) {
	if a := strings.ToLower(str(args, "action")); a != "" && a != "news" {
		k, _ := LookupKind("rss")
		return "", fmt.Errorf("rss has no action %q; %s", a, actionsHelp(k))
	}
	var feeds []string
	for _, f := range strings.FieldsFunc(c.s["feeds"], func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		if f = strings.TrimSpace(f); f != "" {
			feeds = append(feeds, f)
		}
	}
	if one := str(args, "feed", "url"); one != "" {
		feeds = []string{one}
	}
	if len(feeds) == 0 {
		return "", errors.New("no feeds: add their addresses in the connection's settings, or give one as feed")
	}
	query := str(args, "query")
	var items []feedItem
	var failed []string
	for _, f := range feeds {
		raw, err := do(ctx, request{method: "GET", url: f})
		if err != nil {
			failed = append(failed, f+" ("+clip(err.Error(), 80)+")")
			continue
		}
		var doc feedDoc
		if xml.Unmarshal(raw, &doc) != nil {
			failed = append(failed, f+" (not a feed)")
			continue
		}
		source := firstText(doc.Channel.Title, doc.Title, f)
		add := func(title, link, date string) {
			title = strings.TrimSpace(tagRe.ReplaceAllString(title, ""))
			if title == "" || !matches(query, title) {
				return
			}
			t := feedTime(date)
			when := ""
			if !t.IsZero() {
				when = t.Local().Format("2006-01-02 15:04") + " | "
			}
			items = append(items, feedItem{when: t, source: source, line: "- " + when + source + " | " + title + " | " + strings.TrimSpace(link)})
		}
		for _, it := range doc.Channel.Items {
			add(it.Title, it.Link, firstText(it.PubDate, it.Date))
		}
		for _, it := range doc.Items {
			add(it.Title, it.Link, it.Date)
		}
		for _, e := range doc.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			add(e.Title, link, firstText(e.Published, e.Updated))
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].when.After(items[j].when) })
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.line)
	}
	text := capLines(out, intArg(args, "limit", 15, 50), "nothing new in the feeds")
	if len(failed) > 0 {
		text += "\n(could not read: " + strings.Join(failed, "; ") + ")"
	}
	return text, nil
}

func firstText(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// ── weather (Open-Meteo, no key) ───────────────────────────────────────────────────────────

// weatherWords are the WMO weather codes in words.
var weatherWords = map[int]string{
	0: "clear", 1: "mainly clear", 2: "partly cloudy", 3: "overcast", 45: "fog", 48: "freezing fog",
	51: "light drizzle", 53: "drizzle", 55: "heavy drizzle", 56: "freezing drizzle", 57: "freezing drizzle",
	61: "light rain", 63: "rain", 65: "heavy rain", 66: "freezing rain", 67: "freezing rain",
	71: "light snow", 73: "snow", 75: "heavy snow", 77: "snow grains", 80: "showers", 81: "showers", 82: "violent showers",
	85: "snow showers", 86: "snow showers", 95: "thunderstorm", 96: "thunderstorm with hail", 99: "thunderstorm with hail",
}

func weatherWord(code float64) string {
	if w, ok := weatherWords[int(code)]; ok {
		return w
	}
	return "code " + strconv.Itoa(int(code))
}

func makeWeather(s Settings) Connector {
	return newAPI("weather", s, map[string]actionFn{"forecast": func(s Settings, a map[string]any) (request, error) {
		place := pick(a, s, "place")
		if place == "" {
			return request{}, errors.New("place is missing: a city, or set a default place in the connection")
		}
		days := intArg(a, "days", 3, 14)
		return request{method: "GET", url: "https://geocoding-api.open-meteo.com/v1/search?count=1&format=json&name=" + url.QueryEscape(place),
			follow: func(ctx context.Context, raw []byte, _ map[string]string) (string, error) {
				var geo struct {
					Results []struct {
						Name      string  `json:"name"`
						Country   string  `json:"country"`
						Admin     string  `json:"admin1"`
						Latitude  float64 `json:"latitude"`
						Longitude float64 `json:"longitude"`
					} `json:"results"`
				}
				if json.Unmarshal(raw, &geo) != nil || len(geo.Results) == 0 {
					return "", fmt.Errorf("no place called %q", place)
				}
				g := geo.Results[0]
				q := url.Values{
					"latitude": {strconv.FormatFloat(g.Latitude, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(g.Longitude, 'f', 4, 64)},
					"current":       {"temperature_2m,apparent_temperature,weather_code,wind_speed_10m,relative_humidity_2m"},
					"daily":         {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
					"timezone":      {"auto"},
					"forecast_days": {strconv.Itoa(days)},
				}
				out, err := do(ctx, request{method: "GET", url: "https://api.open-meteo.com/v1/forecast?" + q.Encode()})
				if err != nil {
					return "", err
				}
				var f struct {
					Current struct {
						Temp     float64 `json:"temperature_2m"`
						Feels    float64 `json:"apparent_temperature"`
						Code     float64 `json:"weather_code"`
						Wind     float64 `json:"wind_speed_10m"`
						Humidity float64 `json:"relative_humidity_2m"`
					} `json:"current"`
					Daily struct {
						Time []string  `json:"time"`
						Code []float64 `json:"weather_code"`
						Max  []float64 `json:"temperature_2m_max"`
						Min  []float64 `json:"temperature_2m_min"`
						Rain []float64 `json:"precipitation_probability_max"`
					} `json:"daily"`
				}
				if err := json.Unmarshal(out, &f); err != nil {
					return "", errors.New("the forecast is unreadable")
				}
				where := strings.Join(nonEmpty(g.Name, g.Admin, g.Country), ", ")
				lines := []string{fmt.Sprintf("%s now: %s, %.0f°C (feels %.0f°C), wind %.0f km/h, humidity %.0f%%",
					where, weatherWord(f.Current.Code), f.Current.Temp, f.Current.Feels, f.Current.Wind, f.Current.Humidity)}
				for i, day := range f.Daily.Time {
					if i >= len(f.Daily.Code) || i >= len(f.Daily.Max) || i >= len(f.Daily.Min) {
						break
					}
					line := fmt.Sprintf("- %s | %s | %.0f to %.0f°C", day, weatherWord(f.Daily.Code[i]), f.Daily.Min[i], f.Daily.Max[i])
					if i < len(f.Daily.Rain) {
						line += fmt.Sprintf(" | rain %.0f%%", f.Daily.Rain[i])
					}
					lines = append(lines, line)
				}
				return strings.Join(lines, "\n"), nil
			}}, nil
	}}, "forecast")
}

func nonEmpty(vs ...string) []string {
	var out []string
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
