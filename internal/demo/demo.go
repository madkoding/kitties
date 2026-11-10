// Package demo lets a person try the whole interface without an AI key or a container runtime:
// a brain that answers in character and runs a scripted desktop task, and a desktop that draws
// its own screenshots. `kitties gateway -demo` and `kitties chat -demo` use it.
package demo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

// Brain answers like a kitty would, without any model behind it.
type Brain struct {
	mu     sync.Mutex
	rounds map[string]int // desktop task rounds in flight, by the first user line
}

// NewBrain creates the demo brain.
func NewBrain() *Brain { return &Brain{rounds: map[string]int{}} }

// Describe implements provider.Client.
func (b *Brain) Describe() string { return "demo/scripted" }

// Complete implements provider.Client: a canned conversation, with a three-round desktop task
// whenever the person asks to open something and the kitty has a desktop.
func (b *Brain) Complete(_ context.Context, messages []provider.Message) (provider.Reply, error) {
	system, last, name := "", "", "Kitty"
	for _, m := range messages {
		if m.Role == "system" {
			system = m.Content
			if i := strings.Index(system, "You are "); i >= 0 {
				rest := system[i+len("You are "):]
				if j := strings.IndexAny(rest, ",\n"); j > 0 {
					name = rest[:j]
				}
			}
		}
		if m.Role == "user" && !m.Volatile {
			last = m.Content
		}
	}
	desktop := strings.Contains(system, "@tool done")
	lower := strings.ToLower(last)
	usage := provider.Usage{Input: len(system) / 4, Output: 40}
	switch {
	case strings.Contains(system, "You keep the memory of a kitty"):
		// The kitty consolidates what it learned: the corrections and likes it was given, as notes.
		return provider.Reply{Content: distilled(last), Usage: usage}, nil
	case strings.Contains(system, "You name a conversation"):
		return provider.Reply{Content: named(last), Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "proposed: the person"):
		return provider.Reply{Content: "We changed course, so I proposed a new description for this chat: accept it above if it fits.", Usage: usage}, nil
	case strings.Contains(lower, "change of plans:"):
		// The chat changes course: the kitty proposes a new description for it.
		i := strings.Index(lower, "change of plans:") + len("change of plans:")
		return provider.Reply{Content: "@describe propose " + strings.TrimSpace(last[i:]), Usage: usage}, nil
	case strings.Contains(system, "You keep the running summary"):
		return provider.Reply{Content: "The person and the kitties have been planning the launch: the plan is drafted, the numbers are being checked and the announcement is pending.", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "held: the person has to approve"):
		return provider.Reply{Content: "That needs your yes first, so I left it waiting for your approval. Once you approve it goes out, and I will see the result here.", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "noted ("):
		return provider.Reply{Content: "Got it, I will remember that.", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "- @limit "):
		return provider.Reply{Content: "Done: from now on I ask you before posting to the team.", Usage: usage}, nil
	case strings.Contains(lower, "ask me before posting"):
		// Asked to hold back, the kitty limits itself.
		return provider.Reply{Content: "Sure.\n@limit set {\"connector\": \"team-slack\", \"action\": \"send\", \"mode\": \"ask\"}", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "proposed to every kitty"):
		return provider.Reply{Content: "I proposed it for every kitty: it reaches them once you say yes in the memory.", Usage: usage}, nil
	case strings.Contains(lower, "tell every kitty that "):
		text := strings.TrimSpace(last[strings.Index(lower, "tell every kitty that ")+len("tell every kitty that "):])
		return provider.Reply{Content: fmt.Sprintf("Sure.\n@memory remember {\"text\": %q, \"scope\": \"shared\"}", text), Usage: usage}, nil
	case strings.Contains(lower, "remember that "):
		text := strings.TrimSpace(last[strings.Index(lower, "remember that ")+len("remember that "):])
		return provider.Reply{Content: fmt.Sprintf("Noted.\n@memory remember {\"text\": %q}", text), Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "- @delegate "):
		who := strings.TrimSpace(strings.SplitN(strings.SplitN(last, "- @delegate ", 2)[1], " ", 2)[0])
		return provider.Reply{Content: fmt.Sprintf("%s took care of it and reported back; I checked the result and it looks right. Anything else?", who), Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "- @connector "):
		// The service answered: say so, in the connector's own words.
		if strings.Contains(last, "failed:") {
			return provider.Reply{Content: "I tried to post it but the service refused; the error is in the step above. Want me to try another channel?", Usage: usage}, nil
		}
		return provider.Reply{Content: "Posted! The announcement is out, with the three points we agreed on. I will watch for replies.", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && !strings.Contains(last, "- @tool ") && (strings.Contains(last, "- @skill ") || strings.Contains(last, "- @artifact ")):
		// The library answered: the plan was saved, the procedure read.
		return provider.Reply{Content: "Saved the plan to the library as **launch-plan.md** so the team can pick it up, and I followed the report-findings skill for the format.", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") && strings.Contains(last, "- @tool ask_help"):
		// The person took the desktop for the password and gave it back.
		return provider.Reply{Content: "@tool done {\"summary\": \"Signed in to the shop: thank you for the password. Your order history is open on my desktop.\"}", Usage: usage}, nil
	case strings.HasPrefix(last, "Results of your actions") || strings.HasPrefix(last, "[desktop] Results"):
		return provider.Reply{Content: b.nextRound(last), Usage: usage}, nil
	case strings.Contains(last, "Goal: "):
		// The scheduler woke the kitty up for a goal of its own.
		title := strings.TrimSpace(strings.SplitN(strings.SplitN(last, "Goal: ", 2)[1], "\n", 2)[0])
		if strings.Contains(last, "an event from ") {
			return provider.Reply{Content: fmt.Sprintf("Something arrived for **%s**. I read it: it needs a short reply, which I drafted and left in the thread. Nothing else is pending.", title), Usage: usage}, nil
		}
		return provider.Reply{Content: fmt.Sprintf("Checked **%s**: nothing new since last time. Next check as scheduled; nothing is blocked.", title), Usage: usage}, nil
	case strings.Contains(last, "[Attached file: "):
		return provider.Reply{Content: attachedReply(last), Usage: usage}, nil
	case peerAsked(system, lower) != "":
		name := peerAsked(system, lower)
		task := strings.TrimSpace(last[strings.Index(lower, name)+len(name):])
		task = strings.TrimPrefix(strings.TrimPrefix(task, " to "), "to ")
		if task == "" {
			task = "help with this"
		}
		return provider.Reply{Content: fmt.Sprintf("Good idea, %s is better placed for that. I will ask.\n@delegate %s {\"task\": %q}", name, name, task), Usage: usage}, nil
	case firstConnector(system) != "" && (strings.Contains(lower, "post ") || strings.Contains(lower, "announce") || strings.Contains(lower, "publica") || strings.Contains(lower, "share ")):
		name := firstConnector(system)
		return provider.Reply{Content: fmt.Sprintf("Sure, I will post it through %s.\n@connector %s {\"text\": \"Kitties launches next week: autonomous kitties with their own desktops, a connector for every service and a library of skills.\"}", name, name), Usage: usage}, nil
	case desktop && (strings.Contains(lower, "sign in") || strings.Contains(lower, "log in") || strings.Contains(lower, "inicia sesi")):
		return provider.Reply{Content: "The shop is open on my desktop, but its sign-in asks for your password, which I do not have.\n@tool ask_help {\"reason\": \"the shop's sign-in needs your password\"}", Usage: usage}, nil
	case desktop && (strings.Contains(lower, "open") || strings.Contains(lower, "abre") || strings.Contains(lower, "browse") || strings.Contains(lower, "visit")):
		url := "https://example.com"
		for _, w := range strings.Fields(last) {
			if strings.HasPrefix(w, "http") {
				url = strings.Trim(w, ".,")
			}
		}
		return provider.Reply{Content: fmt.Sprintf("On it! I will open %s on my desktop, have a look and tell you what I find.\n@tool open {\"url\": %q}\n@tool wait {\"seconds\": 1}", url, url), Usage: usage}, nil
	case strings.Contains(lower, "hello") || strings.Contains(lower, "hola") || strings.Contains(lower, "hi "):
		return provider.Reply{Content: fmt.Sprintf("Hello! I am %s. Ask me anything, or tell me to open a site and I will do it on my own desktop.", name), Usage: usage}, nil
	case strings.HasPrefix(last, "[skill ") || strings.HasPrefix(last, "[artifact ") || strings.HasPrefix(last, "[skills]") || strings.HasPrefix(last, "[artifacts]"):
		// The library answered: the plan was saved, the procedure read.
		return provider.Reply{Content: "Saved the plan to the library as **launch-plan.md** so the team can pick it up, and I followed the report-findings skill for the format.", Usage: usage}, nil
	case strings.Contains(lower, "@") && !strings.Contains(lower, "@connector"):
		return provider.Reply{Content: fmt.Sprintf("%s here. I agree with the point above and I would add one check: let's confirm the numbers before we commit.", name), Usage: usage}, nil
	case strings.Contains(lower, "plan"):
		return provider.Reply{Content: "Here is how I would split it:\n\n1. Research the options and note the trade-offs.\n2. Draft the proposal with the numbers behind each choice.\n3. Review it together before sending.\n\nThe plan is saved in [launch-plan.md](/v1/artifacts/launch-plan.md). I can take the first step on my desktop if you want.\n" +
			"@skill read report-findings\n@artifact save {\"name\": \"launch-plan.md\", \"content\": \"# Launch plan\\n\\n1. Research the options and note the trade-offs.\\n2. Draft the proposal with the numbers behind each choice.\\n3. Review it together before sending.\"}", Usage: usage}, nil
	default:
		return provider.Reply{Content: fmt.Sprintf("Good question. In short: %s. Tell me if you want me to dig deeper or try it on my desktop.", summarise(last)), Usage: usage}, nil
	}
}

// distilled writes the notes a consolidation answers with: what the person said in their
// feedback, and the notes the kitty already had, as a JSON list.
func distilled(prompt string) string {
	var notes []string
	seen := map[string]bool{}
	add := func(scope, text string) {
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			return
		}
		seen[text] = true
		notes = append(notes, fmt.Sprintf("{\"scope\": %q, \"text\": %q}", scope, text))
	}
	for _, line := range strings.Split(prompt, "\n") {
		switch {
		case strings.HasPrefix(line, "[user] "):
			add("user", strings.TrimPrefix(line, "[user] "))
		case strings.HasPrefix(line, "[work] "):
			add("work", strings.TrimPrefix(line, "[work] "))
		case strings.HasPrefix(line, "- down") && strings.Contains(line, "the person said: "):
			if said := strings.TrimSpace(line[strings.Index(line, "the person said: ")+len("the person said: "):]); said != "" {
				add("user", "Correction: "+said)
			}
		}
	}
	return "[" + strings.Join(notes, ", ") + "]"
}

// peerAsked is the other kitty the person asked this one to involve ("ask luna to ..."),
// when the prompt lists it as a peer; "" otherwise.
func peerAsked(system, lower string) string {
	i := strings.Index(system, "hand a subtask to: ")
	if i < 0 || !strings.Contains(lower, "ask ") {
		return ""
	}
	list := system[i+len("hand a subtask to: "):]
	if j := strings.Index(list, "."); j >= 0 {
		list = list[:j]
	}
	for _, name := range strings.Split(list, ", ") {
		name = strings.TrimSpace(name)
		if name != "" && strings.Contains(lower, "ask "+name) {
			return name
		}
	}
	return ""
}

// firstConnector is the service the system prompt lists ("- name: what it does") to post
// through: one for messages (slack, discord...) when there is one, the first otherwise; "".
func firstConnector(system string) string {
	i := strings.Index(system, "@connector <name>")
	if i < 0 {
		return ""
	}
	first := ""
	for _, line := range strings.Split(system[i:], "\n")[1:] {
		if !strings.HasPrefix(line, "- ") {
			if strings.TrimSpace(line) != "" && !strings.Contains(line, "@connector") {
				break
			}
			continue
		}
		j := strings.Index(line, ":")
		if j <= 2 {
			continue
		}
		name := line[2:j]
		if first == "" {
			first = name
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "slack") || strings.Contains(lower, "discord") || strings.Contains(lower, "telegram") || strings.Contains(lower, "channel") {
			return name
		}
	}
	return first
}

// nextRound scripts the desktop task: a click, then done.
func (b *Brain) nextRound(results string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := "task"
	b.rounds[key]++
	switch b.rounds[key] % 3 {
	case 1:
		return "I follow the page's link to see where it leads.\n@tool click {\"x\": 640, \"y\": 420}\n@tool wait {\"seconds\": 1}"
	case 2:
		return "I scroll down to read the rest of the page.\n@tool scroll {\"direction\": \"down\", \"amount\": 3}\n@tool screenshot {}"
	default:
		_ = results
		return "@tool done {\"summary\": \"Opened the site, followed the first link and read the page. It is a placeholder domain with one paragraph and a link to the IANA page about reserved domains.\"}"
	}
}

// attachedReply is the demo kitty's answer to a message with files: it names what it received.
func attachedReply(last string) string {
	var names []string
	for _, part := range strings.Split(last, "[Attached file: ")[1:] {
		if i := strings.Index(part, " ("); i > 0 {
			names = append(names, "**"+part[:i]+"**")
		}
	}
	return fmt.Sprintf("Got it, I have %s. I read through %s and I can work from here: tell me what you want done with it.",
		strings.Join(names, " and "), map[bool]string{true: "it", false: "them"}[len(names) == 1])
}

func summarise(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "]: "); i >= 0 {
		s = s[i+3:]
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return "it depends on what you need from “" + strings.ReplaceAll(s, "\"", "'") + "”"
}

// Desktop is a container runtime that keeps everything in memory and draws its screenshots.
type Desktop struct {
	mu      sync.Mutex
	running map[string]bool
	opened  map[string]string
	clicks  map[string]int
	clip    map[string]string    // the clipboard of each display
	visited map[string]time.Time // when kitty-refresh last visited the tabs, by display
}

// NewDesktop creates the demo runtime.
func NewDesktop() *Desktop {
	return &Desktop{running: map[string]bool{}, opened: map[string]string{}, clicks: map[string]int{}, clip: map[string]string{}, visited: map[string]time.Time{}}
}

// Up implements orchestrator.Containers.
func (d *Desktop) Up(_ context.Context, s container.Spec) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running[s.Name] = true
	return "demo-" + s.Name, nil
}

