package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/replay"
	"github.com/madkoding/kitties/internal/soul"
)

// deskFleet is a fleet that can also run desktop actions.
type deskFleet struct {
	*fakeFleet
	execs   []string
	shotErr error
	shots   int
	ocr     string   // what kitty-ocr prints
	inputs  []string // what was given to commands on their input
	png     []byte   // the screenshot, when it has to be a real picture
	execErr error
	block   chan struct{} // when set, Exec waits on it (to test cancel)
}

func (d *deskFleet) Exec(ctx context.Context, ref string, argv ...string) (string, error) {
	d.execs = append(d.execs, ref+":"+strings.Join(argv, " "))
	if d.block != nil {
		select {
		case <-d.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if len(argv) > 0 && argv[0] == "kitty-ocr" {
		return d.ocr, nil
	}
	return "ok", d.execErr
}
func (d *deskFleet) ExecInput(ctx context.Context, ref, stdin string, argv ...string) (string, error) {
	d.inputs = append(d.inputs, ref+":"+stdin)
	return d.Exec(ctx, ref, argv...)
}
func (d *deskFleet) Screenshot(context.Context, string) ([]byte, error) {
	d.shots++
	if d.png != nil {
		return d.png, d.shotErr
	}
	return []byte("PNGDATA"), d.shotErr
}

func deskService(t *testing.T, replies ...string) (*Service, *deskFleet, *scripted) {
	t.Helper()
	f, sa, _, _ := fleet(t)
	d := &deskFleet{fakeFleet: f}
	s := service(t, d)
	sa.replies = replies
	return s, d, sa
}

func waitDone(t *testing.T, s *Service, id string) *Session {
	t.Helper()
	s.Wait()
	sess, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Working() {
		t.Fatal("the session is still working")
	}
	return sess
}

func TestDesktopTaskRunsInBackground(t *testing.T) {
	s, d, sa := deskService(t,
		"Opening the site.\n@tool open {\"url\": \"https://example.com\"}\n@tool wait {\"seconds\": 1}",
		"I see it. Clicking.\n@tool click {\"x\": 10, \"y\": 20}",
		"@tool done {\"summary\": \"Opened example.com and clicked the link.\"}")
	sess, _ := s.Create("", []string{"ana"})
	replies, err := s.Send(context.Background(), sess.ID, "open example.com and click the first link")
	if err != nil || len(replies) != 1 || !replies[0].Working || replies[0].Text != "Opening the site." {
		t.Fatalf("Send = %+v, %v", replies, err)
	}
	if _, err := s.Send(context.Background(), sess.ID, "hurry up"); !errors.Is(err, ErrBusy) {
		t.Errorf("a message while working is refused: %v", err)
	}
	got := waitDone(t, s, sess.ID)
	turn := got.Turns[1]
	if turn.Text != "Opening the site.\n\nI see it. Clicking.\n\nOpened example.com and clicked the link." {
		t.Errorf("text = %q", turn.Text)
	}
	actions := []string{}
	for _, st := range turn.Steps {
		actions = append(actions, st.Action)
	}
	if strings.Join(actions, ",") != "open,wait,click,done" {
		t.Errorf("steps = %v", actions)
	}
	if turn.Steps[1].Shot != "1-2.png" || turn.Steps[2].Shot != "1-3.png" || turn.Steps[0].Shot != "" {
		t.Errorf("shots = %+v", turn.Steps)
	}
	if len(d.execs) != 2 || !strings.HasPrefix(d.execs[0], "a1:kitty-open https://example.com") || !strings.Contains(d.execs[1], "xdotool mousemove 10 20 click 1") {
		t.Errorf("execs = %v", d.execs)
	}
	// The model saw the results and a screenshot, and the catalogue in its prompt.
	if !strings.Contains(sa.seen[0][0].Content, "@tool done") {
		t.Error("the prompt carries the tool catalogue")
	}
	second := sa.seen[1]
	last := second[len(second)-1]
	if !strings.Contains(last.Content, "Results of your actions") || !strings.Contains(last.Content, "@tool open") || len(last.Images) != 1 || string(last.Images[0].Data) != "PNGDATA" {
		t.Errorf("results turn = %+v", last)
	}
	if second[len(second)-2].Role != "assistant" || !strings.Contains(second[len(second)-2].Content, "@tool open") {
		t.Error("the kitty's own tool turn is replayed")
	}
	png, err := s.Shot(sess.ID, "1-2.png")
	if err != nil || string(png) != "PNGDATA" {
		t.Errorf("Shot = %q %v", png, err)
	}
	for _, name := range []string{"../x.png", "9-9.png", "1-1.txt"} {
		if _, err := s.Shot(sess.ID, name); err == nil {
			t.Errorf("Shot(%q) must fail", name)
		}
	}
	if _, err := s.Shot("zz", "1-1.png"); err == nil {
		t.Error("Shot of an unknown chat")
	}
	if turn.Usage.Input != 3 {
		t.Errorf("usage adds up across rounds: %+v", turn.Usage)
	}
	// After the task the chat takes messages again.
	sa.replies = []string{"all done"}
	if _, err := s.Send(context.Background(), sess.ID, "thanks"); err != nil {
		t.Error(err)
	}
	if err := s.Delete(sess.ID); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, sess.ID)); !os.IsNotExist(err) {
		t.Error("Delete removes the shots")
	}
}

