package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/kitty"
)

func TestHoldAndInput(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	no := false
	brain, _ := m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	if m.Held(k.ID) {
		t.Error("held from the start")
	}
	if on, err := m.Hold("mimi", true); !on || err != nil || !m.Held(k.ID) {
		t.Errorf("hold = %v %v", on, err)
	}
	took := m.HoldStarted(k.ID)
	if took.IsZero() {
		t.Error("the hold's start is kept")
	}
	if on, err := m.Hold("mimi", false); on || err != nil || m.Held(k.ID) {
		t.Errorf("give back = %v %v", on, err)
	}
	if !m.HoldStarted(k.ID).Equal(took) {
		t.Error("the start outlives the hold")
	}
	if _, err := m.Hold("brain", true); err == nil || m.Held(brain.ID) {
		t.Error("a kitty without a desktop cannot be held")
	}
	if _, err := m.Hold("nobody", true); err == nil {
		t.Error("unknown kitty")
	}
	// The person's click runs, and takes the display over.
	if err := m.Input(ctx, "mimi", desktop.Action{Name: "click", Args: map[string]any{"x": 5.0, "y": 6.0}}); err != nil {
		t.Fatal(err)
	}
	if !m.Held(k.ID) || !strings.Contains(strings.Join(f.calls, " "), "exec") {
		t.Errorf("input = held %v, calls %v", m.Held(k.ID), f.calls)
	}
	// What the person types may be a password: it goes in on the input, never in the arguments.
	if err := m.Input(ctx, "mimi", desktop.Action{Name: "type", Args: map[string]any{"text": "hunter2"}}); err != nil {
		t.Fatal(err)
	}
	if len(f.inputs) != 1 || f.inputs[0] != "kitty-mimi:hunter2" || strings.Contains(strings.Join(f.execs, " "), "hunter2") {
		t.Errorf("typed: inputs %v execs %v", f.inputs, f.execs)
	}
	if _, err := m.ExecInput(ctx, "brain", "x", "ls"); err == nil {
		t.Error("a kitty without a desktop has no container")
	}
	if _, err := m.ExecInput(ctx, "nobody", "x", "ls"); err == nil {
		t.Error("unknown kitty")
	}
	if err := m.Input(ctx, "mimi", desktop.Action{Name: "run", Args: map[string]any{"argv": []any{"rm"}}}); err == nil {
		t.Error("the person's input runs no programs")
	}
	if err := m.Input(ctx, "nobody", desktop.Action{Name: "key", Args: map[string]any{"keys": "a"}}); err == nil {
		t.Error("unknown kitty input")
	}
	// A hold left alone ends by itself.
	now = now.Add(holdIdle + time.Second)
	if m.Held(k.ID) {
		t.Error("an idle hold ends")
	}
}

func TestStreamClipboardAndTheme(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	no := false
	_, _ = m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	if rc, err := m.Stream(ctx, "mimi", 8, 70); err != nil || rc == nil {
		t.Errorf("stream = %v", err)
	}
	if _, err := m.Stream(ctx, "brain", 8, 70); err == nil {
		t.Error("a brain-only kitty has no display to stream")
	}
	if _, err := m.Stream(ctx, "nobody", 8, 70); err == nil {
		t.Error("unknown kitty")
	}
	if out, err := m.Clipboard(ctx, "mimi"); err != nil || out != k.Container+":kitty-clip get" {
		t.Errorf("clipboard = %q %v", out, err)
	}
	if err := m.SetClipboard(ctx, "mimi", "hola"); err != nil || f.execs[len(f.execs)-1] != k.Container+":kitty-clip set hola" {
		t.Errorf("set clipboard = %v %v", err, f.execs)
	}
	if err := m.SetClipboard(ctx, "mimi", strings.Repeat("a", MaxClipboard+1)); err == nil {
		t.Error("too long a text")
	}
	// A theme: refused when unknown, saved and put on the desktop at once, and again on start.
	bad := "neon"
	if _, err := m.Edit("mimi", Update{Theme: &bad}); err == nil {
		t.Error("an unknown theme")
	}
	light := "Greybird-dark"
	got, err := m.Edit("mimi", Update{Theme: &light})
	if err != nil || got.Theme != "Greybird-dark" || f.execs[len(f.execs)-1] != k.Container+":kitty-theme Greybird-dark" {
		t.Fatalf("theme = %+v %v %v", got, err, f.execs)
	}
	f.execs = nil
	if err := m.Start(ctx, "mimi"); err != nil || len(f.execs) != 2 || f.execs[0] != k.Container+":kitty-theme Greybird-dark" || f.execs[1] != k.Container+":kitty-ready" {
		t.Errorf("start = %v %v", err, f.execs)
	}
	// Back to the image's own theme: Numix goes on the running desktop again.
	own := ""
	f.execs = nil
	if got, err := m.Edit("mimi", Update{Theme: &own}); err != nil || got.Theme != "" || len(f.execs) != 1 || f.execs[0] != k.Container+":kitty-theme Numix" {
		t.Errorf("own theme = %+v %v %v", got, err, f.execs)
	}
}

