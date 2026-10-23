package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A PERSON'S DAY: CALENDARS, REMINDERS AND CONTACTS.
//
// The same three things live in different places: in the Mac's own apps (Calendar, Reminders,
// Contacts), in a Google or a Microsoft account, or on a CalDAV/CardDAV server (dav.go). Each
// connector here reads what is coming up, adds to it and finds people, with the same action
// names wherever it can, so a kitty says the same thing whatever the person uses.

// ── the Mac's own apps ─────────────────────────────────────────────────────────────────────

// appleScript is the JavaScript for Automation the apple connector runs: one entry point that
// reads its request from its argument (never from the code), and answers JSON.
const appleScript = `
function run(argv) {
  const a = JSON.parse(argv[0]);
  const iso = (d) => (d ? d.toISOString() : "");
  switch (a.action) {
    case "calendars":
      return JSON.stringify(Application("Calendar").calendars().map((c) => ({ name: c.name(), writable: c.writable() })));
    case "events": {
      const from = new Date(a.from), to = new Date(a.to), out = [];
      Application("Calendar").calendars().forEach((c) => {
        if (a.calendar && c.name() !== a.calendar) return;
        try {
          c.events.whose({ _and: [{ startDate: { _greaterThan: from } }, { startDate: { _lessThan: to } }] })().forEach((e) =>
            out.push({ start: iso(e.startDate()), end: iso(e.endDate()), title: e.summary(), location: e.location() || "", calendar: c.name(), allday: e.alldayEvent() }));
        } catch (err) {}
      });
      return JSON.stringify(out);
    }
    case "add_event": {
      const cal = Application("Calendar");
      const c = a.calendar ? cal.calendars.byName(a.calendar) : cal.calendars().find((x) => x.writable());
      c.events.push(cal.Event({ summary: a.title, startDate: new Date(a.start), endDate: new Date(a.end), location: a.location || "", description: a.notes || "", alldayEvent: !!a.allday }));
      return JSON.stringify({ calendar: c.name() });
    }
    case "reminders": {
      const out = [];
      Application("Reminders").lists().forEach((l) => {
        if (a.list && l.name() !== a.list) return;
        l.reminders.whose({ completed: false })().forEach((x) => out.push({ id: x.id(), title: x.name(), due: iso(x.dueDate()), list: l.name() }));
      });
      return JSON.stringify(out);
    }
    case "add_reminder": {
      const r = Application("Reminders");
      const l = a.list ? r.lists.byName(a.list) : r.defaultList();
      const props = { name: a.title };
      if (a.notes) props.body = a.notes;
      if (a.due) props.dueDate = new Date(a.due);
      const x = r.Reminder(props);
      l.reminders.push(x);
      return JSON.stringify({ id: x.id(), list: l.name() });
    }
    case "complete_reminder": {
      const x = Application("Reminders").reminders.byId(a.id);
      x.completed = true;
      return JSON.stringify({ title: x.name() });
    }
    case "contacts": {
      const p = Application("Contacts");
      const people = a.query ? p.people.whose({ _or: [{ name: { _contains: a.query } }, { organization: { _contains: a.query } }] })() : p.people();
      return JSON.stringify(people.slice(0, a.limit).map((x) => ({ name: x.name(), emails: x.emails().map((e) => e.value()), phones: x.phones().map((e) => e.value()), org: x.organization() || "" })));
    }
    case "add_contact": {
      const p = Application("Contacts");
      const words = a.name.trim().split(/\s+/);
      const x = p.Person({ firstName: words.length > 1 ? words.slice(0, -1).join(" ") : a.name, lastName: words.length > 1 ? words[words.length - 1] : "" });
      p.people.push(x);
      if (a.email) x.emails.push(p.Email({ label: "home", value: a.email }));
      if (a.phone) x.phones.push(p.Phone({ label: "mobile", value: a.phone }));
      if (a.company) x.organization = a.company;
      p.save();
      return JSON.stringify({ name: x.name() });
    }
  }
  throw new Error("unknown action " + a.action);
}`

// osascript runs the script with the request; tests answer without a Mac.
var osascript = func(ctx context.Context, script string, request []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script, string(request))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// onMac is whether the gateway runs on a Mac; tests say yes.
var onMac = func() bool { return runtime.GOOS == "darwin" }