func TestDesktopTaskEndsWithoutDone(t *testing.T) {
	s, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "That is all I can do.")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	if got.Turns[1].Text != "That is all I can do." || len(got.Turns[1].Steps) != 1 {
		t.Errorf("turn = %+v", got.Turns[1])
	}
}

func TestDesktopTaskFailures(t *testing.T) {
	// A failing action is reported to the model; a failing screenshot too; the steps run out.
	s, d, sa := deskService(t, "@tool click {\"x\": 1, \"y\": 1}\n@tool explode {}", "@tool click {\"x\": 2, \"y\": 2}")
	s.MaxSteps = 2
	d.execErr = errors.New("no display")
	d.shotErr = errors.New("no shot")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	turn := got.Turns[1]
	if !strings.Contains(turn.Text, "stopped after 2 rounds") || len(turn.Steps) != 3 || turn.Steps[0].Error != "no display" || turn.Steps[1].Error == "" {
		t.Errorf("turn = %+v", turn)
	}
	results := sa.seen[1][len(sa.seen[1])-1]
	if !strings.Contains(results.Content, "failed: no display") || !strings.Contains(results.Content, "screenshot failed") || len(results.Images) != 0 {
		t.Errorf("results = %s", results.Content)
	}
	// A brain that fails mid-task.
	s, _, sa = deskService(t, "@tool click {\"x\": 1, \"y\": 1}")
	sess, _ = s.Create("", []string{"ana"})
	go func() {
		time.Sleep(10 * time.Millisecond)
	}()
	sa.err = nil
	first := true
	brain := provider.Func(func(ctx context.Context, m []provider.Message) (provider.Reply, error) {
		if first {
			first = false
			return provider.Reply{Content: "@tool click {\"x\": 1, \"y\": 1}"}, nil
		}
		return provider.Reply{}, errors.New("brain down")
	})
	s.fleet.(*deskFleet).brains["a1"] = brain
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	got = waitDone(t, s, sess.ID)
	if !strings.Contains(got.Turns[1].Text, "could not finish: brain down") {
		t.Errorf("turn = %+v", got.Turns[1])
	}
	// A fleet without a desktop runner.
	f, sa2, _, _ := fleet(t)
	plain := service(t, f)
	sa2.replies = []string{"@tool click {\"x\": 1, \"y\": 1}"}
	sess, _ = plain.Create("", []string{"ana"})
	if _, err := plain.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	got = waitDone(t, plain, sess.ID)
	if !strings.Contains(got.Turns[1].Text, "no desktop runner") {
		t.Errorf("turn = %+v", got.Turns[1])
	}
	// A kitty without a desktop never starts a task: the tool lines are plain text.
	f, _, sb, _ := fleet(t)
	plain = service(t, f)
	sb.replies = []string{"@tool click {\"x\": 1, \"y\": 1}\nI cannot click."}
	sess, _ = plain.Create("", []string{"bo"})
	replies, _ := plain.Send(context.Background(), sess.ID, "go")
	if replies[0].Working || !strings.Contains(replies[0].Text, "I cannot click.") {
		t.Errorf("brain-only reply = %+v", replies[0])
	}
}