// Start implements orchestrator.Containers.
func (d *Desktop) Start(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running[name] = true
	return nil
}

// Stop implements orchestrator.Containers.
func (d *Desktop) Stop(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running[name] = false
	return nil
}

// Remove implements orchestrator.Containers.
func (d *Desktop) Remove(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.running, name)
	return nil
}

// List implements orchestrator.Containers.
func (d *Desktop) List(context.Context) ([]container.Info, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []container.Info
	for n, r := range d.running {
		out = append(out, container.Info{ID: "demo-" + n, Name: n, Image: "demo", Status: map[bool]string{true: "Up", false: "Exited"}[r], Running: r})
	}
	return out, nil
}

// Exec implements orchestrator.Containers: it remembers what was opened and clicked.
func (d *Desktop) Exec(_ context.Context, name string, argv ...string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(argv) == 0 {
		return "", nil
	}
	switch argv[0] {
	case "kitty-open":
		if len(argv) > 1 {
			d.opened[name] = argv[1]
		}
		return "opened " + strings.Join(argv[1:], " "), nil
	case "xdotool":
		if len(argv) > 4 && argv[4] == "click" {
			d.clicks[name]++
		}
		return "", nil
	case "kitty-clip":
		if len(argv) > 2 && argv[1] == "set" {
			d.clip[name] = argv[2]
			return "set", nil
		}
		return d.clip[name], nil
	case "cat":
		if len(argv) == 2 && argv[1] == kitty.BrowserVisitsFile {
			// A drawn browser whose tabs were visited a few minutes ago, or just now when asked.
			if at, ok := d.visited[name]; ok {
				return fmt.Sprintf(`{"at": %d, "tabs": 3, "reloaded": 3, "quiet": 0, "kept": 0, "skipped": 0}`, at.Unix()), nil
			}
			return fmt.Sprintf(`{"at": %d, "tabs": 3, "reloaded": 2, "quiet": 1, "kept": 0, "skipped": 0}`, time.Now().Add(-4*time.Minute).Unix()), nil
		}
		return "(demo) " + strings.Join(argv, " ") + "\n", nil
	case "kitty-refresh":
		d.visited[name] = time.Now()
		return "(demo) the tabs were visited again\n", nil
	case "kitty-theme":
		return "theme " + strings.Join(argv[1:], " "), nil
	case "sh":
		if len(argv) == 3 && argv[2] == kitty.MemoryScript {
			// A drawn machine of 2 GB with the browser open and a little swap in use.
			return "memory.max=2147483648\nmemory.current=1288490188\nmemory.swap.max=1073741824\nmemory.swap.current=52428800\n" +
				"inactive_file 134217728\nMemTotal: 16384000 kB\nMemAvailable: 9000000 kB\nSwapTotal: 4096000 kB\nSwapFree: 4000000 kB\n" +
				"--- ps\n 307200 chromium\n 204800 chromium\n 153600 chromium\n 102400 Xvfb\n 61440 lumen-wm\n 40960 pcmanfm\n", nil
		}
		return "(demo) " + strings.Join(argv, " ") + "\n", nil
	case "kitty-backup":
		// What a backup of a drawn desktop holds: nothing real, but sealed and kept like the real one.
		return base64.StdEncoding.EncodeToString([]byte("demo sessions of "+name)) + "\n", nil
	default:
		return "(demo) " + strings.Join(argv, " ") + "\n", nil
	}
}