type appleConnector struct{ s Settings }

func makeApple(s Settings) Connector { return &appleConnector{s: s} }

func (c *appleConnector) Describe() string {
	k, _ := LookupKind("apple")
	return k.Help + " " + actionsHelp(k)
}

// ask runs one request against the Mac's apps and decodes the answer into v.
func (c *appleConnector) ask(ctx context.Context, req map[string]any, v any) error {
	return askMac(ctx, appleScript, "Calendar, Reminders and Contacts", req, v)
}

// askMac runs one request with a JavaScript for Automation script and decodes its JSON answer;
// apps names what it reaches, for the messages.
func askMac(ctx context.Context, script, apps string, req map[string]any, v any) error {
	if !onMac() {
		return fmt.Errorf("the Mac's %s are only reachable when the gateway runs on a Mac (the desktop app does)", apps)
	}
	payload, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, errOut, err := osascript(ctx, script, payload)
	if err != nil {
		msg := strings.TrimSpace(string(errOut))
		low := strings.ToLower(msg)
		switch {
		case strings.Contains(msg, "-1743") || strings.Contains(low, "not authorized") || strings.Contains(low, "not allowed"):
			return errors.New("macOS did not let Kitties use the app: allow it in System Settings → Privacy & Security → Automation (and Calendars, Reminders or Contacts), then try again")
		case strings.Contains(msg, "-2700") || strings.Contains(low, "can’t get application") || strings.Contains(low, "can't get application"):
			return fmt.Errorf("the app is not installed on this Mac (%s)", apps)
		case msg == "":
			msg = err.Error()
		}
		return errors.New("the Mac's apps answered: " + clip(msg, 300))
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), v); err != nil {
		return fmt.Errorf("the Mac's apps answered something unreadable: %s", clip(string(out), 200))
	}
	return nil
}