func TestDesktopTaskCancelAndGroup(t *testing.T) {
	s, d, sa := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "never")
	d.block = make(chan struct{})
	sess, _ := s.Create("team", []string{"ana", "bo"})
	replies, err := s.Send(context.Background(), sess.ID, "ana, click it")
	if err != nil || len(replies) != 1 || replies[0].From != "ana" {
		t.Fatalf("the group waits for the task: %+v %v", replies, err)
	}
	if !s.Cancel(sess.ID) {
		t.Error("Cancel finds the task")
	}
	got := waitDone(t, s, sess.ID)
	if !strings.Contains(got.Turns[1].Text, "cancelled") || len(sa.seen) != 1 {
		t.Errorf("cancelled turn = %+v", got.Turns[1])
	}
	if s.Cancel(sess.ID) {
		t.Error("nothing to cancel afterwards")
	}
	// Connector calls in the same reply as tool lines run before the task.
	e := &echoConn{}
	reg := connector.NewRegistry()
	reg.Add("echo", e)
	d.conns = reg
	d.block = nil
	sa.replies = []string{"@connector echo ping\n@tool done {\"summary\": \"fin\"}"}
	solo, _ := s.Create("", []string{"ana"})
	replies, _ = s.Send(context.Background(), solo.ID, "go")
	if len(replies[0].Calls) != 1 || len(e.calls) != 1 {
		t.Errorf("connector before the task: %+v", replies[0])
	}
	got = waitDone(t, s, solo.ID)
	if got.Turns[1].Text != "fin" {
		t.Errorf("done summary = %q", got.Turns[1].Text)
	}
	// Deleting a working session cancels its task.
	d.block = make(chan struct{})
	sa.replies = []string{"@tool click {\"x\": 1, \"y\": 1}"}
	busy, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), busy.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(busy.ID); err != nil {
		t.Error(err)
	}
	s.Wait()
	// Edits on a vanished session are ignored.
	s.appendStep("zz", 0, Step{})
	s.markShot(busy.ID, 0, "x")
	_ = kitty.Kitty{}
	_ = soul.Soul{}
}

func TestMarkShotWithoutSteps(t *testing.T) {
	s, _, _ := deskService(t)
	sess, _ := s.Create("", []string{"ana"})
	s.mu.Lock()
	s.sessions[sess.ID].Turns = append(s.sessions[sess.ID].Turns, Turn{From: "ana"})
	s.mu.Unlock()
	s.markShot(sess.ID, 0, "0-0.png")
	got, _ := s.Get(sess.ID)
	if len(got.Turns[0].Steps) != 1 || got.Turns[0].Steps[0].Shot != "0-0.png" {
		t.Errorf("a shot without a step makes one: %+v", got.Turns[0])
	}
	s.closeTask(sess.ID, 0, nil, "", false, "done")
	got, _ = s.Get(sess.ID)
	if got.Turns[0].Text != "(ana finished without a word)" {
		t.Errorf("empty finish = %q", got.Turns[0].Text)
	}
	s.dir = filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(s.dir, nil, 0o600)
	if _, err := s.saveShot(sess.ID, 0, nil); err == nil {
		t.Error("saveShot reports a folder that cannot be made")
	}
}

func TestNoScreenshotAfterActionsThatLeaveTheDisplayAlone(t *testing.T) {
	s, d, sa := deskService(t,
		"@tool run {\"argv\": [\"ls\"]}\n@tool windows {}\n@tool read_screen {}",
		"@tool click {\"x\": 10, \"y\": 20}",
		"@tool done {\"summary\": \"ok\"}")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "look around, then click"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, sess.ID)
	// One screenshot, for the click: the text-only batch brought none, and said so.
	if d.shots != 1 {
		t.Errorf("screenshots = %d, want 1", d.shots)
	}
	if len(sa.seen) < 2 {
		t.Fatalf("the model was asked %d times", len(sa.seen))
	}
	if last := sa.seen[1][len(sa.seen[1])-1]; !strings.Contains(last.Content, "no screenshot comes with this") || len(last.Images) != 0 {
		t.Errorf("the kitty was not told why there is no screenshot: %+v", last)
	}
	if !strings.Contains(strings.Join(d.execs, "\n"), "kitty-ocr") {
		t.Errorf("read_screen did not run the OCR: %v", d.execs)
	}
}

