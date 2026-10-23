package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The answers of a CalDAV/CardDAV server, in the order kitties asks.
const (
	davPrincipal = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/</d:href><d:propstat><d:prop><d:current-user-principal><d:href>/123/principal/</d:href></d:current-user-principal></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	davCalHome   = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:href>/123/principal/</d:href><d:propstat><d:prop><c:calendar-home-set><d:href>https://p01.example.com/123/calendars/</d:href></c:calendar-home-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	davCalendars = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
<d:response><d:href>/123/calendars/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/123/calendars/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Home</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat><d:propstat><d:prop><x:color xmlns:x="urn:x"/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response>
<d:response><d:href>/123/calendars/tasks/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Groceries</d:displayname><c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/123/calendars/inbox/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:schedule-inbox/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`
	davEvents = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:href>/123/calendars/home/a.ics</d:href><d:propstat><d:prop><d:getetag>"e1"</d:getetag><c:calendar-data>BEGIN:VCALENDAR
BEGIN:VEVENT
UID:a
DTSTART;TZID=America/Santiago:20261023T100000
DTEND;TZID=America/Santiago:20261023T110000
SUMMARY:Dentist\, Dr. Paz
LOCATION:Av. Providencia
BEGIN:VALARM
SUMMARY:not the event
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:b
DTSTART;VALUE=DATE:20261022
SUMMARY:Ana's birthday
END:VEVENT
END:VCALENDAR</c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	davTodos = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
<d:response><d:href>/123/calendars/tasks/milk.ics</d:href><d:propstat><d:prop><d:getetag>"t1"</d:getetag><c:calendar-data>BEGIN:VCALENDAR
BEGIN:VTODO
UID:milk
SUMMARY:Buy milk
DUE:20261023T120000Z
STATUS:NEEDS-ACTION
END:VTODO
END:VCALENDAR</c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/123/calendars/tasks/bread.ics</d:href><d:propstat><d:prop><c:calendar-data>BEGIN:VCALENDAR
BEGIN:VTODO
UID:bread
SUMMARY:Buy bread
STATUS:COMPLETED
END:VTODO
END:VCALENDAR</c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	davCardHome = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:response><d:href>/123/principal/</d:href><d:propstat><d:prop><card:addressbook-home-set><d:href>/123/carddavhome/</d:href></card:addressbook-home-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	davBooks    = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:response><d:href>/123/carddavhome/card/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><card:addressbook/></d:resourcetype><d:displayname>Contacts</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
	davCards    = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:response><d:href>/123/carddavhome/card/ana.vcf</d:href><d:propstat><d:prop><card:address-data>BEGIN:VCARD
VERSION:3.0
FN:Ana Paz
EMAIL;TYPE=INTERNET:ana@example.com
TEL;TYPE=CELL:+56 9 1234
ORG:Acme;
BDAY:1990-05-01
END:VCARD</card:address-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
)

var dav = Settings{"url": "https://caldav.example.com", "user": "ana@example.com", "password": "app-pass"}

func TestCalDAVEvents(t *testing.T) {
	out, err, cap := run(t, "caldav", dav, map[string]any{"days": "3"}, davPrincipal, davCalHome, davCalendars, davEvents)
	if err != nil {
		t.Fatal(err)
	}
	want := "- 2026-10-22 (all day) | Ana's birthday | Home\n- 2026-10-23 10:00 America/Santiago → 2026-10-23 11:00 America/Santiago | Dentist, Dr. Paz | Av. Providencia | Home"
	if out != want {
		t.Errorf("events =\n%s", out)
	}
	if len(cap.reqs) != 4 || cap.reqs[0].Method != "PROPFIND" || cap.reqs[0].Header.Get("Depth") != "0" || cap.reqs[1].URL.Path != "/123/principal/" ||
		cap.reqs[2].URL.String() != "https://p01.example.com/123/calendars/" || cap.reqs[3].Method != "REPORT" || cap.reqs[3].URL.Path != "/123/calendars/home/" {
		t.Fatalf("requests = %v", cap.reqs)
	}
	if u, p, _ := cap.reqs[0].BasicAuth(); u != "ana@example.com" || p != "app-pass" {
		t.Error("basic auth")
	}
	if !strings.Contains(cap.bodies[3], `<c:expand start="`) || !strings.Contains(cap.bodies[3], `name="VEVENT"`) {
		t.Errorf("report = %s", cap.bodies[3])
	}
	// The calendars, by name and what they hold.
	out, _, _ = run(t, "caldav", dav, map[string]any{"action": "calendars"}, davPrincipal, davCalHome, davCalendars)
	if out != "- Home | vevent\n- Groceries | vtodo" {
		t.Errorf("calendars = %q", out)
	}
	if out, _, _ := run(t, "caldav", dav, map[string]any{"calendar": "Home"}, davPrincipal, davCalHome, davCalendars, `<d:multistatus xmlns:d="DAV:"/>`); !strings.Contains(out, "nothing on the calendar") {
		t.Errorf("empty = %q", out)
	}
	if _, err, _ := run(t, "caldav", dav, map[string]any{"calendar": "Work"}, davPrincipal, davCalHome, davCalendars); err == nil || !strings.Contains(err.Error(), "Home, Groceries") {
		t.Errorf("unknown calendar = %v", err)
	}
}