func (c *appleConnector) Call(ctx context.Context, args map[string]any) (string, error) {
	action := strings.ToLower(str(args, "action"))
	if action == "" {
		action = "events"
	}
	switch action {
	case "calendars":
		var cals []struct {
			Name     string `json:"name"`
			Writable bool   `json:"writable"`
		}
		if err := c.ask(ctx, map[string]any{"action": "calendars"}, &cals); err != nil {
			return "", err
		}
		var out []string
		for _, cal := range cals {
			line := "- " + cal.Name
			if !cal.Writable {
				line += " (read only)"
			}
			out = append(out, line)
		}
		return strings.Join(out, "\n"), nil
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
		var events []struct {
			Start, End, Title, Location, Calendar string
			Allday                                bool
		}
		req := map[string]any{"action": "events", "from": from.Format(time.RFC3339), "to": from.AddDate(0, 0, days).Format(time.RFC3339), "calendar": pick(args, c.s, "calendar")}
		if err := c.ask(ctx, req, &events); err != nil {
			return "", err
		}
		sort.Slice(events, func(i, j int) bool { return events[i].Start < events[j].Start })
		var out []string
		for _, e := range events {
			parts := []string{localTime(e.Start, e.Allday), e.Title}
			if e.Location != "" {
				parts = append(parts, e.Location)
			}
			out = append(out, "- "+strings.Join(append(parts, e.Calendar), " | "))
		}
		if len(out) == 0 {
			return fmt.Sprintf("nothing on the calendar in the next %d days", days), nil
		}
		return strings.Join(out, "\n"), nil
	case "add_event":
		if len(Attendees(args)) > 0 {
			return "", errors.New("the Mac's Calendar cannot invite people through automation: add the event without attendees, or invite them through the account's own connection (Google, Microsoft or CalDAV)")
		}
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
		var r struct{ Calendar string }
		req := map[string]any{"action": "add_event", "title": title, "start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339), "allday": allDay,
			"location": str(args, "location"), "notes": str(args, "notes", "description"), "calendar": pick(args, c.s, "calendar")}
		if err := c.ask(ctx, req, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("added %q to %s on %s", title, r.Calendar, start.Format("2006-01-02 15:04")), nil
	case "reminders":
		var items []struct{ ID, Title, Due, List string }
		if err := c.ask(ctx, map[string]any{"action": "reminders", "list": pick(args, c.s, "list")}, &items); err != nil {
			return "", err
		}
		var out []string
		for _, it := range items {
			line := "- " + it.ID + " | " + it.Title
			if it.Due != "" {
				line += " | due " + localTime(it.Due, false)
			}
			out = append(out, line+" | "+it.List)
		}
		if len(out) == 0 {
			return "no reminders left to do", nil
		}
		return strings.Join(out, "\n"), nil
	case "add_reminder":
		title, err := need(args, "title", "text")
		if err != nil {
			return "", err
		}
		req := map[string]any{"action": "add_reminder", "title": title, "notes": str(args, "notes"), "list": pick(args, c.s, "list")}
		if v := str(args, "due"); v != "" {
			t, _, err := parseWhen(v)
			if err != nil {
				return "", err
			}
			req["due"] = t.Format(time.RFC3339)
		}
		var r struct{ ID, List string }
		if err := c.ask(ctx, req, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("added %q to %s (id %s)", title, r.List, r.ID), nil
	case "complete_reminder":
		id, err := need(args, "id")
		if err != nil {
			return "", err
		}
		var r struct{ Title string }
		if err := c.ask(ctx, map[string]any{"action": "complete_reminder", "id": id}, &r); err != nil {
			return "", err
		}
		return "done: " + r.Title, nil
	case "contacts":
		var people []struct {
			Name           string
			Emails, Phones []string
			Org            string
		}
		if err := c.ask(ctx, map[string]any{"action": "contacts", "query": str(args, "query", "name"), "limit": limitOf(args)}, &people); err != nil {
			return "", err
		}
		var out []string
		for _, p := range people {
			out = append(out, personLine(p.Name, p.Emails, p.Phones, p.Org))
		}
		if len(out) == 0 {
			return "nobody matches", nil
		}
		return strings.Join(out, "\n"), nil
	case "add_contact":
		name, err := need(args, "name")
		if err != nil {
			return "", err
		}
		var r struct{ Name string }
		req := map[string]any{"action": "add_contact", "name": name, "email": str(args, "email"), "phone": str(args, "phone"), "company": str(args, "company")}
		if err := c.ask(ctx, req, &r); err != nil {
			return "", err
		}
		return "added " + r.Name + " to Contacts", nil
	}
	k, _ := LookupKind("apple")
	return "", fmt.Errorf("apple has no action %q; %s", action, actionsHelp(k))
}

// localTime renders an RFC 3339 time in the gateway's zone.
func localTime(v string, allDay bool) string {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return v
	}
	if allDay {
		return t.Local().Format("2006-01-02") + " (all day)"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func personLine(name string, emails, phones []string, org string) string {
	parts := []string{name}
	if len(emails) > 0 {
		parts = append(parts, strings.Join(emails, ", "))
	}
	if len(phones) > 0 {
		parts = append(parts, strings.Join(phones, ", "))
	}
	if org != "" {
		parts = append(parts, org)
	}
	return "- " + strings.Join(parts, " | ")
}

// matches reports whether any of the texts contains the query, ignoring case.
func matches(query string, texts ...string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	for _, t := range texts {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// splitName is a full name as a first and a last name.
func splitName(name string) (first, last string) {
	words := strings.Fields(name)
	if len(words) < 2 {
		return name, ""
	}
	return strings.Join(words[:len(words)-1], " "), words[len(words)-1]
}

// dueTime is the due date a kitty gives, as RFC 3339 in UTC.
func dueTime(args map[string]any) (string, error) {
	v := str(args, "due")
	if v == "" {
		return "", nil
	}
	t, _, err := parseWhen(v)
	if err != nil {
		return "", err
	}
	return t.UTC().Format(time.RFC3339), nil
}

// ── Google: contacts and tasks ─────────────────────────────────────────────────────────────

const googleTasks = "https://tasks.googleapis.com/tasks/v1/lists/@default/tasks"

func googlePersonal() map[string]actionFn {
	return map[string]actionFn{
		"contacts": func(s Settings, a map[string]any) (request, error) {
			query, max := str(a, "query", "name"), limitOf(a)
			return request{method: "GET", url: "https://people.googleapis.com/v1/people/me/connections?personFields=names,emailAddresses,phoneNumbers,organizations&pageSize=1000",
				follow: func(_ context.Context, raw []byte, _ map[string]string) (string, error) {
					var r struct {
						Connections []struct {
							Names         []struct{ DisplayName string } `json:"names"`
							Emails        []struct{ Value string }       `json:"emailAddresses"`
							Phones        []struct{ Value string }       `json:"phoneNumbers"`
							Organizations []struct{ Name string }        `json:"organizations"`
						} `json:"connections"`
					}
					_ = json.Unmarshal(raw, &r)
					var out []string
					for _, p := range r.Connections {
						var name, org string
						if len(p.Names) > 0 {
							name = p.Names[0].DisplayName
						}
						if len(p.Organizations) > 0 {
							org = p.Organizations[0].Name
						}
						var emails, phones []string
						for _, e := range p.Emails {
							emails = append(emails, e.Value)
						}
						for _, ph := range p.Phones {
							phones = append(phones, ph.Value)
						}
						if matches(query, append(append([]string{name, org}, emails...), phones...)...) {
							out = append(out, personLine(name, emails, phones, org))
						}
					}
					return capLines(out, max, "nobody matches"), nil
				}}, nil
		},
		"add_contact": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name")
			if err != nil {
				return request{}, err
			}
			first, last := splitName(name)
			body := map[string]any{"names": []any{map[string]any{"givenName": first, "familyName": last}}}
			if v := str(a, "email"); v != "" {
				body["emailAddresses"] = []any{map[string]any{"value": v}}
			}
			if v := str(a, "phone"); v != "" {
				body["phoneNumbers"] = []any{map[string]any{"value": v}}
			}
			if v := str(a, "company"); v != "" {
				body["organizations"] = []any{map[string]any{"name": v}}
			}
			return request{method: "POST", url: "https://people.googleapis.com/v1/people:createContact", body: body}, nil
		},
		"tasks": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: googleTasks + "?showCompleted=false&maxResults=" + strconv.Itoa(limitOf(a))}, nil
		},
		"add_task": func(s Settings, a map[string]any) (request, error) {
			title, err := need(a, "title", "text")
			if err != nil {
				return request{}, err
			}
			due, err := dueTime(a)
			if err != nil {
				return request{}, err
			}
			body := map[string]any{"title": title, "notes": str(a, "notes")}
			if due != "" {
				body["due"] = due
			}
			return request{method: "POST", url: googleTasks, body: body}, nil
		},
		"complete_task": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id")
			if err != nil {
				return request{}, err
			}
			return request{method: "PATCH", url: googleTasks + "/" + url.PathEscape(id), body: map[string]any{"status": "completed"}}, nil
		},
	}
}

// capLines keeps the first max lines, saying how many more there were.
func capLines(lines []string, max int, empty string) string {
	if len(lines) == 0 {
		return empty
	}
	if len(lines) > max {
		lines = append(lines[:max], fmt.Sprintf("… and %d more (narrow it with a query)", len(lines)-max))
	}
	return strings.Join(lines, "\n")
}

// ── Microsoft: contacts and To Do ──────────────────────────────────────────────────────────

const msGraph = "https://graph.microsoft.com/v1.0/me"

// todoList finds the account's default To Do list, then makes the call on it.
func todoList(then func(ctx context.Context, list string, headers map[string]string) (string, error)) func(ctx context.Context, raw []byte, headers map[string]string) (string, error) {
	return func(ctx context.Context, raw []byte, headers map[string]string) (string, error) {
		var r struct {
			Value []struct {
				ID, DisplayName, WellknownListName string
			} `json:"value"`
		}
		_ = json.Unmarshal(raw, &r)
		if len(r.Value) == 0 {
			return "", errors.New("the account has no To Do lists")
		}
		list := r.Value[0].ID
		for _, l := range r.Value {
			if l.WellknownListName == "defaultList" {
				list = l.ID
			}
		}
		return then(ctx, list, headers)
	}
}

func microsoftPersonal() map[string]actionFn {
	lists := msGraph + "/todo/lists"
	return map[string]actionFn{
		"contacts": func(s Settings, a map[string]any) (request, error) {
			query, max := str(a, "query", "name"), limitOf(a)
			return request{method: "GET", url: msGraph + "/contacts?$select=displayName,emailAddresses,mobilePhone,businessPhones,homePhones,companyName&$top=500",
				follow: func(_ context.Context, raw []byte, _ map[string]string) (string, error) {
					var r struct {
						Value []struct {
							DisplayName    string
							EmailAddresses []struct{ Address string }
							MobilePhone    string
							BusinessPhones []string
							HomePhones     []string
							CompanyName    string
						} `json:"value"`
					}
					_ = json.Unmarshal(raw, &r)
					var out []string
					for _, p := range r.Value {
						var emails []string
						for _, e := range p.EmailAddresses {
							emails = append(emails, e.Address)
						}
						phones := append(append([]string{}, p.BusinessPhones...), p.HomePhones...)
						if p.MobilePhone != "" {
							phones = append([]string{p.MobilePhone}, phones...)
						}
						if matches(query, append(append([]string{p.DisplayName, p.CompanyName}, emails...), phones...)...) {
							out = append(out, personLine(p.DisplayName, emails, phones, p.CompanyName))
						}
					}
					return capLines(out, max, "nobody matches"), nil
				}}, nil
		},
		"add_contact": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name")
			if err != nil {
				return request{}, err
			}
			first, last := splitName(name)
			body := map[string]any{"givenName": first, "surname": last}
			if v := str(a, "email"); v != "" {
				body["emailAddresses"] = []any{map[string]any{"address": v, "name": name}}
			}
			if v := str(a, "phone"); v != "" {
				body["mobilePhone"] = v
			}
			if v := str(a, "company"); v != "" {
				body["companyName"] = v
			}
			return request{method: "POST", url: msGraph + "/contacts", body: body}, nil
		},
		"tasks": func(s Settings, a map[string]any) (request, error) {
			top := strconv.Itoa(limitOf(a))
			return request{method: "GET", url: lists, follow: todoList(func(ctx context.Context, list string, h map[string]string) (string, error) {
				out, err := do(ctx, request{method: "GET", url: lists + "/" + url.PathEscape(list) + "/tasks?$filter=" + url.QueryEscape("status ne 'completed'") + "&$top=" + top, headers: h})
				if err != nil {
					return "", err
				}
				return listing(out, "value", 30, "id", "title", "dueDateTime.dateTime", "importance"), nil
			})}, nil
		},
		"add_task": func(s Settings, a map[string]any) (request, error) {
			title, err := need(a, "title", "text")
			if err != nil {
				return request{}, err
			}
			due, err := dueTime(a)
			if err != nil {
				return request{}, err
			}
			body := map[string]any{"title": title}
			if v := str(a, "notes"); v != "" {
				body["body"] = map[string]any{"content": v, "contentType": "text"}
			}
			if due != "" {
				body["dueDateTime"] = map[string]any{"dateTime": strings.TrimSuffix(due, "Z"), "timeZone": "UTC"}
			}
			return request{method: "GET", url: lists, follow: todoList(func(ctx context.Context, list string, h map[string]string) (string, error) {
				out, err := do(ctx, request{method: "POST", url: lists + "/" + url.PathEscape(list) + "/tasks", headers: h, body: body})
				if err != nil {
					return "", err
				}
				return summary(out), nil
			})}, nil
		},
		"complete_task": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: lists, follow: todoList(func(ctx context.Context, list string, h map[string]string) (string, error) {
				out, err := do(ctx, request{method: "PATCH", url: lists + "/" + url.PathEscape(list) + "/tasks/" + url.PathEscape(id), headers: h, body: map[string]any{"status": "completed"}})
				if err != nil {
					return "", err
				}
				return summary(out), nil
			})}, nil
		},
	}
}

