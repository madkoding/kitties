// Package desktop lets a kitty act on the display of its container: look at it, click, type,
// press keys, scroll, drag, handle its windows, open the browser, start and install programs and
// run commands. The person can take the same display over (Person): they see it live and click
// and type in it, and the kitty's actions wait meanwhile. The model asks for an action with one
// line of the form
//
//	@tool click {"x": 120, "y": 44}
//
// and after the actions of a turn it receives a fresh screenshot, so it sees what it did. The
// argv for xdotool and the image's own helpers is built here; the model never writes a command
// line, and `run` takes an argument list, not a shell string.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Prefix starts a tool line.
const Prefix = "@tool "

// Action is one request of the model.
type Action struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	Raw  string         `json:"raw"`
}

// Runner executes commands in a kitty's container; the orchestrator is one.
type Runner interface {
	Exec(ctx context.Context, ref string, argv ...string) (string, error)
	Screenshot(ctx context.Context, ref string) ([]byte, error)
}

// Tools lists what the model may do, name by name, for the prompt.
var Tools = []struct{ Name, Args, Help string }{
	{"screenshot", `{}`, "look at the display (one is also taken after every batch of actions)"},
	{"click", `{"x": 0, "y": 0, "button": "left|right|middle"}`, "click at a point of the 1280x800 display"},
	{"double_click", `{"x": 0, "y": 0}`, "double-click at a point"},
	{"move", `{"x": 0, "y": 0}`, "move the mouse without clicking"},
	{"type", `{"text": "..."}`, "type text where the focus is"},
	{"key", `{"keys": "ctrl+l"}`, "press a key or a combination (xdotool names: Return, Tab, ctrl+c, alt+F4)"},
	{"scroll", `{"direction": "down|up", "amount": 3}`, "scroll the wheel at the mouse position"},
	{"drag", `{"x": 0, "y": 0, "to_x": 0, "to_y": 0}`, "press the left button at a point and let go at another (move a window, select text)"},
	{"windows", `{}`, "list the open windows (id, workspace, title)"},
	{"focus", `{"title": "..."}`, "bring to the front the window whose title has this text"},
	{"open", `{"url": "https://..."}`, "open a URL in the browser on the display"},
	{"launch", `{"argv": ["xterm"]}`, "start a program with windows and leave it open (run waits for a program to end)"},
	{"install", `{"packages": ["gimp"]}`, "install applications from Debian's repository; they are installed again when the desktop restarts"},
	{"run", `{"argv": ["ls", "-la"]}`, "run a program in your working folder (an argument list, no shell)"},
	{"wait", `{"seconds": 2}`, "wait for something to load (at most 10 seconds)"},
	{"ask_help", `{"reason": "..."}`, "ask the person to take control of your desktop for what you cannot do alone (a password, a captcha, a code they receive); you wait until they give it back, then continue"},
	{"done", `{"summary": "..."}`, "finish the task and report what you achieved"},
}

// Catalogue renders the tools for the system prompt.
func Catalogue() string {
	var b strings.Builder
	b.WriteString("You can act on your display by writing lines of the form `@tool <name> <json>` in your reply, " +
		"one action per line, in order. After the actions run you receive their results and a screenshot, " +
		"and you continue. Write `@tool done {\"summary\": \"...\"}` when the task is finished, and write " +
		"no @tool line at all when you only want to talk. Coordinates are pixels of the 1280x800 display, " +
		"origin top-left. The display is a desktop with IceWM: a taskbar at the bottom (its menu at the left " +
		"corner, then Chromium, a terminal and the files), icons on the desktop to double-click (Work is your " +
		"working folder), a file manager (pcmanfm), LibreOffice, Inkscape and mtPaint; install what else you need. " +
		"When something needs the person (a password, a captcha, a code they receive), use ask_help: they get a " +
		"button to take control of your desktop, see it live and click and type in it, and you go on when they " +
		"give it back. CAPTCHAs and other checks that a human is there are the person's to pass: never try to " +
		"solve or get around them yourself, ask for help at once. The tools:\n")
	for _, t := range Tools {
		fmt.Fprintf(&b, "- %s %s: %s\n", t.Name, t.Args, t.Help)
	}
	return b.String()
}

