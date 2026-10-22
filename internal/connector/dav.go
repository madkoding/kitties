package connector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CALENDARS, REMINDERS AND CONTACTS OVER DAV.
//
// CalDAV and CardDAV are how iCloud, Fastmail, Nextcloud, Synology, mailbox.org, Zoho and most
// other services share calendars, task lists and address books. A kitty reads what is coming
// up, adds events and reminders, ticks reminders off, and finds or adds people. The server is
// found from its address alone: the account's principal, its homes, then its collections.

// davDo sends one DAV request (PROPFIND, REPORT, PUT…) with basic auth and returns the body.
func davDo(ctx context.Context, s Settings, method, target, depth, ctype string, body []byte, extra map[string]string) ([]byte, http.Header, error) {
	var r io.Reader
	if body != nil {
		r = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, r)
	if err != nil {
		return nil, nil, err
	}
	req.SetBasicAuth(s["user"], s["password"])
	req.Header.Set("User-Agent", "kitties")
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := httpDo(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, nil, errors.New("the server refused the user and password: for iCloud use an app-specific password (appleid.apple.com → Sign-In and Security)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, clip(string(data), 300))
	}
	return data, resp.Header, nil
}

// The parts of a DAV multistatus answer kitties reads. encoding/xml matches local names, so the
// DAV:, CalDAV and CardDAV prefixes a server picks do not matter.
type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string `xml:"href"`
	Propstat []struct {
		Prop   davProp `xml:"prop"`
		Status string  `xml:"status"`
	} `xml:"propstat"`
}

type davProp struct {
	DisplayName  string `xml:"displayname"`
	ResourceType struct {
		Inner string `xml:",innerxml"`
	} `xml:"resourcetype"`
	Principal struct {
		Href string `xml:"href"`
	} `xml:"current-user-principal"`
	CalendarHome struct {
		Href string `xml:"href"`
	} `xml:"calendar-home-set"`
	AddressHome struct {
		Href string `xml:"href"`
	} `xml:"addressbook-home-set"`
	Components struct {
		Comps []struct {
			Name string `xml:"name,attr"`
		} `xml:"comp"`
	} `xml:"supported-calendar-component-set"`
	CalendarData string `xml:"calendar-data"`
	AddressData  string `xml:"address-data"`
	ETag         string `xml:"getetag"`
}

// prop merges the successful propstats of a response.
func (r davResponse) prop() davProp {
	var p davProp
	for _, ps := range r.Propstat {
		if ps.Status != "" && !strings.Contains(ps.Status, " 200") {
			continue
		}
		q := ps.Prop
		if q.DisplayName != "" {
			p.DisplayName = q.DisplayName
		}
		if q.ResourceType.Inner != "" {
			p.ResourceType = q.ResourceType
		}
		if q.Principal.Href != "" {
			p.Principal = q.Principal
		}
		if q.CalendarHome.Href != "" {
			p.CalendarHome = q.CalendarHome
		}
		if q.AddressHome.Href != "" {
			p.AddressHome = q.AddressHome
		}
		if len(q.Components.Comps) > 0 {
			p.Components = q.Components
		}
		if q.CalendarData != "" {
			p.CalendarData = q.CalendarData
		}
		if q.AddressData != "" {
			p.AddressData = q.AddressData
		}
		if q.ETag != "" {
			p.ETag = q.ETag
		}
	}
	return p
}

func parseMultistatus(data []byte) (davMultistatus, error) {
	var ms davMultistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return ms, fmt.Errorf("the server's answer is not DAV: %w", err)
	}
	return ms, nil
}

// resolve turns an href of the answer into an absolute URL on the server.
func resolve(baseURL, href string) string {
	b, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	h, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	return b.ResolveReference(h).String()
}

