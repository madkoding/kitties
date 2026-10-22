// Package demo lets a person try the whole interface without an AI key or a container runtime:
// a brain that answers in character and runs a scripted desktop task, and a desktop that draws
// its own screenshots. `kitties gateway -demo` and `kitties chat -demo` use it.
package demo

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"github.com/madkoding/kitties/internal/container"
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
		if m.Role == "user" {
			last = m.Content
		}
	}
	desktop := strings.Contains(system, "@tool done")
	lower := strings.ToLower(last)
	usage := provider.Usage{Input: len(system) / 4, Output: 40}
	switch {
	case strings.HasPrefix(last, "Results of your actions") || strings.HasPrefix(last, "[desktop] Results"):
		return provider.Reply{Content: b.nextRound(last), Usage: usage}, nil
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
		return provider.Reply{Content: "Here is how I would split it:\n\n1. Research the options and note the trade-offs.\n2. Draft the proposal with the numbers behind each choice.\n3. Review it together before sending.\n\nI can take the first step on my desktop if you want.\n" +
			"@skill read report-findings\n@artifact save {\"name\": \"launch-plan.md\", \"content\": \"# Launch plan\\n\\n1. Research the options and note the trade-offs.\\n2. Draft the proposal with the numbers behind each choice.\\n3. Review it together before sending.\"}", Usage: usage}, nil
	default:
		return provider.Reply{Content: fmt.Sprintf("Good question. In short: %s. Tell me if you want me to dig deeper or try it on my desktop.", summarise(last)), Usage: usage}, nil
	}
}

// nextRound scripts the desktop task: a click, then done.
func (b *Brain) nextRound(results string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := "task"
	b.rounds[key]++
	switch b.rounds[key] % 3 {
	case 1:
		return "The page is up. I can see the heading and a link; I will click it.\n@tool click {\"x\": 640, \"y\": 420}\n@tool wait {\"seconds\": 1}"
	case 2:
		return "@tool scroll {\"direction\": \"down\", \"amount\": 3}\n@tool screenshot {}"
	default:
		_ = results
		return "@tool done {\"summary\": \"Opened the site, followed the first link and read the page. It is a placeholder domain with one paragraph and a link to the IANA page about reserved domains.\"}"
	}
}

func summarise(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "]: "); i >= 0 {
		s = s[i+3:]
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return "it depends on what you need from \"" + s + "\""
}

// Desktop is a container runtime that keeps everything in memory and draws its screenshots.
type Desktop struct {
	mu      sync.Mutex
	running map[string]bool
	opened  map[string]string
	clicks  map[string]int
}

// NewDesktop creates the demo runtime.
func NewDesktop() *Desktop {
	return &Desktop{running: map[string]bool{}, opened: map[string]string{}, clicks: map[string]int{}}
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
	default:
		return "(demo) " + strings.Join(argv, " ") + "\n", nil
	}
}

// Screenshot implements orchestrator.Containers: a drawing of a browser window.
func (d *Desktop) Screenshot(_ context.Context, name string) ([]byte, error) {
	d.mu.Lock()
	url, clicks := d.opened[name], d.clicks[name]
	d.mu.Unlock()
	return Render(url, clicks), nil
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
