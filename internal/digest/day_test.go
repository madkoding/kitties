package digest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/connector"
)

// kinded is a connector of a kind that answers each action with a canned text.
type kinded struct {
	kind    string
	answers map[string]string
	fail    error
	asked   []map[string]any
}

func (k *kinded) Describe() string { return k.kind }
func (k *kinded) Kind() string     { return k.kind }
func (k *kinded) Call(_ context.Context, args map[string]any) (string, error) {
	k.asked = append(k.asked, args)
	if k.fail != nil {
		return "", k.fail
	}
	return k.answers[fmt.Sprint(args["action"])], nil
}

type plain struct{}

func (plain) Describe() string                                     { return "" }
func (plain) Call(context.Context, map[string]any) (string, error) { return "never", nil }

func TestMyDay(t *testing.T) {
	reg := connector.NewRegistry()
	many := strings.Repeat("- task\n", 10)
	cal := &kinded{kind: "caldav", answers: map[string]string{"events": "- 10:00 | Dentist", "reminders": many}}
	reg.Add("icloud", cal)
	reg.Add("work", &kinded{kind: "microsoft-365", fail: errors.New("not signed in")})
	reg.Add("sky", &kinded{kind: "weather", answers: map[string]string{"forecast": "Santiago now: clear, 18°C"}})
	reg.Add("nowhere", &kinded{kind: "weather", fail: errors.New("place is missing: a city")})
	reg.Add("slack", &kinded{kind: "slack"})
	reg.Add("old", plain{})
	day := MyDay(reg)(context.Background())
	for _, want := range []string{
		"On the calendar:\n- 10:00 | Dentist\n- (work could not be read: not signed in)",
		"To do:\n- task", "- and 2 more in icloud",
		"Weather:\nSantiago now: clear, 18°C",
	} {
		if !strings.Contains(day, want) {
			t.Errorf("day lacks %q:\n%s", want, day)
		}
	}
	if strings.Contains(day, "nowhere") || strings.Contains(day, "never") {
		t.Errorf("a weather without a place, or a connector without a kind, says something:\n%s", day)
	}
	if cal.asked[0]["days"] != "1" {
		t.Errorf("the calendar is asked for today: %v", cal.asked[0])
	}
	if MyDay(connector.NewRegistry())(context.Background()) != "" {
		t.Error("nothing configured, nothing said")
	}
	if clip("abc", 2) != "ab…" {
		t.Error("clip")
	}
}

func TestDigestWithTheDay(t *testing.T) {
	g := newRig(t)
	g.r.src.Day = func(context.Context) string { return "On the calendar:\n- 10:00 | Dentist" }
	if err := g.r.SendNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.sent[0].Title != "Your kitties, last 24 hours" {
		t.Errorf("the day waits for the setting: %+v", g.sent[0])
	}
	g.cfg.Day = true
	_ = g.r.SendNow(context.Background())
	n := g.sent[1]
	if n.Title != "Your day, and your kitties" || !strings.HasPrefix(n.Body, "On the calendar:\n- 10:00 | Dentist\n\nYour kitties, last 24 hours:\nmimi:") {
		t.Errorf("with the day = %+v", n)
	}
	g.r.src.Day = func(context.Context) string { return "" }
	_ = g.r.SendNow(context.Background())
	if g.sent[2].Title != "Your kitties, last 24 hours" {
		t.Errorf("an empty day = %+v", g.sent[2])
	}
}