const (
	davPropfind = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:prop>%s</d:prop></d:propfind>`
	davXML      = "application/xml; charset=utf-8"
)

// davCollection is a calendar, a task list or an address book.
type davCollection struct {
	URL   string
	Name  string
	Comps []string // VEVENT, VTODO for a calendar
}

func (c davCollection) has(comp string) bool {
	if comp == "" || len(c.Comps) == 0 {
		return true // any collection will do; a server that does not say holds everything
	}
	for _, x := range c.Comps {
		if strings.EqualFold(x, comp) {
			return true
		}
	}
	return false
}

// davHome finds the account's calendar or address book home: from the principal, which the
// server's address names.
func davHome(ctx context.Context, s Settings, card bool) (string, error) {
	root := strings.TrimRight(s["url"], "/") + "/"
	data, _, err := davDo(ctx, s, "PROPFIND", root, "0", davXML, []byte(fmt.Sprintf(davPropfind, "<d:current-user-principal/>")), nil)
	if err != nil {
		return "", err
	}
	ms, err := parseMultistatus(data)
	if err != nil {
		return "", err
	}
	principal := root
	for _, r := range ms.Responses {
		if h := r.prop().Principal.Href; h != "" {
			principal = resolve(root, h)
		}
	}
	prop, what := "<c:calendar-home-set/>", "calendar"
	if card {
		prop, what = "<card:addressbook-home-set/>", "address book"
	}
	data, _, err = davDo(ctx, s, "PROPFIND", principal, "0", davXML, []byte(fmt.Sprintf(davPropfind, prop)), nil)
	if err != nil {
		return "", err
	}
	if ms, err = parseMultistatus(data); err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		p := r.prop()
		if h := p.CalendarHome.Href; h != "" && !card {
			return resolve(principal, h), nil
		}
		if h := p.AddressHome.Href; h != "" && card {
			return resolve(principal, h), nil
		}
	}
	return "", fmt.Errorf("the server named no %s home for this account: check the address", what)
}

// davCollections lists the calendars (or address books) of the account.
func davCollections(ctx context.Context, s Settings, card bool) ([]davCollection, error) {
	home, err := davHome(ctx, s, card)
	if err != nil {
		return nil, err
	}
	props := "<d:resourcetype/><d:displayname/><c:supported-calendar-component-set/>"
	data, _, err := davDo(ctx, s, "PROPFIND", home, "1", davXML, []byte(fmt.Sprintf(davPropfind, props)), nil)
	if err != nil {
		return nil, err
	}
	ms, err := parseMultistatus(data)
	if err != nil {
		return nil, err
	}
	var out []davCollection
	for _, r := range ms.Responses {
		p := r.prop()
		rt := strings.ToLower(p.ResourceType.Inner)
		ok := strings.Contains(rt, "calendar") && !strings.Contains(rt, "schedule-")
		if card {
			ok = strings.Contains(rt, "addressbook")
		}
		if !ok {
			continue
		}
		c := davCollection{URL: resolve(home, r.Href), Name: p.DisplayName}
		for _, comp := range p.Components.Comps {
			c.Comps = append(c.Comps, strings.ToUpper(comp.Name))
		}
		if c.Name == "" {
			c.Name = strings.Trim(r.Href[strings.LastIndex(strings.TrimRight(r.Href, "/"), "/")+1:], "/")
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("the account has no calendars or address books there")
	}
	return out, nil
}

// pickCollection is the collection named by want (its name or its address), or the first that
// holds comp.
func pickCollection(list []davCollection, want, comp string) (davCollection, error) {
	for _, c := range list {
		if want != "" && (strings.EqualFold(c.Name, want) || strings.TrimRight(c.URL, "/") == strings.TrimRight(want, "/")) {
			return c, nil
		}
	}
	if want != "" {
		names := make([]string, 0, len(list))
		for _, c := range list {
			names = append(names, c.Name)
		}
		return davCollection{}, fmt.Errorf("there is no %q here; the account has: %s", want, strings.Join(names, ", "))
	}
	for _, c := range list {
		if c.has(comp) {
			return c, nil
		}
	}
	return davCollection{}, fmt.Errorf("the account has no list that holds %s", strings.ToLower(comp))
}

// ── iCalendar and vCard, as much as kitties needs ───────────────────────────────────────────

// icsProp is one property line: its name, its parameters and its value.
type icsProp struct {
	Name, Params, Value string
}

// icsComponents splits a calendar (or a vCard file) into the components called kind, each as
// its property lines, folded lines joined.
func icsComponents(text, kind string) [][]icsProp {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")
	var out [][]icsProp
	var cur []icsProp
	depth := 0
	for _, line := range strings.Split(text, "\n") {
		up := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case up == "BEGIN:"+kind:
			cur, depth = []icsProp{}, 1
			continue
		case up == "END:"+kind && depth == 1:
			out = append(out, cur)
			cur, depth = nil, 0
			continue
		case cur == nil:
			continue
		case strings.HasPrefix(up, "BEGIN:"):
			depth++ // an alarm inside an event: its lines are not the event's
			continue
		case strings.HasPrefix(up, "END:"):
			depth--
			continue
		}
		if depth != 1 {
			continue
		}
		head, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name, params, _ := strings.Cut(head, ";")
		cur = append(cur, icsProp{Name: strings.ToUpper(name), Params: params, Value: icsUnescape(value)})
	}
	return out
}

func icsUnescape(v string) string {
	r := strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(r.Replace(v))
}

func icsEscape(v string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`)
	return r.Replace(v)
}