// Parse finds the tool lines of a reply and returns them with the remaining text.
func Parse(text string) ([]Action, string) {
	var actions []Action
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "`"))
		if !strings.HasPrefix(trimmed, Prefix) {
			kept = append(kept, line)
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, Prefix))
		name, argsText, _ := strings.Cut(rest, " ")
		a := Action{Name: strings.ToLower(strings.TrimSpace(name)), Raw: trimmed, Args: map[string]any{}}
		if argsText = strings.TrimSpace(argsText); argsText != "" {
			if err := json.Unmarshal([]byte(argsText), &a.Args); err != nil {
				a.Args = map[string]any{"text": argsText}
			}
		}
		actions = append(actions, a)
	}
	return actions, strings.TrimSpace(strings.Join(kept, "\n"))
}

// Done reports whether the action ends the task, and its summary.
func (a Action) Done() (string, bool) {
	if a.Name != "done" {
		return "", false
	}
	return str(a.Args, "summary", "text"), true
}

func str(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			switch x := v.(type) {
			case string:
				return x
			case float64:
				return strconv.FormatFloat(x, 'f', -1, 64)
			}
		}
	}
	return ""
}

func num(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is missing", key)
	}
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("%s is not a number", key)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%s is not a number", key)
}

// Seams for the tests.
var sleep = func(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// Argv builds the command for an action, or an error when the action is not valid. wait and
// done build nothing.
func Argv(a Action) ([]string, error) {
	switch a.Name {
	case "screenshot", "done", "wait", "ask_help":
		return nil, nil
	case "click", "double_click", "move":
		x, err := num(a.Args, "x")
		if err != nil {
			return nil, err
		}
		y, err := num(a.Args, "y")
		if err != nil {
			return nil, err
		}
		if x < 0 || y < 0 || x > 10000 || y > 10000 {
			return nil, errors.New("the point is off the display")
		}
		argv := []string{"xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y)}
		button := map[string]string{"": "1", "left": "1", "middle": "2", "right": "3"}[strings.ToLower(str(a.Args, "button"))]
		if button == "" {
			return nil, errors.New("button is left, middle or right")
		}
		switch a.Name {
		case "click":
			argv = append(argv, "click", button)
		case "double_click":
			argv = append(argv, "click", "--repeat", "2", "--delay", "80", "1")
		}
		return argv, nil
	case "type":
		text := str(a.Args, "text")
		if text == "" {
			return nil, errors.New("text is missing")
		}
		return []string{"xdotool", "type", "--delay", "20", "--", text}, nil
	case "key":
		keys := str(a.Args, "keys", "key", "text")
		if keys == "" || strings.ContainsAny(keys, " \n") {
			return nil, errors.New("keys is one key or combination, like Return or ctrl+l")
		}
		return []string{"xdotool", "key", "--", keys}, nil
	case "scroll":
		amount, err := num(a.Args, "amount")
		if err != nil {
			amount = 3
		}
		if amount < 1 || amount > 50 {
			return nil, errors.New("amount is between 1 and 50")
		}
		button := "5"
		if strings.ToLower(str(a.Args, "direction")) == "up" {
			button = "4"
		}
		return []string{"xdotool", "click", "--repeat", strconv.Itoa(amount), "--delay", "40", button}, nil
	case "drag":
		var p [4]int
		for i, key := range []string{"x", "y", "to_x", "to_y"} {
			n, err := num(a.Args, key)
			if err != nil {
				return nil, err
			}
			if n < 0 || n > 10000 {
				return nil, errors.New("the point is off the display")
			}
			p[i] = n
		}
		// A press, a step halfway, the end and a release, with pauses, as from a hand: a window
		// manager only starts moving a window on a motion after the press.
		return []string{"xdotool", "mousemove", strconv.Itoa(p[0]), strconv.Itoa(p[1]), "mousedown", "1", "sleep", "0.15",
			"mousemove", strconv.Itoa((p[0] + p[2]) / 2), strconv.Itoa((p[1] + p[3]) / 2), "sleep", "0.05",
			"mousemove", "--sync", strconv.Itoa(p[2]), strconv.Itoa(p[3]), "sleep", "0.15", "mouseup", "1"}, nil
	case "windows":
		return []string{"wmctrl", "-l"}, nil
	case "focus":
		title := str(a.Args, "title", "text")
		if strings.TrimSpace(title) == "" {
			return nil, errors.New("title is missing")
		}
		return []string{"wmctrl", "-a", title}, nil
	case "launch":
		argv, err := argList(a.Args)
		if err != nil {
			return nil, err
		}
		return append([]string{"kitty-launch"}, argv...), nil
	case "install":
		packages, err := packageList(a.Args)
		if err != nil {
			return nil, err
		}
		return append([]string{"kitty-install"}, packages...), nil
	case "open":
		url := str(a.Args, "url", "text")
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return nil, errors.New("url must start with http:// or https://")
		}
		return []string{"kitty-open", url}, nil
	case "run":
		return argList(a.Args)
	default:
		return nil, fmt.Errorf("unknown tool %q", a.Name)
	}
}

// argList is a program and its arguments: a list, or a command split on spaces.
func argList(args map[string]any) ([]string, error) {
	raw, ok := args["argv"].([]any)
	if !ok || len(raw) == 0 {
		if cmd := str(args, "command", "program", "text"); cmd != "" {
			return strings.Fields(cmd), nil
		}
		return nil, errors.New("argv is a list with the program first")
	}
	argv := make([]string, 0, len(raw))
	for _, v := range raw {
		argv = append(argv, fmt.Sprint(v))
	}
	return argv, nil
}

var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{0,99}$`)

// packageList is the Debian packages to install: names only, so nothing else reaches apt.
func packageList(args map[string]any) ([]string, error) {
	var names []string
	switch v := args["packages"].(type) {
	case []any:
		for _, x := range v {
			names = append(names, fmt.Sprint(x))
		}
	case string:
		names = strings.Fields(v)
	}
	if len(names) == 0 {
		names = strings.Fields(str(args, "package", "text"))
	}
	if len(names) == 0 || len(names) > 20 {
		return nil, errors.New("packages is a list of 1 to 20 Debian package names")
	}
	for _, n := range names {
		if !packageName.MatchString(n) {
			return nil, fmt.Errorf("%q is not a Debian package name", n)
		}
	}
	return names, nil
}

// personActions are what the person does on a display they took over: the mouse and the keys.
var personActions = map[string]bool{"click": true, "double_click": true, "move": true, "type": true, "key": true, "scroll": true, "drag": true}

// Person builds the command for an action of the person on the display; they only use the mouse
// and the keyboard there, like on a screen of their own.
func Person(a Action) ([]string, error) {
	if !personActions[a.Name] {
		return nil, fmt.Errorf("%q is not something done with the mouse or the keys", a.Name)
	}
	if a.Name == "type" && len(str(a.Args, "text")) > 2000 {
		return nil, errors.New("type at most 2000 characters at a time")
	}
	return Argv(a)
}

// Run executes one action in the kitty's container and returns what it printed.
func Run(ctx context.Context, r Runner, ref string, a Action) (string, error) {
	argv, err := Argv(a)
	if err != nil {
		return "", err
	}
	switch a.Name {
	case "screenshot", "done", "ask_help":
		// ask_help is the chat's to run: it waits for the person.
		return "", nil
	case "wait":
		seconds, err := num(a.Args, "seconds")
		if err != nil || seconds < 1 {
			seconds = 1
		}
		if seconds > 10 {
			seconds = 10
		}
		sleep(ctx, time.Duration(seconds)*time.Second)
		return fmt.Sprintf("waited %ds", seconds), nil
	}
	out, err := r.Exec(ctx, ref, argv...)
	out = strings.TrimSpace(out)
	if len(out) > 4000 {
		out = out[:4000] + "…"
	}
	if err != nil {
		if out == "" {
			return "", err
		}
		return out, err
	}
	if a.Name == "open" || a.Name == "launch" {
		sleep(ctx, 2*time.Second)
	}
	return out, nil
}