// A kitty whose model calls tools natively has every tool of the text catalogue, ask_help
// among them: the one it asks for the person's hands with.
func TestNativeToolsAreTheCataloguesTools(t *testing.T) {
	s, _, _ := deskService(t, "ok")
	ana, _ := s.fleet.Get("ana")
	have := map[string]bool{}
	for _, tool := range s.toolSet(ana, nil) {
		have[tool.Name] = true
	}
	for _, tool := range desktop.Tools {
		if !have[tool.Name] {
			t.Errorf("the native tools lack %s", tool.Name)
		}
	}
}

// A kitty that sees the same screen four times is told to try another way, or to ask.
func TestStuckKittyIsNudged(t *testing.T) {
	click := "@tool click {\"x\": 1, \"y\": 2}"
	s, d, sa := deskService(t, click, click, click, click, "@tool done {\"summary\": \"gave up\"}")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "click it"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, sess.ID)
	var nudged int
	for _, msgs := range sa.seen {
		if strings.Contains(msgs[len(msgs)-1].Content, "left the display exactly as it was") {
			nudged++
		}
	}
	if nudged != 1 {
		t.Errorf("nudged %d times, want once", nudged)
	}
	if !strings.Contains(strings.Join(d.execs, "\n"), "kitty-ocr") {
		t.Errorf("the screen was not read: %v", d.execs)
	}
}

// When the stuck screen is a CAPTCHA or a code, the kitty does not try again: it asks.
func TestStuckOnACaptchaAsksForHelp(t *testing.T) {
	click := "@tool click {\"x\": 1, \"y\": 2}"
	s, d, sa := deskService(t, click, click, click, click, "@tool done {\"summary\": \"in\"}")
	d.ocr = "Verify\n640,300\tI'm not a robot\n700,300\treCAPTCHA"
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "sign me in"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	var asked bool
	for _, st := range got.Turns[1].Steps {
		if st.Action == "ask_help" && strings.Contains(fmt.Sprint(st.Args["reason"]), "a check that a human is there") {
			asked = true
		}
	}
	if !asked {
		t.Errorf("the kitty did not ask for help: %+v", got.Turns[1].Steps)
	}
	last := sa.seen[len(sa.seen)-1]
	if c := last[len(last)-2].Content + last[len(last)-1].Content; !strings.Contains(c, "You were stuck") {
		t.Errorf("the kitty was not told it was stuck: %q", c)
	}
}

func TestWatchScreen(t *testing.T) {
	st := &taskState{}
	a, b := []byte("a"), []byte("b")
	var got []bool
	for _, shot := range [][]byte{a, a, a, a, a, b, b} {
		got = append(got, st.watchScreen(shot))
	}
	// Stuck on the fourth equal picture, counting again after, and a different one resets it.
	want := []bool{false, false, false, true, false, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("watchScreen = %v, want %v", got, want)
		}
	}
}

// A task's screenshots make an animated GIF of what the kitty did.
func TestReplayOfATask(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{200, 50, 50, 255}), image.Point{}, draw.Src)
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	s, d, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 2}", "@tool done {\"summary\": \"ok\"}")
	d.png = buf.Bytes()
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "click it"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, sess.ID)
	film, err := s.Replay(sess.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if g, err := gif.DecodeAll(bytes.NewReader(film)); err != nil || len(g.Image) == 0 {
		t.Errorf("not a film: %v", err)
	}
	if _, err := s.Replay(sess.ID, 0); !errors.Is(err, replay.ErrNoFrames) {
		t.Errorf("the person's own message has no film: %v", err)
	}
	if _, err := s.Replay(sess.ID, 9); err == nil || !strings.HasPrefix(err.Error(), "no such turn") {
		t.Errorf("a turn that is not there: %v", err)
	}
	if _, err := s.Replay("zz", 0); err == nil {
		t.Error("a chat that is not there")
	}
}

func TestKittyBusy(t *testing.T) {
	s, _, _ := deskService(t, "@tool wait {\"seconds\": 1}", "@tool done {\"summary\": \"ok\"}")
	if s.KittyBusy("a1") {
		t.Error("busy before any task")
	}
	sess, _ := s.Create("", []string{"ana"})
	s.mu.Lock()
	s.sessions[sess.ID].Turns = append(s.sessions[sess.ID].Turns, Turn{Kitty: "a1", Working: true})
	s.mu.Unlock()
	if !s.KittyBusy("a1") || s.KittyBusy("b1") {
		t.Error("a kitty with a working turn is busy, another is not")
	}
}
