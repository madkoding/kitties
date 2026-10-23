package agenda

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is something a service reported: a webhook that landed on the gateway.
type Event struct {
	At time.Time `json:"at"`
	// Source is the connector the event came through (its name in the settings).
	Source string `json:"source"`
	// Kind is the service's own name for it (a GitHub event, a Slack type), when it gave one.
	Kind string `json:"kind,omitempty"`
	// Text is what the kitty reads.
	Text string `json:"text"`
}

// maxEventText bounds what a kitty is shown of a payload.
const maxEventText = 2000

// Summarise turns a webhook into an event: the fields that say what happened when they are
// there (GitHub, Slack, Discord, generic), the payload itself otherwise, trimmed.
func Summarise(source, kind string, body []byte) Event {
	ev := Event{Source: source, Kind: kind}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil || len(m) == 0 {
		ev.Text = clip(strings.TrimSpace(string(body)), maxEventText)
		if ev.Text == "" {
			ev.Text = "(an empty event)"
		}
		return ev
	}
	var lines []string
	// GitHub: what happened to what, by whom, and where to look.
	if action := str(m, "action"); action != "" {
		lines = append(lines, "action: "+action)
	}
	for _, key := range []string{"issue", "pull_request", "comment", "release", "discussion"} {
		if sub, ok := m[key].(map[string]any); ok {
			line := key
			if n, ok := sub["number"].(float64); ok {
				line += fmt.Sprintf(" #%d", int(n))
			}
			if t := str(sub, "title", "name", "tag_name"); t != "" {
				line += ": " + t
			}
			if u, ok := sub["user"].(map[string]any); ok {
				if login := str(u, "login"); login != "" {
					line += " (by " + login + ")"
				}
			}
			lines = append(lines, line)
			if b := str(sub, "body"); b != "" {
				lines = append(lines, clip(b, 600))
			}
			if u := str(sub, "html_url"); u != "" {
				lines = append(lines, u)
			}
		}
	}
	if repo, ok := m["repository"].(map[string]any); ok {
		if name := str(repo, "full_name"); name != "" {
			lines = append(lines, "repository: "+name)
		}
	}
	if ref := str(m, "ref"); ref != "" && m["commits"] != nil {
		n := 0
		if commits, ok := m["commits"].([]any); ok {
			n = len(commits)
		}
		lines = append(lines, fmt.Sprintf("push to %s: %d commits", ref, n))
		if head, ok := m["head_commit"].(map[string]any); ok {
			if msg := str(head, "message"); msg != "" {
				lines = append(lines, clip(msg, 300))
			}
		}
	}
	// Stripe: an event's type and the object it is about.
	if typ := str(m, "type"); typ != "" && strings.Contains(typ, ".") {
		if data, ok := m["data"].(map[string]any); ok {
			if obj, ok := data["object"].(map[string]any); ok {
				if ev.Kind == "" {
					ev.Kind = typ
				}
				line := typ
				if id := str(obj, "id"); id != "" {
					line += " " + id
				}
				lines = append(lines, line)
				if amount, ok := obj["amount_due"].(float64); ok {
					lines = append(lines, fmt.Sprintf("amount due: %.2f %s", amount/100, strings.ToUpper(str(obj, "currency"))))
				} else if amount, ok := obj["amount"].(float64); ok {
					lines = append(lines, fmt.Sprintf("amount: %.2f %s", amount/100, strings.ToUpper(str(obj, "currency"))))
				}
				if who := str(obj, "customer_email", "receipt_email", "email"); who != "" {
					lines = append(lines, "customer: "+who)
				}
				if st := str(obj, "status"); st != "" {
					lines = append(lines, "status: "+st)
				}
			}
		}
	}
	// Home Assistant (an automation's rest_command): which device, and what it is now.
	if entity := str(m, "entity_id"); entity != "" {
		line := entity
		if name := str(m, "name", "friendly_name"); name != "" {
			line = name + " (" + entity + ")"
		}
		if from := str(m, "from", "from_state"); from != "" {
			line += " went from " + from
		}
		if to := str(m, "state", "to", "to_state"); to != "" {
			line += " is now " + to
		}
		if ev.Kind == "" {
			ev.Kind = "state_changed"
		}
		lines = append(lines, line)
		if msg := str(m, "message"); msg != "" {
			lines = append(lines, clip(msg, 600))
		}
	}
	// Slack (Events API) and Discord: who said what, where.
	if inner, ok := m["event"].(map[string]any); ok {
		if ev.Kind == "" {
			ev.Kind = str(inner, "type")
		}
		if t := str(inner, "text"); t != "" {
			line := "message"
			if u := str(inner, "user"); u != "" {
				line += " from " + u
			}
			if c := str(inner, "channel"); c != "" {
				line += " in " + c
			}
			lines = append(lines, line+": "+clip(t, 1000))
		}
	}
	if t := str(m, "text", "content", "message"); t != "" && len(lines) == 0 {
		line := "message"
		if u := str(m, "user_name", "user", "author"); u != "" {
			line += " from " + u
		}
		lines = append(lines, line+": "+clip(t, 1000))
	}
	if len(lines) == 0 {
		// Nothing we recognise: the top-level fields, flat, in a stable order.
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, _ := json.Marshal(m[k])
			lines = append(lines, k+": "+clip(string(v), 200))
		}
	}
	ev.Text = clip(strings.Join(lines, "\n"), maxEventText)
	return ev
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
