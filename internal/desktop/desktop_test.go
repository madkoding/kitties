package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls [][]string
	out   string
	err   error
}

func (f *fakeRunner) Exec(_ context.Context, ref string, argv ...string) (string, error) {
	f.calls = append(f.calls, append([]string{ref}, argv...))
	return f.out, f.err
}
func (f *fakeRunner) Screenshot(context.Context, string) ([]byte, error) { return []byte("png"), nil }

func TestCatalogueAndParse(t *testing.T) {
	c := Catalogue()
	for _, tool := range Tools {
		if !strings.Contains(c, "- "+tool.Name+" ") {
			t.Errorf("catalogue lacks %s", tool.Name)
		}
	}
	actions, rest := Parse("I will click.\n@tool click {\"x\": 1, \"y\": 2}\n`@tool type {\"text\": \"hi\"}`\n@tool key Return\n@tool DONE {\"summary\": \"ok\"}\nbye")
	if len(actions) != 4 || rest != "I will click.\nbye" {
		t.Fatalf("Parse = %+v, %q", actions, rest)
	}
	if actions[0].Name != "click" || actions[0].Args["x"] != 1.0 || actions[2].Args["text"] != "Return" {
		t.Errorf("actions = %+v", actions)
	}
	if summary, ok := actions[3].Done(); !ok || summary != "ok" {
		t.Error("done")
	}
	if _, ok := actions[0].Done(); ok {
		t.Error("click is not done")
	}
}

func TestArgv(t *testing.T) {
	good := map[string]string{
		`click {"x": 10, "y": 20}`:                      "xdotool mousemove 10 20 click 1",
		`click {"x": "10", "y": 20, "button": "right"}`: "xdotool mousemove 10 20 click 3",
		`double_click {"x": 1, "y": 1}`:                 "xdotool mousemove 1 1 click --repeat 2 --delay 80 1",
		`move {"x": 1, "y": 1}`:                         "xdotool mousemove 1 1",
		`type {"text": "hello world"}`:                  "xdotool type --delay 20 -- hello world",
		`key {"keys": "ctrl+l"}`:                        "xdotool key -- ctrl+l",
		`scroll {"direction": "up", "amount": 2}`:       "xdotool click --repeat 2 --delay 40 4",
		`scroll {}`:                                     "xdotool click --repeat 3 --delay 40 5",
		`open {"url": "https://example.com"}`:           "kitty-open https://example.com",
		`run {"argv": ["ls", "-la", 2]}`:                "ls -la 2",
		`run {"command": "echo hi"}`:                    "echo hi",
		`type {"text": 5}`:                              "xdotool type --delay 20 -- 5",
		`drag {"x": 1, "y": 2, "to_x": 30, "to_y": 40}`: "xdotool mousemove 1 2 mousedown 1 sleep 0.15 mousemove 15 21 sleep 0.05 mousemove --sync 30 40 sleep 0.15 mouseup 1",
		`windows {}`:                                    "wmctrl -l",
		`focus {"title": "Mozilla Firefox"}`:            "wmctrl -a Mozilla Firefox",
		`launch {"argv": ["xterm", "-e", "top"]}`:       "kitty-launch xterm -e top",
		`launch {"program": "gimp"}`:                    "kitty-launch gimp",
		`install {"packages": ["gimp", "g++"]}`:         "kitty-install gimp g++",
		`install {"packages": "vlc krita"}`:             "kitty-install vlc krita",
		`install {"package": "vlc"}`:                    "kitty-install vlc",
	}
	for line, want := range good {
		actions, _ := Parse(Prefix + line)
		argv, err := Argv(actions[0])
		if err != nil || strings.Join(argv, " ") != want {
			t.Errorf("%s: %v %v, want %s", line, argv, err, want)
		}
	}
	bad := []string{
		`click {"y": 1}`, `click {"x": 1}`, `click {"x": "a", "y": 1}`, `click {"x": -1, "y": 1}`, `click {"x": 1, "y": 1, "button": "x"}`,
		`click {"x": true, "y": 1}`, `type {}`, `key {}`, `key {"keys": "a b"}`, `scroll {"amount": 99}`, `open {"url": "ftp://x"}`,
		`run {}`, `run {"argv": []}`, `explode {}`,
		`drag {"x": 1, "y": 2, "to_x": 3}`, `drag {"x": 1, "y": 2, "to_x": 3, "to_y": -4}`, `focus {}`, `launch {}`,
		`install {}`, `install {"packages": ["-o", "x"]}`, `install {"packages": ["vlc; rm -rf /"]}`,
	}
	for _, line := range bad {
		actions, _ := Parse(Prefix + line)
		if _, err := Argv(actions[0]); err == nil {
			t.Errorf("%s must be refused", line)
		}
	}
	for _, name := range []string{"screenshot", "done", "wait"} {
		if argv, err := Argv(Action{Name: name}); err != nil || argv != nil {
			t.Errorf("%s builds nothing", name)
		}
	}
}

