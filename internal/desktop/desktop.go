// Package desktop lets a kitty act on the display of its container: look at it, click, type,
// press keys, scroll, open the browser and run commands. The model asks for an action with one
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
	{"open", `{"url": "https://..."}`, "open a URL in the browser on the display"},
	{"run", `{"argv": ["ls", "-la"]}`, "run a program in your working folder (an argument list, no shell)"},
	{"wait", `{"seconds": 2}`, "wait for something to load (at most 10 seconds)"},
	{"done", `{"summary": "..."}`, "finish the task and report what you achieved"},
}

// Catalogue renders the tools for the system prompt.
func Catalogue() string {
	var b strings.Builder
	b.WriteString("You can act on your display by writing lines of the form `@tool <name> <json>` in your reply, " +
		"one action per line, in order. After the actions run you receive their results and a screenshot, " +
		"and you continue. Write `@tool done {\"summary\": \"...\"}` when the task is finished, and write " +
		"no @tool line at all when you only want to talk. Coordinates are pixels of the 1280x800 display, " +
		"origin top-left. The tools:\n")
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
	case "screenshot", "done", "wait":
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
	case "open":
		url := str(a.Args, "url", "text")
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return nil, errors.New("url must start with http:// or https://")
		}
		return []string{"kitty-open", url}, nil
	case "run":
		raw, ok := a.Args["argv"].([]any)
		if !ok || len(raw) == 0 {
			if cmd := str(a.Args, "command", "text"); cmd != "" {
				return strings.Fields(cmd), nil
			}
			return nil, errors.New("argv is a list with the program first")
		}
		argv := make([]string, 0, len(raw))
		for _, v := range raw {
			argv = append(argv, fmt.Sprint(v))
		}
		return argv, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", a.Name)
	}
}

// Run executes one action in the kitty's container and returns what it printed.
func Run(ctx context.Context, r Runner, ref string, a Action) (string, error) {
	argv, err := Argv(a)
	if err != nil {
		return "", err
	}
	switch a.Name {
	case "screenshot", "done":
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
	if a.Name == "open" {
		sleep(ctx, 2*time.Second)
	}
	return out, nil
}