func icsGet(props []icsProp, name string) icsProp {
	for _, p := range props {
		if p.Name == name {
			return p
		}
	}
	return icsProp{}
}

func icsAll(props []icsProp, name string) []string {
	var out []string
	for _, p := range props {
		if p.Name == name && p.Value != "" {
			out = append(out, p.Value)
		}
	}
	return out
}

// icsWhen renders a date or date-time property as a person reads it, and a key that sorts.
func icsWhen(p icsProp) (text, key string) {
	v := p.Value
	if v == "" {
		return "", "~"
	}
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		return t.UTC().Format("2006-01-02 15:04 UTC"), t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse("20060102T150405", v); err == nil {
		tz := ""
		for _, param := range strings.Split(p.Params, ";") {
			if k, val, ok := strings.Cut(param, "="); ok && strings.EqualFold(k, "TZID") {
				tz = " " + strings.Trim(val, `"`)
			}
		}
		return t.Format("2006-01-02 15:04") + tz, t.Format(time.RFC3339)
	}
	if t, err := time.Parse("20060102", v); err == nil {
		return t.Format("2006-01-02") + " (all day)", t.Format(time.RFC3339)
	}
	return v, v
}

// parseWhen reads a time a kitty gives: RFC 3339, "2006-01-02 15:04", or a date (all day).
func parseWhen(v string) (t time.Time, allDay bool, err error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, false, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("%q is not a time: use 2026-10-22T15:00:00-03:00, 2026-10-22 15:00 or 2026-10-22", v)
}

func icsStamp(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

func newUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b) + "@kitties"
}

// ── CalDAV ─────────────────────────────────────────────────────────────────────────────────

type caldavConnector struct{ s Settings }

func makeCalDAV(s Settings) Connector { return &caldavConnector{s: s} }

func (c *caldavConnector) Describe() string {
	k, _ := LookupKind("caldav")
	return k.Help + " " + actionsHelp(k)
}

// davItem is an event or a task as it is listed.
type davItem struct {
	key, line, uid, href, etag, data string
	done                             bool
}