// SetLimits takes any RAM and swap: a drawn desktop holds nothing.
func (d *Desktop) SetLimits(context.Context, string, int, int) error { return nil }

// LimitsStale says a drawn desktop always has its limits.
func (d *Desktop) LimitsStale(context.Context, string, int, int) (bool, error) { return false, nil }

// EngineMemoryMB is the memory of a drawn engine.
func (d *Desktop) EngineMemoryMB(context.Context) (int, error) { return 16384, nil }

// Pull implements orchestrator.Containers: a drawn desktop has no image to download.
func (d *Desktop) Pull(context.Context, string) error { return nil }

// ImageStale implements orchestrator.Containers: a drawn desktop is always the newest.
func (d *Desktop) ImageStale(context.Context, string, string) (bool, error) { return false, nil }

// ExecInput implements orchestrator.Containers: the demo types nothing, so the text only has to
// reach no command line.
func (d *Desktop) ExecInput(ctx context.Context, name, _ string, argv ...string) (string, error) {
	return d.Exec(ctx, name, argv...)
}

// Screenshot implements orchestrator.Containers: a drawing of a browser window.
func (d *Desktop) Screenshot(_ context.Context, name string) ([]byte, error) {
	d.mu.Lock()
	url, clicks := d.opened[name], d.clicks[name]
	d.mu.Unlock()
	return Render(url, clicks), nil
}