func TestCalDAVReminders(t *testing.T) {
	out, err, cap := run(t, "caldav", dav, map[string]any{"action": "reminders"}, davPrincipal, davCalHome, davCalendars, davTodos)
	if err != nil || out != "- milk | Buy milk | due 2026-10-23 12:00 UTC | Groceries" {
		t.Errorf("reminders = %q %v", out, err)
	}
	if cap.reqs[3].URL.Path != "/123/calendars/tasks/" || !strings.Contains(cap.bodies[3], `name="VTODO"`) {
		t.Errorf("report on %s", cap.reqs[3].URL)
	}
	out, _, _ = run(t, "caldav", dav, map[string]any{"action": "reminders", "all": "true"}, davPrincipal, davCalHome, davCalendars, davTodos)
	if !strings.Contains(out, "Buy bread | done") {
		t.Errorf("all = %q", out)
	}
	// Ticking one off writes it back, completed, guarded by its etag.
	out, err, cap = run(t, "caldav", dav, map[string]any{"action": "complete_reminder", "id": "milk"}, davPrincipal, davCalHome, davCalendars, davTodos, "")
	if err != nil || out != "done: Buy milk" {
		t.Fatalf("complete = %q %v", out, err)
	}
	put := cap.reqs[4]
	if put.Method != "PUT" || put.URL.Path != "/123/calendars/tasks/milk.ics" || put.Header.Get("If-Match") != `"t1"` ||
		!strings.Contains(cap.bodies[4], "STATUS:COMPLETED") || strings.Contains(cap.bodies[4], "NEEDS-ACTION") || !strings.Contains(cap.bodies[4], "PERCENT-COMPLETE:100") {
		t.Errorf("put = %s %s %v\n%s", put.Method, put.URL, put.Header, cap.bodies[4])
	}
	if _, err, _ := run(t, "caldav", dav, map[string]any{"action": "complete_reminder", "id": "nope"}, davPrincipal, davCalHome, davCalendars, davTodos); err == nil {
		t.Error("an unknown reminder fails")
	}
}