// A start with the image's own theme runs nothing more than the look at whether its desktop is up.
func TestStartWithoutTheme(t *testing.T) {
	f := newFake()
	m, _ := New(testConfig(t), WithContainers(f, nil))
	_, _ = m.Create(context.Background(), Spec{Name: "mimi"})
	if err := m.Start(context.Background(), "mimi"); err != nil || len(f.execs) != 1 || f.execs[0] != "kitty-mimi:kitty-ready" {
		t.Errorf("start = %v %v", err, f.execs)
	}
}

func TestAnyHeld(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	_, _ = m.Create(context.Background(), Spec{Name: "mimi"})
	if m.AnyHeld() {
		t.Error("held from the start")
	}
	_, _ = m.Hold("mimi", true)
	if !m.AnyHeld() {
		t.Error("a held desktop is not seen")
	}
	now = now.Add(holdIdle + time.Second)
	if m.AnyHeld() {
		t.Error("an idle hold counts")
	}
}

func TestVoxl(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	no := false
	_, _ = m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	// A desktop of an older image has no password file: no live view of this kind.
	for _, out := range []string{"", "(demo) cat /tmp/voxl.pw\n", "cat: /tmp/voxl.pw: No such file"} {
		f.outputs = map[string]string{"cat": out}
		if _, err := m.Voxl(ctx, "mimi"); err == nil {
			t.Errorf("%q: a desktop without voxl has no live view", out)
		}
	}
	f.outputs = map[string]string{"cat": "0123456789abcdef0123456789abcdef0123456789abcdef\n"}
	v, err := m.Voxl(ctx, "mimi")
	if err != nil || v.Password != "0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("voxl = %+v %v", v, err)
	}
	if f.execs[len(f.execs)-1] != k.Container+":cat "+VoxlPasswordFile {
		t.Errorf("the password is read with %v", f.execs)
	}
	// The connection goes to voxl's port in this kitty's container, and the machine counts as in
	// use while it is open.
	c, err := v.Dial(ctx)
	if err != nil || !strings.Contains(strings.Join(f.calls, ","), "tunnel "+k.Container+" 6901") {
		t.Fatalf("dial = %v %v", err, f.calls)
	}
	if !watching(m, k.ID) {
		t.Error("an open live view is a person watching")
	}
	_ = c.Close()
	_ = c.Close()
	if watching(m, k.ID) {
		t.Error("a closed live view is not")
	}
	for _, ref := range []string{"brain", "nobody"} {
		if _, err := m.Voxl(ctx, ref); err == nil {
			t.Errorf("%s has no desktop to show", ref)
		}
	}
}

// watching tells whether a person has a live view of the kitty open.
func watching(m *Manager, id string) bool {
	_, streams := m.use.running(id)
	return streams > 0
}

// bootingContainers is a desktop whose voxl writes its password only after a few looks.
type bootingContainers struct {
	*fakeContainers
	looks, ready int
}

func (b *bootingContainers) Exec(ctx context.Context, n string, argv ...string) (string, error) {
	if argv[0] == "cat" {
		b.looks++
		if b.looks <= b.ready {
			return "", errors.New("cat: /tmp/voxl.pw: No such file")
		}
		return "0123456789abcdef0123456789abcdef\n", nil
	}
	return b.fakeContainers.Exec(ctx, n, argv...)
}