// streamEvery is how often the demo's stream draws its display again.
var streamEvery = 300 * time.Millisecond

// Stream implements orchestrator.Containers: the drawing, again whenever it changed.
func (d *Desktop) Stream(ctx context.Context, name string, _, _ int) (io.ReadCloser, error) {
	r, w := io.Pipe()
	go func() {
		var last []byte
		t := time.NewTicker(streamEvery)
		defer t.Stop()
		for {
			frame, _ := d.Screenshot(ctx, name)
			if !bytes.Equal(frame, last) {
				if err := container.WriteFrame(w, frame); err != nil {
					return
				}
				last = frame
			} else if _, err := io.WriteString(w, "PING\n"); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				_ = w.CloseWithError(ctx.Err())
				return
			case <-t.C:
			}
		}
	}()
	return r, nil
}

// Tunnel implements orchestrator.Containers: the demo has no voxl, so the live view falls back
// to its pictures.
func (d *Desktop) Tunnel(context.Context, string, int) (net.Conn, error) {
	return nil, errors.New("the demo desktop has no live stream")
}

// Render draws a 1280x800 desktop with a browser window showing url (nothing when empty).
func Render(url string, clicks int) []byte {
	w, h := 1280, 800
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, img.Bounds(), color.RGBA{0x1e, 0x22, 0x2b, 0xff}) // the desktop
	// A taskbar.
	fill(img, image.Rect(0, h-36, w, h), color.RGBA{0x12, 0x14, 0x1a, 0xff})
	fill(img, image.Rect(10, h-28, 30, h-8), color.RGBA{0x00, 0xf0, 0xff, 0xff})
	if url == "" {
		return encode(img)
	}
	// The browser window: frame, tab bar, address bar, page.
	win := image.Rect(60, 40, w-60, h-70)
	fill(img, win, color.RGBA{0xf4, 0xf4, 0xf6, 0xff})
	fill(img, image.Rect(win.Min.X, win.Min.Y, win.Max.X, win.Min.Y+38), color.RGBA{0xdd, 0xdd, 0xe2, 0xff})
	fill(img, image.Rect(win.Min.X+12, win.Min.Y+8, win.Min.X+200, win.Min.Y+38), color.RGBA{0xf4, 0xf4, 0xf6, 0xff})
	fill(img, image.Rect(win.Min.X, win.Min.Y+38, win.Max.X, win.Min.Y+72), color.RGBA{0xf4, 0xf4, 0xf6, 0xff})
	fill(img, image.Rect(win.Min.X+90, win.Min.Y+44, win.Max.X-20, win.Min.Y+66), color.RGBA{0xff, 0xff, 0xff, 0xff})
	// The URL as a row of dark blocks (no font in the standard library).
	x := win.Min.X + 100
	for i := 0; i < len(url) && x < win.Max.X-40; i++ {
		if url[i] != '/' && url[i] != '.' {
			fill(img, image.Rect(x, win.Min.Y+50, x+6, win.Min.Y+60), color.RGBA{0x33, 0x33, 0x3a, 0xff})
		}
		x += 8
	}
	// The page: a heading, two paragraphs, one link; the link turns visited after a click.
	fill(img, image.Rect(win.Min.X+140, win.Min.Y+140, win.Min.X+620, win.Min.Y+176), color.RGBA{0x20, 0x20, 0x28, 0xff})
	for row := 0; row < 4; row++ {
		y := win.Min.Y + 210 + row*22
		fill(img, image.Rect(win.Min.X+140, y, win.Max.X-180-row*37, y+10), color.RGBA{0x8a, 0x8a, 0x95, 0xff})
	}
	linkColor := color.RGBA{0x1a, 0x5f, 0xd6, 0xff}
	if clicks > 0 {
		linkColor = color.RGBA{0x6a, 0x35, 0xc8, 0xff}
	}
	fill(img, image.Rect(win.Min.X+140, win.Min.Y+330, win.Min.X+360, win.Min.Y+342), linkColor)
	if clicks > 0 {
		// A second page below after the click: more lines.
		for row := 0; row < 5; row++ {
			y := win.Min.Y + 390 + row*22
			fill(img, image.Rect(win.Min.X+140, y, win.Max.X-200-row*23, y+10), color.RGBA{0x8a, 0x8a, 0x95, 0xff})
		}
	}
	// The mouse pointer.
	px, py := 640, 420
	fill(img, image.Rect(px, py, px+3, py+16), color.RGBA{0x00, 0x00, 0x00, 0xff})
	fill(img, image.Rect(px, py, px+12, py+3), color.RGBA{0x00, 0x00, 0x00, 0xff})
	return encode(img)
}

func fill(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// named is the title and description the demo kitty gives a chat: its first message, cut to a
// few words, and a sentence on what it is for.
func named(chat string) string {
	first := ""
	if i := strings.Index(chat, "The conversation:\n"); i >= 0 {
		for _, line := range strings.Split(chat[i:], "\n")[1:] {
			if j := strings.Index(line, "]: "); strings.HasPrefix(line, "[") && j > 0 {
				first = line[j+3:]
				break
			}
		}
	}
	words := strings.Fields(strings.Trim(first, ".!?"))
	if len(words) > 7 {
		words = words[:7]
	}
	title := strings.Join(words, " ")
	if title == "" {
		title = "A chat with the kitties"
	}
	title = strings.ToUpper(title[:1]) + title[1:]
	out, _ := json.Marshal(map[string]string{"title": title,
		"description": "Work through " + strings.ToLower(title[:1]) + title[1:] + " with the kitties: what is decided stays, and the next step is pending."})
	return string(out)
}