func TestCalDAVAdd(t *testing.T) {
	out, err, cap := run(t, "caldav", dav, map[string]any{"action": "add_event", "title": "Lunch, with Bo", "start": "2026-10-24T13:00:00Z", "location": "Café", "notes": "table\nby the window"},
		davPrincipal, davCalHome, davCalendars, "")
	if err != nil || !strings.Contains(out, `added "Lunch, with Bo" to Home`) {
		t.Fatalf("add_event = %q %v", out, err)
	}
	put, body := cap.reqs[3], cap.bodies[3]
	if put.Method != "PUT" || !strings.HasPrefix(put.URL.Path, "/123/calendars/home/") || !strings.HasSuffix(put.URL.Path, ".ics") || put.Header.Get("If-None-Match") != "*" ||
		!strings.Contains(put.Header.Get("Content-Type"), "text/calendar") {
		t.Errorf("put = %s %v", put.URL, put.Header)
	}
	for _, want := range []string{"BEGIN:VEVENT", "DTSTART:20261024T130000Z", "DTEND:20261024T140000Z", `SUMMARY:Lunch\, with Bo`, "LOCATION:Café", `DESCRIPTION:table\nby the window`} {
		if !strings.Contains(body, want) {
			t.Errorf("event lacks %q:\n%s", want, body)
		}
	}
	_, _, cap = run(t, "caldav", dav, map[string]any{"action": "add_event", "title": "Holiday", "start": "2026-12-25", "end": "2026-12-27"}, davPrincipal, davCalHome, davCalendars, "")
	if !strings.Contains(cap.bodies[3], "DTSTART;VALUE=DATE:20261225") || !strings.Contains(cap.bodies[3], "DTEND;VALUE=DATE:20261227") {
		t.Errorf("all day:\n%s", cap.bodies[3])
	}
	out, err, cap = run(t, "caldav", dav, map[string]any{"action": "add_reminder", "title": "Call mum", "due": "2026-10-25", "notes": "her birthday"}, davPrincipal, davCalHome, davCalendars, "")
	if err != nil || !strings.Contains(out, `added "Call mum" to Groceries, due 2026-10-25`) || !strings.HasPrefix(cap.reqs[3].URL.Path, "/123/calendars/tasks/") ||
		!strings.Contains(cap.bodies[3], "DUE;VALUE=DATE:20261025") || !strings.Contains(cap.bodies[3], "STATUS:NEEDS-ACTION") {
		t.Errorf("add_reminder = %q %v\n%s", out, err, cap.bodies[3])
	}
	_, _, cap = run(t, "caldav", dav, map[string]any{"action": "add_reminder", "title": "Pay rent", "due": "2026-10-25 09:30"}, davPrincipal, davCalHome, davCalendars, "")
	if !strings.Contains(cap.bodies[3], "DUE:2026") {
		t.Errorf("timed due:\n%s", cap.bodies[3])
	}
	for _, args := range []map[string]any{
		{"action": "add_event", "start": "2026-10-24"}, {"action": "add_event", "title": "x"}, {"action": "add_event", "title": "x", "start": "tomorrow"},
		{"action": "add_event", "title": "x", "start": "2026-10-24", "end": "later"}, {"action": "add_reminder"}, {"action": "add_reminder", "title": "x", "due": "soon"},
		{"action": "complete_reminder"}, {"action": "dance"}, {"action": "events", "from": "someday"},
	} {
		if _, err, _ := run(t, "caldav", dav, args, davPrincipal, davCalHome, davCalendars); err == nil {
			t.Errorf("%v passes", args)
		}
	}
}

func TestDAVFailures(t *testing.T) {
	c := &capture{code: 401}
	c.install(t)
	conn, _ := Build("caldav", dav, "")
	if _, err := conn.Call(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "app-specific password") {
		t.Errorf("401 = %v", err)
	}
	c.code = 500
	if _, err := conn.Call(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("500 = %v", err)
	}
	for _, answers := range [][]string{
		{"not xml"},
		{davPrincipal, `<d:multistatus xmlns:d="DAV:"/>`},
		{davPrincipal, davCalHome, `<d:multistatus xmlns:d="DAV:"/>`},
		{davPrincipal, "nope"},
		{davPrincipal, davCalHome, "nope"},
		{davPrincipal, davCalHome, davCalendars, "nope"},
	} {
		if _, err, _ := run(t, "caldav", dav, map[string]any{}, answers...); err == nil {
			t.Errorf("%v passes", answers[len(answers)-1])
		}
	}
	if _, err := pickCollection([]davCollection{{Name: "a", Comps: []string{"VEVENT"}}}, "", "VTODO"); err == nil {
		t.Error("no list holds tasks")
	}
	if (davCollection{}).has("VTODO") != true || resolve("::", "x") != "x" {
		t.Error("helpers")
	}
	if _, _, err := davDo(context.Background(), dav, "GET", "://bad", "", "", nil, nil); err == nil {
		t.Error("a bad address fails")
	}
}

