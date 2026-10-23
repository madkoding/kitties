package digest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/connector"
)

// A PERSON'S DAY.
//
// The morning summary can open with what the day holds: the calendar, the reminders still to
// do and the weather, read from whichever connections have them. Each kind answers the same
// question with its own action.

// daySections are what the day says, in order, and the action each kind answers it with.
var daySections = []struct {
	title string
	calls map[string]map[string]any
}{
	{"On the calendar", map[string]map[string]any{
		"caldav":           {"action": "events", "days": "1"},
		"apple":            {"action": "events", "days": "1"},
		"google-workspace": {"action": "list_events", "limit": 10.0},
		"microsoft-365":    {"action": "list_events", "limit": 10.0},
	}},
	{"To do", map[string]map[string]any{
		"caldav":           {"action": "reminders"},
		"apple":            {"action": "reminders"},
		"google-workspace": {"action": "tasks", "limit": 10.0},
		"microsoft-365":    {"action": "tasks", "limit": 10.0},
	}},
	{"Weather", map[string]map[string]any{
		"weather": {"action": "forecast", "days": "1"},
	}},
}

// dayLines is how many lines one connection adds to a section.
const dayLines = 8

// MyDay reads the person's day from the connectors of reg.
func MyDay(reg *connector.Registry) func(ctx context.Context) string {
	return func(ctx context.Context) string {
		names := reg.Names()
		sort.Strings(names)
		var sections []string
		for _, sec := range daySections {
			var lines []string
			for _, name := range names {
				c, ok := reg.Get(name)
				if !ok {
					continue
				}
				k, ok := c.(connector.Kinder)
				if !ok {
					continue
				}
				args, ok := sec.calls[k.Kind()]
				if !ok {
					continue
				}
				callCtx, cancel := context.WithTimeout(ctx, time.Minute)
				out, err := c.Call(callCtx, args)
				cancel()
				if err != nil {
					// A weather connection without a default place has nothing to say.
					if !strings.Contains(err.Error(), "place is missing") {
						lines = append(lines, fmt.Sprintf("- (%s could not be read: %s)", name, clip(err.Error(), 120)))
					}
					continue
				}
				got := strings.Split(strings.TrimSpace(out), "\n")
				if len(got) > dayLines {
					got = append(got[:dayLines], fmt.Sprintf("- and %d more in %s", len(got)-dayLines, name))
				}
				lines = append(lines, got...)
			}
			if len(lines) > 0 {
				sections = append(sections, sec.title+":\n"+strings.Join(lines, "\n"))
			}
		}
		return strings.Join(sections, "\n\n")
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