func TestRun(t *testing.T) {
	var waited []time.Duration
	sleep = func(_ context.Context, d time.Duration) { waited = append(waited, d) }
	defer func() { sleep = func(context.Context, time.Duration) {} }()
	r := &fakeRunner{out: "  clicked \n"}
	ctx := context.Background()
	out, err := Run(ctx, r, "mimi", Action{Name: "click", Args: map[string]any{"x": 1.0, "y": 2.0}})
	if err != nil || out != "clicked" || r.calls[0][0] != "mimi" || r.calls[0][1] != "xdotool" {
		t.Errorf("Run = %q %v %v", out, err, r.calls)
	}
	if _, err := Run(ctx, r, "mimi", Action{Name: "click"}); err == nil {
		t.Error("an invalid action fails before running")
	}
	if out, err := Run(ctx, r, "mimi", Action{Name: "screenshot"}); err != nil || out != "" {
		t.Error("screenshot runs nothing")
	}
	if out, err := Run(ctx, r, "mimi", Action{Name: "wait", Args: map[string]any{"seconds": 99.0}}); err != nil || out != "waited 10s" || waited[0] != 10*time.Second {
		t.Errorf("wait is capped: %q %v", out, waited)
	}
	if out, _ := Run(ctx, r, "mimi", Action{Name: "wait"}); out != "waited 1s" {
		t.Error("wait defaults to 1s")
	}
	if _, err := Run(ctx, r, "mimi", Action{Name: "open", Args: map[string]any{"url": "https://x"}}); err != nil || waited[len(waited)-1] != 2*time.Second {
		t.Error("open waits for the browser")
	}
	r.out = strings.Repeat("x", 5000)
	if out, _ := Run(ctx, r, "mimi", Action{Name: "run", Args: map[string]any{"argv": []any{"ls"}}}); len(out) != 4003 {
		t.Error("output is clipped")
	}
	r.out, r.err = "", errors.New("boom")
	if _, err := Run(ctx, r, "mimi", Action{Name: "run", Args: map[string]any{"argv": []any{"ls"}}}); err == nil {
		t.Error("a failure is reported")
	}
	r.out = "partial"
	if out, err := Run(ctx, r, "mimi", Action{Name: "run", Args: map[string]any{"argv": []any{"ls"}}}); err == nil || out != "partial" {
		t.Error("a failure keeps the output")
	}
}

func TestSleepHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	realSleep := func(ctx context.Context, d time.Duration) {
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}
	realSleep(ctx, time.Second)
	if time.Since(start) > 500*time.Millisecond {
		t.Error("a cancelled context ends the wait")
	}
	realSleep(context.Background(), time.Millisecond)
}

// The person only uses the mouse and the keys on a display they took over.
func TestPerson(t *testing.T) {
	for _, a := range []Action{
		{Name: "click", Args: map[string]any{"x": 1.0, "y": 2.0}},
		{Name: "drag", Args: map[string]any{"x": 1.0, "y": 2.0, "to_x": 3.0, "to_y": 4.0}},
		{Name: "type", Args: map[string]any{"text": "hola"}},
		{Name: "key", Args: map[string]any{"keys": "ctrl+v"}},
		{Name: "scroll", Args: map[string]any{"direction": "up"}},
	} {
		if _, err := Person(a); err != nil {
			t.Errorf("%s: %v", a.Name, err)
		}
	}
	for _, a := range []Action{
		{Name: "run", Args: map[string]any{"argv": []any{"rm"}}},
		{Name: "install", Args: map[string]any{"packages": []any{"vlc"}}},
		{Name: "type", Args: map[string]any{"text": strings.Repeat("a", 2001)}},
		{Name: "key", Args: map[string]any{}},
	} {
		if _, err := Person(a); err == nil {
			t.Errorf("%s must be refused", a.Name)
		}
	}
}

// CAPTCHAs go to the person: the catalogue says so.
func TestCatalogueHandsCaptchasToThePerson(t *testing.T) {
	c := Catalogue()
	if !strings.Contains(c, "CAPTCHAs and other checks that a human is there are the person's") || !strings.Contains(c, "ask_help") {
		t.Error("the catalogue must hand CAPTCHAs to the person")
	}
}