func TestCardDAV(t *testing.T) {
	out, err, cap := run(t, "carddav", dav, map[string]any{"query": "ana & co"}, davPrincipal, davCardHome, davBooks, davCards)
	if err != nil || out != "- Ana Paz | ana@example.com | +56 9 1234 | Acme | birthday 1990-05-01" {
		t.Fatalf("contacts = %q %v", out, err)
	}
	if !strings.Contains(cap.bodies[1], "addressbook-home-set") || cap.reqs[3].Method != "REPORT" || !strings.Contains(cap.bodies[3], "ana &amp; co") {
		t.Errorf("query = %s", cap.bodies[3])
	}
	if out, _, _ := run(t, "carddav", dav, map[string]any{"query": "zed"}, davPrincipal, davCardHome, davBooks, `<d:multistatus xmlns:d="DAV:"/>`); out != "nobody matches zed" {
		t.Errorf("none = %q", out)
	}
	if out, _, _ := run(t, "carddav", dav, map[string]any{}, davPrincipal, davCardHome, davBooks, `<d:multistatus xmlns:d="DAV:"/>`); out != "the address book is empty" {
		t.Errorf("empty = %q", out)
	}
	if out, _, _ := run(t, "carddav", dav, map[string]any{"limit": 0.5}, davPrincipal, davCardHome, davBooks, davCards); !strings.HasPrefix(out, "- Ana Paz") {
		t.Errorf("limit = %q", out)
	}
	out, err, cap = run(t, "carddav", dav, map[string]any{"action": "add_contact", "name": "Bo Diaz Soto", "email": "bo@example.com", "phone": "+1 555", "company": "Kit", "notes": "met at the fair"},
		davPrincipal, davCardHome, davBooks, "")
	if err != nil || out != "added Bo Diaz Soto to Contacts" {
		t.Fatalf("add = %q %v", out, err)
	}
	for _, want := range []string{"FN:Bo Diaz Soto", "N:Soto;Bo Diaz;;;", "EMAIL;TYPE=INTERNET:bo@example.com", "TEL;TYPE=CELL:+1 555", "ORG:Kit", "NOTE:met at the fair"} {
		if !strings.Contains(cap.bodies[3], want) {
			t.Errorf("card lacks %q:\n%s", want, cap.bodies[3])
		}
	}
	if !strings.HasSuffix(cap.reqs[3].URL.Path, ".vcf") || cap.reqs[3].Header.Get("If-None-Match") != "*" {
		t.Errorf("put = %s", cap.reqs[3].URL)
	}
	for _, args := range []map[string]any{{"action": "add_contact"}, {"action": "dance"}} {
		if _, err, _ := run(t, "carddav", dav, args, davPrincipal, davCardHome, davBooks); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	if _, err, _ := run(t, "carddav", dav, map[string]any{}, davPrincipal, davCardHome, davBooks, "junk"); err == nil {
		t.Error("a broken answer fails")
	}
	if _, err, _ := run(t, "carddav", dav, map[string]any{}, "junk"); err == nil {
		t.Error("a broken principal fails")
	}
}

func TestICS(t *testing.T) {
	if got, _ := icsWhen(icsProp{Value: "garbage"}); got != "garbage" {
		t.Error("unknown dates are kept")
	}
	if got, _ := icsWhen(icsProp{Value: "20261023T100000"}); got != "2026-10-23 10:00" {
		t.Errorf("floating = %q", got)
	}
	comps := icsComponents("BEGIN:VEVENT\r\nSUMMARY:a long\r\n  line\r\nnot a property\r\nEND:VEVENT", "VEVENT")
	if len(comps) != 1 || icsGet(comps[0], "SUMMARY").Value != "a long line" {
		t.Errorf("folded = %+v", comps)
	}
	if got := markCompleted("BEGIN:VTODO\nSTATUS:NEEDS-ACTION\nEND:VTODO", time.Unix(0, 0)); !strings.Contains(got, "STATUS:COMPLETED\r\nCOMPLETED:19700101T000000Z") {
		t.Errorf("completed = %q", got)
	}
	if _, _, err := parseWhen("2026-10-23 10:00"); err != nil {
		t.Error(err)
	}
}

// fakeMac answers the osascript calls with canned JSON, recording the requests.
func fakeMac(t *testing.T, answers ...string) *[]string {
	t.Helper()
	oldRun, oldMac := osascript, onMac
	var got []string
	osascript = func(_ context.Context, req []byte) ([]byte, []byte, error) {
		got = append(got, string(req))
		a := answers[0]
		if len(answers) > 1 {
			answers = answers[1:]
		}
		if strings.HasPrefix(a, "ERR:") {
			return nil, []byte(strings.TrimPrefix(a, "ERR:")), errors.New("exit status 1")
		}
		return []byte(a), nil, nil
	}
	onMac = func() bool { return true }
	t.Cleanup(func() { osascript, onMac = oldRun, oldMac })
	return &got
}

func TestApple(t *testing.T) {
	ap := Settings{"calendar": "Family"}
	call := func(args map[string]any) (string, error) {
		c, _ := Build("apple", ap, "")
		return c.Call(context.Background(), args)
	}
	got := fakeMac(t, `[{"start":"2026-10-23T13:00:00Z","end":"2026-10-23T14:00:00Z","title":"Dentist","location":"Centro","calendar":"Family"},{"start":"2026-10-22T00:00:00Z","title":"Ana","calendar":"Birthdays","allday":true}]`)
	out, err := call(map[string]any{"days": "2"})
	if err != nil || !strings.Contains(out, "Ana | Birthdays") || !strings.Contains(out, "| Dentist | Centro | Family") || strings.Index(out, "Ana") > strings.Index(out, "Dentist") {
		t.Errorf("events = %q %v", out, err)
	}
	if !strings.Contains((*got)[0], `"action":"events"`) || !strings.Contains((*got)[0], `"calendar":"Family"`) {
		t.Errorf("request = %s", (*got)[0])
	}
	cases := []struct {
		args   map[string]any
		answer string
		want   string
		sent   string
	}{
		{map[string]any{"action": "calendars"}, `[{"name":"Family","writable":true},{"name":"Holidays","writable":false}]`, "- Family\n- Holidays (read only)", `"calendars"`},
		{map[string]any{"action": "add_event", "title": "Lunch", "start": "2026-10-24 13:00", "location": "Café"}, `{"calendar":"Family"}`, `added "Lunch" to Family`, `"location":"Café"`},
		{map[string]any{"action": "add_event", "title": "Trip", "start": "2026-12-24", "end": "2026-12-26"}, `{"calendar":"Family"}`, `added "Trip"`, `"allday":true`},
		{map[string]any{"action": "reminders"}, `[{"id":"x-1","title":"Milk","due":"2026-10-23T12:00:00Z","list":"Groceries"},{"id":"x-2","title":"Bread","list":"Groceries"}]`, "- x-2 | Bread | Groceries", `"reminders"`},
		{map[string]any{"action": "reminders"}, `[]`, "no reminders left to do", `"reminders"`},
		{map[string]any{"action": "add_reminder", "title": "Call mum", "due": "2026-10-25"}, `{"id":"x-3","list":"Reminders"}`, `added "Call mum" to Reminders (id x-3)`, `"due":"2026-10-25T`},
		{map[string]any{"action": "complete_reminder", "id": "x-1"}, `{"title":"Milk"}`, "done: Milk", `"id":"x-1"`},
		{map[string]any{"action": "contacts", "query": "ana"}, `[{"name":"Ana Paz","emails":["ana@x.com"],"phones":["+56 9"],"org":"Acme"}]`, "- Ana Paz | ana@x.com | +56 9 | Acme", `"query":"ana"`},
		{map[string]any{"action": "contacts"}, `[]`, "nobody matches", `"contacts"`},
		{map[string]any{"action": "add_contact", "name": "Bo Diaz", "email": "bo@x.com"}, `{"name":"Bo Diaz"}`, "added Bo Diaz to Contacts", `"email":"bo@x.com"`},
		{map[string]any{"action": "events"}, `[]`, "nothing on the calendar in the next 7 days", `"events"`},
	}
	for _, c := range cases {
		got := fakeMac(t, c.answer)
		out, err := call(c.args)
		if err != nil || !strings.Contains(out, c.want) || !strings.Contains((*got)[0], c.sent) {
			t.Errorf("%v = %q %v (sent %s)", c.args, out, err, (*got)[0])
		}
	}
	for _, args := range []map[string]any{
		{"action": "add_event", "start": "2026-10-24"}, {"action": "add_event", "title": "x"}, {"action": "add_event", "title": "x", "start": "nope"},
		{"action": "add_event", "title": "x", "start": "2026-10-24", "end": "nope"}, {"action": "add_reminder"}, {"action": "add_reminder", "title": "x", "due": "nope"},
		{"action": "complete_reminder"}, {"action": "add_contact"}, {"action": "dance"}, {"from": "nope"},
	} {
		fakeMac(t, `{}`)
		if _, err := call(args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	fakeMac(t, "ERR:execution error: Not authorized to send Apple events to Calendar. (-1743)")
	if _, err := call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "Privacy & Security") {
		t.Errorf("not allowed = %v", err)
	}
	fakeMac(t, "ERR:")
	if _, err := call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("silent failure = %v", err)
	}
	fakeMac(t, "not json")
	if _, err := call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("unreadable = %v", err)
	}
	for _, a := range []string{"calendars", "reminders", "add_event", "add_reminder", "complete_reminder", "contacts", "add_contact"} {
		fakeMac(t, "not json")
		if _, err := call(map[string]any{"action": a, "title": "x", "start": "2026-10-24", "id": "1", "name": "x"}); err == nil {
			t.Errorf("%s with a broken answer passes", a)
		}
	}
	onMac = func() bool { return false }
	if _, err := call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "runs on a Mac") {
		t.Errorf("off a Mac = %v", err)
	}
	if localTime("x", false) != "x" || !strings.Contains(appleScript, "function run(argv)") {
		t.Error("helpers")
	}
	if c, _ := Build("apple", ap, ""); !strings.Contains(c.Describe(), "add_reminder") {
		t.Error("describe")
	}
}