// query returns the events (or tasks) of the collections that hold comp.
func (c *caldavConnector) query(ctx context.Context, comp, want string, from, to time.Time) ([]davItem, error) {
	list, err := davCollections(ctx, c.s, false)
	if err != nil {
		return nil, err
	}
	if want != "" {
		one, err := pickCollection(list, want, comp)
		if err != nil {
			return nil, err
		}
		list = []davCollection{one}
	}
	data := "<c:calendar-data/>"
	inner := ""
	if comp == "VEVENT" {
		rng := fmt.Sprintf(`start="%s" end="%s"`, icsStamp(from), icsStamp(to))
		data = "<c:calendar-data><c:expand " + rng + "/></c:calendar-data>"
		inner = "<c:time-range " + rng + "/>"
	}
	body := `<?xml version="1.0" encoding="utf-8"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/>` + data +
		`</d:prop><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="` + comp + `">` + inner + `</c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	var items []davItem
	for _, col := range list {
		if !col.has(comp) {
			continue
		}
		raw, _, err := davDo(ctx, c.s, "REPORT", col.URL, "1", davXML, []byte(body), nil)
		if err != nil {
			return nil, err
		}
		ms, err := parseMultistatus(raw)
		if err != nil {
			return nil, err
		}
		for _, r := range ms.Responses {
			p := r.prop()
			for _, props := range icsComponents(p.CalendarData, comp) {
				items = append(items, describeItem(comp, col.Name, props, resolve(col.URL, r.Href), p.ETag, p.CalendarData))
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].key < items[j].key })
	return items, nil
}

func describeItem(comp, list string, props []icsProp, href, etag, data string) davItem {
	uid := icsGet(props, "UID").Value
	title := icsGet(props, "SUMMARY").Value
	if comp == "VEVENT" {
		when, key := icsWhen(icsGet(props, "DTSTART"))
		end, _ := icsWhen(icsGet(props, "DTEND"))
		parts := []string{when}
		if end != "" && end != when {
			parts[0] += " → " + end
		}
		parts = append(parts, title)
		if loc := icsGet(props, "LOCATION").Value; loc != "" {
			parts = append(parts, loc)
		}
		parts = append(parts, list)
		return davItem{key: key, line: "- " + strings.Join(parts, " | "), uid: uid, href: href, etag: etag, data: data}
	}
	due, key := icsWhen(icsGet(props, "DUE"))
	status := strings.ToUpper(icsGet(props, "STATUS").Value)
	line := "- " + uid + " | " + title
	if due != "" {
		line += " | due " + due
	}
	if status == "COMPLETED" {
		line += " | done"
	}
	return davItem{key: key + "|" + title, line: line + " | " + list, uid: uid, href: href, etag: etag, data: data, done: status == "COMPLETED"}
}

func (c *caldavConnector) Call(ctx context.Context, args map[string]any) (string, error) {
	action := strings.ToLower(str(args, "action"))
	if action == "" {
		action = "events"
	}
	switch action {
	case "calendars":
		list, err := davCollections(ctx, c.s, false)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, col := range list {
			holds := strings.Join(col.Comps, ", ")
			if holds == "" {
				holds = "events and tasks"
			}
			lines = append(lines, "- "+col.Name+" | "+strings.ToLower(holds))
		}
		return strings.Join(lines, "\n"), nil
	case "events":
		days := intArg(args, "days", 7, 60)
		from := time.Now()
		if v := str(args, "from"); v != "" {
			t, _, err := parseWhen(v)
			if err != nil {
				return "", err
			}
			from = t
		}
		items, err := c.query(ctx, "VEVENT", pick(args, c.s, "calendar"), from, from.AddDate(0, 0, days))
		if err != nil {
			return "", err
		}
		return lines(items, fmt.Sprintf("nothing on the calendar in the next %d days", days)), nil
	case "reminders":
		items, err := c.query(ctx, "VTODO", pick(args, c.s, "tasks"), time.Time{}, time.Time{})
		if err != nil {
			return "", err
		}
		all := str(args, "all") == "true"
		open := items[:0]
		for _, it := range items {
			if all || !it.done {
				open = append(open, it)
			}
		}
		return lines(open, "no reminders left to do"), nil
	case "add_event":
		return c.addEvent(ctx, args)
	case "add_reminder":
		return c.addReminder(ctx, args)
	case "complete_reminder":
		return c.complete(ctx, args)
	}
	k, _ := LookupKind("caldav")
	return "", fmt.Errorf("caldav has no action %q; %s", action, actionsHelp(k))
}

// intArg is a whole number a kitty gives, def when it gives none, at most max.
func intArg(args map[string]any, key string, def, max int) int {
	n, err := strconv.Atoi(str(args, key))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, max)
}

func lines(items []davItem, empty string) string {
	if len(items) == 0 {
		return empty
	}
	out := make([]string, 0, len(items))
	for i, it := range items {
		if i == 50 {
			out = append(out, fmt.Sprintf("… and %d more", len(items)-50))
			break
		}
		out = append(out, it.line)
	}
	return strings.Join(out, "\n")
}

// put writes a new calendar object into the collection.
func (c *caldavConnector) put(ctx context.Context, col davCollection, uid, body string) error {
	target := strings.TrimRight(col.URL, "/") + "/" + url.PathEscape(strings.TrimSuffix(uid, "@kitties")) + ".ics"
	_, _, err := davDo(ctx, c.s, "PUT", target, "", "text/calendar; charset=utf-8", []byte(body), map[string]string{"If-None-Match": "*"})
	return err
}

func vcalendar(inner string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//kitties//EN\r\n" + inner + "END:VCALENDAR\r\n"
}

func (c *caldavConnector) addEvent(ctx context.Context, args map[string]any) (string, error) {
	title, err := need(args, "title", "summary", "text")
	if err != nil {
		return "", err
	}
	startText, err := need(args, "start")
	if err != nil {
		return "", err
	}
	start, allDay, err := parseWhen(startText)
	if err != nil {
		return "", err
	}
	end := start.Add(time.Hour)
	if allDay {
		end = start.AddDate(0, 0, 1)
	}
	if v := str(args, "end"); v != "" {
		if end, _, err = parseWhen(v); err != nil {
			return "", err
		}
	}
	list, err := davCollections(ctx, c.s, false)
	if err != nil {
		return "", err
	}
	col, err := pickCollection(list, pick(args, c.s, "calendar"), "VEVENT")
	if err != nil {
		return "", err
	}
	uid := newUID()
	var b strings.Builder
	b.WriteString("BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:" + icsStamp(time.Now()) + "\r\n")
	if allDay {
		b.WriteString("DTSTART;VALUE=DATE:" + start.Format("20060102") + "\r\nDTEND;VALUE=DATE:" + end.Format("20060102") + "\r\n")
	} else {
		b.WriteString("DTSTART:" + icsStamp(start) + "\r\nDTEND:" + icsStamp(end) + "\r\n")
	}
	b.WriteString("SUMMARY:" + icsEscape(title) + "\r\n")
	if v := str(args, "location"); v != "" {
		b.WriteString("LOCATION:" + icsEscape(v) + "\r\n")
	}
	if v := str(args, "description", "notes"); v != "" {
		b.WriteString("DESCRIPTION:" + icsEscape(v) + "\r\n")
	}
	b.WriteString("END:VEVENT\r\n")
	if err := c.put(ctx, col, uid, vcalendar(b.String())); err != nil {
		return "", err
	}
	return fmt.Sprintf("added %q to %s on %s", title, col.Name, start.Format("2006-01-02 15:04")), nil
}

func (c *caldavConnector) addReminder(ctx context.Context, args map[string]any) (string, error) {
	title, err := need(args, "title", "text", "summary")
	if err != nil {
		return "", err
	}
	list, err := davCollections(ctx, c.s, false)
	if err != nil {
		return "", err
	}
	col, err := pickCollection(list, pick(args, c.s, "tasks"), "VTODO")
	if err != nil {
		return "", err
	}
	uid := newUID()
	var b strings.Builder
	b.WriteString("BEGIN:VTODO\r\nUID:" + uid + "\r\nDTSTAMP:" + icsStamp(time.Now()) + "\r\nSUMMARY:" + icsEscape(title) + "\r\nSTATUS:NEEDS-ACTION\r\n")
	due := ""
	if v := str(args, "due"); v != "" {
		t, allDay, err := parseWhen(v)
		if err != nil {
			return "", err
		}
		if allDay {
			b.WriteString("DUE;VALUE=DATE:" + t.Format("20060102") + "\r\n")
		} else {
			b.WriteString("DUE:" + icsStamp(t) + "\r\n")
		}
		due = ", due " + v
	}
	if v := str(args, "notes", "description"); v != "" {
		b.WriteString("DESCRIPTION:" + icsEscape(v) + "\r\n")
	}
	b.WriteString("END:VTODO\r\n")
	if err := c.put(ctx, col, uid, vcalendar(b.String())); err != nil {
		return "", err
	}
	return fmt.Sprintf("added %q to %s%s (id %s)", title, col.Name, due, uid), nil
}

// complete ticks a reminder off: its object is read, marked completed and written back, guarded
// by its etag so a change made meanwhile is not overwritten.
func (c *caldavConnector) complete(ctx context.Context, args map[string]any) (string, error) {
	id, err := need(args, "id", "uid")
	if err != nil {
		return "", err
	}
	items, err := c.query(ctx, "VTODO", pick(args, c.s, "tasks"), time.Time{}, time.Time{})
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if it.uid != id {
			continue
		}
		done := markCompleted(it.data, time.Now())
		extra := map[string]string{}
		if it.etag != "" {
			extra["If-Match"] = it.etag
		}
		if _, _, err := davDo(ctx, c.s, "PUT", it.href, "", "text/calendar; charset=utf-8", []byte(done), extra); err != nil {
			return "", err
		}
		return "done: " + strings.SplitN(it.line, " | ", 3)[1], nil
	}
	return "", fmt.Errorf("no reminder with id %q (list them with reminders)", id)
}

// markCompleted sets a VTODO's status to completed, with the time and 100%.
func markCompleted(data string, now time.Time) string {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	var out []string
	inTodo := false
	for _, line := range strings.Split(data, "\n") {
		up := strings.ToUpper(line)
		switch {
		case up == "BEGIN:VTODO":
			inTodo = true
		case up == "END:VTODO" && inTodo:
			out = append(out, "STATUS:COMPLETED", "COMPLETED:"+icsStamp(now), "PERCENT-COMPLETE:100")
			inTodo = false
		case inTodo && (strings.HasPrefix(up, "STATUS") || strings.HasPrefix(up, "COMPLETED") || strings.HasPrefix(up, "PERCENT-COMPLETE")):
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\r\n")
}

// ── CardDAV ────────────────────────────────────────────────────────────────────────────────

type carddavConnector struct{ s Settings }

func makeCardDAV(s Settings) Connector { return &carddavConnector{s: s} }

func (c *carddavConnector) Describe() string {
	k, _ := LookupKind("carddav")
	return k.Help + " " + actionsHelp(k)
}

func (c *carddavConnector) Call(ctx context.Context, args map[string]any) (string, error) {
	action := strings.ToLower(str(args, "action"))
	if action == "" {
		action = "contacts"
	}
	switch action {
	case "contacts", "find":
		return c.find(ctx, str(args, "query", "name", "q"), limitOf(args))
	case "add_contact":
		return c.add(ctx, args)
	}
	k, _ := LookupKind("carddav")
	return "", fmt.Errorf("carddav has no action %q; %s", action, actionsHelp(k))
}

func (c *carddavConnector) book(ctx context.Context) (davCollection, error) {
	list, err := davCollections(ctx, c.s, true)
	if err != nil {
		return davCollection{}, err
	}
	return pickCollection(list, c.s["addressbook"], "")
}

func (c *carddavConnector) find(ctx context.Context, query string, max int) (string, error) {
	book, err := c.book(ctx)
	if err != nil {
		return "", err
	}
	filter := ""
	if query != "" {
		var b strings.Builder
		b.WriteString(`<card:filter test="anyof">`)
		for _, prop := range []string{"FN", "EMAIL", "TEL", "ORG", "NICKNAME"} {
			fmt.Fprintf(&b, `<card:prop-filter name="%s"><card:text-match collation="i;unicode-casemap" match-type="contains">%s</card:text-match></card:prop-filter>`, prop, xmlEscape(query))
		}
		b.WriteString(`</card:filter>`)
		filter = b.String()
	}
	body := `<?xml version="1.0" encoding="utf-8"?><card:addressbook-query xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:prop><d:getetag/><card:address-data/></d:prop>` + filter + `</card:addressbook-query>`
	raw, _, err := davDo(ctx, c.s, "REPORT", book.URL, "1", davXML, []byte(body), nil)
	if err != nil {
		return "", err
	}
	ms, err := parseMultistatus(raw)
	if err != nil {
		return "", err
	}
	var out []string
	for _, r := range ms.Responses {
		for _, props := range icsComponents(r.prop().AddressData, "VCARD") {
			out = append(out, contactLine(props))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		if query != "" {
			return "nobody matches " + query, nil
		}
		return "the address book is empty", nil
	}
	if len(out) > max {
		out = append(out[:max], fmt.Sprintf("… and %d more (narrow it with a query)", len(out)-max))
	}
	return strings.Join(out, "\n"), nil
}

func contactLine(props []icsProp) string {
	parts := []string{icsGet(props, "FN").Value}
	if v := strings.Join(icsAll(props, "EMAIL"), ", "); v != "" {
		parts = append(parts, v)
	}
	if v := strings.Join(icsAll(props, "TEL"), ", "); v != "" {
		parts = append(parts, v)
	}
	if v := strings.Trim(icsGet(props, "ORG").Value, ";"); v != "" {
		parts = append(parts, v)
	}
	if v := icsGet(props, "BDAY").Value; v != "" {
		parts = append(parts, "birthday "+v)
	}
	return "- " + strings.Join(parts, " | ")
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (c *carddavConnector) add(ctx context.Context, args map[string]any) (string, error) {
	name, err := need(args, "name", "full_name")
	if err != nil {
		return "", err
	}
	book, err := c.book(ctx)
	if err != nil {
		return "", err
	}
	uid := newUID()
	first, last := name, ""
	if i := strings.LastIndex(name, " "); i > 0 {
		first, last = name[:i], name[i+1:]
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + icsEscape(name) + "\r\nN:" + icsEscape(last) + ";" + icsEscape(first) + ";;;\r\n")
	if v := str(args, "email"); v != "" {
		b.WriteString("EMAIL;TYPE=INTERNET:" + icsEscape(v) + "\r\n")
	}
	if v := str(args, "phone", "tel"); v != "" {
		b.WriteString("TEL;TYPE=CELL:" + icsEscape(v) + "\r\n")
	}
	if v := str(args, "company", "org"); v != "" {
		b.WriteString("ORG:" + icsEscape(v) + "\r\n")
	}
	if v := str(args, "notes", "note"); v != "" {
		b.WriteString("NOTE:" + icsEscape(v) + "\r\n")
	}
	b.WriteString("END:VCARD\r\n")
	target := strings.TrimRight(book.URL, "/") + "/" + url.PathEscape(strings.TrimSuffix(uid, "@kitties")) + ".vcf"
	if _, _, err := davDo(ctx, c.s, "PUT", target, "", "text/vcard; charset=utf-8", []byte(b.String()), map[string]string{"If-None-Match": "*"}); err != nil {
		return "", err
	}
	return fmt.Sprintf("added %s to %s", name, book.Name), nil
}