// A machine just brought up has not written its voxl password yet: the live view waits for it,
// instead of taking the desktop for one without voxl. Long after the start, it does not wait.
func TestVoxlWaitsForABootingDesktop(t *testing.T) {
	defer func(w, p, win time.Duration) { voxlBootWait, voxlBootPoll, voxlBootWindow = w, p, win }(voxlBootWait, voxlBootPoll, voxlBootWindow)
	voxlBootWait, voxlBootPoll = 50*time.Millisecond, time.Millisecond
	ctx := context.Background()
	b := &bootingContainers{fakeContainers: newFake(), ready: 3}
	m, err := New(testConfig(t), WithContainers(b, nil))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.now = func() time.Time { return now }
	_, _ = m.Create(ctx, Spec{Name: "mimi"})

	// Not started by this gateway: one look, and no live view.
	if _, err := m.Voxl(ctx, "mimi"); err == nil || b.looks != 1 {
		t.Fatalf("not booting: %v after %d looks", err, b.looks)
	}
	// Just started: it waits until the password is there.
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	b.looks = 0
	v, err := m.Voxl(ctx, "mimi")
	if err != nil || v.Password == "" || b.looks != 4 {
		t.Fatalf("booting: %+v %v after %d looks", v, err, b.looks)
	}
	// It gives up after voxlBootWait.
	b.looks, b.ready = 0, 1000
	if _, err := m.Voxl(ctx, "mimi"); err == nil || b.looks != int(voxlBootWait/voxlBootPoll)+1 {
		t.Fatalf("never ready: %v after %d looks", err, b.looks)
	}
	// A cancelled request stops waiting.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Voxl(cctx, "mimi"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	// Long after the start, a missing password is an image without voxl again.
	now = now.Add(voxlBootWindow + time.Second)
	b.looks = 0
	if _, err := m.Voxl(ctx, "mimi"); err == nil || b.looks != 1 {
		t.Errorf("long after the start: %v after %d looks", err, b.looks)
	}
}

// readyContainers is a desktop whose kitty-ready says booting a few times, then ready.
type readyContainers struct {
	*fakeContainers
	asks, booting int
	err           error
	// during is called on each ask while it is booting.
	during func()
}

func (r *readyContainers) Exec(ctx context.Context, n string, argv ...string) (string, error) {
	if argv[0] == "kitty-ready" {
		r.asks++
		if r.err != nil {
			return "", r.err
		}
		if r.asks <= r.booting {
			if r.during != nil && r.asks > 1 {
				r.during()
			}
			return "booting\n", nil
		}
		return "ready\n", nil
	}
	return r.fakeContainers.Exec(ctx, n, argv...)
}

// A start calls the machine on only once its desktop is up, and shows it as starting its desktop
// meanwhile; an image without kitty-ready is not waited for, nor is one that never comes up for
// longer than desktopReadyWait.
func TestStartWaitsForTheDesktop(t *testing.T) {
	defer func(w, p time.Duration) { desktopReadyWait, desktopReadyPoll = w, p }(desktopReadyWait, desktopReadyPoll)
	desktopReadyWait, desktopReadyPoll = time.Second, time.Millisecond
	ctx := context.Background()
	r := &readyContainers{fakeContainers: newFake(), booting: 3}
	m, err := New(testConfig(t), WithContainers(r, nil))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.Create(ctx, Spec{Name: "mimi"})

	// While it waits, the machine is starting its desktop.
	var seen []string
	r.during = func() {
		got, _ := m.Get("mimi")
		p, _ := m.Power("mimi")
		seen = append(seen, got.Status+"/"+p.Step)
	}
	if err := m.Start(ctx, "mimi"); err != nil || r.asks != 4 {
		t.Fatalf("start = %v after %d asks", err, r.asks)
	}
	if got, _ := m.Get("mimi"); got.Status != kitty.StatusRunning {
		t.Errorf("after the wait: %s", got.Status)
	}
	if p, _ := m.Power("mimi"); p.State != kitty.PowerOn || p.Step != "" {
		t.Errorf("power after the wait = %+v", p)
	}
	if strings.Join(seen, ",") != "starting/desktop,starting/desktop" {
		t.Errorf("while booting: %v", seen)
	}
	r.during = nil

	// Never ready: called on anyway after the wait.
	r.asks, r.booting = 0, 1<<30
	desktopReadyWait = 20 * time.Millisecond
	if err := m.Start(ctx, "mimi"); err != nil || r.asks < 2 {
		t.Errorf("never ready = %v after %d asks", err, r.asks)
	}
	// No kitty-ready (an older image): one ask, no wait.
	r.asks, r.err = 0, errors.New("kitty-ready: not found")
	if err := m.Start(ctx, "mimi"); err != nil || r.asks != 1 {
		t.Errorf("older image = %v after %d asks", err, r.asks)
	}
	// A start that is given up while it waits fails, with the reason on the kitty.
	r.asks, r.err, r.booting = 0, nil, 1<<30
	desktopReadyWait = time.Minute
	cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := m.Start(cctx, "mimi"); err == nil || !strings.Contains(err.Error(), "did not come up") {
		t.Errorf("given up = %v", err)
	}
	if got, _ := m.Get("mimi"); got.Status != kitty.StatusError {
		t.Errorf("given up: %s", got.Status)
	}
}