var google = Settings{"client_id": "cid"}

func TestGoogleContactsAndTasks(t *testing.T) {
	people := `{"connections":[{"names":[{"displayName":"Ana Paz"}],"emailAddresses":[{"value":"ana@x.com"}],"phoneNumbers":[{"value":"+56 9"}],"organizations":[{"name":"Acme"}]},{"names":[{"displayName":"Bo"}]}]}`
	out, err, cap := run(t, "google-workspace", google, map[string]any{"action": "contacts", "query": "acme"}, people)
	if err != nil || out != "- Ana Paz | ana@x.com | +56 9 | Acme" || !strings.Contains(cap.reqs[0].URL.String(), "people/me/connections") || cap.reqs[0].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("contacts = %q %v", out, err)
	}
	if out, _, _ := run(t, "google-workspace", google, map[string]any{"action": "contacts", "limit": 1.0}, people); !strings.Contains(out, "and 1 more") {
		t.Errorf("capped = %q", out)
	}
	if out, _, _ := run(t, "google-workspace", google, map[string]any{"action": "contacts", "query": "zed"}, people); out != "nobody matches" {
		t.Errorf("none = %q", out)
	}
	_, err, cap = run(t, "google-workspace", google, map[string]any{"action": "add_contact", "name": "Bo Diaz", "email": "bo@x.com", "phone": "1", "company": "K"})
	if err != nil || !strings.Contains(cap.bodies[0], `"familyName":"Diaz"`) || !strings.Contains(cap.bodies[0], `"emailAddresses"`) || !strings.HasSuffix(cap.reqs[0].URL.Path, "people:createContact") {
		t.Errorf("add_contact = %v %s", err, cap.bodies[0])
	}
	out, _, cap = run(t, "google-workspace", google, map[string]any{"action": "tasks"}, `{"items":[{"id":"t1","title":"Milk","due":"2026-10-23T00:00:00.000Z"}]}`)
	if !strings.Contains(out, "- t1 | Milk | 2026-10-23") || !strings.Contains(cap.reqs[0].URL.String(), "showCompleted=false") {
		t.Errorf("tasks = %q", out)
	}
	_, err, cap = run(t, "google-workspace", google, map[string]any{"action": "add_task", "title": "Milk", "due": "2026-10-24", "notes": "2 l"})
	if err != nil || !strings.Contains(cap.bodies[0], `"due":"2026-10-2`) || cap.reqs[0].Method != "POST" {
		t.Errorf("add_task = %v %s", err, cap.bodies[0])
	}
	_, err, cap = run(t, "google-workspace", google, map[string]any{"action": "complete_task", "id": "t1"})
	if err != nil || cap.reqs[0].Method != "PATCH" || !strings.HasSuffix(cap.reqs[0].URL.Path, "/tasks/t1") || cap.bodies[0] != `{"status":"completed"}` {
		t.Errorf("complete = %v %s %s", err, cap.reqs[0].URL, cap.bodies[0])
	}
	for _, args := range []map[string]any{{"action": "add_contact"}, {"action": "add_task"}, {"action": "add_task", "title": "x", "due": "nope"}, {"action": "complete_task"}} {
		if _, err, _ := run(t, "google-workspace", google, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
}

var ms = Settings{"client_id": "cid"}

func TestMicrosoftContactsAndTodo(t *testing.T) {
	contacts := `{"value":[{"displayName":"Ana Paz","emailAddresses":[{"address":"ana@x.com"}],"mobilePhone":"+56 9","businessPhones":["+56 2"],"companyName":"Acme"}]}`
	out, err, _ := run(t, "microsoft-365", ms, map[string]any{"action": "contacts", "query": "ana"}, contacts)
	if err != nil || out != "- Ana Paz | ana@x.com | +56 9, +56 2 | Acme" {
		t.Errorf("contacts = %q %v", out, err)
	}
	_, _, cap := run(t, "microsoft-365", ms, map[string]any{"action": "add_contact", "name": "Bo Diaz", "email": "bo@x.com", "phone": "1", "company": "K"})
	if !strings.Contains(cap.bodies[0], `"surname":"Diaz"`) || !strings.Contains(cap.bodies[0], `"mobilePhone":"1"`) {
		t.Errorf("add_contact = %s", cap.bodies[0])
	}
	lists := `{"value":[{"id":"L0","wellknownListName":"none"},{"id":"L1","wellknownListName":"defaultList"}]}`
	out, err, cap = run(t, "microsoft-365", ms, map[string]any{"action": "tasks"}, lists, `{"value":[{"id":"t1","title":"Milk","importance":"high","dueDateTime":{"dateTime":"2026-10-23T00:00:00"}}]}`)
	if err != nil || out != "- t1 | Milk | 2026-10-23T00:00:00 | high" || !strings.Contains(cap.reqs[1].URL.String(), "/todo/lists/L1/tasks?$filter=") || cap.reqs[1].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("tasks = %q %v %s", out, err, cap.reqs[1].URL)
	}
	_, err, cap = run(t, "microsoft-365", ms, map[string]any{"action": "add_task", "title": "Milk", "due": "2026-10-24T10:00:00Z", "notes": "2 l"}, lists, `{"id":"t2"}`)
	if err != nil || cap.reqs[1].Method != "POST" || !strings.Contains(cap.bodies[1], `"dateTime":"2026-10-24T10:00:00"`) || !strings.Contains(cap.bodies[1], `"content":"2 l"`) {
		t.Errorf("add_task = %v %s", err, cap.bodies[1])
	}
	_, err, cap = run(t, "microsoft-365", ms, map[string]any{"action": "complete_task", "id": "t1"}, lists, `{"id":"t1"}`)
	if err != nil || cap.reqs[1].Method != "PATCH" || !strings.HasSuffix(cap.reqs[1].URL.Path, "/lists/L1/tasks/t1") {
		t.Errorf("complete = %v %s", err, cap.reqs[1].URL)
	}
	if _, err, _ := run(t, "microsoft-365", ms, map[string]any{"action": "tasks"}, `{"value":[]}`); err == nil || !strings.Contains(err.Error(), "no To Do lists") {
		t.Errorf("no lists = %v", err)
	}
	if out, _, _ := run(t, "microsoft-365", ms, map[string]any{"action": "tasks"}, `{"value":[{"id":"L9"}]}`, `{"value":[]}`); !strings.Contains(out, "") {
		t.Errorf("first list = %q", out)
	}
	for _, args := range []map[string]any{{"action": "add_contact"}, {"action": "add_task"}, {"action": "add_task", "title": "x", "due": "nope"}, {"action": "complete_task"}} {
		if _, err, _ := run(t, "microsoft-365", ms, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	// A call after the lists that fails says so.
	c := &capture{answer: []string{lists}}
	c.install(t)
	for _, action := range []string{"tasks", "add_task", "complete_task"} {
		follow := microsoftPersonal()[action]
		req, _ := follow(ms, map[string]any{"title": "x", "id": "t"})
		c.code = 0
		c.reqs = nil
		c.answer = []string{"{}"}
		c.code = 500
		if _, err := req.follow(context.Background(), []byte(lists), map[string]string{}); err == nil {
			t.Errorf("%s after the lists passes on a 500", action)
		}
	}
}

func TestInvites(t *testing.T) {
	if got := Attendees(map[string]any{"attendees": "ana@x.com, mailto:bo@y.org; not-an-email"}); len(got) != 2 || got[1] != "bo@y.org" {
		t.Errorf("attendees = %v", got)
	}
	if got := Attendees(map[string]any{"attendees": []any{"ana@x.com", 3.0}}); len(got) != 1 {
		t.Errorf("list = %v", got)
	}
	for _, kind := range []string{"google-workspace", "microsoft-365"} {
		if RiskOfCall(kind, map[string]any{"action": "create_event"}) != RiskNone ||
			RiskOfCall(kind, map[string]any{"action": "create_event", "attendees": "ana@x.com"}) != RiskAsk {
			t.Errorf("%s: an event with guests asks first", kind)
		}
	}
	if RiskOfCall("caldav", map[string]any{"action": "add_event", "attendees": []any{"ana@x.com"}}) != RiskAsk {
		t.Error("caldav guests ask first")
	}
	_, _, cap := run(t, "google-workspace", google, map[string]any{"action": "create_event", "summary": "Plan", "start": "2026-10-24T10:00:00Z", "attendees": "ana@x.com"})
	if !strings.Contains(cap.reqs[0].URL.String(), "sendUpdates=all") || !strings.Contains(cap.bodies[0], `"attendees":[{"email":"ana@x.com"}]`) {
		t.Errorf("google = %s %s", cap.reqs[0].URL, cap.bodies[0])
	}
	_, _, cap = run(t, "google-workspace", google, map[string]any{"action": "create_event", "summary": "Alone", "start": "2026-10-24T10:00:00Z"})
	if strings.Contains(cap.reqs[0].URL.String(), "sendUpdates") || strings.Contains(cap.bodies[0], "attendees") {
		t.Errorf("no guests = %s", cap.bodies[0])
	}
	_, _, cap = run(t, "microsoft-365", ms, map[string]any{"action": "create_event", "subject": "Plan", "start": "2026-10-24T10:00:00", "attendees": "ana@x.com"})
	if !strings.Contains(cap.bodies[0], `"attendees":[{"emailAddress":{"address":"ana@x.com"},"type":"required"}]`) {
		t.Errorf("microsoft = %s", cap.bodies[0])
	}
	_, _, cap = run(t, "caldav", dav, map[string]any{"action": "add_event", "title": "Plan", "start": "2026-10-24T10:00:00Z", "attendees": "bo@y.org"}, davPrincipal, davCalHome, davCalendars, "")
	if !strings.Contains(cap.bodies[3], "ORGANIZER:mailto:ana@example.com") || !strings.Contains(cap.bodies[3], "RSVP=TRUE:mailto:bo@y.org") {
		t.Errorf("caldav:\n%s", cap.bodies[3])
	}
	fakeMac(t, `{}`)
	c, _ := Build("apple", Settings{}, "")
	if _, err := c.Call(context.Background(), map[string]any{"action": "add_event", "title": "x", "start": "2026-10-24", "attendees": "a@b.c"}); err == nil || !strings.Contains(err.Error(), "cannot invite") {
		t.Errorf("apple = %v", err)
	}
}