// personalKinds are the calendar, reminder, contact and home kinds of the catalogue.
func personalKinds() []Kind {
	fEvents := []Action{
		{Name: "events", Help: "what is on the calendar (the next 7 days by default)", Args: []string{"days (optional)", "from (optional)", "calendar (optional)"}},
		{Name: "add_event", Help: "add an event; with attendees it invites them (that asks you first)", Args: []string{fTitle, "start (2026-10-22 15:00, RFC 3339, or a date for all day)", "end (optional; an hour later)", "location (optional)", "notes (optional)", "attendees (optional emails)", "calendar (optional)"}},
		{Name: "calendars", Help: "the calendars and task lists of the account"},
	}
	fReminders := []Action{
		{Name: "reminders", Help: "the reminders still to do, with their ids", Args: []string{"list (optional)"}},
		{Name: "add_reminder", Help: "add a reminder", Args: []string{fTitle, "due (optional)", "notes (optional)", "list (optional)"}},
		{Name: "complete_reminder", Help: "tick a reminder off", Args: []string{"id"}},
	}
	fContacts := []Action{
		{Name: "contacts", Help: "find people by name, email, phone or company", Args: []string{"query (optional)", fLimit}},
		{Name: "add_contact", Help: "add a person", Args: []string{"name", "email (optional)", "phone (optional)", "company (optional)"}},
	}
	return []Kind{
		{Name: "caldav", Category: CatPersonal, Label: "Calendar & reminders (CalDAV)", Help: "calendars and reminders on iCloud, Fastmail, Nextcloud, Synology or any CalDAV server: what is coming up, new events and reminders, ticking them off",
			Fields: []Field{
				f("url", "Server", "https://caldav.icloud.com, https://caldav.fastmail.com, https://cloud.example.com/remote.php/dav", true, false),
				f("user", "User", "the account's email or user name", true, false),
				f("password", "App password", "an app-specific password (iCloud: appleid.apple.com → Sign-In and Security)", true, true),
				f("calendar", "Calendar", "where new events go (its name); empty: the first one", false, false),
				f("tasks", "Reminders list", "where new reminders go; empty: the first task list", false, false),
			},
			Actions: append(append([]Action{}, fEvents...), fReminders...), make: makeCalDAV},
		{Name: "carddav", Category: CatPersonal, Label: "Contacts (CardDAV)", Help: "the address book on iCloud, Fastmail, Nextcloud or any CardDAV server: find people and add them",
			Fields: []Field{
				f("url", "Server", "https://contacts.icloud.com, https://carddav.fastmail.com, https://cloud.example.com/remote.php/dav", true, false),
				f("user", "User", "", true, false),
				f("password", "App password", "an app-specific password", true, true),
				f("addressbook", "Address book", "its name; empty: the first one", false, false),
			},
			Actions: fContacts, make: makeCardDAV},
		{Name: "apple", Category: CatPersonal, Label: "Mac Calendar, Reminders & Contacts", Help: "the Mac's own Calendar, Reminders and Contacts apps (with whatever accounts they sync), when the gateway runs on the Mac; macOS asks once to allow it",
			Fields: []Field{
				f("calendar", "Calendar", "where new events go; empty: the first one that can be written", false, false),
				f("list", "Reminders list", "where new reminders go; empty: the default list", false, false),
			},
			Actions: append(append(append([]Action{}, fEvents...), fReminders...), fContacts...), make: makeApple},
		{Name: "home-assistant", Category: CatHome, Label: "Home Assistant", Help: "the house through Home Assistant: what the lights, sensors, heating and doors say, and acting on them; an automation can tell the kitties what happens (a rest_command posting to /v1/hooks/<name>)",
			Fields: []Field{
				f("url", "Address", "http://homeassistant.local:8123", true, false),
				f("token", "Long-lived access token", "your profile in Home Assistant → Security → Long-lived access tokens", true, true),
			},
			Actions: []Action{
				{Name: "states", Help: "devices and their state", Args: []string{"query (optional: kitchen, temperature…)", "domain (optional: light, sensor, climate…)"}},
				{Name: "call_service", Help: "act on a device: turn a light on, set the heating, lock a door", Args: []string{"service (light.turn_on, climate.set_temperature…)", "entity_id", "data (optional object: brightness_pct, temperature…)"}},
			}, make: makeHomeAssistant},
		{Name: "rss", Category: CatHome, Label: "News feeds (RSS)", Help: "the latest from the RSS and Atom feeds a person follows: blogs, news, releases, podcasts",
			Fields:  []Field{f("feeds", "Feeds", "their addresses, separated by commas or one per line", true, false)},
			Actions: []Action{{Name: "news", Help: "the newest items of every feed (or of one)", Args: []string{"query (optional)", "feed (optional)", fLimit}}}, make: makeFeeds},
		{Name: "weather", Category: CatHome, Label: "Weather", Help: "the weather now and the forecast for a place, from Open-Meteo (no key needed)",
			Fields:  []Field{f("place", "Default place", "a city: Santiago, Madrid…", false, false)},
			Actions: []Action{{Name: "forecast", Help: "the weather now and in the next days", Args: []string{"place (optional)", "days (optional, 3 by default)"}}}, make: makeWeather},
	}
}
